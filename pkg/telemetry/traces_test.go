package telemetry

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"testing"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestTracesFileClient_RoutesByServerName(t *testing.T) {
	dir := t.TempDir()
	githubPath := filepath.Join(dir, "github-traces.jsonl")
	weatherPath := filepath.Join(dir, "weather-traces.jsonl")

	c := NewTracesFileClient()
	t.Cleanup(func() { _ = c.Stop(context.Background()) })

	if err := c.AddServer("github", githubPath, LogOpts{}); err != nil {
		t.Fatalf("AddServer github: %v", err)
	}
	if err := c.AddServer("weather", weatherPath, LogOpts{}); err != nil {
		t.Fatalf("AddServer weather: %v", err)
	}

	// Build one ResourceSpans containing three spans: one tagged github,
	// one tagged weather, one untagged. The client should split them into
	// two files.
	rs := &tracepb.ResourceSpans{
		Resource: &resourcepb.Resource{},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Scope: &commonpb.InstrumentationScope{Name: "gridctl/test"},
			Spans: []*tracepb.Span{
				makeSpan("github", "span-github", 1),
				makeSpan("weather", "span-weather", 2),
				makeSpan("", "span-orphan", 3), // no server.name → dropped
			},
		}},
	}
	if err := c.UploadTraces(context.Background(), []*tracepb.ResourceSpans{rs}); err != nil {
		t.Fatalf("UploadTraces: %v", err)
	}

	githubLines := readTraceLines(t, githubPath)
	weatherLines := readTraceLines(t, weatherPath)

	if len(githubLines) != 1 {
		t.Fatalf("github lines = %d, want 1", len(githubLines))
	}
	if len(weatherLines) != 1 {
		t.Fatalf("weather lines = %d, want 1", len(weatherLines))
	}

	// Verify each line is a parseable TracesData envelope and contains
	// only spans from its server.
	for _, line := range githubLines {
		var td tracepb.TracesData
		if err := protojson.Unmarshal([]byte(line), &td); err != nil {
			t.Errorf("github line not OTLP-JSON: %v", err)
			continue
		}
		count := 0
		for _, rs := range td.ResourceSpans {
			for _, ss := range rs.ScopeSpans {
				count += len(ss.Spans)
				for _, sp := range ss.Spans {
					if got := extractServerNameFromAttrs(sp.Attributes); got != "github" {
						t.Errorf("github line contained span tagged %q, want github", got)
					}
				}
			}
		}
		if count != 1 {
			t.Errorf("github envelope contained %d spans; want 1", count)
		}
	}
}

func TestTracesFileClient_RemoveServerStopsWriting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "github-traces.jsonl")

	c := NewTracesFileClient()
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	if err := c.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	rs := &tracepb.ResourceSpans{
		Resource: &resourcepb.Resource{},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Scope: &commonpb.InstrumentationScope{Name: "gridctl/test"},
			Spans: []*tracepb.Span{makeSpan("github", "first", 1)},
		}},
	}
	if err := c.UploadTraces(context.Background(), []*tracepb.ResourceSpans{rs}); err != nil {
		t.Fatalf("UploadTraces #1: %v", err)
	}

	c.RemoveServer("github")

	rs2 := &tracepb.ResourceSpans{
		Resource: &resourcepb.Resource{},
		ScopeSpans: []*tracepb.ScopeSpans{{
			Scope: &commonpb.InstrumentationScope{Name: "gridctl/test"},
			Spans: []*tracepb.Span{makeSpan("github", "second", 2)},
		}},
	}
	if err := c.UploadTraces(context.Background(), []*tracepb.ResourceSpans{rs2}); err != nil {
		t.Fatalf("UploadTraces #2: %v", err)
	}

	lines := readTraceLines(t, path)
	if len(lines) != 1 {
		t.Errorf("after RemoveServer: lines = %d, want 1", len(lines))
	}
}

func TestTracesFileClient_FileMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traces.jsonl")
	c := NewTracesFileClient()
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	if err := c.AddServer("github", path, LogOpts{}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file mode = %v, want 0600", got)
	}
}

func TestTracesFileClient_NoOpOnEmptyBatch(t *testing.T) {
	c := NewTracesFileClient()
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	if err := c.UploadTraces(context.Background(), nil); err != nil {
		t.Errorf("UploadTraces(nil) returned %v, want nil", err)
	}
}

// makeSpan builds a minimal valid Span with a server.name attribute. seed
// distinguishes ids so spans don't collide in tests.
func makeSpan(serverName, name string, seed byte) *tracepb.Span {
	s := &tracepb.Span{
		Name:              name,
		TraceId:           []byte{seed, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		SpanId:            []byte{seed, 0, 0, 0, 0, 0, 0, 0},
		StartTimeUnixNano: 1_000_000_000,
		EndTimeUnixNano:   2_000_000_000,
	}
	if serverName != "" {
		s.Attributes = []*commonpb.KeyValue{{
			Key: serverNameAttrKey,
			Value: &commonpb.AnyValue{
				Value: &commonpb.AnyValue_StringValue{StringValue: serverName},
			},
		}}
	}
	return s
}

func readTraceLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		txt := scanner.Text()
		if txt != "" {
			lines = append(lines, txt)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return lines
}
