package server

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/pocketcluster/agent/internal/types"
)

func (s *Server) startJob(kind types.JobKind, title, message string, run func(context.Context, string) (types.JobStatus, string, error)) types.Job {
	job := types.Job{
		ID:        uuid.NewString(),
		Kind:      kind,
		Status:    types.JobPending,
		Title:     title,
		Message:   message,
		CreatedAt: time.Now(),
	}
	s.jobs.upsert(job)

	go func(jobID string) {
		s.jobs.upsert(types.Job{
			ID:      jobID,
			Kind:    kind,
			Status:  types.JobRunning,
			Title:   title,
			Message: message,
		})
		status, doneMessage, err := run(context.Background(), jobID)
		if err != nil {
			if status == "" {
				status = types.JobFailed
			}
			s.jobs.upsert(types.Job{
				ID:         jobID,
				Kind:       kind,
				Status:     status,
				Title:      title,
				Message:    doneMessage,
				Error:      err.Error(),
				FinishedAt: time.Now(),
			})
			return
		}
		if status == "" {
			status = types.JobDone
		}
		s.jobs.upsert(types.Job{
			ID:         jobID,
			Kind:       kind,
			Status:     status,
			Title:      title,
			Message:    doneMessage,
			FinishedAt: time.Now(),
		})
	}(job.ID)

	return job
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	writeOK(w, http.StatusOK, map[string]any{"jobs": s.jobs.list()})
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("jobId")
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "jobId required")
		return
	}
	job, ok := s.jobs.get(jobID)
	if !ok {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "job not found")
		return
	}
	writeOK(w, http.StatusOK, job)
}

func (s *Server) handleJobRescan(w http.ResponseWriter, r *http.Request) {
	job := s.startJob(types.JobRescan, "Rescanning health state", "Refreshing pool health from current metadata and reachable replicas.", func(ctx context.Context, jobID string) (types.JobStatus, string, error) {
		taskID := "job:" + jobID + ":rescan"
		s.trackSyncTask(taskID, types.SyncTaskIntegrityCheck, types.SyncTaskRunning, "Rescanning health state", "health", "Refreshing pool health snapshots.", "")
		s.runHealthScan(ctx)
		s.finishSyncTask(taskID, types.SyncTaskIntegrityCheck, "Rescanning health state", "health", "Health scan finished.")
		return types.JobDone, "Health scan completed.", nil
	})
	writeOK(w, http.StatusAccepted, job)
}

func (s *Server) handleJobRepairUnderReplicated(w http.ResponseWriter, r *http.Request) {
	job := s.startJob(types.JobRepairUnderReplicated, "Repairing under-replicated data", "Trying to restore target replica coverage for the current risky chunks.", func(ctx context.Context, jobID string) (types.JobStatus, string, error) {
		chunkMap := s.ChunkHealthSnapshot()
		targets := make([]string, 0, len(chunkMap))
		for chunkID, detail := range chunkMap {
			if detail.Status == types.ReplicaUnderReplicated || detail.Status == types.ReplicaRepairing {
				targets = append(targets, chunkID)
			}
		}
		if len(targets) == 0 {
			s.runHealthScan(ctx)
			return types.JobDone, "No under-replicated chunks need repair right now.", nil
		}

		nodes, err := s.store.ListNodes()
		if err != nil {
			return types.JobFailed, "Could not load node list for repair.", err
		}

		var blockedCount int
		var retryCount int
		for _, chunkID := range targets {
			taskID := "job:" + jobID + ":repair:" + chunkID
			s.trackSyncTask(taskID, types.SyncTaskReplicaRepair, types.SyncTaskRunning, "Repairing replica", chunkID, "Manual repair job is attempting this chunk.", "")
			s.MarkRepairing(chunkID, true)
			err := s.repairChunkReplicas(ctx, chunkID, nodes)
			s.MarkRepairing(chunkID, false)
			if err != nil {
				status := repairFailureStatus(err)
				s.failSyncTask(taskID, types.SyncTaskReplicaRepair, status, "Repairing replica", chunkID, "Manual repair pass did not finish for this chunk.", err.Error())
				if status == types.SyncTaskBlocked {
					blockedCount++
				} else {
					retryCount++
				}
				continue
			}
			s.finishSyncTask(taskID, types.SyncTaskReplicaRepair, "Repairing replica", chunkID, "Manual repair satisfied the target replica count.")
		}

		s.runHealthScan(ctx)
		switch {
		case blockedCount > 0 && retryCount == 0:
			return types.JobBlocked, "Repair is blocked until missing replica nodes return online.", nil
		case blockedCount > 0 || retryCount > 0:
			return types.JobRetrying, "Repair made partial progress, but some chunks still need another pass.", nil
		default:
			return types.JobDone, "Repair pass completed for all currently under-replicated chunks.", nil
		}
	})
	writeOK(w, http.StatusAccepted, job)
}
