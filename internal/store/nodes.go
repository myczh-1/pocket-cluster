package store

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

// Node operations

func (s *Store) UpsertNode(n *types.Node) error {
	return s.upsertNode(n, false)
}

func (s *Store) UpdateNodeFull(n *types.Node) error {
	return s.upsertNode(n, true)
}

func (s *Store) upsertNode(n *types.Node, forceUpdate bool) error {
	candidates, err := json.Marshal(n.AddressCandidates)
	if err != nil {
		return err
	}
	// Build conflict-update SET clause.
	// forceUpdate=false: preserve existing non-empty values (merge semantics).
	// forceUpdate=true:  overwrite all fields unconditionally (full replace).
	setClauses := []string{
		"name", "platform", "address", "address_candidates",
		"last_working_address", "public_key",
		"total_bytes", "available_bytes",
		"status", "trusted", "last_seen",
	}
	var setParts []string
	for _, col := range setClauses {
		if forceUpdate {
			setParts = append(setParts, col+" = excluded."+col)
		} else {
			switch col {
			case "name", "platform", "address", "last_working_address",
				"public_key", "status":
				setParts = append(setParts, col+" = CASE WHEN excluded."+col+" != '' THEN excluded."+col+" ELSE nodes."+col+" END")
			case "address_candidates":
				setParts = append(setParts, col+" = CASE WHEN excluded."+col+" != '[]' THEN excluded."+col+" ELSE nodes."+col+" END")
			case "total_bytes", "available_bytes", "trusted", "last_seen":
				setParts = append(setParts, col+" = CASE WHEN excluded."+col+" != 0 THEN excluded."+col+" ELSE nodes."+col+" END")
			}
		}
	}
	setParts = append(setParts, "used_bytes = excluded.used_bytes")
	setParts = append(setParts, "joined_at = CASE WHEN excluded.joined_at != 0 THEN excluded.joined_at ELSE nodes.joined_at END")
	query := `INSERT INTO nodes
		(node_id, name, platform, address, address_candidates, last_working_address, public_key, total_bytes, used_bytes, available_bytes, status, trusted, last_seen, joined_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(node_id) DO UPDATE SET ` + strings.Join(setParts, ", ")
	_, err = s.db.Exec(query,
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
