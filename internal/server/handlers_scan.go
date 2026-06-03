package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func (s *Server) handleScanNetwork(w http.ResponseWriter, r *http.Request) {
	// Get local IP to determine subnet
	localIP := s.getLocalIP()
	if localIP == "" && s.localIP != "" {
		localIP = s.localIP
	}
	log.Printf("network scan: local IP = %q (server=%q)", localIP, s.localIP)
	if localIP == "" {
		writeError(w, http.StatusInternalServerError, "NO_NETWORK", "cannot determine local network - try manual join")
		return
	}
	// Parse subnet (assume /24)
	parts := strings.Split(localIP, ".")
	if len(parts) != 4 {
		writeError(w, http.StatusInternalServerError, "INVALID_IP", "invalid local IP")
		return
	}
	prefix := strings.Join(parts[:3], ".")

	// Scan subnet for PocketCluster nodes
	var (
		mu    sync.Mutex
		nodes []map[string]any
		wg    sync.WaitGroup
		sem   = make(chan struct{}, 50) // Limit concurrency
	)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	for i := 1; i < 255; i++ {
		ip := fmt.Sprintf("%s.%d", prefix, i)
		if ip == localIP {
			continue // Skip self
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()

			addr := fmt.Sprintf("%s:%d", ip, s.cfg.HTTPPort)
			nodeInfo, err := s.probeNode(ctx, addr)
			if err != nil {
				return
			}

			mu.Lock()
			nodes = append(nodes, nodeInfo)
			mu.Unlock()
		}(ip)
	}

	wg.Wait()

	writeJSON(w, http.StatusOK, types.APIResponse{
		OK: true,
		Data: mustMarshal(map[string]any{
			"local_ip":    localIP,
			"subnet":      prefix + ".0/24",
			"found":       len(nodes),
			"nodes":       nodes,
		}),
	})
}

func (s *Server) getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				return ip4.String()
			}
		}
	}
	return ""
}

func (s *Server) probeNode(ctx context.Context, addr string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/health", nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var envelope types.APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}

	if !envelope.OK {
		return nil, fmt.Errorf("not a PocketCluster node")
	}

	// Extract node info from health response
	var health struct {
		NodeID string `json:"node_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(envelope.Data, &health); err != nil {
		return nil, err
	}

	return map[string]any{
		"node_id": health.NodeID,
		"address": addr,
		"status":  health.Status,
	}, nil
}
