//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

const (
	otlpTraceParent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	otlpParentSpan  = "b7ad6b7169203331"
	otlpSecret      = "planted-secret-value-otel"
	otlpHeaderName  = "X-Gridctl-Otlp"
	otlpHeaderValue = "planted-header-value"
)

type otlpCapture struct {
	mu      sync.Mutex
	paths   []string
	headers []http.Header
	traces  []*coltracepb.ExportTraceServiceRequest
	metrics []*colmetricspb.ExportMetricsServiceRequest
}

func (c *otlpCapture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	c.mu.Lock()
	c.paths = append(c.paths, r.URL.Path)
	c.headers = append(c.headers, r.Header.Clone())
	c.mu.Unlock()
	switch r.URL.Path {
	case "/v1/traces":
		req := &coltracepb.ExportTraceServiceRequest{}
		if err := proto.Unmarshal(body, req); err == nil {
			c.mu.Lock()
			c.traces = append(c.traces, req)
			c.mu.Unlock()
		}
	case "/v1/metrics":
		req := &colmetricspb.ExportMetricsServiceRequest{}
		if err := proto.Unmarshal(body, req); err == nil {
			c.mu.Lock()
			c.metrics = append(c.metrics, req)
			c.mu.Unlock()
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (c *otlpCapture) sawPath(path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range c.paths {
		if p == path {
			return true
		}
	}
	return false
}

func (c *otlpCapture) sawHeader() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, h := range c.headers {
		if h.Get(otlpHeaderName) == otlpHeaderValue {
			return true
		}
	}
	return false
}

func startOTLPReceiver(t *testing.T) (*otlpCapture, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cap := &otlpCapture{}
	srv := &http.Server{Handler: cap}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return cap, "http://" + ln.Addr().String()
}

func TestOTLPReceiver_ToolCallConventions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	root := repoRoot(t)
	dir := t.TempDir()
	bin := buildRaceBinary(t, ctx, filepath.Join(root, "cmd/gridctl"), filepath.Join(dir, "gridctl"))
	mock := buildRaceBinary(t, ctx, filepath.Join(root, "examples", "_mock-servers", "local-stdio-server"), filepath.Join(dir, "mock"))
	recv, endpoint := startOTLPReceiver(t)
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	stackPath := filepath.Join(dir, "stack.yaml")
	stack := fmt.Sprintf(`version: "1"
name: otel-receiver
network:
  name: otel-receiver-net
mcp-servers:
  - name: echo
    command: [%q]
gateway:
  tracing:
    export: otlp
    endpoint: %s
    headers:
      %s: %s
  metrics:
    otlp:
      endpoint: %s
      interval: 1s
      timeout: 5s
      headers:
        %s: %s
policy:
  default: allow
  rules:
    - match: echo__add
      action: block
`, mock, endpoint, otlpHeaderName, otlpHeaderValue, endpoint, otlpHeaderName, otlpHeaderValue)
	if err := os.WriteFile(stackPath, []byte(stack), 0o644); err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	logPath := filepath.Join(dir, "daemon.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	daemon := exec.CommandContext(ctx, bin, "apply", stackPath, "--foreground", "--port", fmt.Sprint(port))
	daemon.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "GRIDCTL_HOME=" + home, "NO_COLOR=1"}
	daemon.Stdout = logFile
	daemon.Stderr = logFile
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if daemon.Process != nil {
			_ = daemon.Process.Signal(syscall.SIGTERM)
			_ = daemon.Wait()
		}
		if t.Failed() {
			data, _ := os.ReadFile(logPath)
			t.Logf("daemon:\n%s", data)
		}
	}()
	waitForReady(t, ctx, port)
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	session := otlpInitialize(t, ctx, base)
	otlpCall(t, ctx, base, session, "echo__echo", map[string]any{"message": otlpSecret}, true)
	otlpCall(t, ctx, base, session, "echo__add", map[string]any{"a": 1, "b": 2}, false)
	_ = daemon.Process.Signal(syscall.SIGTERM)
	_ = daemon.Wait()
	daemon.Process = nil

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && (!recv.sawPath("/v1/traces") || !recv.sawPath("/v1/metrics")) {
		time.Sleep(100 * time.Millisecond)
	}
	if !recv.sawPath("/v1/traces") || !recv.sawPath("/v1/metrics") {
		t.Fatalf("paths = %v, want /v1/traces and /v1/metrics from a pathless endpoint", recv.paths)
	}
	if !recv.sawHeader() {
		t.Fatal("OTLP request missing configured header")
	}
	span := findServerSpan(t, recv, "tools/call echo")
	if span.parent != otlpParentSpan {
		t.Fatalf("parent span id = %s, want %s", span.parent, otlpParentSpan)
	}
	if span.attrs["mcp.method.name"] != "tools/call" || span.attrs["gen_ai.tool.name"] != "echo" || span.attrs["gridctl.server.name"] != "echo" {
		t.Fatalf("span attrs = %v", span.attrs)
	}
	if span.attrs["network.transport"] != "pipe" {
		t.Fatalf("network.transport = %q, want pipe", span.attrs["network.transport"])
	}
	if span.service != "otel-receiver" {
		t.Fatalf("service.name = %q", span.service)
	}
	if spanHasSecret(recv) {
		t.Fatal("exported attribute contains the planted secret")
	}
	completed := findMetric(t, recv, "gridctl.gateway.tool.calls", map[string]string{
		"gridctl.server.name":          "echo",
		"gen_ai.tool.name":             "echo",
		"gridctl.dispatch.disposition": "completed",
	})
	if !completed {
		t.Fatal("missing completed tool-call counter")
	}
	if !findMetric(t, recv, "mcp.server.operation.duration", map[string]string{"gridctl.server.name": "echo", "gen_ai.tool.name": "echo"}) {
		t.Fatal("missing server operation duration")
	}
	if !findMetric(t, recv, "gridctl.gateway.tool.calls", map[string]string{
		"gridctl.dispatch.disposition": "denied",
		"gridctl.dispatch.stage":       "gate",
	}) {
		t.Fatalf("missing denied tool-call counter; metrics:\n%s", dumpMetrics(recv))
	}
	if !findMetric(t, recv, "mcp.server.operation.duration", map[string]string{"error.type": "denied"}) {
		t.Fatalf("missing denied error.type; metrics:\n%s", dumpMetrics(recv))
	}
}

type capturedSpan struct {
	name    string
	parent  string
	service string
	attrs   map[string]string
}

func findServerSpan(t *testing.T, cap *otlpCapture, name string) capturedSpan {
	t.Helper()
	cap.mu.Lock()
	defer cap.mu.Unlock()
	for _, req := range cap.traces {
		for _, rs := range req.GetResourceSpans() {
			service := attrString(rs.GetResource().GetAttributes(), "service.name")
			for _, ss := range rs.GetScopeSpans() {
				for _, sp := range ss.GetSpans() {
					if sp.GetName() == name && sp.GetKind() == tracepb.Span_SPAN_KIND_SERVER {
						return capturedSpan{
							name:    sp.GetName(),
							parent:  fmt.Sprintf("%x", sp.GetParentSpanId()),
							service: service,
							attrs:   attrMap(sp.GetAttributes()),
						}
					}
				}
			}
		}
	}
	t.Fatalf("SERVER span %q not found", name)
	return capturedSpan{}
}

func spanHasSecret(cap *otlpCapture) bool {
	cap.mu.Lock()
	defer cap.mu.Unlock()
	for _, req := range cap.traces {
		for _, rs := range req.GetResourceSpans() {
			for _, ss := range rs.GetScopeSpans() {
				for _, sp := range ss.GetSpans() {
					for _, kv := range sp.GetAttributes() {
						if strings.Contains(kv.GetValue().GetStringValue(), otlpSecret) {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

func findMetric(t *testing.T, cap *otlpCapture, name string, want map[string]string) bool {
	t.Helper()
	cap.mu.Lock()
	defer cap.mu.Unlock()
	for _, req := range cap.metrics {
		for _, rm := range req.GetResourceMetrics() {
			for _, sm := range rm.GetScopeMetrics() {
				for _, m := range sm.GetMetrics() {
					if m.GetName() != name {
						continue
					}
					for _, attrs := range metricAttrSets(m) {
						if attrsMatch(attrs, want) {
							return true
						}
					}
				}
			}
		}
	}
	return false
}

func dumpMetrics(cap *otlpCapture) string {
	cap.mu.Lock()
	defer cap.mu.Unlock()
	var b strings.Builder
	for _, req := range cap.metrics {
		for _, rm := range req.GetResourceMetrics() {
			for _, sm := range rm.GetScopeMetrics() {
				for _, m := range sm.GetMetrics() {
					fmt.Fprintf(&b, "%s\n", m.GetName())
					for _, attrs := range metricAttrSets(m) {
						fmt.Fprintf(&b, "  %v\n", attrs)
					}
				}
			}
		}
	}
	return b.String()
}

func metricAttrSets(m *metricspb.Metric) []map[string]string {
	var out []map[string]string
	switch data := m.GetData().(type) {
	case *metricspb.Metric_Sum:
		for _, dp := range data.Sum.GetDataPoints() {
			out = append(out, attrMap(dp.GetAttributes()))
		}
	case *metricspb.Metric_Histogram:
		for _, dp := range data.Histogram.GetDataPoints() {
			out = append(out, attrMap(dp.GetAttributes()))
		}
	case *metricspb.Metric_Gauge:
		for _, dp := range data.Gauge.GetDataPoints() {
			out = append(out, attrMap(dp.GetAttributes()))
		}
	}
	return out
}

func attrsMatch(got, want map[string]string) bool {
	for k, v := range want {
		if got[k] != v {
			return false
		}
	}
	return true
}

func attrMap(kvs []*commonpb.KeyValue) map[string]string {
	out := map[string]string{}
	for _, kv := range kvs {
		if kv == nil || kv.Value == nil {
			continue
		}
		if s := kv.Value.GetStringValue(); s != "" {
			out[kv.Key] = s
		}
	}
	return out
}

func attrString(kvs []*commonpb.KeyValue, key string) string {
	return attrMap(kvs)[key]
}

func otlpInitialize(t *testing.T, ctx context.Context, base string) string {
	t.Helper()
	body := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"otel-test","version":"1"}}}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		return sid
	}
	t.Fatalf("initialize status %d missing session", resp.StatusCode)
	return ""
}

func otlpCall(t *testing.T, ctx context.Context, base, session, name string, args map[string]any, wantOK bool) {
	t.Helper()
	params := map[string]any{
		"name":      name,
		"arguments": args,
		"_meta":     map[string]string{"traceparent": otlpTraceParent},
	}
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": params})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/mcp", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Mcp-Session-Id", session)
	req.Header.Set("MCP-Protocol-Version", "2025-11-25")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("%s status %d body %s", name, resp.StatusCode, raw)
	}
	if wantOK && bytes.Contains(raw, []byte(`"isError":true`)) {
		t.Fatalf("%s returned isError: %s", name, raw)
	}
}

func waitForReady(t *testing.T, ctx context.Context, port int) {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/ready", port)
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("daemon did not become ready")
}


