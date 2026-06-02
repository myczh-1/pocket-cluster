package server

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/config"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func TestFetchMissingChunksCopiesReplicaAndPublishesLocalReplica(t *testing.T) {
	content := []byte("replicated chunk payload")

	remoteStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer remoteStore.Close()
	remoteChunks := chunk.New(t.TempDir())
	if err := remoteChunks.Init(); err != nil {
		t.Fatal(err)
	}
	remoteCfg := &config.Config{NodeID: "remote", ClusterID: "cluster"}
	remoteSrv := New(remoteCfg, remoteStore, remoteChunks)
	hash, size, err := remoteChunks.Store(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := remoteStore.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: size, StoredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := remoteStore.UpsertReplica(&types.Replica{ChunkID: hash, NodeID: remoteCfg.NodeID, Status: "available", StoredAt: now, VerifiedAt: now}); err != nil {
		t.Fatal(err)
	}
	remoteHTTP := httptest.NewServer(remoteSrv.Handler())
	defer remoteHTTP.Close()

	localStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	localChunks := chunk.New(t.TempDir())
	if err := localChunks.Init(); err != nil {
		t.Fatal(err)
	}
	localCfg := &config.Config{NodeID: "local", ClusterID: "cluster"}
	localSrv := New(localCfg, localStore, localChunks)
	if err := localStore.UpsertNode(&types.Node{NodeID: remoteCfg.NodeID, Address: strings.TrimPrefix(remoteHTTP.URL, "http://"), Status: "online", Trusted: true}); err != nil {
		t.Fatal(err)
	}
	if err := localStore.UpsertFile(&types.File{FileID: "file", Name: "file.txt", Path: "/file.txt", ChunkIDs: []string{hash}, ModifiedBy: remoteCfg.NodeID}); err != nil {
		t.Fatal(err)
	}
	if err := localStore.UpsertReplica(&types.Replica{ChunkID: hash, NodeID: remoteCfg.NodeID, Status: "available", StoredAt: now, VerifiedAt: now}); err != nil {
		t.Fatal(err)
	}

	if err := localSrv.fetchMissingChunks(context.Background()); err != nil {
		t.Fatal(err)
	}
	f, _, err := localChunks.Open(hash)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, len(content))
	if _, err := io.ReadFull(f, buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf, content) {
		t.Fatalf("local chunk content = %q, want %q", buf, content)
	}
	replicas, err := localStore.GetReplicas(hash)
	if err != nil {
		t.Fatal(err)
	}
	foundLocal := false
	for _, replica := range replicas {
		if replica.NodeID == localCfg.NodeID && replica.Status == "available" {
			foundLocal = true
		}
	}
	if !foundLocal {
		t.Fatal("local replica metadata was not recorded")
	}
}

func TestRepairChunkReplicasPushesLocalChunkToTrustedPeer(t *testing.T) {
	content := []byte("chunk needing a second replica")

	remoteStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer remoteStore.Close()
	remoteChunks := chunk.New(t.TempDir())
	if err := remoteChunks.Init(); err != nil {
		t.Fatal(err)
	}
	remoteCfg := &config.Config{NodeID: "remote", ClusterID: "cluster"}
	remoteSrv := New(remoteCfg, remoteStore, remoteChunks)
	remoteHTTP := httptest.NewServer(remoteSrv.Handler())
	defer remoteHTTP.Close()

	localStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	localChunks := chunk.New(t.TempDir())
	if err := localChunks.Init(); err != nil {
		t.Fatal(err)
	}
	localCfg := &config.Config{NodeID: "local", ClusterID: "cluster"}
	localSrv := New(localCfg, localStore, localChunks)
	hash, size, err := localChunks.Store(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := localStore.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: size, StoredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := localStore.UpsertReplica(&types.Replica{ChunkID: hash, NodeID: localCfg.NodeID, Status: "available", StoredAt: now, VerifiedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := localStore.UpsertNode(&types.Node{NodeID: remoteCfg.NodeID, Address: strings.TrimPrefix(remoteHTTP.URL, "http://"), Status: "online", Trusted: true}); err != nil {
		t.Fatal(err)
	}
	nodes, err := localStore.ListNodes()
	if err != nil {
		t.Fatal(err)
	}

	if err := localSrv.repairChunkReplicas(context.Background(), hash, nodes); err != nil {
		t.Fatal(err)
	}
	if !remoteChunks.Exists(hash) {
		t.Fatal("remote peer did not store pushed chunk")
	}
	replicas, err := localStore.GetReplicas(hash)
	if err != nil {
		t.Fatal(err)
	}
	available := availableReplicaNodes(replicas)
	if _, ok := available[localCfg.NodeID]; !ok {
		t.Fatal("local replica missing after repair")
	}
	if _, ok := available[remoteCfg.NodeID]; !ok {
		t.Fatal("remote replica missing after repair")
	}
	if status := localSrv.replicaStatusForChunks([]string{hash}); status != types.ReplicaHealthy {
		t.Fatalf("replica status = %s, want %s", status, types.ReplicaHealthy)
	}
}

func unusedTCPAddress() (string, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", err
	}
	return address, nil
}

func TestSyncOnceRefreshesResponsivePeerLastSeen(t *testing.T) {
	remoteStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer remoteStore.Close()
	remoteChunks := chunk.New(t.TempDir())
	if err := remoteChunks.Init(); err != nil {
		t.Fatal(err)
	}
	remoteCfg := &config.Config{NodeID: "remote", ClusterID: "cluster"}
	remoteHTTP := httptest.NewServer(New(remoteCfg, remoteStore, remoteChunks).Handler())
	defer remoteHTTP.Close()

	localStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	localChunks := chunk.New(t.TempDir())
	if err := localChunks.Init(); err != nil {
		t.Fatal(err)
	}
	oldSeen := time.Now().Add(-nodeOfflineAfter - time.Second)
	if err := localStore.UpsertNode(&types.Node{NodeID: remoteCfg.NodeID, Address: strings.TrimPrefix(remoteHTTP.URL, "http://"), Status: "online", Trusted: true, LastSeen: oldSeen}); err != nil {
		t.Fatal(err)
	}
	localSrv := New(&config.Config{NodeID: "local", ClusterID: "cluster"}, localStore, localChunks)

	if err := localSrv.SyncOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	remoteNode, err := localStore.GetNode(remoteCfg.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if remoteNode.Status != "online" {
		t.Fatalf("status = %q, want online", remoteNode.Status)
	}
	if !remoteNode.LastSeen.After(oldSeen) {
		t.Fatalf("last_seen = %s, want after %s", remoteNode.LastSeen, oldSeen)
	}
}

func TestSyncOnceMarksStaleFailedPeerOffline(t *testing.T) {
	localStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	localChunks := chunk.New(t.TempDir())
	if err := localChunks.Init(); err != nil {
		t.Fatal(err)
	}
	address, err := unusedTCPAddress()
	if err != nil {
		t.Fatal(err)
	}
	oldSeen := time.Now().Add(-nodeOfflineAfter - time.Second)
	if err := localStore.UpsertNode(&types.Node{NodeID: "stale-peer", Address: address, Status: "online", Trusted: true, LastSeen: oldSeen}); err != nil {
		t.Fatal(err)
	}
	localSrv := New(&config.Config{NodeID: "local", ClusterID: "cluster"}, localStore, localChunks)

	if err := localSrv.SyncOnce(context.Background()); err == nil {
		t.Fatal("SyncOnce succeeded; expected failed peer error")
	}
	peer, err := localStore.GetNode("stale-peer")
	if err != nil {
		t.Fatal(err)
	}
	if peer.Status != "offline" {
		t.Fatalf("status = %q, want offline", peer.Status)
	}
}

func TestReplicaStatusIgnoresOfflineReplicaNodes(t *testing.T) {
	localStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	localChunks := chunk.New(t.TempDir())
	if err := localChunks.Init(); err != nil {
		t.Fatal(err)
	}
	localSrv := New(&config.Config{NodeID: "local", ClusterID: "cluster"}, localStore, localChunks)
	now := time.Now()
	const chunkID = "chunk"
	if err := localStore.UpsertReplica(&types.Replica{ChunkID: chunkID, NodeID: "local", Status: "available", StoredAt: now, VerifiedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := localStore.UpsertReplica(&types.Replica{ChunkID: chunkID, NodeID: "remote", Status: "available", StoredAt: now, VerifiedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := localStore.UpsertNode(&types.Node{NodeID: "remote", Status: "offline", Trusted: true, LastSeen: now.Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}

	if status := localSrv.replicaStatusForChunks([]string{chunkID}); status != types.ReplicaUnderReplicated {
		t.Fatalf("replica status = %s, want %s", status, types.ReplicaUnderReplicated)
	}
}

func TestPullEventsIgnoresEnvironmentHTTPProxy(t *testing.T) {
	ip, ok := nonLoopbackIPv4()
	if !ok {
		t.Skip("no non-loopback IPv4 address available")
	}

	var proxyHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		http.Error(w, "proxy should not handle peer sync", http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("NO_PROXY", "")
	t.Setenv("no_proxy", "")

	remoteStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer remoteStore.Close()
	remoteChunks := chunk.New(t.TempDir())
	if err := remoteChunks.Init(); err != nil {
		t.Fatal(err)
	}
	remoteCfg := &config.Config{NodeID: "remote", ClusterID: "cluster"}
	remoteSrv := New(remoteCfg, remoteStore, remoteChunks)
	if _, err := remoteSrv.appendEvent(types.EventNodeUpdate, &types.Node{NodeID: remoteCfg.NodeID, Address: "remote-address", Status: "online"}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort(ip.String(), "0"))
	if err != nil {
		t.Skipf("non-loopback IPv4 %s is not listenable: %v", ip, err)
	}
	remoteHTTP := httptest.NewUnstartedServer(remoteSrv.Handler())
	remoteHTTP.Listener = listener
	remoteHTTP.Start()
	defer remoteHTTP.Close()

	localStore, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer localStore.Close()
	localChunks := chunk.New(t.TempDir())
	if err := localChunks.Init(); err != nil {
		t.Fatal(err)
	}
	localSrv := New(&config.Config{NodeID: "local", ClusterID: "cluster"}, localStore, localChunks)

	remoteNode := types.Node{NodeID: remoteCfg.NodeID, Address: strings.TrimPrefix(remoteHTTP.URL, "http://"), Status: "online", Trusted: true}
	if err := localSrv.pullEvents(context.Background(), remoteNode); err != nil {
		t.Fatal(err)
	}
	if proxyHits.Load() != 0 {
		t.Fatalf("peer sync used environment HTTP proxy %d time(s)", proxyHits.Load())
	}
	if _, err := localStore.GetNode(remoteCfg.NodeID); err != nil {
		t.Fatalf("remote event was not applied: %v", err)
	}
}

func nonLoopbackIPv4() (net.IP, bool) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, false
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ipv4 := ip.To4(); ipv4 != nil {
				return ipv4, true
			}
		}
	}
	return nil, false
}
