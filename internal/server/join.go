package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pocketcluster/agent/internal/config"
	"github.com/pocketcluster/agent/internal/peernet"
	"github.com/pocketcluster/agent/internal/types"
)

func (s *Server) JoinViaBootstrap(bootstrap, joinToken string) error {
	self, err := s.store.GetNode(s.cfg.NodeID)
	if err != nil {
		return err
	}
	join, err := callJoinRequest(bootstrap, joinToken, s.cfg, self)
	if err != nil {
		return err
	}
	s.cfg.ClusterID = join.ClusterID
	if err := s.cfg.Save(); err != nil {
		return err
	}
	now := time.Now()
	for _, ref := range join.ExistingNodes {
		if ref.NodeID == s.cfg.NodeID || ref.Address == "" {
			continue
		}
		if err := s.store.UpdateNodeFull(&types.Node{
			NodeID:    ref.NodeID,
			Name:      ref.Name,
			Address:   normalizeNodeAddress(ref.Address),
			PublicKey: ref.PublicKey,
			Status:    "online",
			Trusted:   true,
			LastSeen:  now,
		}); err != nil {
			return err
		}
	}
	return nil
}

func callJoinRequest(bootstrap, joinToken string, cfg *config.Config, self *types.Node) (*types.JoinResponse, error) {
	bootstrap = strings.TrimRight(bootstrap, "/")
	reqBody := types.JoinRequest{
		JoinToken: joinToken,
		NodeID:    cfg.NodeID,
		PublicKey: cfg.PublicKey,
		DeviceInfo: types.DeviceInfo{
			Name:           cfg.Name,
			Platform:       cfg.Platform,
			Address:        self.Address,
			TotalBytes:     self.TotalBytes,
			AvailableBytes: self.AvailableBytes,
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, bootstrap+"/api/join/request", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := peernet.NewHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bootstrap returned status %d", resp.StatusCode)
	}
	var envelope types.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if !envelope.OK {
		if envelope.Error != nil {
			return nil, fmt.Errorf("%s: %s", envelope.Error.Code, envelope.Error.Message)
		}
		return nil, fmt.Errorf("join rejected")
	}
	var join types.JoinResponse
	if err := json.Unmarshal(envelope.Data, &join); err != nil {
		return nil, err
	}
	return &join, nil
}

func normalizeNodeAddress(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "https://")
	return strings.TrimRight(value, "/")
}
