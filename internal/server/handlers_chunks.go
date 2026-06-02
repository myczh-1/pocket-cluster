package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func (s *Server) handleGetChunk(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if hash == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "hash required")
		return
	}
	f, size, err := s.chunks.Open(hash)
	if err != nil {
		writeError(w, http.StatusNotFound, "CHUNK_NOT_FOUND", hash)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprint(size))
	io.Copy(w, f)
}

func (s *Server) handleStoreChunk(w http.ResponseWriter, r *http.Request) {
	expectedHash := r.Header.Get("X-Chunk-Hash")
	if expectedHash == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "X-Chunk-Hash header required")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	h := sha256.Sum256(body)
	actualHash := fmt.Sprintf("%x", h)
	if actualHash != expectedHash {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "hash mismatch")
		return
	}
	if !s.chunks.Exists(actualHash) {
		chunkPath := s.chunks.Path(actualHash)
		os.MkdirAll(filepath.Dir(chunkPath), 0o755)
		os.WriteFile(chunkPath, body, 0o644)
		s.store.UpsertChunk(&types.Chunk{ChunkID: actualHash, SizeBytes: int64(len(body)), StoredAt: time.Now()})
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"hash":       actualHash,
		"size_bytes": len(body),
		"stored":     true,
	})})
}

func (s *Server) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	events, err := s.store.GetEventsSince(since, 1000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"events":   events,
		"has_more": len(events) >= 1000,
	})})
}

func (s *Server) handlePushEvents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Events []types.Event `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	accepted := 0
	for _, e := range req.Events {
		inserted, err := s.store.InsertEvent(&e)
		if err != nil {
			continue
		}
		if inserted {
			if err := s.applyEvent(e); err != nil {
				continue
			}
			accepted++
		}
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"accepted":  accepted,
		"conflicts": []any{},
	})})
}
