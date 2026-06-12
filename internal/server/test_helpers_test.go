package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketcluster/agent/internal/config"
)

func newTestConfig(t *testing.T, nodeID string) *config.Config {
	t.Helper()
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.NodeID = nodeID
	cfg.ClusterID = "cluster"
	cfg.Name = nodeID
	cfg.Platform = "test"
	if err := cfg.SetPoolCredentials("admin", "testpass"); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func loginTestSession(t *testing.T, srv *Server) string {
	t.Helper()
	res := httptest.NewRecorder()
	body := `{"username":"admin","password":"testpass"}`
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(res, req)
	if res.Code != 200 {
		t.Fatalf("login status = %d: %s", res.Code, res.Body.String())
	}
	for _, c := range res.Result().Cookies() {
		if c.Name == "pc-session" {
			return c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}


func withAuth(req *http.Request, session string) *http.Request {
	if session != "" {
		req.AddCookie(&http.Cookie{Name: "pc-session", Value: session})
	}
	return req
}