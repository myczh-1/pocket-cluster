package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pocketcluster/agent/internal/chunk"
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
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"cwd":     abs,
		"parent":  filepath.Dir(abs),
		"entries": result,
	})})
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

	var chunkIDs []string
	totalSize := int64(0)
	var first [1]byte
	for {
		n, readErr := f.Read(first[:])
		if n == 0 {
			break
		}
		if readErr != nil && n == 0 {
			break
		}
		hash, size, storeErr := s.chunks.Store(io.MultiReader(bytes.NewReader(first[:]), io.LimitReader(f, chunk.ChunkSize-1)))
		if storeErr != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", storeErr.Error())
			return
		}
		now := time.Now()
		if err := s.store.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: size, StoredAt: now}); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		replica := &types.Replica{ChunkID: hash, NodeID: s.cfg.NodeID, Status: "available", StoredAt: now, VerifiedAt: now}
		if err := s.store.UpsertReplica(replica); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if _, err := s.appendEvent(types.EventChunkReplicaAdd, replica); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		chunkIDs = append(chunkIDs, hash)
		totalSize += size
		if readErr != nil {
			break
		}
	}
	if totalSize == 0 {
		chunkIDs = nil
	}

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
		SizeBytes:  totalSize,
		MimeType:   mimeType,
		VersionID:  versionID,
		ChunkIDs:   chunkIDs,
		CreatedAt:  now,
		ModifiedAt: now,
		ModifiedBy: s.cfg.NodeID,
	}
	if err := s.prepareFilePut(poolFile); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if err := s.store.UpsertFile(poolFile); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if _, err := s.appendEvent(types.EventFilePut, poolFile); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	var repairErr error
	nodes, err := s.store.ListNodes()
	if err != nil {
		repairErr = err
	} else {
		for _, chunkID := range chunkIDs {
			if err := s.repairChunkReplicas(r.Context(), chunkID, nodes); err != nil && repairErr == nil {
				repairErr = err
			}
		}
	}

	replicaStatus := s.replicaStatusForChunks(chunkIDs)
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
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"file_id":        poolFile.FileID,
		"path":           poolFile.Path,
		"size_bytes":     poolFile.SizeBytes,
		"chunk_count":    len(chunkIDs),
		"version_id":     poolFile.VersionID,
		"replica_status": string(replicaStatus),
		"delete_local":   deleteLocal,
		"delete_error":   deleteErr,
	})})
}
