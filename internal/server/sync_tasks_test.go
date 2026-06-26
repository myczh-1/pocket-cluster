package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pocketcluster/agent/internal/chunk"
	"github.com/pocketcluster/agent/internal/store"
	"github.com/pocketcluster/agent/internal/types"
)

func TestHandleSyncTasksReturnsTrackedTasks(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	chunks := chunk.New(t.TempDir())
	if err := chunks.Init(); err != nil {
		t.Fatal(err)
	}
	srv := New(newTestConfig(t, "local"), st, chunks)
	session := loginTestSession(t, srv)

	srv.trackSyncTask("pull:node-a", types.SyncTaskMetadataPull, types.SyncTaskRunning, "Pulling metadata", "node-a", "Fetching remote events from this node.", "")
	srv.finishSyncTask("upload:file-1", types.SyncTaskUpload, "Uploading file", "/docs/report.pdf", "File content committed; replica repair continues in the background.")

	req := withAuth(httptest.NewRequest(http.MethodGet, "/api/sync/tasks", nil), session)
	res := httptest.NewRecorder()
	srv.Handler().ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}

	var body struct {
		OK   bool `json:"ok"`
		Data struct {
			Tasks []types.SyncTask `json:"tasks"`
		} `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if !body.OK {
		t.Fatalf("response not ok: %+v", body)
	}
	if len(body.Data.Tasks) != 2 {
		t.Fatalf("task count = %d, want 2", len(body.Data.Tasks))
	}
	if body.Data.Tasks[0].UpdatedAt.Before(body.Data.Tasks[1].UpdatedAt) {
		t.Fatalf("tasks not ordered by latest update: %+v", body.Data.Tasks)
	}
}

func TestRepairFailureStatus(t *testing.T) {
	if got := repairFailureStatus(nil); got != types.SyncTaskDone {
		t.Fatalf("nil error status = %q, want %q", got, types.SyncTaskDone)
	}
	if got := repairFailureStatus(assertErr("chunk unavailable: abc")); got != types.SyncTaskBlocked {
		t.Fatalf("chunk unavailable status = %q, want %q", got, types.SyncTaskBlocked)
	}
	if got := repairFailureStatus(assertErr("dial tcp timeout")); got != types.SyncTaskRetrying {
		t.Fatalf("generic failure status = %q, want %q", got, types.SyncTaskRetrying)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
