package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestListLocalFilesReturnsEntries(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("world"), 0o644)
	os.Mkdir(filepath.Join(dir, "subdir"), 0o755)

	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	session := loginTestSession(t, srv)

	res := httptest.NewRecorder()
	req := withAuth(httptest.NewRequest(http.MethodGet, "/api/local/files?path="+dir, nil), session)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			Cwd     string `json:"cwd"`
			Parent  string `json:"parent"`
			Entries []struct {
				Name  string `json:"name"`
				IsDir bool   `json:"is_dir"`
			} `json:"entries"`
		} `json:"data"`
	}
	json.NewDecoder(res.Body).Decode(&body)
	if !body.OK {
		t.Fatal("ok = false")
	}
	if body.Data.Cwd != dir {
		t.Fatalf("cwd = %q, want %q", body.Data.Cwd, dir)
	}
	if len(body.Data.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(body.Data.Entries))
	}
}

func TestMigrateLocalFileKeepsLocalWhenReplicationIncomplete(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "migrate-me.txt")
	os.WriteFile(src, []byte("hello pool"), 0o644)
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	session := loginTestSession(t, srv)

	body, _ := json.Marshal(map[string]any{
		"path":         src,
		"target_path":  "/migrated.txt",
		"delete_local": true,
	})
	res := httptest.NewRecorder()
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/local/migrate", bytes.NewReader(body)), session)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusConflict, res.Body.String())
	}
	var resp struct {
		OK    bool `json:"ok"`
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.NewDecoder(res.Body).Decode(&resp)
	if resp.OK {
		t.Fatal("ok = true")
	}
	if resp.Error == nil || resp.Error.Code != "REPLICATION_INCOMPLETE" {
		t.Fatalf("error = %#v, want REPLICATION_INCOMPLETE", resp.Error)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("local file should have been kept: %v", err)
	}
}
