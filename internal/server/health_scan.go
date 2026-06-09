package server

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"
	"github.com/pocketcluster/agent/internal/types"
)

const (
	healthScanInterval = 60 * time.Second
	tombstoneRetention = 7 * 24 * time.Hour // 7 days
)

// HealthSummary describes the overall replica health of the pool.
type HealthSummary struct {
	TotalFiles      int                `json:"total_files"`
	TotalChunks     int                `json:"total_chunks"`
	HealthyChunks   int                `json:"healthy_chunks"`
	UnderReplicated int                `json:"under_replicated_chunks"`
	Unavailable     int                `json:"unavailable_chunks"`
	RepairingChunks int                `json:"repairing_chunks"`
	OverallStatus   types.ReplicaStatus `json:"overall_status"`
	LastScanAt      time.Time          `json:"last_scan_at"`
	LastRepairAt    time.Time          `json:"last_repair_at"`
}

// ChunkHealthDetail describes a single chunk's replica health.
type ChunkHealthDetail struct {
	ChunkID          string             `json:"chunk_id"`
	SizeBytes        int64              `json:"size_bytes"`
	ReplicaCount     int                `json:"replica_count"`
	OnlineReplicas   int                `json:"online_replicas"`
	TargetReplicas   int                `json:"target_replicas"`
	Status           types.ReplicaStatus `json:"status"`
	ReplicaNodes     []ReplicaNodeInfo  `json:"replica_nodes"`
	ReferencingFiles []string           `json:"referencing_files,omitempty"`
}

// ReplicaNodeInfo describes a replica on a specific node.
type ReplicaNodeInfo struct {
	NodeID   string `json:"node_id"`
	Status   string `json:"status"`
	Online   bool   `json:"online"`
	HasChunk bool   `json:"has_chunk"`
}

// FileHealthDetail describes a file's replica health with per-chunk breakdown.
type FileHealthDetail struct {
	FileID     string              `json:"file_id"`
	Path       string              `json:"path"`
	Name       string              `json:"name"`
	SizeBytes  int64               `json:"size_bytes"`
	ChunkCount int                 `json:"chunk_count"`
	Status     types.ReplicaStatus `json:"status"`
	Chunks     []ChunkHealthDetail `json:"chunks"`
}

type healthScanner struct {
	mu               sync.RWMutex
	summary          HealthSummary
	chunkHealth      map[string]ChunkHealthDetail
	repairing        map[string]bool // chunkIDs currently being repaired
	underReplicated  []string        // chunkIDs needing repair, set by scan
	skipRemoteVerify bool            // skip HEAD verification in tests
}
func newHealthScanner() *healthScanner {
	return &healthScanner{
		chunkHealth: make(map[string]ChunkHealthDetail),
		repairing:   make(map[string]bool),
	}
}
// StartHealthScan runs the periodic health scan loop.
func (s *Server) StartHealthScan(ctx context.Context) {
	s.health = newHealthScanner()
	ticker := time.NewTicker(healthScanInterval)
	defer ticker.Stop()
	// Run once immediately on startup
	s.runHealthScan(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runHealthScan(ctx)
		}
	}
}

func (s *Server) runHealthScan(ctx context.Context) {
	nodes, err := s.store.ListNodes()
	if err != nil {
		log.Printf("health scan: list nodes: %v", err)
		return
	}
	onlineSet := onlineNodeSet(nodes, s.cfg.NodeID)

	files, err := s.store.ListAllFiles()
	if err != nil {
		log.Printf("health scan: list files: %v", err)
		return
	}

	// Build chunk -> referencing files map
	chunkFiles := make(map[string][]string)
	fileCount := 0
	for _, f := range files {
		if f.Deleted || f.IsDir {
			continue
		}
		fileCount++
		for _, chunkID := range f.ChunkIDs {
			chunkFiles[chunkID] = append(chunkFiles[chunkID], f.Path)
		}
	}

	chunks, err := s.store.ListChunks()
	if err != nil {
		log.Printf("health scan: list chunks: %v", err)
		return
	}

	summary := HealthSummary{
		TotalFiles:  fileCount,
		TotalChunks: len(chunks),
		LastScanAt:  time.Now(),
	}
	chunkHealthMap := make(map[string]ChunkHealthDetail, len(chunks))
	var underReplicated []string

	for _, c := range chunks {
		replicas, err := s.store.GetReplicas(c.ChunkID)
		if err != nil {
			continue
		}
		totalReplicas := 0
		onlineReplicaCount := 0
		var nodeInfos []ReplicaNodeInfo
		for _, r := range replicas {
			if r.Status != "available" {
				continue
			}
			totalReplicas++
			isOnline := false
			if _, ok := onlineSet[r.NodeID]; ok {
				isOnline = true
			}
			hasChunk := false
			if r.NodeID == s.cfg.NodeID {
				hasChunk = s.chunks.Exists(c.ChunkID)
			} else if isOnline && !s.health.skipRemoteVerify {
				// Verify remote chunk existence via HEAD request
				n, _ := s.store.GetNode(r.NodeID)
				if n != nil && len(nodeDialAddresses(*n)) > 0 {
					hasChunk = s.verifyRemoteChunkExists(nodeDialAddresses(*n)[0], c.ChunkID)
				}
			} else if isOnline {
				// Test mode: assume remote node has chunk
				hasChunk = true
			}
			if hasChunk && isOnline {
				onlineReplicaCount++
			}
			nodeInfos = append(nodeInfos, ReplicaNodeInfo{
				NodeID:   r.NodeID,
				Status:   r.Status,
				Online:   isOnline,
				HasChunk: hasChunk,
			})
		}

		status := types.ReplicaHealthy
		if onlineReplicaCount == 0 {
			status = types.ReplicaUnavailable
			summary.Unavailable++
		} else if onlineReplicaCount < targetReplicaCount {
			status = types.ReplicaUnderReplicated
			summary.UnderReplicated++
			underReplicated = append(underReplicated, c.ChunkID)
		} else {
			summary.HealthyChunks++
		}

		s.health.mu.RLock()
		isRepairing := s.health.repairing[c.ChunkID]
		s.health.mu.RUnlock()
		if isRepairing {
			status = "repairing"
			summary.RepairingChunks++
		}

		detail := ChunkHealthDetail{
			ChunkID:          c.ChunkID,
			SizeBytes:        c.SizeBytes,
			ReplicaCount:     totalReplicas,
			OnlineReplicas:   onlineReplicaCount,
			TargetReplicas:   targetReplicaCount,
			Status:           status,
			ReplicaNodes:     nodeInfos,
			ReferencingFiles: chunkFiles[c.ChunkID],
		}
		chunkHealthMap[c.ChunkID] = detail
	}

	// Determine overall status
	summary.OverallStatus = types.ReplicaHealthy
	if summary.Unavailable > 0 {
		summary.OverallStatus = types.ReplicaUnavailable
	} else if summary.UnderReplicated > 0 {
		summary.OverallStatus = types.ReplicaUnderReplicated
	}

	s.health.mu.Lock()
	s.health.summary = summary
	s.health.chunkHealth = chunkHealthMap
	s.health.underReplicated = underReplicated
	s.health.mu.Unlock()
	if len(underReplicated) > 0 {
		log.Printf("health scan: %d under-replicated chunks detected (sync loop will repair)", len(underReplicated))
	}
}

// CleanupTombstones removes files that have been deleted longer than tombstoneRetention.
func (s *Server) CleanupTombstones() error {
	deleted, err := s.store.ListAllFilesIncludingDeleted()
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-tombstoneRetention)
	for _, f := range deleted {
		if !f.Deleted {
			continue
		}
		if f.ModifiedAt.After(cutoff) {
			continue
		}
		if err := s.store.PurgeFile(f.FileID); err != nil {
			log.Printf("tombstone cleanup: purge %s: %v", f.Path, err)
			continue
		}
		// Clean up unreferenced chunks
		for _, chunkID := range f.ChunkIDs {
			ref, _ := s.store.IsChunkReferenced(chunkID)
			if !ref {
				s.chunks.Remove(chunkID)
				s.store.MarkReplicaRemoved(chunkID, s.cfg.NodeID, time.Now())
			}
		}
	}
	return nil
}
// DrainUnderReplicated returns and clears the under-replicated chunk list from the last scan.
func (s *Server) DrainUnderReplicated() []string {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()
	chunks := s.health.underReplicated
	s.health.underReplicated = nil
	return chunks
}
// MarkRepairing sets the repairing state for a chunk (for UI display).
func (s *Server) MarkRepairing(chunkID string, repairing bool) {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()
	if repairing {
		s.health.repairing[chunkID] = true
	} else {
		delete(s.health.repairing, chunkID)
	}
}
// SetLastRepairAt updates the last repair timestamp.
func (s *Server) SetLastRepairAt(t time.Time) {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()
	s.health.summary.LastRepairAt = t
}

// HealthSummarySnapshot returns the current health summary (thread-safe).
func (s *Server) HealthSummarySnapshot() HealthSummary {
	s.health.mu.RLock()
	defer s.health.mu.RUnlock()
	return s.health.summary
}

// ChunkHealthSnapshot returns the current per-chunk health details (thread-safe).
func (s *Server) ChunkHealthSnapshot() map[string]ChunkHealthDetail {
	s.health.mu.RLock()
	defer s.health.mu.RUnlock()
	cp := make(map[string]ChunkHealthDetail, len(s.health.chunkHealth))
	for k, v := range s.health.chunkHealth {
		cp[k] = v
	}
	return cp
}

// FileHealth returns the health detail for a specific file.
func (s *Server) FileHealth(fileID string) (*FileHealthDetail, error) {
	f, err := s.store.GetFileByID(fileID)
	if err != nil {
		return nil, err
	}
	chunkDetails := s.ChunkHealthSnapshot()
	var chunks []ChunkHealthDetail
	fileStatus := types.ReplicaHealthy
	for _, chunkID := range f.ChunkIDs {
		if detail, ok := chunkDetails[chunkID]; ok {
			chunks = append(chunks, detail)
			if detail.Status == types.ReplicaUnavailable {
				fileStatus = types.ReplicaUnavailable
			} else if detail.Status == types.ReplicaUnderReplicated && fileStatus != types.ReplicaUnavailable {
				fileStatus = types.ReplicaUnderReplicated
			}
		}
	}
	return &FileHealthDetail{
		FileID:     f.FileID,
		Path:       f.Path,
		Name:       f.Name,
		SizeBytes:  f.SizeBytes,
		ChunkCount: len(f.ChunkIDs),
		Status:     fileStatus,
		Chunks:     chunks,
	}, nil
}
// verifyRemoteChunkExists checks if a remote node actually has a chunk via HEAD request.
func (s *Server) verifyRemoteChunkExists(nodeAddress, chunkID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := "http://" + nodeAddress + "/api/chunks/" + chunkID
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return false
	}
	if err := s.signPeerRequest(req, emptyBodySHA256); err != nil {
		return false
	}
	resp, err := peerHTTPClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
