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
	if s.cfg.ClusterID != "" && s.cfg.HasPoolCredentials() {
		writeError(w, http.StatusConflict, "ALREADY_JOINED", "node already belongs to a cluster")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&req)
	}
	if req.Username == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "username and password are required")
		return
	}
	if s.cfg.ClusterID == "" {
		s.cfg.ClusterID = uuid.New().String()
	}
	if err := s.cfg.SetPoolCredentials(req.Username, req.Password); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if err := s.cfg.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	sessionToken := s.sessions.create()
	http.SetCookie(w, &http.Cookie{
		Name:     "pc-session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(24 * time.Hour.Seconds()),
	})
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"cluster_id": s.cfg.ClusterID,
		"username":   s.cfg.PoolUser,
	})})
}

func (s *Server) handleJoinCluster(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Bootstrap    string `json:"bootstrap"`
		JoinToken    string `json:"join_token,omitempty"`
		PoolUser     string `json:"pool_user,omitempty"`
		PoolPassword string `json:"pool_password,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if req.Bootstrap == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "bootstrap required")
		return
	}
	if err := s.JoinViaBootstrap(req.Bootstrap, req.JoinToken, req.PoolUser, req.PoolPassword); err != nil {
		writeError(w, http.StatusBadGateway, "JOIN_FAILED", err.Error())
		return
	}
	nodes, _ := s.store.ListNodes()
	sessionToken := s.sessions.create()
	http.SetCookie(w, &http.Cookie{
		Name:     "pc-session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(24 * time.Hour.Seconds()),
	})
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
	if s.cfg.ClusterID == "" {
		writeError(w, http.StatusBadRequest, "NOT_READY", "this node is not part of a pool yet")
		return
	}
	// Check if this node was already approved while the joiner is polling.
	existing, err := s.store.GetNode(req.NodeID)
	if err == nil && existing.Trusted && req.PublicKey != "" && req.PublicKey == existing.PublicKey {
		nodes, _ := s.store.ListNodes()
		var refs []types.Node
		for _, n := range nodes {
			if n.NodeID != req.NodeID {
				refs = append(refs, n)
			}
		}
		writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(types.JoinResponse{
			ClusterID:     s.cfg.ClusterID,
			Approved:      true,
			ExistingNodes: refs,
			PoolUser:      s.cfg.PoolUser,
			PoolPassHash:  s.cfg.PoolPassHash,
		})})
		return
	}
	if pending, err := s.store.GetPendingJoin(req.NodeID); err == nil && req.PublicKey != "" && req.PublicKey == pending.PublicKey {
		writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
			"approved": false,
			"pending":  true,
			"message":  "join request is still waiting for approval from pool member",
		})})
		return
	}
	// Validate pool credentials if provided
	if req.PoolUser != "" && req.PoolPassword != "" {
		if req.PoolUser != s.cfg.PoolUser || !s.cfg.CheckPoolPassword(req.PoolPassword) {
			writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid pool credentials")
			return
		}
	} else if req.JoinToken != "" {
		accepted, err := s.store.UseInvite(inviteTokenHash(req.JoinToken), time.Now())
		if err != nil || !accepted {
			writeError(w, http.StatusForbidden, "JOIN_TOKEN_INVALID", "join token is invalid or expired")
			return
		}
	} else {
		writeError(w, http.StatusBadRequest, "CREDENTIALS_REQUIRED", "pool credentials or invite token required")
		return
	}
	// Normalize address
	advertisedAddress := normalizeNodeAddress(req.DeviceInfo.Address)
	observedAddress := addressFromRemote(r.RemoteAddr, advertisedAddress)
	if advertisedAddress == "" || isLoopbackAddress(advertisedAddress) {
		advertisedAddress = observedAddress
	}
	now := time.Now()
	pj := &types.PendingJoin{
		NodeID:          req.NodeID,
		Name:            req.DeviceInfo.Name,
		Platform:        req.DeviceInfo.Platform,
		Address:         advertisedAddress,
		ObservedAddress: observedAddress,
		PublicKey:       req.PublicKey,
		TotalBytes:      req.DeviceInfo.TotalBytes,
		AvailableBytes:  req.DeviceInfo.AvailableBytes,
		RequestedAt:     now,
		ExpiresAt:       now.Add(30 * time.Minute),
	}
	if err := s.store.CreatePendingJoin(pj); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"approved": false,
		"pending":  true,
		"message":  "join request received, waiting for approval from pool member",
	})})
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

func (s *Server) handleUploadProgress(w http.ResponseWriter, r *http.Request) {
	list := s.uploadProgress.list()
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(list)})
}

func (s *Server) handleJoinApprove(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeId")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "nodeId required")
		return
	}
	pj, err := s.store.GetPendingJoin(nodeID)
	if err != nil {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "pending join request not found")
		return
	}
	now := time.Now()
	advertisedAddress := normalizeNodeAddress(pj.Address)
	observedAddress := normalizeNodeAddress(pj.ObservedAddress)
	if advertisedAddress == "" || isLoopbackAddress(advertisedAddress) {
		advertisedAddress = observedAddress
	}
	candidates := filterLoopbackAddresses(mergeAddresses(advertisedAddress, observedAddress))
	newNode := &types.Node{
		NodeID:             pj.NodeID,
		Name:               pj.Name,
		Platform:           pj.Platform,
		Address:            advertisedAddress,
		AddressCandidates:  candidates,
		LastWorkingAddress: observedAddress,
		PublicKey:          pj.PublicKey,
		TotalBytes:         pj.TotalBytes,
		AvailableBytes:     pj.AvailableBytes,
		Status:             "online",
		Trusted:            true,
		LastSeen:           now,
		JoinedAt:           now,
	}
	if err := s.store.UpdateNodeFull(newNode); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	s.store.DeletePendingJoin(nodeID)
	s.appendEvent(types.EventNodeJoin, newNode)
	nodes, _ := s.store.ListNodes()
	var refs []types.Node
	for _, n := range nodes {
		if n.NodeID != nodeID {
			refs = append(refs, n)
		}
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(types.JoinResponse{
		ClusterID:     s.cfg.ClusterID,
		Approved:      true,
		ExistingNodes: refs,
		PoolUser:      s.cfg.PoolUser,
		PoolPassHash:  s.cfg.PoolPassHash,
	})})
}

func (s *Server) handleListPendingJoins(w http.ResponseWriter, r *http.Request) {
	s.store.CleanExpiredPendingJoins()
	pending, err := s.store.ListPendingJoins()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(pending)})
}
