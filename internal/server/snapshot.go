package server

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/pocketcluster/agent/internal/types"
	"log"
	"net/http"
	"time"
)

const (
	snapshotEventThreshold = 1000
	snapshotInterval       = 24 * time.Hour
)

type metadataSnapshot struct {
	SnapshotID  string          `json:"snapshot_id"`
	CreatedAt   time.Time       `json:"created_at"`
	CreatedBy   string          `json:"created_by"`
	LastEventID string          `json:"last_event_id"`
	Cluster     snapshotCluster `json:"cluster"`
	Nodes       []types.Node    `json:"nodes"`
	Files       []types.File    `json:"files"`
	Chunks      []types.Chunk   `json:"chunks"`
	Replicas    []types.Replica `json:"replicas"`
}
type snapshotCluster struct {
	ClusterID string `json:"cluster_id"`
}

// StartSnapshotScheduler runs a background loop that creates snapshots
// when the event count exceeds snapshotEventThreshold or every snapshotInterval.
// After persisting a snapshot, it prunes events covered by the previous snapshot.
func (s *Server) StartSnapshotScheduler(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	var lastSnapshotAt time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := s.store.EventCount()
			if err != nil {
				log.Printf("snapshot scheduler: event count: %v", err)
				continue
			}
			elapsed := time.Since(lastSnapshotAt)
			if count < snapshotEventThreshold && elapsed < snapshotInterval {
				continue
			}
			if err := s.createAndPersistSnapshot(); err != nil {
				log.Printf("snapshot scheduler: create: %v", err)
				continue
			}
			lastSnapshotAt = time.Now()
			log.Printf("snapshot scheduler: created snapshot (events=%d, elapsed=%s)", count, elapsed.Round(time.Second))
		}
	}
}
func (s *Server) buildSnapshot() (*metadataSnapshot, error) {
	data, err := s.store.MetadataSnapshot()
	if err != nil {
		return nil, err
	}
	return &metadataSnapshot{
		SnapshotID:  uuid.NewString(),
		CreatedAt:   time.Now(),
		CreatedBy:   s.cfg.NodeID,
		LastEventID: data.LastEventID,
		Cluster:     snapshotCluster{ClusterID: s.cfg.ClusterID},
		Nodes:       data.Nodes,
		Files:       data.Files,
		Chunks:      data.Chunks,
		Replicas:    data.Replicas,
	}, nil
}

// createAndPersistSnapshot creates a new snapshot from the current metadata state,
// persists it, and prunes events that are now covered by the snapshot.
func (s *Server) createAndPersistSnapshot() error {
	snapshot, err := s.buildSnapshot()
	if err != nil {
		return err
	}
	jsonData, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	// Load previous snapshot to know what event_id we can prune up to
	prev, err := s.store.LoadLatestSnapshot()
	if err != nil {
		return err
	}
	// Save the new snapshot
	if err := s.store.SaveSnapshot(snapshot.SnapshotID, s.cfg.NodeID, snapshot.LastEventID, jsonData); err != nil {
		return err
	}
	// Prune events covered by the *previous* snapshot (safe: the new snapshot
	// captures everything up to snapshot.LastEventID, so pruning the previous
	// snapshot's boundary is safe — peers syncing incrementally will still get
	// events from prev.LastEventID+1 to snapshot.LastEventID).
	if prev != nil && prev.LastEventID != "" {
		pruned, err := s.store.PruneEventsBefore(prev.LastEventID)
		if err != nil {
			log.Printf("snapshot scheduler: prune events: %v", err)
		} else if pruned > 0 {
			log.Printf("snapshot scheduler: pruned %d old events (up to %s)", pruned, prev.LastEventID)
		}
	}
	// Clean up old snapshot records, keeping only the latest.
	s.store.PruneOldSnapshots()
	return nil
}
func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	// Try to serve persisted snapshot first
	ps, err := s.store.LoadLatestSnapshot()
	if err == nil && ps != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ps.Data))
		return
	}
	// Fall back to on-the-fly snapshot
	snapshot, err := s.buildSnapshot()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(snapshot)
}
