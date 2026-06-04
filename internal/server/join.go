package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/pocketcluster/agent/internal/config"
	"github.com/pocketcluster/agent/internal/peernet"
	"github.com/pocketcluster/agent/internal/types"
)

func (s *Server) JoinViaBootstrap(bootstrap, joinToken, poolUser, poolPassword string) error {
	self, err := s.store.GetNode(s.cfg.NodeID)
	if err != nil {
		return err
	}
	join, err := callJoinRequest(bootstrap, joinToken, poolUser, poolPassword, s.cfg, self)
	if err != nil {
		return err
	}
	// Poll for approval if pending
	if !join.Approved {
		log.Printf("join request pending, waiting for approval...")
		pollInterval := 5 * time.Second
		if s.joinPollInterval > 0 {
			pollInterval = s.joinPollInterval
		}
		for i := 0; i < 60; i++ { // poll for up to 5 minutes
			time.Sleep(pollInterval)
			join, err = callJoinRequest(bootstrap, joinToken, poolUser, poolPassword, s.cfg, self)
			if err != nil {
				return err
			}
			if join.Approved {
				break
			}
		}
		if !join.Approved {
			return fmt.Errorf("join request timed out waiting for approval")
		}
	}
	s.cfg.ClusterID = join.ClusterID
	if poolUser != "" && poolPassword != "" {
		s.cfg.SetPoolCredentials(poolUser, poolPassword)
	}
	if err := s.cfg.Save(); err != nil {
		return err
	}
	now := time.Now()
	for _, ref := range join.ExistingNodes {
		if ref.NodeID == s.cfg.NodeID || ref.Address == "" {
			continue
		}
		status := ref.Status
		if status == "" {
			status = "online"
		}
		lastSeen := ref.LastSeen
		if lastSeen.IsZero() {
			lastSeen = now
		}
		candidates := mergeAddressCandidates(ref.AddressCandidates, ref.Address, ref.LastWorkingAddress)
		if err := s.store.UpdateNodeFull(&types.Node{
			NodeID:             ref.NodeID,
			Name:               ref.Name,
			Platform:           ref.Platform,
			Address:            normalizeNodeAddress(ref.Address),
			AddressCandidates:  candidates,
			LastWorkingAddress: normalizeNodeAddress(ref.LastWorkingAddress),
			PublicKey:          ref.PublicKey,
			TotalBytes:         ref.TotalBytes,
			UsedBytes:          ref.UsedBytes,
			AvailableBytes:     ref.AvailableBytes,
			Status:             status,
			Trusted:            true,
			LastSeen:           lastSeen,
			JoinedAt:           ref.JoinedAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func callJoinRequest(bootstrap, joinToken, poolUser, poolPassword string, cfg *config.Config, self *types.Node) (*types.JoinResponse, error) {
	bootstrap = normalizeBootstrapURL(bootstrap)
	reqBody := types.JoinRequest{
		JoinToken:    joinToken,
		PoolUser:     poolUser,
		PoolPassword: poolPassword,
		NodeID:       cfg.NodeID,
		PublicKey:    cfg.PublicKey,
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

func normalizeBootstrapURL(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, "/")
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}
	return "http://" + value
}

func normalizeNodeAddress(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "https://")
	return strings.TrimRight(value, "/")
}
