package server

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed web-dist/*
var webFS embed.FS

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/node/info", s.handleNodeInfo)
	mux.HandleFunc("GET /api/nodes", s.handleListNodes)
	mux.HandleFunc("GET /api/nodes/discovered", s.handleListDiscovered)
	mux.HandleFunc("POST /api/invites", s.handleCreateInvite)
	mux.HandleFunc("POST /api/cluster", s.handleCreateCluster)
	mux.HandleFunc("POST /api/join", s.handleJoinCluster)
	mux.HandleFunc("POST /api/join/request", s.handleJoinRequest)
	mux.HandleFunc("POST /api/join/approve/{nodeId}", s.handleJoinApprove)
	mux.HandleFunc("GET /api/join/pending", s.handleListPendingJoins)
	mux.HandleFunc("GET /api/files", s.handleListFiles)
	mux.HandleFunc("POST /api/files/upload", s.handleUpload)
	mux.HandleFunc("DELETE /api/files", s.handleDelete)
	mux.HandleFunc("PATCH /api/files/rename", s.handleRename)
	mux.HandleFunc("GET /api/files/download", s.handleDownload)
	mux.HandleFunc("GET /api/chunks/{hash}", s.handleGetChunk)
	mux.HandleFunc("HEAD /api/chunks/{hash}", s.handleHeadChunk)
	mux.HandleFunc("POST /api/chunks", s.handleStoreChunk)
	mux.HandleFunc("GET /api/events", s.handleGetEvents)
	mux.HandleFunc("POST /api/events/push", s.handlePushEvents)
	mux.HandleFunc("GET /api/snapshot", s.handleSnapshot)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/network/scan", s.handleScanNetwork)
	mux.HandleFunc("GET /api/logs", s.handleGetLogs)
	mux.HandleFunc("GET /api/agent/logs", s.handleAgentLogs)
	mux.HandleFunc("GET /api/local/files", s.handleListLocalFiles)
	mux.HandleFunc("POST /api/local/migrate", s.handleMigrateLocalFile)
	mux.HandleFunc("GET /api/auth/status", s.handleGetAuthStatus)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/uploads", s.handleUploadProgress)
	mux.HandleFunc("GET /api/health/summary", s.handleHealthSummary)
	mux.HandleFunc("GET /api/health/chunks", s.handleHealthChunks)
	mux.HandleFunc("GET /api/health/chunks/{hash}", s.handleHealthChunkDetail)
	mux.HandleFunc("GET /api/health/files/{fileId}", s.handleHealthFileDetail)

	sub, _ := fs.Sub(webFS, "web-dist")
	fileServer := http.FileServer(http.FS(sub))

	index, err := sub.Open("index.html")
	if err != nil {
		panic(err)
	}
	index.Close()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}
		if shouldServeIndex(sub, r.URL.Path) {
			http.ServeFileFS(w, r, sub, "index.html")
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	// WebDAV with Basic Auth
	mux.HandleFunc("/dav/", func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.HasPoolCredentials() {
			user, pass, ok := r.BasicAuth()
			if !ok || user != s.cfg.PoolUser || !s.cfg.CheckPoolPassword(pass) {
				w.Header().Set("WWW-Authenticate", `Basic realm="PocketCluster"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		s.handleWebDAV(w, r)
	})

	return s.authMiddleware(mux)
}

func shouldServeIndex(files fs.FS, urlPath string) bool {
	if urlPath == "/" {
		return false
	}
	path := strings.TrimPrefix(urlPath, "/")
	if path == "" || strings.Contains(path, "..") {
		return true
	}
	f, err := files.Open(path)
	if err == nil {
		f.Close()
		return false
	}
	return !strings.Contains(path[strings.LastIndex(path, "/")+1:], ".")
}
