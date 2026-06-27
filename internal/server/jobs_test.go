package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
