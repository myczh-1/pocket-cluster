package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func TestTrashAPIListsAndRestoresDeletedFile(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	session := loginTestSession(t, srv)
	file := &types.File{
		FileID: "recoverable", Name: "recover.txt", Path: "/recover.txt",
		VersionID: "version-1", ChunkIDs: []string{"chunk-a"},
		CreatedAt: time.UnixMilli(1000), ModifiedAt: time.UnixMilli(1000), ModifiedBy: "local",
	}
	if err := st.UpsertFile(file); err != nil {
		t.Fatal(err)
	}

	deleteRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(deleteRes, withAuth(httptest.NewRequest(http.MethodDelete, "/api/files?path=/recover.txt", nil), session))
	if deleteRes.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", deleteRes.Code, deleteRes.Body.String())
	}

	listRes := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRes, withAuth(httptest.NewRequest(http.MethodGet, "/api/trash", nil), session))
	if listRes.Code != http.StatusOK {
		t.Fatalf("trash status = %d: %s", listRes.Code, listRes.Body.String())
	}
	var listEnvelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Entries []struct {
				FileID    string    `json:"file_id"`
				DeletedAt time.Time `json:"deleted_at"`
				ExpiresAt time.Time `json:"expires_at"`
			} `json:"entries"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRes.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatal(err)
	}
	if len(listEnvelope.Data.Entries) != 1 || listEnvelope.Data.Entries[0].FileID != file.FileID {
		t.Fatalf("trash entries = %+v", listEnvelope.Data.Entries)
	}
	if !listEnvelope.Data.Entries[0].ExpiresAt.After(listEnvelope.Data.Entries[0].DeletedAt) {
		t.Fatalf("trash expiry = %+v", listEnvelope.Data.Entries[0])
	}

	restoreRes := httptest.NewRecorder()
	restoreReq := withAuth(httptest.NewRequest(http.MethodPost, "/api/trash/restore", strings.NewReader(`{"file_id":"recoverable"}`)), session)
	restoreReq.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(restoreRes, restoreReq)
	if restoreRes.Code != http.StatusOK {
		t.Fatalf("restore status = %d: %s", restoreRes.Code, restoreRes.Body.String())
	}
	if _, err := st.GetFile(file.Path); err != nil {
		t.Fatalf("restored file not visible: %v", err)
	}
	events, err := st.GetEventsSince("", 100)
	if err != nil {
		t.Fatal(err)
	}
	foundRestore := false
	for _, event := range events {
		if event.Type == types.EventFileRestore {
			foundRestore = true
			break
		}
	}
	if !foundRestore {
		t.Fatal("restore event not recorded")
	}
}

func TestDirectoryRestoreEventRestoresDeletedChildren(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	for _, file := range []*types.File{
		{FileID: "dir", Name: "docs", Path: "/docs", IsDir: true},
		{FileID: "child", Name: "report.txt", Path: "/docs/report.txt", VersionID: "version-child"},
	} {
		if err := st.UpsertFile(file); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.MarkChildrenDeleted("/docs", "node-a"); err != nil {
		t.Fatal(err)
	}
	payload := mustJSON(t, map[string]string{"file_id": "dir", "path": "/docs", "restored_by": "node-b"})
	if err := srv.applyEvent(types.Event{Type: types.EventDirRestore, NodeID: "node-b", Timestamp: time.UnixMilli(5000), Payload: payload}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/docs", "/docs/report.txt"} {
		if _, err := st.GetFile(path); err != nil {
			t.Fatalf("restored %s: %v", path, err)
		}
	}
}

func TestDirectoryCreateEventUsesSharedFileID(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	payload := mustJSON(t, map[string]string{
		"file_id":    "shared-directory-id",
		"path":       "/shared",
		"created_by": "node-a",
	})
	if err := srv.applyEvent(types.Event{
		Type:      types.EventDirCreate,
		NodeID:    "node-a",
		Timestamp: time.UnixMilli(6000),
		Payload:   payload,
	}); err != nil {
		t.Fatal(err)
	}
	dir, err := st.GetFile("/shared")
	if err != nil {
		t.Fatal(err)
	}
	if dir.FileID != "shared-directory-id" {
		t.Fatalf("directory file id = %q", dir.FileID)
	}
}
