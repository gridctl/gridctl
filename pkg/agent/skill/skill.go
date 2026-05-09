// Package skill is the gridctl typed Skill SDK. Skills are typed Go
// or TypeScript handlers that the gateway exposes as MCP tools — the
// same envelope an upstream client sees for any other MCP tool. The
// surface here is the typed flavor of pkg/mcp.AgentClient.CallTool,
// not a sibling of it: a registered skill becomes a tool on the
// registry's MCP server and is callable through Gateway.CallTool
// exactly like any tool from a downstream server.
//
// The package has two layers:
//
//   - Definition / Registry — the runtime-facing surface that crosses
//     package boundaries. Definitions carry a name, description, an
//     inferred JSON Schema for input, and an Invoker that takes an
//     untyped argument map and returns *mcp.ToolCallResult. The
//     registry server lifts Definitions into mcp.Tool entries and
//     dispatches CallTool to them.
//
//   - Define[I, O] — the typed authoring surface skill authors use.
//     Inputs and outputs are Go structs with `json` and `jsonschema`
//     tags; the helper infers the input schema, marshals/unmarshals
//     across the boundary, and returns a Definition.
//
// Recursive composability is non-negotiable: local execution
// (gridctl run <skill>) and remote execution (an upstream client
// invoking via the gateway) share one code path. The Invoker
// signature mirrors AgentClient.CallTool so a gridctl instance
// pointed at another over MCP gets the same shape it would if the
// skill ran in-process.
package skill

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// Invoker is the runtime-facing handler signature. It takes the same
// argument map shape pkg/mcp.AgentClient.CallTool receives so the
// registry server can hand calls straight through without translation.
//
// Invokers MUST honor ctx cancellation: an Invoker that ignores ctx
// blocks the gateway's deadline propagation and breaks any caller
// that relies on context-scoped timeouts.
type Invoker func(ctx context.Context, arguments map[string]any) (*mcp.ToolCallResult, error)

// ErrSkillNotRegistered is returned by Registry.CallTool when no
// definition is registered under the requested name. The registry
// server (pkg/registry) tests for this with errors.Is so it can fall
// through to the TS-dispatcher path without coupling to error wording.
var ErrSkillNotRegistered = errors.New("skill: not registered")

// Definition is the runtime-facing skill descriptor. The registry
// server lifts Definitions into mcp.Tool entries the gateway exposes
// over the wire and dispatches CallTool to Invoker.
//
// Build Definitions with Define[I, O] for typed Go skills, or
// construct directly when wrapping a non-Go handler (the TS
// dispatcher in pkg/agent/sandbox does the latter).
type Definition struct {
	// Name is the skill's stable identifier. It becomes the unprefixed
	// MCP tool name the gateway prefixes with the registry server name.
	// Names are constrained to non-empty strings; the registry rejects
	// duplicates.
	Name string

	// Description is the human-readable summary the model sees when
	// deciding whether to call the skill. Treat it as a docstring: the
	// model will pattern-match on it.
	Description string

	// InputSchema is the JSON Schema describing the skill's argument
	// object. For skills built via Define[I, O] the schema is inferred
	// from I; for hand-built definitions, the schema MUST validate as
	// a JSON object. An empty schema reads as "any object" — which is
	// permissive but legal.
	InputSchema json.RawMessage

	// Invoker executes the skill. Errors returned by Invoker propagate
	// to the caller verbatim; the runtime does not retry, log, or
	// translate them. Use *mcp.ToolCallResult with IsError=true for
	// "the skill ran but the result is an error" semantics.
	Invoker Invoker
}

// Tool returns the mcp.Tool envelope the registry exposes for this
// definition. The returned schema is a copy so callers cannot mutate
// the Definition's InputSchema by mutating the Tool.
func (d *Definition) Tool() mcp.Tool {
	schema := d.InputSchema
	if len(schema) == 0 {
		// An empty schema is not valid JSON, which would break clients
		// that try to parse it. Surface the permissive "any object"
		// shape instead so the wire form is always valid.
		schema = json.RawMessage(`{"type":"object"}`)
	}
	out := make(json.RawMessage, len(schema))
	copy(out, schema)
	return mcp.Tool{
		Name:        d.Name,
		Description: d.Description,
		InputSchema: out,
	}
}

// validate returns the first reason the definition is malformed.
// Used by Registry.Register so callers cannot install a Definition
// that the runtime cannot dispatch to.
func (d *Definition) validate() error {
	if d == nil {
		return errors.New("skill: nil definition")
	}
	if d.Name == "" {
		return errors.New("skill: definition has empty name")
	}
	if d.Invoker == nil {
		return fmt.Errorf("skill %q: invoker is nil", d.Name)
	}
	if len(d.InputSchema) > 0 {
		var probe map[string]any
		if err := json.Unmarshal(d.InputSchema, &probe); err != nil {
			return fmt.Errorf("skill %q: input schema is not valid JSON: %w", d.Name, err)
		}
	}
	return nil
}

// Registry is a concurrent-safe collection of Definitions. The
// registry server (pkg/registry) reads from a Registry to populate
// Tools() and dispatch CallTool, so the same Registry that hosts a
// programmatically-registered Go skill also serves it to upstream
// MCP clients through the gateway.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*Definition
}

// NewRegistry constructs an empty Registry.
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]*Definition)}
}

// Register installs a Definition. Returns an error if the definition
// is malformed or its name is already taken — the registry refuses
// duplicates rather than silently overwriting so registration order
// stays observable.
func (r *Registry) Register(def *Definition) error {
	if err := def.validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skills[def.Name]; exists {
		return fmt.Errorf("skill %q: already registered", def.Name)
	}
	r.skills[def.Name] = def
	return nil
}

// Get returns the Definition registered under name, or false when no
// such skill exists. The returned pointer is the registry's own —
// callers MUST NOT mutate it.
func (r *Registry) Get(name string) (*Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.skills[name]
	return def, ok
}

// List returns every registered Definition in name-sorted order. The
// slice is a snapshot; mutations after the call do not affect it.
func (r *Registry) List() []*Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Definition, 0, len(r.skills))
	for _, def := range r.skills {
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Tools returns every registered Definition lifted into the mcp.Tool
// envelope the registry server exposes through Server.Tools(). The
// slice is name-sorted so wire output is deterministic across
// reloads.
func (r *Registry) Tools() []mcp.Tool {
	defs := r.List()
	out := make([]mcp.Tool, len(defs))
	for i, def := range defs {
		out[i] = def.Tool()
	}
	return out
}

// CallTool dispatches a registered skill by unprefixed name. The
// signature matches mcp.AgentClient.CallTool so the registry server
// can delegate without translation. An unknown skill returns an error
// that wraps ErrSkillNotRegistered so callers can distinguish "no such
// skill" from "the skill ran and erred."
func (r *Registry) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	def, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrSkillNotRegistered, name)
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	return def.Invoker(ctx, arguments)
}
