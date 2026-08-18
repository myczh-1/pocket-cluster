package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func TestEvacuateNodeRejectsOfflineNode(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	srv := New(newTestConfig(t, "local"), st, chunks)
	if err := st.UpsertNode(&types.Node{NodeID: "offline", Status: "offline", Trusted: true}); err != nil {
		t.Fatal(err)
	}
	session := loginTestSession(t, srv)
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/nodes/offline/evacuate", nil), session)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusConflict, res.Body.String())
	}
	if !strings.Contains(res.Body.String(), "NODE_OFFLINE") {
		t.Fatalf("response = %s, want NODE_OFFLINE", res.Body.String())
	}
}

func TestEvacuateNodeCreatesTwoVerifiedReplacementReplicas(t *testing.T) {
	localCfg := newTestConfig(t, "leaving")
	localStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	localChunks := chunk.New(t.TempDir())
	if err := localChunks.Init(); err != nil {
		t.Fatal(err)
	}
	localSrv := New(localCfg, localStore, localChunks)

	remoteConfigs := []*types.Node{}
	remoteChunkStores := []*chunk.Storage{}
	for _, name := range []string{"peer-a", "peer-b"} {
		remoteCfg := newTestConfig(t, name)
		remoteStore, openErr := store.Open(t.TempDir())
		if openErr != nil {
			t.Fatal(openErr)
		}
		t.Cleanup(func() { remoteStore.Close() })
		remoteChunks := chunk.New(t.TempDir())
		if initErr := remoteChunks.Init(); initErr != nil {
			t.Fatal(initErr)
		}
		if nodeErr := remoteStore.UpsertNode(&types.Node{NodeID: localCfg.NodeID, PublicKey: localCfg.PublicKey, Status: "online", Trusted: true}); nodeErr != nil {
			t.Fatal(nodeErr)
		}
		remoteHTTP := httptest.NewServer(New(remoteCfg, remoteStore, remoteChunks).Handler())
		t.Cleanup(remoteHTTP.Close)
		remoteConfigs = append(remoteConfigs, &types.Node{
			NodeID:    remoteCfg.NodeID,
			Name:      name,
			Address:   strings.TrimPrefix(remoteHTTP.URL, "http://"),
			PublicKey: remoteCfg.PublicKey,
			Status:    "online",
			Trusted:   true,
		})
		remoteChunkStores = append(remoteChunkStores, remoteChunks)
	}

	now := time.Now()
	if err := localStore.UpsertNode(&types.Node{NodeID: localCfg.NodeID, Name: "leaving", Status: "online", Trusted: true}); err != nil {
		t.Fatal(err)
	}
	for _, node := range remoteConfigs {
		if err := localStore.UpsertNode(node); err != nil {
			t.Fatal(err)
		}
	}
	hash, size, err := localChunks.Store(bytes.NewReader([]byte("evacuate me")))
	if err != nil {
		t.Fatal(err)
	}
	if err := localStore.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: size, StoredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := localStore.UpsertReplica(&types.Replica{ChunkID: hash, NodeID: localCfg.NodeID, Status: "available", StoredAt: now, VerifiedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := localStore.UpsertFile(&types.File{FileID: "file", Name: "file.txt", Path: "/file.txt", ChunkIDs: []string{hash}}); err != nil {
		t.Fatal(err)
	}

	status, err := localSrv.evacuateNode(context.Background(), localCfg.NodeID, "test-job")
	if err != nil {
		t.Fatal(err)
	}
	if !status.SafeToExit {
		t.Fatalf("evacuation status = %+v, want safe_to_exit", status)
	}
	if status.SafeChunks != 1 || status.PendingChunks != 0 {
		t.Fatalf("evacuation counts = safe %d pending %d, want 1/0", status.SafeChunks, status.PendingChunks)
	}
	for i, chunks := range remoteChunkStores {
		if !chunks.Exists(hash) {
			t.Fatalf("peer %d did not receive evacuated chunk", i)
		}
	}
}
