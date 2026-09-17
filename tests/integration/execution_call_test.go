//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/execution"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runtime"
	dockerruntime "github.com/gridctl/gridctl/pkg/runtime/docker"
)

func TestExecution_CanonicalCallAdmission(t *testing.T) {
	info, err := runtime.DetectRuntime(runtime.DetectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rt, err := dockerruntime.NewWithInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	if err := rt.EnsureImage(ctx, "python:3.13-alpine"); err != nil {
		t.Fatal(err)
	}
	uid, gid := uint32(65534), uint32(65534)
	server := config.MCPServer{Image: "python:3.13-alpine", Transport: "stdio", Execution: &config.ExecutionConfig{Mode: "hardened", UID: &uid, GID: &gid}}
	contract, err := config.ResolveExecution(server)
	if err != nil {
		t.Fatal(err)
	}
	cfg := runtime.WorkloadConfig{Name: "fixture", Stack: fmt.Sprintf("execution-call-%d", time.Now().UnixNano()), Type: runtime.WorkloadTypeMCPServer, Image: "python:3.13-alpine", Command: []string{"python", "-u", "-c", pythonFixtureModule + "\nmain()\n"}, Transport: "stdio", Execution: contract}
	t.Cleanup(func() {
		cleanupCtx, done := context.WithTimeout(context.Background(), 20*time.Second)
		defer done()
		exists, id, err := rt.Exists(cleanupCtx, dockerruntime.ContainerName(cfg.Stack, cfg.Name))
		if err == nil && exists {
			_ = rt.Remove(cleanupCtx, id)
		}
	})
	status, err := rt.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	gateway := mcp.NewGateway()
	t.Cleanup(func() { gateway.Close() })
	gateway.SetDockerClient(rt.Client())
	client, err := gateway.BuildAgentClient(ctx, mcp.MCPServerConfig{
		Name: "fixture", Transport: mcp.TransportStdio, ContainerID: string(status.ID), ProtocolGeneration: "handshake", Execution: server.Execution,
		ExecutionCheck: func(ctx context.Context) (*execution.Report, error) {
			return rt.CheckExecution(ctx, string(status.ID), contract)
		},
		ExecutionBeforeStart: func(ctx context.Context) error {
			return rt.CheckExecutionBeforeStart(ctx, string(status.ID), contract)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c, ok := client.(io.Closer); ok {
			_ = c.Close()
		}
	})
	if err := client.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	gateway.Router().AddClient(client)
	gateway.Router().RefreshTools()

	result, out, err := gateway.CallCanonicalTool(ctx, mcp.ToolCallParams{Name: "fixture__echo", Arguments: map[string]any{"message": "hello"}})
	if err != nil || result == nil || result.IsError || out.Reason != "ok" {
		t.Fatalf("positive call failed: %v %+v %+v", err, result, out)
	}

	apiSrv := api.NewServer(gateway, nil)
	handler := apiSrv.Handler()
	req := httptest.NewRequest(http.MethodPost, "/api/tools/call", bytes.NewBufferString(`{"name":"fixture__echo","arguments":{"message":"via-rest"}}`))
	req.Host = "localhost:8180"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rest status %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("rest body %s", rec.Body.String())
	}

	if closer, ok := client.(io.Closer); ok {
		_ = closer.Close()
	}
	denied, dout, err := gateway.CallCanonicalTool(ctx, mcp.ToolCallParams{Name: "fixture__echo", Arguments: map[string]any{"message": "no"}})
	if err != nil {
		t.Fatal(err)
	}
	if dout.Detail != mcp.DetailExecutionAdmission && dout.Reason != "unknown_tool" && (denied == nil || !denied.IsError) {
		t.Fatalf("expected admission or routing failure after close: %+v %+v", denied, dout)
	}
}
