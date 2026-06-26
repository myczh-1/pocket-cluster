package server

import (
	"encoding/json"
	"log"
	"net/http"
	"sort"
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

type syncTaskStore struct {
	mu sync.RWMutex
	m  map[string]*types.SyncTask
}

func newSyncTaskStore() *syncTaskStore {
	return &syncTaskStore{m: make(map[string]*types.SyncTask)}
}

func (s *syncTaskStore) upsert(task types.SyncTask) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	existing, ok := s.m[task.ID]
	if ok {
		if task.StartedAt.IsZero() {
			task.StartedAt = existing.StartedAt
		}
		if task.Title == "" {
			task.Title = existing.Title
		}
		if task.Target == "" {
			task.Target = existing.Target
		}
		if task.Kind == "" {
			task.Kind = existing.Kind
		}
	}
	if task.StartedAt.IsZero() {
		task.StartedAt = now
	}
	task.UpdatedAt = now

	copy := task
	if copy.Status == types.SyncTaskDone || copy.Status == types.SyncTaskFailed || copy.Status == types.SyncTaskBlocked {
		if copy.FinishedAt.IsZero() {
			copy.FinishedAt = now
		}
	} else {
		copy.FinishedAt = time.Time{}
	}
	s.m[copy.ID] = &copy
	s.pruneLocked(now)
}

func (s *syncTaskStore) list() []types.SyncTask {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]types.SyncTask, 0, len(s.m))
	for _, task := range s.m {
		list = append(list, *task)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].UpdatedAt.After(list[j].UpdatedAt)
	})
	return list
}

func (s *syncTaskStore) pruneLocked(now time.Time) {
	const keepDoneFor = 30 * time.Minute
	const maxTasks = 200

	if len(s.m) == 0 {
		return
	}

	for id, task := range s.m {
		if task.FinishedAt.IsZero() {
			continue
		}
		if now.Sub(task.FinishedAt) > keepDoneFor {
			delete(s.m, id)
		}
	}
	if len(s.m) <= maxTasks {
		return
	}

	list := make([]types.SyncTask, 0, len(s.m))
	for _, task := range s.m {
		list = append(list, *task)
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].UpdatedAt.Before(list[j].UpdatedAt)
	})
	for _, task := range list[:len(list)-maxTasks] {
		delete(s.m, task.ID)
	}
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
	syncTasks        *syncTaskStore
	peerHTTPClient   peerHTTPDoer
	webDAVLocks      webdav.LockSystem
	loginLimiter     *loginLimiter
	lastRecovery     time.Time // last time offline nodes were pinged for recovery
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
		syncTasks:      newSyncTaskStore(),
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

func writeOK(w http.ResponseWriter, status int, data any) {
	resp := types.APIResponse{OK: true}
	if data != nil {
		body, err := json.Marshal(data)
		if err != nil {
			log.Printf("writeOK marshal error: %v", err)
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "failed to encode response")
			return
		}
		resp.Data = body
	}
	writeJSON(w, status, resp)
}
