package server

import (
	"fmt"
	"net/http"
	"github.com/pocketcluster/agent/internal/types"
)

// GET /api/health/summary
func (s *Server) handleHealthSummary(w http.ResponseWriter, r *http.Request) {
	if s.health == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", "health scanner not started")
		return
	}
	summary := s.HealthSummarySnapshot()
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(summary)})
}

// GET /api/health/chunks
// GET /api/health/chunks?limit=100&offset=0
func (s *Server) handleHealthChunks(w http.ResponseWriter, r *http.Request) {
	if s.health == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", "health scanner not started")
		return
	}
	chunks := s.ChunkHealthSnapshot()
	list := make([]ChunkHealthDetail, 0, len(chunks))
	for _, v := range chunks {
		list = append(list, v)
	}
	// Pagination
	limit := 100
	offset := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		fmt.Sscanf(v, "%d", &limit)
		if limit > 500 {
			limit = 500
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		fmt.Sscanf(v, "%d", &offset)
	}
	total := len(list)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"chunks": list[offset:end],
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})})
}

// GET /api/health/chunks/{hash}
func (s *Server) handleHealthChunkDetail(w http.ResponseWriter, r *http.Request) {
	if s.health == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", "health scanner not started")
		return
	}
	hash := r.PathValue("hash")
	if hash == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "chunk hash required")
		return
	}
	s.health.mu.RLock()
	detail, ok := s.health.chunkHealth[hash]
	s.health.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "chunk not found in health scan")
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(detail)})
}

// GET /api/health/files/{fileId}
func (s *Server) handleHealthFileDetail(w http.ResponseWriter, r *http.Request) {
	if s.health == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", "health scanner not started")
		return
	}
	fileID := r.PathValue("fileId")
	if fileID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "file ID required")
		return
	}
	detail, err := s.FileHealth(fileID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(detail)})
}
