package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	_ "modernc.org/sqlite"

	"github.com/pocketcluster/agent/internal/types"
)

type Store struct {
	db *sql.DB
}

func Open(dataDir string) (*Store, error) {
	dbPath := filepath.Join(dataDir, "metadata.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS nodes (
			node_id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			platform TEXT NOT NULL DEFAULT '',
			address TEXT NOT NULL DEFAULT '',
			public_key TEXT NOT NULL DEFAULT '',
			total_bytes INTEGER NOT NULL DEFAULT 0,
			used_bytes INTEGER NOT NULL DEFAULT 0,
			available_bytes INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'offline',
			trusted INTEGER NOT NULL DEFAULT 0,
			last_seen INTEGER NOT NULL DEFAULT 0,
			joined_at INTEGER NOT NULL DEFAULT 0,
			address_candidates TEXT NOT NULL DEFAULT '[]',
			last_working_address TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS files (
			file_id TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL UNIQUE,
			is_dir INTEGER NOT NULL DEFAULT 0,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			mime_type TEXT NOT NULL DEFAULT '',
			version_id TEXT NOT NULL DEFAULT '',
			parent_version_id TEXT NOT NULL DEFAULT '',
			chunk_ids TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL DEFAULT 0,
			modified_at INTEGER NOT NULL DEFAULT 0,
			modified_by TEXT NOT NULL DEFAULT '',
			deleted INTEGER NOT NULL DEFAULT 0,
			conflict_of TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS chunks (
			chunk_id TEXT PRIMARY KEY,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			stored_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS replicas (
			chunk_id TEXT NOT NULL,
			node_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'available',
			stored_at INTEGER NOT NULL DEFAULT 0,
			verified_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (chunk_id, node_id)
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			event_id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			node_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			timestamp INTEGER NOT NULL,
			payload TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE TABLE IF NOT EXISTS invites (
			token_hash TEXT PRIMARY KEY,
			created_at INTEGER NOT NULL DEFAULT 0,
			expires_at INTEGER NOT NULL DEFAULT 0,
			used_at INTEGER NOT NULL DEFAULT 0,
			created_by TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS peer_pushed_events (
			peer_node_id TEXT NOT NULL,
			event_id TEXT NOT NULL,
			pushed_at INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (peer_node_id, event_id)
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS files_fts USING fts5(file_id UNINDEXED, name, path, tokenize = 'unicode61')`,
		`CREATE INDEX IF NOT EXISTS idx_files_path ON files(path)`,
		`CREATE INDEX IF NOT EXISTS idx_events_node_seq ON events(node_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_invites_expires_at ON invites(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_peer_pushed_events_peer ON peer_pushed_events(peer_node_id)`,
	}
	for _, m := range migrations {
		if _, err := s.db.Exec(m); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	if err := s.addColumnIfMissing("nodes", "address_candidates", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("nodes", "last_working_address", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.seedFileSearchIndex(); err != nil {
		return err
	}
	return nil
}

func (s *Store) addColumnIfMissing(table, column, definition string) error {
	exists, err := s.columnExists(table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, definition)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *Store) columnExists(table, column string) (bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) seedFileSearchIndex() error {
	var indexed int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM files_fts`).Scan(&indexed); err != nil {
		return err
	}
	if indexed != 0 {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO files_fts (file_id, name, path) SELECT file_id, name, path FROM files WHERE deleted = 0`)
	return err
}

func (s *Store) indexFile(f *types.File) error {
	if _, err := s.db.Exec(`DELETE FROM files_fts WHERE file_id = ?`, f.FileID); err != nil {
		return err
	}
	if f.Deleted {
		return nil
	}
	_, err := s.db.Exec(`INSERT INTO files_fts (file_id, name, path) VALUES (?, ?, ?)`, f.FileID, f.Name, f.Path)
	return err
}

func (s *Store) deleteFileIndex(fileID string) error {
	_, err := s.db.Exec(`DELETE FROM files_fts WHERE file_id = ?`, fileID)
	return err
}

func fileSearchQuery(keyword string) string {
	var parts []string
	var token []rune
	flush := func() {
		if len(token) == 0 {
			return
		}
		parts = append(parts, `"`+string(token)+`"*`)
		token = token[:0]
	}
	for _, r := range keyword {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			token = append(token, unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return strings.Join(parts, " ")
}
func isDirectChild(parent, child string) bool {
	if parent == "" {
		parent = "/"
	}
	if parent == "/" {
		if child == "/" || !strings.HasPrefix(child, "/") {
			return false
		}
		return !strings.Contains(child[1:], "/")
	}
	prefix := strings.TrimRight(parent, "/") + "/"
	if !strings.HasPrefix(child, prefix) {
		return false
	}
	return !strings.Contains(child[len(prefix):], "/")
}

// Node operations

func (s *Store) UpsertNode(n *types.Node) error {
	candidates, err := json.Marshal(n.AddressCandidates)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO nodes
		(node_id, name, platform, address, address_candidates, last_working_address, public_key, total_bytes, used_bytes, available_bytes, status, trusted, last_seen, joined_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE nodes.name END,
			platform = CASE WHEN excluded.platform != '' THEN excluded.platform ELSE nodes.platform END,
			address = CASE WHEN excluded.address != '' THEN excluded.address ELSE nodes.address END,
			address_candidates = CASE WHEN excluded.address_candidates != '[]' THEN excluded.address_candidates ELSE nodes.address_candidates END,
			last_working_address = CASE WHEN excluded.last_working_address != '' THEN excluded.last_working_address ELSE nodes.last_working_address END,
			public_key = CASE WHEN excluded.public_key != '' THEN excluded.public_key ELSE nodes.public_key END,
			total_bytes = CASE WHEN excluded.total_bytes != 0 THEN excluded.total_bytes ELSE nodes.total_bytes END,
			used_bytes = excluded.used_bytes,
			available_bytes = CASE WHEN excluded.available_bytes != 0 THEN excluded.available_bytes ELSE nodes.available_bytes END,
			status = CASE WHEN excluded.status != '' THEN excluded.status ELSE nodes.status END,
			trusted = CASE WHEN excluded.trusted != 0 THEN excluded.trusted ELSE nodes.trusted END,
			last_seen = CASE WHEN excluded.last_seen != 0 THEN excluded.last_seen ELSE nodes.last_seen END,
			joined_at = CASE WHEN excluded.joined_at != 0 THEN excluded.joined_at ELSE nodes.joined_at END`,
		n.NodeID, n.Name, n.Platform, n.Address, string(candidates), n.LastWorkingAddress, n.PublicKey, n.TotalBytes, n.UsedBytes, n.AvailableBytes,
		n.Status, boolToInt(n.Trusted), timeMillis(n.LastSeen), timeMillis(n.JoinedAt))
	return err
}

func (s *Store) UpdateNodeStatus(nodeID, status string, lastSeen time.Time) error {
	_, err := s.db.Exec(`UPDATE nodes SET status = ?, last_seen = ? WHERE node_id = ?`,
		status, timeMillis(lastSeen), nodeID)
	return err
}

func (s *Store) UpdateNodeLastWorkingAddress(nodeID, address string, lastSeen time.Time) error {
	_, err := s.db.Exec(`UPDATE nodes SET last_working_address = ?, status = 'online', last_seen = ? WHERE node_id = ?`,
		address, timeMillis(lastSeen), nodeID)
	return err
}

func (s *Store) MarkStaleNodesOffline(cutoff time.Time) (int64, error) {
	res, err := s.db.Exec(`UPDATE nodes SET status = 'offline' WHERE status = 'online' AND trusted = 1 AND last_seen > 0 AND last_seen < ?`,
		timeMillis(cutoff))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) UpdateNodeFull(n *types.Node) error {
	candidates, err := json.Marshal(n.AddressCandidates)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO nodes
		(node_id, name, platform, address, address_candidates, last_working_address, public_key, total_bytes, used_bytes, available_bytes, status, trusted, last_seen, joined_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET
			name = excluded.name,
			platform = excluded.platform,
			address = excluded.address,
			address_candidates = excluded.address_candidates,
			last_working_address = excluded.last_working_address,
			public_key = excluded.public_key,
			total_bytes = excluded.total_bytes,
			used_bytes = excluded.used_bytes,
			available_bytes = excluded.available_bytes,
			status = excluded.status,
			trusted = excluded.trusted,
			last_seen = excluded.last_seen,
			joined_at = CASE WHEN excluded.joined_at != 0 THEN excluded.joined_at ELSE nodes.joined_at END`,
		n.NodeID, n.Name, n.Platform, n.Address, string(candidates), n.LastWorkingAddress, n.PublicKey, n.TotalBytes, n.UsedBytes, n.AvailableBytes,
		n.Status, boolToInt(n.Trusted), timeMillis(n.LastSeen), timeMillis(n.JoinedAt))
	return err
}
func (s *Store) GetNode(nodeID string) (*types.Node, error) {
	row := s.db.QueryRow(`SELECT node_id, name, platform, address, address_candidates, last_working_address, public_key, total_bytes, used_bytes, available_bytes, status, trusted, last_seen, joined_at FROM nodes WHERE node_id = ?`, nodeID)
	return scanNode(row)
}

func (s *Store) HasTrustedNodeAtAddress(address string) bool {
	var count int
	s.db.QueryRow(`SELECT COUNT(*) FROM nodes WHERE trusted = 1 AND (address = ? OR last_working_address = ? OR address_candidates LIKE ?)`,
		address, address, "%\""+address+"\"%").Scan(&count)
	return count > 0
}

func (s *Store) ListNodes() ([]types.Node, error) {
	rows, err := s.db.Query(`SELECT node_id, name, platform, address, address_candidates, last_working_address, public_key, total_bytes, used_bytes, available_bytes, status, trusted, last_seen, joined_at FROM nodes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []types.Node
	for rows.Next() {
		n, err := scanNodeRows(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, *n)
	}
	return nodes, rows.Err()
}

// File operations

func (s *Store) UpsertFile(f *types.File) error {
	chunkJSON, err := json.Marshal(f.ChunkIDs)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT OR REPLACE INTO files
		(file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.FileID, f.Name, f.Path, boolToInt(f.IsDir), f.SizeBytes, f.MimeType,
		f.VersionID, f.ParentVersionID, string(chunkJSON),
		f.CreatedAt.UnixMilli(), f.ModifiedAt.UnixMilli(), f.ModifiedBy,
		boolToInt(f.Deleted), f.ConflictOf)
	if err != nil {
		return err
	}
	return s.indexFile(f)
}

func (s *Store) GetFile(path string) (*types.File, error) {
	row := s.db.QueryRow(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files WHERE path = ? AND deleted = 0`, path)
	return scanFile(row)
}

func (s *Store) GetFileByID(fileID string) (*types.File, error) {
	row := s.db.QueryRow(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files WHERE file_id = ?`, fileID)
	return scanFile(row)
}

func (s *Store) ListFiles(dirPath string) ([]types.File, error) {
	rows, err := s.db.Query(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files WHERE deleted = 0 ORDER BY path ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []types.File
	for rows.Next() {
		f, err := scanFileRows(rows)
		if err != nil {
			return nil, err
		}
		if isDirectChild(dirPath, f.Path) {
			files = append(files, *f)
		}
	}
	return files, rows.Err()
}

func (s *Store) MarkFileDeleted(path string, deletedBy string) error {
	_, err := s.db.Exec(`UPDATE files SET deleted = 1, modified_by = ?, modified_at = ? WHERE path = ? AND deleted = 0`,
		deletedBy, time.Now().UnixMilli(), path)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM files_fts WHERE file_id IN (SELECT file_id FROM files WHERE path = ?)`, path)
	return err
}

func (s *Store) RenameFile(fileID, oldPath, newPath string, modifiedBy string, modifiedAt time.Time) error {
	name := filepath.Base(newPath)
	_, err := s.db.Exec(`UPDATE files SET path = ?, name = ?, modified_by = ?, modified_at = ? WHERE file_id = ? AND path = ? AND deleted = 0`,
		newPath, name, modifiedBy, timeMillis(modifiedAt), fileID, oldPath)
	if err != nil {
		return err
	}
	if err := s.deleteFileIndex(fileID); err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO files_fts (file_id, name, path) VALUES (?, ?, ?)`, fileID, name, newPath)
	return err
}

func (s *Store) MarkFileConflict(originalFileID, conflictFileID, conflictPath, conflictVersionID, parentVersionID, modifiedBy string, modifiedAt time.Time) error {
	name := filepath.Base(conflictPath)
	ts := timeMillis(modifiedAt)
	res, err := s.db.Exec(`UPDATE files SET path = ?, name = ?, version_id = ?, parent_version_id = ?, conflict_of = ?, modified_by = ?, modified_at = ? WHERE file_id = ?`,
		conflictPath, name, conflictVersionID, parentVersionID, originalFileID, modifiedBy, ts, conflictFileID)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 0 {
		return s.indexFile(&types.File{FileID: conflictFileID, Name: name, Path: conflictPath})
	}
	_, err = s.db.Exec(`INSERT INTO files (file_id, name, path, version_id, parent_version_id, created_at, modified_at, modified_by, conflict_of) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		conflictFileID, name, conflictPath, conflictVersionID, parentVersionID, ts, ts, modifiedBy, originalFileID)
	if err != nil {
		return err
	}
	return s.indexFile(&types.File{FileID: conflictFileID, Name: name, Path: conflictPath})
}

func (s *Store) ListAllFilesIncludingDeleted() ([]types.File, error) {
	rows, err := s.db.Query(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files ORDER BY path ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []types.File
	for rows.Next() {
		f, err := scanFileRows(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, *f)
	}
	return files, rows.Err()
}

func (s *Store) ListAllFiles() ([]types.File, error) {
	rows, err := s.db.Query(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files WHERE deleted = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []types.File
	for rows.Next() {
		f, err := scanFileRows(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, *f)
	}
	return files, rows.Err()
}

func (s *Store) SearchFiles(keyword string) ([]types.File, error) {
	query := fileSearchQuery(keyword)
	if query == "" {
		return []types.File{}, nil
	}
	rows, err := s.db.Query(`SELECT f.file_id, f.name, f.path, f.is_dir, f.size_bytes, f.mime_type, f.version_id, f.parent_version_id, f.chunk_ids, f.created_at, f.modified_at, f.modified_by, f.deleted, f.conflict_of
		FROM files f
		JOIN files_fts ON files_fts.file_id = f.file_id
		WHERE f.deleted = 0 AND files_fts MATCH ?
		ORDER BY f.path ASC`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var files []types.File
	for rows.Next() {
		f, err := scanFileRows(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, *f)
	}
	return files, rows.Err()
}

// Chunk operations

func (s *Store) UpsertChunk(c *types.Chunk) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO chunks (chunk_id, size_bytes, stored_at) VALUES (?, ?, ?)`,
		c.ChunkID, c.SizeBytes, c.StoredAt.UnixMilli())
	return err
}

func (s *Store) GetChunk(chunkID string) (*types.Chunk, error) {
	row := s.db.QueryRow(`SELECT chunk_id, size_bytes, stored_at FROM chunks WHERE chunk_id = ?`, chunkID)
	var c types.Chunk
	var ts int64
	if err := row.Scan(&c.ChunkID, &c.SizeBytes, &ts); err != nil {
		return nil, err
	}
	c.StoredAt = time.UnixMilli(ts)
	return &c, nil
}

// Replica operations
func (s *Store) ListChunks() ([]types.Chunk, error) {
	rows, err := s.db.Query(`SELECT chunk_id, size_bytes, stored_at FROM chunks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var chunks []types.Chunk
	for rows.Next() {
		var c types.Chunk
		var ts int64
		if err := rows.Scan(&c.ChunkID, &c.SizeBytes, &ts); err != nil {
			return nil, err
		}
		c.StoredAt = time.UnixMilli(ts)
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

func (s *Store) UpsertReplica(r *types.Replica) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO replicas (chunk_id, node_id, status, stored_at, verified_at) VALUES (?, ?, ?, ?, ?)`,
		r.ChunkID, r.NodeID, r.Status, r.StoredAt.UnixMilli(), r.VerifiedAt.UnixMilli())
	return err
}

func (s *Store) ListReplicas() ([]types.Replica, error) {
	rows, err := s.db.Query(`SELECT chunk_id, node_id, status, stored_at, verified_at FROM replicas ORDER BY chunk_id ASC, node_id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reps []types.Replica
	for rows.Next() {
		var r types.Replica
		var stored, verified int64
		if err := rows.Scan(&r.ChunkID, &r.NodeID, &r.Status, &stored, &verified); err != nil {
			return nil, err
		}
		r.StoredAt = time.UnixMilli(stored)
		r.VerifiedAt = time.UnixMilli(verified)
		reps = append(reps, r)
	}
	return reps, rows.Err()
}

func (s *Store) MarkReplicaRemoved(chunkID, nodeID string, verifiedAt time.Time) error {
	_, err := s.db.Exec(`UPDATE replicas SET status = 'removed', verified_at = ? WHERE chunk_id = ? AND node_id = ?`,
		timeMillis(verifiedAt), chunkID, nodeID)
	return err
}

func (s *Store) GetReplicas(chunkID string) ([]types.Replica, error) {
	rows, err := s.db.Query(`SELECT chunk_id, node_id, status, stored_at, verified_at FROM replicas WHERE chunk_id = ?`, chunkID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reps []types.Replica
	for rows.Next() {
		var r types.Replica
		var stored, verified int64
		if err := rows.Scan(&r.ChunkID, &r.NodeID, &r.Status, &stored, &verified); err != nil {
			return nil, err
		}
		r.StoredAt = time.UnixMilli(stored)
		r.VerifiedAt = time.UnixMilli(verified)
		reps = append(reps, r)
	}
	return reps, rows.Err()
}
func (s *Store) GetNodeChunkIDs(nodeID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT chunk_id FROM replicas WHERE node_id = ? AND status = 'available'`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Invite operations

func (s *Store) CreateInvite(invite *types.Invite) error {
	_, err := s.db.Exec(`INSERT INTO invites (token_hash, created_at, expires_at, used_at, created_by) VALUES (?, ?, ?, ?, ?)`,
		invite.TokenHash, timeMillis(invite.CreatedAt), timeMillis(invite.ExpiresAt), timeMillis(invite.UsedAt), invite.CreatedBy)
	return err
}

func (s *Store) UseInvite(tokenHash string, now time.Time) (bool, error) {
	res, err := s.db.Exec(`UPDATE invites SET used_at = ? WHERE token_hash = ? AND used_at = 0 AND expires_at > ?`,
		timeMillis(now), tokenHash, timeMillis(now))
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	return rows == 1, err
}

// Event operations

func (s *Store) InsertEvent(e *types.Event) (bool, error) {
	res, err := s.db.Exec(`INSERT OR IGNORE INTO events (event_id, type, node_id, seq, timestamp, payload) VALUES (?, ?, ?, ?, ?, ?)`,
		e.EventID, string(e.Type), e.NodeID, e.Seq, e.Timestamp.UnixMilli(), string(e.Payload))
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	return rows > 0, err
}

func (s *Store) GetEventsSince(sinceEventID string, limit int) ([]types.Event, error) {
	if limit <= 0 {
		limit = 1000
	}
	var rows *sql.Rows
	var err error
	if sinceEventID == "" {
		rows, err = s.db.Query(`SELECT event_id, type, node_id, seq, timestamp, payload FROM events ORDER BY node_id ASC, seq ASC LIMIT ?`, limit)
	} else {
		rows, err = s.db.Query(`SELECT event_id, type, node_id, seq, timestamp, payload FROM events WHERE event_id > ? ORDER BY event_id ASC LIMIT ?`, sinceEventID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []types.Event
	for rows.Next() {
		var e types.Event
		var ts int64
		var typ string
		var payload string
		if err := rows.Scan(&e.EventID, &typ, &e.NodeID, &e.Seq, &ts, &payload); err != nil {
			return nil, err
		}
		e.Type = types.EventType(typ)
		e.Timestamp = time.UnixMilli(ts)
		e.Payload = json.RawMessage(payload)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) GetUnpushedEvents(peerNodeID string, limit int) ([]types.Event, error) {
	if limit <= 0 {
		limit = 1000
	}
	rows, err := s.db.Query(`SELECT e.event_id, e.type, e.node_id, e.seq, e.timestamp, e.payload
		FROM events e
		WHERE NOT EXISTS (
			SELECT 1 FROM peer_pushed_events p
			WHERE p.peer_node_id = ? AND p.event_id = e.event_id
		)
		ORDER BY e.node_id ASC, e.seq ASC
		LIMIT ?`, peerNodeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []types.Event
	for rows.Next() {
		var e types.Event
		var ts int64
		var typ string
		var payload string
		if err := rows.Scan(&e.EventID, &typ, &e.NodeID, &e.Seq, &ts, &payload); err != nil {
			return nil, err
		}
		e.Type = types.EventType(typ)
		e.Timestamp = time.UnixMilli(ts)
		e.Payload = json.RawMessage(payload)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) MarkEventsPushed(peerNodeID string, events []types.Event, pushedAt time.Time) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range events {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO peer_pushed_events (peer_node_id, event_id, pushed_at) VALUES (?, ?, ?)`,
			peerNodeID, e.EventID, timeMillis(pushedAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) NextSeq(nodeID string) (int64, error) {
	row := s.db.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM events WHERE node_id = ?`, nodeID)
	var seq int64
	if err := row.Scan(&seq); err != nil {
		return 0, err
	}
	return seq, nil
}

func (s *Store) LatestEventID() (string, error) {
	row := s.db.QueryRow(`SELECT event_id FROM events ORDER BY timestamp DESC, node_id DESC, seq DESC LIMIT 1`)
	var eventID string
	if err := row.Scan(&eventID); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return eventID, nil
}

// Storage stats

func (s *Store) ChunkCount() (int, error) {
	row := s.db.QueryRow(`SELECT COUNT(*) FROM chunks`)
	var count int
	err := row.Scan(&count)
	return count, err
}

func (s *Store) TotalChunkBytes() (int64, error) {
	row := s.db.QueryRow(`SELECT COALESCE(SUM(size_bytes), 0) FROM chunks`)
	var total int64
	err := row.Scan(&total)
	return total, err
}

// Helpers

func timeMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intToBool(i int) bool {
	return i != 0
}

type scannable interface {
	Scan(dest ...any) error
}

func scanNode(row scannable) (*types.Node, error) {
	var n types.Node
	var lastSeen, joinedAt int64
	var trusted int
	var candidatesJSON string
	if err := row.Scan(&n.NodeID, &n.Name, &n.Platform, &n.Address, &candidatesJSON, &n.LastWorkingAddress, &n.PublicKey,
		&n.TotalBytes, &n.UsedBytes, &n.AvailableBytes,
		&n.Status, &trusted, &lastSeen, &joinedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(candidatesJSON), &n.AddressCandidates)
	n.Trusted = intToBool(trusted)
	n.LastSeen = time.UnixMilli(lastSeen)
	n.JoinedAt = time.UnixMilli(joinedAt)
	return &n, nil
}

func scanNodeRows(rows *sql.Rows) (*types.Node, error) {
	return scanNode(rows)
}

func scanFile(row scannable) (*types.File, error) {
	var f types.File
	var chunkJSON string
	var created, modified int64
	var isDir, deleted int
	if err := row.Scan(&f.FileID, &f.Name, &f.Path, &isDir, &f.SizeBytes, &f.MimeType,
		&f.VersionID, &f.ParentVersionID, &chunkJSON,
		&created, &modified, &f.ModifiedBy, &deleted, &f.ConflictOf); err != nil {
		return nil, err
	}
	f.IsDir = intToBool(isDir)
	f.Deleted = intToBool(deleted)
	f.CreatedAt = time.UnixMilli(created)
	f.ModifiedAt = time.UnixMilli(modified)
	_ = json.Unmarshal([]byte(chunkJSON), &f.ChunkIDs)
	return &f, nil
}

func scanFileRows(rows *sql.Rows) (*types.File, error) {
	return scanFile(rows)
}
