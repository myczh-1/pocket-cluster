package server

import (
	"net/http"
	"strings"
)

// GET /api/webdav/info
func (s *Server) handleWebDAVInfo(w http.ResponseWriter, r *http.Request) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	host := r.Host
	if host == "" {
		host = "localhost:7788"
	}
	baseURL := scheme + "://" + host
	writeOK(w, http.StatusOK, map[string]any{
		"enabled":       true,
		"url":           baseURL + "/dav/",
		"path":          "/dav/",
		"auth_required": s.cfg.HasPoolCredentials(),
		"username":      s.cfg.PoolUser,
	})
}
