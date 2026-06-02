package server

import (
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

const syncRequestTimeout = 15 * time.Second

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
		if n.NodeID == s.cfg.NodeID || n.Address == "" || n.Status == "offline" {
			continue
		}
		if err := s.pullEvents(ctx, n); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := s.fetchMissingChunks(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (s *Server) pullEvents(ctx context.Context, n types.Node) error {
	ctx, cancel := context.WithTimeout(ctx, syncRequestTimeout)
	defer cancel()
	url := "http://" + n.Address + "/api/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
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

func (s *Server) fetchMissingChunks(ctx context.Context) error {
	files, err := s.store.ListAllFiles()
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
			if s.chunks.Exists(chunkID) {
				continue
			}
			if err := s.fetchChunkFromReplica(ctx, chunkID); err != nil {
				return err
			}
		}
	}
	return nil
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
		if err != nil || n.Address == "" {
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

func (s *Server) storeRemoteChunk(ctx context.Context, address, chunkID string) (string, int64, error) {
	url := "http://" + address + "/api/chunks/" + chunkID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
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
		if err != nil || n.Address == "" {
			continue
		}
		url := "http://" + n.Address + "/api/chunks/" + chunkID
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
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
