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

func TestRemoteSequentialFileUpdateKeepsMainPath(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()

	existing := &types.File{
		FileID: "shared-file", Name: "shared.txt", Path: "/shared.txt",
		VersionID: "version-1", CreatedAt: time.UnixMilli(1000), ModifiedAt: time.UnixMilli(1000), ModifiedBy: "node-a",
	}
	if err := st.UpsertFile(existing); err != nil {
		t.Fatal(err)
	}
	incoming := &types.File{
		FileID: "shared-file", Name: "shared.txt", Path: "/shared.txt",
		VersionID: "version-2", ParentVersionID: "version-1", CreatedAt: existing.CreatedAt, ModifiedAt: time.UnixMilli(2000), ModifiedBy: "node-b",
	}
	if err := srv.applyEvent(types.Event{Type: types.EventFilePut, NodeID: "node-b", Payload: mustJSON(t, incoming)}); err != nil {
		t.Fatal(err)
	}

	current, err := st.GetFile(existing.Path)
	if err != nil {
		t.Fatal(err)
	}
	if current.FileID != existing.FileID || current.VersionID != incoming.VersionID || current.ParentVersionID != existing.VersionID {
		t.Fatalf("sequential update metadata = %+v", current)
	}
	files, err := st.ListFiles("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("sequential update created %d entries, want 1", len(files))
	}
}

func TestConcurrentSameFileUpdatesConvergeIndependentOfArrivalOrder(t *testing.T) {
	base := &types.File{
		FileID: "shared-file", Name: "report.txt", Path: "/report.txt",
		VersionID: "version-base", ChunkIDs: []string{"chunk-base"},
		CreatedAt: time.UnixMilli(1000), ModifiedAt: time.UnixMilli(1000), ModifiedBy: "node-base",
	}
	fromA := &types.File{
		FileID: base.FileID, Name: base.Name, Path: base.Path,
		VersionID: "version-a", ParentVersionID: base.VersionID, ChunkIDs: []string{"chunk-a"},
		CreatedAt: base.CreatedAt, ModifiedAt: time.UnixMilli(2000), ModifiedBy: "node-a",
	}
	fromB := &types.File{
		FileID: base.FileID, Name: base.Name, Path: base.Path,
		VersionID: "version-b", ParentVersionID: base.VersionID, ChunkIDs: []string{"chunk-b"},
		CreatedAt: base.CreatedAt, ModifiedAt: time.UnixMilli(3000), ModifiedBy: "node-b",
	}

	applyOrder := func(t *testing.T, versions ...*types.File) []types.File {
		t.Helper()
		_, st, srv := newJoinTestServer(t, "local")
		defer st.Close()
		if err := st.UpsertFile(base); err != nil {
			t.Fatal(err)
		}
		for _, version := range versions {
			if err := srv.applyEvent(types.Event{Type: types.EventFilePut, NodeID: version.ModifiedBy, Payload: mustJSON(t, version)}); err != nil {
				t.Fatal(err)
			}
		}
		files, err := st.ListFiles("/")
		if err != nil {
			t.Fatal(err)
		}
		return files
	}

	aThenB := applyOrder(t, fromA, fromB, fromB)
	bThenA := applyOrder(t, fromB, fromA, fromA)
	if got, want := normalizedFileViews(aThenB), normalizedFileViews(bThenA); got != want {
		t.Fatalf("arrival order changed file views\nA then B: %s\nB then A: %s", got, want)
	}
	if len(aThenB) != 2 {
		t.Fatalf("concurrent views = %d, want main plus conflict", len(aThenB))
	}
	main := fileAtPath(aThenB, base.Path)
	if main == nil || main.VersionID != fromA.VersionID {
		t.Fatalf("main view = %+v, want deterministic A branch", main)
	}
	var conflict *types.File
	for i := range aThenB {
		if aThenB[i].ConflictOf == base.FileID {
			conflict = &aThenB[i]
			break
		}
	}
	if conflict == nil || conflict.VersionID != fromB.VersionID || conflict.FileID != concurrentConflictFileID(base.FileID, fromB.VersionID) {
		t.Fatalf("conflict view = %+v, want deterministic B copy", conflict)
	}
}

func TestConcurrentWinnerBranchStaysMainAfterLaterUpdate(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	base := &types.File{FileID: "shared", Name: "shared.txt", Path: "/shared.txt", VersionID: "base", CreatedAt: time.UnixMilli(1000), ModifiedAt: time.UnixMilli(1000)}
	branchA := &types.File{FileID: base.FileID, Name: base.Name, Path: base.Path, VersionID: "a-first", ParentVersionID: base.VersionID, CreatedAt: base.CreatedAt, ModifiedAt: time.UnixMilli(2000), ModifiedBy: "node-a"}
	branchB := &types.File{FileID: base.FileID, Name: base.Name, Path: base.Path, VersionID: "b-first", ParentVersionID: base.VersionID, CreatedAt: base.CreatedAt, ModifiedAt: time.UnixMilli(3000), ModifiedBy: "node-b"}
	branchANext := &types.File{FileID: base.FileID, Name: base.Name, Path: base.Path, VersionID: "z-later", ParentVersionID: branchA.VersionID, CreatedAt: base.CreatedAt, ModifiedAt: time.UnixMilli(4000), ModifiedBy: "node-a"}
	if err := st.UpsertFile(base); err != nil {
		t.Fatal(err)
	}
	for _, version := range []*types.File{branchB, branchA, branchANext} {
		if err := srv.applyEvent(types.Event{Type: types.EventFilePut, NodeID: version.ModifiedBy, Payload: mustJSON(t, version)}); err != nil {
			t.Fatal(err)
		}
	}
	main, err := st.GetFile(base.Path)
	if err != nil {
		t.Fatal(err)
	}
	if main.VersionID != branchANext.VersionID {
		t.Fatalf("main version = %q, want later A version %q", main.VersionID, branchANext.VersionID)
	}
	files, err := st.ListFiles("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("views after continuing winner branch = %d, want 2", len(files))
	}
}

func normalizedFileViews(files []types.File) string {
	type view struct {
		Path            string
		FileID          string
		VersionID       string
		ParentVersionID string
		ConflictOf      string
	}
	views := make([]view, 0, len(files))
	for _, file := range files {
		views = append(views, view{file.Path, file.FileID, file.VersionID, file.ParentVersionID, file.ConflictOf})
	}
	body, _ := json.Marshal(views)
	return string(body)
}

func fileAtPath(files []types.File, path string) *types.File {
	for i := range files {
		if files[i].Path == path {
			return &files[i]
		}
	}
	return nil
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
