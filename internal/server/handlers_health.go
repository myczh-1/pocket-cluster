package server

import (
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

// GET /api/health/summary
func (s *Server) handleHealthSummary(w http.ResponseWriter, r *http.Request) {
	if s.health == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", "health scanner not started")
		return
	}
	summary := s.HealthSummarySnapshot()
	writeOK(w, http.StatusOK, summary)
}

// GET /api/health/insights
func (s *Server) handleHealthInsights(w http.ResponseWriter, r *http.Request) {
	if s.health == nil {
		writeError(w, http.StatusServiceUnavailable, "NOT_READY", "health scanner not started")
		return
	}

	files, err := s.store.ListAllFiles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}
	chunks, err := s.store.ListChunks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}

	var logicalBytes int64
	fileCount := 0
	for _, f := range files {
		if f.Deleted || f.IsDir {
			continue
		}
		fileCount++
		logicalBytes += f.SizeBytes
	}

	var uniqueChunkBytes int64
	for _, c := range chunks {
		uniqueChunkBytes += c.SizeBytes
	}
	dedupSavedBytes := logicalBytes - uniqueChunkBytes
	if dedupSavedBytes < 0 {
		dedupSavedBytes = 0
	}
	dedupRatio := 0.0
	if logicalBytes > 0 {
		dedupRatio = float64(dedupSavedBytes) / float64(logicalBytes)
	}

	summary := s.HealthSummarySnapshot()
	chunkHealth := s.ChunkHealthSnapshot()
	affected := map[string]struct{}{}
	for _, detail := range chunkHealth {
		if detail.Status == types.ReplicaHealthy {
			continue
		}
		for _, p := range detail.ReferencingFiles {
			affected[p] = struct{}{}
		}
	}
	affectedFiles := make([]string, 0, len(affected))
	for p := range affected {
		affectedFiles = append(affectedFiles, p)
	}
	sort.Strings(affectedFiles)

	s.health.mu.RLock()
	queuedChunks := append([]string(nil), s.health.underReplicated...)
	repairingChunks := make([]string, 0, len(s.health.repairing))
	for chunkID := range s.health.repairing {
		repairingChunks = append(repairingChunks, chunkID)
	}
	s.health.mu.RUnlock()
	sort.Strings(queuedChunks)
	sort.Strings(repairingChunks)

	repairStatus := "idle"
	repairMessage := "Replica coverage is currently stable."
	nextRetrySeconds := 0
	switch {
	case len(repairingChunks) > 0:
		repairStatus = "repairing"
		repairMessage = "The sync loop is copying chunks to restore the target replica count."
		nextRetrySeconds = 2
	case len(queuedChunks) > 0 || summary.UnderReplicated > 0:
		repairStatus = "queued"
		repairMessage = "Under-replicated chunks are queued for the next sync repair pass."
		nextRetrySeconds = 2
	case summary.Unavailable > 0:
		repairStatus = "blocked"
		repairMessage = "Some chunks have no online replica; repair is blocked until a replica node returns."
	}

	writeOK(w, http.StatusOK, map[string]any{
		"storage": map[string]any{
			"file_count":         fileCount,
			"logical_bytes":      logicalBytes,
			"unique_chunk_bytes": uniqueChunkBytes,
			"dedup_saved_bytes":  dedupSavedBytes,
			"dedup_ratio":        dedupRatio,
		},
		"risk": map[string]any{
			"affected_file_count": len(affectedFiles),
			"affected_files":      affectedFiles,
		},
		"repair": map[string]any{
			"status":              repairStatus,
			"message":             repairMessage,
			"queued_chunks":       len(queuedChunks),
			"repairing_chunks":    len(repairingChunks),
			"repairing_chunk_ids": repairingChunks,
			"last_scan_at":        summary.LastScanAt,
			"last_repair_at":      summary.LastRepairAt,
			"next_retry_seconds":  nextRetrySeconds,
		},
		"generated_at": time.Now(),
	})
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
	writeOK(w, http.StatusOK, map[string]any{
		"chunks": list[offset:end],
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
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
	writeOK(w, http.StatusOK, detail)
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
	writeOK(w, http.StatusOK, detail)
}
