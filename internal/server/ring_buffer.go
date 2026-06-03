package server

import (
	"net/http"

	"github.com/pocketcluster/agent/internal/types"
)

// LogRing is a ring buffer for agent log lines, set by main.
var LogRing *RingBuffer

type RingBuffer struct {
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
	r.buf[r.pos] = line
	r.pos = (r.pos + 1) % r.size
	if r.pos == 0 {
		r.full = true
	}
}

func (r *RingBuffer) Lines() []string {
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
	if LogRing == nil {
		writeJSON(w, http.StatusOK, types.APIResponse{
			OK:   true,
			Data: mustMarshal(map[string]any{"lines": []string{}}),
		})
		return
	}
	writeJSON(w, http.StatusOK, types.APIResponse{
		OK:   true,
		Data: mustMarshal(map[string]any{"lines": LogRing.Lines()}),
	})
}
