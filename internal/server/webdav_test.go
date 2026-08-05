package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func basicAuth() string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:testpass"))
}

func readWebDAVFile(t *testing.T, handler http.Handler, target string) []byte {
	t.Helper()
	res := httptest.NewRecorder()
	req := httptest.NewRequest("GET", target, nil)
	req.Header.Set("Authorization", basicAuth())
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want %d: %s", target, res.Code, http.StatusOK, res.Body.String())
	}
	return res.Body.Bytes()
}

func TestWebDAVBrowse(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()

	if err := st.UpsertFile(&types.File{
		FileID: "f1", Name: "hello.txt", Path: "/hello.txt",
		SizeBytes: 5, ModifiedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	res := httptest.NewRecorder()
	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Depth", "1")
	req.Header.Set("Authorization", basicAuth())
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusMultiStatus {
		t.Fatalf("PROPFIND status = %d, want %d: %s", res.Code, http.StatusMultiStatus, res.Body.String())
	}
	if !bytes.Contains(res.Body.Bytes(), []byte("hello.txt")) {
		t.Fatalf("missing hello.txt in response")
	}
}

func TestWebDAVUploadAndDownload(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()

	content := []byte("hello world content for webdav")

	// PUT
	res := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/dav/hello.txt", bytes.NewReader(content))
	req.Header.Set("Authorization", basicAuth())
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated && res.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200/201: %s", res.Code, res.Body.String())
	}

	// GET
	res = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/dav/hello.txt", nil)
	req.Header.Set("Authorization", basicAuth())
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	if res.Body.String() != string(content) {
		t.Fatalf("content mismatch: got %q, want %q", res.Body.String(), string(content))
	}
}

func TestWebDAVGetUsesVersionETag(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()

	content := []byte("etag content")
	res := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/dav/etag.txt", bytes.NewReader(content))
	req.Header.Set("Authorization", basicAuth())
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusCreated && res.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200/201: %s", res.Code, res.Body.String())
	}
	f, err := st.GetFile("/etag.txt")
	if err != nil {
		t.Fatal(err)
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/dav/etag.txt", nil)
	req.Header.Set("Authorization", basicAuth())
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	if got, want := res.Header().Get("ETag"), `"`+f.VersionID+`"`; got != want {
		t.Fatalf("GET ETag = %q, want %q", got, want)
	}
}

func TestWebDAVOverwriteRequiresCurrentETag(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()
	handler := srv.Handler()

	res := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/dav/overwrite.txt", bytes.NewReader([]byte("first")))
	req.Header.Set("Authorization", basicAuth())
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated && res.Code != http.StatusOK {
		t.Fatalf("first PUT status = %d, want 200/201: %s", res.Code, res.Body.String())
	}
	first, err := st.GetFile("/overwrite.txt")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ChunkIDs) != 1 {
		t.Fatalf("first chunk count = %d, want 1", len(first.ChunkIDs))
	}
	oldChunkID := first.ChunkIDs[0]

	res = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/dav/overwrite.txt", bytes.NewReader([]byte("blind overwrite")))
	req.Header.Set("Authorization", basicAuth())
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusPreconditionRequired {
		t.Fatalf("blind overwrite status = %d, want %d: %s", res.Code, http.StatusPreconditionRequired, res.Body.String())
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/dav/overwrite.txt", bytes.NewReader([]byte("stale overwrite")))
	req.Header.Set("Authorization", basicAuth())
	req.Header.Set("If-Match", `"stale-version"`)
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale overwrite status = %d, want %d: %s", res.Code, http.StatusPreconditionFailed, res.Body.String())
	}

	current, err := st.GetFile("/overwrite.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(readWebDAVFile(t, handler, "/dav/overwrite.txt")) != "first" {
		t.Fatal("failed precondition changed file content")
	}

	res = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/dav/overwrite.txt", bytes.NewReader([]byte("second")))
	req.Header.Set("Authorization", basicAuth())
	req.Header.Set("If-Match", `"`+current.VersionID+`"`)
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusCreated && res.Code != http.StatusOK {
		t.Fatalf("conditional overwrite status = %d, want 200/201: %s", res.Code, res.Body.String())
	}
	if srv.chunks.Exists(oldChunkID) {
		t.Fatalf("old chunk %s still exists after overwrite", oldChunkID)
	}
	if string(readWebDAVFile(t, handler, "/dav/overwrite.txt")) != "second" {
		t.Fatal("conditional overwrite did not update file content")
	}
}

func TestDavWriteFileCloseCleansStagedChunksOnError(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()

	writer := &davWriteFile{
		name:   "/failed.txt",
		store:  st,
		chunks: srv.chunks,
		nodeID: srv.cfg.NodeID,
		srv:    srv,
		ctx:    context.Background(),
	}
	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if err := writer.flushChunk(); err != nil {
		t.Fatal(err)
	}
	if len(writer.chunkIDs) != 1 {
		t.Fatalf("chunk count = %d, want 1", len(writer.chunkIDs))
	}
	chunkID := writer.chunkIDs[0]
	if !srv.chunks.Exists(chunkID) {
		t.Fatalf("chunk %s missing before failed close", chunkID)
	}

	writer.writeErr = errors.New("write failed")
	if err := writer.Close(); err == nil {
		t.Fatal("Close succeeded after write error")
	}
	if srv.chunks.Exists(chunkID) {
		t.Fatalf("chunk %s still exists after failed close", chunkID)
	}
	if f, err := st.GetFile("/failed.txt"); err == nil && !f.Deleted {
		t.Fatalf("file committed despite failed close: %+v", f)
	}
}

func TestDavReadFileStreamsChunksAndSeeks(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()

	firstHash, firstSize, err := srv.chunks.Store(bytes.NewReader([]byte("alpha")))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertChunk(&types.Chunk{ChunkID: firstHash, SizeBytes: firstSize, StoredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	secondHash, secondSize, err := srv.chunks.Store(bytes.NewReader([]byte("bravo")))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertChunk(&types.Chunk{ChunkID: secondHash, SizeBytes: secondSize, StoredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	file := &types.File{
		FileID:    "streamed",
		Name:      "streamed.txt",
		Path:      "/streamed.txt",
		SizeBytes: firstSize + secondSize,
		ChunkIDs:  []string{firstHash, secondHash},
	}
	reader, err := newDavReadFile(file, st, srv.chunks)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	all := make([]byte, int(file.SizeBytes))

	if _, err := io.ReadFull(reader, all); err != nil {
		t.Fatal(err)
	}
	if string(all) != "alphabravo" {
		t.Fatalf("read = %q, want alphabravo", string(all))
	}
	if _, err := reader.Seek(2, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	window := make([]byte, 4)
	if _, err := io.ReadFull(reader, window); err != nil {
		t.Fatal(err)
	}
	if string(window) != "phab" {
		t.Fatalf("seek read = %q, want phab", string(window))
	}
	if _, err := reader.Seek(-5, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	tail, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(tail) != "bravo" {
		t.Fatalf("tail = %q, want bravo", string(tail))
	}
}

func TestWebDAVBasicAuth(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()

	// No auth
	res := httptest.NewRecorder()
	req := httptest.NewRequest("PROPFIND", "/dav/", nil)
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthed status = %d, want %d", res.Code, http.StatusUnauthorized)
	}

	// With auth
	res = httptest.NewRecorder()
	req = httptest.NewRequest("PROPFIND", "/dav/", nil)
	req.Header.Set("Authorization", basicAuth())
	req.Header.Set("Depth", "1")
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusMultiStatus {
		t.Fatalf("authed status = %d, want %d: %s", res.Code, http.StatusMultiStatus, res.Body.String())
	}
}

func TestWebDAVInfo(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()

	session := loginTestSession(t, srv)
	res := httptest.NewRecorder()
	req := withAuth(httptest.NewRequest(http.MethodGet, "/api/webdav/info", nil), session)
	req.Host = "pocket.local:7788"
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("info status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	var envelope types.APIResponse
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Enabled      bool   `json:"enabled"`
		URL          string `json:"url"`
		AuthRequired bool   `json:"auth_required"`
		Username     string `json:"username"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Enabled || payload.URL != "http://pocket.local:7788/dav/" {
		t.Fatalf("unexpected WebDAV info: %+v", payload)
	}
	if !payload.AuthRequired || payload.Username != "admin" {
		t.Fatalf("unexpected auth info: %+v", payload)
	}
}

func TestWebDAVDelete(t *testing.T) {
	_, st, srv := newJoinTestServer(t, "local")
	defer st.Close()

	if err := st.UpsertFile(&types.File{
		FileID: "del1", Name: "delete-me.txt", Path: "/delete-me.txt",
		SizeBytes: 5, ModifiedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	res := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/dav/delete-me.txt", nil)
	req.Header.Set("Authorization", basicAuth())
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want %d: %s", res.Code, http.StatusNoContent, res.Body.String())
	}

	_, err := st.GetFile("/delete-me.txt")
	if err == nil {
		t.Fatal("file should be deleted")
	}
}
