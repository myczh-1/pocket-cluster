package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/types"
)

const (
	maxUploadBytes       = 16 * 1024 * 1024 * 1024
	maxUploadMemoryBytes = 8 * 1024 * 1024
)

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadMemoryBytes); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	targetPath := r.FormValue("path")
	if targetPath == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "path is required")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	uploadID := uuid.New().String()
	s.uploadProgress.add(&uploadStatus{ID: uploadID, FileName: header.Filename, Status: "uploading"})
	defer s.uploadProgress.delete(uploadID)
	defer file.Close()

	var chunkIDs []string
	totalSize := int64(0)
	for {
		hash, size, err := s.chunks.StoreSized(file, chunk.ChunkSize)
		if err == io.EOF || (err == nil && size == 0) {
			break
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if _, _, err := s.recordLocalChunkReplica(hash, size, time.Now()); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		chunkIDs = append(chunkIDs, hash)
		totalSize += size
	}

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		if detected := mime.TypeByExtension(filepath.Ext(targetPath)); detected != "" {
			mimeType = detected
		} else {
			mimeType = "application/octet-stream"
		}
	}
	now := time.Now()
	fileID := uuid.New().String()
	versionID := uuid.NewString()
	f := &types.File{
		FileID:     fileID,
		Name:       filepath.Base(targetPath),
		Path:       targetPath,
		SizeBytes:  totalSize,
		MimeType:   mimeType,
		VersionID:  versionID,
		ChunkIDs:   chunkIDs,
		CreatedAt:  now,
		ModifiedAt: now,
		ModifiedBy: s.cfg.NodeID,
	}
	if err := s.prepareFilePut(f); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if err := s.store.UpsertFile(f); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if _, err := s.appendEvent(types.EventFilePut, f); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	repairChunkIDs := append([]string(nil), chunkIDs...)
	go s.repairChunksAsync(repairChunkIDs)
	s.uploadProgress.set(uploadID, "done", "", totalSize)
	replicaStatus := s.replicaStatusForChunks(chunkIDs)
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"file_id":        f.FileID,
		"path":           f.Path,
		"size_bytes":     f.SizeBytes,
		"chunk_count":    len(chunkIDs),
		"version_id":     f.VersionID,
		"replica_status": string(replicaStatus),
		"conflict_of":    f.ConflictOf,
	})})
	// Trigger immediate health scan so the UI reflects the new file.
	go s.runHealthScan(context.Background())
}
func (s *Server) repairChunksAsync(chunkIDs []string) {
	nodes, err := s.store.ListNodes()
	if err != nil {
		log.Printf("upload repair: list nodes: %v", err)
		return
	}
	for _, chunkID := range chunkIDs {
		if err := s.repairChunkReplicas(context.Background(), chunkID, nodes); err != nil {
			log.Printf("upload repair: chunk %s: %v", chunkID, err)
		}
	}
	s.runHealthScan(context.Background())
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	id := r.URL.Query().Get("id")
	var f *types.File
	var err error
	if path != "" {
		f, err = s.store.GetFile(path)
	} else if id != "" {
		f, err = s.store.GetFileByID(id)
	} else {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "path or id required")
		return
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
		return
	}
	if f.IsDir {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "cannot download directory")
		return
	}
	for _, chunkID := range f.ChunkIDs {
		if !s.isChunkReadable(r.Context(), chunkID) {
			writeError(w, http.StatusNotFound, "CHUNK_NOT_FOUND", chunkID)
			return
		}
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.Name))
	for _, chunkID := range f.ChunkIDs {
		if err := s.writeChunk(r.Context(), w, chunkID); err != nil {
			log.Printf("download %s: chunk %s unavailable after precheck: %v", f.Path, chunkID, err)
			return
		}
	}
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "path is required")
		return
	}
	f, err := s.store.GetFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
		return
	}
	if f.IsDir {
		if err := s.store.MarkChildrenDeleted(path, s.cfg.NodeID); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if _, err := s.appendEvent(types.EventDirDelete, map[string]string{"path": path, "deleted_by": s.cfg.NodeID}); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	} else {
		if err := s.store.MarkFileDeleted(path, s.cfg.NodeID); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if _, err := s.appendEvent(types.EventFileDelete, map[string]string{"path": path, "deleted_by": s.cfg.NodeID}); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]string{"path": path, "status": "deleted"})})
}

// PATCH /api/files/rename — rename or move a file
func (s *Server) handleRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path    string `json:"path"`
		NewPath string `json:"new_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if req.Path == "" || req.NewPath == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "path and new_path are required")
		return
	}
	f, err := s.store.GetFile(req.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
		return
	}
	now := time.Now()
	if err := s.store.RenameFile(f.FileID, req.Path, req.NewPath, s.cfg.NodeID, now); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if f.IsDir {
		if err := s.store.RenameChildren(req.Path, req.NewPath, s.cfg.NodeID, now); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	}
	if _, err := s.appendEvent(types.EventFileRename, map[string]string{
		"file_id":  f.FileID,
		"old_path": req.Path,
		"new_path": req.NewPath,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]string{
		"file_id":  f.FileID,
		"old_path": req.Path,
		"new_path": req.NewPath,
	})})
}
