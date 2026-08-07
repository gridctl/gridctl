package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/optimize"
)

func TestFormatImpact(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "—"},
		{-1, "—"},
		{950, "950"},
		{18_000, "18.0K"},
		{2_500_000, "2.5M"},
	}
	for _, tc := range cases {
		if got := formatImpact(tc.in); got != tc.want {
			t.Errorf("formatImpact(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSeverityLabel(t *testing.T) {
	cases := []struct {
		in   optimize.Severity
		want string
	}{
		{optimize.SeverityCritical, "✗ critical"},
		{optimize.SeverityWarn, "⚠ warn"},
		{optimize.SeverityInfo, "ℹ info"},
	}
	for _, tc := range cases {
		if got := severityLabel(tc.in); got != tc.want {
			t.Errorf("severityLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine("hello\nworld"); got != "hello" {
		t.Errorf("firstLine multi-line = %q, want %q", got, "hello")
	}
	if got := firstLine("hello"); got != "hello" {
		t.Errorf("firstLine single-line = %q, want %q", got, "hello")
	}
	if got := firstLine(""); got != "" {
		t.Errorf("firstLine empty = %q, want %q", got, "")
	}
}

func TestHasActionableFindings(t *testing.T) {
	infoOnly := []optimize.Finding{{Severity: optimize.SeverityInfo}}
	if hasActionableFindings(infoOnly) {
		t.Errorf("info-only findings should not be actionable")
	}

	withWarn := []optimize.Finding{{Severity: optimize.SeverityWarn}}
	if !hasActionableFindings(withWarn) {
		t.Errorf("warn finding should be actionable")
	}

	if hasActionableFindings(nil) {
		t.Errorf("nil findings should not be actionable")
	}
}

func TestRenderOptimizeTable_NoFindings(t *testing.T) {
	var buf bytes.Buffer
	renderOptimizeTable(&buf, optimize.OptimizeReport{HealthScore: 100}, false)
	if !strings.Contains(buf.String(), "No findings") {
		t.Errorf("expected 'No findings' message; got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "100/100") {
		t.Errorf("expected health score to render; got: %s", buf.String())
	}
}

func TestRenderOptimizeTable_PrintsRemediation(t *testing.T) {
	var buf bytes.Buffer
	report := optimize.OptimizeReport{
		HealthScore: 80,
		Findings: []optimize.Finding{{
			ID:                  "unused-server-github",
			Heuristic:           "unused_server",
			Severity:            optimize.SeverityWarn,
			Title:               "Unused server: github",
			Summary:             "no calls observed",
			ImpactTokensPerWeek: 18_000,
			Remediation:         "mcp-servers:\n  # delete entry: github",
			DetectedAt:          time.Now(),
		}},
	}
	renderOptimizeTable(&buf, report, false)
	out := buf.String()
	for _, want := range []string{"github", "warn", "18.0K", "delete entry"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected output to contain %q; got: %s", want, out)
		}
	}
}

func TestFetchOptimizeReport_HappyPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("min_impact"); got != "500" {
			t.Errorf("min_impact query = %q, want %q", got, "500")
		}
		if got := r.URL.Query().Get("severity"); got != "warn" {
			t.Errorf("severity query = %q, want %q", got, "warn")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(optimize.OptimizeReport{HealthScore: 100})
	}))
	defer server.Close()

	port := mustPort(t, server.URL)
	report, err := fetchOptimizeReport(port, "", 500, "warn")
	if err != nil {
		t.Fatalf("fetchOptimizeReport: %v", err)
	}
	if report.HealthScore != 100 {
		t.Errorf("HealthScore = %d, want 100", report.HealthScore)
	}
}

func TestFetchOptimizeReport_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "stack 'foo' is not the active stack", http.StatusNotFound)
	}))
	defer server.Close()

	port := mustPort(t, server.URL)
	_, err := fetchOptimizeReport(port, "foo", 0, "")
	if err == nil {
		t.Fatal("expected error on 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected status code in error; got %v", err)
	}
}

// mustPort extracts the integer port from an httptest server URL. The
// CLI's portFromURL (in link.go) returns a string; the optimize command
// works with int ports, so this test helper bridges the two.
func mustPort(t *testing.T, raw string) int {
	t.Helper()
	port, err := strconv.Atoi(portFromURL(raw))
	if err != nil {
		t.Fatalf("parse port from %q: %v", raw, err)
	}
	return port
}
