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
	if err := mw.WriteField("path", "x"); err != nil {
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
	f, err := st.GetFile("x")
	if err != nil {
		t.Fatal(err)
	}
	if f.MimeType != "application/octet-stream" {
		t.Fatalf("mime = %q, want application/octet-stream", f.MimeType)
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
		writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{"accepted": accepted})})
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
