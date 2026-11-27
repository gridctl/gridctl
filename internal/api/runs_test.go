package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/gridctl/gridctl/pkg/state"
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

// seedDisabledDaemon writes a live state file owned by this process and one
// run record, as on a daemon started with recording disabled.
func seedDisabledDaemon(t *testing.T, stackName string) string {
	t.Helper()
	if err := state.Save(&state.DaemonState{StackName: stackName, PID: os.Getpid(), Port: 1, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	dir, err := runs.Dir(stackName)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "runs.jsonl")
	line := `{"schema_version":1,"sequence":1,"attempt_id":"a1","returned_at":"2026-01-01T00:00:01Z","requested_name":"s__t","disposition":"completed","stage":"downstream","reason":"ok"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHandleRunsWipe_DisabledDaemonDeletesHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GRIDCTL_HOME", "")
	srv := newTestServer(t)
	srv.SetStackName("demo")
	path := seedDisabledDaemon(t, "demo")

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodPost, "/api/runs/wipe", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["success"] != true || payload["partial"] != false || payload["recording_enabled"] != false {
		t.Fatalf("payload = %v", payload)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("run file still present: %v", err)
	}

	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/runs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	var list struct {
		Records []any `json:"records"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Records) != 0 {
		t.Fatalf("records after wipe = %v", list.Records)
	}
}

func TestHandleRunsWipe_DisabledDaemonWithStackFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GRIDCTL_HOME", "")
	stackPath := filepath.Join(t.TempDir(), "stack.yaml")
	if err := os.WriteFile(stackPath, []byte("version: \"1\"\nname: demo\nmcp-servers: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t)
	srv.SetStackName("demo")
	srv.SetStackFile(stackPath)
	path := seedDisabledDaemon(t, "demo")

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodPost, "/api/runs/wipe", nil))
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["success"] != true {
		t.Fatalf("payload = %v", payload)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("run file still present: %v", err)
	}
}

func runsStatusRetention(t *testing.T, srv *Server) (int, float64, float64) {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, loopbackRequest(http.MethodGet, "/api/runs/status", nil))
	var payload map[string]any
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
	}
	days, _ := payload["retention_max_age_days"].(float64)
	bytes, _ := payload["retention_max_bytes"].(float64)
	return rec.Code, days, bytes
}

func TestHandleRunsStatus_RetentionWhileDisabled(t *testing.T) {
	writeStack := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "stack.yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	const header = "version: \"1\"\nname: demo\nmcp-servers: []\n"

	t.Run("configured", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("GRIDCTL_HOME", "")
		srv := newTestServer(t)
		srv.SetStackName("demo")
		srv.SetStackFile(writeStack(t, header+"runs:\n  enabled: false\n  retention:\n    max_size_mb: 50\n    max_age_days: 3\n"))
		code, days, bytes := runsStatusRetention(t, srv)
		if code != http.StatusOK || days != 3 || bytes != 52428800 {
			t.Fatalf("code=%d days=%v bytes=%v", code, days, bytes)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("GRIDCTL_HOME", "")
		srv := newTestServer(t)
		srv.SetStackName("demo")
		srv.SetStackFile(writeStack(t, header))
		code, days, bytes := runsStatusRetention(t, srv)
		if code != http.StatusOK || days != 7 || bytes != 104857600 {
			t.Fatalf("code=%d days=%v bytes=%v", code, days, bytes)
		}
	})

	t.Run("unreadable stack file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("GRIDCTL_HOME", "")
		srv := newTestServer(t)
		srv.SetStackName("demo")
		srv.SetStackFile(filepath.Join(t.TempDir(), "missing.yaml"))
		if code, _, _ := runsStatusRetention(t, srv); code != http.StatusOK {
			t.Fatalf("code=%d", code)
		}
		srv.SetStackFile(writeStack(t, "runs: [not, a, mapping\n"))
		if code, _, _ := runsStatusRetention(t, srv); code != http.StatusOK {
			t.Fatalf("code=%d", code)
		}
	})

	t.Run("disabled recorder keeps no stale values", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("GRIDCTL_HOME", "")
		r, err := runs.NewRecorder(runs.Config{
			Enabled: true, StackName: "demo", MaxBytes: 5 << 20, MaxAge: 2 * 24 * time.Hour,
			QueueSize: 4, SyncInterval: time.Second, ShutdownDrain: time.Millisecond,
		}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		if err := r.Apply(runs.Config{Enabled: false, StackName: "demo"}); err != nil {
			t.Fatal(err)
		}
		if st := r.Status(); st.RetentionMaxAgeDays != 2 {
			t.Fatalf("precondition: disabled recorder reports %d days", st.RetentionMaxAgeDays)
		}
		srv := newTestServer(t)
		srv.SetStackName("demo")
		srv.SetRunRecorder(r)
		srv.SetStackFile(writeStack(t, header))
		code, days, bytes := runsStatusRetention(t, srv)
		if code != http.StatusOK || days != 7 || bytes != 104857600 {
			t.Fatalf("code=%d days=%v bytes=%v", code, days, bytes)
		}
	})
}
