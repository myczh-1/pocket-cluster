package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	info := map[string]any{
		"node_id":        s.cfg.NodeID,
		"name":           s.cfg.Name,
		"platform":       s.cfg.Platform,
		"cluster_id":     s.cfg.ClusterID,
		"discovery_mode": s.cfg.DiscoveryMode,
	}
	if err == nil {
		info["status"] = n.Status
		info["total_bytes"] = n.TotalBytes
		info["used_bytes"] = n.UsedBytes
		info["available_bytes"] = n.AvailableBytes
	} else {
		info["status"] = "online"
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(info)})
}

func (s *Server) handleCreateCluster(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ClusterID != "" {
		writeError(w, http.StatusConflict, "ALREADY_JOINED", "node already belongs to a cluster")
		return
	}
	s.cfg.ClusterID = uuid.New().String()
	if err := s.cfg.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"cluster_id": s.cfg.ClusterID,
	})})
}

func (s *Server) handleJoinCluster(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bootstrap string `json:"bootstrap"`
		JoinToken string `json:"join_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if req.Bootstrap == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "bootstrap required")
		return
	}
	if err := s.JoinViaBootstrap(req.Bootstrap, req.JoinToken); err != nil {
		writeError(w, http.StatusBadGateway, "JOIN_FAILED", err.Error())
		return
	}
	nodes, _ := s.store.ListNodes()
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"cluster_id": s.cfg.ClusterID,
		"node_count": len(nodes),
	})})
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	trusted := make([]types.Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Trusted {
			trusted = append(trusted, n)
		}
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(trusted)})
}

func (s *Server) handleListDiscovered(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListNodes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	discovered := make([]types.Node, 0)
	for _, n := range nodes {
		if !n.Trusted {
			discovered = append(discovered, n)
		}
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(discovered)})
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	token, err := newInviteToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	now := time.Now()
	expiresAt := now.Add(15 * time.Minute)
	invite := &types.Invite{
		TokenHash: inviteTokenHash(token),
		CreatedAt: now,
		ExpiresAt: expiresAt,
		CreatedBy: s.cfg.NodeID,
	}
	if err := s.store.CreateInvite(invite); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"join_token": token,
		"expires_at": expiresAt,
	})})
}

func newInviteToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func inviteTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Server) handleJoinRequest(w http.ResponseWriter, r *http.Request) {
	var req types.JoinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	now := time.Now()
	if s.cfg.DiscoveryMode == "auto" {
		// Auto mode: no token required
	} else if req.JoinToken == "" {
		writeError(w, http.StatusForbidden, "JOIN_TOKEN_REQUIRED", "join token required")
		return
	} else {
		accepted, err := s.store.UseInvite(inviteTokenHash(req.JoinToken), now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		if !accepted {
			writeError(w, http.StatusForbidden, "JOIN_TOKEN_INVALID", "join token is invalid, expired, or already used")
			return
		}
	}
	if s.cfg.ClusterID == "" {
		s.cfg.ClusterID = uuid.New().String()
		if err := s.cfg.Save(); err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
	}
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
	if err := s.store.UpdateNodeFull(newNode); err != nil {
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
			refs = append(refs, types.NodeRef{NodeID: n.NodeID, Name: n.Name, Address: n.Address, PublicKey: n.PublicKey, TotalBytes: n.TotalBytes, AvailableBytes: n.AvailableBytes})
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
