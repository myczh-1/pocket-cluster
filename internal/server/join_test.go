package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/config"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func TestJoinWithPoolCredentialsCreatesPending(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "bootstrap")
	defer st.Close()

	joinReq := types.JoinRequest{NodeID: "new-node", PublicKey: "pub", PoolUser: "admin", PoolPassword: "testpass", DeviceInfo: types.DeviceInfo{Name: "new", Address: "127.0.0.1:7789"}}
	body := mustJSON(t, joinReq)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/join/request", bytes.NewReader(body))
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("join request status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	pending, err := st.ListPendingJoins()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].NodeID != "new-node" {
		t.Fatalf("expected 1 pending join for new-node, got %v", pending)
	}
}

func TestJoinWithWrongCredentialsRejected(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "bootstrap")
	defer st.Close()

	joinReq := types.JoinRequest{NodeID: "new-node", PublicKey: "pub", PoolUser: "admin", PoolPassword: "wrong", DeviceInfo: types.DeviceInfo{Name: "new", Address: "127.0.0.1:7789"}}
	body := mustJSON(t, joinReq)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/join/request", bytes.NewReader(body))
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("wrong creds status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestJoinWithInviteTokenCreatesPending(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "bootstrap")
	defer st.Close()

	token := createInviteToken(t, srv)
	joinReq := types.JoinRequest{NodeID: "new-node", PublicKey: "pub", JoinToken: token, DeviceInfo: types.DeviceInfo{Name: "new", Address: "127.0.0.1:7789"}}
	body := mustJSON(t, joinReq)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/join/request", bytes.NewReader(body))
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("join with token status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	pending, err := st.ListPendingJoins()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending join, got %d", len(pending))
	}
}

func TestApproveJoinAddsNode(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "bootstrap")
	defer st.Close()
	session := loginTestSession(t, srv)

	joinReq := types.JoinRequest{NodeID: "new-node", PublicKey: "pub", PoolUser: "admin", PoolPassword: "testpass", DeviceInfo: types.DeviceInfo{Name: "new", Address: "127.0.0.1:7789"}}
	body := mustJSON(t, joinReq)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/join/request", bytes.NewReader(body))
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("join request status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	pendingBeforeApprove, err := st.GetPendingJoin("new-node")
	if err != nil {
		t.Fatal(err)
	}
	if pendingBeforeApprove.ObservedAddress == "" || isLoopbackAddress(pendingBeforeApprove.ObservedAddress) {
		t.Fatalf("observed address = %q, want non-loopback address", pendingBeforeApprove.ObservedAddress)
	}

	res = httptest.NewRecorder()
	req = withAuth(httptest.NewRequest(http.MethodPost, "/api/join/approve/new-node", nil), session)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	node, err := st.GetNode("new-node")
	if err != nil {
		t.Fatal(err)
	}
	if !node.Trusted || node.Status != "online" {
		t.Fatalf("approved node trusted/status = %v/%s, want true/online", node.Trusted, node.Status)
	}
	if node.LastWorkingAddress != pendingBeforeApprove.ObservedAddress {
		t.Fatalf("last working address = %q, want observed %q", node.LastWorkingAddress, pendingBeforeApprove.ObservedAddress)
	}
	pending, _ := st.ListPendingJoins()
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending after approve, got %d", len(pending))
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
	session := loginTestSession(t, bootstrapSrv)

	joinerCfg, joinerStore, joinerSrv := newJoinTestServer(t, "joiner")
	if err := joinerStore.UpsertNode(&types.Node{NodeID: "joiner", Name: "joiner", Address: "127.0.0.1:7789", Status: "online", TotalBytes: 1000, AvailableBytes: 900}); err != nil {
		t.Fatal(err)
	}
	joinerCfg.ClusterID = ""
	joinerCfg.PoolUser = ""
	joinerCfg.PoolPassHash = ""
	if err := joinerCfg.Save(); err != nil {
		t.Fatal(err)
	}
	defer joinerStore.Close()

	// Approve in a goroutine after a short delay
	go func() {
		time.Sleep(2 * time.Second)
		res := httptest.NewRecorder()
		req := withAuth(httptest.NewRequest(http.MethodPost, "/api/join/approve/joiner", nil), session)
		bootstrapSrv.Handler().ServeHTTP(res, req)
	}()

	// This will block until approved (polling)
	joinReq := map[string]string{
		"bootstrap":     bootstrapHTTP.URL,
		"pool_user":     "admin",
		"pool_password": "testpass",
	}
	body := mustJSON(t, joinReq)
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
}

func TestJoinClusterRequiresSessionWhenConfigured(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "configured")
	defer st.Close()

	body := mustJSON(t, map[string]string{"bootstrap": "http://127.0.0.1:1", "pool_user": "admin", "pool_password": "testpass"})
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewReader(body))
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("join without session status = %d, want %d: %s", res.Code, http.StatusUnauthorized, res.Body.String())
	}
}

func TestInviteJoinPollingDoesNotReconsumeTokenAndSavesCredentials(t *testing.T) {
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
	bootstrapHTTP := httptest.NewServer(bootstrapSrv.Handler())
	defer bootstrapHTTP.Close()
	session := loginTestSession(t, bootstrapSrv)
	token := createInviteToken(t, bootstrapSrv)

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
	joinerCfg.ClusterID = ""
	joinerCfg.PoolUser = ""
	joinerCfg.PoolPassHash = ""
	if err := joinerCfg.Save(); err != nil {
		t.Fatal(err)
	}
	defer joinerStore.Close()

	go func() {
		time.Sleep(250 * time.Millisecond)
		res := httptest.NewRecorder()
		req := withAuth(httptest.NewRequest(http.MethodPost, "/api/join/approve/joiner", nil), session)
		bootstrapSrv.Handler().ServeHTTP(res, req)
	}()

	body := mustJSON(t, map[string]string{
		"bootstrap":  bootstrapHTTP.URL,
		"join_token": token,
	})
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/join", bytes.NewReader(body))
	joinerSrv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("invite join status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	reloaded, err := config.Load(joinerCfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.ClusterID != bootstrapCfg.ClusterID {
		t.Fatalf("cluster_id = %q, want %q", reloaded.ClusterID, bootstrapCfg.ClusterID)
	}
	if reloaded.PoolUser != "admin" || !reloaded.CheckPoolPassword("testpass") {
		t.Fatalf("joined node did not persist pool credentials")
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
	session := loginTestSession(t, bootstrapSrv)

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
	joinerCfg.ClusterID = ""
	joinerCfg.PoolUser = ""
	joinerCfg.PoolPassHash = ""

	// Approve in a goroutine after a short delay
	go func() {
		time.Sleep(2 * time.Second)
		res := httptest.NewRecorder()
		req := withAuth(httptest.NewRequest(http.MethodPost, "/api/join/approve/joiner", nil), session)
		bootstrapSrv.Handler().ServeHTTP(res, req)
	}()

	body := mustJSON(t, map[string]string{
		"bootstrap":     bootstrapHTTP.URL,
		"pool_user":     "admin",
		"pool_password": "testpass",
	})
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
	session := loginTestSession(t, bootstrapSrv)

	joinReq := types.JoinRequest{NodeID: "new-mac", PublicKey: "pub", PoolUser: "admin", PoolPassword: "testpass", DeviceInfo: types.DeviceInfo{Name: "new mac", Address: "10.8.0.11:7788"}}
	body := mustJSON(t, joinReq)
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/join/request", bytes.NewReader(body))
	bootstrapSrv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("join request status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = withAuth(httptest.NewRequest(http.MethodPost, "/api/join/approve/new-mac", nil), session)
	bootstrapSrv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}

	oldMac, err := bootstrapStore.GetNode("old-mac")
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
		t.Fatalf("addressFromRemote = %q, want %q", got, "10.8.0.20:7788")
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
	return cfg, st, New(cfg, st, chunks, WithJoinPollInterval(100*time.Millisecond))
}

func createInviteToken(t *testing.T, srv *Server) string {
	t.Helper()
	session := loginTestSession(t, srv)
	res := httptest.NewRecorder()
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/invites", nil), session)
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

	joinerCfg := newTestConfig(t, "joiner")
	body := mustJSON(t, map[string]any{
		"node_id":       joinerCfg.NodeID,
		"public_key":    joinerCfg.PublicKey,
		"pool_user":     "admin",
		"pool_password": "testpass",
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
	pending, err := bootstrapStore.ListPendingJoins()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].Address == "localhost:7788" {
		t.Fatalf("pending address = %q, want non-loopback", pending[0].Address)
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
