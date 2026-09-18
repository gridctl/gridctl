package mcp

import (
	"context"
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

	"github.com/gridctl/gridctl/pkg/jsonrpc"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/gridctl/gridctl/pkg/tracing"
	"go.opentelemetry.io/otel"
)

func TestSensitiveDispatch_RealNetworkObservationSinks(t *testing.T) {
	ctx := context.Background()
	handle := sensitiveSentinel(t)
	encoded := base64.RawStdEncoding.EncodeToString([]byte(handle))
	remoteID := "remote-" + sensitiveSentinel(t)[9:]
	session := "session-" + sensitiveSentinel(t)[9:]
	secrets := []string{handle, encoded, remoteID, session}
	var otlpMu sync.Mutex
	var otlpBody []byte
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		otlpMu.Lock()
		otlpBody = append(otlpBody, data...)
		otlpMu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer collector.Close()
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	defer otel.SetTracerProvider(previousProvider)
	defer otel.SetTextMapPropagator(previousPropagator)
	provider := tracing.NewProvider(&tracing.Config{Enabled: true, Sampling: 1, Export: "otlp", Endpoint: collector.URL, MaxTraces: 100})
	if err := provider.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = provider.Shutdown(ctx) }()

	logFile, err := os.Create(filepath.Join(t.TempDir(), "diagnostics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	logBuffer := logging.NewLogBuffer(100)
	logger := slog.New(logging.NewBufferHandler(logBuffer, slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: slog.LevelDebug})))
	dir := t.TempDir()
	recorder, err := runs.NewRecorder(runs.Config{Enabled: true, Dir: dir, QueueSize: 64, MaxBytes: 1 << 20, MaxAge: time.Hour, ShutdownDrain: time.Second}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()

	// Use the production HTTP MCP client over a real socket to exercise gateway
	// observation. Classification stays trusted and private to this package.
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request jsonrpc.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var params ToolCallParams
		if err := json.Unmarshal(request.Params, &params); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if params.Arguments["fail"] == true {
			_ = json.NewEncoder(w).Encode(jsonrpc.NewErrorResponse(request.ID, -32000, strings.Join(secrets, " ")))
			return
		}
		content, _ := json.Marshal(map[string]any{"handle": handle, "nested": map[string]any{"data": encoded, "remote": remoteID, "session": session}})
		_ = json.NewEncoder(w).Encode(jsonrpc.NewSuccessResponse(request.ID, &ToolCallResult{Content: []Content{NewTextContent(string(content))}}))
	}))
	defer downstream.Close()
	g := NewGateway()
	defer g.Close()
	g.SetLogger(logger)
	counter := &sensitiveCounterSpy{}
	g.SetTokenCounter(counter)
	g.SetDefaultOutputFormat("toon")
	g.SetRunSink(recSink{rec: recorder})
	g.SetCodeMode(time.Second)
	client := NewClient("agent", downstream.URL)
	client.SetTools([]Tool{{Name: "send"}})
	g.Router().AddClient(client)
	g.Router().RefreshTools()
	g.serverMeta["agent"] = MCPServerConfig{sensitiveCalls: true}
	observer := &sensitiveObserverSpy{}
	g.SetToolCallObserver(observer)
	ctx = WithClientAccessID(WithClientID(ctx, handle), handle)
	result, err := g.HandleToolsCall(ctx, ToolCallParams{Name: "agent__send", Arguments: map[string]any{"data": map[string]any{handle: encoded}}})
	if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, handle) {
		t.Fatal("successful caller delivery lost", err)
	}
	result, err = g.HandleToolsCall(ctx, ToolCallParams{Name: "agent__send", Arguments: map[string]any{"fail": true}})
	if err != nil || !result.IsError || !strings.Contains(result.Content[0].Text, "sensitive_call_failed") {
		t.Fatal("unsafe failure category", err)
	}
	for _, code := range []string{
		`const value = mcp.callTool("agent", "send", {}); console.log(value.handle); value;`,
		`const value = mcp.callTool("agent", "send", {}); throw value.nested.data;`,
		fmt.Sprintf(`mcp.callTool("agent", "send", {}); throw %q + %q;`, encoded[:20], encoded[20:]),
		fmt.Sprintf(`const syntaxMarker = %q; throw ;`, handle),
	} {
		result, err := g.HandleToolsCall(ctx, ToolCallParams{Name: MetaToolExecute, Arguments: map[string]any{"code": code}})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(code, "console.log") && (result.IsError || !strings.Contains(result.Content[0].Text, handle)) {
			t.Fatal("code-mode caller delivery lost")
		}
	}
	_, _ = g.HandleToolsCall(ctx, ToolCallParams{Name: "unknown__" + handle})
	if observer.legacy.count() != 0 || observer.rich.count() != 0 || len(observer.safe) != 5 || !observer.safe[1].Failed {
		t.Fatalf("observation accounting: %+v", observer.safe)
	}
	if counter.calls.count() != 0 {
		t.Fatal("sensitive payload reached configured token counter")
	}
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
	records, err := runs.Query(ctx, dir, runs.Filter{}, 100, nil, recorder.WipeEpoch())
	if err != nil {
		t.Fatal(err)
	}
	if len(records.Records) < 8 {
		t.Fatalf("missing run evidence: %d", len(records.Records))
	}
	traces := provider.Buffer.GetRecent(100)
	if len(traces) < 7 {
		t.Fatalf("missing trace evidence: %d", len(traces))
	}
	otlpMu.Lock()
	exported := append([]byte(nil), otlpBody...)
	otlpMu.Unlock()
	if len(exported) == 0 {
		t.Fatal("OTLP receiver got no traces")
	}
	for name, value := range map[string]any{"logs": string(logs), "buffer": logBuffer.GetRecent(100), "runs": records, "traces": traces, "otlp": string(exported), "observer": observer.safe} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range secrets {
			if strings.Contains(string(data), secret) {
				t.Fatalf("secret retained in %s", name)
			}
		}
	}
}
