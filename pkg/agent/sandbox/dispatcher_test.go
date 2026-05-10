package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gridctl/gridctl/pkg/mcp"
)

// TestDispatcher_DispatchReadsSourceAndWrapsResult is the unit-test
// proof that the concrete Dispatcher reads the TS source from the
// supplied path, runs it under the sandbox with per-call Bindings, and
// wraps the resolved value in an mcp.ToolCallResult shaped the way an
// MCP tool reply does. This is the path the gateway hands to external
// MCP clients calling a TS skill by name.
func TestDispatcher_DispatchReadsSourceAndWrapsResult(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "echo.ts")
	src := `export default async function (i: any) { return { echoed: i }; }`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatalf("writing skill source: %v", err)
	}

	sb := New(2 * time.Second)
	disp, err := NewDispatcher(sb, func(_ context.Context, _ string) Bindings { return Bindings{} })
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	res, err := disp.Dispatch(context.Background(), "echo", path, map[string]any{"v": 7})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(res.Content) != 1 {
		t.Fatalf("expected one content block, got %d", len(res.Content))
	}
	text := res.Content[0].Text
	if !strings.Contains(text, `"echoed":`) || !strings.Contains(text, `"v":7`) {
		t.Errorf("content text = %q, want JSON containing echoed.v=7", text)
	}
}

// TestDispatcher_BindingsProviderReceivesContextAndName confirms the
// per-call BindingsProvider hook is invoked with the run's context and
// skill name on every Dispatch — that is the surface long-lived
// dispatchers use to scope ToolCaller/ChatModel/Approver to one call.
func TestDispatcher_BindingsProviderReceivesContextAndName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "noop.ts")
	if err := os.WriteFile(path, []byte(`export default async function () { return null; }`), 0o600); err != nil {
		t.Fatalf("writing skill source: %v", err)
	}

	type captured struct {
		ctx  context.Context
		name string
	}
	calls := []captured{}
	sb := New(2 * time.Second)
	disp, err := NewDispatcher(sb, func(ctx context.Context, name string) Bindings {
		calls = append(calls, captured{ctx: ctx, name: name})
		return Bindings{}
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	type ctxKey string
	parentCtx := context.WithValue(context.Background(), ctxKey("trace"), "abc")
	if _, err := disp.Dispatch(parentCtx, "skill-name", path, nil); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("BindingsProvider invoked %d times, want 1", len(calls))
	}
	if calls[0].name != "skill-name" {
		t.Errorf("BindingsProvider got name=%q, want skill-name", calls[0].name)
	}
	if got := calls[0].ctx.Value(ctxKey("trace")); got != "abc" {
		t.Errorf("BindingsProvider context lost trace value: got %v", got)
	}
}

// TestDispatcher_DispatchReportsMissingSource confirms a missing source
// file surfaces as a structured error rather than a panic.
func TestDispatcher_DispatchReportsMissingSource(t *testing.T) {
	t.Parallel()
	sb := New(2 * time.Second)
	disp, err := NewDispatcher(sb, nil)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	_, err = disp.Dispatch(context.Background(), "ghost", filepath.Join(t.TempDir(), "missing.ts"), nil)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if !strings.Contains(err.Error(), `ts skill "ghost"`) {
		t.Errorf("err = %v, want skill-named error", err)
	}
}

// TestDispatcher_NewDispatcherRejectsNilSandbox locks in the
// constructor's contract: a nil sandbox is a programmer error caught
// at wire time, not at first call.
func TestDispatcher_NewDispatcherRejectsNilSandbox(t *testing.T) {
	t.Parallel()
	_, err := NewDispatcher(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil sandbox")
	}
	if !strings.Contains(err.Error(), "non-nil sandbox") {
		t.Errorf("err = %v, want explanation that sandbox is required", err)
	}
}

// TestDispatcher_SatisfiesRegistryTSDispatcherShape is a structural
// check: the concrete Dispatcher implements the dispatch contract the
// registry server consumes (Dispatch(ctx, name, path, args) → result).
// The registry interface lives in pkg/registry; we mirror its shape
// here so a future signature change in either package fails this test
// rather than a downstream integration.
func TestDispatcher_SatisfiesRegistryTSDispatcherShape(t *testing.T) {
	t.Parallel()
	type tsDispatcher interface {
		Dispatch(ctx context.Context, name, sourcePath string, arguments map[string]any) (*mcp.ToolCallResult, error)
	}
	sb := New(time.Second)
	disp, err := NewDispatcher(sb, nil)
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	var _ tsDispatcher = disp
}
