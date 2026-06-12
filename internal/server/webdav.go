package server

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"sort"
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

func (d *davFS) OpenFile(ctx context.Context, name string, flag int, _ os.FileMode) (webdav.File, error) {
	name = normPath(name)
	if flag&os.O_CREATE != 0 {
		return &davWriteFile{name: name, store: d.store, chunks: d.chunks, nodeID: d.nodeID, srv: d.srv, ctx: ctx}, nil
	}
	if name == "/" {
		return d.openDir("/")
	}
	f, err := d.store.GetFile(name)
	if err == nil && !f.Deleted {
		return newDavReadFile(f, d.store, d.chunks)
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

func (d *davDir) Close() error                   { return nil }
func (d *davDir) Read([]byte) (int, error)       { return 0, io.EOF }
func (d *davDir) Seek(int64, int) (int64, error) { return 0, nil }
func (d *davDir) Write([]byte) (int, error)      { return 0, os.ErrPermission }
func (d *davDir) Stat() (os.FileInfo, error)     { return &davInfo{name: d.name, isDir: true}, nil }
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
	file         *types.File
	chunks       *chunk.Storage
	chunkOffsets []int64
	readPos      int64
	chunkIndex   int
	chunkFile    *os.File
}

func newDavReadFile(file *types.File, st *store.Store, chunks *chunk.Storage) (*davReadFile, error) {
	offsets := make([]int64, len(file.ChunkIDs)+1)
	for i, cid := range file.ChunkIDs {
		chunkMeta, err := st.GetChunk(cid)
		if err != nil {
			return nil, err
		}
		offsets[i+1] = offsets[i] + chunkMeta.SizeBytes
	}
	return &davReadFile{file: file, chunks: chunks, chunkOffsets: offsets}, nil
}

func (f *davReadFile) Close() error {
	if f.chunkFile == nil {
		return nil
	}
	err := f.chunkFile.Close()
	f.chunkFile = nil
	return err
}
func (f *davReadFile) Write([]byte) (int, error)          { return 0, os.ErrPermission }
func (f *davReadFile) Readdir(int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }
func (f *davReadFile) Stat() (os.FileInfo, error)         { return fileToInfo(f.file), nil }

func (f *davReadFile) openCurrentChunk() error {
	if f.chunkIndex >= len(f.file.ChunkIDs) {
		return io.ErrUnexpectedEOF
	}
	r, _, err := f.chunks.Open(f.file.ChunkIDs[f.chunkIndex])
	if err != nil {
		return err
	}
	f.chunkFile = r
	return nil
}

func (f *davReadFile) closeCurrentChunk() error {
	if f.chunkFile == nil {
		return nil
	}
	err := f.chunkFile.Close()
	f.chunkFile = nil
	return err
}

func (f *davReadFile) locateChunk(offset int64) (int, int64) {
	chunkIndex := sort.Search(len(f.chunkOffsets)-1, func(i int) bool {
		return f.chunkOffsets[i+1] > offset
	})
	return chunkIndex, offset - f.chunkOffsets[chunkIndex]
}

func (f *davReadFile) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if f.readPos >= f.file.SizeBytes {
		return 0, io.EOF
	}
	if remaining := f.file.SizeBytes - f.readPos; int64(len(p)) > remaining {
		p = p[:int(remaining)]
	}
	total := 0
	for len(p) > 0 && f.readPos < f.file.SizeBytes {
		if f.chunkFile == nil {
			if err := f.openCurrentChunk(); err != nil {
				if total > 0 {
					return total, nil
				}
				return 0, err
			}
		}
		n, err := f.chunkFile.Read(p)
		if n > 0 {
			f.readPos += int64(n)
			total += n
			p = p[n:]
		}
		if err == io.EOF {
			if cerr := f.closeCurrentChunk(); cerr != nil && total == 0 {
				return 0, cerr
			}
			f.chunkIndex++
			continue
		}
		if err != nil {
			if total > 0 {
				return total, nil
			}
			return 0, err
		}
		if n == 0 {
			break
		}
	}
	if total == 0 {
		return 0, io.EOF
	}
	return total, nil
}

func (f *davReadFile) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = f.readPos + offset
	case io.SeekEnd:
		abs = f.file.SizeBytes + offset
	default:
		return 0, os.ErrInvalid
	}
	if abs < 0 || abs > f.file.SizeBytes {
		return 0, os.ErrInvalid
	}
	if err := f.closeCurrentChunk(); err != nil {
		return 0, err
	}
	f.readPos = abs
	if abs == f.file.SizeBytes {
		f.chunkIndex = len(f.file.ChunkIDs)
		return abs, nil
	}
	chunkIndex, chunkOffset := f.locateChunk(abs)
	f.chunkIndex = chunkIndex
	if err := f.openCurrentChunk(); err != nil {
		return 0, err
	}
	if _, err := f.chunkFile.Seek(chunkOffset, io.SeekStart); err != nil {
		f.closeCurrentChunk()
		return 0, err
	}
	return abs, nil
}

// ---------- write file ----------

type davWriteFile struct {
	name   string
	store  *store.Store
	chunks *chunk.Storage
	nodeID string
	srv    *Server
	ctx    context.Context
	buf    bytes.Buffer

	chunkIDs       []string
	stagedChunkIDs []string
	totalSize      int64
	writeErr       error
	closeErr       error
	closed         bool
}

func (f *davWriteFile) Close() error {
	if f.closed {
		return f.closeErr
	}
	f.closed = true
	if f.writeErr != nil {
		f.cleanupStagedChunks()
		f.closeErr = f.writeErr
		return f.closeErr
	}
	if f.ctx != nil {
		if err := f.ctx.Err(); err != nil {
			f.cleanupStagedChunks()
			f.closeErr = err
			return err
		}
	}
	// Flush remaining buffered data as the final chunk.
	if f.buf.Len() > 0 {
		if err := f.flushChunk(); err != nil {
			f.cleanupStagedChunks()
			f.closeErr = err
			return err
		}
	}
	mimeType := "application/octet-stream"
	if detected := mime.TypeByExtension(path.Ext(f.name)); detected != "" {
		mimeType = detected
	}
	now := time.Now()
	file := &types.File{
		FileID:     uuid.New().String(),
		Name:       path.Base(f.name),
		Path:       f.name,
		SizeBytes:  f.totalSize,
		MimeType:   mimeType,
		VersionID:  uuid.NewString(),
		ChunkIDs:   f.chunkIDs,
		CreatedAt:  now,
		ModifiedAt: now,
		ModifiedBy: f.nodeID,
	}
	if f.srv != nil {
		if err := f.srv.commitFilePut(file, filePutOptions{}); err != nil {
			f.cleanupStagedChunks()
			f.closeErr = err
			return err
		}
	} else if err := f.store.UpsertFile(file); err != nil {
		f.cleanupStagedChunks()
		f.closeErr = err
		return err
	}
	return nil
}

func (f *davWriteFile) flushChunk() error {
	hash, size, err := f.chunks.Store(bytes.NewReader(f.buf.Bytes()))
	if err != nil {
		return err
	}
	f.stagedChunkIDs = append(f.stagedChunkIDs, hash)
	f.buf.Reset()
	now := time.Now()
	if f.srv != nil && f.nodeID == f.srv.cfg.NodeID {
		if _, _, err := f.srv.recordLocalChunkReplica(hash, size, now); err != nil {
			return err
		}
	} else {
		if err := f.store.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: size, StoredAt: now}); err != nil {
			return err
		}
		replica := &types.Replica{ChunkID: hash, NodeID: f.nodeID, Status: "available", StoredAt: now, VerifiedAt: now}
		if err := f.store.UpsertReplica(replica); err != nil {
			return err
		}
		if f.srv != nil {
			if _, err := f.srv.appendEvent(types.EventChunkReplicaAdd, replica); err != nil {
				return err
			}
		}
	}
	f.chunkIDs = append(f.chunkIDs, hash)
	f.totalSize += size
	return nil
}

func (f *davWriteFile) cleanupStagedChunks() {
	if f.srv == nil {
		return
	}
	f.srv.cleanupUnreferencedChunks(f.stagedChunkIDs)
}

func (f *davWriteFile) ReadFrom(r io.Reader) (int64, error) {
	var total int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			written, writeErr := f.Write(buf[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				f.writeErr = io.ErrShortWrite
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			f.writeErr = readErr
			return total, readErr
		}
	}
}

func (f *davWriteFile) Read([]byte) (int, error)           { return 0, os.ErrPermission }
func (f *davWriteFile) Readdir(int) ([]os.FileInfo, error) { return nil, os.ErrInvalid }
func (f *davWriteFile) Seek(int64, int) (int64, error)     { return 0, nil }
func (f *davWriteFile) Stat() (os.FileInfo, error)         { return &davInfo{name: path.Base(f.name)}, nil }

func (f *davWriteFile) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	written, err := f.buf.Write(p)
	if err != nil {
		f.writeErr = err
		return written, err
	}
	// Flush complete chunks as data arrives to avoid buffering the entire file.
	for f.buf.Len() >= chunk.ChunkSize {
		if err := f.flushChunk(); err != nil {
			f.writeErr = err
			return written, err
		}
	}
	return written, nil
}

// ---------- FileInfo ----------

type davInfo struct {
	name    string
	isDir   bool
	size    int64
	modTime time.Time
}

func (i *davInfo) Name() string { return i.name }
func (i *davInfo) Size() int64  { return i.size }
func (i *davInfo) Mode() os.FileMode {
	if i.isDir {
		return os.ModeDir | 0o755
	}
	return 0o644
}
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
	// TODO: replace MemLS with cluster-wide locking before advertising WebDAV
	// as safe for concurrent writes from multiple mounted clients.
	(&webdav.Handler{
		FileSystem: &davFS{store: s.store, chunks: s.chunks, nodeID: s.cfg.NodeID, srv: s},
		LockSystem: s.webDAVLocks,
		Prefix:     "/dav",
	}).ServeHTTP(w, r)
}
