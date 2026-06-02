package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func TestInsertEventReportsDuplicatesAndReadsPayload(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	event := &types.Event{
		EventID:   "node-a:1",
		Type:      types.EventFilePut,
		NodeID:    "node-a",
		Seq:       1,
		Timestamp: time.UnixMilli(1234),
		Payload:   json.RawMessage(`{"path":"/a.txt"}`),
	}
	inserted, err := s.InsertEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("first insert was not reported as inserted")
	}
	inserted, err = s.InsertEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Fatal("duplicate insert was reported as inserted")
	}

	events, err := s.GetEventsSince("", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if string(events[0].Payload) != string(event.Payload) {
		t.Fatalf("payload = %s, want %s", events[0].Payload, event.Payload)
	}
}

func TestUpsertNodePreservesTrustWhenDiscoveryUpdatesAddress(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	joined := &types.Node{NodeID: "node-b", PublicKey: "pub", Address: "old:7788", Trusted: true, LastSeen: time.UnixMilli(1)}
	if err := s.UpsertNode(joined); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNode(&types.Node{NodeID: "node-b", Address: "new:7788", Status: "online", LastSeen: time.UnixMilli(2)}); err != nil {
		t.Fatal(err)
	}
	n, err := s.GetNode("node-b")
	if err != nil {
		t.Fatal(err)
	}
	if !n.Trusted {
		t.Fatal("discovery update cleared trusted flag")
	}
	if n.PublicKey != "pub" {
		t.Fatalf("public key = %q, want preserved", n.PublicKey)
	}
	if n.Address != "new:7788" {
		t.Fatalf("address = %q, want new:7788", n.Address)
	}
}

func TestUpsertNodeZeroTimeDoesNotOverwriteLastSeen(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	seen := time.UnixMilli(1234)
	if err := s.UpsertNode(&types.Node{NodeID: "node-c", Status: "online", LastSeen: seen}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNode(&types.Node{NodeID: "node-c", Status: "offline"}); err != nil {
		t.Fatal(err)
	}
	n, err := s.GetNode("node-c")
	if err != nil {
		t.Fatal(err)
	}
	if !n.LastSeen.Equal(seen) {
		t.Fatalf("last_seen = %s, want %s", n.LastSeen, seen)
	}
	if n.Status != "offline" {
		t.Fatalf("status = %q, want offline", n.Status)
	}
}

func TestUseInviteIsOneTimeAndExpires(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.UnixMilli(1000)
	if err := s.CreateInvite(&types.Invite{TokenHash: "active", CreatedAt: now, ExpiresAt: now.Add(time.Minute), CreatedBy: "node"}); err != nil {
		t.Fatal(err)
	}
	ok, err := s.UseInvite("active", now)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("active invite was not accepted")
	}
	ok, err = s.UseInvite("active", now)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("used invite was accepted a second time")
	}

	if err := s.CreateInvite(&types.Invite{TokenHash: "expired", CreatedAt: now, ExpiresAt: now.Add(-time.Second), CreatedBy: "node"}); err != nil {
		t.Fatal(err)
	}
	ok, err = s.UseInvite("expired", now)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expired invite was accepted")
	}
}
