package main

import (
	"context"
	"flag"
	"fmt"
	"io"
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


	"github.com/google/uuid"
	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/config"
	"github.com/pocketcluster/agent/internal/discovery"
	"github.com/pocketcluster/agent/internal/server"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

var version = "dev"

const nodeCapacityUpdateInterval = 30 * time.Second

func main() {
	dataDir := flag.String("data", defaultDataDir(), "data directory path")
	port := flag.Int("port", 7788, "HTTP listen port")
	name := flag.String("name", "", "node name (default: hostname)")
	iface := flag.String("iface", "", "network interface for mDNS (e.g., wlan0)")
	advertiseIP := flag.String("advertise-ip", "", "IP address to advertise for mDNS")
	localIP := flag.String("local-ip", "", "local IP address for network operations")
	joinBootstrap := flag.String("join", "", "bootstrap base URL to join")
	joinToken := flag.String("join-token", "", "invite token for joining an existing cluster")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("PocketCluster Agent", version)
		os.Exit(0)
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Set up ring buffer logger for agent logs API
	agentLogRing := server.NewRingBuffer(200)
	server.LogRing = agentLogRing
	log.SetOutput(&logWriter{ring: agentLogRing, orig: log.Writer()})
	log.Printf("PocketCluster Agent starting (platform=%s, data=%s)", runtime.GOOS, *dataDir)

	cfg, err := config.Load(*dataDir)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg.HTTPPort = *port
	if *name != "" {
		cfg.Name = *name
	}
	s, err := store.Open(*dataDir)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer s.Close()

	cs := chunk.New(*dataDir)
	if err := cs.Init(); err != nil {
		log.Fatalf("init chunk storage: %v", err)
	}

	selfNode, err := buildSelfNode(cfg, *dataDir, *port)
	if err != nil {
		log.Printf("read disk capacity: %v", err)
	}
	if err := s.UpdateNodeFull(selfNode); err != nil {
		log.Fatalf("update self node: %v", err)
	}
	srv := server.New(cfg, s, cs, *localIP)
	handler := srv.Handler()

	addr := fmt.Sprintf(":%d", *port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}
	log.Printf("listening on %s (node_id=%s)", listener.Addr(), cfg.NodeID)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	disc := discovery.New(cfg.NodeID, cfg.Name, cfg.Platform, *port, *iface, *advertiseIP)
	if err := disc.Start(ctx); err != nil {
		log.Printf("mDNS discovery failed to start: %v", err)
	} else {
		log.Printf("mDNS discovery started")
	}
	go syncDiscoveredNodes(ctx, s, srv, disc, cfg)

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
	go refreshSelfNode(ctx, cfg, s, srv, *dataDir, *port)

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

func syncDiscoveredNodes(ctx context.Context, s *store.Store, srv *server.Server, disc *discovery.Discovery, cfg *config.Config) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	ticksWithoutCluster := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			discovered := disc.Nodes()

			joined := false

			for _, n := range discovered {
				if n.NodeID == cfg.NodeID {
					continue
				}
				if s.HasTrustedNodeAtAddress(n.Address) {
					continue
				}
				if cfg.DiscoveryMode == "auto" && cfg.ClusterID == "" {
					if err := srv.JoinViaBootstrap("http://"+n.Address, ""); err != nil {
						log.Printf("auto-join %s (%s): %v", n.Name, n.Address, err)
					} else {
						log.Printf("auto-joined %s (%s)", n.Name, n.Address)
						joined = true
						break
					}
				}
				if cfg.DiscoveryMode == "invite" || cfg.ClusterID != "" {
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

			if cfg.DiscoveryMode == "auto" && cfg.ClusterID == "" && !joined {
				ticksWithoutCluster++
				if ticksWithoutCluster >= 5 {
					cfg.ClusterID = uuid.New().String()
					if err := cfg.Save(); err != nil {
						log.Printf("auto-create cluster: %v", err)
					} else {
						log.Printf("auto-created cluster %s", cfg.ClusterID)
					}
				}
			} else {
				ticksWithoutCluster = 0
			}
		}
	}
}

func buildSelfNode(cfg *config.Config, dataDir string, port int) (*types.Node, error) {
	disk, err := config.GetDiskStats(dataDir)
	totalBytes, availableBytes := int64(0), int64(0)
	if disk != nil {
		totalBytes = disk.TotalBytes
		availableBytes = disk.AvailableBytes
	}
	usedBytes := totalBytes - availableBytes
	if usedBytes < 0 {
		usedBytes = 0
	}
	now := time.Now()
	address := fmt.Sprintf("%s:%d", localAddress(), port)
	return &types.Node{
		NodeID:            cfg.NodeID,
		Name:              cfg.Name,
		Platform:          cfg.Platform,
		Address:           address,
		AddressCandidates: []string{address},
		PublicKey:         cfg.PublicKey,
		Status:            "online",
		Trusted:           true,
		TotalBytes:        totalBytes,
		UsedBytes:         usedBytes,
		AvailableBytes:    availableBytes,
		LastSeen:          now,
		JoinedAt:          now,
	}, err
}

func refreshSelfNode(ctx context.Context, cfg *config.Config, s *store.Store, srv *server.Server, dataDir string, port int) {
	ticker := time.NewTicker(nodeCapacityUpdateInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := buildSelfNode(cfg, dataDir, port)
			if err != nil {
				log.Printf("refresh self node: %v", err)
				continue
			}
			if err := s.UpdateNodeFull(n); err != nil {
				log.Printf("update self node: %v", err)
				continue
			}
			if cfg.ClusterID != "" {
				if err := srv.PublishNodeUpdate(n); err != nil {
					log.Printf("publish node update: %v", err)
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

type logWriter struct {
	ring *server.RingBuffer
	orig io.Writer
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	line := string(p)
	w.ring.Add(line)
	return w.orig.Write(p)
}
