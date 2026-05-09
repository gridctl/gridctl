package sandbox

import (
	"context"
	"encoding/json"
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
