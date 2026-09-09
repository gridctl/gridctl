# AGENTS.md

Guidance for AI coding agents working in this repository. Follows the [agents.md](https://agents.md) open specification: a single, predictable entry point for agent-relevant context. Human-facing onboarding lives in `README.md` and `CONTRIBUTING.md`; agent-specific tooling files (`CLAUDE.md`, etc.) re-export this file rather than duplicating it.

## What gridctl is

Gridctl is an MCP (Model Context Protocol) gateway with a built-in skills and agents registry. A user declares a stack of MCP servers (containerized stdio, SSE/HTTP, OpenAPI-backed, local processes, SaaS proxies) in `stack.yaml`, runs `gridctl apply`, and gridctl orchestrates the containers, fans tool calls to the right server, and surfaces every active `SKILL.md` to upstream clients as an MCP prompt. A projection engine (`pkg/project`, one lockfile at `~/.gridctl/project.lock.yaml`) also places skills, agents, global-context rules, and gateway wiring onto disk for file-reading clients, and packs (`gridctl-pack.yaml`) import all of it from one git repo. The same process embeds a React web UI on `:8180`. Inspired by Containerlab.

## Build and run

Task (https://taskfile.dev) is the entry point for development builds and Go/frontend checks; tasks live in `Taskfile.yml`. Run `task --list` for the full catalog and `task --summary <name>` for per-task notes. Install with `brew install go-task/tap/go-task` (or `npm install -g @go-task/cli`, or `go install github.com/go-task/task/v3/cmd/task@latest`). A transitional Makefile shim forwards the old `make <target>` names. Common tasks:

| Task | Notes |
|---|---|
| `task build` | Builds the web frontend (`web/dist` → `cmd/gridctl/web/dist`), then builds the Go binary with `-tags embed_web` so the UI is embedded. Produces `./gridctl` in the repo root. |
| `task build:go` | Backend only. Skips the embed tag if `cmd/gridctl/web/dist` is absent (UI 404s in that case). |
| `task build:web` | Builds the frontend and stages `web/dist` in `cmd/gridctl/web/dist` for embedding; does not compile Go. |
| `task dev` | Runs the Vite dev server (`web/`) against a separately-running backend. |
| `task test` | `go test -race ./...` (unit tests only, same race detector CI runs). |
| `task test:integration` | `go test -tags=integration -race -timeout 15m ./tests/integration/...`. The full suite requires Docker (or Podman); selected HTTP/subprocess suites need no container runtime. All use real dependencies per Article IV of `CONSTITUTION.md`; mocks are disallowed in `tests/integration/`. |
| `task test:frontend` | `cd web && npm test` (Vitest). |
| `task lint` | `golangci-lint run` plus `npm run lint` in `web/` (both CI-gated). |
| `task generate` | Regenerates `go.uber.org/mock` mocks under `pkg/mcp/` and `pkg/runtime/`. Required after touching the interfaces they're generated from. |
| `task mock:servers` | Builds and runs the example mock MCP servers in `examples/_mock-servers/` (HTTP on PORT, SSE on PORT+1; `PORT=9001` default). Pair with `task mock:clean`. |

Always test local changes with `task build` followed by `./gridctl …`. The `gridctl` binary on `$PATH` is typically a brew-installed release and will not reflect your changes.

`gridctl serve` and `gridctl apply` daemonize by default. If you need a process you can ctrl-C (or that a test script can kill), pass `-f` / `--foreground`.

Run a single Go test:

```bash
go test -v -run TestFunctionName ./pkg/runtime/...
go test -v -race -tags=integration -run TestToolGroups_Authentication ./tests/integration/...
```

Lint:

```bash
golangci-lint run                # backend (gosec is enabled; see .golangci.yml for the curated exclusions)
cd web && npm run lint           # frontend; zero-error baseline, enforced by the gatekeeper frontend CI job
```

Release tooling has separate Python policy/scanner tests: `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts -p 'test_*.py' -v` (Python 3.11+, jsonschema 4.23.0, PyYAML 6.0.3, Bash, and jq). These are not included in `task test`. `.github/workflows/release.yaml` reuses all six exact-commit Gatekeeper jobs, assembles and authenticates a draft with GoReleaser, verifies it on Linux/macOS, publishes, and only then advances Homebrew. See `docs/release-verification.md` for external verification, immutable-mode prerequisites, and recovery; local fixtures do not replace hosted acceptance.

## Code architecture

The shape of the codebase from the outside in:

```
cmd/gridctl/        Cobra CLI entry points, one file per subcommand (apply, serve, link, var, skill, ctx, pack, project, optimize, …).
                    embed.go pulls in cmd/gridctl/web/dist via go:embed under the embed_web build tag.
internal/api/       REST handlers backing the web UI (one file per resource: stack, skills, vault, pins, telemetry, traces, …).
                    Python source validation, resolution previews, generated files, and status provenance live in
                    python_sources.go. The Server struct in api.go wires together every pkg/* subsystem the UI needs.
                    Server.Handler assembles HTTP routes and CORS/Host/auth middleware; auth.go protects operational
                    namespaces, including grouped MCP/SSE. UI files, probes, terminal preflight, and the exact
                    state-validated downstream OAuth callback do not require the gateway token.
internal/probe/     Ephemeral MCP tool-list probe for the "add server" wizard (not registered with the gateway).
pkg/catalog/        MCP server catalog behind `gridctl search` / `gridctl add`: curated embedded entries plus the
                    official MCP Registry, with install-shape mapping into stack.yaml server blocks.
pkg/config/         stack.yaml schema, defaults and validation, variable/env expansion, plan diffing, health-check parsing.
                    export.go owns the shared non-resolving ExportStack projection for CLI/API exports, with bounded
                    sensitive-literal rejection and source ancestry for CLI destination checks. Runtime loaders stay separate.
pkg/runtime/        Container orchestration. Orchestrator is the WorkloadRuntime + Builder front; it prepares one desired
                    source image per logical MCP server before reconciling replicas. pkg/runtime/docker is the Docker
                    implementation. Runtime auto-detected (docker → podman) unless --runtime is set.
pkg/builder/        Image building from git or local Dockerfiles and generated Python builds for exact public PyPI releases
                    or packaged git/local projects, with resolved build plans, isolated Git worktrees, content-addressed
                    image tags, label-verified cache reuse, and non-secret provenance labels. Also owns bounded public-PyPI
                    resolution, static Python package/project metadata inspection, supported-interpreter selection,
                    console-script resolution, and deterministic digest-pinned uv Dockerfile generation.
pkg/mcp/            MCP protocol: gateway (router + tool aggregation), stdio/SSE/streamable transports, OpenAPI-as-MCP,
                    autoscaler, code mode sandbox (goja), replica sets, schema pinning hooks.
pkg/mcpauth/        Downstream OAuth 2.1 brokering for external servers (discovery, dynamic client registration,
                    token store, callback listener). Backed by `gridctl auth`.
pkg/registry/       Skills registry: discovers SKILL.md files, parses frontmatter, validates, serves as MCP prompts.
pkg/skills/         Remote skill and agent management (git import, lockfile, fingerprinting, updater, security scan).
pkg/project/        Unified projection engine: one lockfile (~/.gridctl/project.lock.yaml), flock, hashing, backups,
                    drift states, and migrate-on-read. The kind packages below are its tenants.
pkg/skillsync/      Projects active registry skills into native client skill directories (`gridctl skill project`).
pkg/agentsync/      Projects imported agents to clients: identity copy for Claude Code, rendered dialects for
                    OpenCode, Copilot, and Gemini CLI (`gridctl skill project --kind agent`).
pkg/contexts/       Global agent-context projection (`gridctl ctx`): one canonical file, or opt-in rule fragments
                    with multi-file and compiled per-client assembly.
pkg/modelsync/      Model routing policy projection (`gridctl models`): a router-only LiteLLM fragment plus its include:
                    line in the parent config, and an OpenCode provider stanza, with drift and restart-pending state.
pkg/wiring/         Key-level ownership of gateway entries merged into client MCP configs (`gridctl project`, link/unlink).
pkg/pack/           gridctl-pack.yaml manifest schema; pkg/packops owns orchestration shared by the CLI and REST handlers.
pkg/skillpins/      TOFU pins over skill documents (per-file digests, findings); the `gridctl skill pins` store.
pkg/limits/         Enforces the `limits:` block: token-bucket rate limits on the tool-call dispatch path.
pkg/provisioner/    LLM-client config writers (claude, claudecode, cursor, windsurf, gemini, antigravity, opencode, grok, goose,
                    cline, anythingllm, lmstudio, roo, zed, continue, vscode). JSON and TOML helpers in json.go / toml.go.
                    Backed by `gridctl link` / `gridctl unlink`.
pkg/vault/          Encrypted variable store (XChaCha20-Poly1305 + Argon2id). The `gridctl var` and (deprecated) `gridctl vault` CLIs.
pkg/varrun/         Explicit stored-variable delivery to child processes, including output redaction and signal forwarding.
pkg/varscan/        Exact stored-secret scanning for working-tree files and staged Git blobs.
pkg/pins/           TOFU schema pinning for tool definitions; drift surfaces in pkg/pins + `gridctl pins`.
pkg/optimize/       Usage analysis: feeds `gridctl optimize` and the UI's findings panel with token-denominated findings.
pkg/telemetry/      Tool-call accounting (counts, latency, tokens). Buffered in-memory; surfaced via /api/telemetry.
pkg/tracing/        OTLP exporter + in-memory trace buffer for `gridctl traces` and the UI traces panel.
pkg/reload/         Stack hot-reload (file watcher + diff-and-apply path).
pkg/controller/     Application composition root: builds the gateway, mounts the API server, embedded UI, and MCP transports
                    (gateway_builder.go), and owns deploy/daemonize orchestration for `gridctl apply` and `gridctl serve`.
pkg/metrics/, pkg/token/, pkg/format/, pkg/output/, pkg/logging/, pkg/jsonrpc/, pkg/state/, pkg/git/, pkg/dockerclient/   Supporting libs.

web/                React 19 + Vite + TypeScript. Tailwind v4 (postcss plugin). Zustand stores in src/stores/, route map in
                    src/routes.tsx, feature components grouped under src/components/<workspace>/. Nine workspaces:
                    Stack, Library, Vault (Variables), Tools, Metrics, Pins, Logs, Traces, Connections. The Detached*Page
                    files are popout windows that mirror specific panels.

tests/integration/  Real-runtime suites (build tag `integration`). Cover gateway lifecycle, hot reload, autoscaler,
                    replicas, transports (incl. Podman), private git auth, generated Python source builds, and
                    optimize heuristics. Grouped auth tests use real HTTP and a subprocess MCP backend.
examples/           Example stack YAMLs grouped by surface (getting-started, transports, openapi, registry, secrets-vault,
                    code-mode, platforms, tracing, access-control, autoscale, declarative-link, gateways, portable-stack,
                    portable-pack, model-policy, python-sources). examples/_mock-servers/ is the source for `task mock:servers`.
scripts/            Build/test helpers and release tooling: release.py owns gate, inventory, verification, draft/public,
                    and tap policy; release-tools.py pins executables and the SPDX schema; release-acceptance.py exercises
                    authorized sandbox releases. test_release.py and test_govulncheck.py cover local policy regressions.
docs/               User-facing documentation (cli-reference, config-schema, api-reference, skills, packs, tools-workspace,
                    global-context, model-policy, scaling, usage-observability, installation, release-verification,
                    project-status, troubleshooting).
```

End-to-end request flow for an upstream HTTP MCP tool call: client → HTTP listener built by `pkg/controller` (gateway_builder.go) → `internal/api.Server.Handler` (CORS, Host validation, configured auth, and route/group selection) → `pkg/mcp` Streamable HTTP transport (Host/Origin checks and protocol handling) → `mcp.Gateway` router → per-server `mcp.Client` (process/stdio/SSE/HTTP/OpenAPI) → response, with telemetry, tracing, schema pinning, and (optional) output-format conversion attached on the way back. Legacy SSE routes return a negotiation hint rather than dispatching tools.

End-to-end for the web UI: component or React store action → `/api/...` handler in `internal/api/` → method on `Server` → call into the relevant `pkg/*` subsystem → JSON response → component or store state update → component re-render. The Stack spec view's Export YAML action calls `/api/stack/export` and downloads the non-resolving projection; raw spec retrieval and editing remain separate and may contain authored credentials. Verbose apply uses bounded controller diagnostic summaries, not resolved stack JSON.

## Constitution

`CONSTITUTION.md` is binding for every change. Articles that most often catch a refactor by surprise:

- **III (Test-first):** every exported function gets a test before merge; bug fixes need a regression test.
- **IV (Integration tests use real dependencies):** anything under `tests/integration/` uses real dependencies (containers, network connections, or subprocesses as applicable) and must pass `-race`. Mocks are unit-test only.
- **V (No panics in `pkg/` or `internal/`):** return errors. CLI init in `cmd/` is the only place panic is allowed.
- **VI (Context propagation):** any I/O, blocking, or external call takes `context.Context` as the first arg and respects cancellation.
- **VIII (Semantic versioning):** changed export output and newly rejected configurations are breaking changes; they require a major release and a changelog announcement, not a patch or minor release.
- **IX (Stack YAML back-compat):** new `stack.yaml` fields are optional with a default that preserves existing behavior. Renames and removals are breaking changes.
- **X (Machine-readable CLI output):** structured commands need machine-readable output (`--format json` or equivalent) and meaningful exit codes: `0` for success, nonzero for failure. Check each command's contract; export uses `0`/`1`, while several command families distinguish `0`/`1`/`2`.
- **XIV (Structured logging):** use `log/slog` in library code; no `fmt.Println` / `log.Printf`.
- **XV (Changelog discipline):** every user-visible change lands an entry under `[Unreleased]` in `CHANGELOG.md` in the same PR.

`CONTRIBUTING.md` covers branch prefixes, commit format, and the PR/CI process; follow it rather than re-deriving conventions.
