package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
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
	tombstoneTicker := time.NewTicker(1 * time.Hour)
	defer tombstoneTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.SyncOnce(ctx); err != nil {
				log.Printf("sync: %v", err)
			}
		case <-tombstoneTicker.C:
			if err := s.CleanupTombstones(); err != nil {
				log.Printf("tombstone cleanup: %v", err)
			}
		}
	}
}

func (s *Server) SyncOnce(ctx context.Context) error {
	if _, err := s.store.MarkStaleNodesOffline(time.Now().Add(-nodeOfflineAfter)); err != nil {
		log.Printf("mark stale nodes: %v", err)
	}
	nodes, err := s.store.ListNodes()
	if err != nil {
		return err
	}
	var firstErr error
	for _, n := range nodes {
		if n.NodeID == s.cfg.NodeID || n.Address == "" || !n.Trusted {
			continue
		}
		pullAddress, pullErr := s.pullEvents(ctx, n)
		pushAddress, pushErr := s.pushEvents(ctx, n)
		if pullErr == nil || pushErr == nil {
			workingAddress := pullAddress
			if workingAddress == "" {
				workingAddress = pushAddress
			}
			if err := s.markPeerOnline(n.NodeID, workingAddress, time.Now()); err != nil && firstErr == nil {
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

func (s *Server) markPeerOnline(nodeID, address string, now time.Time) error {
	if address != "" {
		return s.store.UpdateNodeLastWorkingAddress(nodeID, address, now)
	}
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

func (s *Server) pullEvents(ctx context.Context, n types.Node) (string, error) {
	var lastErr error
	for _, address := range nodeDialAddresses(n) {
		if err := s.pullEventsFrom(ctx, n, address); err != nil {
			lastErr = err
			continue
		}
		return address, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no dial address")
	}
	return "", lastErr
}

func (s *Server) pullEventsFrom(ctx context.Context, n types.Node, address string) error {
	ctx, cancel := context.WithTimeout(ctx, syncRequestTimeout)
	defer cancel()
	url := "http://" + address + "/api/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if err := s.signPeerRequest(req, emptyBodySHA256); err != nil {
		return err
	}
	resp, err := peerHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("pull events from %s at %s: %w", n.NodeID, address, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("pull events from %s at %s: status %d", n.NodeID, address, resp.StatusCode)
	}
	var envelope types.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("pull events from %s at %s: api error", n.NodeID, address)
	}
	var payload struct {
		Events []types.Event `json:"events"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		return err
	}
	for _, e := range payload.Events {
		e = rewritePushedNodeAddress(e, n.NodeID, address)
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

func (s *Server) pushEvents(ctx context.Context, n types.Node) (string, error) {
	events, err := s.store.GetUnpushedEvents(n.NodeID, 1000)
	if err != nil {
		return "", err
	}
	if events == nil {
		events = []types.Event{}
	}
	body, err := json.Marshal(map[string]any{"events": events})
	if err != nil {
		return "", err
	}
	var lastErr error
	for _, address := range nodeDialAddresses(n) {
		if err := s.pushEventsTo(ctx, n, address, body, len(events)); err != nil {
			lastErr = err
			continue
		}
		if err := s.store.MarkEventsPushed(n.NodeID, events, time.Now()); err != nil {
			return "", err
		}
		return address, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no dial address")
	}
	return "", lastErr
}

func (s *Server) pushEventsTo(ctx context.Context, n types.Node, address string, body []byte, expected int) error {
	ctx, cancel := context.WithTimeout(ctx, syncRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+address+"/api/events/push", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := s.signPeerRequest(req, sha256Hex(body)); err != nil {
		return err
	}
	resp, err := peerHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("push events to %s at %s: %w", n.NodeID, address, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("push events to %s at %s: status %d", n.NodeID, address, resp.StatusCode)
	}
	var envelope types.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("push events to %s at %s: api error", n.NodeID, address)
	}
	var payload struct {
		Accepted int `json:"accepted"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		return err
	}
	if payload.Accepted != expected {
		return fmt.Errorf("push events to %s at %s: accepted %d of %d", n.NodeID, address, payload.Accepted, expected)
	}
	return nil
}
func (s *Server) fetchMissingChunks(ctx context.Context) error {
	// Get chunks referenced by files
	files, err := s.store.ListAllFiles()
	if err != nil {
		return err
	}
	nodes, err := s.store.ListNodes()
	if err != nil {
		return err
	}
	// Get chunks this node is responsible for
	myChunks, err := s.store.GetNodeChunkIDs(s.cfg.NodeID)
	if err != nil {
		return err
	}
	myChunkSet := make(map[string]struct{}, len(myChunks))
	for _, id := range myChunks {
		myChunkSet[id] = struct{}{}
	}
	seen := make(map[string]struct{})
	for _, f := range files {
		for _, chunkID := range f.ChunkIDs {
			if _, ok := seen[chunkID]; ok {
				continue
			}
			seen[chunkID] = struct{}{}
			// Only fetch if we have the chunk locally, have a replica record, or it's referenced by a file
			if s.chunks.Exists(chunkID) {
				// We have it, just repair replicas
				if err := s.repairChunkReplicas(ctx, chunkID, nodes); err != nil {
					return err
				}
				continue
			}
			if _, assigned := myChunkSet[chunkID]; assigned {
				// We're assigned this chunk but don't have it locally
				if err := s.repairChunkReplicas(ctx, chunkID, nodes); err != nil {
					return err
				}
			}
			// If we're not assigned and don't have it, skip (sharding mode)
			// But if there are no other replicas available, we should still try to get it
			replicas, _ := s.store.GetReplicas(chunkID)
			hasOnlineReplica := false
			for _, r := range replicas {
				if r.NodeID != s.cfg.NodeID && r.Status == "available" {
					n, _ := s.store.GetNode(r.NodeID)
					if n != nil && n.Status == "online" && n.Trusted {
						hasOnlineReplica = true
						break
					}
				}
			}
			if !hasOnlineReplica {
				// No other node has this chunk, we should try to get it
				if err := s.repairChunkReplicas(ctx, chunkID, nodes); err != nil {
					return err
				}
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
	// Count available replicas, but only count local if we actually have the chunk
	availableCount := 0
	for _, r := range replicas {
		if r.Status != "available" {
			continue
		}
		if _, isOnline := online[r.NodeID]; !isOnline {
			continue
		}
		if r.NodeID == s.cfg.NodeID {
			if s.chunks.Exists(chunkID) {
				availableCount++
			}
		} else {
			availableCount++
		}
	}
	if availableCount >= targetReplicaCount {
		return nil
	}
	existing := availableReplicaNodes(replicas)
	if s.chunks.Exists(chunkID) {
		_, err := s.pushChunkToPeer(ctx, chunkID, existing, nodes)
		return err
	}
	// We don't have the chunk locally, fetch it
	if err := s.fetchChunkFromReplica(ctx, chunkID); err != nil {
		return err
	}
	return nil
}

func (s *Server) pushChunkToPeer(ctx context.Context, chunkID string, existing map[string]struct{}, nodes []types.Node) (bool, error) {
	// Filter and sort candidates
	type candidate struct {
		node           types.Node
		availableBytes int64
		isDesktop      bool
	}
	var candidates []candidate
	for _, n := range nodes {
		if n.NodeID == s.cfg.NodeID || n.Status == "offline" || !n.Trusted || len(nodeDialAddresses(n)) == 0 {
			continue
		}
		if _, ok := existing[n.NodeID]; ok {
			continue
		}
		isDesktop := n.Platform == "darwin" || n.Platform == "linux" || n.Platform == "windows"
		candidates = append(candidates, candidate{node: n, availableBytes: n.AvailableBytes, isDesktop: isDesktop})
	}
	// Sort: desktop first, then by available space descending
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].isDesktop != candidates[j].isDesktop {
			return candidates[i].isDesktop
		}
		return candidates[i].availableBytes > candidates[j].availableBytes
	})
	for _, c := range candidates {
		if err := s.storeChunkToPeer(ctx, c.node, chunkID); err != nil {
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
		if err != nil || n.Status == "offline" || !n.Trusted || len(nodeDialAddresses(*n)) == 0 {
			continue
		}
		var hash string
		var size int64
		var fetchErr error
		var workingAddress string
		for _, address := range nodeDialAddresses(*n) {
			ctx, cancel := context.WithTimeout(ctx, syncRequestTimeout)
			hash, size, fetchErr = s.storeRemoteChunk(ctx, address, chunkID)
			cancel()
			if fetchErr == nil {
				workingAddress = address
				break
			}
		}
		if fetchErr != nil {
			continue
		}
		if hash != chunkID {
			_ = s.chunks.Remove(hash)
			continue
		}
		now := time.Now()
		_ = s.store.UpdateNodeLastWorkingAddress(replica.NodeID, workingAddress, now)
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
	var lastErr error
	for _, address := range nodeDialAddresses(n) {
		cf, size, err := s.chunks.Open(chunkID)
		if err != nil {
			return err
		}
		reqCtx, cancel := context.WithTimeout(ctx, syncRequestTimeout)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, "http://"+address+"/api/chunks", cf)
		if err != nil {
			cf.Close()
			cancel()
			return err
		}
		if err := s.signPeerRequest(req, chunkID); err != nil {
			cf.Close()
			cancel()
			return err
		}
		req.Header.Set("X-Chunk-Hash", chunkID)
		req.ContentLength = size
		resp, err := peerHTTPClient.Do(req)
		cf.Close()
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("chunk store status %d", resp.StatusCode)
			resp.Body.Close()
			continue
		}
		var envelope types.APIResponse
		if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
			resp.Body.Close()
			return err
		}
		resp.Body.Close()
		if !envelope.OK {
			lastErr = fmt.Errorf("chunk store api error")
			continue
		}
		var payload struct {
			Replica *types.Replica `json:"replica"`
		}
		if err := json.Unmarshal(envelope.Data, &payload); err != nil {
			return err
		}
		now := time.Now()
		_ = s.store.UpdateNodeLastWorkingAddress(n.NodeID, address, now)
		if payload.Replica != nil {
			return s.store.UpsertReplica(payload.Replica)
		}
		return s.store.UpsertReplica(&types.Replica{ChunkID: chunkID, NodeID: n.NodeID, Status: "available", StoredAt: now, VerifiedAt: now})
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no dial address")
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

func (s *Server) isChunkReadable(ctx context.Context, chunkID string) bool {
	cf, _, err := s.chunks.Open(chunkID)
	if err == nil {
		cf.Close()
		return true
	}
	replicas, err := s.store.GetReplicas(chunkID)
	if err != nil {
		return false
	}
	for _, replica := range replicas {
		if replica.NodeID == s.cfg.NodeID || replica.Status != "available" {
			continue
		}
		n, err := s.store.GetNode(replica.NodeID)
		if err != nil || n.Status == "offline" || !n.Trusted || len(nodeDialAddresses(*n)) == 0 {
			continue
		}
		for _, address := range nodeDialAddresses(*n) {
			reqCtx, cancel := context.WithTimeout(ctx, syncRequestTimeout)
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+address+"/api/chunks/"+chunkID, nil)
			if err != nil {
				cancel()
				continue
			}
			if err := s.signPeerRequest(req, emptyBodySHA256); err != nil {
				cancel()
				continue
			}
			resp, err := peerHTTPClient.Do(req)
			cancel()
			if err != nil {
				continue
			}
			if resp.StatusCode == http.StatusOK {
				resp.Body.Close()
				_ = s.store.UpdateNodeLastWorkingAddress(replica.NodeID, address, time.Now())
				return true
			}
			resp.Body.Close()
		}
	}
	return false
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
		if err != nil || n.Status == "offline" || !n.Trusted || len(nodeDialAddresses(*n)) == 0 {
			continue
		}
		for _, address := range nodeDialAddresses(*n) {
			url := "http://" + address + "/api/chunks/" + chunkID
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
			if err == nil {
				_ = s.store.UpdateNodeLastWorkingAddress(replica.NodeID, address, time.Now())
			}
			return err
		}
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
