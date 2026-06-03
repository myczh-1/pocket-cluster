package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/config"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

type Server struct {
	cfg     *config.Config
	store   *store.Store
	chunks  *chunk.Storage
	localIP string // optional: local IP address override
	started time.Time
}

func New(cfg *config.Config, s *store.Store, c *chunk.Storage, localIP ...string) *Server {
	ip := ""
	if len(localIP) > 0 {
		ip = localIP[0]
	}
	return &Server{cfg: cfg, store: s, chunks: c, localIP: ip, started: time.Now()}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, types.APIResponse{OK: false, Error: &types.APIError{Code: code, Message: msg}})
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
