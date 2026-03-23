package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/tracing"
)

func TestFormatTraceDuration(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{0, "0ms"},
		{99, "99ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1500, "1.5s"},
		{2000, "2.0s"},
	}
	for _, tc := range tests {
		got := formatTraceDuration(tc.ms)
		if got != tc.want {
			t.Errorf("formatTraceDuration(%d) = %q, want %q", tc.ms, got, tc.want)
		}
	}
}

func TestBuildTracesURL_noFilters(t *testing.T) {
	// Reset globals.
	tracesServer = ""
	tracesErrorsOnly = false
	tracesMinDuration = ""

	got := buildTracesURL(8080)
	want := "http://localhost:8080/api/traces"
	if got != want {
		t.Errorf("buildTracesURL = %q, want %q", got, want)
	}
}

func TestBuildTracesURL_allFilters(t *testing.T) {
	tracesServer = "github"
	tracesErrorsOnly = true
	tracesMinDuration = "100ms"
	defer func() {
		tracesServer = ""
		tracesErrorsOnly = false
		tracesMinDuration = ""
	}()

	got := buildTracesURL(9090)
	if !strings.HasPrefix(got, "http://localhost:9090/api/traces?") {
		t.Errorf("URL missing base: %q", got)
	}
	for _, param := range []string{"server=github", "errors=true", "min_duration=100ms"} {
		if !strings.Contains(got, param) {
			t.Errorf("URL missing param %q: %s", param, got)
		}
	}
}

func TestPrintTracesTable_empty(t *testing.T) {
	var sb strings.Builder
	printTracesTable(&sb, []tracing.TraceRecord{})
	// Should only have the header line.
	lines := strings.Split(strings.TrimSpace(sb.String()), "\n")
	if len(lines) != 1 {
		t.Errorf("expected 1 line (header), got %d", len(lines))
	}
	if !strings.Contains(lines[0], "TRACE ID") {
		t.Errorf("header missing TRACE ID: %q", lines[0])
	}
}

func TestPrintTracesTable_rows(t *testing.T) {
	now := time.Now()
	records := []tracing.TraceRecord{
		{
			TraceID:    "aabbccddeeff0011",
			Operation:  "tools/call",
			DurationMs: 234,
			SpanCount:  5,
			IsError:    false,
			StartTime:  now,
			EndTime:    now.Add(234 * time.Millisecond),
		},
		{
			TraceID:    "112233445566778899",
			Operation:  "tools/call chrome",
			DurationMs: 1200,
			SpanCount:  8,
			IsError:    true,
			StartTime:  now,
			EndTime:    now.Add(1200 * time.Millisecond),
		},
	}

	var sb strings.Builder
	printTracesTable(&sb, records)
	out := sb.String()

	if !strings.Contains(out, "aabbccddeeff0011") {
		t.Error("output missing first trace ID")
	}
	if !strings.Contains(out, "234ms") {
		t.Error("output missing duration 234ms")
	}
	if !strings.Contains(out, "ok") {
		t.Error("output missing status ok")
	}
	if !strings.Contains(out, "error") {
		t.Error("output missing status error")
	}
	// Long trace ID should be truncated to 16 chars.
	if strings.Contains(out, "112233445566778899") {
		t.Error("long trace ID should be truncated to 16 chars")
	}
	if !strings.Contains(out, "1.2s") {
		t.Error("output missing duration 1.2s")
	}
}

func TestPrintWaterfall_header(t *testing.T) {
	now := time.Now()
	tr := tracing.TraceRecord{
		TraceID:    "abcdef123456",
		Operation:  "tools/call",
		DurationMs: 100,
		SpanCount:  2,
		StartTime:  now,
		EndTime:    now.Add(100 * time.Millisecond),
		Spans: []tracing.SpanRecord{
			{
				TraceID:    "abcdef123456",
				SpanID:     "span1",
				Name:       "gateway.receive",
				StartTime:  now,
				EndTime:    now.Add(10 * time.Millisecond),
				DurationMs: 10,
				Status:     "Ok",
			},
			{
				TraceID:    "abcdef123456",
				SpanID:     "span2",
				Name:       "mcp.client.call_tool",
				StartTime:  now.Add(10 * time.Millisecond),
				EndTime:    now.Add(100 * time.Millisecond),
				DurationMs: 90,
				Status:     "Ok",
			},
		},
	}

	var sb strings.Builder
	printWaterfall(&sb, tr)
	out := sb.String()

	if !strings.HasPrefix(out, "Trace abcdef123456") {
		t.Errorf("waterfall header wrong: %q", strings.SplitN(out, "\n", 2)[0])
	}
	if !strings.Contains(out, "100ms") {
		t.Error("header missing duration")
	}
	if !strings.Contains(out, "2 spans") {
		t.Error("header missing span count")
	}
	if !strings.Contains(out, "gateway.receive") {
		t.Error("missing span name")
	}
	if !strings.Contains(out, "mcp.client.call_tool") {
		t.Error("missing span name")
	}
	// Last span uses └─ connector.
	if !strings.Contains(out, "└─") {
		t.Error("missing └─ connector for last span")
	}
	// Non-last span uses ├─ connector.
	if !strings.Contains(out, "├─") {
		t.Error("missing ├─ connector for non-last span")
	}
}

func TestPrintWaterfall_spanAttrs(t *testing.T) {
	now := time.Now()
	tr := tracing.TraceRecord{
		TraceID:    "trace001",
		DurationMs: 50,
		SpanCount:  1,
		StartTime:  now,
		EndTime:    now.Add(50 * time.Millisecond),
		Spans: []tracing.SpanRecord{
			{
				TraceID:    "trace001",
				SpanID:     "s1",
				Name:       "mcp.client.call_tool",
				StartTime:  now,
				EndTime:    now.Add(50 * time.Millisecond),
				DurationMs: 50,
				Status:     "Ok",
				Attrs: map[string]string{
					"network.transport": "http",
					"server.name":       "github",
				},
			},
		},
	}

	var sb strings.Builder
	printWaterfall(&sb, tr)
	out := sb.String()

	if !strings.Contains(out, "transport: http") {
		t.Error("missing transport attribute sub-line")
	}
	if !strings.Contains(out, "server: github") {
		t.Error("missing server attribute in sub-line")
	}
}

func TestFetchTraces_success(t *testing.T) {
	now := time.Now()
	records := []tracing.TraceRecord{
		{TraceID: "trace1", Operation: "tools/call", DurationMs: 100, StartTime: now, EndTime: now.Add(100 * time.Millisecond)},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/traces" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(records)
	}))
	defer srv.Close()

	// Extract port from test server URL.
	var port int
	_, _ = fmt.Sscanf(srv.URL, "http://127.0.0.1:%d", &port)

	// Reset filters.
	tracesServer = ""
	tracesErrorsOnly = false
	tracesMinDuration = ""

	got, err := fetchTraces(port)
	if err != nil {
		t.Fatalf("fetchTraces error: %v", err)
	}
	if len(got) != 1 || got[0].TraceID != "trace1" {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestFetchTraces_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	var port int
	_, _ = fmt.Sscanf(srv.URL, "http://127.0.0.1:%d", &port)

	tracesServer = ""
	tracesErrorsOnly = false
	tracesMinDuration = ""

	_, err := fetchTraces(port)
	// Should error because response body isn't valid JSON for []TraceRecord.
	if err == nil {
		t.Error("expected error from server error response, got nil")
	}
}
