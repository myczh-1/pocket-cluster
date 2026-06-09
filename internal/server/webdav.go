package server

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/webdav"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

type davFS struct {
	store  *store.Store
	chunks *chunk.Storage
	nodeID string
	srv    *Server
}

func normPath(name string) string {
	if name == "" {
		return "/"
	}
	clean := path.Clean(name)
	if clean == "." {
		return "/"
	}
	if !strings.HasPrefix(clean, "/") {
		clean = "/" + clean
	}
	return clean
}
func (d *davFS) Mkdir(_ context.Context, name string, _ os.FileMode) error {
	name = normPath(name)
	if name == "/" {
		return nil
	}
	dir := &types.File{
		FileID:     uuid.New().String(),
		Name:       path.Base(name),
		Path:       name,
		IsDir:      true,
		CreatedAt:  time.Now(),
		ModifiedAt: time.Now(),
		ModifiedBy: d.nodeID,
	}
	if err := d.store.UpsertFile(dir); err != nil {
		return err
	}
	if d.srv != nil {
		d.srv.appendEvent(types.EventDirCreate, map[string]string{"path": name, "created_by": d.nodeID})
	}
	return nil
}

func (d *davFS) OpenFile(_ context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	name = normPath(name)
	if flag&os.O_CREATE != 0 {
		return &davWriteFile{name: name, store: d.store, chunks: d.chunks, nodeID: d.nodeID, srv: d.srv}, nil
	}
	if name == "/" {
		return d.openDir("/")
	}
	f, err := d.store.GetFile(name)
	if err == nil && !f.Deleted {
		return &davReadFile{file: f, chunks: d.chunks}, nil
	}
	children, cerr := d.store.ListFiles(name)
	if cerr == nil && len(children) > 0 {
		return d.openDir(name)
	}
	return nil, os.ErrNotExist
}

func (d *davFS) openDir(name string) (webdav.File, error) {
	files, err := d.store.ListFiles(name)
	if err != nil {
		return nil, err
	}
	var entries []os.FileInfo
	for i := range files {
		if !files[i].Deleted {
			entries = append(entries, fileToInfo(&files[i]))
		}
	}
	return &davDir{name: name, entries: entries}, nil
}

func (d *davFS) RemoveAll(_ context.Context, name string) error {
	name = normPath(name)
	f, err := d.store.GetFile(name)
	if err != nil {
		return os.ErrNotExist
	}
	if f.IsDir {
		// Recursively delete directory and all children
		if err := d.store.MarkChildrenDeleted(name, d.nodeID); err != nil {
			return err
		}
		if d.srv != nil {
			d.srv.appendEvent(types.EventDirDelete, map[string]string{"path": name, "deleted_by": d.nodeID})
		}
	} else {
		if err := d.store.MarkFileDeleted(name, d.nodeID); err != nil {
			return err
		}
		if d.srv != nil {
			d.srv.appendEvent(types.EventFileDelete, map[string]string{"path": name, "deleted_by": d.nodeID})
		}
	}
	return nil
}

func (d *davFS) Rename(_ context.Context, oldName, newName string) error {
	oldName = normPath(oldName)
	newName = normPath(newName)
	f, err := d.store.GetFile(oldName)
	if err != nil {
		return os.ErrNotExist
	}
	now := time.Now()
	if err := d.store.RenameFile(f.FileID, oldName, newName, d.nodeID, now); err != nil {
		return err
	}
	if f.IsDir {
		if err := d.store.RenameChildren(oldName, newName, d.nodeID, now); err != nil {
			return err
		}
	}
	if d.srv != nil {
		d.srv.appendEvent(types.EventFileRename, map[string]string{
			"file_id":  f.FileID,
			"old_path": oldName,
			"new_path": newName,
		})
	}
	return nil
}
func (d *davFS) Stat(_ context.Context, name string) (os.FileInfo, error) {
	name = normPath(name)
	if name == "/" {
		return &davInfo{name: "/", isDir: true}, nil
	}
	f, err := d.store.GetFile(name)
	if err == nil && !f.Deleted {
		return fileToInfo(f), nil
	}
	children, cerr := d.store.ListFiles(name)
	if cerr == nil && len(children) > 0 {
		return &davInfo{name: path.Base(name), isDir: true}, nil
	}
	return nil, os.ErrNotExist
}

// ---------- directory ----------

type davDir struct {
	name    string
	entries []os.FileInfo
	pos     int
}

func (d *davDir) Close() error                                   { return nil }
func (d *davDir) Read([]byte) (int, error)                       { return 0, io.EOF }
func (d *davDir) Seek(int64, int) (int64, error)                 { return 0, nil }
func (d *davDir) Write([]byte) (int, error)                      { return 0, os.ErrPermission }
func (d *davDir) Stat() (os.FileInfo, error)                     { return &davInfo{name: d.name, isDir: true}, nil }
func (d *davDir) Readdir(count int) ([]os.FileInfo, error) {
	if count <= 0 {
		out := d.entries[d.pos:]
		d.pos = len(d.entries)
		return out, nil
	}
	end := d.pos + count
	if end > len(d.entries) {
		end = len(d.entries)
	}
	out := d.entries[d.pos:end]
	d.pos = end
	return out, nil
}

// ---------- read file ----------

type davReadFile struct {
	file    *types.File
	chunks  *chunk.Storage
	data    []byte
	readPos int
}

func (f *davReadFile) Close() error                       { return nil }
func (f *davReadFile) Write([]byte) (int, error)          { return 0, os.ErrPermission }
func (f *davReadFile) Readdir(int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }
func (f *davReadFile) Stat() (os.FileInfo, error)         { return fileToInfo(f.file), nil }

func (f *davReadFile) ensureLoaded() error {
	if f.data == nil {
		var buf bytes.Buffer
		for _, cid := range f.file.ChunkIDs {
			r, _, err := f.chunks.Open(cid)
			if err != nil {
				return err
			}
			io.Copy(&buf, r)
			r.Close()
		}
		f.data = buf.Bytes()
	}
	return nil
}

func (f *davReadFile) Read(p []byte) (int, error) {
	if err := f.ensureLoaded(); err != nil {
		return 0, err
	}
	if f.readPos >= len(f.data) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.readPos:])
	f.readPos += n
	return n, nil
}

func (f *davReadFile) Seek(offset int64, whence int) (int64, error) {
	if err := f.ensureLoaded(); err != nil {
		return 0, err
	}
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = int64(f.readPos) + offset
	case io.SeekEnd:
		abs = int64(len(f.data)) + offset
	default:
		return 0, os.ErrInvalid
	}
	if abs < 0 {
		return 0, os.ErrInvalid
	}
	f.readPos = int(abs)
	return abs, nil
}

// ---------- write file ----------

type davWriteFile struct {
	name   string
	store  *store.Store
	chunks *chunk.Storage
	nodeID string
	srv    *Server
	buf    bytes.Buffer
}
func (f *davWriteFile) Close() error {
	data := f.buf.Bytes()
	if len(data) == 0 {
		return nil
	}
	var chunkIDs []string
	totalSize := int64(0)
	r := bytes.NewReader(data)
	buf := make([]byte, chunk.ChunkSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			hash, size, storeErr := f.chunks.Store(bytes.NewReader(buf[:n]))
			if storeErr != nil {
				return storeErr
			}
			now := time.Now()
			f.store.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: size, StoredAt: now})
			replica := &types.Replica{ChunkID: hash, NodeID: f.nodeID, Status: "available", StoredAt: now, VerifiedAt: now}
			f.store.UpsertReplica(replica)
			if f.srv != nil {
				f.srv.appendEvent(types.EventChunkReplicaAdd, replica)
			}
			chunkIDs = append(chunkIDs, hash)
			totalSize += size
		}
		if err != nil {
			break
		}
	}
	mimeType := "application/octet-stream"
	if detected := mime.TypeByExtension(path.Ext(f.name)); detected != "" {
		mimeType = detected
	}
	// Clean up old chunks if overwriting
	if old, err := f.store.GetFile(f.name); err == nil && !old.Deleted {
		for _, cid := range old.ChunkIDs {
			ref, _ := f.store.IsChunkReferenced(cid)
			if !ref {
				f.chunks.Remove(cid)
				f.store.MarkReplicaRemoved(cid, f.nodeID, time.Now())
			}
		}
	}
	now2 := time.Now()
	file := &types.File{
		FileID:     uuid.New().String(),
		Name:       path.Base(f.name),
		Path:       f.name,
		SizeBytes:  totalSize,
		MimeType:   mimeType,
		VersionID:  uuid.NewString(),
		ChunkIDs:   chunkIDs,
		CreatedAt:  now2,
		ModifiedAt: now2,
		ModifiedBy: f.nodeID,
	}
	if err := f.store.UpsertFile(file); err != nil {
		return err
	}
	if f.srv != nil {
		f.srv.appendEvent(types.EventFilePut, file)
	}
	return nil
}

func (f *davWriteFile) Read([]byte) (int, error)          { return 0, os.ErrPermission }
func (f *davWriteFile) Readdir(int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }
func (f *davWriteFile) Seek(int64, int) (int64, error)     { return 0, nil }
func (f *davWriteFile) Stat() (os.FileInfo, error)         { return &davInfo{name: path.Base(f.name)}, nil }
func (f *davWriteFile) Write(p []byte) (int, error)        { return f.buf.Write(p) }

// ---------- FileInfo ----------

type davInfo struct {
	name    string
	isDir   bool
	size    int64
	modTime time.Time
}

func (i *davInfo) Name() string       { return i.name }
func (i *davInfo) Size() int64        { return i.size }
func (i *davInfo) Mode() os.FileMode  { if i.isDir { return os.ModeDir | 0o755 }; return 0o644 }
func (i *davInfo) ModTime() time.Time { return i.modTime }
func (i *davInfo) IsDir() bool        { return i.isDir }
func (i *davInfo) Sys() interface{}   { return nil }

func fileToInfo(f *types.File) *davInfo {
	return &davInfo{name: f.Name, size: f.SizeBytes, modTime: f.ModifiedAt}
}

// ---------- ETag support ----------
type etagResponseWriter struct {
	http.ResponseWriter
	etag   string
	status int
}
func (w *etagResponseWriter) WriteHeader(code int) {
	w.status = code
	if w.etag != "" && code == http.StatusOK {
		w.ResponseWriter.Header().Set("ETag", w.etag)
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *etagResponseWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}
// ---------- mount ----------
func (s *Server) handleWebDAV(w http.ResponseWriter, r *http.Request) {
	// Set ETag for GET/HEAD based on file version
	if r.Method == "GET" || r.Method == "HEAD" {
		name := normPath(strings.TrimPrefix(r.URL.Path, "/dav"))
		if f, err := s.store.GetFile(name); err == nil && !f.Deleted && f.VersionID != "" {
			etag := `"` + f.VersionID + `"`
			w.Header().Set("ETag", etag)
			// Check If-Match / If-None-Match
			if match := r.Header.Get("If-None-Match"); match == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
	}
	// Check If-Match on write methods to prevent concurrent overwrites
	if r.Method == "PUT" || r.Method == "PATCH" || r.Method == "PROPPATCH" {
		if ifMatch := r.Header.Get("If-Match"); ifMatch != "" {
			name := normPath(strings.TrimPrefix(r.URL.Path, "/dav"))
			if f, err := s.store.GetFile(name); err == nil && !f.Deleted {
				currentETag := `"` + f.VersionID + `"`
				if ifMatch != currentETag {
					w.WriteHeader(http.StatusPreconditionFailed)
					return
				}
			}
		}
	}
	(&webdav.Handler{
		FileSystem: &davFS{store: s.store, chunks: s.chunks, nodeID: s.cfg.NodeID, srv: s},
		LockSystem: webdav.NewMemLS(),
		Prefix:     "/dav",
	}).ServeHTTP(w, r)
}
