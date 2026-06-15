package store

import (
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func (s *Store) CreatePendingJoin(pj *types.PendingJoin) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO pending_joins (node_id, name, platform, address, observed_address, public_key, total_bytes, available_bytes, requested_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		pj.NodeID, pj.Name, pj.Platform, pj.Address, pj.ObservedAddress, pj.PublicKey, pj.TotalBytes, pj.AvailableBytes, pj.RequestedAt.UnixMilli(), pj.ExpiresAt.UnixMilli())
	return err
}

func (s *Store) ListPendingJoins() ([]types.PendingJoin, error) {
	rows, err := s.db.Query(`SELECT node_id, name, platform, address, observed_address, public_key, total_bytes, available_bytes, requested_at, expires_at FROM pending_joins WHERE expires_at > ? ORDER BY requested_at`, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []types.PendingJoin
	for rows.Next() {
		var pj types.PendingJoin
		var req, exp int64
		if err := rows.Scan(&pj.NodeID, &pj.Name, &pj.Platform, &pj.Address, &pj.ObservedAddress, &pj.PublicKey, &pj.TotalBytes, &pj.AvailableBytes, &req, &exp); err != nil {
			return nil, err
		}
		pj.RequestedAt = time.UnixMilli(req)
		pj.ExpiresAt = time.UnixMilli(exp)
		result = append(result, pj)
	}
	return result, rows.Err()
}

func (s *Store) GetPendingJoin(nodeID string) (*types.PendingJoin, error) {
	var pj types.PendingJoin
	var req, exp int64
	err := s.db.QueryRow(`SELECT node_id, name, platform, address, observed_address, public_key, total_bytes, available_bytes, requested_at, expires_at FROM pending_joins WHERE node_id = ? AND expires_at > ?`, nodeID, time.Now().UnixMilli()).Scan(&pj.NodeID, &pj.Name, &pj.Platform, &pj.Address, &pj.ObservedAddress, &pj.PublicKey, &pj.TotalBytes, &pj.AvailableBytes, &req, &exp)
	if err != nil {
		return nil, err
	}
	pj.RequestedAt = time.UnixMilli(req)
	pj.ExpiresAt = time.UnixMilli(exp)
	return &pj, nil
}

func (s *Store) DeletePendingJoin(nodeID string) error {
	_, err := s.db.Exec(`DELETE FROM pending_joins WHERE node_id = ?`, nodeID)
	return err
}

func (s *Store) CleanExpiredPendingJoins() {
	s.db.Exec(`DELETE FROM pending_joins WHERE expires_at <= ?`, time.Now().UnixMilli())
}

func (s *Store) IsChunkReferenced(chunkID string) (bool, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM file_chunks fc
		JOIN files f ON f.file_id = fc.file_id
		WHERE f.deleted = 0 AND fc.chunk_id = ?
	`, chunkID).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
