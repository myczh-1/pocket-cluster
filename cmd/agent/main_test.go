package main

import (
	"testing"

	"github.com/pocketcluster/agent/internal/config"

	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func TestNormalizeJoinAddresses(t *testing.T) {
	if got := normalizeBaseURL(" http://127.0.0.1:7788/ "); got != "http://127.0.0.1:7788" {
		t.Fatalf("normalizeBaseURL = %q", got)
	}
	if got := normalizePeerAddress("http://127.0.0.1:7788/"); got != "127.0.0.1:7788" {
		t.Fatalf("normalizePeerAddress http = %q", got)
	}
	if got := normalizePeerAddress("https://node.local:7788/"); got != "node.local:7788" {
		t.Fatalf("normalizePeerAddress https = %q", got)
	}
}

func TestBuildSelfNodeReportsCapacity(t *testing.T) {
	cfg := testConfig(t)
	node, err := buildSelfNode(cfg, t.TempDir(), 7788)
	if err != nil {
		t.Fatal(err)
	}
	if node.PublicKey != cfg.PublicKey {
		t.Fatal("self node public key not populated")
	}
	if node.TotalBytes <= 0 {
		t.Fatalf("total bytes = %d, want > 0", node.TotalBytes)
	}
	if node.AvailableBytes <= 0 {
		t.Fatalf("available bytes = %d, want > 0", node.AvailableBytes)
	}
	if node.UsedBytes < 0 {
		t.Fatalf("used bytes = %d, want >= 0", node.UsedBytes)
	}
}

func TestDiscoveredStatusKeepsUntrustedPeersOutOfOnlineSet(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if got := discoveredStatus(st, "peer"); got != "discovered" {
		t.Fatalf("new discovered status = %q, want discovered", got)
	}
	if err := st.UpsertNode(&types.Node{NodeID: "peer", Trusted: true, Status: "online"}); err != nil {
		t.Fatal(err)
	}
	if got := discoveredStatus(st, "peer"); got != "online" {
		t.Fatalf("trusted discovered status = %q, want online", got)
	}
}

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}
