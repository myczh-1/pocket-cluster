package server

import (
	"bytes"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func TestFilesWithReplicaStatusTracksReplicaReadiness(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	cfg := newTestConfig(t, "local")
	srv := New(cfg, st, chunks)

	hash, size, err := chunks.Store(bytes.NewReader([]byte("ready")))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := st.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: size, StoredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertReplica(&types.Replica{ChunkID: hash, NodeID: cfg.NodeID, Status: "available", StoredAt: now, VerifiedAt: now}); err != nil {
		t.Fatal(err)
	}
	file := types.File{FileID: "file", Name: "ready.txt", Path: "/ready.txt", ChunkIDs: []string{hash}}

	entries := srv.filesWithReplicaStatus([]types.File{file})
	if got := entries[0].ReplicaStatus; got != types.ReplicaUnderReplicated {
		t.Fatalf("replica status = %s, want %s", got, types.ReplicaUnderReplicated)
	}

	if err := st.UpsertNode(&types.Node{NodeID: "peer", Status: "online", Trusted: true}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertReplica(&types.Replica{ChunkID: hash, NodeID: "peer", Status: "available", StoredAt: now, VerifiedAt: now}); err != nil {
		t.Fatal(err)
	}
	entries = srv.filesWithReplicaStatus([]types.File{file})
	if got := entries[0].ReplicaStatus; got != types.ReplicaHealthy {
		t.Fatalf("replica status = %s, want %s", got, types.ReplicaHealthy)
	}
}
