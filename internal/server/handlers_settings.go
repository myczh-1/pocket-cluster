package server

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeOK(w, http.StatusOK, map[string]any{
		"tombstone_retention_hours": s.cfg.TombstoneRetentionHours,
	})
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TombstoneRetentionHours int `json:"tombstone_retention_hours"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}
	if req.TombstoneRetentionHours < 1 || req.TombstoneRetentionHours > 24*90 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "tombstone retention must be between 1 and 2160 hours")
		return
	}
	s.cfg.SetTombstoneRetentionHours(req.TombstoneRetentionHours)
	if err := s.cfg.Save(); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	writeOK(w, http.StatusOK, map[string]any{
		"tombstone_retention_hours": s.cfg.TombstoneRetentionHours,
	})
}
