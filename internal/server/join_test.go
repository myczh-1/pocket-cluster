package server

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/config"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func TestJoinRequiresValidInviteToken(t *testing.T) {
	cfg, st, srv := newJoinTestServer(t, "bootstrap")
	cfg.DiscoveryMode = "invite"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	_ = cfg
	defer st.Close()

	joinReq := types.JoinRequest{NodeID: "new-node", PublicKey: "pub", DeviceInfo: types.DeviceInfo{Name: "new", Address: "127.0.0.1:7789"}}
	body := mustJSON(t, joinReq)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/join/request", bytes.NewReader(body))
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("join without token status = %d, want %d", res.Code, http.StatusForbidden)
	}

	token := createInviteToken(t, srv)
	joinReq.JoinToken = token
	body = mustJSON(t, joinReq)
	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/join/request", bytes.NewReader(body))
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("join with token status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	node, err := st.GetNode(joinReq.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if !node.Trusted || node.Status != "online" {
		t.Fatalf("joined node trusted/status = %v/%s, want true/online", node.Trusted, node.Status)
	}

	joinReq.NodeID = "second-node"
	body = mustJSON(t, joinReq)
	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/join/request", bytes.NewReader(body))
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("reused token status = %d, want %d", res.Code, http.StatusForbidden)
	}
}

func TestJoinClusterViaUI(t *testing.T) {
	bootstrapCfg, bootstrapStore, bootstrapSrv := newJoinTestServer(t, "bootstrap")
	if err := bootstrapStore.UpsertNode(&types.Node{NodeID: "bootstrap", Name: "bootstrap", Address: "127.0.0.1:7788", Status: "online", TotalBytes: 2000, AvailableBytes: 1800}); err != nil {
		t.Fatal(err)
	}
	defer bootstrapStore.Close()
	bootstrapCfg.ClusterID = "test-cluster"
	if err := bootstrapCfg.Save(); err != nil {
		t.Fatal(err)
	}
	bootstrapHTTP := httptest.NewServer(bootstrapSrv.Handler())
	defer bootstrapHTTP.Close()

	token := createInviteToken(t, bootstrapSrv)

	joinerCfg, joinerStore, joinerSrv := newJoinTestServer(t, "joiner")
	if err := joinerStore.UpsertNode(&types.Node{NodeID: "joiner", Name: "joiner", Address: "127.0.0.1:7789", Status: "online", TotalBytes: 1000, AvailableBytes: 900}); err != nil {
		t.Fatal(err)
	}
	joinerCfg.ClusterID = ""
	if err := joinerCfg.Save(); err != nil {
		t.Fatal(err)
	}
	defer joinerStore.Close()
	if joinerCfg.ClusterID != "" {
		t.Fatalf("joiner already has cluster_id %q", joinerCfg.ClusterID)
	}

	body := mustJSON(t, map[string]string{"bootstrap": bootstrapHTTP.URL, "join_token": token})
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewReader(body))
	joinerSrv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("join status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}

	reloaded, err := config.Load(joinerCfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ClusterID != "test-cluster" {
		t.Fatalf("cluster_id = %q, want test-cluster", reloaded.ClusterID)
	}
	bootstrapNode, err := joinerStore.GetNode("bootstrap")
	if err != nil {
		t.Fatalf("bootstrap node not found: %v", err)
	}
	if !bootstrapNode.Trusted {
		t.Fatal("bootstrap node not trusted")
	}
}

func TestJoinClusterAutoModeAcceptsBareAddressWithoutToken(t *testing.T) {
	bootstrapCfg, bootstrapStore, bootstrapSrv := newJoinTestServer(t, "bootstrap")
	if err := bootstrapStore.UpdateNodeFull(&types.Node{
		NodeID:         "bootstrap",
		Name:           "bootstrap",
		Address:        "127.0.0.1:7788",
		PublicKey:      bootstrapCfg.PublicKey,
		Status:         "online",
		Trusted:        true,
		TotalBytes:     2000,
		AvailableBytes: 1800,
	}); err != nil {
		t.Fatal(err)
	}
	defer bootstrapStore.Close()
	bootstrapCfg.DiscoveryMode = "auto"
	bootstrapCfg.ClusterID = "auto-cluster"
	if err := bootstrapCfg.Save(); err != nil {
		t.Fatal(err)
	}
	bootstrapHTTP := httptest.NewServer(bootstrapSrv.Handler())
	defer bootstrapHTTP.Close()

	joinerCfg, joinerStore, joinerSrv := newJoinTestServer(t, "joiner")
	if err := joinerStore.UpdateNodeFull(&types.Node{
		NodeID:         "joiner",
		Name:           "joiner",
		Address:        "127.0.0.1:7789",
		PublicKey:      joinerCfg.PublicKey,
		Status:         "online",
		Trusted:        true,
		TotalBytes:     1000,
		AvailableBytes: 900,
	}); err != nil {
		t.Fatal(err)
	}
	defer joinerStore.Close()

	body := mustJSON(t, map[string]string{"bootstrap": strings.TrimPrefix(bootstrapHTTP.URL, "http://")})
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewReader(body))
	joinerSrv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("auto join status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	reloaded, err := config.Load(joinerCfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ClusterID != "auto-cluster" {
		t.Fatalf("cluster_id = %q, want auto-cluster", reloaded.ClusterID)
	}
}

func TestJoinClusterPreservesExistingNodeStatus(t *testing.T) {
	bootstrapCfg, bootstrapStore, bootstrapSrv := newJoinTestServer(t, "bootstrap")
	if err := bootstrapStore.UpdateNodeFull(&types.Node{
		NodeID:    "old-mac",
		Name:      "old mac",
		Address:   "10.8.0.10:7788",
		PublicKey: "old-key",
		Status:    "offline",
		Trusted:   true,
		LastSeen:  time.UnixMilli(1000),
	}); err != nil {
		t.Fatal(err)
	}
	defer bootstrapStore.Close()
	bootstrapCfg.ClusterID = "test-cluster"
	if err := bootstrapCfg.Save(); err != nil {
		t.Fatal(err)
	}
	bootstrapHTTP := httptest.NewServer(bootstrapSrv.Handler())
	defer bootstrapHTTP.Close()

	joinerCfg, joinerStore, joinerSrv := newJoinTestServer(t, "new-mac")
	if err := joinerStore.UpdateNodeFull(&types.Node{NodeID: "new-mac", Name: "new mac", Address: "10.8.0.11:7788", PublicKey: joinerCfg.PublicKey, Status: "online", Trusted: true}); err != nil {
		t.Fatal(err)
	}
	defer joinerStore.Close()

	body := mustJSON(t, map[string]string{"bootstrap": bootstrapHTTP.URL})
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewReader(body))
	joinerSrv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("join status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	oldMac, err := joinerStore.GetNode("old-mac")
	if err != nil {
		t.Fatal(err)
	}
	if oldMac.Status != "offline" {
		t.Fatalf("old mac status = %q, want offline", oldMac.Status)
	}
	if !oldMac.LastSeen.Equal(time.UnixMilli(1000)) {
		t.Fatalf("old mac last_seen = %s, want preserved", oldMac.LastSeen)
	}
}

func TestAddressFromRemoteKeepsAdvertisedPort(t *testing.T) {
	got := addressFromRemote("10.8.0.20:53210", "192.168.1.10:7788")
	if got != "10.8.0.20:7788" {
		t.Fatalf("address = %q, want VPN host with advertised port", got)
	}
}

func newJoinTestServer(t *testing.T, nodeID string) (*config.Config, *store.Store, *Server) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		st.Close()
		t.Fatal(err)
	}
	cfg := newTestConfig(t, nodeID)
	return cfg, st, New(cfg, st, chunks)
}

func createInviteToken(t *testing.T, srv *Server) string {
	t.Helper()
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/invites", nil)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("create invite status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var envelope types.APIResponse
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		JoinToken string `json:"join_token"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.JoinToken == "" {
		t.Fatal("empty join token")
	}
	return payload.JoinToken
}

func TestJoinRequestReplacesLoopbackAddressWithObserved(t *testing.T) {
	bootstrapCfg, bootstrapStore, bootstrapSrv := newJoinTestServer(t, "bootstrap")
	defer bootstrapStore.Close()
	bootstrapCfg.ClusterID = "test-cluster"
	bootstrapCfg.DiscoveryMode = "auto"
	if err := bootstrapCfg.Save(); err != nil {
		t.Fatal(err)
	}
	bootstrapHTTP := httptest.NewServer(bootstrapSrv.Handler())
	defer bootstrapHTTP.Close()

	joinerCfg := newTestConfig(t, "joiner")
	body := mustJSON(t, map[string]any{
		"node_id":    joinerCfg.NodeID,
		"public_key": joinerCfg.PublicKey,
		"device_info": map[string]any{
			"name":     "phone",
			"platform": "android",
			"address":  "localhost:7788",
		},
	})
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/join/request", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	bootstrapSrv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("join request status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	node, err := bootstrapStore.GetNode(joinerCfg.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	if node.Address == "localhost:7788" {
		t.Fatalf("address = %q, want non-loopback address", node.Address)
	}
	if node.Address == "" {
		t.Fatal("address is empty")
	}
	host, _, _ := net.SplitHostPort(node.Address)
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		t.Fatalf("address %q is still loopback", node.Address)
	}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
