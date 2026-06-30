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
	liveChunkIDs := make(map[string]struct{})
	for _, f := range files {
		if f.Deleted || f.IsDir {
			continue
		}
		fileCount++
		logicalBytes += f.SizeBytes
		for _, chunkID := range f.ChunkIDs {
			liveChunkIDs[chunkID] = struct{}{}
		}
	}

	var activeUniqueChunkBytes int64
	var retainedUniqueChunkBytes int64
	activeUniqueChunkCount := 0
	retainedUniqueChunkCount := 0
	for _, c := range chunks {
		if _, ok := liveChunkIDs[c.ChunkID]; ok {
			activeUniqueChunkBytes += c.SizeBytes
			activeUniqueChunkCount++
			continue
		}
		retainedUniqueChunkBytes += c.SizeBytes
		retainedUniqueChunkCount++
	}
	replicas, err := s.store.ListReplicas()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}
	chunkSizes := make(map[string]int64, len(chunks))
	for _, c := range chunks {
		chunkSizes[c.ChunkID] = c.SizeBytes
	}
	var activePhysicalReplicaBytes int64
	activePhysicalReplicaCount := 0
	var retainedPhysicalReplicaBytes int64
	retainedPhysicalReplicaCount := 0
	for _, replica := range replicas {
		if replica.Status != "available" {
			continue
		}
		if _, ok := liveChunkIDs[replica.ChunkID]; ok {
			activePhysicalReplicaCount++
			activePhysicalReplicaBytes += chunkSizes[replica.ChunkID]
			continue
		}
		retainedPhysicalReplicaCount++
		retainedPhysicalReplicaBytes += chunkSizes[replica.ChunkID]
	}
	dedupSavedBytes := logicalBytes - activeUniqueChunkBytes
	if dedupSavedBytes < 0 {
		dedupSavedBytes = 0
	}
	dedupRatio := 0.0
	if logicalBytes > 0 {
		dedupRatio = float64(dedupSavedBytes) / float64(logicalBytes)
	}

	summary := s.HealthSummarySnapshot()
	chunkHealth := s.ChunkHealthSnapshot()
	nodes, err := s.store.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}
	affected := map[string]struct{}{}
	type fileRiskItem struct {
		FileID                string              `json:"file_id"`
		Path                  string              `json:"path"`
		ChunkCount            int                 `json:"chunk_count"`
		Status                types.ReplicaStatus `json:"status"`
		ReadableChunks        int                 `json:"readable_chunks"`
		UnavailableChunks     int                 `json:"unavailable_chunks"`
		UnderReplicatedChunks int                 `json:"under_replicated_chunks"`
	}
	fileRisks := make([]fileRiskItem, 0)
	nodeReplicaCounts := make(map[string]int)
	nodeRiskCounts := make(map[string]int)
	nodeRepairingCounts := make(map[string]int)
	activeHealthyChunks := 0
	activeUnderReplicatedChunks := 0
	activeUnavailableChunks := 0
	activeRepairingChunks := 0
	statusRank := func(status types.ReplicaStatus) int {
		switch status {
		case types.ReplicaUnavailable:
			return 3
		case types.ReplicaRepairing:
			return 2
		case types.ReplicaUnderReplicated:
			return 1
		default:
			return 0
		}
	}

	for _, replica := range replicas {
		if replica.Status != "available" {
			continue
		}
		if _, ok := liveChunkIDs[replica.ChunkID]; !ok {
			continue
		}
		nodeReplicaCounts[replica.NodeID]++
	}
	for _, detail := range chunkHealth {
		_, isLiveChunk := liveChunkIDs[detail.ChunkID]
		if isLiveChunk {
			switch detail.Status {
			case types.ReplicaHealthy:
				activeHealthyChunks++
			case types.ReplicaUnderReplicated:
				activeUnderReplicatedChunks++
			case types.ReplicaUnavailable:
				activeUnavailableChunks++
			case types.ReplicaRepairing:
				activeRepairingChunks++
			}
		}
		for _, replica := range detail.ReplicaNodes {
			if !replica.HasChunk {
				continue
			}
			if isLiveChunk && detail.Status != types.ReplicaHealthy && !replica.Online {
				nodeRiskCounts[replica.NodeID]++
			}
			if isLiveChunk && detail.Status == types.ReplicaRepairing {
				nodeRepairingCounts[replica.NodeID]++
			}
		}
		if !isLiveChunk || detail.Status == types.ReplicaHealthy {
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

	for _, f := range files {
		if f.Deleted || f.IsDir {
			continue
		}
		item := fileRiskItem{
			FileID:     f.FileID,
			Path:       f.Path,
			ChunkCount: len(f.ChunkIDs),
			Status:     types.ReplicaHealthy,
		}
		for _, chunkID := range f.ChunkIDs {
			detail, ok := chunkHealth[chunkID]
			if !ok {
				continue
			}
			if detail.Status != types.ReplicaUnavailable {
				item.ReadableChunks++
			}
			switch detail.Status {
			case types.ReplicaUnavailable:
				item.UnavailableChunks++
				item.Status = types.ReplicaUnavailable
			case types.ReplicaUnderReplicated:
				item.UnderReplicatedChunks++
				if item.Status == types.ReplicaHealthy {
					item.Status = types.ReplicaUnderReplicated
				}
			case types.ReplicaRepairing:
				item.UnderReplicatedChunks++
				if item.Status == types.ReplicaHealthy || item.Status == types.ReplicaUnderReplicated {
					item.Status = types.ReplicaRepairing
				}
			}
		}
		if item.Status != types.ReplicaHealthy {
			fileRisks = append(fileRisks, item)
		}
	}
	sort.Slice(fileRisks, func(i, j int) bool {
		left, right := fileRisks[i], fileRisks[j]
		if statusRank(left.Status) != statusRank(right.Status) {
			return statusRank(left.Status) > statusRank(right.Status)
		}
		if left.UnavailableChunks != right.UnavailableChunks {
			return left.UnavailableChunks > right.UnavailableChunks
		}
		if left.UnderReplicatedChunks != right.UnderReplicatedChunks {
			return left.UnderReplicatedChunks > right.UnderReplicatedChunks
		}
		return left.Path < right.Path
	})

	type nodeRiskItem struct {
		NodeID          string    `json:"node_id"`
		Name            string    `json:"name"`
		Platform        string    `json:"platform"`
		Status          string    `json:"status"`
		LastSeen        time.Time `json:"last_seen"`
		UsedBytes       int64     `json:"used_bytes"`
		TotalBytes      int64     `json:"total_bytes"`
		ReplicaCount    int       `json:"replica_count"`
		RiskChunkCount  int       `json:"risk_chunk_count"`
		RepairingChunks int       `json:"repairing_chunks"`
	}
	nodeRisks := make([]nodeRiskItem, 0, len(nodes))
	for _, n := range nodes {
		if !n.Trusted {
			continue
		}
		nodeRisks = append(nodeRisks, nodeRiskItem{
			NodeID:          n.NodeID,
			Name:            n.Name,
			Platform:        n.Platform,
			Status:          n.Status,
			LastSeen:        n.LastSeen,
			UsedBytes:       n.UsedBytes,
			TotalBytes:      n.TotalBytes,
			ReplicaCount:    nodeReplicaCounts[n.NodeID],
			RiskChunkCount:  nodeRiskCounts[n.NodeID],
			RepairingChunks: nodeRepairingCounts[n.NodeID],
		})
	}
	sort.Slice(nodeRisks, func(i, j int) bool {
		left, right := nodeRisks[i], nodeRisks[j]
		if left.RiskChunkCount != right.RiskChunkCount {
			return left.RiskChunkCount > right.RiskChunkCount
		}
		if left.ReplicaCount != right.ReplicaCount {
			return left.ReplicaCount > right.ReplicaCount
		}
		return left.NodeID < right.NodeID
	})

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
			"file_count":                      fileCount,
			"logical_bytes":                   logicalBytes,
			"unique_chunk_count":              activeUniqueChunkCount,
			"unique_chunk_bytes":              activeUniqueChunkBytes,
			"physical_replica_count":          activePhysicalReplicaCount,
			"physical_replica_bytes":          activePhysicalReplicaBytes,
			"retained_unique_chunk_count":     retainedUniqueChunkCount,
			"retained_unique_chunk_bytes":     retainedUniqueChunkBytes,
			"retained_physical_replica_count": retainedPhysicalReplicaCount,
			"retained_physical_replica_bytes": retainedPhysicalReplicaBytes,
			"dedup_saved_bytes":               dedupSavedBytes,
			"dedup_ratio":                     dedupRatio,
			"tombstone_retention_hours":       int(s.cfg.TombstoneRetentionDuration().Hours()),
		},
		"coverage": map[string]any{
			"total_chunks":            activeUniqueChunkCount,
			"healthy_chunks":          activeHealthyChunks,
			"under_replicated_chunks": activeUnderReplicatedChunks,
			"unavailable_chunks":      activeUnavailableChunks,
			"repairing_chunks":        activeRepairingChunks,
		},
		"risk": map[string]any{
			"affected_file_count": len(affectedFiles),
			"affected_files":      affectedFiles,
			"files":               fileRisks,
			"nodes":               nodeRisks,
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
