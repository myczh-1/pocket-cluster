package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/config"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func TestJoinRequiresValidInviteToken(t *testing.T) {
	cfg, st, srv := newJoinTestServer(t, "bootstrap")
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

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
