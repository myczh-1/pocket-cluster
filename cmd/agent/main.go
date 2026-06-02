package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/config"
	"github.com/pocketcluster/agent/internal/discovery"
	"github.com/pocketcluster/agent/internal/server"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func main() {
	dataDir := flag.String("data", defaultDataDir(), "data directory path")
	port := flag.Int("port", 7788, "HTTP listen port")
	joinBootstrap := flag.String("join", "", "bootstrap base URL to join")
	joinToken := flag.String("join-token", "", "invite token for joining an existing cluster")
	flag.Parse()

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("PocketCluster Agent starting (platform=%s, data=%s)", runtime.GOOS, *dataDir)

	cfg, err := config.Load(*dataDir)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg.HTTPPort = *port

	s, err := store.Open(*dataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer s.Close()

	cs := chunk.New(*dataDir)
	if err := cs.Init(); err != nil {
		log.Fatalf("init chunk storage: %v", err)
	}

	disk, _ := config.GetDiskStats(*dataDir)
	totalBytes, availableBytes := int64(0), int64(0)
	if disk != nil {
		totalBytes = disk.TotalBytes
		availableBytes = disk.AvailableBytes
	}
	now := time.Now()
	selfNode := &types.Node{
		NodeID:         cfg.NodeID,
		Name:           cfg.Name,
		Platform:       cfg.Platform,
		Address:        fmt.Sprintf("%s:%d", localAddress(), *port),
		Status:         "online",
		Trusted:        true,
		TotalBytes:     totalBytes,
		AvailableBytes: availableBytes,
		LastSeen:       now,
		JoinedAt:       now,
	}
	s.UpsertNode(selfNode)
	srv := server.New(cfg, s, cs)
	handler := srv.Handler()

	addr := fmt.Sprintf(":%d", *port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
	log.Printf("listening on %s (node_id=%s)", listener.Addr(), cfg.NodeID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	disc := discovery.New(cfg.NodeID, cfg.Name, cfg.Platform, *port)
	if err := disc.Start(ctx); err != nil {
		log.Printf("mDNS discovery failed to start: %v", err)
	} else {
		log.Printf("mDNS discovery started")
	}
	go syncDiscoveredNodes(ctx, s, disc, cfg.NodeID)

	httpSrv := &http.Server{Handler: handler}
	go func() {
		if err := httpSrv.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()

	if *joinBootstrap != "" {
		bootstrap := normalizeBaseURL(*joinBootstrap)
		if err := srv.JoinViaBootstrap(bootstrap, *joinToken); err != nil {
			log.Fatalf("join cluster: %v", err)
		}
		log.Printf("joined cluster %s via %s", cfg.ClusterID, bootstrap)
	}
	go srv.StartSync(ctx, 2*time.Second)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("shutting down...")
	disc.Stop()
	httpSrv.Close()
}

func defaultDataDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pocketcluster")
}

func syncDiscoveredNodes(ctx context.Context, s *store.Store, disc *discovery.Discovery, selfNodeID string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			for _, n := range disc.Nodes() {
				if n.NodeID == selfNodeID {
					continue
				}
				if s.HasTrustedNodeAtAddress(n.Address) {
					continue
				}
				if err := s.UpsertNode(&types.Node{
					NodeID:   n.NodeID,
					Name:     n.Name,
					Platform: n.Platform,
					Address:  n.Address,
					Status:   discoveredStatus(s, n.NodeID),
					LastSeen: now,
				}); err != nil {
					log.Printf("update discovered node %s: %v", n.NodeID, err)
				}
			}
		}
	}
}

func discoveredStatus(s *store.Store, nodeID string) string {
	n, err := s.GetNode(nodeID)
	if err == nil && n.Trusted {
		return "online"
	}
	return "discovered"
}

func normalizeBaseURL(value string) string {
	value = strings.TrimSpace(value)
	return strings.TrimRight(value, "/")
}

func normalizePeerAddress(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "http://")
	value = strings.TrimPrefix(value, "https://")
	return strings.TrimRight(value, "/")
}

func localAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "localhost"
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ipv4 := ip.To4(); ipv4 != nil {
				return ipv4.String()
			}
		}
	}
	return "localhost"
}
