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
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/logging"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/metrics"
	"github.com/gridctl/gridctl/pkg/runs"
	"github.com/gridctl/gridctl/pkg/token"
	"github.com/gridctl/gridctl/pkg/tracing"
	"go.opentelemetry.io/otel"
)

// Exercise the shared diagnostic boundary through real REST requests, a real
// subprocess MCP server, persisted records, and the observation APIs. Sensitive
// construction metadata remains private; its payload-exclusion tests live with
// the gateway and use a real HTTP client plus OTLP receiver.
func TestCapabilityDiagnostics_RESTObservationAPIs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	entropy := make([]byte, 32)
	if _, err := rand.Read(entropy); err != nil {
		t.Fatal(err)
	}
	handle := "gca2a_t1_" + base64.RawURLEncoding.EncodeToString(entropy)
	if _, err := rand.Read(entropy); err != nil {
		t.Fatal(err)
	}
	auth := base64.RawURLEncoding.EncodeToString(entropy)
	buffer := logging.NewLogBuffer(100)
	logger := slog.New(logging.NewBufferHandler(buffer, nil))
	previousProvider := otel.GetTracerProvider()
	previousPropagator := otel.GetTextMapPropagator()
	defer otel.SetTracerProvider(previousProvider)
	defer otel.SetTextMapPropagator(previousPropagator)
	provider := tracing.NewProvider(&tracing.Config{Enabled: true, Sampling: 1, MaxTraces: 100})
	if err := provider.Init(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = provider.Shutdown(context.Background()) }()
	recorder, err := runs.NewRecorder(runs.Config{Enabled: true, Dir: t.TempDir(), MaxBytes: 1 << 20, MaxAge: time.Hour, ShutdownDrain: time.Second}, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer recorder.Close()
	gateway := mcp.NewGateway()
	defer gateway.Close()
	gateway.SetLogger(logger)
	gateway.SetRunSink(recSink{rec: recorder})
	accumulator := metrics.NewAccumulator(100)
	gateway.SetToolCallObserver(metrics.NewObserver(token.NewHeuristicCounter(4), accumulator))
	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprint(port))
	waitForPort(t, ctx, port)
	if err := gateway.RegisterMCPServer(ctx, mcp.MCPServerConfig{Name: "echo", Transport: mcp.TransportHTTP, Endpoint: fmt.Sprintf("http://127.0.0.1:%d/mcp", port)}); err != nil {
		t.Fatal(err)
	}
	server := api.NewServer(gateway, nil)
	server.SetAuth("bearer", auth, "")
	server.SetStackName("capability-diagnostics")
	server.SetRunRecorder(recorder)
	server.SetLogBuffer(buffer)
	server.SetTraceBuffer(provider.Buffer)
	server.SetMetricsAccumulator(accumulator)
	listener := httptest.NewServer(server.Handler())
	defer listener.Close()
	request := func(method, path string, input any) (int, []byte) {
		t.Helper()
		var body []byte
		if input != nil {
			var err error
			body, err = json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, listener.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+auth)
		req.Header.Set("Content-Type", "application/json")
		response, err := listener.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, data
	}
	status, body := request(http.MethodPost, "/api/tools/call", map[string]any{"name": "echo__echo", "client": handle, "arguments": map[string]any{"message": handle}})
	if status != http.StatusOK {
		t.Fatalf("call status %d", status)
	}
	var call struct {
		Result *mcp.ToolCallResult `json:"result"`
	}
	if err := json.Unmarshal(body, &call); err != nil {
		t.Fatal(err)
	}
	if call.Result == nil || call.Result.IsError || !strings.Contains(call.Result.Content[0].Text, handle) {
		t.Fatal("ordinary source delivery changed")
	}
	_, _ = request(http.MethodPost, "/api/tools/call", map[string]any{"name": "echo__" + handle, "client": handle, "arguments": map[string]any{}})
	recorder.Close()
	for _, path := range []string{"/api/logs", "/api/traces", "/api/metrics/tokens", "/api/runs"} {
		status, body := request(http.MethodGet, path, nil)
		if status != http.StatusOK {
			t.Fatalf("%s status %d", path, status)
		}
		if bytes.Contains(body, []byte(handle)) || bytes.Contains(body, []byte(auth)) {
			t.Fatalf("secret in %s", path)
		}
		if len(body) == 0 {
			t.Fatalf("empty evidence for %s", path)
		}
	}
	if accumulator.Snapshot().Session.InputTokens == 0 || provider.Buffer.Count() != 2 || buffer.Count() == 0 {
		t.Fatal("observation sinks were not exercised")
	}
	query, err := runs.Query(ctx, recorder.DirPath(), runs.Filter{}, 10, nil, recorder.WipeEpoch())
	if err != nil || len(query.Records) != 2 {
		t.Fatal("persisted run evidence missing", err)
	}
}
