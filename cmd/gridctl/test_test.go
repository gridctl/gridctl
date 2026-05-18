package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/registry"
)

func TestTestVerdictLabel(t *testing.T) {
	cases := []struct {
		in   registry.TestSeverity
		want string
	}{
		{registry.TestSeverityPass, "✓ pass"},
		{registry.TestSeverityFail, "✗ fail"},
		{registry.TestSeverityError, "! error"},
		{"", "—"},
		{"unknown", "unknown"},
	}
	for _, tc := range cases {
		if got := testVerdictLabel(tc.in); got != tc.want {
			t.Errorf("testVerdictLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRenderTestTable_DryRun(t *testing.T) {
	var buf bytes.Buffer
	report := registry.TestReport{
		SkillName: "fixture",
		DryRun:    true,
		Results: []registry.TestResult{
			{Index: 0, Criterion: "given X, when Y, then Z"},
		},
	}
	renderTestTable(&buf, report)
	out := buf.String()
	if !strings.Contains(out, "given X") {
		t.Errorf("expected criterion text in output; got: %s", out)
	}
	if !strings.Contains(out, "dry run") {
		t.Errorf("expected 'dry run' footer; got: %s", out)
	}
}

func TestRenderTestTable_Verdicts(t *testing.T) {
	var buf bytes.Buffer
	report := registry.TestReport{
		SkillName:  "fixture",
		Evaluator:  "deterministic",
		PassCount:  1,
		FailCount:  1,
		ErrorCount: 0,
		Results: []registry.TestResult{
			{Index: 0, Criterion: "PASS: ok", Severity: registry.TestSeverityPass, Message: "good", EvaluatedAt: time.Now()},
			{Index: 1, Criterion: "FAIL: nope", Severity: registry.TestSeverityFail, Message: "missing handler", EvaluatedAt: time.Now()},
		},
	}
	renderTestTable(&buf, report)
	out := buf.String()
	for _, want := range []string{"pass", "fail", "PASS: ok", "FAIL: nope", "missing handler", "deterministic", "Skill: fixture"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q; got: %s", want, out)
		}
	}
}

func TestRenderTestTable_NoResults(t *testing.T) {
	var buf bytes.Buffer
	report := registry.TestReport{SkillName: "empty"}
	renderTestTable(&buf, report)
	if !strings.Contains(buf.String(), "No criteria") {
		t.Errorf("expected 'No criteria' message; got: %s", buf.String())
	}
}

func TestFetchTestReport_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/api/registry/skills/my-skill/test") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("criterion"); got != "1" {
			t.Errorf("criterion query = %q, want %q", got, "1")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(registry.TestReport{
			SkillName: "my-skill",
			PassCount: 1,
			Results: []registry.TestResult{
				{Index: 1, Severity: registry.TestSeverityPass, Criterion: "PASS: c1"},
			},
			Evaluator: "deterministic",
		})
	}))
	defer server.Close()

	port := mustPort(t, server.URL)
	report, err := fetchTestReport(port, "my-skill", 1, false)
	if err != nil {
		t.Fatalf("fetchTestReport: %v", err)
	}
	if report.SkillName != "my-skill" {
		t.Errorf("SkillName = %q, want my-skill", report.SkillName)
	}
	if report.PassCount != 1 {
		t.Errorf("PassCount = %d, want 1", report.PassCount)
	}
}

func TestFetchTestReport_DryRunQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("dry_run"); got != "1" {
			t.Errorf("dry_run query = %q, want %q", got, "1")
		}
		if got := r.URL.Query().Get("criterion"); got != "" {
			t.Errorf("criterion query should be empty in default case; got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(registry.TestReport{SkillName: "x", DryRun: true})
	}))
	defer server.Close()

	port := mustPort(t, server.URL)
	report, err := fetchTestReport(port, "x", -1, true)
	if err != nil {
		t.Fatalf("fetchTestReport: %v", err)
	}
	if !report.DryRun {
		t.Error("DryRun should be true")
	}
}

func TestFetchTestReport_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"Skill not found"}`, http.StatusNotFound)
	}))
	defer server.Close()

	port := mustPort(t, server.URL)
	_, err := fetchTestReport(port, "missing", -1, false)
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected status code in error; got %v", err)
	}
}
