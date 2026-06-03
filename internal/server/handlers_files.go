package server

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/types"
)

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 30); err != nil {
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
	defer file.Close()

	var chunkIDs []string
	totalSize := int64(0)
	for {
		chunkBuf := make([]byte, chunk.ChunkSize)
		n, readErr := io.ReadFull(file, chunkBuf)
		if n > 0 {
			h := sha256.Sum256(chunkBuf[:n])
			hash := fmt.Sprintf("%x", h)
			if !s.chunks.Exists(hash) {
				chunkPath := s.chunks.Path(hash)
				if err := os.MkdirAll(filepath.Dir(chunkPath), 0o755); err != nil {
					writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
					return
				}
				if err := os.WriteFile(chunkPath, chunkBuf[:n], 0o644); err != nil {
					writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
					return
				}
				if err := s.store.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: int64(n), StoredAt: time.Now()}); err != nil {
					writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
					return
				}
			}
			now := time.Now()
			replica := &types.Replica{
				ChunkID:    hash,
				NodeID:     s.cfg.NodeID,
				Status:     "available",
				StoredAt:   now,
				VerifiedAt: now,
			}
			if err := s.store.UpsertReplica(replica); err != nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
				return
			}
			if _, err := s.appendEvent(types.EventChunkReplicaAdd, replica); err != nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
				return
			}
			chunkIDs = append(chunkIDs, hash)
			totalSize += int64(n)
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", readErr.Error())
			return
		}
	}

	mime := header.Header.Get("Content-Type")
	if mime == "" {
		mime = detectMime(targetPath)
	}
	now := time.Now()
	fileID := uuid.New().String()
	versionID := fmt.Sprintf("%x", sha256.Sum256([]byte(fileID+strings.Join(chunkIDs, ",")+s.cfg.NodeID+fmt.Sprint(now.UnixNano()))))
	f := &types.File{
		FileID:     fileID,
		Name:       filepath.Base(targetPath),
		Path:       targetPath,
		SizeBytes:  totalSize,
		MimeType:   mime,
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
	nodes, err := s.store.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	for _, chunkID := range chunkIDs {
		if err := s.repairChunkReplicas(r.Context(), chunkID, nodes); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	}
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
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", f.Name))
	for _, chunkID := range f.ChunkIDs {
		if err := s.writeChunk(r.Context(), w, chunkID); err != nil {
			writeError(w, http.StatusNotFound, "CHUNK_NOT_FOUND", chunkID)
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
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "cannot delete directory")
		return
	}
	if err := s.store.MarkFileDeleted(path, s.cfg.NodeID); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if _, err := s.appendEvent(types.EventFileDelete, map[string]string{"path": path, "deleted_by": s.cfg.NodeID}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]string{"path": path, "status": "deleted"})})
}
