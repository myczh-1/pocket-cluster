package server

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"sync"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/config"
	"github.com/pocketcluster/agent/internal/peernet"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
	"golang.org/x/net/webdav"
)

type peerHTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type uploadStatus struct {
	ID         string `json:"id"`
	FileName   string `json:"file_name"`
	TotalBytes int64  `json:"total_bytes"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

type uploadProgressStore struct {
	mu sync.RWMutex
	m  map[string]*uploadStatus
}

func newUploadProgressStore() *uploadProgressStore {
	return &uploadProgressStore{m: make(map[string]*uploadStatus)}
}

func (p *uploadProgressStore) add(status *uploadStatus) {
	p.mu.Lock()
	p.m[status.ID] = status
	p.mu.Unlock()
}

func (p *uploadProgressStore) set(uploadID, status, errMsg string, totalBytes int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	progress, ok := p.m[uploadID]
	if !ok {
		return
	}
	if status != "" {
		progress.Status = status
	}
	if errMsg != "" {
		progress.Error = errMsg
	}
	if totalBytes >= 0 {
		progress.TotalBytes = totalBytes
	}
}

func (p *uploadProgressStore) delete(uploadID string) {
	p.mu.Lock()
	delete(p.m, uploadID)
	p.mu.Unlock()
}

func (p *uploadProgressStore) list() []uploadStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	list := make([]uploadStatus, 0, len(p.m))
	for _, v := range p.m {
		list = append(list, *v)
	}
	return list
}

type Server struct {
	cfg              *config.Config
	store            *store.Store
	chunks           *chunk.Storage
	localIP          string
	logRing          *RingBuffer
	sessions         *sessionStore
	health           *healthScanner
	started          time.Time
	joinPollInterval time.Duration
	uploadProgress   *uploadProgressStore
	peerHTTPClient   peerHTTPDoer
	webDAVLocks      webdav.LockSystem
	loginLimiter     *loginLimiter
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

func WithPeerHTTPClient(client peerHTTPDoer) Option {
	return func(s *Server) {
		if client != nil {
			s.peerHTTPClient = client
		}
	}
}
func New(cfg *config.Config, s *store.Store, c *chunk.Storage, opts ...Option) *Server {
	srv := &Server{
		cfg:            cfg,
		store:          s,
		chunks:         c,
		sessions:       newSessionStore(),
		health:         newHealthScanner(),
		started:        time.Now(),
		uploadProgress: newUploadProgressStore(),
		peerHTTPClient: peernet.NewHTTPClient(),
		webDAVLocks:    webdav.NewMemLS(),
		loginLimiter:   newLoginLimiter(5, time.Minute),
	}
	for _, opt := range opts {
		opt(srv)
	}
	return srv
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, types.APIResponse{OK: false, Error: &types.APIError{Code: code, Message: msg}})
}

func mustMarshal(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic("mustMarshal: " + err.Error())
	}
	return b
}
