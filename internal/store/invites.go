package store

import (
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

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
