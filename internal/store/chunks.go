package store

import (
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

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
