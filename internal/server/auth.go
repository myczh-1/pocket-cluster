package server

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	authBodySHA256Header = "X-Body-SHA256"
	authSkew             = 5 * time.Minute
	emptyBodySHA256      = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
)

// publicPaths are accessible without any authentication.
var publicPaths = map[string]bool{
	"/api/health":       true,
	"/api/join/request": true,
}

// loginPaths are accessible without a session (used during login flow).
var loginPaths = map[string]bool{
	"/api/auth/login":  true,
	"/api/auth/status": true,
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/api/join" {
			if s.cfg.ClusterID == "" && !s.cfg.HasPoolCredentials() {
				next.ServeHTTP(w, r)
				return
			}
			if s.isWebSession(r) {
				next.ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "login required")
			return
		}
		if requiresPeerSignature(r) {
			s.verifyPeerSignature(w, r, next)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		if !s.cfg.HasPoolCredentials() {
			next.ServeHTTP(w, r)
			return
		}
		if loginPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		if s.isWebSession(r) {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusUnauthorized, "AUTH_REQUIRED", "login required")
	})
}

func (s *Server) isWebSession(r *http.Request) bool {
	cookie, err := r.Cookie("pc-session")
	if err != nil || cookie.Value == "" {
		return false
	}
	return s.sessions.isValid(cookie.Value)
}

func (s *Server) verifyPeerSignature(w http.ResponseWriter, r *http.Request, next http.Handler) {
	nodeID := r.Header.Get("X-Node-ID")
	sigB64 := r.Header.Get("X-Signature")
	tsStr := r.Header.Get("X-Timestamp")
	bodyHash := r.Header.Get(authBodySHA256Header)
	if nodeID == "" || sigB64 == "" || tsStr == "" || bodyHash == "" {
		writeError(w, http.StatusUnauthorized, "SIGNATURE_INVALID", "peer signature required")
		return
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "SIGNATURE_INVALID", "invalid timestamp")
		return
	}
	if delta := time.Since(time.UnixMilli(ts)); delta > authSkew || delta < -authSkew {
		writeError(w, http.StatusUnauthorized, "SIGNATURE_INVALID", "timestamp outside allowed skew")
		return
	}
	n, err := s.store.GetNode(nodeID)
	if err != nil || !n.Trusted {
		writeError(w, http.StatusUnauthorized, "SIGNATURE_INVALID", "unknown or untrusted node")
		return
	}
	pubBytes, err := base64.StdEncoding.DecodeString(n.PublicKey)
	if err != nil || len(pubBytes) != ed25519.PublicKeySize {
		writeError(w, http.StatusUnauthorized, "SIGNATURE_INVALID", "invalid public key")
		return
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "SIGNATURE_INVALID", "invalid signature format")
		return
	}
	msg := signatureMessage(r.Method, r.URL.RequestURI(), bodyHash, nodeID, tsStr)
	if !ed25519.Verify(ed25519.PublicKey(pubBytes), []byte(msg), sigBytes) {
		writeError(w, http.StatusUnauthorized, "SIGNATURE_INVALID", "signature verification failed")
		return
	}
	next.ServeHTTP(w, r)
}
func (s *Server) signPeerRequest(req *http.Request, bodyHash string) error {
	privateKey, err := s.cfg.Ed25519PrivateKey()
	if err != nil {
		return err
	}
	sig, ts := SignRequest(privateKey, req.Method, req.URL.RequestURI(), bodyHash, s.cfg.NodeID)
	req.Header.Set("X-Node-ID", s.cfg.NodeID)
	req.Header.Set("X-Signature", sig)
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set(authBodySHA256Header, bodyHash)
	return nil
}

func SignRequest(privateKey ed25519.PrivateKey, method, requestURI, bodyHash, nodeID string) (string, string) {
	ts := fmt.Sprint(time.Now().UnixMilli())
	msg := signatureMessage(method, requestURI, bodyHash, nodeID, ts)
	sig := ed25519.Sign(privateKey, []byte(msg))
	return base64.StdEncoding.EncodeToString(sig), ts
}

func signatureMessage(method, requestURI, bodyHash, nodeID, timestamp string) string {
	return strings.Join([]string{method, requestURI, bodyHash, nodeID, timestamp}, "\n")
}

func requiresPeerSignature(r *http.Request) bool {
	if r.URL.Path == "/api/events" || r.URL.Path == "/api/events/push" || r.URL.Path == "/api/chunks" || r.URL.Path == "/api/snapshot" {
		return true
	}
	return strings.HasPrefix(r.URL.Path, "/api/chunks/")
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
