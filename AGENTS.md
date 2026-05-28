# AGENTS.md

Guidance for AI coding agents working in this repository. Follows the [agents.md](https://agents.md) open specification — a single, predictable entry point for agent-relevant context. Human-facing onboarding lives in `README.md` and `CONTRIBUTING.md`; agent-specific tooling files (`CLAUDE.md`, etc.) re-export this file rather than duplicating it.

## What gridctl is

Gridctl is an MCP (Model Context Protocol) gateway with a built-in skills registry. A user declares a stack of MCP servers (containerized stdio, SSE/HTTP, OpenAPI-backed, local processes, SaaS proxies) in `stack.yaml`, runs `gridctl apply`, and gridctl orchestrates the containers, fans tool calls to the right server, and surfaces every active `SKILL.md` to upstream clients as an MCP prompt. The same process embeds a React web UI on `:8180`. Inspired by Containerlab.

## Build and run

The Makefile is the entry point for everything. Common targets:

| Target | Notes |
|---|---|
| `make build` | Builds the web frontend (`web/dist` → `cmd/gridctl/web/dist`), then builds the Go binary with `-tags embed_web` so the UI is embedded. Produces `./gridctl` in the repo root. |
| `make build-go` | Backend only. Skips the embed tag if `cmd/gridctl/web/dist` is absent (UI 404s in that case). |
| `make build-web` | Frontend only (`cd web && npm run build`). |
| `make dev` | Runs the Vite dev server (`web/`) against a separately-running backend. |
| `make test` | `go test -v ./...` — unit tests only. |
| `make test-integration` | `go test -v -tags=integration ./tests/integration/...`. Requires Docker (or Podman) — these tests hit real container runtimes per Article IV of `CONSTITUTION.md`; mocks are disallowed in `tests/integration/`. |
| `make test-frontend` | `cd web && npm test` (Vitest). |
| `make generate` | Regenerates `go.uber.org/mock` mocks under `pkg/mcp/` and `pkg/runtime/`. Required after touching the interfaces they're generated from. |
| `make update-pricing` | Refreshes the embedded LiteLLM pricing snapshot at `pkg/pricing/data/model_prices.json` (weekly cadence). |
| `make mock-servers [PORT=9001]` | Builds and runs the example mock MCP servers in `examples/_mock-servers/` (HTTP on PORT, SSE on PORT+1). Pair with `make clean-mock-servers`. |

Always test local changes with `make build` followed by `./gridctl …`. The `gridctl` binary on `$PATH` is typically a brew-installed release and will not reflect your changes.

`gridctl serve` and `gridctl apply` daemonize by default. If you need a process you can ctrl-C (or that a test script can kill), pass `-f` / `--foreground`.

Run a single Go test:

```bash
go test -v -run TestFunctionName ./pkg/runtime/...
go test -v -race -tags=integration -run TestGatewayLifecycle ./tests/integration/...
```

Lint:

```bash
golangci-lint run                # backend (gosec is enabled — see .golangci.yml for the curated exclusions)
cd web && npm run lint           # frontend; pre-existing errors live in src/pages/Detached*.tsx — scope review to files you touched
```

## Code architecture

The shape of the codebase from the outside in:

```
cmd/gridctl/        Cobra CLI entry points — one file per subcommand (apply, serve, link, var, skill, optimize, …).
                    embed.go pulls in cmd/gridctl/web/dist via go:embed under the embed_web build tag.
internal/server/    HTTP server bootstrap: mounts the API, the embedded UI, and the MCP transports.
internal/api/       REST handlers backing the web UI (one file per resource: stack, skills, vault, pins, telemetry, traces, …).
                    The Server struct in api.go wires together every pkg/* subsystem the UI needs to talk to.
internal/probe/     Ephemeral MCP tool-list probe for the "add server" wizard (not registered with the gateway).
pkg/config/         stack.yaml schema, loader, variable/env expansion, plan diffing, health-check parsing.
pkg/runtime/        Container orchestration. Orchestrator is the WorkloadRuntime + Builder front; pkg/runtime/docker is the
                    Docker implementation. Runtime auto-detected (docker → podman) unless --runtime is set.
pkg/builder/        Image building from git or local Dockerfiles, with a content-addressed cache.
pkg/mcp/            MCP protocol: gateway (router + tool aggregation), stdio/SSE/streamable transports, OpenAPI-as-MCP,
                    autoscaler, code mode sandbox (goja), replica sets, schema pinning hooks.
pkg/registry/       Skills registry — discovers SKILL.md files, parses frontmatter, validates, serves as MCP prompts.
pkg/skills/         Remote skill management (git import, lockfile, fingerprinting, updater).
pkg/provisioner/    LLM-client config writers (claude, claudecode, cursor, windsurf, gemini, opencode, grok, goose,
                    cline, anythingllm, roo, zed, continue, vscode). JSON and TOML helpers in json.go / toml.go.
                    Backed by `gridctl link` / `gridctl unlink`.
pkg/vault/          Encrypted variable store (XChaCha20-Poly1305 + Argon2id). The `gridctl var` and (deprecated) `gridctl vault` CLIs.
pkg/pins/           TOFU schema pinning for tool definitions; drift surfaces in pkg/pins + `gridctl pins`.
pkg/optimize/       Cost analysis — feeds `gridctl optimize` and the UI's findings panel using the embedded LiteLLM prices.
pkg/telemetry/      Tool-call accounting (counts, latency, cost). Buffered in-memory; surfaced via /api/telemetry.
pkg/tracing/        OTLP exporter + in-memory trace buffer for `gridctl traces` and the UI traces panel.
pkg/reload/         Stack hot-reload (file watcher + diff-and-apply path).
pkg/metrics/, pkg/token/, pkg/format/, pkg/pricing/, pkg/output/, pkg/logging/, pkg/jsonrpc/, pkg/state/, pkg/git/, pkg/controller/, pkg/dockerclient/   Supporting libs.

web/                React 19 + Vite + TypeScript. Tailwind v4 (postcss plugin). Zustand stores in src/stores/, route map in
                    src/routes.tsx, feature components grouped under src/components/<workspace>/. The Detached*Page files
                    are popout windows that mirror specific panels.

tests/integration/  Real-runtime suites (build tag `integration`). Cover gateway lifecycle, hot reload, autoscaler,
                    replicas, transports (incl. Podman), code-mode cost, private git auth, optimize heuristics.
examples/           Example stack YAMLs grouped by surface (getting-started, transports, openapi, registry, secrets-vault,
                    code-mode, platforms, tracing). examples/_mock-servers/ is the source for `make mock-servers`.
docs/               User-facing documentation (cli-reference, config-schema, skills, scaling, cost-observability,
                    api-reference, installation, project-status, troubleshooting).
```

End-to-end request flow for an upstream MCP tool call: client → `internal/server` HTTP listener → `pkg/mcp` transport (SSE/streamable/stdio) → `mcp.Gateway` router → per-server `mcp.Client` (process/SSE/HTTP/OpenAPI) → response, with telemetry, tracing, schema pinning, and (optional) output-format conversion attached on the way back.

End-to-end for the web UI: React store action → `/api/...` handler in `internal/api/` → method on `Server` → call into the relevant `pkg/*` subsystem → JSON response → store update → component re-render.

## Constitution

`CONSTITUTION.md` is binding for every change. Articles that most often catch a refactor by surprise:

- **III — Test-first:** every exported function gets a test before merge; bug fixes need a regression test.
- **IV — Integration tests use real dependencies:** anything under `tests/integration/` runs against real Docker/Podman and must pass `-race`. Mocks are unit-test only.
- **V — No panics in `pkg/` or `internal/`:** return errors. CLI init in `cmd/` is the only place panic is allowed.
- **VI — Context propagation:** any I/O, blocking, or external call takes `context.Context` as the first arg and respects cancellation.
- **IX — Stack YAML back-compat:** new `stack.yaml` fields are optional with a default that preserves existing behavior. Renames and removals are breaking changes.
- **X — Machine-readable CLI output:** structured commands need a `--format json` (or equivalent) and meaningful exit codes (`0`/`1`/`2`).
- **XIV — Structured logging:** use `log/slog` in library code; no `fmt.Println` / `log.Printf`.
- **XV — Changelog discipline:** every user-visible change lands an entry under `[Unreleased]` in `CHANGELOG.md` in the same PR.

`CONTRIBUTING.md` covers branch prefixes, commit format, and the PR/CI process — follow it rather than re-deriving conventions.
