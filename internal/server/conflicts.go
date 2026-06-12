package server

import (
	"database/sql"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

const maxConflictPathAttempts = 1000

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
	conflictPath, err := s.nextConflictPath(originalPath, f.ModifiedBy, f.ModifiedAt)
	if err != nil {
		return err
	}
	f.Path = conflictPath
	f.Name = path.Base(f.Path)
	if f.ParentVersionID == "" {
		f.ParentVersionID = existing.VersionID
	}
	return nil
}

type filePutOptions struct {
	ConflictOnExisting bool
}

func (s *Server) commitFilePut(f *types.File, opts filePutOptions) error {
	var overwrittenChunkIDs []string
	if opts.ConflictOnExisting {
		if err := s.prepareFilePut(f); err != nil {
			return err
		}
	} else {
		existing, err := s.store.GetFile(f.Path)
		if err != nil {
			if err != sql.ErrNoRows {
				return err
			}
		} else if !existing.Deleted && existing.FileID != f.FileID && existing.VersionID != f.VersionID {
			overwrittenChunkIDs = append([]string(nil), existing.ChunkIDs...)
		}
	}
	if err := s.store.UpsertFile(f); err != nil {
		return err
	}
	if _, err := s.appendEvent(types.EventFilePut, f); err != nil {
		return err
	}
	if len(overwrittenChunkIDs) > 0 {
		s.cleanupUnreferencedChunks(overwrittenChunkIDs)
	}
	return nil
}

func (s *Server) cleanupUnreferencedChunks(chunkIDs []string) {
	for _, chunkID := range chunkIDs {
		ref, err := s.store.IsChunkReferenced(chunkID)
		if err != nil || ref {
			continue
		}
		_ = s.chunks.Remove(chunkID)
		_ = s.store.MarkReplicaRemoved(chunkID, s.cfg.NodeID, time.Now())
	}
}

func (s *Server) nextConflictPath(originalPath, nodeID string, modifiedAt time.Time) (string, error) {
	if modifiedAt.IsZero() {
		modifiedAt = time.Now()
	}
	base := conflictPath(originalPath, nodeID, modifiedAt)
	if _, err := s.store.GetFile(base); err != nil {
		if err == sql.ErrNoRows {
			return base, nil
		}
		return "", err
	}
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for i := 2; i <= maxConflictPathAttempts; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := s.store.GetFile(candidate); err != nil {
			if err == sql.ErrNoRows {
				return candidate, nil
			}
			return "", err
		}
	}
	return "", fmt.Errorf("too many conflict files for %s", originalPath)
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
