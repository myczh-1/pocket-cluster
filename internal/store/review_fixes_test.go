package store

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func TestListFilesTreatsPercentAndUnderscoreLiterally(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	files := []*types.File{
		{FileID: "literal-percent", Name: "child.txt", Path: "/100%/child.txt"},
		{FileID: "wild-percent", Name: "child.txt", Path: "/100x/child.txt"},
		{FileID: "literal-underscore", Name: "child.txt", Path: "/a_b/child.txt"},
		{FileID: "wild-underscore", Name: "child.txt", Path: "/acb/child.txt"},
	}
	for _, f := range files {
		if err := s.UpsertFile(f); err != nil {
			t.Fatal(err)
		}
	}
	percent, err := s.ListFiles("/100%")
	if err != nil {
		t.Fatal(err)
	}
	if len(percent) != 1 || percent[0].FileID != "literal-percent" {
		t.Fatalf("/100%% entries = %#v, want literal percent child only", percent)
	}
	underscore, err := s.ListFiles("/a_b")
	if err != nil {
		t.Fatal(err)
	}
	if len(underscore) != 1 || underscore[0].FileID != "literal-underscore" {
		t.Fatalf("/a_b entries = %#v, want literal underscore child only", underscore)
	}
}

func TestGetUnpushedEventsExcludesMarkedPeerEvents(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	events := []types.Event{
		{EventID: "node:1", Type: types.EventNodeUpdate, NodeID: "node", Seq: 1, Timestamp: now, Payload: json.RawMessage(`{"node_id":"node"}`)},
		{EventID: "node:2", Type: types.EventNodeUpdate, NodeID: "node", Seq: 2, Timestamp: now.Add(time.Millisecond), Payload: json.RawMessage(`{"node_id":"node"}`)},
	}
	for i := range events {
		if _, err := s.InsertEvent(&events[i]); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.GetUnpushedEvents("peer", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 {
		t.Fatalf("first unpushed count = %d, want 2", len(first))
	}
	if err := s.MarkEventsPushed("peer", first[:1], now); err != nil {
		t.Fatal(err)
	}
	second, err := s.GetUnpushedEvents("peer", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].EventID != "node:2" {
		t.Fatalf("second unpushed = %#v, want node:2 only", second)
	}
	otherPeer, err := s.GetUnpushedEvents("other-peer", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(otherPeer) != 2 {
		t.Fatalf("other peer unpushed count = %d, want 2", len(otherPeer))
	}
}
