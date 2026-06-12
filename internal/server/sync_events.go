package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

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
	resp, err := s.peerHTTPClient.Do(req)
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
		Events          []types.Event   `json:"events"`
		Snapshot        json.RawMessage `json:"snapshot"`
		SnapshotEventID string          `json:"snapshot_event_id"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		return err
	}
	// If the peer sent a snapshot and we have no local events, bootstrap from it.
	if payload.Snapshot != nil {
		localCount, _ := s.store.EventCount()
		if localCount == 0 {
			var snap store.MetadataSnapshot
			if err := json.Unmarshal(payload.Snapshot, &snap); err != nil {
				return fmt.Errorf("unmarshal snapshot: %w", err)
			}
			if err := s.store.LoadSnapshot(&snap); err != nil {
				return fmt.Errorf("load snapshot: %w", err)
			}
			log.Printf("sync: loaded snapshot from %s (event_id=%s, nodes=%d, files=%d)",
				n.NodeID, snap.LastEventID, len(snap.Nodes), len(snap.Files))
		}
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
	resp, err := s.peerHTTPClient.Do(req)
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
