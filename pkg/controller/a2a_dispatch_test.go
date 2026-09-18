package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gridctl/gridctl/internal/api"
	"github.com/gridctl/gridctl/pkg/config"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/pins"
)

func TestInstallSchemaPinning_A2ADispatchAfterStartup(t *testing.T) {
	for _, mode := range []string{"disabled", "warn", "block", "stackless"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			var revision, calls atomic.Int64
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.Header().Set("Cache-Control", "no-store")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"url": "http://" + r.Host, "protocolVersion": "0.3.0", "description": fmt.Sprint(revision.Load()),
						"defaultInputModes": []string{"text/plain"}, "defaultOutputModes": []string{"text/plain"},
					})
					return
				}
				calls.Add(1)
				var req struct{ ID string }
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{
					"kind": "message", "messageId": "response", "role": "agent", "parts": []any{map[string]any{"kind": "text", "text": "hello"}},
				}})
			}))
			defer remote.Close()
			g := mcp.NewGateway()
			defer g.Close()
			srv := api.NewServer(g, nil)
			store := pins.NewWithPath(t.TempDir(), "startup")
			stack := pinningStack(&config.SchemaPinningConfig{Action: mode, Enabled: boolPtr(mode != "disabled")})
			if mode == "stackless" {
				stack = nil
			}
			installSchemaPinning(g, srv, stack, store)
			if err := g.RegisterMCPServer(t.Context(), mcp.MCPServerConfig{Name: "agent", A2A: true, PinSchemas: boolPtr(false), A2AConfig: &mcp.A2AClientConfig{Card: remote.URL}}); err != nil {
				t.Fatal(err)
			}
			call := func() *mcp.ToolCallResult {
				t.Helper()
				result, err := g.CallTool(t.Context(), "agent__send", map[string]any{"message": "hello"})
				if err != nil || result == nil {
					t.Fatal("missing adapter response", err)
				}
				return result
			}
			if call().IsError || calls.Load() != 1 {
				t.Fatal("startup trust did not admit first use")
			}
			revision.Add(1)
			if !call().IsError || calls.Load() != 1 {
				t.Fatal("legacy pin settings bypassed card drift")
			}
			snapshot, err := g.CardTrust().Snapshot(t.Context(), "agent")
			if err != nil {
				t.Fatal(err)
			}
			if err := g.CardTrust().Approve(t.Context(), "agent", snapshot.Hash()); err != nil {
				t.Fatal(err)
			}
			if call().IsError || calls.Load() != 2 {
				t.Fatal("approved adapter remained blocked")
			}
		})
	}
}
