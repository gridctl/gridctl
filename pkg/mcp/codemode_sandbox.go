package mcp

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
)

// MaxCodeSize is the maximum allowed code input size (64KB).
const MaxCodeSize = 64 * 1024

// DefaultCodeModeTimeout is the default code mode execution timeout.
const DefaultCodeModeTimeout = 30 * time.Second

// Sandbox executes JavaScript code in a goja runtime with MCP tool bindings.
type Sandbox struct {
	timeout     time.Duration
	fetchConfig FetchConfig
}

// NewSandbox creates a sandbox with the given execution timeout and default fetch config.
func NewSandbox(timeout time.Duration) *Sandbox {
	if timeout <= 0 {
		timeout = DefaultCodeModeTimeout
	}
	return &Sandbox{timeout: timeout, fetchConfig: DefaultFetchConfig()}
}

// NewSandboxWithConfig creates a sandbox with the given execution timeout and fetch config.
func NewSandboxWithConfig(timeout time.Duration, fetchConfig FetchConfig) *Sandbox {
	if timeout <= 0 {
		timeout = DefaultCodeModeTimeout
	}
	return &Sandbox{timeout: timeout, fetchConfig: fetchConfig}
}

// ExecuteResult contains the output of a sandbox execution.
type ExecuteResult struct {
	Value   string   // Return value (JSON-encoded)
	Console []string // Captured console.log/warn/error output
}

// Execute runs JavaScript code in a fresh goja runtime with MCP tool bindings.
// The code is transpiled from modern JS to ES2015 before execution.
func (s *Sandbox) Execute(ctx context.Context, code string, caller ToolCaller, allowedTools []Tool) (*ExecuteResult, error) {
	if len(code) > MaxCodeSize {
		return nil, fmt.Errorf("code too large: %d bytes (maximum is %d bytes)", len(code), MaxCodeSize)
	}

	// Transpile modern JS to ES2015 for goja compatibility
	transpiled, err := Transpile(code)
	if err != nil {
		return nil, fmt.Errorf("transpilation failed: %w", err)
	}

	// Set up execution timeout
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// Create event loop — provides setTimeout/clearTimeout on the runtime
	loop := eventloop.NewEventLoop(eventloop.EnableConsole(false))

	// Build allowed tool set for ACL enforcement
	toolSet := make(map[string]bool, len(allowedTools))
	for _, t := range allowedTools {
		toolSet[t.Name] = true
	}

	var consoleOutput []string
	var val goja.Value
	var runErr error

	loop.Run(func(vm *goja.Runtime) {
		// Interrupt JS execution and terminate the event loop on timeout.
		// loop.Terminate() cancels pending timers, preventing goroutine leaks
		// when long-duration sleeps are interrupted mid-execution.
		go func() {
			<-ctx.Done()
			vm.Interrupt("execution timeout exceeded")
			loop.Terminate()
		}()

		// Disable timer APIs not supported by this sandbox
		_ = vm.Set("setInterval", goja.Undefined())
		_ = vm.Set("clearInterval", goja.Undefined())
		_ = vm.Set("setImmediate", goja.Undefined())
		_ = vm.Set("clearImmediate", goja.Undefined())

		// Inject console object (overrides goja_nodejs default with output capture)
		console := vm.NewObject()
		logFn := func(call goja.FunctionCall) goja.Value {
			parts := make([]string, len(call.Arguments))
			for i, arg := range call.Arguments {
				parts[i] = arg.String()
			}
			consoleOutput = append(consoleOutput, strings.Join(parts, " "))
			return goja.Undefined()
		}
		_ = console.Set("log", logFn)
		_ = console.Set("warn", logFn)
		_ = console.Set("error", logFn)
		_ = vm.Set("console", console)

		// Inject mcp.callTool binding
		mcpObj := vm.NewObject()
		_ = mcpObj.Set("callTool", func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) < 2 {
				panic(vm.NewGoError(fmt.Errorf("mcp.callTool requires at least 2 arguments: serverName, toolName, [args]")))
			}

			serverName := call.Arguments[0].String()
			toolName := call.Arguments[1].String()

			var args map[string]any
			if len(call.Arguments) >= 3 {
				exported := call.Arguments[2].Export()
				if m, ok := exported.(map[string]any); ok {
					args = m
				}
			}

			// Build prefixed tool name (server__tool)
			prefixedName := serverName + ToolNameDelimiter + toolName

			// Enforce ACL
			if !toolSet[prefixedName] {
				panic(vm.NewGoError(fmt.Errorf("access denied: tool '%s' from server '%s' is not available", toolName, serverName)))
			}

			// Call the tool through the gateway
			result, err := caller.CallTool(ctx, prefixedName, args)
			if err != nil {
				panic(vm.NewGoError(fmt.Errorf("tool call failed: %w", err)))
			}

			if result.IsError {
				text := ""
				for _, c := range result.Content {
					if c.Text != "" {
						text = c.Text
						break
					}
				}
				panic(vm.NewGoError(fmt.Errorf("tool error: %s", text)))
			}

			// Parse the tool result content into a native JS value
			// so agents can immediately access fields (e.g., result.field)
			for _, c := range result.Content {
				if c.Text != "" {
					var parsed any
					if json.Unmarshal([]byte(c.Text), &parsed) == nil {
						return vm.ToValue(parsed)
					}
					return vm.ToValue(c.Text)
				}
			}

			return goja.Undefined()
		})
		_ = vm.Set("mcp", mcpObj)

		// Inject crypto object with randomUUID()
		cryptoObj := vm.NewObject()
		_ = cryptoObj.Set("randomUUID", func(call goja.FunctionCall) goja.Value {
			var b [16]byte
			if _, err := crand.Read(b[:]); err != nil {
				panic(vm.NewGoError(fmt.Errorf("crypto.randomUUID: %w", err)))
			}
			b[6] = (b[6] & 0x0f) | 0x40 // version 4
			b[8] = (b[8] & 0x3f) | 0x80 // variant bits
			uuid := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
			return vm.ToValue(uuid)
		})
		_ = vm.Set("crypto", cryptoObj)

		// Inject sleep(ms) — returns a Promise that resolves after ms milliseconds.
		// Primary use case: await sleep(1000) for polling delays and retry backoff.
		_ = vm.Set("sleep", func(call goja.FunctionCall) goja.Value {
			if len(call.Arguments) == 0 {
				panic(vm.NewGoError(fmt.Errorf("sleep requires a delay argument")))
			}
			delay := time.Duration(call.Arguments[0].ToInteger()) * time.Millisecond
			promise, resolve, _ := vm.NewPromise()
			loop.SetTimeout(func(*goja.Runtime) {
				_ = resolve(goja.Undefined())
			}, delay)
			return vm.ToValue(promise)
		})

		// Inject fetch(url, options) — sandboxed HTTP client with SSRF mitigations
		sf := newSandboxedFetch(s.fetchConfig)
		sf.inject(vm, loop)

		rawVal, runStringErr := vm.RunString(transpiled)
		if runStringErr != nil {
			runErr = runStringErr
			return
		}

		// If the returned value is a thenable (Promise from an async function or
		// explicit new Promise), attach .then/.catch handlers to capture the
		// settled value. The event loop will call the handlers when the Promise
		// settles, driven by whatever timers or RunOnLoop callbacks are pending.
		if rawVal != nil && !goja.IsUndefined(rawVal) && !goja.IsNull(rawVal) {
			rawObj := rawVal.ToObject(vm)
			thenFn := rawObj.Get("then")
			if thenFn != nil && !goja.IsUndefined(thenFn) {
				if thenCallable, ok := goja.AssertFunction(thenFn); ok {
					_, _ = thenCallable(rawVal,
						vm.ToValue(func(call goja.FunctionCall) goja.Value {
							if len(call.Arguments) > 0 {
								val = call.Arguments[0]
							}
							return goja.Undefined()
						}),
						vm.ToValue(func(call goja.FunctionCall) goja.Value {
							if len(call.Arguments) > 0 {
								runErr = fmt.Errorf("runtime error: %s", call.Arguments[0].String())
							}
							return goja.Undefined()
						}),
					)
					return // val will be set by .then callback once Promise settles
				}
			}
		}

		val = rawVal
	})

	if runErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("execution exceeded %s timeout", s.timeout)
		}
		if jsErr, ok := runErr.(*goja.InterruptedError); ok {
			return nil, fmt.Errorf("execution interrupted: %s", jsErr.Value())
		}
		return nil, fmt.Errorf("runtime error: %w", runErr)
	}

	// Format the return value
	execResult := &ExecuteResult{
		Console: consoleOutput,
	}

	if val != nil && !goja.IsUndefined(val) && !goja.IsNull(val) {
		exported := val.Export()
		jsonBytes, jsonErr := json.Marshal(exported)
		if jsonErr == nil {
			execResult.Value = string(jsonBytes)
		} else {
			execResult.Value = val.String()
		}
	}

	return execResult, nil
}
