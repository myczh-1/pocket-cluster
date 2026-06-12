package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

const (
	sessionTTL             = 24 * time.Hour
	sessionCleanupInterval = time.Hour
)

// TODO: persist sessions if restart-stable browser login becomes required.
// The current in-memory store intentionally logs users out on agent restart.
type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]time.Time)}
}

func (s *sessionStore) create() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	return token
}

func (s *sessionStore) isValid(token string) bool {
	s.mu.RLock()
	expires, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	if time.Now().After(expires) {
		s.mu.Lock()
		delete(s.sessions, token)
		s.mu.Unlock()
		return false
	}
	return true
}

func (s *sessionStore) delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

type loginLimiter struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	failures map[string][]time.Time
}

func newLoginLimiter(max int, window time.Duration) *loginLimiter {
	return &loginLimiter{max: max, window: window, failures: make(map[string][]time.Time)}
}

func loginClientKey(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil && host != "" {
		return host
	}
	if remoteAddr != "" {
		return remoteAddr
	}
	return "unknown"
}

func (l *loginLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	failures := l.activeFailuresLocked(key, now)
	return len(failures) < l.max
}

func (l *loginLimiter) recordFailure(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	failures := l.activeFailuresLocked(key, now)
	failures = append(failures, now)
	l.failures[key] = failures
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	delete(l.failures, key)
	l.mu.Unlock()
}

func (l *loginLimiter) activeFailuresLocked(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	failures := l.failures[key]
	firstActive := 0
	for firstActive < len(failures) && failures[firstActive].Before(cutoff) {
		firstActive++
	}
	if firstActive > 0 {
		failures = append(failures[:0], failures[firstActive:]...)
	}
	if len(failures) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = failures
	return failures
}

func (s *sessionStore) cleanupExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, expires := range s.sessions {
		if now.After(expires) {
			delete(s.sessions, token)
		}
	}
}

func (s *Server) StartSessionCleanup(ctx context.Context) {
	ticker := time.NewTicker(sessionCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s.sessions.cleanupExpired(now)
		}
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}
	if !s.cfg.HasPoolCredentials() {
		writeError(w, http.StatusBadRequest, "NOT_CONFIGURED", "pool credentials not set")
		return
	}
	clientKey := loginClientKey(r.RemoteAddr)
	now := time.Now()
	if !s.loginLimiter.allow(clientKey, now) {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many failed login attempts")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if req.Username != s.cfg.PoolUser || !s.cfg.CheckPoolPassword(req.Password) {
		s.loginLimiter.recordFailure(clientKey, now)
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
		return
	}
	s.loginLimiter.reset(clientKey)
	sessionToken := s.sessions.create()
	http.SetCookie(w, &http.Cookie{
		Name:     "pc-session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]string{
		"username": s.cfg.PoolUser,
	})})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("pc-session"); err == nil {
		s.sessions.delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "pc-session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true})
}

func (s *Server) handleGetAuthStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"has_credentials": s.cfg.HasPoolCredentials(),
		"username":        s.cfg.PoolUser,
	})})
}
