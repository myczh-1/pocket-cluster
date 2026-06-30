package server

import (
	"context"
	"fmt"
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

// handleJobIntegrityCheck re-verifies every locally stored chunk by recomputing
// its SHA-256 hash on disk and comparing against the recorded chunk ID. Chunks
// whose file is absent are reported as missing; chunks whose hash no longer
// matches are reported as corrupt. The verified_at timestamp of the local
// replica is refreshed for every chunk that passes verification.
func (s *Server) handleJobIntegrityCheck(w http.ResponseWriter, r *http.Request) {
	job := s.startJob(types.JobIntegrityCheck, "Verifying chunk integrity", "Recomputing hashes for every local chunk to detect corruption or loss.", func(ctx context.Context, jobID string) (types.JobStatus, string, error) {
		taskID := "job:" + jobID + ":integrity"
		s.trackSyncTask(taskID, types.SyncTaskIntegrityCheck, types.SyncTaskRunning, "Verifying chunk integrity", "local", "Recomputing chunk hashes on disk.", "")

		chunks, err := s.store.ListChunks()
		if err != nil {
			s.failSyncTask(taskID, types.SyncTaskIntegrityCheck, types.SyncTaskFailed, "Verifying chunk integrity", "local", "Could not load chunk list for verification.", err.Error())
			return types.JobFailed, "Could not load chunk list for verification.", err
		}

		var verified, missing, corrupt int
		var firstCorrupt, firstMissing string
		now := time.Now()

		for _, c := range chunks {
			if !s.chunks.Exists(c.ChunkID) {
				missing++
				if firstMissing == "" {
					firstMissing = c.ChunkID
				}
				continue
			}
			if err := s.chunks.Verify(c.ChunkID); err != nil {
				corrupt++
				if firstCorrupt == "" {
					firstCorrupt = c.ChunkID
				}
				continue
			}
			verified++
			// Refresh verified_at on the local replica so operators can see
			// the last time each chunk was integrity-checked.
			replica := &types.Replica{ChunkID: c.ChunkID, NodeID: s.cfg.NodeID, Status: "available", StoredAt: c.StoredAt, VerifiedAt: now}
			_ = s.store.UpsertReplica(replica)
		}

		s.runHealthScan(ctx)

		switch {
		case corrupt > 0:
			msg := fmt.Sprintf("Integrity check found %d corrupt chunk(s) and %d missing chunk(s) out of %d total.", corrupt, missing, len(chunks))
			if firstCorrupt != "" {
				msg += " First corrupt chunk: " + firstCorrupt + "."
			}
			if firstMissing != "" {
				msg += " First missing chunk: " + firstMissing + "."
			}
			s.failSyncTask(taskID, types.SyncTaskIntegrityCheck, types.SyncTaskFailed, "Verifying chunk integrity", "local", msg, "")
			return types.JobFailed, msg, nil
		case missing > 0:
			msg := fmt.Sprintf("Integrity check verified %d chunk(s); %d chunk(s) are missing from local disk. Run repair to restore coverage.", verified, missing)
			if firstMissing != "" {
				msg += " First missing chunk: " + firstMissing + "."
			}
			s.finishSyncTask(taskID, types.SyncTaskIntegrityCheck, "Verifying chunk integrity", "local", msg)
			return types.JobRetrying, msg, nil
		default:
			msg := fmt.Sprintf("All %d local chunk(s) passed integrity verification.", verified)
			s.finishSyncTask(taskID, types.SyncTaskIntegrityCheck, "Verifying chunk integrity", "local", msg)
			return types.JobDone, msg, nil
		}
	})
	writeOK(w, http.StatusAccepted, job)
}

func (s *Server) handleJobPurgeRetainedData(w http.ResponseWriter, r *http.Request) {
	job := s.startJob(types.JobPurgeRetainedData, "Purging retained deleted data", "Removing tombstoned file metadata immediately and reclaiming unreferenced chunk replicas.", func(ctx context.Context, jobID string) (types.JobStatus, string, error) {
		taskID := "job:" + jobID + ":retention-purge"
		s.trackSyncTask(taskID, types.SyncTaskRetentionPurge, types.SyncTaskRunning, "Purging retained deleted data", "retained-data", "Force-cleaning deleted file tombstones and unreferenced chunks.", "")
		purged, err := s.PurgeRetainedDataContext(ctx)
		if err != nil {
			s.failSyncTask(taskID, types.SyncTaskRetentionPurge, types.SyncTaskFailed, "Purging retained deleted data", "retained-data", "Immediate cleanup failed.", err.Error())
			return types.JobFailed, "Immediate cleanup failed.", err
		}
		s.runHealthScan(ctx)
		if purged == 0 {
			s.finishSyncTask(taskID, types.SyncTaskRetentionPurge, "Purging retained deleted data", "retained-data", "No retained deleted files needed cleanup.")
			return types.JobDone, "No retained deleted files needed cleanup.", nil
		}
		msg := fmt.Sprintf("Purged %d deleted file tombstone(s) and triggered immediate chunk reclamation.", purged)
		s.finishSyncTask(taskID, types.SyncTaskRetentionPurge, "Purging retained deleted data", "retained-data", msg)
		return types.JobDone, msg, nil
	})
	writeOK(w, http.StatusAccepted, job)
}
