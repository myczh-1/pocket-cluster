package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func newTestHealthServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	srv := New(newTestConfig(t, "selfNode"), st, chunks)
	srv.health = newHealthScanner()
	srv.health.skipRemoteVerify = true
	return srv
}

func TestHealthScanDetectsUnderReplicated(t *testing.T) {
	s := newTestHealthServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Insert a node
	node := &types.Node{
		NodeID:   "nodeA",
		Name:     "Node A",
		Platform: "linux",
		Address:  "192.168.1.10:7788",
		Status:   "online",
		Trusted:  true,
		LastSeen: time.Now(),
	}
	if err := s.store.UpdateNodeFull(node); err != nil {
		t.Fatal(err)
	}

	// Insert a file with one chunk
	chunkID := "abc123"
	if err := s.store.UpsertChunk(&types.Chunk{ChunkID: chunkID, SizeBytes: 1024, StoredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.UpsertFile(&types.File{
		FileID:     "f1",
		Name:       "test.txt",
		Path:       "/test.txt",
		SizeBytes:  1024,
		VersionID:  "v1",
		ChunkIDs:   []string{chunkID},
		CreatedAt:  time.Now(),
		ModifiedAt: time.Now(),
		ModifiedBy: "nodeA",
	}); err != nil {
		t.Fatal(err)
	}

	// Insert only one replica (below target of 2)
	if err := s.store.UpsertReplica(&types.Replica{
		ChunkID:    chunkID,
		NodeID:     "nodeA",
		Status:     "available",
		StoredAt:   time.Now(),
		VerifiedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	// Run scan
	s.runHealthScan(ctx)

	// Check results
	summary := s.HealthSummarySnapshot()
	if summary.TotalFiles != 1 {
		t.Errorf("expected 1 total file, got %d", summary.TotalFiles)
	}
	if summary.TotalChunks != 1 {
		t.Errorf("expected 1 total chunk, got %d", summary.TotalChunks)
	}
	if summary.UnderReplicated != 1 {
		t.Errorf("expected 1 under-replicated chunk, got %d", summary.UnderReplicated)
	}
	if summary.OverallStatus != types.ReplicaUnderReplicated {
		t.Errorf("expected overall status under_replicated, got %s", summary.OverallStatus)
	}

	// Check chunk health detail
	chunks := s.ChunkHealthSnapshot()
	detail, ok := chunks[chunkID]
	if !ok {
		t.Fatal("chunk not found in health snapshot")
	}
	if detail.OnlineReplicas != 1 {
		t.Errorf("expected 1 online replica, got %d", detail.OnlineReplicas)
	}
	if detail.TargetReplicas != 2 {
		t.Errorf("expected target replicas 2, got %d", detail.TargetReplicas)
	}
	if detail.Status != types.ReplicaUnderReplicated {
		t.Errorf("expected chunk status under_replicated, got %s", detail.Status)
	}
}

func TestHealthInsightsReportsEfficiencyAndRisk(t *testing.T) {
	s := newTestHealthServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := s.store.UpdateNodeFull(&types.Node{
		NodeID:   "nodeA",
		Name:     "Node A",
		Platform: "linux",
		Address:  "127.0.0.1:7788",
		Status:   "online",
		Trusted:  true,
		LastSeen: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.UpsertChunk(&types.Chunk{ChunkID: "shared", SizeBytes: 100, StoredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []types.File{
		{FileID: "f1", Name: "a.bin", Path: "/a.bin", SizeBytes: 100, VersionID: "v1", ChunkIDs: []string{"shared"}, CreatedAt: time.Now(), ModifiedAt: time.Now(), ModifiedBy: "nodeA"},
		{FileID: "f2", Name: "b.bin", Path: "/b.bin", SizeBytes: 100, VersionID: "v2", ChunkIDs: []string{"shared"}, CreatedAt: time.Now(), ModifiedAt: time.Now(), ModifiedBy: "nodeA"},
	} {
		if err := s.store.UpsertFile(&f); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.store.UpsertReplica(&types.Replica{
		ChunkID:    "shared",
		NodeID:     "nodeA",
		Status:     "available",
		StoredAt:   time.Now(),
		VerifiedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	s.runHealthScan(ctx)

	session := loginTestSession(t, s)
	res := httptest.NewRecorder()
	req := withAuth(httptest.NewRequest(http.MethodGet, "/api/health/insights", nil), session)
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("insights status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var envelope types.APIResponse
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Storage struct {
			FileCount        int   `json:"file_count"`
			LogicalBytes     int64 `json:"logical_bytes"`
			UniqueChunkBytes int64 `json:"unique_chunk_bytes"`
			DedupSavedBytes  int64 `json:"dedup_saved_bytes"`
		} `json:"storage"`
		Risk struct {
			AffectedFileCount int      `json:"affected_file_count"`
			AffectedFiles     []string `json:"affected_files"`
		} `json:"risk"`
		Repair struct {
			Status       string `json:"status"`
			QueuedChunks int    `json:"queued_chunks"`
		} `json:"repair"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Storage.FileCount != 2 || payload.Storage.LogicalBytes != 200 || payload.Storage.UniqueChunkBytes != 100 || payload.Storage.DedupSavedBytes != 100 {
		t.Fatalf("unexpected storage insights: %+v", payload.Storage)
	}
	if payload.Risk.AffectedFileCount != 2 {
		t.Fatalf("affected file count = %d, want 2 (%+v)", payload.Risk.AffectedFileCount, payload.Risk.AffectedFiles)
	}
	if payload.Repair.Status != "queued" || payload.Repair.QueuedChunks != 1 {
		t.Fatalf("unexpected repair insight: %+v", payload.Repair)
	}
}

func TestHealthScanHealthyChunks(t *testing.T) {
	s := newTestHealthServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Insert two nodes
	for _, id := range []string{"nodeA", "nodeB"} {
		if err := s.store.UpdateNodeFull(&types.Node{
			NodeID:   id,
			Name:     "Node " + id,
			Platform: "linux",
			Address:  "192.168.1.10:7788",
			Status:   "online",
			Trusted:  true,
			LastSeen: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	chunkID := "def456"
	if err := s.store.UpsertChunk(&types.Chunk{ChunkID: chunkID, SizeBytes: 2048, StoredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.UpsertFile(&types.File{
		FileID:     "f2",
		Name:       "test2.txt",
		Path:       "/test2.txt",
		SizeBytes:  2048,
		VersionID:  "v2",
		ChunkIDs:   []string{chunkID},
		CreatedAt:  time.Now(),
		ModifiedAt: time.Now(),
		ModifiedBy: "nodeA",
	}); err != nil {
		t.Fatal(err)
	}

	// Insert 2 replicas (meets target)
	for _, nodeID := range []string{"nodeA", "nodeB"} {
		if err := s.store.UpsertReplica(&types.Replica{
			ChunkID:    chunkID,
			NodeID:     nodeID,
			Status:     "available",
			StoredAt:   time.Now(),
			VerifiedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	s.runHealthScan(ctx)

	summary := s.HealthSummarySnapshot()
	if summary.HealthyChunks != 1 {
		t.Errorf("expected 1 healthy chunk, got %d", summary.HealthyChunks)
	}
	if summary.OverallStatus != types.ReplicaHealthy {
		t.Errorf("expected overall status healthy, got %s", summary.OverallStatus)
	}
}

func TestFileHealthDetail(t *testing.T) {
	s := newTestHealthServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup
	if err := s.store.UpdateNodeFull(&types.Node{
		NodeID: "nodeA", Name: "A", Platform: "linux", Address: "192.168.1.10:7788",
		Status: "online", Trusted: true, LastSeen: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	chnks := []string{"c1", "c2"}
	for _, cid := range chnks {
		if err := s.store.UpsertChunk(&types.Chunk{ChunkID: cid, SizeBytes: 1024, StoredAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if err := s.store.UpsertReplica(&types.Replica{
			ChunkID: cid, NodeID: "nodeA", Status: "available",
			StoredAt: time.Now(), VerifiedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.store.UpsertFile(&types.File{
		FileID: "f3", Name: "multi.txt", Path: "/multi.txt", SizeBytes: 2048,
		VersionID: "v3", ChunkIDs: chnks, CreatedAt: time.Now(),
		ModifiedAt: time.Now(), ModifiedBy: "nodeA",
	}); err != nil {
		t.Fatal(err)
	}

	s.runHealthScan(ctx)

	detail, err := s.FileHealth("f3")
	if err != nil {
		t.Fatal(err)
	}
	if detail.FileID != "f3" {
		t.Errorf("expected file ID f3, got %s", detail.FileID)
	}
	if detail.ChunkCount != 2 {
		t.Errorf("expected 2 chunks, got %d", detail.ChunkCount)
	}
	if len(detail.Chunks) != 2 {
		t.Errorf("expected 2 chunk details, got %d", len(detail.Chunks))
	}
}

func TestPurgeFile(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now()
	if err := st.UpsertFile(&types.File{
		FileID: "purge1", Name: "old.txt", Path: "/old.txt",
		SizeBytes: 100, VersionID: "v1", CreatedAt: now,
		ModifiedAt: now, ModifiedBy: "nodeA",
	}); err != nil {
		t.Fatal(err)
	}

	// Mark deleted
	if err := st.MarkFileDeleted("/old.txt", "nodeA"); err != nil {
		t.Fatal(err)
	}

	// Verify it's marked deleted
	files, err := st.ListAllFilesIncludingDeleted()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if f.FileID == "purge1" && f.Deleted {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected to find deleted file")
	}

	// Purge
	if err := st.PurgeFile("purge1"); err != nil {
		t.Fatal(err)
	}

	// Verify it's gone
	files, err = st.ListAllFilesIncludingDeleted()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.FileID == "purge1" {
			t.Fatal("expected file to be purged")
		}
	}
}
