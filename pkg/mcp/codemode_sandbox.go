package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// MaxCodeSize is the maximum allowed code input size (64KB).
const MaxCodeSize = 64 * 1024

// DefaultCodeModeTimeout is the default code mode execution timeout.
const DefaultCodeModeTimeout = 30 * time.Second

// Sandbox executes JavaScript code in a goja runtime with MCP tool bindings.
type Sandbox struct {
	timeout time.Duration
}

// NewSandbox creates a sandbox with the given execution timeout.
func NewSandbox(timeout time.Duration) *Sandbox {
	if timeout <= 0 {
		timeout = DefaultCodeModeTimeout
	}
	return &Sandbox{timeout: timeout}
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

	// Create fresh runtime (prevents state leakage between executions)
	vm := goja.New()

	// Set up timeout via interrupt
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	go func() {
		<-ctx.Done()
		vm.Interrupt("execution timeout exceeded")
	}()

	// Build allowed tool set for ACL enforcement
	toolSet := make(map[string]bool, len(allowedTools))
	for _, t := range allowedTools {
		toolSet[t.Name] = true
	}

	// Capture console output
	var consoleOutput []string

	// Inject console object
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

	// Execute the transpiled code
	val, err := vm.RunString(transpiled)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("execution exceeded %s timeout", s.timeout)
		}
		if jsErr, ok := err.(*goja.InterruptedError); ok {
			return nil, fmt.Errorf("execution interrupted: %s", jsErr.Value())
		}
		return nil, fmt.Errorf("runtime error: %w", err)
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
