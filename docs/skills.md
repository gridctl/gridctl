# Skills

The three flavors of gridctl skills — when to reach for each, how they fit together, and what the gateway does with them.

A *skill* is a unit of behaviour the gateway exposes to upstream MCP clients as either a prompt or a tool. File presence is the discriminator: the registry walker classifies a skill by looking at what sits next to `SKILL.md` on disk, not by reading a `kind:` field in the frontmatter (there isn't one — and we don't plan to add one; the frontmatter stays agentskills.io-compliant).

| Flavor | Files | Surfaces as | Runtime |
|---|---|---|---|
| Prompt-only | `SKILL.md` | MCP prompt + tool (body unchanged) | None — the body is the artifact |
| Code (TS) | `SKILL.md` + `skill.ts` + `agent.json` | MCP tool (typed input/output) | goja + esbuild sandbox |
| Code (Go) | `SKILL.md` + `skill.go` + `skill_test.go` | MCP tool (typed input/output) | Go plugin (`buildmode=plugin`) |

All three sit in the same registry directory (`~/.gridctl/registry/skills/<name>/`) and are loadable by `store.Load()`. The same upstream MCP client cannot tell them apart at call time — that is the invariant the rest of this doc protects.

---

## Prompt-only skills

Reach for prompt-only when the skill is prose. A severity matrix the model is supposed to apply verbatim. A runbook step the operator wants surfaced into an upstream client (Claude Desktop, an IDE, a CLI) without an intermediate handler.

Scaffold:

```sh
gridctl agent init --prompt-only --name my-skill
```

Output: one file — `SKILL.md` — with frontmatter (`name`, `description`, `state: active`) and a markdown body. No `skill.ts`, no `skill.go`, no `agent.json`. The body is the artifact. Edit it, and the next time the registry walker runs the change surfaces to upstream clients verbatim — no rebuild, no restart of anything other than the registry refresh.

Post-scaffold hint:

```
run: gridctl skill list --remote && gridctl run my-skill
```

`gridctl run` is not the primary path for prompt-only skills — the body surfaces as an MCP prompt to upstream clients, and an upstream client is what reads it. `gridctl run` works because every skill registers a tool envelope; it just isn't where the prompt-only flavor pays off.

Substitution like `{{variable}}` inside the body is convention-only. gridctl does not template-expand placeholders. The upstream MCP client is responsible for filling them in if it chooses to.

---

## Code skills (TypeScript)

Reach for TS when the skill is a graph — at least one `tool()` call, at least one `llm()` call, possibly a `handoff()` to another skill — and you want to iterate fast. The TS path runs in-process inside a goja runtime with an esbuild transpile pass on first call; there is no separate build step beyond writing source.

Scaffold:

```sh
gridctl agent init --name my-skill           # default is --lang ts
gridctl agent init --lang ts --name my-skill # explicit
```

Output:

- `SKILL.md` — frontmatter + body (becomes `skill.body` inside the handler).
- `skill.ts` — default-export async function `(input) => output`. The bindings the sandbox installs are: `tool()`, `llm()`, `parallel()`, `handoff()`, `approval()`, and the read-only `skill` object (`skill.body`, `skill.name`).
- `agent.json` — project config the dev server reads.

Post-scaffold hint:

```
run: gridctl agent dev --root "<dir>"
```

`gridctl agent dev` exposes the dev-server API (`/api/agent/dev/skills`, `/events`) for JSON/CLI use; the browser canvas is served by the daemon at `http://localhost:8180/agent` when you pass `--agent-dev-root <dir>` to `gridctl serve` (or place skills under `~/.gridctl/registry/skills`, which the daemon picks up by default). The canvas gives you a watcher that re-renders the graph on save in <300ms, click-to-`$EDITOR` jumps via `vscode://file/...:line`, and a trace overlay that shows the same status pills and latency the gateway records at runtime. The IDE is read-only — code is canon, the canvas never writes back.

The TS sandbox's tool calls flow through the same `agent.ToolCaller` the gateway uses, so tracing, pricing, replica routing, vault auth, and tool whitelisting apply unchanged. `handoff()` resolves through the same `SkillCaller` interface the registry server uses, which means a TS skill calling a Go skill or a remote skill traverses the same dispatcher path an upstream client would — local execution and remote execution are one code path.

---

## Code skills (Go)

Reach for Go when typed input/output structs and Go-side libraries (a tracing SDK, a vault client, a Kubernetes client-go that the goja runtime cannot host) are load-bearing. The Go path produces a compiled `.so` artifact via `go build -buildmode=plugin`; the gateway opens it at start and the plugin self-registers against a shared `*skill.Registry`.

Scaffold:

```sh
gridctl agent init --lang go --name my-skill
```

Output:

- `SKILL.md` — frontmatter + body (becomes `ctx.SkillBody()` inside the handler).
- `skill.go` — `package main` declaring `HelloInput` / `HelloOutput` structs with `json` + `jsonschema` tags, a `New() *skill.Definition` constructor returning `skill.MustDefine[HelloInput, HelloOutput]`, the plugin entry point `RegisterSkill(*skill.Registry) error`, and an empty `func main() {}` stub so plain `go build` compile-checks the source.
- `skill_test.go` — table test exercising `Define[HelloInput, HelloOutput]` with a `fakeRunContext` stub satisfying the `skill.RunContext` interface.

Post-scaffold hint:

```
run: gridctl agent dev --root "<dir>"
```

Then to compile:

```sh
gridctl agent build my-skill
```

This writes `dist/skill.so` and `dist/manifest.json`. The gateway loads the `.so` at the next start (Go plugins cannot be unloaded — see operational sharp edges below).

The typed runner signature is:

```go
func(ctx skill.RunContext, input I) (O, error)
```

`skill.RunContext` embeds `context.Context` (cancellation and request-scoped values flow through unchanged) and adds `SkillBody() string` plus `SkillName() string`. Body and name are captured at `Define` time and read with no per-call I/O — a single pointer chase, no synchronisation cost.

The plugin entry point is fixed:

```go
func RegisterSkill(reg *skill.Registry) error {
    return reg.Register(New())
}
```

`plugin.Lookup("RegisterSkill")` is what the gateway calls after `plugin.Open` — if your `skill.go` doesn't export a function with this exact name and signature, the load fails and the gateway logs a warning. `gridctl agent validate <skill>` parses `skill.go` via `go/parser` and catches the missing-symbol case before you waste a `go build` round-trip.

---

## The hybrid pattern

The hybrid pattern is the third move: a *code* skill whose handler reads its own `SKILL.md` body and feeds it to the LLM as the system prompt. Code drives the graph; prose drives the behaviour. Edit the body, change runtime behaviour, no code change.

In Go:

```go
func run(ctx skill.RunContext, in TriageInput) (TriageOutput, error) {
    req := agent.ChatRequest{
        Model:    "claude-sonnet-4-6",
        System:   ctx.SkillBody(),   // the post-frontmatter markdown
        Messages: []agent.Message{ /* ... */ },
    }
    resp, err := provider.Generate(ctx, req)
    // ...
}
```

In TS:

```ts
import { llm, skill } from "@gridctl/agent";

const reply = await llm({
  model: "claude-sonnet-4-6",
  system: skill.body,                 // the post-frontmatter markdown
  messages: [{ role: "user", content: `Skill: ${skill.name}\n...` }],
});
```

Both surfaces resolve to the same string for the same skill — that is the parity invariant. A TS skill and a Go skill registered against the same registry with the same `SKILL.md` body see byte-identical values for `skill.body` / `ctx.SkillBody()`.

The reference example is [`examples/registry/items/incident-triage-hybrid/`](../examples/registry/items/incident-triage-hybrid/): a Go skill whose `SKILL.md` carries a real SRE severity matrix, decision rules, and a runbook, and whose handler feeds that prose into `agent.ChatRequest.System` via `ctx.SkillBody()`. The hybrid contract is byte-checked in `skill_test.go` — the test asserts `buildRequest(...)`'s `System` field equals the on-disk body verbatim, so a refactor that breaks the wiring fails the test, not production.

The TS counterpart lives in [`examples/registry/items/triage-ts/`](../examples/registry/items/triage-ts/), and the Go non-hybrid variant in [`examples/registry/items/triage-go/`](../examples/registry/items/triage-go/) — same incident-triage shape, three ways.

---

## Recursive composability

A skill that hands off to another skill receives the *target's* body, not the caller's. Body is per-skill, not per-run.

The wiring:

- TS: a TS skill calls `handoff("greet", { ... })`. The sandbox's `SkillCaller` dispatches against `*skill.Registry`. The runner that backs `greet` reads `skill.body` and gets `greet`'s body — not the caller's.
- Go: the same handoff path crosses the registry's `CallTool` surface. A Go skill invoked via handoff reads `ctx.SkillBody()` and gets its own body.
- Remote: pointing one gridctl at another over MCP replaces the in-process registry with the gateway. The handoff path through the sandbox's `SkillCaller` is identical; the body still resolves per-skill on the remote end.

Two tests carry the invariant between them. `pkg/agent/sandbox/recursive_test.go` proves the *dispatch* side: a TS skill that handoffs to a typed Go skill registered in the same registry traverses the cross-package `SkillCaller` path and receives the Go skill's output unchanged — same code path an upstream MCP client would take. The *body-per-skill* side is byte-checked in `examples/registry/items/incident-triage-hybrid/skill_test.go`: `TestBuildRequest_*` asserts that `buildRequest(ctx, ...).System` equals the fixture body verbatim, so the hybrid contract — body resolves per-skill, not per-run, and reaches `agent.ChatRequest.System` unchanged — fails the test before it fails production.

The gateway-builder plugin loader re-decorates each loaded plugin's `Definition` with the on-disk `SKILL.md` body via the `skill.WithSkillBody(ctx, body)` helper. The override is a context value, so concurrent invocations of the same `Definition` each see the body wired by their own call's context — the per-skill, per-call view stays consistent without a generics-crossing re-registration the loader cannot perform.

---

## Operational sharp edges (Go plugins)

The Go plugin path has real operational constraints. They live here so the operator debugging a `plugin.Open` failure reads them first.

### Host/plugin Go version must match

The gridctl daemon and every `skill.so` it loads must build with identical Go toolchain versions and identical dependency-graph hashes. If you upgrade your Go toolchain and rebuild the daemon without rebuilding the skill plugin, `plugin.Open` returns *"plugin was built with a different version of package X"* — there is no runtime fix; the operator has to rebuild plugins after a daemon rebuild.

gridctl makes this less painful with **manifest guardrails**. At `gridctl agent build` time, the build writes two fields into `dist/manifest.json`:

- `go_version` — the building toolchain's `runtime.Version()` (e.g. `"go1.26.3"`).
- `go_mod_hash` — sha256 of the `go.mod` resolved by walking up from the handler source.

At gateway start, the plugin loader reads the manifest *before* calling `plugin.Open` and compares both fields against the running daemon. On mismatch the loader skips the plugin and emits an actionable warning:

```
plugin built with go1.26.3, daemon running go1.27.0 — rebuild with `gridctl agent build <name>`
```

The opaque toolchain-mismatch error never reaches the operator. A missing manifest is also a skip-with-warn — same shape, different message. A missing `go_mod_hash` on either side reads as "skip the check, not the plugin": standalone scaffolds without a parent `go.mod` would otherwise become unloadable.

### Plugins cannot be unloaded

`plugin.Open` is one-way. Once a `.so` is loaded into the daemon process it stays resident. Hot-reload that reads a new skill set keeps the previously-loaded plugins around. Don't try to refresh Go plugins during a hot reload — only refresh on daemon start. The TS path does not have this constraint; goja runtimes are per-call.

### Linux and macOS only

`go build -buildmode=plugin` is not supported on Windows. `gridctl agent build` for a Go skill on Windows returns a pre-flight error before invoking the toolchain:

```
go skill build requires Linux or macOS — Go plugins are not available on Windows
```

…instead of letting an opaque `-buildmode=plugin not supported on windows/amd64` toolchain message leak through. The gateway-builder plugin loader is also a no-op on Windows (the loader is replaced with a stub at build time) so a Windows daemon walks past Go-handler skills without trying to load them.

### `RegisterSkill` is the plugin contract

The gateway-builder loader calls `plugin.Lookup("RegisterSkill")` and expects a function with the exact signature:

```go
func RegisterSkill(*skill.Registry) error
```

Plugins that don't export the exact symbol name and signature fail `plugin.Lookup` and the loader logs a warning. `gridctl agent validate <skill>` is the static check that catches this at author time — it parses `skill.go` via `go/parser` and reports a clear error before the operator wastes a `go build` round-trip.

### One broken plugin does not block gateway start

Per-skill failures — missing `.so`, `plugin.Open` failure, missing `RegisterSkill` symbol, wrong signature, non-nil error from `RegisterSkill` — are logged at warn and the loop continues. The skill surfaces as a missing tool at call time, which is the correct user-visible signal. The gateway always starts.

---

## What we do NOT support, and why

A short list of things people sometimes expect and we deliberately don't ship. Most of these are open questions we'd want a concrete trigger for before committing.

**`kind:` (or `runtime:`, or `entry:`) in the SKILL.md frontmatter.** File presence is the discriminator. Adding a `kind:` field would force the agentskills.io ecosystem to special-case gridctl skills, and the value would always be derivable from the directory contents anyway. The frontmatter stays compliant; the existing `state:` and `acceptance_criteria:` extensions are the only gridctl-specific fields.

**Sub-process / `hashicorp/go-plugin`-style fallback for Go skills.** `go build -buildmode=plugin` has real operational sharp edges (above), but it is the cheapest path to typed Go skills without inventing an IPC layer. If `plugin.Open` proves operationally unworkable in real use, the sub-process fallback is the documented escape hatch — but until that trigger is concrete, two parallel Go runtimes is YAGNI. The `manifest.json` `handler` field is naming-flexible (`"go"` today, leaving room for `"go-binary"` later) so the schema doesn't paint itself into a corner.

**Per-skill `allowed_providers` in the manifest.** Provider gating is a runtime / vault concern, not a skill-manifest concern. The vault decides what keys exist; the gateway decides what gets routed where. Putting it in the skill manifest would split that policy across two places.

**Skill marketplace publishing logic per flavor.** The existing `pkg/skills` git-import infrastructure already covers all three flavors. There is no flavor-specific publish path because all three are just directories under `skills/<name>/`.

**`gridctl run` for prompt-only skills.** Prompt-only skills are MCP prompts, not invocable tools that return structured output. They surface to upstream MCP clients (Claude Desktop, an IDE) and are consumed there. `gridctl run` would have to invent a presentation layer the upstream client is already responsible for; we leave it where it belongs.

**Go plugin hot-reload.** Plugins can't be unloaded, so "hot reload" for the Go path means "load new plugins on top of the old ones forever." That is a memory leak, not a feature. The TS path supports hot reload because goja runtimes are per-call. For Go skills, restart the daemon.

**Two parallel `Define` shapes (one with body, one without).** `Define[I, O](name, description, body, run)` takes the body as a positional argument. The earlier `DefineWithBody` variant was considered and rejected — two parallel APIs drift over time, and the breaking-change cut at three first-party call sites was tractable. Pre-1.0 is the right window for a clean cut; we took it.

---

## References

- `pkg/agent/skill/skill.go` — the package doc; the two-layer mental model (Definition/Registry runtime-facing, `Define[I, O]` author-facing).
- `pkg/agent/skill/typed.go` — `RunContext`, `Define[I, O]`, `WithSkillBody`.
- `pkg/agent/sandbox/sandbox.go` — `Bindings`; the `SkillBody` / `SkillName` fields the JS `skill` global reads from.
- `pkg/controller/go_plugins.go` — `loadGoSkillPlugins`, the manifest-guardrail-then-`plugin.Open` loader. Called from `pkg/controller/gateway_builder.go:Build()` after the registry store is populated.
- `pkg/controller/gateway_builder.go` — `makeDispatcherBindings` (per-call body resolution for the TS sandbox path).
- `pkg/agent/sandbox/recursive_test.go` — the cross-package handoff smoke test (TS → typed Go via `SkillCaller`). The body-per-skill invariant itself is byte-checked in `examples/registry/items/incident-triage-hybrid/skill_test.go` (`TestBuildRequest_*` asserts `req.System` equals the on-disk body verbatim).
- `examples/registry/items/triage-ts/`, `triage-go/`, `incident-triage-hybrid/` — the three reference examples.
