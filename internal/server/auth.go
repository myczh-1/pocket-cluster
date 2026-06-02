package server

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"
)

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" || r.URL.Path == "/api/join/request" {
			next.ServeHTTP(w, r)
			return
		}
		nodeID := r.Header.Get("X-Node-ID")
		if nodeID == "" {
			next.ServeHTTP(w, r)
			return
		}
		sigB64 := r.Header.Get("X-Signature")
		tsStr := r.Header.Get("X-Timestamp")
		if sigB64 == "" || tsStr == "" {
			next.ServeHTTP(w, r)
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
		msg := fmt.Sprintf("%s%s%s%s", r.Method, r.URL.Path, r.Header.Get("X-Node-ID"), tsStr)
		if !ed25519.Verify(ed25519.PublicKey(pubBytes), []byte(msg), sigBytes) {
			writeError(w, http.StatusUnauthorized, "SIGNATURE_INVALID", "signature verification failed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func SignRequest(privateKey ed25519.PrivateKey, method, path, nodeID string) (string, string) {
	ts := fmt.Sprint(time.Now().UnixMilli())
	msg := fmt.Sprintf("%s%s%s%s", method, path, nodeID, ts)
	sig := ed25519.Sign(privateKey, []byte(msg))
	return base64.StdEncoding.EncodeToString(sig), ts
}
