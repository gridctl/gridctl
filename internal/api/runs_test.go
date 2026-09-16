package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/runs"
)

func TestHandleRuns_NoStack(t *testing.T) {
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleRuns_ReadsAppendedRecords(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", "")
	srv := newTestServer(t)
	srv.SetStackName("demo")
	dir, err := runs.Dir("demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	line := `{"schema_version":1,"sequence":1,"attempt_id":"a1","returned_at":"2026-01-01T00:00:01Z","requested_name":"s__t","disposition":"completed","stage":"downstream","reason":"ok"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "runs.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	records, _ := payload["records"].([]any)
	if len(records) != 1 {
		t.Fatalf("records = %v", payload["records"])
	}
	row := records[0].(map[string]any)
	if row["attempt_id"] != "a1" {
		t.Fatalf("attempt_id = %v", row["attempt_id"])
	}
	if _, ok := row["arguments"]; ok {
		t.Fatal("arguments leaked")
	}
}

func TestHandleRuns_UnreadableIsNotEmptySuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GRIDCTL_HOME", "")
	srv := newTestServer(t)
	srv.SetStackName("demo")
	dir, err := runs.Dir("demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "runs.jsonl")
	if err := os.WriteFile(path, []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var payload struct {
		Records  []any          `json:"records"`
		Partial  bool           `json:"partial"`
		Warnings []runs.Warning `json:"warnings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Partial || len(payload.Records) != 0 || len(payload.Warnings) == 0 {
		t.Fatalf("expected partial warnings, got %+v", payload)
	}
}

func TestHandleRunsWipe_RejectsPerServer(t *testing.T) {
	srv := newTestServer(t)
	srv.SetStackName("demo")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodPost, "/api/runs/wipe?server=github", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestHandleRunsStatus_Notes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GRIDCTL_HOME", "")
	srv := newTestServer(t)
	srv.SetStackName("demo")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/runs/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"recordingNote", "privacyNote", "retentionNote", "historical_loss"} {
		if payload[key] == nil || payload[key] == "" {
			t.Fatalf("missing %s", key)
		}
	}
}

func TestHandleRuns_AuthProtected(t *testing.T) {
	srv := newTestServer(t)
	srv.SetAuth("bearer", "secret", "")
	srv.SetStackName("demo")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestPatchStackRuns_RoundTrip(t *testing.T) {
	src := []byte("version: \"1\"\nname: demo\nmcp-servers: []\n")
	enabled := true
	out, err := patchStackRuns(src, stackRunsRequest{Enabled: &enabled})
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "runs:") || !strings.Contains(got, "enabled: true") {
		t.Fatalf("patched yaml = %s", got)
	}
}

func TestRunCursorRoundTrip(t *testing.T) {
	c := runs.Cursor{WipeEpoch: 3, Sequence: 9, AttemptID: "a", ReturnedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	got, err := runs.DecodeCursor(runs.EncodeCursor(c))
	if err != nil {
		t.Fatal(err)
	}
	if got.WipeEpoch != c.WipeEpoch || got.Sequence != c.Sequence || got.AttemptID != c.AttemptID {
		t.Fatalf("%+v vs %+v", got, c)
	}
}
