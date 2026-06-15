package server

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pocketcluster/agent/internal/types"
)

type localEntry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsDir     bool   `json:"is_dir"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

func (s *Server) handleListLocalFiles(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		home, _ := os.UserHomeDir()
		if home == "" {
			home = "/"
		}
		path = home
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	result := make([]localEntry, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		result = append(result, localEntry{
			Name:      e.Name(),
			Path:      filepath.Join(abs, e.Name()),
			IsDir:     e.IsDir(),
			SizeBytes: info.Size(),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].IsDir != result[j].IsDir {
			return result[i].IsDir
		}
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})
	writeOK(w, http.StatusOK, map[string]any{
		"cwd":     abs,
		"parent":  filepath.Dir(abs),
		"entries": result,
	})
}

func (s *Server) handleMigrateLocalFile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path        string `json:"path"`
		TargetPath  string `json:"target_path"`
		DeleteLocal bool   `json:"delete_local"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if req.Path == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "path is required")
		return
	}
	if req.TargetPath == "" {
		req.TargetPath = "/" + filepath.Base(req.Path)
	}
	abs, err := filepath.Abs(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if info.IsDir() {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "cannot migrate directory, only files")
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	defer f.Close()

	staged, err := s.storeLocalChunks(f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			s.cleanupUnreferencedChunks(context.Background(), staged.stagedChunkIDs)
		}
	}()

	mimeType := "application/octet-stream"
	if detected := mime.TypeByExtension(filepath.Ext(abs)); detected != "" {
		mimeType = detected
	}
	now := time.Now()
	fileID := uuid.New().String()
	versionID := uuid.NewString()
	poolFile := &types.File{
		FileID:     fileID,
		Name:       filepath.Base(req.TargetPath),
		Path:       req.TargetPath,
		SizeBytes:  staged.totalSize,
		MimeType:   mimeType,
		VersionID:  versionID,
		ChunkIDs:   staged.chunkIDs,
		CreatedAt:  now,
		ModifiedAt: now,
		ModifiedBy: s.cfg.NodeID,
	}
	if err := s.commitFilePut(poolFile, filePutOptions{ConflictOnExisting: true}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	committed = true
	var repairErr error
	nodes, err := s.store.ListNodes()
	if err != nil {
		repairErr = err
	} else {
		for _, chunkID := range staged.chunkIDs {
			if err := s.repairChunkReplicas(r.Context(), chunkID, nodes); err != nil && repairErr == nil {
				repairErr = err
			}
		}
	}

	replicaStatus := s.replicaStatusForChunks(staged.chunkIDs)
	var deleteErr string
	deleteLocal := false
	if req.DeleteLocal {
		if repairErr != nil {
			writeError(w, http.StatusConflict, "REPLICATION_INCOMPLETE", "migration copied to pool but local file was kept: "+repairErr.Error())
			return
		}
		if replicaStatus != types.ReplicaHealthy {
			writeError(w, http.StatusConflict, "REPLICATION_INCOMPLETE", "migration copied to pool but local file was kept: replica status is "+string(replicaStatus))
			return
		}
		if err := os.Remove(abs); err != nil {
			deleteErr = err.Error()
		} else {
			deleteLocal = true
		}
	}
	writeOK(w, http.StatusOK, map[string]any{
		"file_id":        poolFile.FileID,
		"path":           poolFile.Path,
		"size_bytes":     poolFile.SizeBytes,
		"chunk_count":    len(staged.chunkIDs),
		"version_id":     poolFile.VersionID,
		"replica_status": string(replicaStatus),
		"delete_local":   deleteLocal,
		"delete_error":   deleteErr,
	})
}
