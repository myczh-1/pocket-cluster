package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticHandlerServesDesktopWebIndex(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("root status = %d, want %d", res.Code, http.StatusOK)
	}
	if !strings.Contains(res.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("root did not serve web index: %q", res.Body.String())
	}
}

func TestStaticHandlerFallsBackToIndexForClientRoutesOnly(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	session := loginTestSession(t, srv)
	handler := srv.Handler()

	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files", nil)
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("client route status = %d, want %d", res.Code, http.StatusOK)
	}
	if !strings.Contains(res.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("client route did not serve web index: %q", res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil)
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, want %d", res.Code, http.StatusNotFound)
	}

	res = httptest.NewRecorder()
	req = withAuth(httptest.NewRequest(http.MethodGet, "/api/missing", nil), session)
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("missing api status = %d, want %d", res.Code, http.StatusNotFound)
	}
}
