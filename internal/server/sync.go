package server

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

const (
	syncRequestTimeout  = 15 * time.Second
	targetReplicaCount  = 2
	nodeOfflineAfter    = 30 * time.Second
	minFreeSpace        = 256 * 1024 * 1024 // 256MB minimum free space to accept a chunk
	syncPeerConcurrency = 5
)

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
			if err := s.CleanupTombstonesContext(ctx); err != nil {
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
	firstErr := s.syncPeers(ctx, nodes)
	if err := s.fetchMissingChunks(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	if s.health != nil {
		underReplicated := s.DrainUnderReplicated()
		if len(underReplicated) > 0 {
			nodes, _ := s.store.ListNodes()
			var stillNeeded []string
			for _, chunkID := range underReplicated {
				s.MarkRepairing(chunkID, true)
				if err := s.repairChunkReplicas(ctx, chunkID, nodes); err != nil {
					log.Printf("sync: repair chunk %s: %v", chunkID, err)
				}
				s.MarkRepairing(chunkID, false)
				// Re-queue if still under-replicated after repair attempt.
				replicas, _ := s.store.GetReplicas(chunkID)
				online := onlineNodeSet(nodes, s.cfg.NodeID)
				if len(availableOnlineReplicaNodes(replicas, online)) < targetReplicaCount {
					stillNeeded = append(stillNeeded, chunkID)
				}
			}
			if len(stillNeeded) > 0 {
				s.QueueUnderReplicated(stillNeeded)
			}
			s.SetLastRepairAt(time.Now())
		}
	}
	return firstErr
}

func (s *Server) syncPeers(ctx context.Context, nodes []types.Node) error {
	sem := make(chan struct{}, syncPeerConcurrency)
	errs := make(chan error, len(nodes))
	var wg sync.WaitGroup
	for _, n := range nodes {
		if n.NodeID == s.cfg.NodeID || n.Address == "" || !n.Trusted {
			continue
		}
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errs <- ctx.Err()
				return
			}
			if err := s.syncPeer(ctx, n); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) syncPeer(ctx context.Context, n types.Node) error {
	var pullAddress string
	var pushAddress string
	var pullErr error
	var pushErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		pullAddress, pullErr = s.pullEvents(ctx, n)
	}()
	go func() {
		defer wg.Done()
		pushAddress, pushErr = s.pushEvents(ctx, n)
	}()
	wg.Wait()
	if pullErr == nil || pushErr == nil {
		workingAddress := pullAddress
		if workingAddress == "" {
			workingAddress = pushAddress
		}
		return s.markPeerOnline(n.NodeID, workingAddress, time.Now())
	}
	if err := s.markPeerOfflineIfStale(n, time.Now()); err != nil {
		return err
	}
	return fmt.Errorf("pull events from %s: %w; push events: %v", n.NodeID, pullErr, pushErr)
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
