package api

import (
	"context"
	"encoding/json"

	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/agent/runner"
	"github.com/gridctl/gridctl/pkg/mcp"
	"github.com/gridctl/gridctl/pkg/registry"
)

// runPersisterAdapter bridges the MCP gateway's RunPersister hook to
// the runner + persist.Store. It is the single chokepoint that decides
// whether a routed tool call should land in ~/.gridctl/runs/<run_id>.jsonl:
// typed-skill calls routed to the registry server are persisted via
// runner.Run; everything else (Go-plugin lookups bypassing tools/list,
// upstream MCP servers proxied through the router) falls through to
// direct dispatch with no ledger side effect.
//
// Persistence wiring is dynamic — the adapter consults the API server's
// runStore() and registryServer at PersistAndCall time so partial
// fixtures (test wiring that sets only one component) and the
// production SetAgentRuntime sequence both work without ordering
// constraints.
type runPersisterAdapter struct {
	server *Server
}

func newRunPersisterAdapter(s *Server) *runPersisterAdapter {
	return &runPersisterAdapter{server: s}
}

// PersistAndCall implements mcp.RunPersister.
func (a *runPersisterAdapter) PersistAndCall(ctx context.Context, client mcp.AgentClient, toolName string, arguments map[string]any) (string, *mcp.ToolCallResult, error) {
	if a == nil || a.server == nil || client == nil {
		return passthrough(ctx, client, toolName, arguments)
	}

	store, regServer := a.resolve()
	if store == nil || regServer == nil {
		return passthrough(ctx, client, toolName, arguments)
	}

	// Only persist calls that route through the typed-skill registry
	// server. Upstream MCP proxies surface under their own client name
	// and stay stateless.
	if client.Name() != regServer.Name() {
		return passthrough(ctx, client, toolName, arguments)
	}

	sk, err := regServer.Store().GetSkill(toolName)
	if err != nil || sk.HandlerLanguage != "ts" {
		return passthrough(ctx, client, toolName, arguments)
	}

	args := arguments
	if args == nil {
		args = map[string]any{}
	}
	rawInput, err := json.Marshal(args)
	if err != nil {
		// Argument shapes routed through the gateway have already been
		// JSON-decoded once, so re-marshalling should never fail in
		// practice; record an empty object rather than dropping the
		// run on the floor.
		rawInput = []byte(`{}`)
	}

	return runner.Run(ctx, store, client, runner.StartOptions{
		Skill:    toolName,
		Flavor:   sk.HandlerLanguage,
		Input:    args,
		RawInput: rawInput,
	})
}

func (a *runPersisterAdapter) resolve() (*persist.Store, *registry.Server) {
	store := a.server.runStore()
	regServer := a.server.registryServer
	return store, regServer
}

func passthrough(ctx context.Context, client mcp.AgentClient, toolName string, arguments map[string]any) (string, *mcp.ToolCallResult, error) {
	result, err := client.CallTool(ctx, toolName, arguments)
	return "", result, err
}
