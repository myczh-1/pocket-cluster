package server

import (
	"net/http"

	"github.com/pocketcluster/agent/internal/types"
)

func (s *Server) handleGetLogs(w http.ResponseWriter, r *http.Request) {
	events, err := s.store.GetEventsSince("", 50)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	type logEntry struct {
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		NodeID    string `json:"node_id"`
		Detail    string `json:"detail"`
	}

	logs := make([]logEntry, 0, len(events))
	for _, e := range events {
		detail := string(e.Payload)
		if len(detail) > 200 {
			detail = detail[:200] + "…"
		}
		logs = append(logs, logEntry{
			Timestamp: e.Timestamp.Format("15:04:05"),
			Type:      string(e.Type),
			NodeID:    e.NodeID,
			Detail:    detail,
		})
	}

	writeJSON(w, http.StatusOK, types.APIResponse{
		OK:   true,
		Data: mustMarshal(map[string]any{"entries": logs}),
	})
}
