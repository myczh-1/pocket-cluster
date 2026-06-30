package server

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func TestUploadShortPathDoesNotPanicAndUsesFallbackMime(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	srv := New(newTestConfig(t, "local"), st, chunks)
	session := loginTestSession(t, srv)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("path", "/x"); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("file", "x")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/files/upload", &body), session)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	f, err := st.GetFile("/x")
	if err != nil {
		t.Fatal(err)
	}
	if f.MimeType != "application/octet-stream" {
		t.Fatalf("mime = %q, want application/octet-stream", f.MimeType)
	}
}

func TestWriteOKReturnsErrorWhenResponseCannotMarshal(t *testing.T) {
	res := httptest.NewRecorder()

	writeOK(res, http.StatusOK, map[string]any{"bad": make(chan int)})

	if res.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusInternalServerError)
	}
	var body types.APIResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.OK || body.Error == nil || body.Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("unexpected body: %+v", body)
	}
}

func TestUploadEmptyFileDoesNotCreateEmptyChunk(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	session := loginTestSession(t, srv)

	res := httptest.NewRecorder()
	req := withAuth(uploadRequest(t, "/empty.txt", "empty.txt", nil), session)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	file, err := st.GetFile("/empty.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(file.ChunkIDs) != 0 {
		t.Fatalf("empty file chunk count = %d, want 0", len(file.ChunkIDs))
	}
}

func TestRenameRejectsRelativeAndHiddenPathComponents(t *testing.T) {
	for _, p := range []string{
		"relative.txt",
		"/",
		"/a/../b.txt",
		"/a/./b.txt",
		"/.hidden",
		"/dir/.hidden",
	} {
		if err := validateRenamePath(p); err == nil {
			t.Fatalf("validateRenamePath(%q) succeeded, want error", p)
		}
	}
	if err := validateRenamePath("/dir/visible.txt"); err != nil {
		t.Fatalf("validateRenamePath valid path failed: %v", err)
	}
}

func TestNormalizePoolFilePathCollapsesHarmlessDuplicateSlashes(t *testing.T) {
	got, err := normalizePoolFilePath("/dir//visible.txt")
	if err != nil {
		t.Fatalf("normalizePoolFilePath returned error: %v", err)
	}
	if got != "/dir/visible.txt" {
		t.Fatalf("normalized path = %q, want %q", got, "/dir/visible.txt")
	}

	got, err = normalizePoolFilePath("/dir/visible.txt/")
	if err != nil {
		t.Fatalf("normalizePoolFilePath trailing slash returned error: %v", err)
	}
	if got != "/dir/visible.txt" {
		t.Fatalf("normalized trailing-slash path = %q, want %q", got, "/dir/visible.txt")
	}
}

func TestRenameCanonicalizesDuplicateSlashes(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	srv := New(newTestConfig(t, "local"), st, chunks)
	session := loginTestSession(t, srv)

	if err := st.UpsertFile(&types.File{
		FileID:     "rename-me",
		Name:       "old.txt",
		Path:       "/old.txt",
		SizeBytes:  1,
		VersionID:  "v1",
		ChunkIDs:   nil,
		CreatedAt:  time.Now(),
		ModifiedAt: time.Now(),
		ModifiedBy: "local",
	}); err != nil {
		t.Fatal(err)
	}

	req := withAuth(httptest.NewRequest(http.MethodPatch, "/api/files/rename", strings.NewReader(`{"path":"/old.txt","new_path":"//folder//new.txt/"}`)), session)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("rename status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	if _, err := st.GetFile("/old.txt"); err == nil {
		t.Fatal("old path still exists after rename")
	}
	f, err := st.GetFile("/folder/new.txt")
	if err != nil {
		t.Fatalf("canonicalized path not found: %v", err)
	}
	if f.Name != "new.txt" {
		t.Fatalf("renamed file name = %q, want %q", f.Name, "new.txt")
	}
}

func TestUploadReturnsBeforeReplicaRepairFailure(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	srv := New(newTestConfig(t, "local"), st, chunks)
	session := loginTestSession(t, srv)
	if err := st.UpsertNode(&types.Node{NodeID: "remote", Address: "127.0.0.1:1", Status: "online", Trusted: true}); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("path", "/background-repair.txt"); err != nil {
		t.Fatal(err)
	}
	part, err := mw.CreateFormFile("file", "background-repair.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("payload")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/files/upload", &body), session)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("upload status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	time.Sleep(100 * time.Millisecond)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadPrechecksChunksBeforeWritingBody(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	srv := New(newTestConfig(t, "local"), st, chunks)
	session := loginTestSession(t, srv)
	hash, _, err := chunks.Store(bytes.NewReader([]byte("first chunk")))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertFile(&types.File{FileID: "file", Name: "file.bin", Path: "/file.bin", ChunkIDs: []string{hash, "missingchunk"}, ModifiedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	res := httptest.NewRecorder()
	req := withAuth(httptest.NewRequest(http.MethodGet, "/api/files/download?path=/file.bin", nil), session)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("download status = %d, want %d", res.Code, http.StatusNotFound)
	}
	if strings.Contains(res.Body.String(), "first chunk") {
		t.Fatalf("response contains partial file bytes before error: %q", res.Body.String())
	}
}

func TestRepairChunkReplicasReturnsFetchError(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	srv := New(newTestConfig(t, "local"), st, chunks)
	if err := st.UpsertReplica(&types.Replica{ChunkID: "missingchunk", NodeID: "remote", Status: "available", StoredAt: time.Now(), VerifiedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertNode(&types.Node{NodeID: "remote", Address: "127.0.0.1:1", Status: "online", Trusted: true}); err != nil {
		t.Fatal(err)
	}
	nodes, err := st.ListNodes()
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.repairChunkReplicas(context.Background(), "missingchunk", nodes); err == nil {
		t.Fatal("repair succeeded; want fetch error")
	}
}

func TestStoreChunkDeduplicatesReplicaEvents(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	srv := New(newTestConfig(t, "local"), st, chunks)
	body := []byte("same chunk pushed twice")
	hash := sha256Hex(body)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/chunks", bytes.NewReader(body))
		req.Header.Set("X-Chunk-Hash", hash)
		req.Header.Set(authBodySHA256Header, hash)
		res := httptest.NewRecorder()
		srv.handleStoreChunk(res, req)
		if res.Code != http.StatusOK {
			t.Fatalf("store chunk attempt %d status = %d, want %d: %s", i+1, res.Code, http.StatusOK, res.Body.String())
		}
	}

	count, err := st.EventCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("event count = %d, want 1", count)
	}
}

func TestPushEventsMarksEventsPushedPerPeer(t *testing.T) {
	var firstBatch atomic.Int32
	remoteHTTP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/events/push" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Events []types.Event `json:"events"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		accepted := len(req.Events)
		if accepted > 0 {
			firstBatch.Add(int32(accepted))
		}
		writeOK(w, http.StatusOK, map[string]any{"accepted": accepted})
	}))
	defer remoteHTTP.Close()

	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	srv := New(newTestConfig(t, "local"), st, chunks)
	if _, err := srv.appendEvent(types.EventNodeUpdate, &types.Node{NodeID: "local", Status: "online"}); err != nil {
		t.Fatal(err)
	}
	remote := types.Node{NodeID: "remote", Address: strings.TrimPrefix(remoteHTTP.URL, "http://"), Status: "online", Trusted: true}
	if _, err := srv.pushEvents(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.pushEvents(context.Background(), remote); err != nil {
		t.Fatal(err)
	}
	if firstBatch.Load() != 1 {
		t.Fatalf("pushed event count = %d, want exactly one non-empty push", firstBatch.Load())
	}
}

func TestJoinApproveIsNotOpenApprovalStub(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	session := loginTestSession(t, srv)
	res := httptest.NewRecorder()
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/join/approve", nil), session)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("join approve status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestAgentLogsUseInjectedRingBuffer(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	ring := NewRingBuffer(2)
	ring.Add("first")
	ring.Add("second")
	srv := New(newTestConfig(t, "local"), st, chunks, WithLogRing(ring))
	session := loginTestSession(t, srv)

	res := httptest.NewRecorder()
	req := withAuth(httptest.NewRequest(http.MethodGet, "/api/agent/logs", nil), session)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("logs status = %d, want %d", res.Code, http.StatusOK)
	}
	if !strings.Contains(res.Body.String(), "first") || !strings.Contains(res.Body.String(), "second") {
		t.Fatalf("logs response = %s, want injected ring lines", res.Body.String())
	}
}

func TestDeleteRemovesChunks(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	session := loginTestSession(t, srv)

	// Upload a file
	content := []byte("test content for delete")
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("path", "/delete-test.txt")
	part, _ := mw.CreateFormFile("file", "delete-test.txt")
	part.Write(content)
	mw.Close()

	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/files/upload", &body), session)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("upload status = %d: %s", res.Code, res.Body.String())
	}

	// Get the file to find chunk IDs
	f, err := st.GetFile("/delete-test.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.ChunkIDs) == 0 {
		t.Fatal("no chunks")
	}
	chunkID := f.ChunkIDs[0]

	// Verify chunk exists on disk
	if !srv.chunks.Exists(chunkID) {
		t.Fatal("chunk should exist before delete")
	}

	// Delete the file
	res = httptest.NewRecorder()
	req = withAuth(httptest.NewRequest(http.MethodDelete, "/api/files?path=/delete-test.txt", nil), session)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("delete status = %d: %s", res.Code, res.Body.String())
	}

	// Chunks are no longer removed immediately on delete.
	// They are tombstoned and cleaned up later by CleanupTombstones.
	// Verify file is tombstoned
	f2, err := st.GetFileByID(f.FileID)
	if err != nil {
		t.Fatal(err)
	}
	if !f2.Deleted {
		t.Fatal("file should be marked deleted")
	}
	// Chunk should still exist until GC runs
	if !srv.chunks.Exists(chunkID) {
		t.Fatal("chunk should still exist until tombstone cleanup")
	}
}
