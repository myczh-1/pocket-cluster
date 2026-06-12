package server

import "github.com/pocketcluster/agent/internal/types"

func (s *Server) replicaStatusForChunks(chunkIDs []string) types.ReplicaStatus {
	if len(chunkIDs) == 0 {
		return types.ReplicaHealthy
	}
	nodes, err := s.store.ListNodes()
	if err != nil {
		return types.ReplicaUnavailable
	}
	online := onlineNodeSet(nodes, s.cfg.NodeID)
	status := types.ReplicaHealthy
	for _, chunkID := range chunkIDs {
		replicas, err := s.store.GetReplicas(chunkID)
		if err != nil {
			return types.ReplicaUnavailable
		}
		count := len(availableOnlineReplicaNodes(replicas, online))
		if count == 0 {
			return types.ReplicaUnavailable
		}
		if count < targetReplicaCount {
			status = types.ReplicaUnderReplicated
		}
	}
	return status
}

func onlineNodeSet(nodes []types.Node, selfNodeID string) map[string]struct{} {
	online := map[string]struct{}{selfNodeID: {}}
	for _, n := range nodes {
		if n.NodeID == selfNodeID {
			continue
		}
		if n.Status == "online" && n.Trusted {
			online[n.NodeID] = struct{}{}
		}
	}
	return online
}

func availableOnlineReplicaNodes(replicas []types.Replica, online map[string]struct{}) map[string]struct{} {
	available := make(map[string]struct{}, len(replicas))
	for _, replica := range replicas {
		if replica.Status != "available" {
			continue
		}
		if _, ok := online[replica.NodeID]; ok {
			available[replica.NodeID] = struct{}{}
		}
	}
	return available
}

func availableReplicaNodes(replicas []types.Replica) map[string]struct{} {
	available := make(map[string]struct{}, len(replicas))
	for _, replica := range replicas {
		if replica.Status == "available" {
			available[replica.NodeID] = struct{}{}
		}
	}
	return available
}
