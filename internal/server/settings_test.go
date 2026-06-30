package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pocketcluster/agent/internal/types"
)

func TestUpdateSettingsChangesTombstoneRetention(t *testing.T) {
	s := newTestHealthServer(t)
	session := loginTestSession(t, s)

	res := httptest.NewRecorder()
	req := withAuth(httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader(`{"tombstone_retention_hours":24}`)), session)
	req.Header.Set("Content-Type", "application/json")
	s.Handler().ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("update settings status = %d: %s", res.Code, res.Body.String())
	}
	if got := int(s.cfg.TombstoneRetentionDuration().Hours()); got != 24 {
		t.Fatalf("retention hours = %d, want 24", got)
	}

	getRes := httptest.NewRecorder()
	getReq := withAuth(httptest.NewRequest(http.MethodGet, "/api/settings", nil), session)
	s.Handler().ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get settings status = %d: %s", getRes.Code, getRes.Body.String())
	}
	var envelope types.APIResponse
	if err := json.Unmarshal(getRes.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		TombstoneRetentionHours int `json:"tombstone_retention_hours"`
	}
	if err := json.Unmarshal(envelope.Data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.TombstoneRetentionHours != 24 {
		t.Fatalf("settings payload = %+v, want retention 24", payload)
	}
}
