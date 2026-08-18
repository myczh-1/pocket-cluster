package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

const maxConflictPathAttempts = 1000

// applyRemoteFilePut records immutable file versions before rebuilding the
// materialized file view. This makes same-file concurrent WebDAV updates
// converge independently of event arrival order.
func (s *Server) applyRemoteFilePut(incoming *types.File) error {
	if err := s.store.RecordFileVersion(incoming); err != nil {
		return err
	}
	existing, err := s.store.GetFile(incoming.Path)
	if err == sql.ErrNoRows {
		existing, err = s.store.GetFileByID(incoming.FileID)
	}
	if err == sql.ErrNoRows {
		return s.store.UpsertFile(incoming)
	}
	if err != nil {
		return err
	}
	if existing.FileID != incoming.FileID {
		if err := s.prepareFilePut(incoming); err != nil {
			return err
		}
		return s.store.UpsertFile(incoming)
	}
	if err := s.store.EnsureFileVersion(existing); err != nil {
		return err
	}
	return s.rebuildConcurrentFileViews(existing.FileID, existing.Path)
}

func (s *Server) rebuildConcurrentFileViews(fileID, mainPath string) error {
	versions, err := s.store.ListFileVersions(fileID)
	if err != nil {
		return err
	}
	if len(versions) == 0 {
		return fmt.Errorf("file %s has no recorded versions", fileID)
	}
	byID := make(map[string]*types.File, len(versions))
	hasChild := make(map[string]bool, len(versions))
	for i := range versions {
		version := &versions[i]
		byID[version.VersionID] = version
		if version.ParentVersionID != "" {
			hasChild[version.ParentVersionID] = true
		}
	}
	var heads []*types.File
	for i := range versions {
		if !hasChild[versions[i].VersionID] {
			heads = append(heads, &versions[i])
		}
	}
	if len(heads) == 0 {
		return fmt.Errorf("file %s version graph has no head", fileID)
	}
	sort.Slice(heads, func(i, j int) bool {
		return compareVersionPaths(versionPath(heads[i].VersionID, byID), versionPath(heads[j].VersionID, byID)) < 0
	})

	winner := *heads[0]
	winner.FileID = fileID
	winner.Path = mainPath
	winner.Name = path.Base(mainPath)
	winner.ConflictOf = ""
	views := []*types.File{&winner}
	for _, loser := range heads[1:] {
		conflict := *loser
		conflict.FileID = concurrentConflictFileID(fileID, loser.VersionID)
		conflict.Path = concurrentConflictPath(mainPath, loser)
		conflict.Name = path.Base(conflict.Path)
		conflict.ConflictOf = fileID
		views = append(views, &conflict)
	}
	oldChunkIDs, err := s.store.ReplaceConcurrentFileViews(fileID, views)
	if err != nil {
		return err
	}
	s.cleanupUnreferencedChunks(context.Background(), oldChunkIDs)
	return nil
}

func versionPath(versionID string, versions map[string]*types.File) []string {
	var reversed []string
	seen := make(map[string]bool)
	for versionID != "" && !seen[versionID] {
		seen[versionID] = true
		reversed = append(reversed, versionID)
		version, ok := versions[versionID]
		if !ok {
			break
		}
		versionID = version.ParentVersionID
	}
	result := make([]string, len(reversed))
	for i := range reversed {
		result[len(reversed)-1-i] = reversed[i]
	}
	return result
}

func compareVersionPaths(a, b []string) int {
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

func concurrentConflictFileID(fileID, versionID string) string {
	return fileID + ".conflict." + versionID
}

func concurrentConflictPath(originalPath string, version *types.File) string {
	dir := path.Dir(originalPath)
	if dir == "." {
		dir = "/"
	}
	base := path.Base(originalPath)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	node := sanitizeConflictPart(version.ModifiedBy)
	if len(node) > 8 {
		node = node[:8]
	}
	versionPart := sanitizeConflictPart(version.VersionID)
	if len(versionPart) > 8 {
		versionPart = versionPart[:8]
	}
	name := fmt.Sprintf("%s.sync-conflict-%s-%s-%s%s", stem, node, version.ModifiedAt.UTC().Format("20060102-150405"), versionPart, ext)
	if dir == "/" {
		return "/" + name
	}
	return dir + "/" + name
}

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
		} else if !existing.Deleted && existing.VersionID != f.VersionID {
			overwrittenChunkIDs = append([]string(nil), existing.ChunkIDs...)
		}
	}
	body, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if _, err := s.store.UpsertFileWithEvent(f, s.cfg.NodeID, types.EventFilePut, body, time.Now()); err != nil {
		return err
	}
	if len(overwrittenChunkIDs) > 0 {
		s.cleanupUnreferencedChunks(context.Background(), overwrittenChunkIDs)
	}
	return nil
}

func (s *Server) cleanupUnreferencedChunks(ctx context.Context, chunkIDs []string) {
	s.cleanupChunks(ctx, chunkIDs, true)
}

func (s *Server) cleanupChunks(ctx context.Context, chunkIDs []string, preserveRecoverableVersions bool) {
	cutoff := time.Now().Add(-s.cfg.TombstoneRetentionDuration())
	for _, chunkID := range chunkIDs {
		select {
		case <-ctx.Done():
			return
		default:
		}
		ref, err := s.store.IsChunkReferenced(chunkID)
		if err != nil || ref {
			continue
		}
		if preserveRecoverableVersions {
			recoverable, err := s.store.IsChunkInRecoverableVersion(chunkID, cutoff)
			if err != nil || recoverable {
				continue
			}
		}
		removed, err := s.removeLocalReplica(chunkID, time.Now())
		if err != nil {
			log.Printf("cleanup chunk %s: %v", chunkID, err)
			continue
		}
		if removed {
			if _, err := s.appendEvent(types.EventChunkReplicaRemove, map[string]string{
				"chunk_id": chunkID,
				"node_id":  s.cfg.NodeID,
			}); err != nil {
				log.Printf("cleanup chunk %s: append replica remove event: %v", chunkID, err)
			}
		}
		if err := s.store.DeleteChunkIfUnreferenced(chunkID); err != nil {
			log.Printf("cleanup chunk %s: delete chunk metadata: %v", chunkID, err)
		}
	}
}

func (s *Server) removeLocalReplica(chunkID string, now time.Time) (bool, error) {
	replicas, err := s.store.GetReplicas(chunkID)
	if err != nil {
		return false, err
	}
	hadAvailableReplica := false
	for _, replica := range replicas {
		if replica.NodeID == s.cfg.NodeID && replica.Status == "available" {
			hadAvailableReplica = true
			break
		}
	}
	hadChunkFile := s.chunks.Exists(chunkID)
	if hadChunkFile {
		if err := s.chunks.Remove(chunkID); err != nil {
			return false, err
		}
	}
	if !hadAvailableReplica && !hadChunkFile {
		return false, nil
	}
	if err := s.store.MarkReplicaRemoved(chunkID, s.cfg.NodeID, now); err != nil {
		return false, err
	}
	return true, nil
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
