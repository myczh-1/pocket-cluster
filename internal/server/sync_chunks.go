package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func (s *Server) fetchMissingChunks(ctx context.Context) error {
	files, err := s.store.ListAllFiles()
	if err != nil {
		return err
	}
	nodes, err := s.store.ListNodes()
	if err != nil {
		return err
	}
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
			if err := s.ensureChunkAvailable(ctx, chunkID, myChunkSet, nodes); err != nil {
				return err
			}
		}
	}
	return nil
}

// ensureChunkAvailable ensures a single chunk has enough replicas.
// It fetches or repairs as needed.
func (s *Server) ensureChunkAvailable(ctx context.Context, chunkID string, myChunkSet map[string]struct{}, nodes []types.Node) error {
	if s.chunks.Exists(chunkID) {
		return s.repairChunkReplicas(ctx, chunkID, nodes)
	}
	if _, assigned := myChunkSet[chunkID]; assigned {
		return s.repairChunkReplicas(ctx, chunkID, nodes)
	}
	// Not assigned and not local — check if any online replica exists
	replicas, _ := s.store.GetReplicas(chunkID)
	for _, r := range replicas {
		if r.NodeID != s.cfg.NodeID && r.Status == "available" {
			if n, _ := s.store.GetNode(r.NodeID); n != nil && n.Status == "online" && n.Trusted {
				return nil // another node has it
			}
		}
	}
	// No other node has this chunk, try to get it
	return s.repairChunkReplicas(ctx, chunkID, nodes)
}

func (s *Server) repairChunkReplicas(ctx context.Context, chunkID string, nodes []types.Node) error {
	replicas, err := s.store.GetReplicas(chunkID)
	if err != nil {
		return err
	}
	online := onlineNodeSet(nodes, s.cfg.NodeID)
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
	return s.fetchChunkFromReplica(ctx, chunkID)
}

func (s *Server) pushChunkToPeer(ctx context.Context, chunkID string, existing map[string]struct{}, nodes []types.Node) (bool, error) {
	type candidate struct {
		node           types.Node
		availableBytes int64
		isDesktop      bool
	}
	var candidates []candidate
	chunk, err := s.store.GetChunk(chunkID)
	if err != nil {
		return false, fmt.Errorf("get chunk %s: %w", chunkID, err)
	}
	for _, n := range nodes {
		if n.NodeID == s.cfg.NodeID || n.Status == "offline" || !n.Trusted || len(nodeDialAddresses(n)) == 0 {
			continue
		}
		if _, ok := existing[n.NodeID]; ok {
			continue
		}
		if n.AvailableBytes > 0 && n.AvailableBytes-chunk.SizeBytes < minFreeSpace {
			continue
		}
		isDesktop := n.Platform == "darwin" || n.Platform == "linux" || n.Platform == "windows"
		candidates = append(candidates, candidate{node: n, availableBytes: n.AvailableBytes, isDesktop: isDesktop})
	}
	if len(candidates) == 0 {
		return false, nil
	}
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
	return false, fmt.Errorf("push chunk %s: all %d candidates rejected", chunkID, len(candidates))
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
			reqCtx, cancel := context.WithTimeout(ctx, syncRequestTimeout)
			hash, size, fetchErr = s.storeRemoteChunk(reqCtx, address, chunkID)
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
		_, _, err = s.recordLocalChunkReplica(chunkID, size, now)
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
		resp, err := s.peerHTTPClient.Do(req)
		cf.Close()
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		payload, err := decodeChunkStoreResponse(resp)
		if err != nil {
			lastErr = err
			continue
		}
		now := time.Now()
		_ = s.store.UpdateNodeLastWorkingAddress(n.NodeID, address, now)
		if payload != nil {
			return s.store.UpsertReplica(payload)
		}
		return s.store.UpsertReplica(&types.Replica{ChunkID: chunkID, NodeID: n.NodeID, Status: "available", StoredAt: now, VerifiedAt: now})
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("no dial address")
}

func decodeChunkStoreResponse(resp *http.Response) (*types.Replica, error) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("chunk store status %d", resp.StatusCode)
	}
	var envelope types.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if !envelope.OK {
		return nil, fmt.Errorf("chunk store api error")
	}
	var payload struct {
		Replica *types.Replica `json:"replica"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		return nil, err
	}
	return payload.Replica, nil
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
	resp, err := s.peerHTTPClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("chunk fetch status %d", resp.StatusCode)
	}
	return s.chunks.Store(resp.Body)
}

// forEachAvailableReplica iterates over available replicas of a chunk,
// calling fn for each reachable peer. Stops when fn returns nil.
// Returns the error from the last attempted fn call, or nil if fn succeeded.
func (s *Server) forEachAvailableReplica(ctx context.Context, chunkID string, fn func(ctx context.Context, node types.Node, address string) error) error {
	replicas, err := s.store.GetReplicas(chunkID)
	if err != nil {
		return err
	}
	var lastErr error
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
			lastErr = fn(reqCtx, *n, address)
			cancel()
			if lastErr == nil {
				_ = s.store.UpdateNodeLastWorkingAddress(replica.NodeID, address, time.Now())
				return nil
			}
		}
	}
	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("chunk unavailable: %s", chunkID)
}

// isChunkReadable checks if a chunk is available locally or from any peer.
func (s *Server) isChunkReadable(ctx context.Context, chunkID string) bool {
	cf, _, err := s.chunks.Open(chunkID)
	if err == nil {
		cf.Close()
		return true
	}
	return s.forEachAvailableReplica(ctx, chunkID, func(ctx context.Context, node types.Node, address string) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/api/chunks/"+chunkID, nil)
		if err != nil {
			return err
		}
		if err := s.signPeerRequest(req, emptyBodySHA256); err != nil {
			return err
		}
		resp, err := s.peerHTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		return nil
	}) == nil
}

// writeChunk writes a chunk's content to w, fetching from local storage or a peer.
func (s *Server) writeChunk(ctx context.Context, w io.Writer, chunkID string) error {
	cf, _, err := s.chunks.Open(chunkID)
	if err == nil {
		defer cf.Close()
		_, err = io.Copy(w, cf)
		return err
	}
	return s.forEachAvailableReplica(ctx, chunkID, func(ctx context.Context, node types.Node, address string) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+address+"/api/chunks/"+chunkID, nil)
		if err != nil {
			return err
		}
		if err := s.signPeerRequest(req, emptyBodySHA256); err != nil {
			return err
		}
		resp, err := s.peerHTTPClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status %d", resp.StatusCode)
		}
		_, err = io.Copy(w, resp.Body)
		return err
	})
}
