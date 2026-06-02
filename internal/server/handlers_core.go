package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/pocketcluster/agent/internal/types"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, types.APIResponse{
		OK: true,
		Data: mustMarshal(map[string]any{
			"node_id":        s.cfg.NodeID,
			"status":         "online",
			"uptime_seconds": int(time.Since(s.started).Seconds()),
		}),
	})
}

func (s *Server) handleNodeInfo(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.GetNode(s.cfg.NodeID)
	if err != nil {
		writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
			"node_id":  s.cfg.NodeID,
			"name":     s.cfg.Name,
			"platform": s.cfg.Platform,
			"status":   "online",
		})})
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(n)})
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(nodes)})
}

func (s *Server) handleJoinRequest(w http.ResponseWriter, r *http.Request) {
	var req types.JoinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if s.cfg.ClusterID == "" {
		s.cfg.ClusterID = uuid.New().String()
		s.cfg.Save()
	}
	now := time.Now()
	address := req.DeviceInfo.Address
	if address == "" {
		address = r.RemoteAddr
	}
	newNode := &types.Node{
		NodeID:         req.NodeID,
		Name:           req.DeviceInfo.Name,
		Platform:       req.DeviceInfo.Platform,
		Address:        address,
		PublicKey:      req.PublicKey,
		TotalBytes:     req.DeviceInfo.TotalBytes,
		AvailableBytes: req.DeviceInfo.AvailableBytes,
		Status:         "online",
		Trusted:        true,
		LastSeen:       now,
		JoinedAt:       now,
	}
	if err := s.store.UpsertNode(newNode); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if _, err := s.appendEvent(types.EventNodeJoin, newNode); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	nodes, _ := s.store.ListNodes()
	var refs []types.NodeRef
	for _, n := range nodes {
		if n.NodeID != req.NodeID {
			refs = append(refs, types.NodeRef{NodeID: n.NodeID, Address: n.Address})
		}
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(types.JoinResponse{
		ClusterID:     s.cfg.ClusterID,
		Approved:      true,
		ExistingNodes: refs,
	})})
}

func (s *Server) handleJoinApprove(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]bool{"approved": true})})
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" {
		path = "/"
	}
	keyword := r.URL.Query().Get("q")
	if keyword != "" {
		files, err := s.store.SearchFiles(keyword)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{"path": path, "entries": files})})
		return
	}
	files, err := s.store.ListFiles(path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{"path": path, "entries": files})})
}
