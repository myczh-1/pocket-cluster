package store

import (
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

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
