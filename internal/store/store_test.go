package store

import (
	"encoding/json"
	"fmt"
	"github.com/pocketcluster/agent/internal/types"
	"testing"
	"time"
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

func TestGetEventsSinceUsesNodeSeqOrdering(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for _, e := range []*types.Event{
		{EventID: "node-a:1", Type: types.EventNodeUpdate, NodeID: "node-a", Seq: 1, Timestamp: time.UnixMilli(1), Payload: json.RawMessage(`{}`)},
		{EventID: "node-a:2", Type: types.EventNodeUpdate, NodeID: "node-a", Seq: 2, Timestamp: time.UnixMilli(2), Payload: json.RawMessage(`{}`)},
		{EventID: "node-a:10", Type: types.EventNodeUpdate, NodeID: "node-a", Seq: 10, Timestamp: time.UnixMilli(10), Payload: json.RawMessage(`{}`)},
		{EventID: "node-b:1", Type: types.EventNodeUpdate, NodeID: "node-b", Seq: 1, Timestamp: time.UnixMilli(11), Payload: json.RawMessage(`{}`)},
	} {
		if _, err := s.InsertEvent(e); err != nil {
			t.Fatal(err)
		}
	}

	events, err := s.GetEventsSince("node-a:2", 10)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(events))
	for _, e := range events {
		got = append(got, e.EventID)
	}
	want := []string{"node-a:10", "node-b:1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("events since node-a:2 = %v, want %v", got, want)
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
func TestSaveAndLoadLatestSnapshot(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// No snapshot yet
	latest, err := s.LoadLatestSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if latest != nil {
		t.Fatal("expected nil snapshot when none saved")
	}
	// Save a snapshot
	data := []byte(`{"nodes":[],"files":[],"chunks":[],"replicas":[]}`)
	if err := s.SaveSnapshot("snap-1", "node-a", "node-a:42", data); err != nil {
		t.Fatal(err)
	}
	latest, err = s.LoadLatestSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if latest == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if latest.SnapshotID != "snap-1" {
		t.Fatalf("snapshot_id = %s, want snap-1", latest.SnapshotID)
	}
	if latest.LastEventID != "node-a:42" {
		t.Fatalf("last_event_id = %s, want node-a:42", latest.LastEventID)
	}
	// Save a second (newer) snapshot
	data2 := []byte(`{"nodes":[],"files":[],"chunks":[],"replicas":[]}`)
	if err := s.SaveSnapshot("snap-2", "node-a", "node-a:100", data2); err != nil {
		t.Fatal(err)
	}
	latest, err = s.LoadLatestSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if latest.SnapshotID != "snap-2" {
		t.Fatalf("snapshot_id = %s, want snap-2", latest.SnapshotID)
	}
}
func TestEventCountAndPrune(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Insert 5 events
	for i := 1; i <= 5; i++ {
		e := &types.Event{
			EventID:   fmt.Sprintf("node-a:%d", i),
			Type:      types.EventFilePut,
			NodeID:    "node-a",
			Seq:       int64(i),
			Timestamp: time.Now(),
			Payload:   json.RawMessage(`{}`),
		}
		if _, err := s.InsertEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	count, err := s.EventCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("event count = %d, want 5", count)
	}
	// Prune events up to node-a:3
	pruned, err := s.PruneEventsBefore("node-a:3")
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 3 {
		t.Fatalf("pruned = %d, want 3", pruned)
	}
	count, err = s.EventCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("event count after prune = %d, want 2", count)
	}
	// Remaining events should be node-a:4 and node-a:5
	events, err := s.GetEventsSince("", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("remaining events = %d, want 2", len(events))
	}
	if events[0].EventID != "node-a:4" || events[1].EventID != "node-a:5" {
		t.Fatalf("remaining events = %s, %s; want node-a:4, node-a:5", events[0].EventID, events[1].EventID)
	}
}
func TestLoadSnapshot(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	snap := &MetadataSnapshot{
		LastEventID: "node-a:10",
		Nodes: []types.Node{{
			NodeID:   "node-a",
			Name:     "Test Node",
			Platform: "linux",
			Address:  "192.168.1.10:7788",
			Status:   "online",
			Trusted:  true,
			LastSeen: now,
			JoinedAt: now,
		}},
		Files: []types.File{{
			FileID:     "file-1",
			Name:       "test.txt",
			Path:       "/test.txt",
			SizeBytes:  1024,
			VersionID:  "v1",
			CreatedAt:  now,
			ModifiedAt: now,
		}},
		Chunks: []types.Chunk{{
			ChunkID:   "abc123",
			SizeBytes: 1024,
			StoredAt:  now,
		}},
		Replicas: []types.Replica{{
			ChunkID:    "abc123",
			NodeID:     "node-a",
			Status:     "available",
			StoredAt:   now,
			VerifiedAt: now,
		}},
	}
	if err := s.LoadSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	// Verify node was loaded
	node, err := s.GetNode("node-a")
	if err != nil {
		t.Fatal(err)
	}
	if node.Name != "Test Node" {
		t.Fatalf("node name = %s, want Test Node", node.Name)
	}
	// Verify file was loaded
	file, err := s.GetFile("/test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if file.Name != "test.txt" {
		t.Fatalf("file name = %s, want test.txt", file.Name)
	}
	// Verify chunk was loaded
	chunk, err := s.GetChunk("abc123")
	if err != nil {
		t.Fatal(err)
	}
	if chunk.SizeBytes != 1024 {
		t.Fatalf("chunk size = %d, want 1024", chunk.SizeBytes)
	}
	// Verify replica was loaded
	replicas, err := s.GetReplicas("abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(replicas) != 1 {
		t.Fatalf("replica count = %d, want 1", len(replicas))
	}
	if replicas[0].NodeID != "node-a" {
		t.Fatalf("replica node = %s, want node-a", replicas[0].NodeID)
	}
}
func TestLoadSnapshotRebuildsFTSIndex(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now()
	snap := &MetadataSnapshot{
		LastEventID: "node-a:5",
		Nodes:       []types.Node{},
		Files: []types.File{{
			FileID:     "file-1",
			Name:       "important-document.pdf",
			Path:       "/docs/important-document.pdf",
			SizeBytes:  4096,
			VersionID:  "v1",
			CreatedAt:  now,
			ModifiedAt: now,
		}, {
			FileID:     "file-2",
			Name:       "photo.jpg",
			Path:       "/photos/photo.jpg",
			SizeBytes:  8192,
			VersionID:  "v2",
			CreatedAt:  now,
			ModifiedAt: now,
		}, {
			FileID:     "file-3",
			Name:       "deleted.txt",
			Path:       "/deleted.txt",
			SizeBytes:  100,
			VersionID:  "v3",
			Deleted:    true,
			CreatedAt:  now,
			ModifiedAt: now,
		}},
		Chunks:   []types.Chunk{},
		Replicas: []types.Replica{},
	}
	if err := s.LoadSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	// Search should find non-deleted files
	results, err := s.SearchFiles("important")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("search 'important': got %d results, want 1", len(results))
	}
	if results[0].Name != "important-document.pdf" {
		t.Fatalf("search result name = %s, want important-document.pdf", results[0].Name)
	}
	results, err = s.SearchFiles("photo")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("search 'photo': got %d results, want 1", len(results))
	}
	// Deleted files should not appear in search
	results, err = s.SearchFiles("deleted")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("search 'deleted': got %d results, want 0", len(results))
	}
}

func TestAddColumnIfMissingRejectsUnsafeSQL(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if err := s.addColumnIfMissing("nodes; DROP TABLE files", "safe", "TEXT NOT NULL DEFAULT ''"); err == nil {
		t.Fatal("unsafe table identifier was accepted")
	}
	if err := s.addColumnIfMissing("nodes", "safe; DROP TABLE files", "TEXT NOT NULL DEFAULT ''"); err == nil {
		t.Fatal("unsafe column identifier was accepted")
	}
	if err := s.addColumnIfMissing("nodes", "safe_column", "TEXT DEFAULT (randomblob(1024))"); err == nil {
		t.Fatal("unsafe column definition was accepted")
	}
}
