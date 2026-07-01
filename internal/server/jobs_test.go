package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func newJobsTestServer(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	return New(newTestConfig(t, "local"), st, chunks)
}

func TestJobRescanLifecycle(t *testing.T) {
	srv := newJobsTestServer(t)
	session := loginTestSession(t, srv)

	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/jobs/rescan", nil), session)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusAccepted, res.Body.String())
	}

	var envelope types.APIResponse
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var job types.Job
	if err := json.Unmarshal(envelope.Data, &job); err != nil {
		t.Fatal(err)
	}
	if job.Kind != types.JobRescan {
		t.Fatalf("job kind = %q, want %q", job.Kind, types.JobRescan)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := srv.jobs.get(job.ID)
		if ok && got.Status == types.JobDone {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := srv.jobs.get(job.ID)
	t.Fatalf("job did not finish: %+v", got)
}

func TestListJobsReturnsTriggeredJobs(t *testing.T) {
	srv := newJobsTestServer(t)
	session := loginTestSession(t, srv)
	srv.jobs.upsert(types.Job{ID: "job-1", Kind: types.JobRescan, Status: types.JobDone, Title: "Rescanning", CreatedAt: time.Now().Add(-time.Minute), FinishedAt: time.Now()})
	srv.jobs.upsert(types.Job{ID: "job-2", Kind: types.JobRepairUnderReplicated, Status: types.JobRunning, Title: "Repairing", CreatedAt: time.Now()})

	req := withAuth(httptest.NewRequest(http.MethodGet, "/api/jobs", nil), session)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}

	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Jobs []types.Job `json:"jobs"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data.Jobs) != 2 {
		t.Fatalf("job count = %d, want 2", len(envelope.Data.Jobs))
	}
	if envelope.Data.Jobs[0].ID != "job-2" {
		t.Fatalf("expected latest-updated job first, got %+v", envelope.Data.Jobs)
	}
}

func TestGetJobReturnsNotFoundForUnknownID(t *testing.T) {
	srv := newJobsTestServer(t)
	session := loginTestSession(t, srv)

	req := withAuth(httptest.NewRequest(http.MethodGet, "/api/jobs/missing", nil), session)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusNotFound)
	}
}

func TestJobIntegrityCheckAllVerified(t *testing.T) {
	srv := newJobsTestServer(t)
	session := loginTestSession(t, srv)

	hash, _, err := srv.chunks.Store(bytes.NewReader([]byte("integrity payload")))
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.store.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: 17, StoredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/jobs/integrity-check", nil), session)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusAccepted, res.Body.String())
	}

	var envelope types.APIResponse
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var job types.Job
	if err := json.Unmarshal(envelope.Data, &job); err != nil {
		t.Fatal(err)
	}
	if job.Kind != types.JobIntegrityCheck {
		t.Fatalf("job kind = %q, want %q", job.Kind, types.JobIntegrityCheck)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := srv.jobs.get(job.ID)
		if ok && got.Status == types.JobDone {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := srv.jobs.get(job.ID)
	t.Fatalf("job did not finish as done: %+v", got)
}

func TestJobIntegrityCheckDetectsCorruptChunk(t *testing.T) {
	srv := newJobsTestServer(t)
	session := loginTestSession(t, srv)

	hash, _, err := srv.chunks.Store(bytes.NewReader([]byte("original content")))
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.store.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: 16, StoredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	// Corrupt the chunk file on disk so its hash no longer matches the ID.
	if err := os.WriteFile(srv.chunks.Path(hash), []byte("tampered!!!!!!!"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/jobs/integrity-check", nil), session)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusAccepted, res.Body.String())
	}

	var envelope types.APIResponse
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var job types.Job
	if err := json.Unmarshal(envelope.Data, &job); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := srv.jobs.get(job.ID)
		if ok && (got.Status == types.JobFailed || got.Status == types.JobDone) {
			if got.Status != types.JobFailed {
				t.Fatalf("expected JobFailed for corrupt chunk, got %+v", got)
			}
			if !strings.Contains(got.Message, "corrupt") {
				t.Fatalf("expected message to mention corruption, got %q", got.Message)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := srv.jobs.get(job.ID)
	t.Fatalf("job did not finish: %+v", got)
}

func TestJobIntegrityCheckDetectsMissingChunk(t *testing.T) {
	srv := newJobsTestServer(t)
	session := loginTestSession(t, srv)

	hash, _, err := srv.chunks.Store(bytes.NewReader([]byte("will be removed")))
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.store.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: 15, StoredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	// Remove the chunk file from disk to simulate data loss.
	if err := srv.chunks.Remove(hash); err != nil {
		t.Fatal(err)
	}

	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/jobs/integrity-check", nil), session)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusAccepted, res.Body.String())
	}

	var envelope types.APIResponse
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var job types.Job
	if err := json.Unmarshal(envelope.Data, &job); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := srv.jobs.get(job.ID)
		if ok && (got.Status == types.JobRetrying || got.Status == types.JobDone || got.Status == types.JobFailed) {
			if got.Status != types.JobRetrying {
				t.Fatalf("expected JobRetrying for missing chunk, got %+v", got)
			}
			if !strings.Contains(got.Message, "missing") {
				t.Fatalf("expected message to mention missing, got %q", got.Message)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := srv.jobs.get(job.ID)
	t.Fatalf("job did not finish: %+v", got)
}

func TestJobPurgeRetainedDataPurgesDeletedFilesImmediately(t *testing.T) {
	srv := newJobsTestServer(t)
	session := loginTestSession(t, srv)

	hash, _, err := srv.chunks.Store(bytes.NewReader([]byte("retained payload")))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := srv.store.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: 16, StoredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.UpsertReplica(&types.Replica{ChunkID: hash, NodeID: srv.cfg.NodeID, Status: "available", StoredAt: now, VerifiedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.UpsertFile(&types.File{
		FileID:     "deleted-file",
		Name:       "old.txt",
		Path:       "/old.txt",
		SizeBytes:  16,
		VersionID:  "v1",
		ChunkIDs:   []string{hash},
		CreatedAt:  now,
		ModifiedAt: now,
		ModifiedBy: srv.cfg.NodeID,
		Deleted:    true,
	}); err != nil {
		t.Fatal(err)
	}

	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/jobs/purge-retained-data", nil), session)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusAccepted, res.Body.String())
	}

	var envelope types.APIResponse
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var job types.Job
	if err := json.Unmarshal(envelope.Data, &job); err != nil {
		t.Fatal(err)
	}
	if job.Kind != types.JobPurgeRetainedData {
		t.Fatalf("job kind = %q, want %q", job.Kind, types.JobPurgeRetainedData)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := srv.jobs.get(job.ID)
		if ok && got.Status == types.JobDone {
			if _, err := srv.store.GetFileByID("deleted-file"); err == nil {
				t.Fatal("deleted file tombstone still exists after purge job")
			}
			if srv.chunks.Exists(hash) {
				t.Fatal("retained chunk file still exists after purge job")
			}
			if _, err := srv.store.GetChunk(hash); err == nil {
				t.Fatal("retained chunk metadata still exists after purge job")
			}
			events, err := srv.store.GetEventsSince("", 20)
			if err != nil {
				t.Fatal(err)
			}
			var eventTypes []string
			for _, event := range events {
				eventTypes = append(eventTypes, string(event.Type))
			}
			gotTypes := strings.Join(eventTypes, ",")
			if !strings.Contains(gotTypes, string(types.EventFilePurge)) {
				t.Fatalf("expected %s event, got %s", types.EventFilePurge, gotTypes)
			}
			if !strings.Contains(gotTypes, string(types.EventChunkReplicaRemove)) {
				t.Fatalf("expected %s event, got %s", types.EventChunkReplicaRemove, gotTypes)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := srv.jobs.get(job.ID)
	t.Fatalf("purge job did not finish: %+v", got)
}

func TestJobPurgeRetainedDataPurgesDeletedDirectoriesImmediately(t *testing.T) {
	srv := newJobsTestServer(t)
	session := loginTestSession(t, srv)

	hash, _, err := srv.chunks.Store(bytes.NewReader([]byte("nested payload")))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := srv.store.UpsertChunk(&types.Chunk{ChunkID: hash, SizeBytes: 14, StoredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.UpsertReplica(&types.Replica{ChunkID: hash, NodeID: srv.cfg.NodeID, Status: "available", StoredAt: now, VerifiedAt: now}); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []types.File{
		{
			FileID:     "dir",
			Name:       "photos",
			Path:       "/photos",
			IsDir:      true,
			CreatedAt:  now,
			ModifiedAt: now,
			ModifiedBy: srv.cfg.NodeID,
			Deleted:    true,
		},
		{
			FileID:     "child",
			Name:       "cat.jpg",
			Path:       "/photos/cat.jpg",
			SizeBytes:  14,
			VersionID:  "v1",
			ChunkIDs:   []string{hash},
			CreatedAt:  now,
			ModifiedAt: now,
			ModifiedBy: srv.cfg.NodeID,
			Deleted:    true,
		},
	} {
		entry := entry
		if err := srv.store.UpsertFile(&entry); err != nil {
			t.Fatal(err)
		}
	}

	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/jobs/purge-retained-data", nil), session)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusAccepted, res.Body.String())
	}

	var envelope types.APIResponse
	if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var job types.Job
	if err := json.Unmarshal(envelope.Data, &job); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, ok := srv.jobs.get(job.ID)
		if ok && got.Status == types.JobDone {
			if _, err := srv.store.GetFileByID("dir"); err == nil {
				t.Fatal("deleted directory tombstone still exists after purge job")
			}
			if _, err := srv.store.GetFileByID("child"); err == nil {
				t.Fatal("deleted child file tombstone still exists after purge job")
			}
			if srv.chunks.Exists(hash) {
				t.Fatal("retained chunk file still exists after directory purge job")
			}
			events, err := srv.store.GetEventsSince("", 20)
			if err != nil {
				t.Fatal(err)
			}
			var eventTypes []string
			for _, event := range events {
				eventTypes = append(eventTypes, string(event.Type))
			}
			gotTypes := strings.Join(eventTypes, ",")
			if !strings.Contains(gotTypes, string(types.EventDirPurge)) {
				t.Fatalf("expected %s event, got %s", types.EventDirPurge, gotTypes)
			}
			if !strings.Contains(gotTypes, string(types.EventFilePurge)) {
				t.Fatalf("expected %s event, got %s", types.EventFilePurge, gotTypes)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := srv.jobs.get(job.ID)
	t.Fatalf("directory purge job did not finish: %+v", got)
}
