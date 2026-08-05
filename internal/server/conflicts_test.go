package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func TestUploadExistingPathCreatesConflictFile(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	session := loginTestSession(t, srv)

	existing := &types.File{
		FileID:     "original-file",
		Name:       "shared.txt",
		Path:       "/shared.txt",
		SizeBytes:  3,
		VersionID:  "original-version",
		CreatedAt:  time.UnixMilli(1000),
		ModifiedAt: time.UnixMilli(1000),
		ModifiedBy: "other-node",
	}
	if err := st.UpsertFile(existing); err != nil {
		t.Fatal(err)
	}

	res := httptest.NewRecorder()
	req := withAuth(uploadRequest(t, "/shared.txt", "shared.txt", []byte("new content")), session)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var envelope types.APIResponse
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Path       string `json:"path"`
		ConflictOf string `json:"conflict_of"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Path == existing.Path {
		t.Fatal("upload overwrote original path instead of creating conflict")
	}
	if !strings.Contains(payload.Path, "sync-conflict-local") {
		t.Fatalf("conflict path = %q, want sync-conflict-local marker", payload.Path)
	}
	if payload.ConflictOf != existing.FileID {
		t.Fatalf("conflict_of = %q, want %q", payload.ConflictOf, existing.FileID)
	}
	current, err := st.GetFile(existing.Path)
	if err != nil {
		t.Fatal(err)
	}
	if current.FileID != existing.FileID {
		t.Fatalf("original file id = %q, want preserved %q", current.FileID, existing.FileID)
	}
	conflict, err := st.GetFile(payload.Path)
	if err != nil {
		t.Fatal(err)
	}
	if conflict.ConflictOf != existing.FileID {
		t.Fatalf("stored conflict_of = %q, want %q", conflict.ConflictOf, existing.FileID)
	}
}

func TestRemoteFilePutConflictDoesNotOverwriteLocalFile(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()

	local := &types.File{
		FileID:     "local-file",
		Name:       "shared.txt",
		Path:       "/shared.txt",
		SizeBytes:  5,
		VersionID:  "local-version",
		CreatedAt:  time.UnixMilli(1000),
		ModifiedAt: time.UnixMilli(1000),
		ModifiedBy: "local",
	}
	if err := st.UpsertFile(local); err != nil {
		t.Fatal(err)
	}
	remote := &types.File{
		FileID:     "remote-file",
		Name:       "shared.txt",
		Path:       "/shared.txt",
		SizeBytes:  6,
		VersionID:  "remote-version",
		CreatedAt:  time.UnixMilli(2000),
		ModifiedAt: time.UnixMilli(2000),
		ModifiedBy: "remote-node",
	}
	body := mustJSON(t, remote)
	if err := srv.applyEvent(types.Event{Type: types.EventFilePut, NodeID: "remote-node", Payload: body}); err != nil {
		t.Fatal(err)
	}

	current, err := st.GetFile(local.Path)
	if err != nil {
		t.Fatal(err)
	}
	if current.FileID != local.FileID {
		t.Fatalf("local path file id = %q, want %q", current.FileID, local.FileID)
	}
	files, err := st.ListFiles("/")
	if err != nil {
		t.Fatal(err)
	}
	var conflict *types.File
	for i := range files {
		if files[i].ConflictOf == local.FileID {
			conflict = &files[i]
			break
		}
	}
	if conflict == nil {
		t.Fatal("remote conflict file not found")
	}
	if conflict.FileID != remote.FileID {
		t.Fatalf("conflict file id = %q, want remote %q", conflict.FileID, remote.FileID)
	}
	if !strings.Contains(conflict.Path, "sync-conflict-remote-n") {
		t.Fatalf("conflict path = %q, want remote marker", conflict.Path)
	}
}

func uploadRequest(t *testing.T, targetPath, filename string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("path", targetPath); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/files/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}
