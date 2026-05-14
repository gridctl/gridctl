// Package sandbox runs typed TypeScript skills in goja with the
// gridctl-shaped agent bindings (tool, llm, parallel, handoff,
// approval) injected as globals. It is a thin layer over the existing
// pkg/mcp Code Mode infrastructure: transpile via esbuild, execute on
// a single-shot goja runtime owned by an event loop, deliver async
// results through Promises that loop.RunOnLoop schedules back onto
// the event loop thread.
//
// Recursive composability is the design constraint that shapes this
// package: tool() flows through a gridctl-shaped ToolCaller (which is
// almost always a *mcp.Gateway adapter), so a TS skill calling a tool
// goes through the gateway's existing tracing, pricing, replica
// routing, vault auth, and tool whitelisting paths. handoff() routes
// to the same skill registry the gateway exposes as MCP tools — the
// "skill calls another skill" path is the same bytes-on-the-wire path
// an upstream MCP client would take, just short-circuited in-process.
//
// Phase C scope: bindings work; sandboxing is a single-call event
// loop; the approval() binding is a stub that auto-approves (the real
// approval gates land in Phase E). Hot-reload is Phase F. JSONL
// persistence is Phase E.
package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// MaxSourceSize caps the transpiled-source length the sandbox will
// run. Skills are expected to be small; very large bundles point at a
// missing import-bundling step that should land before sandbox
// execution, not be papered over here.
const MaxSourceSize = 256 * 1024

// DefaultTimeout bounds a single skill invocation. Skills with their
// own deadlines should set ctx.WithTimeout before calling Execute.
const DefaultTimeout = 60 * time.Second

// DefaultMaxParallel is the soft cap on parallel() concurrency. The
// hard cap and orchestrator-level coordination land in Phase D; this
// keeps a runaway parallel() from spawning thousands of goroutines.
const DefaultMaxParallel = 4

// Bindings are the runtime-supplied collaborators the sandbox injects
// as JS globals. Every field is optional: bindings that go unset are
// either omitted from the global scope (tool, llm, handoff) or stubbed
// (approval auto-approves when ApprovalDecider is nil). Skills that
// touch a missing binding raise a JS error at call time.
type Bindings struct {
	// ToolCaller dispatches tool() calls. The runtime gives this an
	// adapter over *mcp.Gateway so MCP tracing, pricing, replica
	// routing, vault auth, and tool whitelisting all apply.
	ToolCaller agent.ToolCaller

	// AllowedTools is the ACL the tool() binding consults before
	// dispatching. Each tool's Name is the prefixed form
	// (server__tool); if the binding receives an unprefixed name and
	// only one tool with that suffix is allowed, the binding allows
	// it. Empty AllowedTools disables tool() entirely.
	AllowedTools []mcp.Tool

	// ChatModel is the LLM provider the llm() binding dispatches to.
	// nil disables the binding.
	ChatModel agent.ChatModel

	// SkillCaller is the dispatcher handoff() uses to invoke another
	// skill. The runtime wires this to the registry's CallTool path
	// so a skill-to-skill handoff and an upstream MCP client calling
	// the same skill share one code path. nil disables the binding.
	SkillCaller SkillCaller

	// Approver is invoked by approval(). nil auto-approves with the
	// prompt as the response — Phase E replaces the stub with a real
	// gate.
	Approver Approver

	// MaxParallel caps parallel() concurrency. Zero means
	// DefaultMaxParallel.
	MaxParallel int

	// SkillBody is the post-frontmatter markdown the registry parsed
	// from this invocation's SKILL.md. The runtime exposes it to the
	// JS sandbox as `skill.body` so TS skills can drive the same
	// hybrid pattern Go skills hit through ctx.SkillBody() — feed
	// per-skill prose into an llm() call's `system` field without
	// hardcoding it in the handler. Empty string for skills with no
	// body and for harness paths (tests) that don't plumb one through.
	SkillBody string

	// SkillName is the registered skill name. Exposed as `skill.name`
	// in the JS sandbox for parity with Go's ctx.SkillName().
	SkillName string

	// Session, when non-nil, is the per-invocation telemetry handle
	// the bindings emit through. The runtime wires this to the
	// parent run's recorder so external MCP clients firing tools/call
	// against a typed skill see the same per-node trace the in-IDE
	// Run Launcher produces. A nil Session disables emission — the
	// bindings still function, the ledger just records the run's
	// terminal boundaries only.
	Session *RunSession
}

// SkillCaller is the dispatch surface handoff() uses. The signature
// matches mcp.AgentClient.CallTool exactly so callers can hand the
// sandbox the same registry-backed dispatcher the gateway uses.
type SkillCaller interface {
	CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.ToolCallResult, error)
}

// Approver decides an approval gate. Phase C ships a stub auto-approver
// when no real Approver is wired; Phase E replaces it with the
// CLI/web/MCP-backed gate.
type Approver interface {
	Approve(ctx context.Context, prompt string) (ApprovalDecision, error)
}

// ApprovalDecision is what an Approver returns. The string is surfaced
// back to the JS caller verbatim so authors can pattern-match on it.
type ApprovalDecision struct {
	// Approved reports the gate decision. False rejects the request;
	// the JS caller sees `{ approved: false, reason }` and can branch.
	Approved bool

	// Reason is a free-form note. Authors typically include it in the
	// next prompt or log it for audit.
	Reason string
}

// Sandbox is a transpiler + goja runtime factory. The struct itself
// is reusable across calls; each Execute creates a fresh runtime, so
// scripts cannot leak state across invocations.
type Sandbox struct {
	timeout time.Duration
}

// New constructs a sandbox. A non-positive timeout is replaced with
// DefaultTimeout so callers cannot accidentally disable the deadline.
func New(timeout time.Duration) *Sandbox {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Sandbox{timeout: timeout}
}

// Result is what Execute returns to the caller. Value is the JSON
// encoding of the skill's resolved return value (a Promise for async
// skills); Console captures every console.log/warn/error line.
type Result struct {
	// Value is the skill's resolved return value, encoded as JSON. An
	// empty string means the skill returned undefined or null.
	Value string

	// Console is one captured line per console.log/warn/error call,
	// in invocation order.
	Console []string
}

// Execute transpiles the TypeScript source, runs it under a fresh
// goja runtime with the supplied bindings injected, and returns the
// resolved value plus captured console output. The skill is expected
// to assign its handler to module.exports.default — the standard
// shape after esbuild emits CommonJS for `export default async function ...`.
//
// Execute honors ctx cancellation: a deadline-exceeded ctx terminates
// the event loop and reports the timeout as an error.
func (s *Sandbox) Execute(ctx context.Context, source string, input any, b Bindings) (*Result, error) {
	if len(source) > MaxSourceSize {
		return nil, fmt.Errorf("source too large: %d bytes (maximum is %d)", len(source), MaxSourceSize)
	}
	transpiled, err := transpileTS(source)
	if err != nil {
		return nil, err
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshaling skill input: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	loop := eventloop.NewEventLoop(eventloop.EnableConsole(false))

	var consoleMu sync.Mutex
	var consoleOutput []string
	var resultVal goja.Value
	var runErr error

	// done closes when loop.Run returns; the watchdog goroutine
	// selects on it so a normal completion does not leak a goroutine
	// that interrupts a runtime the caller no longer owns.
	done := make(chan struct{})
	defer close(done)

	loop.Run(func(vm *goja.Runtime) {
		// Terminate the loop when ctx expires so a skill that holds
		// the runtime hostage cannot block past the deadline.
		go func() {
			select {
			case <-runCtx.Done():
				vm.Interrupt("execution timeout exceeded")
				loop.Terminate()
			case <-done:
			}
		}()

		installConsole(vm, &consoleMu, &consoleOutput)
		installModuleHarness(vm)
		installSkillBinding(vm, b)
		installToolBinding(vm, loop, runCtx, s.timeout, b)
		installLLMBinding(vm, loop, runCtx, s.timeout, b)
		installParallelBinding(vm, loop, b)
		installHandoffBinding(vm, loop, runCtx, s.timeout, b)
		installApprovalBinding(vm, loop, runCtx, s.timeout, b)
		// Install the require() shim after the bindings so it can read
		// their global values into the @gridctl/agent module object.
		installRequireShim(vm)

		// Run the transpiled skill source first; this populates
		// module.exports.default with the handler the harness then
		// invokes with the parsed input.
		if _, err := vm.RunString(transpiled); err != nil {
			runErr = fmt.Errorf("loading skill module: %w", err)
			return
		}

		harness := fmt.Sprintf(`
			(function() {
				if (!module.exports || typeof module.exports.default !== 'function') {
					throw new Error('skill must export a default function');
				}
				return module.exports.default(%s);
			})()
		`, string(inputJSON))

		raw, err := vm.RunString(harness)
		if err != nil {
			runErr = err
			return
		}

		// If the harness returned a thenable, attach .then handlers
		// that capture the settled value into the outer resultVal /
		// runErr the post-loop block reads. The capture has to happen
		// here, not in a helper that returns by value, because the
		// .then callbacks fire later in the same loop.Run pass and
		// they must write into the variable the post-loop block reads.
		if raw == nil || goja.IsUndefined(raw) || goja.IsNull(raw) {
			resultVal = raw
			return
		}
		obj := raw.ToObject(vm)
		thenFn := obj.Get("then")
		if thenFn == nil || goja.IsUndefined(thenFn) {
			resultVal = raw
			return
		}
		thenCallable, ok := goja.AssertFunction(thenFn)
		if !ok {
			resultVal = raw
			return
		}
		_, _ = thenCallable(raw,
			vm.ToValue(func(call goja.FunctionCall) goja.Value {
				if len(call.Arguments) > 0 {
					resultVal = call.Arguments[0]
				}
				return goja.Undefined()
			}),
			vm.ToValue(func(call goja.FunctionCall) goja.Value {
				if len(call.Arguments) > 0 {
					runErr = fmt.Errorf("skill rejected: %s", call.Arguments[0].String())
				}
				return goja.Undefined()
			}),
		)
	})

	if runErr != nil {
		if runCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("skill execution exceeded %s timeout", s.timeout)
		}
		if jsErr, ok := runErr.(*goja.InterruptedError); ok {
			return nil, fmt.Errorf("skill execution interrupted: %s", jsErr.Value())
		}
		return nil, runErr
	}

	consoleMu.Lock()
	console := append([]string(nil), consoleOutput...)
	consoleMu.Unlock()
	out := &Result{Console: console}
	if resultVal != nil && !goja.IsUndefined(resultVal) && !goja.IsNull(resultVal) {
		exported := resultVal.Export()
		if encoded, err := json.Marshal(exported); err == nil {
			out.Value = string(encoded)
		} else {
			out.Value = resultVal.String()
		}
	}
	return out, nil
}

// installConsole replaces goja's default console with a capturing one.
// The mutex is owned by the caller so the post-loop snapshot can read
// safely; binding goroutines that have not yet delivered (e.g. a
// timed-out tool call still completing) will never deliver because
// their loop.RunOnLoop closures are dropped after Terminate, but they
// can still touch consoleOutput before we read it. The snapshot is
// taken under the same lock to keep that path race-free.
func installConsole(vm *goja.Runtime, mu *sync.Mutex, out *[]string) {
	console := vm.NewObject()
	logFn := func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, arg := range call.Arguments {
			parts[i] = arg.String()
		}
		mu.Lock()
		*out = append(*out, strings.Join(parts, " "))
		mu.Unlock()
		return goja.Undefined()
	}
	_ = console.Set("log", logFn)
	_ = console.Set("warn", logFn)
	_ = console.Set("error", logFn)
	_ = vm.Set("console", console)
}

// installModuleHarness exposes the CommonJS module / exports globals
// esbuild's CommonJS output writes into. Skills that omit the default
// export raise an error in the harness wrapper.
func installModuleHarness(vm *goja.Runtime) {
	module := vm.NewObject()
	exports := vm.NewObject()
	_ = module.Set("exports", exports)
	_ = vm.Set("module", module)
	_ = vm.Set("exports", exports)
}

// agentModuleName is the only module specifier the require() shim
// resolves. Skills that author against the gridctl SDK import from this
// package; everything else fails fast so authors notice unsupported
// imports at runtime rather than getting a silently-empty module.
const agentModuleName = "@gridctl/agent"

// agentModuleExports lists the names the @gridctl/agent module re-exports
// — they map 1:1 onto the bindings each install* function registers as a
// JS global. Keeping this in one place ensures a new binding is exposed
// through both the global path and the import path with one change.
var agentModuleExports = []string{"tool", "llm", "parallel", "handoff", "approval", "skill"}

// installRequireShim wires a minimal CommonJS-style require() that
// resolves the @gridctl/agent specifier the scaffold (and esbuild's
// CommonJS output) emits. The shim returns an object whose properties
// are the goja values the binding installs already registered as
// globals — that's why this call MUST run after every install* binding.
//
// Any other module name throws so unexpected imports surface as a
// runtime error rather than a silently-undefined member access.
func installRequireShim(vm *goja.Runtime) {
	_ = vm.Set("require", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(vm.NewGoError(fmt.Errorf("require: missing module name")))
		}
		name := call.Arguments[0].String()
		if name != agentModuleName {
			panic(vm.NewGoError(fmt.Errorf("require: unknown module %q", name)))
		}
		mod := vm.NewObject()
		for _, export := range agentModuleExports {
			// A goja Set on a property whose binding is missing yields
			// `undefined`; copying the live value preserves the function
			// identity so esbuild's `(0, mod.tool)(...)` indirect-call
			// form still dispatches to the binding closure.
			_ = mod.Set(export, vm.Get(export))
		}
		return vm.ToValue(mod)
	})
}

