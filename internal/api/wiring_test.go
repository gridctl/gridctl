package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/wiring"
)

func doWiring(t *testing.T, s *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = loopbackRequest(method, path, nil)
	} else {
		req = loopbackRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	switch path {
	case "/api/project/wiring/status":
		s.handleProjectWiringStatus(w, req)
	case "/api/project/wiring/adopt":
		s.handleProjectWiringAdopt(w, req)
	default:
		t.Fatalf("unknown wiring path %s", path)
	}
	return w
}

func TestHandleProjectWiringStatus(t *testing.T) {
	fake := &linkFakeProv{slug: "claude", detected: true}
	// A pre-existing entry gridctl never recorded reads as foreign.
	fake.entries = map[string]map[string]any{"gridctl": {"url": "http://localhost:9999"}}
	_, s := newLinkHarness(t, fake)

	rec := doWiring(t, s, http.MethodGet, "/api/project/wiring/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var rows []wiring.Row
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	var foreign *wiring.Row
	for i := range rows {
		if rows[i].Client == "claude" && rows[i].Name == "gridctl" {
			foreign = &rows[i]
		}
	}
	if foreign == nil || foreign.State != wiring.StateForeign {
		t.Fatalf("expected a foreign row for claude/gridctl, got %+v", rows)
	}
	if foreign.Remediation == "" {
		t.Error("foreign row must carry a remediation hint")
	}
}

func TestHandleProjectWiringStatus_EmptyIsArray(t *testing.T) {
	_, s := newLinkHarness(t) // no clients registered
	rec := doWiring(t, s, http.MethodGet, "/api/project/wiring/status", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Errorf("empty status = %q, want []", got)
	}
}

func TestHandleProjectWiringAdopt(t *testing.T) {
	fake := &linkFakeProv{slug: "claude", detected: true}
	fake.entries = map[string]map[string]any{"gridctl": {"url": "http://localhost:9999"}}
	_, s := newLinkHarness(t, fake)

	rec := doWiring(t, s, http.MethodPost, "/api/project/wiring/adopt", `{"client":"claude"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("adopt status = %d: %s", rec.Code, rec.Body.String())
	}
	var res wiring.Result
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.Action != wiring.ActionAdopted {
		t.Errorf("action = %q, want %q", res.Action, wiring.ActionAdopted)
	}

	// The adopted entry now reads in-sync (or stale against a differing
	// planned value) rather than foreign: ownership is recorded.
	rec = doWiring(t, s, http.MethodGet, "/api/project/wiring/status", "")
	var rows []wiring.Row
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Client == "claude" && row.Name == "gridctl" && row.State == wiring.StateForeign {
			t.Errorf("entry still foreign after adopt: %+v", row)
		}
	}
}

func TestHandleProjectWiringAdopt_Errors(t *testing.T) {
	detected := &linkFakeProv{slug: "claude", detected: true}
	undetected := &linkFakeProv{slug: "cursor", detected: false}
	_, s := newLinkHarness(t, detected, undetected)

	cases := []struct {
		label string
		body  string
		want  int
	}{
		{"missing client", `{}`, http.StatusBadRequest},
		{"unknown client", `{"client":"ghost"}`, http.StatusBadRequest},
		{"undetected client", `{"client":"cursor"}`, http.StatusConflict},
		{"nothing to adopt", `{"client":"claude"}`, http.StatusConflict},
	}
	for _, tc := range cases {
		rec := doWiring(t, s, http.MethodPost, "/api/project/wiring/adopt", tc.body)
		if rec.Code != tc.want {
			t.Errorf("%s: status = %d, want %d (%s)", tc.label, rec.Code, tc.want, rec.Body.String())
		}
	}

	// The refusal reaches the wire with the engine's message, not a
	// generic error: the UI renders it verbatim.
	rec := doWiring(t, s, http.MethodPost, "/api/project/wiring/adopt", `{"client":"claude"}`)
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp["error"], "nothing to adopt") {
		t.Errorf("409 body must carry the engine message, got %q", resp["error"])
	}
}
