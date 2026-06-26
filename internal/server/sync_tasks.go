package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/pocketcluster/agent/internal/types"
)

func (s *Server) trackSyncTask(id string, kind types.SyncTaskKind, status types.SyncTaskStatus, title, target, message, errMsg string) {
	if s.syncTasks == nil || id == "" {
		return
	}
	s.syncTasks.upsert(types.SyncTask{
		ID:      id,
		Kind:    kind,
		Status:  status,
		Title:   title,
		Target:  target,
		Message: message,
		Error:   errMsg,
	})
}

func (s *Server) finishSyncTask(id string, kind types.SyncTaskKind, title, target, message string) {
	if s.syncTasks == nil || id == "" {
		return
	}
	s.syncTasks.upsert(types.SyncTask{
		ID:         id,
		Kind:       kind,
		Status:     types.SyncTaskDone,
		Title:      title,
		Target:     target,
		Message:    message,
		FinishedAt: time.Now(),
	})
}

func (s *Server) failSyncTask(id string, kind types.SyncTaskKind, status types.SyncTaskStatus, title, target, message, errMsg string) {
	if s.syncTasks == nil || id == "" {
		return
	}
	if status == "" {
		status = types.SyncTaskFailed
	}
	s.syncTasks.upsert(types.SyncTask{
		ID:         id,
		Kind:       kind,
		Status:     status,
		Title:      title,
		Target:     target,
		Message:    message,
		Error:      errMsg,
		FinishedAt: time.Now(),
	})
}

func repairFailureStatus(err error) types.SyncTaskStatus {
	if err == nil {
		return types.SyncTaskDone
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no available replica") || strings.Contains(msg, "chunk unavailable") {
		return types.SyncTaskBlocked
	}
	return types.SyncTaskRetrying
}

func (s *Server) handleSyncTasks(w http.ResponseWriter, r *http.Request) {
	list := s.syncTasks.list()
	writeOK(w, http.StatusOK, map[string]any{"tasks": list})
}
