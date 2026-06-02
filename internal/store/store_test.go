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

func TestUpdateNodeFullOverwritesZeroCapacity(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Simulate: node first stored with zero capacity (failed auto-join)
	if err := s.UpsertNode(&types.Node{NodeID: "peer", Name: "peer", Address: "10.0.0.1:7788", Status: "online", Trusted: true}); err != nil {
		t.Fatal(err)
	}
	n, err := s.GetNode("peer")
	if err != nil {
		t.Fatal(err)
	}
	if n.TotalBytes != 0 {
		t.Fatalf("initial total_bytes = %d, want 0", n.TotalBytes)
	}

	// Simulate: node re-joined with real capacity
	if err := s.UpdateNodeFull(&types.Node{NodeID: "peer", Name: "peer", Address: "10.0.0.1:7788", Status: "online", Trusted: true, TotalBytes: 500, AvailableBytes: 400}); err != nil {
		t.Fatal(err)
	}
	n, err = s.GetNode("peer")
	if err != nil {
		t.Fatal(err)
	}
	if n.TotalBytes != 500 {
		t.Fatalf("total_bytes = %d, want 500", n.TotalBytes)
	}
	if n.AvailableBytes != 400 {
		t.Fatalf("available_bytes = %d, want 400", n.AvailableBytes)
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

func TestUpdateNodeFullPersistsAddressCandidates(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	wantCandidates := []string{"192.168.1.10:7788", "10.8.0.10:7788"}
	if err := s.UpdateNodeFull(&types.Node{
		NodeID:             "peer",
		Address:            wantCandidates[0],
		AddressCandidates:  wantCandidates,
		LastWorkingAddress: wantCandidates[1],
		Status:             "online",
		Trusted:            true,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNode("peer")
	if err != nil {
		t.Fatal(err)
	}
	if got.LastWorkingAddress != wantCandidates[1] {
		t.Fatalf("last_working_address = %q, want %q", got.LastWorkingAddress, wantCandidates[1])
	}
	if len(got.AddressCandidates) != len(wantCandidates) {
		t.Fatalf("address candidates = %#v, want %#v", got.AddressCandidates, wantCandidates)
	}
	for i := range wantCandidates {
		if got.AddressCandidates[i] != wantCandidates[i] {
			t.Fatalf("address candidates = %#v, want %#v", got.AddressCandidates, wantCandidates)
		}
	}
}
