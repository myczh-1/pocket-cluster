package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/types"
)

const (
	maxUploadBytes       = 16 * 1024 * 1024 * 1024
	maxUploadMemoryBytes = 8 * 1024 * 1024
)

type localChunkStaging struct {
	chunkIDs       []string
	stagedChunkIDs []string
	totalSize      int64
}

func (s *Server) storeLocalChunks(r io.Reader) (localChunkStaging, error) {
	var staged localChunkStaging
	var first [1]byte
	for {
		n, readErr := r.Read(first[:])
		if n == 0 {
			if readErr != nil && readErr != io.EOF {
				return staged, readErr
			}
			break
		}
		hash, size, err := s.chunks.Store(io.MultiReader(bytes.NewReader(first[:n]), io.LimitReader(r, chunk.ChunkSize-int64(n))))
		if err != nil {
			return staged, err
		}
		staged.stagedChunkIDs = append(staged.stagedChunkIDs, hash)
		if _, _, err := s.recordLocalChunkReplica(hash, size, time.Now()); err != nil {
			return staged, err
		}
		staged.chunkIDs = append(staged.chunkIDs, hash)
		staged.totalSize += size
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return staged, readErr
		}
	}
	return staged, nil
}

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
	targetPath, err := normalizePoolFilePath(targetPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	uploadID := uuid.New().String()
	s.uploadProgress.add(&uploadStatus{ID: uploadID, FileName: header.Filename, Status: "uploading"})
	taskID := "upload:" + uploadID
	s.trackSyncTask(taskID, types.SyncTaskUpload, types.SyncTaskRunning, "Uploading file", targetPath, "Writing file into the local pool node.", "")
	defer s.uploadProgress.delete(uploadID)
	defer file.Close()

	staged, err := s.storeLocalChunks(file)
	if err != nil {
		s.failSyncTask(taskID, types.SyncTaskUpload, types.SyncTaskFailed, "Uploading file", targetPath, "Upload failed before metadata commit.", err.Error())
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			s.cleanupUnreferencedChunks(context.Background(), staged.stagedChunkIDs)
		}
	}()

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
		SizeBytes:  staged.totalSize,
		MimeType:   mimeType,
		VersionID:  versionID,
		ChunkIDs:   staged.chunkIDs,
		CreatedAt:  now,
		ModifiedAt: now,
		ModifiedBy: s.cfg.NodeID,
	}
	if err := s.commitFilePut(f, filePutOptions{ConflictOnExisting: true}); err != nil {
		s.failSyncTask(taskID, types.SyncTaskUpload, types.SyncTaskFailed, "Uploading file", targetPath, "Upload failed while committing file metadata.", err.Error())
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	committed = true
	repairChunkIDs := append([]string(nil), staged.chunkIDs...)
	go s.repairChunksAsync(repairChunkIDs)
	s.uploadProgress.set(uploadID, "done", "", staged.totalSize)
	s.finishSyncTask(taskID, types.SyncTaskUpload, "Uploading file", targetPath, "File content committed; replica repair continues in the background.")
	replicaStatus := s.replicaStatusForChunks(staged.chunkIDs)
	writeOK(w, http.StatusOK, map[string]any{
		"file_id":        f.FileID,
		"path":           f.Path,
		"size_bytes":     f.SizeBytes,
		"chunk_count":    len(staged.chunkIDs),
		"version_id":     f.VersionID,
		"replica_status": string(replicaStatus),
		"conflict_of":    f.ConflictOf,
	})
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
		taskID := "repair:" + chunkID
		s.trackSyncTask(taskID, types.SyncTaskReplicaRepair, types.SyncTaskRunning, "Repairing replica", chunkID, "Trying to reach the target replica count for this chunk.", "")
		if err := s.repairChunkReplicas(context.Background(), chunkID, nodes); err != nil {
			s.failSyncTask(taskID, types.SyncTaskReplicaRepair, repairFailureStatus(err), "Repairing replica", chunkID, "Replica repair did not complete in this pass.", err.Error())
			log.Printf("upload repair: chunk %s: %v", chunkID, err)
			continue
		}
		s.finishSyncTask(taskID, types.SyncTaskReplicaRepair, "Repairing replica", chunkID, "Replica target satisfied for this chunk.")
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
	w.Header().Set("Content-Length", strconv.FormatInt(f.SizeBytes, 10))
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
	writeOK(w, http.StatusOK, map[string]string{"path": path, "status": "deleted"})
	go s.runHealthScan(context.Background())
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
	newPath, err := normalizePoolFilePath(req.NewPath)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PATH", err.Error())
		return
	}
	f, err := s.store.GetFile(req.Path)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "file not found")
		return
	}
	now := time.Now()
	if err := s.store.RenameFile(f.FileID, req.Path, newPath, s.cfg.NodeID, now); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if f.IsDir {
		if err := s.store.RenameChildren(req.Path, newPath, s.cfg.NodeID, now); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	}
	if _, err := s.appendEvent(types.EventFileRename, map[string]string{
		"file_id":  f.FileID,
		"old_path": req.Path,
		"new_path": newPath,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeOK(w, http.StatusOK, map[string]string{
		"file_id":  f.FileID,
		"old_path": req.Path,
		"new_path": newPath,
	})
}

// normalizePoolFilePath validates and canonicalizes a pool file path.
func normalizePoolFilePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	if !strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("path %q must be absolute", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" {
			continue
		}
		if seg == "." || seg == ".." {
			return "", fmt.Errorf("path %q cannot contain relative components", p)
		}
		if strings.HasPrefix(seg, ".") {
			return "", fmt.Errorf("name %q is not allowed: hidden files and relative paths are forbidden", seg)
		}
	}
	cleaned := path.Clean(p)
	if cleaned == "." || cleaned == "/" {
		return "", fmt.Errorf("path %q must point to a file or directory name under the pool root", p)
	}
	return cleaned, nil
}

func validateRenamePath(p string) error {
	_, err := normalizePoolFilePath(p)
	return err
}
