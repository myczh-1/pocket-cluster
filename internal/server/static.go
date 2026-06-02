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
	mux.HandleFunc("POST /api/join/request", s.handleJoinRequest)
	mux.HandleFunc("POST /api/join/approve", s.handleJoinApprove)
	mux.HandleFunc("GET /api/files", s.handleListFiles)
	mux.HandleFunc("POST /api/files/upload", s.handleUpload)
	mux.HandleFunc("GET /api/files/download", s.handleDownload)
	mux.HandleFunc("GET /api/chunks/{hash}", s.handleGetChunk)
	mux.HandleFunc("POST /api/chunks", s.handleStoreChunk)
	mux.HandleFunc("GET /api/events", s.handleGetEvents)
	mux.HandleFunc("POST /api/events/push", s.handlePushEvents)
	mux.HandleFunc("GET /api/health", s.handleHealth)

	sub, _ := fs.Sub(webFS, "web-dist")
	fileServer := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	return s.authMiddleware(mux)
}
