//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/pins"
	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/gridctl/gridctl/pkg/telemetry"
	"github.com/gridctl/gridctl/pkg/token"
	"github.com/gridctl/gridctl/pkg/tracing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
)

func TestA2AAdapter_ObservationSinks(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	var exportedMu sync.Mutex
	var exported []byte
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
		if err != nil {
			t.Error(err)
		}
		exportedMu.Lock()
		exported = append(exported, body...)
		exportedMu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
	}))
	defer collector.Close()
	previousProvider, previousPropagator := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	defer otel.SetTracerProvider(previousProvider)
	defer otel.SetTextMapPropagator(previousPropagator)
	provider := tracing.NewProvider(&tracing.Config{Enabled: true, Sampling: 1, Export: "otlp", Endpoint: collector.URL, MaxTraces: 100})
	if err := provider.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = provider.Shutdown(context.Background()) }()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	telemetryDir := filepath.Join(dir, ".gridctl", "telemetry", "a2a-observations", "agent")
	if err := os.MkdirAll(telemetryDir, 0700); err != nil {
		t.Fatal(err)
	}
	traceFiles := telemetry.NewTracesFileClient()
	if err := traceFiles.AddServer("agent", filepath.Join(telemetryDir, "traces.jsonl"), telemetry.LogOpts{}); err != nil {
		t.Fatal(err)
	}
	exporter, err := otlptrace.New(ctx, traceFiles)
	if err != nil {
		t.Fatal(err)
	}
	provider.RegisterExporter(exporter)
	logFile, err := os.Create(filepath.Join(dir, "diagnostics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	buffer := logging.NewLogBuffer(100)
	logRouter := telemetry.NewLogRouter(logging.NewBufferHandler(buffer, slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err := logRouter.AddServer("agent", filepath.Join(telemetryDir, "logs.jsonl"), telemetry.LogOpts{}); err != nil {
		t.Fatal(err)
	}
	defer logRouter.Close()
	logger := slog.New(logRouter)
	recorder, err := runs.NewRecorder(runs.Config{Enabled: true, Dir: filepath.Join(dir, "runs"), QueueSize: 64, MaxBytes: 1 << 20, MaxAge: time.Hour, ShutdownDrain: time.Second}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	f := newA2AAdapterFixture(t, "1.0")
	f.echoData.Store(true)
	g := mcp.NewGateway()
	defer g.Close()
	g.SetLogger(logger)
	g.SetRunSink(recSink{rec: recorder})
	g.SetCodeMode(5 * time.Second)
	accumulator := metrics.NewAccumulator(100)
	flusher := telemetry.NewMetricsFlusher(accumulator, time.Hour)
	if err := flusher.AddServer("agent", filepath.Join(telemetryDir, "metrics.jsonl"), telemetry.LogOpts{}); err != nil {
		t.Fatal(err)
	}
	flusher.Start()
	defer flusher.Stop()
	g.SetToolCallObserver(metrics.NewObserver(token.NewHeuristicCounter(4), accumulator))
	if err := g.SetCardPinStorage(ctx, pins.NewWithPath(dir, "observations")); err != nil {
		t.Fatal(err)
	}
	if err := g.RegisterMCPServer(ctx, mcp.MCPServerConfig{Name: "agent", A2A: true, A2AConfig: &mcp.A2AClientConfig{Card: f.server.URL, Profile: "bedrock"}}); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(g, nil)
	auth := rand.Text()
	server.SetAuth("bearer", auth, "")
	server.SetStackName("a2a-observations")
	server.SetRunRecorder(recorder)
	server.SetLogBuffer(buffer)
	server.SetTraceBuffer(provider.Buffer)
	server.SetMetricsAccumulator(accumulator)
	listener := httptest.NewServer(server.Handler())
	defer listener.Close()
	request := func(method, path string, input any) []byte {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, method, listener.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Content-Type", "application/json")
		res, err := listener.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err = io.ReadAll(io.LimitReader(res.Body, 4<<20))
		if err != nil {
			t.Fatal(err)
		}
		if method == "GET" && res.StatusCode != http.StatusOK {
			t.Fatalf("observation endpoint %s returned %d", path, res.StatusCode)
		}
		return body
	}
	call := func(tool, label string, args map[string]any) *mcp.ToolCallResult {
		t.Helper()
		body := request("POST", "/api/tools/call", map[string]any{"name": "agent__" + tool, "client": label, "arguments": args})
		var response struct {
			Result *mcp.ToolCallResult `json:"result"`
		}
		if json.Unmarshal(body, &response) != nil || response.Result == nil {
			t.Fatal("missing caller-delivered result")
		}
		return response.Result
	}
	first := a2aAdapterEnvelope(t, call("send", "owner", map[string]any{"message": "new conversation"}))
	handle := first["task_handle"].(string)
	contextHandle := first["context_handle"].(string)
	encoded := base64.RawStdEncoding.EncodeToString([]byte(handle))
	result := call("send", handle, map[string]any{"message": "nested data", "data": map[string]any{handle: map[string]any{"encoded": encoded}}})
	if result.IsError || !strings.Contains(result.Content[0].Text, handle) || !strings.Contains(result.Content[0].Text, encoded) {
		t.Fatal("sensitive application content lost in intentional REST delivery")
	}
	if got := a2aAdapterEnvelope(t, call("task_get", handle, map[string]any{"task_handle": contextHandle})); got["error"] != "capability_unavailable" {
		t.Fatal("wrong-kind authority accepted")
	}
	request("POST", "/api/tools/call", map[string]any{"name": "agent__" + handle, "client": handle, "arguments": map[string]any{}})
	for _, code := range []string{
		fmt.Sprintf(`const r = mcp.callTool("agent", "task_get", {task_handle: %q}); console.log(r.task_handle); r;`, handle),
		fmt.Sprintf(`mcp.callTool("agent", "task_get", {task_handle: %q}); throw %q + %q;`, handle, encoded[:20], encoded[20:]),
		fmt.Sprintf(`const marker = %q; throw ;`, handle),
	} {
		result, err := g.HandleToolsCall(mcp.WithClientID(ctx, handle), mcp.ToolCallParams{Name: mcp.MetaToolExecute, Arguments: map[string]any{"code": code}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(code, "console.log") && (result.IsError || !strings.Contains(result.Content[0].Text, handle)) {
			t.Fatal("code-mode console/result lost usable handle")
		}
	}
	remoteSecret := rand.Text()
	for _, failure := range []string{"rpc", "parser", "transport", "card"} {
		f.mu.Lock()
		f.onRPC = func(w http.ResponseWriter, r *http.Request, id, _ string, _ map[string]any) bool {
			w.Header().Set("X-Remote-Diagnostic", remoteSecret)
			switch failure {
			case "rpc":
				w.WriteHeader(http.StatusFailedDependency)
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32055, "message": remoteSecret, "data": map[string]any{handle: encoded}}})
			case "parser":
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":` + remoteSecret))
			case "transport":
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return true
				}
				_ = conn.Close()
			default:
				t.Error("card failure allowed an RPC")
			}
			return true
		}
		if failure == "card" {
			f.onCard = func(w http.ResponseWriter, _ *http.Request) bool {
				w.Header().Set("Retry-After", "30")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(remoteSecret + handle + encoded))
				return true
			}
		}
		f.mu.Unlock()
		before := f.calls.Load()
		result := call("task_get", "owner", map[string]any{"task_handle": handle})
		if !result.IsError || strings.Contains(result.Content[0].Text, remoteSecret) || strings.Contains(result.Content[0].Text, encoded) || strings.Contains(result.Content[0].Text, handle) {
			t.Fatalf("%s error escaped the safe result boundary", failure)
		}
		if failure == "card" && f.calls.Load() != before {
			t.Fatal("expired card failure dispatched remote work")
		}
	}
	beforeGET, beforeRPC := f.gets.Load(), f.calls.Load()
	if result := call("task_get", "owner", map[string]any{"task_handle": handle}); !result.IsError || f.gets.Load() != beforeGET || f.calls.Load() != beforeRPC {
		t.Fatal("card failure backoff fetched again or served stale work")
	}
	if err := g.RegisterMCPServer(ctx, mcp.MCPServerConfig{Name: "failed-card", A2A: true, A2AConfig: &mcp.A2AClientConfig{Card: f.server.URL + "?private=" + remoteSecret}}); err == nil || strings.Contains(err.Error(), remoteSecret) {
		t.Fatal("card initialization did not return a safe failure")
	}
	flusher.Stop()
	recorder.Close()
	if err := provider.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := logFile.Sync(); err != nil {
		t.Fatal(err)
	}
	logs, err := os.ReadFile(logFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{auth, handle, contextHandle, encoded, f.remotePrefix, remoteSecret}
	f.mu.Lock()
	secrets = append(secrets, f.sessions...)
	f.mu.Unlock()
	sinks := map[string][]byte{"log file": logs}
	for _, path := range []string{"/api/logs", "/api/traces", "/api/metrics/tokens", "/api/runs", "/api/telemetry/inventory"} {
		sinks[path] = request("GET", path, nil)
	}
	for _, signal := range []string{"logs", "metrics", "traces"} {
		body, err := os.ReadFile(filepath.Join(telemetryDir, signal+".jsonl"))
		if err != nil || len(body) == 0 {
			t.Fatalf("persisted %s evidence missing: %v", signal, err)
		}
		sinks["telemetry "+signal] = body
		if !bytes.Contains(sinks["/api/telemetry/inventory"], []byte(signal+".jsonl")) {
			t.Fatalf("telemetry inventory omitted %s", signal)
		}
	}
	files, err := filepath.Glob(filepath.Join(recorder.DirPath(), "*.jsonl"))
	if err != nil || len(files) == 0 {
		t.Fatal("persisted run records missing")
	}
	for _, file := range files {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		sinks["runs JSONL"] = append(sinks["runs JSONL"], body...)
	}
	exportedMu.Lock()
	sinks["OTLP"] = append([]byte(nil), exported...)
	exportedMu.Unlock()
	if len(sinks["OTLP"]) == 0 || buffer.Count() == 0 || provider.Buffer.Count() < 5 || accumulator.Snapshot().Session.InputTokens == 0 {
		t.Fatal("observation evidence was not produced")
	}
	for name, body := range sinks {
		for _, secret := range secrets {
			if bytes.Contains(body, []byte(secret)) {
				t.Fatalf("secret disclosed through %s", name)
			}
		}
	}
}
