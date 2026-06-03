package server

import (
	"net/http"
	"sync"

	"github.com/pocketcluster/agent/internal/types"
)

type RingBuffer struct {
	mu   sync.Mutex
	buf  []string
	pos  int
	size int
	full bool
}

func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		buf:  make([]string, size),
		size: size,
	}
}

func (r *RingBuffer) Add(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.pos] = line
	r.pos = (r.pos + 1) % r.size
	if r.pos == 0 {
		r.full = true
	}
}

func (r *RingBuffer) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		result := make([]string, r.pos)
		copy(result, r.buf[:r.pos])
		return result
	}
	result := make([]string, r.size)
	copy(result, r.buf[r.pos:])
	copy(result[r.size-r.pos:], r.buf[:r.pos])
	return result
}

func (s *Server) handleAgentLogs(w http.ResponseWriter, r *http.Request) {
	if s.logRing == nil {
		writeJSON(w, http.StatusOK, types.APIResponse{
			OK:   true,
			Data: mustMarshal(map[string]any{"lines": []string{}}),
		})
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{
		OK:   true,
		Data: mustMarshal(map[string]any{"lines": s.logRing.Lines()}),
	})
}
