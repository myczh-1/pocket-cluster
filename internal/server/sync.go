package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pocketcluster/agent/internal/peernet"
	"github.com/pocketcluster/agent/internal/types"
)

const (
	syncRequestTimeout = 15 * time.Second
	targetReplicaCount = 2
	nodeOfflineAfter   = 30 * time.Second
)

var peerHTTPClient = peernet.NewHTTPClient()

func (s *Server) StartSync(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.SyncOnce(ctx); err != nil {
				log.Printf("sync: %v", err)
			}
		}
	}
}

func (s *Server) SyncOnce(ctx context.Context) error {
	nodes, err := s.store.ListNodes()
	if err != nil {
		return err
	}
	var firstErr error
	for _, n := range nodes {
		if n.NodeID == s.cfg.NodeID || n.Address == "" || !n.Trusted {
			continue
		}
		pullErr := s.pullEvents(ctx, n)
		pushErr := s.pushEvents(ctx, n)
		if pullErr == nil || pushErr == nil {
			if err := s.markPeerOnline(n.NodeID, time.Now()); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if markErr := s.markPeerOfflineIfStale(n, time.Now()); markErr != nil && firstErr == nil {
			firstErr = markErr
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("pull events from %s: %w; push events: %v", n.NodeID, pullErr, pushErr)
		}
	}
	if err := s.fetchMissingChunks(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (s *Server) markPeerOnline(nodeID string, now time.Time) error {
	return s.store.UpdateNodeStatus(nodeID, "online", now)
}

func (s *Server) markPeerOfflineIfStale(n types.Node, now time.Time) error {
	if !isNodeStale(n, now) {
		return nil
	}
	return s.store.UpdateNodeStatus(n.NodeID, "offline", n.LastSeen)
}

func isNodeStale(n types.Node, now time.Time) bool {
	return n.LastSeen.IsZero() || now.Sub(n.LastSeen) >= nodeOfflineAfter
}

func (s *Server) pullEvents(ctx context.Context, n types.Node) error {
	ctx, cancel := context.WithTimeout(ctx, syncRequestTimeout)
	defer cancel()
	url := "http://" + n.Address + "/api/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if err := s.signPeerRequest(req, emptyBodySHA256); err != nil {
		return err
	}
	resp, err := peerHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("pull events from %s: %w", n.NodeID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pull events from %s: status %d", n.NodeID, resp.StatusCode)
	}
	var envelope types.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("pull events from %s: api error", n.NodeID)
	}
	var payload struct {
		Events []types.Event `json:"events"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		return err
	}
	for _, e := range payload.Events {
		inserted, err := s.store.InsertEvent(&e)
		if err != nil {
			return err
		}
		if inserted {
			if err := s.applyEvent(e); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) pushEvents(ctx context.Context, n types.Node) error {
	ctx, cancel := context.WithTimeout(ctx, syncRequestTimeout)
	defer cancel()
	events, err := s.store.GetEventsSince("", 1000)
	if err != nil {
		return err
	}
	if events == nil {
		events = []types.Event{}
	}
	body, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+n.Address+"/api/events/push", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := s.signPeerRequest(req, sha256Hex(body)); err != nil {
		return err
	}
	resp, err := peerHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("push events to %s: %w", n.NodeID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("push events to %s: status %d", n.NodeID, resp.StatusCode)
	}
	return nil
}

func (s *Server) fetchMissingChunks(ctx context.Context) error {
	files, err := s.store.ListAllFiles()
	if err != nil {
		return err
	}
	nodes, err := s.store.ListNodes()
	if err != nil {
		return err
	}
	seen := make(map[string]struct{})
	for _, f := range files {
		for _, chunkID := range f.ChunkIDs {
			if _, ok := seen[chunkID]; ok {
				continue
			}
			seen[chunkID] = struct{}{}
			if err := s.repairChunkReplicas(ctx, chunkID, nodes); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) repairChunkReplicas(ctx context.Context, chunkID string, nodes []types.Node) error {
	replicas, err := s.store.GetReplicas(chunkID)
	if err != nil {
		return err
	}
	online := onlineNodeSet(nodes, s.cfg.NodeID)
	available := availableOnlineReplicaNodes(replicas, online)
	if len(available) >= targetReplicaCount {
		return nil
	}
	existing := availableReplicaNodes(replicas)
	if s.chunks.Exists(chunkID) {
		_, err := s.pushChunkToPeer(ctx, chunkID, existing, nodes)
		return err
	}
	if err := s.fetchChunkFromReplica(ctx, chunkID); err != nil {
		return nil
	}
	return nil
}

func (s *Server) pushChunkToPeer(ctx context.Context, chunkID string, existing map[string]struct{}, nodes []types.Node) (bool, error) {
	for _, n := range nodes {
		if n.NodeID == s.cfg.NodeID || n.Address == "" || n.Status == "offline" || !n.Trusted {
			continue
		}
		if _, ok := existing[n.NodeID]; ok {
			continue
		}
		if err := s.storeChunkToPeer(ctx, n, chunkID); err != nil {
			continue
		}
		return true, nil
	}
	return false, nil
}

func (s *Server) fetchChunkFromReplica(ctx context.Context, chunkID string) error {
	replicas, err := s.store.GetReplicas(chunkID)
	if err != nil {
		return err
	}
	for _, replica := range replicas {
		if replica.NodeID == s.cfg.NodeID || replica.Status != "available" {
			continue
		}
		n, err := s.store.GetNode(replica.NodeID)
		if err != nil || n.Address == "" || n.Status == "offline" || !n.Trusted {
			continue
		}
		ctx, cancel := context.WithTimeout(ctx, syncRequestTimeout)
		hash, size, fetchErr := s.storeRemoteChunk(ctx, n.Address, chunkID)
		cancel()
		if fetchErr != nil {
			continue
		}
		if hash != chunkID {
			_ = s.chunks.Remove(hash)
			continue
		}
		now := time.Now()
		if err := s.store.UpsertChunk(&types.Chunk{ChunkID: chunkID, SizeBytes: size, StoredAt: now}); err != nil {
			return err
		}
		replica := &types.Replica{ChunkID: chunkID, NodeID: s.cfg.NodeID, Status: "available", StoredAt: now, VerifiedAt: now}
		if err := s.store.UpsertReplica(replica); err != nil {
			return err
		}
		_, err = s.appendEvent(types.EventChunkReplicaAdd, replica)
		return err
	}
	return fmt.Errorf("no available replica for chunk %s", chunkID)
}

func (s *Server) storeChunkToPeer(ctx context.Context, n types.Node, chunkID string) error {
	cf, size, err := s.chunks.Open(chunkID)
	if err != nil {
		return err
	}
	defer cf.Close()

	ctx, cancel := context.WithTimeout(ctx, syncRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+n.Address+"/api/chunks", cf)
	if err != nil {
		return err
	}
	if err := s.signPeerRequest(req, chunkID); err != nil {
		return err
	}
	req.Header.Set("X-Chunk-Hash", chunkID)
	req.ContentLength = size
	resp, err := peerHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("chunk store status %d", resp.StatusCode)
	}
	var envelope types.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("chunk store api error")
	}
	var payload struct {
		Replica *types.Replica `json:"replica"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		return err
	}
	if payload.Replica != nil {
		return s.store.UpsertReplica(payload.Replica)
	}
	now := time.Now()
	return s.store.UpsertReplica(&types.Replica{ChunkID: chunkID, NodeID: n.NodeID, Status: "available", StoredAt: now, VerifiedAt: now})
}

func (s *Server) storeRemoteChunk(ctx context.Context, address, chunkID string) (string, int64, error) {
	url := "http://" + address + "/api/chunks/" + chunkID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	if err := s.signPeerRequest(req, emptyBodySHA256); err != nil {
		return "", 0, err
	}
	resp, err := peerHTTPClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("chunk fetch status %d", resp.StatusCode)
	}
	return s.chunks.Store(resp.Body)
}

func (s *Server) writeChunk(ctx context.Context, w io.Writer, chunkID string) error {
	cf, _, err := s.chunks.Open(chunkID)
	if err == nil {
		defer cf.Close()
		_, err = io.Copy(w, cf)
		return err
	}
	replicas, err := s.store.GetReplicas(chunkID)
	if err != nil {
		return err
	}
	for _, replica := range replicas {
		if replica.NodeID == s.cfg.NodeID || replica.Status != "available" {
			continue
		}
		n, err := s.store.GetNode(replica.NodeID)
		if err != nil || n.Address == "" || n.Status == "offline" || !n.Trusted {
			continue
		}
		url := "http://" + n.Address + "/api/chunks/" + chunkID
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		if err := s.signPeerRequest(req, emptyBodySHA256); err != nil {
			continue
		}
		resp, err := peerHTTPClient.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}
		_, err = io.Copy(w, resp.Body)
		resp.Body.Close()
		return err
	}
	return fmt.Errorf("chunk unavailable: %s", strings.TrimSpace(chunkID))
}

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
