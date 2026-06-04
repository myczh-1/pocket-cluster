package server

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

const sessionTTL = 24 * time.Hour

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

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "POST required")
		return
	}
	if !s.cfg.HasPoolCredentials() {
		writeError(w, http.StatusBadRequest, "NOT_CONFIGURED", "pool credentials not set")
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
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
		return
	}
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
	http.SetCookie(w, &http.Cookie{
		Name:   "pc-session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true})
}

func (s *Server) handleGetAuthStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, types.APIResponse{OK: true, Data: mustMarshal(map[string]any{
		"has_credentials": s.cfg.HasPoolCredentials(),
		"username":        s.cfg.PoolUser,
	})})
}
