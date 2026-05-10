// Package runtime aggregates the agent runtime's process-wide state into
// a single handle the gateway hangs off via SetAgentRuntime. Centralising
// the run store, approval registry, sandbox, dev server, and active LLM
// provider lets downstream consumers (HTTP API, CLI, dispatcher) pull
// every collaborator from one place rather than threading four setters
// through the API server.
//
// The package lives below pkg/agent (rather than alongside it) because
// pkg/agent already imports pkg/mcp; placing Runtime here lets us
// import pkg/agent/sandbox + compose + persist + dev/devserver without
// taking pkg/mcp into the closure of those packages. The gateway
// holds *Runtime as an opaque mcp.AgentRuntime — consumers type-assert
// to read the components.
package runtime

import (
	"sync"

	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/agent/compose"
	"github.com/gridctl/gridctl/pkg/agent/dev/devserver"
	"github.com/gridctl/gridctl/pkg/agent/persist"
	"github.com/gridctl/gridctl/pkg/agent/sandbox"
)

// Runtime is the agent runtime aggregate. Construct with NewRuntime;
// late-binding components (ChatModel, DevServer) plug in via setters
// once their upstream factories have run. All accessors are safe for
// concurrent use.
type Runtime struct {
	mu sync.RWMutex

	runStore         *persist.Store
	approvalRegistry *compose.Registry
	sandbox          *sandbox.Sandbox

	chatModel agent.ChatModel
	devServer *devserver.Server
}

// NewRuntime constructs a Runtime with the load-bearing components the
// dispatcher and HTTP handlers need at process start. The ChatModel and
// DevServer plug in later once the API server has resolved them.
func NewRuntime(store *persist.Store, reg *compose.Registry, sb *sandbox.Sandbox) *Runtime {
	return &Runtime{
		runStore:         store,
		approvalRegistry: reg,
		sandbox:          sb,
	}
}

// AgentRuntimeMarker satisfies the mcp.AgentRuntime interface so the
// Gateway can hold *Runtime opaquely without the agent → mcp → agent
// import cycle. The method is intentionally empty — the marker exists
// purely to keep the Gateway's accessor type-safe.
func (*Runtime) AgentRuntimeMarker() {}

// RunStore returns the JSONL run-event ledger. Nil only when NewRuntime
// was called without one (defensive — callers should always pass a real
// store).
func (r *Runtime) RunStore() *persist.Store {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.runStore
}

// ApprovalRegistry returns the in-process approval-gate registry.
func (r *Runtime) ApprovalRegistry() *compose.Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.approvalRegistry
}

// Sandbox returns the goja sandbox used to execute TypeScript skills.
func (r *Runtime) Sandbox() *sandbox.Sandbox {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sandbox
}

// SetChatModel installs (or replaces) the LLM provider exposed to skills
// via the sandbox's llm() binding. Plumbed late because the provider is
// built by the API-server stage of gateway construction, after the
// runtime aggregate already exists.
func (r *Runtime) SetChatModel(m agent.ChatModel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chatModel = m
}

// ChatModel returns the active LLM provider, or nil if none is wired.
// Callers should treat nil as "llm() unavailable" and surface a clear
// error rather than panicking.
func (r *Runtime) ChatModel() agent.ChatModel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.chatModel
}

// SetDevServer installs the agent IDE dev server. nil disables the
// /api/agent/dev/* routes, which then return 503.
func (r *Runtime) SetDevServer(d *devserver.Server) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.devServer = d
}

// DevServer returns the agent IDE dev server, or nil when not wired.
func (r *Runtime) DevServer() *devserver.Server {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.devServer
}
