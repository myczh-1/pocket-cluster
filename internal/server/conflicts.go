package server

import (
	"database/sql"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func (s *Server) prepareFilePut(f *types.File) error {
	existing, err := s.store.GetFile(f.Path)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if existing.FileID == f.FileID || existing.VersionID == f.VersionID {
		return nil
	}
	originalPath := f.Path
	if f.ConflictOf == "" {
		f.ConflictOf = existing.FileID
	}
	f.Path = s.nextConflictPath(originalPath, f.ModifiedBy, f.ModifiedAt)
	f.Name = path.Base(f.Path)
	if f.ParentVersionID == "" {
		f.ParentVersionID = existing.VersionID
	}
	return nil
}

func (s *Server) nextConflictPath(originalPath, nodeID string, modifiedAt time.Time) string {
	if modifiedAt.IsZero() {
		modifiedAt = time.Now()
	}
	base := conflictPath(originalPath, nodeID, modifiedAt)
	if _, err := s.store.GetFile(base); err == sql.ErrNoRows {
		return base
	}
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := s.store.GetFile(candidate); err == sql.ErrNoRows {
			return candidate
		}
	}
}

func conflictPath(originalPath, nodeID string, modifiedAt time.Time) string {
	dir := path.Dir(originalPath)
	if dir == "." {
		dir = "/"
	}
	base := path.Base(originalPath)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	node := sanitizeConflictPart(nodeID)
	if len(node) > 8 {
		node = node[:8]
	}
	name := fmt.Sprintf("%s.sync-conflict-%s-%s%s", stem, node, modifiedAt.UTC().Format("20060102-150405"), ext)
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

func sanitizeConflictPart(value string) string {
	if value == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}
