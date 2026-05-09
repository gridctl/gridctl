package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/gridctl/gridctl/pkg/agent"
	"github.com/gridctl/gridctl/pkg/mcp"
)

// keepAliveSlack is the headroom over the sandbox execution timeout
// that the asyncDeliver watchdog uses. The runCtx watchdog in
// Sandbox.Execute is the real timeout; this timer's only job is to
// keep the event loop pinned open so loop.RunOnLoop deliveries from
// goroutines actually fire, and to surface a missed-delivery bug as
// a Promise rejection rather than a hung loop. The slack ensures the
// timer outlives the runCtx so a deliver mid-shutdown still lands.
const keepAliveSlack = 5 * time.Second

// asyncDeliver wraps the keep-the-event-loop-alive + deliver pattern
// every async binding follows. The returned function is safe to call
// from a goroutine: it schedules its closure back onto the event loop
// thread (where vm and loop are owned) and clears the keepAlive timer
// so the loop can drain. The reject argument is invoked only when the
// keepAlive timer fires before any delivery, surfacing missed-delivery
// as a Promise rejection rather than a hung loop.
func asyncDeliver(vm *goja.Runtime, loop *eventloop.EventLoop, timeout time.Duration, reject func(reason any) error, hint string) (deliver func(fn func(*goja.Runtime))) {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	keepAlive := loop.SetTimeout(func(*goja.Runtime) {
		_ = reject(vm.NewGoError(fmt.Errorf("%s: result was never delivered", hint)))
	}, timeout+keepAliveSlack)
	deliver = func(fn func(*goja.Runtime)) {
		loop.RunOnLoop(func(vm *goja.Runtime) {
			loop.ClearTimeout(keepAlive)
			fn(vm)
		})
	}
	return deliver
}

// normalizeArgs round-trips a JS-exported argument map through JSON
// so values follow JSON conventions (numbers as float64, etc.). goja
// exports JS integers as int64 and floats as float64; tool callers
// downstream of the sandbox typically receive arguments via JSON
// transport and expect float64 throughout. Round-tripping flattens
// the difference at the boundary.
func normalizeArgs(raw any) map[string]any {
	if raw == nil {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	out := map[string]any{}
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil
	}
	return out
}

// installToolBinding wires `tool(name, args)` as a global. The async
// shape mirrors codemode_fetch.go: build a Promise, do the dispatch
// in a goroutine, deliver the result back to the event loop thread
// via loop.RunOnLoop.
//
// The binding accepts both the prefixed (server__tool) and unprefixed
// (tool) forms. Unprefixed names are resolved against AllowedTools by
// matching the suffix; if exactly one allowed tool matches, the
// binding uses it. Multiple matches force the caller to pass the
// prefixed form.
func installToolBinding(vm *goja.Runtime, loop *eventloop.EventLoop, ctx context.Context, timeout time.Duration, b Bindings) {
	if b.ToolCaller == nil || len(b.AllowedTools) == 0 {
		_ = vm.Set("tool", func(call goja.FunctionCall) goja.Value {
			panic(vm.NewGoError(fmt.Errorf("tool() is not available: no ToolCaller wired or AllowedTools is empty")))
		})
		return
	}
	allowed := make(map[string]bool, len(b.AllowedTools))
	for _, t := range b.AllowedTools {
		allowed[t.Name] = true
	}

	_ = vm.Set("tool", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(vm.NewGoError(fmt.Errorf("tool() requires at least 1 argument: name, [args]")))
		}
		name := call.Arguments[0].String()
		var args map[string]any
		if len(call.Arguments) >= 2 {
			args = normalizeArgs(call.Arguments[1].Export())
		}

		resolved, err := resolveToolName(name, allowed)
		if err != nil {
			panic(vm.NewGoError(err))
		}

		promise, resolve, reject := vm.NewPromise()
		deliver := asyncDeliver(vm, loop, timeout, reject, fmt.Sprintf("tool %q", resolved))
		go func() {
			res, err := b.ToolCaller.CallTool(ctx, resolved, args)
			deliver(func(vm *goja.Runtime) {
				if err != nil {
					_ = reject(vm.NewGoError(fmt.Errorf("tool %q: %w", resolved, err)))
					return
				}
				if res != nil && res.IsError {
					_ = reject(vm.NewGoError(fmt.Errorf("tool %q error: %s", resolved, contentText(res))))
					return
				}
				_ = resolve(vm.ToValue(decodeContent(res)))
			})
		}()
		return vm.ToValue(promise)
	})
}

// resolveToolName accepts either a prefixed (server__tool) or
// unprefixed (tool) name and returns the prefixed form the gateway
// expects, or an error if the resolution is ambiguous or the tool is
// not allowed.
func resolveToolName(name string, allowed map[string]bool) (string, error) {
	if allowed[name] {
		return name, nil
	}
	if strings.Contains(name, mcp.ToolNameDelimiter) {
		return "", fmt.Errorf("tool %q: not in allowed tool list", name)
	}
	// Unprefixed: look for a single suffix match.
	suffix := mcp.ToolNameDelimiter + name
	var matches []string
	for prefixed := range allowed {
		if strings.HasSuffix(prefixed, suffix) {
			matches = append(matches, prefixed)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("tool %q: not in allowed tool list", name)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("tool %q: ambiguous (matches %s); use the prefixed form", name, strings.Join(matches, ", "))
	}
}

// installLLMBinding wires `llm(req)` as a global. The request shape
// is a JS object that maps to agent.ChatRequest; the response is a JS
// object that maps to agent.ChatResponse.
func installLLMBinding(vm *goja.Runtime, loop *eventloop.EventLoop, ctx context.Context, timeout time.Duration, b Bindings) {
	if b.ChatModel == nil {
		_ = vm.Set("llm", func(call goja.FunctionCall) goja.Value {
			panic(vm.NewGoError(fmt.Errorf("llm() is not available: no ChatModel wired")))
		})
		return
	}
	_ = vm.Set("llm", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(vm.NewGoError(fmt.Errorf("llm() requires a request object")))
		}
		raw := call.Arguments[0].Export()
		req, err := buildChatRequest(raw)
		if err != nil {
			panic(vm.NewGoError(err))
		}

		promise, resolve, reject := vm.NewPromise()
		deliver := asyncDeliver(vm, loop, timeout, reject, "llm")
		go func() {
			resp, err := b.ChatModel.Generate(ctx, req)
			deliver(func(vm *goja.Runtime) {
				if err != nil {
					_ = reject(vm.NewGoError(fmt.Errorf("llm: %w", err)))
					return
				}
				_ = resolve(vm.ToValue(chatResponseToMap(resp)))
			})
		}()
		return vm.ToValue(promise)
	})
}

// buildChatRequest converts the JS request object into the gridctl
// ChatRequest shape. The conversion goes through json.Marshal /
// Unmarshal so field names follow the agent package's JSON tags
// without a hand-maintained mapping table.
func buildChatRequest(raw any) (agent.ChatRequest, error) {
	if raw == nil {
		return agent.ChatRequest{}, fmt.Errorf("llm: request is null or undefined")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return agent.ChatRequest{}, fmt.Errorf("llm: marshaling request: %w", err)
	}
	var req agent.ChatRequest
	if err := json.Unmarshal(encoded, &req); err != nil {
		return agent.ChatRequest{}, fmt.Errorf("llm: decoding request: %w", err)
	}
	if req.Model == "" {
		return agent.ChatRequest{}, fmt.Errorf("llm: model is required")
	}
	if len(req.Messages) == 0 {
		return agent.ChatRequest{}, fmt.Errorf("llm: messages is required")
	}
	return req, nil
}

// chatResponseToMap renders the response into a map shaped for JS
// consumers. The round-trip via JSON keeps the shape stable across
// changes to the Go struct without touching the JS contract.
func chatResponseToMap(resp agent.ChatResponse) map[string]any {
	encoded, err := json.Marshal(resp)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	out := map[string]any{}
	_ = json.Unmarshal(encoded, &out)
	return out
}

// installParallelBinding wires `parallel(items, fn)` — runs `fn` over
// each item with a concurrency cap. The hard orchestrator cap lives
// in Phase D; this implementation defends against trivial blowouts
// by capping at MaxParallel.
func installParallelBinding(vm *goja.Runtime, loop *eventloop.EventLoop, b Bindings) {
	cap := b.MaxParallel
	if cap <= 0 {
		cap = DefaultMaxParallel
	}
	_ = vm.Set("parallel", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewGoError(fmt.Errorf("parallel(items, fn) requires 2 arguments")))
		}
		itemsVal := call.Arguments[0]
		fn, ok := goja.AssertFunction(call.Arguments[1])
		if !ok {
			panic(vm.NewGoError(fmt.Errorf("parallel: second argument must be a function")))
		}

		// Materialise the JS array into a slice of goja.Values up
		// front. Items are passed back into the callback unchanged,
		// preserving identity for objects; the export round-trip is
		// avoided so authors can pass non-JSON-clean data.
		itemsObj := itemsVal.ToObject(vm)
		length := int(itemsObj.Get("length").ToInteger())
		items := make([]goja.Value, length)
		for i := 0; i < length; i++ {
			items[i] = itemsObj.Get(fmt.Sprintf("%d", i))
		}

		final, resolve, reject := vm.NewPromise()
		results := make([]goja.Value, length)
		remaining := length
		var rejected bool

		// Sequential dispatch with a sliding window: at most `cap`
		// promises in flight. Each pending promise's continuation
		// either kicks off the next item or settles `final`. Doing
		// the bookkeeping on the event-loop thread keeps the slice
		// access race-free without a mutex.
		next := length // index of the first item not yet dispatched
		var dispatch func(index int)
		dispatch = func(index int) {
			out, err := fn(goja.Undefined(), items[index], vm.ToValue(index))
			if err != nil {
				if !rejected {
					rejected = true
					_ = reject(vm.NewGoError(fmt.Errorf("parallel[%d]: %w", index, err)))
				}
				return
			}
			outObj := out.ToObject(vm)
			thenFn := outObj.Get("then")
			thenCallable, isPromise := goja.AssertFunction(thenFn)
			if !isPromise {
				results[index] = out
				remaining--
				if next < length {
					idx := next
					next++
					loop.RunOnLoop(func(*goja.Runtime) { dispatch(idx) })
				} else if remaining == 0 && !rejected {
					_ = resolve(vm.ToValue(results))
				}
				return
			}
			_, _ = thenCallable(out,
				vm.ToValue(func(call goja.FunctionCall) goja.Value {
					if rejected {
						return goja.Undefined()
					}
					var v goja.Value
					if len(call.Arguments) > 0 {
						v = call.Arguments[0]
					}
					results[index] = v
					remaining--
					if next < length {
						idx := next
						next++
						dispatch(idx)
					} else if remaining == 0 && !rejected {
						_ = resolve(vm.ToValue(results))
					}
					return goja.Undefined()
				}),
				vm.ToValue(func(call goja.FunctionCall) goja.Value {
					if rejected {
						return goja.Undefined()
					}
					rejected = true
					msg := ""
					if len(call.Arguments) > 0 {
						msg = call.Arguments[0].String()
					}
					_ = reject(vm.NewGoError(fmt.Errorf("parallel[%d]: %s", index, msg)))
					return goja.Undefined()
				}),
			)
		}

		// Kick off up to `cap` items.
		initial := length
		if initial > cap {
			initial = cap
		}
		next = initial
		if initial == 0 {
			_ = resolve(vm.ToValue([]any{}))
			return vm.ToValue(final)
		}
		for i := 0; i < initial; i++ {
			i := i
			loop.RunOnLoop(func(*goja.Runtime) { dispatch(i) })
		}
		return vm.ToValue(final)
	})
}

// installHandoffBinding wires `handoff(name, input)` — the recursive
// composition primitive. handoff() routes into the registry's CallTool
// path so a skill calling another skill goes through the same code an
// upstream MCP client would.
func installHandoffBinding(vm *goja.Runtime, loop *eventloop.EventLoop, ctx context.Context, timeout time.Duration, b Bindings) {
	if b.SkillCaller == nil {
		_ = vm.Set("handoff", func(call goja.FunctionCall) goja.Value {
			panic(vm.NewGoError(fmt.Errorf("handoff() is not available: no SkillCaller wired")))
		})
		return
	}
	_ = vm.Set("handoff", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			panic(vm.NewGoError(fmt.Errorf("handoff(name, [input]) requires at least 1 argument")))
		}
		name := call.Arguments[0].String()
		var input map[string]any
		if len(call.Arguments) >= 2 && !goja.IsUndefined(call.Arguments[1]) && !goja.IsNull(call.Arguments[1]) {
			input = normalizeArgs(call.Arguments[1].Export())
		}

		promise, resolve, reject := vm.NewPromise()
		deliver := asyncDeliver(vm, loop, timeout, reject, fmt.Sprintf("handoff %q", name))
		go func() {
			res, err := b.SkillCaller.CallTool(ctx, name, input)
			deliver(func(vm *goja.Runtime) {
				if err != nil {
					_ = reject(vm.NewGoError(fmt.Errorf("handoff %q: %w", name, err)))
					return
				}
				if res != nil && res.IsError {
					_ = reject(vm.NewGoError(fmt.Errorf("handoff %q error: %s", name, contentText(res))))
					return
				}
				_ = resolve(vm.ToValue(decodeContent(res)))
			})
		}()
		return vm.ToValue(promise)
	})
}

// installApprovalBinding wires `approval(prompt)`. With no Approver
// configured the binding auto-approves with the prompt as the reason
// — Phase E replaces the stub with a real CLI/web/MCP gate. Errors
// returned by Approver are turned into Promise rejections so authors
// can `try`/`catch`.
func installApprovalBinding(vm *goja.Runtime, loop *eventloop.EventLoop, ctx context.Context, timeout time.Duration, b Bindings) {
	approver := b.Approver
	_ = vm.Set("approval", func(call goja.FunctionCall) goja.Value {
		prompt := ""
		if len(call.Arguments) >= 1 {
			prompt = call.Arguments[0].String()
		}
		promise, resolve, reject := vm.NewPromise()
		deliver := asyncDeliver(vm, loop, timeout, reject, "approval")
		if approver == nil {
			deliver(func(vm *goja.Runtime) {
				_ = resolve(vm.ToValue(map[string]any{
					"approved":  true,
					"reason":    prompt,
					"automatic": true,
				}))
			})
			return vm.ToValue(promise)
		}
		go func() {
			decision, err := approver.Approve(ctx, prompt)
			deliver(func(vm *goja.Runtime) {
				if err != nil {
					_ = reject(vm.NewGoError(fmt.Errorf("approval: %w", err)))
					return
				}
				_ = resolve(vm.ToValue(map[string]any{
					"approved":  decision.Approved,
					"reason":    decision.Reason,
					"automatic": false,
				}))
			})
		}()
		return vm.ToValue(promise)
	})
}

// contentText collapses a tool result's content blocks into a single
// string for error messages.
func contentText(res *mcp.ToolCallResult) string {
	if res == nil {
		return ""
	}
	parts := make([]string, 0, len(res.Content))
	for _, c := range res.Content {
		if c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, " ")
}

// decodeContent renders a tool result's content block back into a JS
// value. Single text blocks containing JSON decode to objects so
// authors can read fields directly; non-JSON text returns as a string;
// multi-block content returns the array of blocks verbatim. nil
// results return undefined-equivalent (an empty map).
//
// Mutex is intentionally not needed: the caller already serialises
// access by running the helper inside loop.RunOnLoop.
func decodeContent(res *mcp.ToolCallResult) any {
	if res == nil || len(res.Content) == 0 {
		return map[string]any{}
	}
	if len(res.Content) == 1 && res.Content[0].Text != "" {
		var parsed any
		if err := json.Unmarshal([]byte(res.Content[0].Text), &parsed); err == nil {
			return parsed
		}
		return res.Content[0].Text
	}
	out := make([]map[string]any, len(res.Content))
	for i, c := range res.Content {
		out[i] = map[string]any{"type": c.Type, "text": c.Text}
	}
	return out
}

