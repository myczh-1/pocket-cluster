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
	cfg             *config.Config
	store           *store.Store
	chunks          *chunk.Storage
	localIP         string
	logRing         *RingBuffer
	sessions        *sessionStore
	started         time.Time
	joinPollInterval time.Duration
}

type Option func(*Server)

func WithLocalIP(localIP string) Option {
	return func(s *Server) {
		s.localIP = localIP
	}
}

func WithLogRing(ring *RingBuffer) Option {
	return func(s *Server) {
		s.logRing = ring
	}
}
func WithJoinPollInterval(d time.Duration) Option {
	return func(s *Server) {
		s.joinPollInterval = d
	}
}
func New(cfg *config.Config, s *store.Store, c *chunk.Storage, opts ...Option) *Server {
	srv := &Server{cfg: cfg, store: s, chunks: c, sessions: newSessionStore(), started: time.Now()}
	for _, opt := range opts {
		opt(srv)
	}
	return srv
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
