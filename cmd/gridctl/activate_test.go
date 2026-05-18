package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gridctl/gridctl/pkg/registry"
)

func TestPostActivate_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/registry/skills/my-skill/activate") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(registry.AgentSkill{
			Name:  "my-skill",
			State: registry.StateActive,
		})
	}))
	defer server.Close()

	sk, status, _, err := postActivate(server.URL, "my-skill")
	if err != nil {
		t.Fatalf("postActivate: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if sk == nil || sk.State != registry.StateActive {
		t.Errorf("expected active skill, got %+v", sk)
	}
}

func TestPostActivate_PathEscaping(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	_, _, _, err := postActivate(server.URL, "weird name/with-slash")
	if err != nil {
		t.Fatalf("postActivate: %v", err)
	}
	if !strings.Contains(gotPath, "weird%20name%2Fwith-slash") {
		t.Errorf("expected escaped path segment, got %s", gotPath)
	}
}

func TestRunActivate_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(registry.AgentSkill{Name: "demo", State: registry.StateActive})
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exit := runActivate(&stdout, &stderr, server.URL, "demo", "", false)
	if exit != activateExitOK {
		t.Errorf("exit = %d, want %d; stderr=%s", exit, activateExitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Activated demo") {
		t.Errorf("expected human-readable success line; got %q", stdout.String())
	}
}

func TestRunActivate_Success_JSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(registry.AgentSkill{Name: "demo", State: registry.StateActive})
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exit := runActivate(&stdout, &stderr, server.URL, "demo", "json", false)
	if exit != activateExitOK {
		t.Fatalf("exit = %d, want 0; stderr=%s", exit, stderr.String())
	}

	var payload map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v (%q)", err, stdout.String())
	}
	if payload["skill"] != "demo" || payload["state"] != string(registry.StateActive) {
		t.Errorf("unexpected JSON payload: %+v", payload)
	}
}

func TestRunActivate_Success_Quiet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(registry.AgentSkill{Name: "demo", State: registry.StateActive})
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exit := runActivate(&stdout, &stderr, server.URL, "demo", "", true)
	if exit != activateExitOK {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if stdout.Len() != 0 {
		t.Errorf("expected no stdout under --quiet, got %q", stdout.String())
	}
}

func TestRunActivate_Success_NoContent(t *testing.T) {
	// Forward-compatibility: if the API ever returns 204, the CLI must
	// still treat it as success.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exit := runActivate(&stdout, &stderr, server.URL, "demo", "json", false)
	if exit != activateExitOK {
		t.Fatalf("exit = %d, want 0; stderr=%s", exit, stderr.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v", err)
	}
	if payload["state"] != string(registry.StateActive) {
		t.Errorf("expected default state=active when body is empty; got %+v", payload)
	}
}

func TestRunActivate_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"Skill not found: ghost"}`, http.StatusNotFound)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exit := runActivate(&stdout, &stderr, server.URL, "ghost", "", false)
	if exit != activateExitNotFound {
		t.Errorf("exit = %d, want %d", exit, activateExitNotFound)
	}
	if !strings.Contains(stderr.String(), "Skill not found: ghost") {
		t.Errorf("expected 'Skill not found' on stderr; got %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("expected no stdout on error; got %q", stdout.String())
	}
}

func TestRunActivate_Conflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"skill missing acceptance_criteria"}`, http.StatusConflict)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exit := runActivate(&stdout, &stderr, server.URL, "demo", "", false)
	if exit != activateExitInfrastructure {
		t.Errorf("exit = %d, want %d", exit, activateExitInfrastructure)
	}
	if !strings.Contains(stderr.String(), "missing acceptance_criteria") {
		t.Errorf("expected server message surfaced verbatim; got %q", stderr.String())
	}
}

func TestRunActivate_RegistryUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"Registry not available"}`, http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	exit := runActivate(&stdout, &stderr, server.URL, "demo", "", false)
	if exit != activateExitInfrastructure {
		t.Errorf("exit = %d, want %d", exit, activateExitInfrastructure)
	}
	if !strings.Contains(stderr.String(), "Registry not available") {
		t.Errorf("expected 503 message; got %q", stderr.String())
	}
}

func TestRunActivate_NetworkFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Close() // close immediately so the next request fails

	var stdout, stderr bytes.Buffer
	exit := runActivate(&stdout, &stderr, server.URL, "demo", "", false)
	if exit != activateExitInfrastructure {
		t.Errorf("exit = %d, want %d", exit, activateExitInfrastructure)
	}
	if !strings.Contains(stderr.String(), "activate:") {
		t.Errorf("expected error prefix; got %q", stderr.String())
	}
}

func TestExtractServerError(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"json envelope", `{"error":"boom"}`, "boom"},
		{"raw text", `plain text error`, "plain text error"},
		{"empty", ``, ""},
		{"whitespace", "   \n\t  ", ""},
		{"json without error field", `{"foo":"bar"}`, `{"foo":"bar"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractServerError([]byte(tc.body))
			if got != tc.want {
				t.Errorf("extractServerError(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}
