package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/pocketcluster/agent/internal/types"
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

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	files, err := s.store.ListAllFilesIncludingDeleted()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	chunks, err := s.store.ListChunks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	replicas, err := s.store.ListReplicas()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	lastEventID, err := s.store.LatestEventID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	snapshot := metadataSnapshot{
		SnapshotID:  uuid.NewString(),
		CreatedAt:   time.Now(),
		CreatedBy:   s.cfg.NodeID,
		LastEventID: lastEventID,
		Cluster:     snapshotCluster{ClusterID: s.cfg.ClusterID},
		Nodes:       nodes,
		Files:       files,
		Chunks:      chunks,
		Replicas:    replicas,
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(snapshot)
}
