package main

import (
	"testing"

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
