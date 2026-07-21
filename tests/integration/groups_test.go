//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/mcp"
)

func groupsTestPolicy() *mcp.GroupPolicy {
	readOnly := true
	return mcp.NewGroupPolicy(mcp.GroupsSpec{
		"release": {
			Description: "Release bundle",
			Servers:     []string{"alpha"},
			Overrides: map[string]mcp.GroupOverrideSpec{
				"alpha__echo": {
					Name:         "shout",
					Description:  "Echo a message back, loudly.",
					ReadOnlyHint: &readOnly,
				},
			},
		},
	})
}

// TestToolGroups_EndToEnd exercises a group endpoint through the real HTTP
// transport against a real mock MCP server: session binding, the curated and
// rewritten tools/list surface, rename dispatch, deny-on-call, the unknown-
// group 404, and code-mode enforcement inside the sandbox.
func TestToolGroups_EndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
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
		if err := gw.RegisterMCPServer(ctx, mcp.MCPServerConfig{
			Name:      name,
			Transport: mcp.TransportHTTP,
			Endpoint:  fmt.Sprintf("http://127.0.0.1:%d/mcp", port),
		}); err != nil {
			t.Fatalf("RegisterMCPServer(%s): %v", name, err)
		}
	}
	gw.SetGroupPolicy(groupsTestPolicy())

	apiServer := api.NewServer(gw, nil)
	ts := httptest.NewServer(apiServer.Handler())
	t.Cleanup(ts.Close)

	post := func(t *testing.T, path, session string, body map[string]any) (*http.Response, map[string]any) {
		t.Helper()
		raw, _ := json.Marshal(body)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ts.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		var decoded map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&decoded)
		resp.Body.Close()
		return resp, decoded
	}

	initialize := func(t *testing.T, path string) string {
		t.Helper()
		resp, _ := post(t, path, "", map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "initialize",
			"params": map[string]any{
				"protocolVersion": "2025-06-18",
				"clientInfo":      map[string]any{"name": "claude-code", "version": "1.0"},
			},
		})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("initialize on %s: status %d", path, resp.StatusCode)
		}
		session := resp.Header.Get("Mcp-Session-Id")
		if session == "" {
			t.Fatalf("initialize on %s returned no session id", path)
		}
		return session
	}

	t.Run("unknown_group_404s_before_session", func(t *testing.T) {
		resp, _ := post(t, "/groups/nope/mcp", "", map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "initialize",
			"params": map[string]any{"protocolVersion": "2025-06-18", "clientInfo": map[string]any{"name": "x"}},
		})
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("unknown group initialize: status %d, want 404", resp.StatusCode)
		}
	})

	session := initialize(t, "/groups/release/mcp")

	t.Run("tools_list_is_curated_and_rewritten", func(t *testing.T) {
		_, body := post(t, "/groups/release/mcp", session, map[string]any{
			"jsonrpc": "2.0", "id": 2, "method": "tools/list",
		})
		raw, _ := json.Marshal(body)
		text := string(raw)
		if !strings.Contains(text, `"shout"`) {
			t.Errorf("renamed tool missing from group surface: %s", text)
		}
		if !strings.Contains(text, "Echo a message back, loudly.") {
			t.Errorf("description rewrite missing: %s", text)
		}
		if !strings.Contains(text, `"readOnlyHint":true`) {
			t.Errorf("injected annotation missing: %s", text)
		}
		if strings.Contains(text, "alpha__echo") || strings.Contains(text, "beta__") {
			t.Errorf("group surface leaked canonical or non-member tools: %s", text)
		}
	})

	t.Run("renamed_call_dispatches", func(t *testing.T) {
		_, body := post(t, "/groups/release/mcp", session, map[string]any{
			"jsonrpc": "2.0", "id": 3, "method": "tools/call",
			"params": map[string]any{"name": "shout", "arguments": map[string]any{"message": "hi"}},
		})
		raw, _ := json.Marshal(body)
		if !strings.Contains(string(raw), "Echo: hi") {
			t.Fatalf("renamed call did not dispatch downstream: %s", raw)
		}
	})

	t.Run("non_member_call_denied_in_band", func(t *testing.T) {
		_, body := post(t, "/groups/release/mcp", session, map[string]any{
			"jsonrpc": "2.0", "id": 4, "method": "tools/call",
			"params": map[string]any{"name": "beta__echo", "arguments": map[string]any{"message": "hi"}},
		})
		raw, _ := json.Marshal(body)
		text := string(raw)
		if !strings.Contains(text, `"isError":true`) || !strings.Contains(text, `not in group \"release\"`) {
			t.Fatalf("expected in-band group denial, got: %s", text)
		}
	})

	t.Run("default_endpoint_unchanged", func(t *testing.T) {
		defSession := initialize(t, "/mcp")
		_, body := post(t, "/mcp", defSession, map[string]any{
			"jsonrpc": "2.0", "id": 5, "method": "tools/list",
		})
		raw, _ := json.Marshal(body)
		if !strings.Contains(string(raw), "alpha__echo") || !strings.Contains(string(raw), "beta__echo") {
			t.Errorf("default endpoint no longer serves the full canonical surface: %s", raw)
		}
	})

	t.Run("code_mode_sandbox_enforced", func(t *testing.T) {
		gw.SetCodeMode(10 * time.Second)
		t.Cleanup(func() { gw.SetGroupPolicy(groupsTestPolicy()) })
		groupCtx := mcp.WithGroup(ctx, "release")

		// A sandboxed call to a member (via its canonical construction)
		// succeeds; the group ctx re-enters HandleToolsCall.
		ok, err := gw.HandleToolsCall(groupCtx, mcp.ToolCallParams{
			Name: mcp.MetaToolExecute,
			Arguments: map[string]any{
				"code": `(async () => { return await mcp.callTool("alpha", "shout", {message: "in"}); })()`,
			},
		})
		if err != nil {
			t.Fatalf("code-mode execute(member): %v", err)
		}
		if ok.IsError {
			t.Fatalf("sandboxed member call should succeed: %+v", ok.Content)
		}

		denied, err := gw.HandleToolsCall(groupCtx, mcp.ToolCallParams{
			Name: mcp.MetaToolExecute,
			Arguments: map[string]any{
				"code": `(async () => { return await mcp.callTool("beta", "echo", {message: "out"}); })()`,
			},
		})
		if err != nil {
			t.Fatalf("code-mode execute(non-member): %v", err)
		}
		// Two layers can deny: the sandbox's own ACL (its universe is the
		// group surface, so the tool is simply absent) or, if anything got
		// past it, the gateway's group membership check on re-entry.
		if !denied.IsError ||
			!(strings.Contains(denied.Content[0].Text, "not in group") ||
				strings.Contains(denied.Content[0].Text, "access denied")) {
			t.Fatalf("sandboxed non-member call should be denied: %+v", denied.Content)
		}
	})
}
