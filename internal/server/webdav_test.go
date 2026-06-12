package server

import (
	"bytes"
	"encoding/base64"
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
