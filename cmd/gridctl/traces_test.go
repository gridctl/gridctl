package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	// The API reads minDuration (camelCase); min_duration is silently ignored.
	for _, param := range []string{"server=github", "errors=true", "minDuration=100ms"} {
		if !strings.Contains(got, param) {
			t.Errorf("URL missing param %q: %s", param, got)
		}
	}
	if strings.Contains(got, "min_duration") {
		t.Errorf("URL uses min_duration, which the API does not read: %s", got)
	}
}

func TestPrintTracesTable_empty(t *testing.T) {
	var sb strings.Builder
	printTracesTable(&sb, []apiTraceSummary{})
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
	records := []apiTraceSummary{
		{
			TraceID:   "aabbccddeeff0011",
			Operation: "github › search_code",
			Duration:  234,
			SpanCount: 5,
			Status:    "ok",
			StartTime: now,
		},
		{
			TraceID:   "112233445566778899",
			Operation: "chrome › navigate",
			Duration:  1200,
			SpanCount: 8,
			HasError:  true,
			Status:    "error",
			StartTime: now,
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
	detail := apiTraceDetail{
		TraceID: "abcdef123456",
		Spans: []apiSpan{
			{
				SpanID:    "span1",
				Name:      "gateway.receive",
				StartTime: now,
				EndTime:   now.Add(10 * time.Millisecond),
				Duration:  10,
				Status:    "ok",
			},
			{
				SpanID:    "span2",
				Name:      "mcp.client.call_tool",
				StartTime: now.Add(10 * time.Millisecond),
				EndTime:   now.Add(100 * time.Millisecond),
				Duration:  90,
				Status:    "ok",
			},
		},
	}

	var sb strings.Builder
	printWaterfall(&sb, detail)
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

func TestPrintWaterfall_derivedEndTime(t *testing.T) {
	// endTime is omitted by the API when zero; the waterfall must derive it
	// from startTime + duration rather than rendering a zero-length trace.
	now := time.Now()
	detail := apiTraceDetail{
		TraceID: "trace002",
		Spans: []apiSpan{
			{SpanID: "s1", Name: "mcp.tools.call", StartTime: now, Duration: 80, Status: "ok"},
		},
	}

	var sb strings.Builder
	printWaterfall(&sb, detail)
	out := sb.String()

	if !strings.Contains(out, "80ms") {
		t.Errorf("expected derived 80ms duration, got: %s", out)
	}
	if strings.Contains(out, "(0ms") {
		t.Errorf("trace duration should not be 0ms: %s", out)
	}
}

func TestPrintWaterfall_spanAttrs(t *testing.T) {
	now := time.Now()
	detail := apiTraceDetail{
		TraceID: "trace001",
		Spans: []apiSpan{
			{
				SpanID:    "s1",
				Name:      "mcp.client.call_tool",
				StartTime: now,
				EndTime:   now.Add(50 * time.Millisecond),
				Duration:  50,
				Status:    "ok",
				Attributes: map[string]string{
					"network.transport": "http",
					"server.name":       "github",
				},
			},
		},
	}

	var sb strings.Builder
	printWaterfall(&sb, detail)
	out := sb.String()

	if !strings.Contains(out, "transport: http") {
		t.Error("missing transport attribute sub-line")
	}
	if !strings.Contains(out, "server: github") {
		t.Error("missing server attribute in sub-line")
	}
}

// TestFetchTraces_success mocks the ACTUAL JSON served by the API (camelCase
// envelope), not a round-trip of internal structs, so wire drift cannot hide.
func TestFetchTraces_success(t *testing.T) {
	const body = `{
		"traces": [
			{
				"traceId": "trace1",
				"rootSpanId": "root1",
				"operation": "github › search_code",
				"tool": "search_code",
				"client": "claude-code",
				"server": "github",
				"startTime": "2026-07-24T10:00:00.123456789Z",
				"duration": 100,
				"spanCount": 3,
				"hasError": false,
				"status": "ok"
			}
		],
		"total": 1,
		"tracingEnabled": true,
		"bufferSize": 42,
		"bufferCapacity": 1000
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/traces" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
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
	if len(got.Traces) != 1 {
		t.Fatalf("expected 1 trace, got %d", len(got.Traces))
	}
	tr := got.Traces[0]
	if tr.TraceID != "trace1" || tr.Tool != "search_code" || tr.Client != "claude-code" || tr.Server != "github" {
		t.Errorf("unexpected summary fields: %+v", tr)
	}
	if tr.Duration != 100 || tr.SpanCount != 3 || tr.Status != "ok" {
		t.Errorf("unexpected numeric/status fields: %+v", tr)
	}
	if tr.StartTime.IsZero() {
		t.Error("startTime failed to parse")
	}
	if got.Total != 1 || !got.TracingEnabled || got.BufferSize != 42 || got.BufferCapacity != 1000 {
		t.Errorf("unexpected envelope fields: %+v", got)
	}
}

func TestFetchTraces_badRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `invalid minDuration: use a Go duration (e.g. "500ms") or bare milliseconds`, http.StatusBadRequest)
	}))
	defer srv.Close()

	var port int
	_, _ = fmt.Sscanf(srv.URL, "http://127.0.0.1:%d", &port)

	tracesServer = ""
	tracesErrorsOnly = false
	tracesMinDuration = ""

	_, err := fetchTraces(port)
	if err == nil {
		t.Fatal("expected error from 400 response, got nil")
	}
	if !strings.Contains(err.Error(), "invalid minDuration") {
		t.Errorf("error should surface the API message, got: %v", err)
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
	if err == nil {
		t.Error("expected error from server error response, got nil")
	}
}
