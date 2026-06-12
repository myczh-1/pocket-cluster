package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

type MetadataSnapshot struct {
	LastEventID string
	Nodes       []types.Node
	Files       []types.File
	Chunks      []types.Chunk
	Replicas    []types.Replica
}

type PersistedSnapshot struct {
	SnapshotID  string
	CreatedAt   time.Time
	CreatedBy   string
	LastEventID string
	Data        string // JSON-encoded MetadataSnapshot
}

func (s *Store) MetadataSnapshot() (*MetadataSnapshot, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	nodes, err := listNodesTx(tx)
	if err != nil {
		return nil, err
	}
	files, err := listAllFilesIncludingDeletedTx(tx)
	if err != nil {
		return nil, err
	}
	chunks, err := listChunksTx(tx)
	if err != nil {
		return nil, err
	}
	replicas, err := listReplicasTx(tx)
	if err != nil {
		return nil, err
	}
	lastEventID, err := latestEventIDTx(tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &MetadataSnapshot{
		LastEventID: lastEventID,
		Nodes:       nodes,
		Files:       files,
		Chunks:      chunks,
		Replicas:    replicas,
	}, nil
}

// SaveSnapshot persists a metadata snapshot for bootstrap and event pruning.
func (s *Store) SaveSnapshot(snapshotID, createdBy, lastEventID string, data []byte) error {
	_, err := s.db.Exec(`INSERT INTO snapshots (snapshot_id, created_at, created_by, last_event_id, data) VALUES (?, ?, ?, ?, ?)`,
		snapshotID, time.Now().UnixMilli(), createdBy, lastEventID, string(data))
	return err
}

// LoadLatestSnapshot returns the most recently created snapshot, or nil if none exists.
func (s *Store) LoadLatestSnapshot() (*PersistedSnapshot, error) {
	row := s.db.QueryRow(`SELECT snapshot_id, created_at, created_by, last_event_id, data FROM snapshots ORDER BY created_at DESC LIMIT 1`)
	var ps PersistedSnapshot
	var ts int64
	if err := row.Scan(&ps.SnapshotID, &ts, &ps.CreatedBy, &ps.LastEventID, &ps.Data); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	ps.CreatedAt = time.UnixMilli(ts)
	return &ps, nil
}

// EventCount returns the total number of events in the log.
func (s *Store) EventCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&count)
	return count, err
}

// PruneEventsBefore deletes events with event_id <= beforeEventID.
// This is safe to call after a snapshot captures state up to beforeEventID.
func (s *Store) PruneEventsBefore(beforeEventID string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM events WHERE event_id <= ?`, beforeEventID)
	if err != nil {
		return 0, err
	}
	// Also clean up peer_pushed_events for deleted events.
	s.db.Exec(`DELETE FROM peer_pushed_events WHERE event_id IN (
		SELECT p.event_id FROM peer_pushed_events p
		LEFT JOIN events e ON p.event_id = e.event_id
		WHERE e.event_id IS NULL
	)`)
	return res.RowsAffected()
}

// PruneOldSnapshots deletes all snapshots except the most recent one.
func (s *Store) PruneOldSnapshots() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM snapshots WHERE id NOT IN (SELECT id FROM snapshots ORDER BY created_at DESC LIMIT 1)`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// LoadSnapshot applies a metadata snapshot to the store by upserting all entities.
// This is used by new nodes to bootstrap from a peer's snapshot.
func (s *Store) LoadSnapshot(snap *MetadataSnapshot) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, n := range snap.Nodes {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO nodes (node_id, name, platform, address, address_candidates, last_working_address, public_key, total_bytes, used_bytes, available_bytes, status, trusted, last_seen, joined_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.NodeID, n.Name, n.Platform, n.Address, mustJSON(n.AddressCandidates), n.LastWorkingAddress, n.PublicKey, n.TotalBytes, n.UsedBytes, n.AvailableBytes, n.Status, boolToInt(n.Trusted), n.LastSeen.UnixMilli(), n.JoinedAt.UnixMilli()); err != nil {
			return err
		}
	}
	// Clear and rebuild FTS index inside the transaction.
	if _, err := tx.Exec(`DELETE FROM files_fts`); err != nil {
		return err
	}
	for _, f := range snap.Files {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO files (file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			f.FileID, f.Name, f.Path, boolToInt(f.IsDir), f.SizeBytes, f.MimeType, f.VersionID, f.ParentVersionID, mustJSON(f.ChunkIDs), timeMillis(f.CreatedAt), timeMillis(f.ModifiedAt), f.ModifiedBy, boolToInt(f.Deleted), f.ConflictOf); err != nil {
			return err
		}
		if !f.Deleted {
			if _, err := tx.Exec(`INSERT INTO files_fts (file_id, name, path) VALUES (?, ?, ?)`, f.FileID, f.Name, f.Path); err != nil {
				return err
			}
		}
	}
	for _, c := range snap.Chunks {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO chunks (chunk_id, size_bytes, stored_at) VALUES (?, ?, ?)`,
			c.ChunkID, c.SizeBytes, timeMillis(c.StoredAt)); err != nil {
			return err
		}
	}
	for _, r := range snap.Replicas {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO replicas (chunk_id, node_id, status, stored_at, verified_at) VALUES (?, ?, ?, ?, ?)`,
			r.ChunkID, r.NodeID, r.Status, timeMillis(r.StoredAt), timeMillis(r.VerifiedAt)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic("mustJSON: " + err.Error())
	}
	return string(b)
}

func listNodesTx(tx *sql.Tx) ([]types.Node, error) {
	rows, err := tx.Query(`SELECT node_id, name, platform, address, address_candidates, last_working_address, public_key, total_bytes, used_bytes, available_bytes, status, trusted, last_seen, joined_at FROM nodes`)
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

func listAllFilesIncludingDeletedTx(tx *sql.Tx) ([]types.File, error) {
	rows, err := tx.Query(`SELECT file_id, name, path, is_dir, size_bytes, mime_type, version_id, parent_version_id, chunk_ids, created_at, modified_at, modified_by, deleted, conflict_of FROM files ORDER BY path ASC`)
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

func listChunksTx(tx *sql.Tx) ([]types.Chunk, error) {
	rows, err := tx.Query(`SELECT chunk_id, size_bytes, stored_at FROM chunks`)
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

func listReplicasTx(tx *sql.Tx) ([]types.Replica, error) {
	rows, err := tx.Query(`SELECT chunk_id, node_id, status, stored_at, verified_at FROM replicas ORDER BY chunk_id ASC, node_id ASC`)
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

func latestEventIDTx(tx *sql.Tx) (string, error) {
	row := tx.QueryRow(`SELECT event_id FROM events ORDER BY timestamp DESC, node_id DESC, seq DESC LIMIT 1`)
	var eventID string
	if err := row.Scan(&eventID); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return eventID, nil
}
