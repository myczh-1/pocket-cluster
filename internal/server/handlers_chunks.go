package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

// handleHeadChunk is a lightweight existence check for a chunk.
func (s *Server) handleHeadChunk(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if hash == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	f, size, err := s.chunks.Open(hash)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f.Close()
	w.Header().Set("Content-Length", fmt.Sprint(size))
	w.WriteHeader(http.StatusOK)
}
func (s *Server) handleGetChunk(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if hash == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "hash required")
		return
	}
	f, size, err := s.chunks.Open(hash)
	if err != nil {
		writeError(w, http.StatusNotFound, "CHUNK_NOT_FOUND", hash)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprint(size))
	io.Copy(w, f)
}

func (s *Server) handleStoreChunk(w http.ResponseWriter, r *http.Request) {
	expectedHash := r.Header.Get("X-Chunk-Hash")
	if expectedHash == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "X-Chunk-Hash header required")
		return
	}
	if bodyHash := r.Header.Get(authBodySHA256Header); bodyHash != expectedHash {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "signed body hash mismatch")
		return
	}
	actualHash, size, err := s.chunks.Store(r.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if actualHash != expectedHash {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "hash mismatch")
		return
	}
	now := time.Now()
	if err := s.store.UpsertChunk(&types.Chunk{ChunkID: actualHash, SizeBytes: size, StoredAt: now}); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	replica := &types.Replica{ChunkID: actualHash, NodeID: s.cfg.NodeID, Status: "available", StoredAt: now, VerifiedAt: now}
	if err := s.store.UpsertReplica(replica); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if _, err := s.appendEvent(types.EventChunkReplicaAdd, replica); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"hash":       actualHash,
		"size_bytes": size,
		"stored":     true,
		"replica":    replica,
	})})
}

func (s *Server) handleGetEvents(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	events, err := s.store.GetEventsSince(since, 1000)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"events":   events,
		"has_more": len(events) >= 1000,
	})})
}

func (s *Server) handlePushEvents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Events []types.Event `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	accepted := 0
	senderNodeID := r.Header.Get("X-Node-ID")
	for _, e := range req.Events {
		e = rewritePushedNodeAddress(e, senderNodeID, r.RemoteAddr)
		inserted, err := s.store.InsertEvent(&e)
		if err != nil {
			continue
		}
		if !inserted {
			accepted++
			continue
		}
		if err := s.applyEvent(e); err != nil {
			continue
		}
		accepted++
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"accepted":  accepted,
		"conflicts": []any{},
	})})
}

func rewritePushedNodeAddress(e types.Event, senderNodeID, remoteAddr string) types.Event {
	if e.Type != types.EventNodeUpdate && e.Type != types.EventNodeJoin {
		return e
	}
	var n types.Node
	if err := json.Unmarshal(e.Payload, &n); err != nil {
		return e
	}
	if n.NodeID == "" || n.NodeID != senderNodeID {
		return e
	}
	observedAddress := addressFromRemote(remoteAddr, n.Address)
	n.Address = normalizeNodeAddress(n.Address)
	if isLoopbackAddress(n.Address) && !isLoopbackAddress(observedAddress) {
		n.Address = observedAddress
	}
	n.AddressCandidates = filterLoopbackAddresses(mergeAddressCandidates(n.AddressCandidates, n.Address, observedAddress))
	n.LastWorkingAddress = observedAddress
	if payload, err := json.Marshal(n); err == nil {
		e.Payload = payload
	}
	return e
}
