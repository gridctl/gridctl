//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/runs"
)

// TestWhitelistDispatch_EnforcedAcrossPaths registers a real mock HTTP server
// with a tools: whitelist and checks every exposure path. The mock advertises
// echo, add, and get_time; only echo is visible. A hidden call that reached
// the mock would succeed, so an unknown-tool error is also proof the
// downstream server was not invoked.
func TestWhitelistDispatch_EnforcedAcrossPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	port := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", port))
	waitForPort(t, ctx, port)

	gw := mcp.NewGateway()
	t.Cleanup(func() { gw.Close() })
	if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
		Name:      "alpha",
		Transport: mcp.TransportHTTP,
		Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		Tools:     []string{"echo"},
	}); err != nil {
		t.Fatalf("RegisterMCPServer: %v", err)
	}
	gw.SetGroupPolicy(mcp.NewGroupPolicy(mcp.GroupsSpec{
		"grp": {Servers: []string{"alpha"}},
	}))

	hidden := mcp.ToolCallParams{Name: "alpha__add", Arguments: map[string]any{"a": 1, "b": 2}}
	visible := mcp.ToolCallParams{Name: "alpha__echo", Arguments: map[string]any{"message": "hi"}}

	assertHidden := func(t *testing.T, res *mcp.ToolCallResult, err error) {
		t.Helper()
		if err != nil || res == nil || !res.IsError {
			t.Fatalf("expected hidden rejection: err=%v res=%+v", err, res)
		}
		if len(res.Content) == 0 || res.Content[0].Text != "Error: unknown tool" {
			t.Fatalf("hidden text = %+v", res.Content)
		}
	}
	assertVisible := func(t *testing.T, res *mcp.ToolCallResult, err error) {
		t.Helper()
		if err != nil || res == nil || res.IsError {
			t.Fatalf("visible call failed: err=%v res=%+v", err, res)
		}
		if len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, "Echo: hi") {
			t.Fatalf("visible text = %+v", res.Content)
		}
	}

	t.Run("tools_list", func(t *testing.T) {
		res, err := gw.HandleToolsList(ctx)
		if err != nil {
			t.Fatalf("HandleToolsList: %v", err)
		}
		if len(res.Tools) != 1 || res.Tools[0].Name != "alpha__echo" {
			names := make([]string, 0, len(res.Tools))
			for _, tool := range res.Tools {
				names = append(names, tool.Name)
			}
			t.Fatalf("tools/list = %v, want [alpha__echo]", names)
		}
	})

	t.Run("direct", func(t *testing.T) {
		res, err := gw.HandleToolsCall(ctx, hidden)
		assertHidden(t, res, err)
		res, err = gw.HandleToolsCall(ctx, visible)
		assertVisible(t, res, err)
	})

	t.Run("group", func(t *testing.T) {
		groupCtx := mcp.WithGroup(ctx, "grp")
		res, err := gw.HandleToolsCall(groupCtx, hidden)
		assertHidden(t, res, err)
		res, err = gw.HandleToolsCall(groupCtx, visible)
		assertVisible(t, res, err)
	})

	t.Run("canonical", func(t *testing.T) {
		res, out, err := gw.CallCanonicalTool(ctx, hidden)
		assertHidden(t, res, err)
		if out.Reason != runs.ReasonUnknownTool || out.Stage != runs.StageRouting || out.Completion != mcp.CompletionNotStarted {
			t.Fatalf("outcome = %+v", out)
		}
		res, out, err = gw.CallCanonicalTool(ctx, visible)
		assertVisible(t, res, err)
		if out.Disposition != runs.DispositionCompleted {
			t.Fatalf("visible outcome = %+v", out)
		}
	})

	gw.SetCodeMode(10 * time.Second)

	t.Run("code mode direct", func(t *testing.T) {
		res, err := gw.HandleToolsCall(ctx, hidden)
		assertHidden(t, res, err)
		res, err = gw.HandleToolsCall(ctx, visible)
		assertVisible(t, res, err)
	})

	t.Run("code mode execute", func(t *testing.T) {
		denied, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
			Name: mcp.MetaToolExecute,
			Arguments: map[string]any{
				"code": `(async () => { return await mcp.callTool("alpha", "add", {a: 1, b: 2}); })()`,
			},
		})
		if err != nil || denied == nil || !denied.IsError || !strings.Contains(denied.Content[0].Text, "access denied") {
			t.Fatalf("execute hidden = err=%v res=%+v", err, denied)
		}
		ok, err := gw.HandleToolsCall(ctx, mcp.ToolCallParams{
			Name: mcp.MetaToolExecute,
			Arguments: map[string]any{
				"code": `(async () => { return await mcp.callTool("alpha", "echo", {message: "hi"}); })()`,
			},
		})
		if err != nil || ok == nil || ok.IsError {
			t.Fatalf("execute visible = err=%v res=%+v", err, ok)
		}
	})
}
