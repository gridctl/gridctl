//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// TestPerClientScope_EnforcedAcrossPaths is the end-to-end guard for the
// per-client access model. It runs two real mock MCP servers ("alpha" and
// "beta"), scopes a client to "alpha" only under default-deny, and asserts the
// scope holds on every exposure path for two distinct client identities —
// crucially including the code-mode search/execute surface, which sources its
// tool universe from the same aggregated set as the direct path and would
// otherwise be a bypass.
func TestPerClientScope_EnforcedAcrossPaths(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if mockHTTPServerBin == "" {
		t.Skip("mock server binary not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	alphaPort := freePort(t)
	betaPort := freePort(t)
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", alphaPort))
	startMockServer(t, mockHTTPServerBin, "-port", fmt.Sprintf("%d", betaPort))
	waitForPort(t, ctx, alphaPort)
	waitForPort(t, ctx, betaPort)

	gw := mcp.NewGateway()
	t.Cleanup(func() { gw.Close() })

	for name, port := range map[string]int{"alpha": alphaPort, "beta": betaPort} {
		cfg := mcp.MCPServerConfig{
			Name:      name,
			Transport: mcp.TransportHTTP,
			Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		}
		if err := gw.RegisterMCPServer(ctx, cfg); err != nil {
			t.Fatalf("RegisterMCPServer(%s): %v", name, err)
		}
	}

	// cursor is scoped to alpha only; everyone else is denied by default.
	gw.SetClientAccessPolicy(mcp.NewClientAccessPolicy(&mcp.ClientAccessSpec{
		Default: "deny",
		Profiles: map[string]mcp.ClientProfileSpec{
			"cursor": {Servers: []string{"alpha"}},
		},
	}))

	cursorCtx := mcp.WithClientAccessID(ctx, "cursor")
	windsurfCtx := mcp.WithClientAccessID(ctx, "windsurf") // unlisted -> denied

	// --- Direct path: tools/list (code mode off) ---
	t.Run("tools_list_scoped", func(t *testing.T) {
		res, err := gw.HandleToolsList(cursorCtx)
		if err != nil {
			t.Fatalf("HandleToolsList: %v", err)
		}
		if len(res.Tools) == 0 {
			t.Fatal("scoped client should see alpha tools, got none")
		}
		for _, tool := range res.Tools {
			if !strings.HasPrefix(tool.Name, "alpha__") {
				t.Errorf("cursor tools/list leaked out-of-scope tool %q", tool.Name)
			}
		}

		// Unlisted client under default-deny sees nothing.
		res, err = gw.HandleToolsList(windsurfCtx)
		if err != nil {
			t.Fatalf("HandleToolsList(windsurf): %v", err)
		}
		if len(res.Tools) != 0 {
			t.Errorf("unlisted client should see no tools, got %d", len(res.Tools))
		}
	})

	// --- Direct path: tools/call rejection ---
	t.Run("tools_call_denied", func(t *testing.T) {
		res, err := gw.HandleToolsCall(cursorCtx, mcp.ToolCallParams{
			Name:      "beta__echo",
			Arguments: map[string]any{"message": "hi"},
		})
		if err != nil {
			t.Fatalf("HandleToolsCall: %v", err)
		}
		if !res.IsError || !strings.Contains(res.Content[0].Text, "access scope") {
			t.Errorf("expected beta__echo to be denied for cursor, got %+v", res.Content)
		}

		// An in-scope call succeeds against the real downstream server.
		res, err = gw.HandleToolsCall(cursorCtx, mcp.ToolCallParams{
			Name:      "alpha__echo",
			Arguments: map[string]any{"message": "hi"},
		})
		if err != nil {
			t.Fatalf("HandleToolsCall(alpha__echo): %v", err)
		}
		if res.IsError {
			t.Errorf("in-scope alpha__echo should succeed, got %+v", res.Content)
		}
	})

	// --- Code-mode path: search universe + execute enforcement ---
	t.Run("code_mode_universe_scoped", func(t *testing.T) {
		gw.SetCodeMode(10 * time.Second)

		// search must not reveal beta tools to cursor.
		res, err := gw.HandleToolsCall(cursorCtx, mcp.ToolCallParams{
			Name:      mcp.MetaToolSearch,
			Arguments: map[string]any{"query": ""},
		})
		if err != nil {
			t.Fatalf("code-mode search: %v", err)
		}
		if strings.Contains(res.Content[0].Text, "beta__") {
			t.Errorf("code-mode search leaked beta tools to cursor: %q", res.Content[0].Text)
		}
		if !strings.Contains(res.Content[0].Text, "alpha__echo") {
			t.Errorf("code-mode search should expose in-scope alpha tools: %q", res.Content[0].Text)
		}

		// execute calling an out-of-scope tool is blocked inside the sandbox
		// because the scoped universe excludes it.
		denied, err := gw.HandleToolsCall(cursorCtx, mcp.ToolCallParams{
			Name: mcp.MetaToolExecute,
			Arguments: map[string]any{
				"code": `(async () => { return await mcp.callTool("beta", "echo", {message: "x"}); })()`,
			},
		})
		if err != nil {
			t.Fatalf("code-mode execute(beta): %v", err)
		}
		if !denied.IsError || !strings.Contains(denied.Content[0].Text, "access denied") {
			t.Errorf("expected code-mode beta call to be denied, got %+v", denied.Content)
		}

		// execute calling an in-scope tool succeeds end-to-end.
		ok, err := gw.HandleToolsCall(cursorCtx, mcp.ToolCallParams{
			Name: mcp.MetaToolExecute,
			Arguments: map[string]any{
				"code": `(async () => { return await mcp.callTool("alpha", "echo", {message: "ok"}); })()`,
			},
		})
		if err != nil {
			t.Fatalf("code-mode execute(alpha): %v", err)
		}
		if ok.IsError {
			t.Errorf("in-scope code-mode alpha call should succeed, got %+v", ok.Content)
		}
	})
}
