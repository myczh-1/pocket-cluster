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

func TestSearchFilesUsesIndexedTokenSearch(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.UpsertFile(&types.File{FileID: "report", Name: "Quarterly Report.pdf", Path: "/docs/Quarterly Report.pdf"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertFile(&types.File{FileID: "notes", Name: "notes.txt", Path: "/docs/notes.txt"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.SearchFiles("repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].FileID != "report" {
		t.Fatalf("search repo = %#v, want report", got)
	}

	got, err = s.SearchFiles(`"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("quote-only search = %#v, want no results", got)
	}
}

func TestSearchIndexTracksRenameAndDelete(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.UpsertFile(&types.File{FileID: "file", Name: "draft.txt", Path: "/draft.txt"}); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameFile("file", "/draft.txt", "/final.txt", "node", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchFiles("final")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != "/final.txt" {
		t.Fatalf("search final = %#v, want renamed file", got)
	}
	if err := s.MarkFileDeleted("/final.txt", "node"); err != nil {
		t.Fatal(err)
	}
	got, err = s.SearchFiles("final")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("deleted file search = %#v, want no results", got)
	}
}

func TestMarkStaleNodesOffline(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	if err := s.UpsertNode(&types.Node{NodeID: "self", Status: "online", Trusted: true, LastSeen: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNode(&types.Node{NodeID: "fresh-peer", Status: "online", Trusted: true, LastSeen: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNode(&types.Node{NodeID: "stale-peer", Status: "online", Trusted: true, LastSeen: now.Add(-60 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNode(&types.Node{NodeID: "untrusted-stale", Status: "online", Trusted: false, LastSeen: now.Add(-60 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertNode(&types.Node{NodeID: "already-offline", Status: "offline", Trusted: true, LastSeen: now.Add(-60 * time.Second)}); err != nil {
		t.Fatal(err)
	}

	affected, err := s.MarkStaleNodesOffline(now.Add(-30 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if affected != 1 {
		t.Fatalf("affected = %d, want 1", affected)
	}
	n, err := s.GetNode("stale-peer")
	if err != nil {
		t.Fatal(err)
	}
	if n.Status != "offline" {
		t.Fatalf("stale-peer status = %q, want offline", n.Status)
	}
	for _, id := range []string{"self", "fresh-peer", "untrusted-stale", "already-offline"} {
		n, err := s.GetNode(id)
		if err != nil {
			t.Fatal(err)
		}
		if id == "self" || id == "fresh-peer" {
			if n.Status != "online" {
				t.Fatalf("%s status = %q, want online", id, n.Status)
			}
		}
	}
}
