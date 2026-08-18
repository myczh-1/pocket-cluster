package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	"github.com/pocketcluster/agent/internal/types"
)

type NodeEvacuationStatus struct {
	NodeID                   string `json:"node_id"`
	State                    string `json:"state"`
	TotalChunks              int    `json:"total_chunks"`
	SafeChunks               int    `json:"safe_chunks"`
	PendingChunks            int    `json:"pending_chunks"`
	RequiredOtherReplicas    int    `json:"required_other_replicas"`
	EligibleDestinationNodes int    `json:"eligible_destination_nodes"`
	SafeToExit               bool   `json:"safe_to_exit"`
	Message                  string `json:"message"`
}

func (s *Server) nodeEvacuationStatus(nodeID string) (NodeEvacuationStatus, error) {
	node, err := s.store.GetNode(nodeID)
	if err != nil || !node.Trusted {
		return NodeEvacuationStatus{}, fmt.Errorf("trusted node not found")
	}
	chunkIDs, err := s.store.GetNodeChunkIDs(nodeID)
	if err != nil {
		return NodeEvacuationStatus{}, err
	}
	sort.Strings(chunkIDs)
	health := s.ChunkHealthSnapshot()
	nodes, err := s.store.ListNodes()
	if err != nil {
		return NodeEvacuationStatus{}, err
	}
	eligible := 0
	for _, candidate := range nodes {
		if candidate.NodeID != nodeID && candidate.Trusted && candidate.Status == "online" {
			eligible++
		}
	}
	if nodeID != s.cfg.NodeID {
		foundLocal := false
		for _, candidate := range nodes {
			if candidate.NodeID == s.cfg.NodeID {
				foundLocal = true
				break
			}
		}
		if !foundLocal {
			eligible++
		}
	}

	status := NodeEvacuationStatus{
		NodeID:                   nodeID,
		TotalChunks:              len(chunkIDs),
		RequiredOtherReplicas:    targetReplicaCount,
		EligibleDestinationNodes: eligible,
	}
	for _, chunkID := range chunkIDs {
		detail, ok := health[chunkID]
		if !ok {
			continue
		}
		verifiedOthers := 0
		for _, replica := range detail.ReplicaNodes {
			if replica.NodeID != nodeID && replica.Status == "available" && replica.Online && replica.HasChunk {
				verifiedOthers++
			}
		}
		if verifiedOthers >= targetReplicaCount {
			status.SafeChunks++
		}
	}
	status.PendingChunks = status.TotalChunks - status.SafeChunks
	status.SafeToExit = status.PendingChunks == 0
	switch {
	case status.SafeToExit && status.TotalChunks == 0:
		status.State = "ready"
		status.Message = "This node holds no pool chunks and can be stopped safely."
	case status.SafeToExit:
		status.State = "ready"
		status.Message = "Every chunk on this node has two verified copies on other online nodes."
	case eligible < targetReplicaCount:
		status.State = "blocked"
		status.Message = fmt.Sprintf("Keep this node online and add or reconnect %d more destination node(s).", targetReplicaCount-eligible)
	default:
		status.State = "needs_migration"
		status.Message = "Some chunks still need verified copies on other nodes."
	}
	return status, nil
}

func (s *Server) handleListNodeEvacuation(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "STORE_ERROR", err.Error())
		return
	}
	statuses := make([]NodeEvacuationStatus, 0, len(nodes))
	for _, node := range nodes {
		if !node.Trusted {
			continue
		}
		status, err := s.nodeEvacuationStatus(node.NodeID)
		if err == nil {
			statuses = append(statuses, status)
		}
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].NodeID < statuses[j].NodeID })
	writeOK(w, http.StatusOK, map[string]any{"nodes": statuses})
}

func (s *Server) handleEvacuateNode(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeId")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "nodeId required")
		return
	}
	if _, err := s.nodeEvacuationStatus(nodeID); err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		return
	}
	if !s.beginNodeEvacuation(nodeID) {
		writeError(w, http.StatusConflict, "ALREADY_RUNNING", "node evacuation is already running")
		return
	}
	job := s.startJob(types.JobNodeEvacuation, "Preparing node for safe exit", "Copying this node's chunks to verified destinations.", func(ctx context.Context, jobID string) (types.JobStatus, string, error) {
		defer s.endNodeEvacuation(nodeID)
		status, err := s.evacuateNode(ctx, nodeID, jobID)
		if err != nil {
			return types.JobFailed, "Node evacuation stopped because a copy operation failed.", err
		}
		if !status.SafeToExit {
			return types.JobBlocked, status.Message, nil
		}
		return types.JobDone, status.Message, nil
	})
	writeOK(w, http.StatusAccepted, job)
}

func (s *Server) beginNodeEvacuation(nodeID string) bool {
	s.evacuationMu.Lock()
	defer s.evacuationMu.Unlock()
	if s.evacuatingNodes[nodeID] {
		return false
	}
	s.evacuatingNodes[nodeID] = true
	return true
}

func (s *Server) endNodeEvacuation(nodeID string) {
	s.evacuationMu.Lock()
	delete(s.evacuatingNodes, nodeID)
	s.evacuationMu.Unlock()
}

func (s *Server) evacuateNode(ctx context.Context, nodeID, jobID string) (NodeEvacuationStatus, error) {
	s.runHealthScan(ctx)
	chunkIDs, err := s.store.GetNodeChunkIDs(nodeID)
	if err != nil {
		return NodeEvacuationStatus{}, err
	}
	sort.Strings(chunkIDs)
	nodes, err := s.store.ListNodes()
	if err != nil {
		return NodeEvacuationStatus{}, err
	}
	for _, chunkID := range chunkIDs {
		if err := ctx.Err(); err != nil {
			return NodeEvacuationStatus{}, err
		}
		taskID := "job:" + jobID + ":evacuate:" + chunkID
		s.trackSyncTask(taskID, types.SyncTaskReplicaRepair, types.SyncTaskRunning, "Copying chunk away from node", chunkID, "Creating verified replacement replicas before the node exits.", "")
		existing := s.verifiedReplicaNodesExcluding(chunkID, nodeID)
		if len(existing) >= targetReplicaCount {
			s.finishSyncTask(taskID, types.SyncTaskReplicaRepair, "Copying chunk away from node", chunkID, "Replacement replicas are already verified.")
			continue
		}
		if !s.chunks.Exists(chunkID) {
			if err := s.fetchChunkFromReplica(ctx, chunkID); err != nil {
				s.failSyncTask(taskID, types.SyncTaskReplicaRepair, types.SyncTaskBlocked, "Copying chunk away from node", chunkID, "No reachable source could provide this chunk.", err.Error())
				continue
			}
		}
		if s.cfg.NodeID != nodeID && s.chunks.Exists(chunkID) {
			existing[s.cfg.NodeID] = struct{}{}
		}
		candidates := make([]types.Node, 0, len(nodes))
		for _, node := range nodes {
			if node.NodeID != nodeID {
				candidates = append(candidates, node)
			}
		}
		for len(existing) < targetReplicaCount {
			destinationNodeID, pushErr := s.pushChunkToPeer(ctx, chunkID, existing, candidates)
			if pushErr != nil {
				s.failSyncTask(taskID, types.SyncTaskReplicaRepair, repairFailureStatus(pushErr), "Copying chunk away from node", chunkID, "A replacement replica could not be created.", pushErr.Error())
				break
			}
			if destinationNodeID == "" {
				s.failSyncTask(taskID, types.SyncTaskReplicaRepair, types.SyncTaskBlocked, "Copying chunk away from node", chunkID, "Not enough online destination nodes are available.", "")
				break
			}
			existing[destinationNodeID] = struct{}{}
		}
		if len(existing) >= targetReplicaCount {
			s.finishSyncTask(taskID, types.SyncTaskReplicaRepair, "Copying chunk away from node", chunkID, "Replacement replicas were created successfully.")
		}
	}
	s.runHealthScan(ctx)
	return s.nodeEvacuationStatus(nodeID)
}

func (s *Server) verifiedReplicaNodesExcluding(chunkID, excludedNodeID string) map[string]struct{} {
	verified := make(map[string]struct{})
	detail, ok := s.ChunkHealthSnapshot()[chunkID]
	if !ok {
		return verified
	}
	for _, replica := range detail.ReplicaNodes {
		if replica.NodeID != excludedNodeID && replica.Status == "available" && replica.Online && replica.HasChunk {
			verified[replica.NodeID] = struct{}{}
		}
	}
	return verified
}
