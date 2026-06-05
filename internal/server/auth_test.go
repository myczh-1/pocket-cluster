package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketcluster/agent/internal/types"
)

func TestPeerEventEndpointRequiresSignature(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "receiver")
	defer st.Close()

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned peer request status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestPeerEventEndpointAcceptsTrustedSignature(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "receiver")
	defer st.Close()
	signer := newTestConfig(t, "trusted")
	if err := st.UpsertNode(&types.Node{NodeID: signer.NodeID, PublicKey: signer.PublicKey, Status: "online", Trusted: true}); err != nil {
		t.Fatal(err)
	}

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
	privateKey, err := signer.Ed25519PrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	sig, ts := SignRequest(privateKey, req.Method, req.URL.RequestURI(), emptyBodySHA256, signer.NodeID)
	req.Header.Set("X-Node-ID", signer.NodeID)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set(authBodySHA256Header, emptyBodySHA256)

	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("signed peer request status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
}

func TestLogoutRevokesServerSession(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "receiver")
	defer st.Close()
	session := loginTestSession(t, srv)

	res := httptest.NewRecorder()
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil), session)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = withAuth(httptest.NewRequest(http.MethodGet, "/api/node/info", nil), session)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("reused session status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}
