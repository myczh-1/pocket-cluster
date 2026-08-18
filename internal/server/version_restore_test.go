package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func TestFileVersionAPIListsAndRestoresSupersededContent(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	handler := srv.Handler()

	putDAVVersion(t, handler, "/history.txt", "first", "")
	first, err := st.GetFile("/history.txt")
	if err != nil {
		t.Fatal(err)
	}
	putDAVVersion(t, handler, "/history.txt", "second", first.VersionID)
	current, err := st.GetFile("/history.txt")
	if err != nil {
		t.Fatal(err)
	}

	session := loginTestSession(t, srv)
	listRes := httptest.NewRecorder()
	handler.ServeHTTP(listRes, withAuth(httptest.NewRequest(http.MethodGet, "/api/files/versions?file_id="+current.FileID, nil), session))
	if listRes.Code != http.StatusOK {
		t.Fatalf("list versions status = %d: %s", listRes.Code, listRes.Body.String())
	}
	var listEnvelope struct {
		Data struct {
			Entries []struct {
				VersionID string `json:"version_id"`
			} `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRes.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(listEnvelope.Data.Entries) != 1 || listEnvelope.Data.Entries[0].VersionID != first.VersionID {
		t.Fatalf("history entries = %+v", listEnvelope.Data.Entries)
	}

	restoreRes := httptest.NewRecorder()
	restoreBody := `{"file_id":"` + current.FileID + `","version_id":"` + first.VersionID + `"}`
	restoreReq := withAuth(httptest.NewRequest(http.MethodPost, "/api/files/versions/restore", strings.NewReader(restoreBody)), session)
	restoreReq.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(restoreRes, restoreReq)
	if restoreRes.Code != http.StatusOK {
		t.Fatalf("restore version status = %d: %s", restoreRes.Code, restoreRes.Body.String())
	}
	if got := string(readWebDAVFile(t, handler, "/dav/history.txt")); got != "first" {
		t.Fatalf("restored content = %q", got)
	}
	restored, err := st.GetFile("/history.txt")
	if err != nil {
		t.Fatal(err)
	}
	if restored.VersionID == first.VersionID || restored.ParentVersionID != current.VersionID {
		t.Fatalf("restored version lineage = %+v", restored)
	}
}

func TestCleanupExpiresSupersededVersionChunks(t *testing.T) {
	cfg, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	cfg.SetTombstoneRetentionHours(1)
	hash, size, err := srv.chunks.Store(strings.NewReader("expired content"))
	if err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-3 * time.Hour)
	supersededAt := time.Now().Add(-2 * time.Hour)
	if err := st.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: size, StoredAt: oldTime}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertReplica(&types.Replica{ChunkID: hash, NodeID: cfg.NodeID, Status: "available", StoredAt: oldTime, VerifiedAt: oldTime}); err != nil {
		t.Fatal(err)
	}
	base := &types.File{FileID: "file", Name: "old.txt", Path: "/old.txt", VersionID: "old", ChunkIDs: []string{hash}, CreatedAt: oldTime, ModifiedAt: oldTime, ModifiedBy: cfg.NodeID}
	current := &types.File{FileID: "file", Name: "old.txt", Path: "/old.txt", VersionID: "current", ParentVersionID: "old", CreatedAt: oldTime, ModifiedAt: supersededAt, ModifiedBy: cfg.NodeID}
	for _, version := range []*types.File{base, current} {
		if err := st.RecordFileVersion(version); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.UpsertFile(current); err != nil {
		t.Fatal(err)
	}
	if err := srv.CleanupTombstonesContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if srv.chunks.Exists(hash) {
		t.Fatalf("expired historical chunk %s still exists", hash)
	}
}

func TestFileVersionRestoreRejectsUnavailableContentWithoutMutation(t *testing.T) {
	cfg, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	now := time.Now()
	base := &types.File{
		FileID: "file", Name: "missing.txt", Path: "/missing.txt", VersionID: "old", ChunkIDs: []string{"missing-chunk"},
		CreatedAt: now.Add(-time.Hour), ModifiedAt: now.Add(-time.Hour), ModifiedBy: cfg.NodeID,
	}
	current := &types.File{
		FileID: "file", Name: "missing.txt", Path: "/missing.txt", VersionID: "current", ParentVersionID: "old",
		CreatedAt: base.CreatedAt, ModifiedAt: now, ModifiedBy: cfg.NodeID,
	}
	for _, version := range []*types.File{base, current} {
		if err := st.RecordFileVersion(version); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.UpsertFile(current); err != nil {
		t.Fatal(err)
	}
	session := loginTestSession(t, srv)
	res := httptest.NewRecorder()
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/files/versions/restore", strings.NewReader(`{"file_id":"file","version_id":"old"}`)), session)
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusConflict {
		t.Fatalf("restore unavailable status = %d: %s", res.Code, res.Body.String())
	}
	got, err := st.GetFile("/missing.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.VersionID != current.VersionID {
		t.Fatalf("unavailable restore changed current version to %q", got.VersionID)
	}
}

func putDAVVersion(t *testing.T, handler http.Handler, target, content, ifMatch string) {
	t.Helper()
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/dav"+target, bytes.NewBufferString(content))
	req.Header.Set("Authorization", basicAuth())
	if ifMatch != "" {
		req.Header.Set("If-Match", `"`+ifMatch+`"`)
	}
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated && res.Code != http.StatusOK && res.Code != http.StatusNoContent {
		t.Fatalf("PUT %s status = %d: %s", target, res.Code, res.Body.String())
	}
}
