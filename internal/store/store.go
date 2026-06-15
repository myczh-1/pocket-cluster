package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

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

var schemaVersion = 6

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}
	var current int
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	if current < 1 {
		if err := s.migrateV1(); err != nil {
			return err
		}
	}
	if current < 2 {
		if err := s.migrateV2(); err != nil {
			return err
		}
	}
	if current < 3 {
		if err := s.migrateV3(); err != nil {
			return err
		}
	}
	if current < 4 {
		if err := s.migrateV4(); err != nil {
			return err
		}
	}
	if current < 5 {
		if err := s.migrateV5(); err != nil {
			return err
		}
	}
	if current < 6 {
		if err := s.migrateV6(); err != nil {
			return err
		}
	}

	if current < schemaVersion {
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("begin schema version tx: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM schema_version`); err != nil {
			tx.Rollback()
			return fmt.Errorf("delete schema_version: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (?)`, schemaVersion); err != nil {
			tx.Rollback()
			return fmt.Errorf("insert schema_version: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit schema_version: %w", err)
		}
	}
	return nil
}

func (s *Store) migrateV1() error {
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
			return fmt.Errorf("migrate v1: %w", err)
		}
	}
	return nil
}

func (s *Store) migrateV2() error {
	if err := s.addColumnIfMissing("nodes", "address_candidates", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("nodes", "last_working_address", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.seedFileSearchIndex(); err != nil {
		return err
	}
	if err := s.clearLoopbackAddresses(); err != nil {
		return err
	}
	return nil
}

func (s *Store) migrateV3() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS pending_joins (
		node_id TEXT PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		platform TEXT NOT NULL DEFAULT '',
		address TEXT NOT NULL DEFAULT '',
		public_key TEXT NOT NULL DEFAULT '',
		total_bytes INTEGER NOT NULL DEFAULT 0,
		available_bytes INTEGER NOT NULL DEFAULT 0,
		requested_at INTEGER NOT NULL DEFAULT 0,
		observed_address TEXT NOT NULL DEFAULT '',
		expires_at INTEGER NOT NULL DEFAULT 0
	)`)
	return err
}

func (s *Store) migrateV4() error {
	return s.addColumnIfMissing("pending_joins", "observed_address", "TEXT NOT NULL DEFAULT ''")
}

func (s *Store) migrateV5() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS snapshots (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		snapshot_id TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		created_by TEXT NOT NULL DEFAULT '',
		last_event_id TEXT NOT NULL DEFAULT '',
		data TEXT NOT NULL DEFAULT '{}'
	)`)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_snapshots_created_at ON snapshots(created_at)`)
	return err
}
func (s *Store) migrateV6() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS file_chunks (
		file_id TEXT NOT NULL,
		chunk_id TEXT NOT NULL,
		position INTEGER NOT NULL,
		PRIMARY KEY (file_id, chunk_id, position)
	)`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_file_chunks_chunk_id ON file_chunks(chunk_id)`); err != nil {
		return err
	}
	files, err := s.ListAllFilesIncludingDeleted()
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, f := range files {
		if err := upsertFileChunksTx(tx, f.FileID, f.ChunkIDs); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) addColumnIfMissing(table, column, definition string) error {
	if !isMigrationIdentifier(table) || !isMigrationIdentifier(column) || !isMigrationColumnDefinition(definition) {
		return fmt.Errorf("unsafe migration column definition: %s.%s %s", table, column, definition)
	}
	exists, err := s.columnExists(table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", quoteMigrationIdentifier(table), quoteMigrationIdentifier(column), definition)); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	return nil
}

func (s *Store) columnExists(table, column string) (bool, error) {
	if !isMigrationIdentifier(table) || !isMigrationIdentifier(column) {
		return false, fmt.Errorf("unsafe migration identifier: %s.%s", table, column)
	}
	rows, err := s.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", quoteMigrationIdentifier(table)))
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
func isMigrationIdentifier(identifier string) bool {
	if identifier == "" {
		return false
	}
	for i := 0; i < len(identifier); i++ {
		c := identifier[i]
		if c >= 'a' && c <= 'z' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			continue
		}
		if c == '_' {
			continue
		}
		if i > 0 && c >= '0' && c <= '9' {
			continue
		}
		return false
	}
	return true
}

func quoteMigrationIdentifier(identifier string) string {
	return `"` + identifier + `"`
}

func isMigrationColumnDefinition(definition string) bool {
	switch definition {
	case "TEXT NOT NULL DEFAULT '[]'", "TEXT NOT NULL DEFAULT ''":
		return true
	default:
		return false
	}
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

func (s *Store) clearLoopbackAddresses() error {
	_, err := s.db.Exec(`UPDATE nodes SET address = '', address_candidates = '[]', last_working_address = '' WHERE address LIKE 'localhost:%' OR address LIKE '127.0.0.1:%' OR address LIKE '[::1]:%'`)
	return err
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
	if err := json.Unmarshal([]byte(candidatesJSON), &n.AddressCandidates); err != nil {
		return nil, fmt.Errorf("unmarshal address_candidates: %w", err)
	}
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
	if err := json.Unmarshal([]byte(chunkJSON), &f.ChunkIDs); err != nil {
		return nil, fmt.Errorf("unmarshal chunk_ids: %w", err)
	}
	return &f, nil
}

func scanFileRows(rows *sql.Rows) (*types.File, error) {
	return scanFile(rows)
}
