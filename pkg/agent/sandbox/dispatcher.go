package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/gridctl/gridctl/pkg/agent/skill"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// SourceLoader returns the TypeScript source for a skill by name.
// The dispatcher calls SourceLoader on every invocation rather than
// caching so that on-disk edits become visible without a registry
// reload — the watcher in Phase F will narrow this further, but the
// uncached read is safe for Phase C.
type SourceLoader func(name string) (string, error)

// FileSourceLoader returns a SourceLoader that reads the file at the
// path the lookup callback resolves for the given skill name. The
// indirection lets the registry walker hand the dispatcher a closure
// over the registered skill paths without leaking the registry's
// internal layout into this package.
func FileSourceLoader(lookup func(name string) (string, bool)) SourceLoader {
	return func(name string) (string, error) {
		path, ok := lookup(name)
		if !ok {
			return "", fmt.Errorf("ts skill %q: source path not registered", name)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("ts skill %q: reading source: %w", name, err)
		}
		return string(data), nil
	}
}

// BindingsProvider returns the Bindings the dispatcher uses for one
// invocation. The closure is called per Dispatch so a long-lived
// dispatcher can hand each call request-scoped collaborators (a fresh
// ToolCaller, the current ChatModel, the run's Approver) without
// caching state across calls.
type BindingsProvider func(ctx context.Context, skillName string) Bindings

// Dispatcher implements registry.TSDispatcher: each Dispatch reads the
// TS skill source from disk and runs it through the sandbox using the
// per-call Bindings. The struct is constructed once at gateway-build
// time and registered on the registry server via SetTSDispatcher; all
// per-call collaborators flow through the BindingsProvider closure.
type Dispatcher struct {
	sb       *Sandbox
	bindings BindingsProvider
}

// NewDispatcher constructs a Dispatcher. Passing a nil sandbox is
// rejected — the caller is expected to construct a Sandbox with the
// timeout it wants enforced for skill execution. A nil bindings
// provider is allowed; calls then run with empty Bindings (no tool(),
// no llm(), etc.) and any binding access from the skill raises a JS
// error at call time.
func NewDispatcher(sb *Sandbox, bp BindingsProvider) (*Dispatcher, error) {
	if sb == nil {
		return nil, errors.New("sandbox: dispatcher requires a non-nil sandbox")
	}
	return &Dispatcher{sb: sb, bindings: bp}, nil
}

// Dispatch runs `name`'s TS source through the sandbox and wraps the
// returned value in an mcp.ToolCallResult shaped the way a typed-skill
// MCP tool reply does. The marshaling matches NewInvoker's so the
// bytes-on-the-wire are the same whether the gateway calls the skill
// via the dispatcher (external MCP clients) or via the typed-skill
// registry (handoff() inside another skill).
func (d *Dispatcher) Dispatch(ctx context.Context, name, sourcePath string, arguments map[string]any) (*mcp.ToolCallResult, error) {
	if d == nil || d.sb == nil {
		return nil, errors.New("sandbox: dispatcher not initialized")
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("ts skill %q: reading source: %w", name, err)
	}
	var b Bindings
	if d.bindings != nil {
		b = d.bindings(ctx, name)
	}
	result, err := d.sb.Execute(ctx, string(source), arguments, b)
	if err != nil {
		return nil, err
	}
	text := result.Value
	if text == "" {
		text = "null"
	}
	// Validate the value is JSON; if it isn't (the skill returned a
	// non-serialisable value), wrap it in a JSON string so the upstream
	// caller still receives a syntactically valid content block.
	var probe any
	if err := json.Unmarshal([]byte(text), &probe); err != nil {
		text = fmt.Sprintf("%q", result.Value)
	}
	return &mcp.ToolCallResult{
		Content: []mcp.Content{mcp.NewTextContent(text)},
	}, nil
}

// NewInvoker builds a skill.Invoker that runs `name`'s TS source
// inside the sandbox using the supplied bindings. The returned
// Invoker is safe to register on a skill.Registry — it satisfies the
// signature exactly.
//
// The bindings closure is called per-invocation so a long-lived
// dispatcher can hand each call a fresh ToolCaller, ChatModel, or
// SkillCaller that captures call-scoped context (e.g., a request-id
// or tracing span).
func (s *Sandbox) NewInvoker(name string, loadSource SourceLoader, bindings func(ctx context.Context) Bindings) skill.Invoker {
	return func(ctx context.Context, arguments map[string]any) (*mcp.ToolCallResult, error) {
		source, err := loadSource(name)
		if err != nil {
			return nil, err
		}
		var b Bindings
		if bindings != nil {
			b = bindings(ctx)
		}
		result, err := s.Execute(ctx, source, arguments, b)
		if err != nil {
			return nil, err
		}
		text := result.Value
		if text == "" {
			text = "null"
		}
		// Validate the value is JSON; if it isn't (the skill returned a
		// non-serialisable value), wrap it in a JSON string so the
		// upstream caller still gets a syntactically valid content
		// block.
		var probe any
		if err := json.Unmarshal([]byte(text), &probe); err != nil {
			text = fmt.Sprintf("%q", result.Value)
		}
		return &mcp.ToolCallResult{
			Content: []mcp.Content{mcp.NewTextContent(text)},
		}, nil
	}
}
