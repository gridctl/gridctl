//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

func TestA2AAdapter_CurrentPolicyGuards(t *testing.T) {
	for _, version := range []string{"0.3", "1.0"} {
		t.Run(version, func(t *testing.T) {
			f := newA2AAdapterFixture(t, version)
			g := mcp.NewGateway()
			defer g.Close()
			if err := g.SetCardPinStorage(t.Context(), pins.NewWithPath(t.TempDir(), "policy")); err != nil {
				t.Fatal(err)
			}
			if err := g.RegisterMCPServer(t.Context(), mcp.MCPServerConfig{Name: "agent", A2A: true, A2AConfig: &mcp.A2AClientConfig{Card: f.server.URL}}); err != nil {
				t.Fatal(err)
			}
			result, err := g.CallTool(t.Context(), "agent__send", map[string]any{"message": "hello"})
			if err != nil {
				t.Fatal(err)
			}
			handle := a2aAdapterEnvelope(t, result)["task_handle"].(string)
			get := map[string]any{"task_handle": handle}
			denied := func(ctx context.Context, name string, args map[string]any) {
				t.Helper()
				beforeGET, beforeRPC := f.gets.Load(), f.calls.Load()
				result, err := g.HandleToolsCall(ctx, mcp.ToolCallParams{Name: name, Arguments: args})
				if err == nil && (result == nil || !result.IsError) {
					t.Fatal("policy-denied authority dispatched")
				}
				if f.gets.Load() != beforeGET || f.calls.Load() != beforeRPC {
					t.Fatal("policy denial reached discovery or RPC")
				}
			}
			adapter := g.Router().GetClient("agent").(*mcp.A2AClient)
			adapter.SetToolWhitelist([]string{"send"})
			g.Router().RefreshTools()
			denied(t.Context(), "agent__task_get", get)
			if len(adapter.PinSnapshot().Records()) != 6 {
				t.Fatal("whitelist removed mandatory trust evidence")
			}
			adapter.SetToolWhitelist(nil)
			g.Router().RefreshTools()
			g.SetClientAccessPolicy(mcp.NewClientAccessPolicy(&mcp.ClientAccessSpec{Default: "deny", Profiles: map[string]mcp.ClientProfileSpec{"permitted": {Servers: []string{"agent"}}}}))
			denied(t.Context(), "agent__task_get", get)
			denied(mcp.WithClientAccessID(t.Context(), "impersonator"), "agent__task_get", get)
			allowed := mcp.WithClientAccessID(t.Context(), "permitted")
			result, err = g.CallTool(allowed, "agent__task_get", get)
			if err != nil || result.IsError {
				t.Fatal("valid deliberate capability transfer rejected")
			}
			g.SetGroupPolicy(mcp.NewGroupPolicy(mcp.GroupsSpec{"readers": {Tools: []string{"agent__task_get"}, Overrides: map[string]mcp.GroupOverrideSpec{"agent__task_get": {Name: "read"}}}}))
			group := mcp.WithGroup(allowed, "readers")
			result, err = g.HandleToolsCall(group, mcp.ToolCallParams{Name: "read", Arguments: get})
			if err != nil || result.IsError || a2aAdapterEnvelope(t, result)["task_handle"] != handle {
				t.Fatal("group alias failed verified authority dispatch")
			}
			denied(group, "agent__task_cancel", get)
			denied(group, "read", map[string]any{"task_handle": "raw-task-id"})
			g.SetCodeMode(5 * time.Second)
			code := fmt.Sprintf(`mcp.callTool("agent", "read", {task_handle: %q})`, handle)
			result, err = g.HandleToolsCall(group, mcp.ToolCallParams{Name: mcp.MetaToolExecute, Arguments: map[string]any{"code": code}})
			if err != nil || result.IsError || !strings.Contains(result.Content[0].Text, handle) {
				t.Fatal("group code-mode result lost capability")
			}
			denied(group, mcp.MetaToolExecute, map[string]any{"code": fmt.Sprintf(`mcp.callTool("agent", "task_cancel", {task_handle: %q})`, handle)})
			denied(group, mcp.MetaToolExecute, map[string]any{"code": `const r = mcp.callTool("agent", "read", {task_handle: "raw-task-id"}); if (r.error) throw r.error; r;`})
			installLimits(g, &config.LimitsConfig{RateLimits: []config.RateLimit{{Server: "agent", CallsPerMinute: 1, Burst: 1}}})
			result, err = g.CallTool(allowed, "agent__task_get", get)
			if err != nil || result.IsError {
				t.Fatal("initial rate budget unavailable")
			}
			denied(allowed, "agent__task_get", get)
		})
	}
}
