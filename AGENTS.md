# Gridctl Development Guide

## Project Overview

Gridctl is an MCP (Model Context Protocol) orchestration tool - "Containerlab for MCP Infrastructure".

**Architecture:**
- Controller (Go): Reads stack.yaml, manages Docker containers
- Gateway (Go): Protocol bridge that aggregates tools from downstream MCP servers
- UI (React + React Flow): Visualizes stack with real-time status

## Protocol Bridge Architecture

Gridctl's core value is acting as a **Protocol Bridge** between MCP transports:

```
                    ┌─────────────────────┐
                    │    Claude Desktop   │
                    │    (SSE Client)     │
                    └──────────┬──────────┘
                               │ SSE (GET /sse + POST /message)
                               ▼
                    ┌─────────────────────┐
                    │   Gridctl Gateway    │
                    │  (Protocol Bridge)  │
                    └───┬─────────────┬───┘
                        │             │
           Stdio        │             │  HTTP
    (Docker Attach)     ▼             ▼  (POST /mcp)
              ┌─────────────┐   ┌─────────────┐
              │  MCP Srv A  │   │  MCP Srv B  │
              │ (stdio MCP) │   │ (HTTP MCP)  │
              └─────────────┘   └─────────────┘
```

**Southbound (to MCP servers):**
- **Stdio (Container)**: Uses Docker container attach for stdin/stdout communication
- **Stdio (Local Process)**: Spawns local process on host, communicates via stdin/stdout
- **Stdio (SSH)**: Connects to remote host via SSH, communicates via stdin/stdout over the SSH connection
- **HTTP**: Standard HTTP POST to container's /mcp endpoint
- **External URL**: Connects to MCP servers running outside Docker

**Northbound (to clients):**
- **SSE**: Server-Sent Events for persistent connections (Claude Desktop)
- **HTTP POST**: Standard JSON-RPC 2.0 to /mcp endpoint

## Multi-Network Routing

Gridctl runs as a host binary (like Containerlab, Terraform, Docker Compose), enabling cross-network routing:

```
┌─────────────┐     ┌─────────────┐
│  Network A  │     │  Network B  │
│ (MCP Srv 1) │     │ (MCP Srv 2) │
└──────┬──────┘     └──────┬──────┘
       │   Docker Socket   │
       └─────────┬─────────┘
       ┌─────────▼─────────┐
       │   gridctl binary   │
       │  Routes JSON-RPC  │
       │  through memory   │
       └─────────┬─────────┘
       ┌─────────▼─────────┐
       │   localhost:8180  │
       └───────────────────┘
```

Network isolation between MCP servers while routing through the gateway.

## Directory Structure

```
gridctl/
├── cmd/gridctl/           # CLI entry point
│   ├── main.go           # Entry point
│   ├── root.go           # Cobra root command + serve command
│   ├── apply.go          # Start stack + gateway
│   ├── validate.go       # Validate stack YAML (exit 0/1/2, --format json)
│   ├── plan.go           # Diff spec against running state (--yes, --format json)
│   ├── export.go         # Reverse-engineer stack.yaml from running state (-o, --format)
│   ├── destroy.go        # Stop containers
│   ├── status.go         # Show container status
│   ├── info.go           # Show detected container runtime
│   ├── link.go           # Connect LLM clients to gateway
│   ├── unlink.go         # Remove gridctl from LLM clients
│   ├── reload.go         # Hot reload stack configuration
│   ├── skill.go          # Remote skill management (add, update, remove, pin, info, validate, try)
│   ├── vault.go          # Vault secret management commands
│   ├── pins.go           # Schema pin management commands
│   ├── traces.go         # Distributed traces CLI command (table, waterfall, follow)
│   ├── test.go           # Skill acceptance criteria runner (exit 0/1/2)
│   ├── activate.go       # Skill activation with acceptance criteria enforcement
│   ├── version.go        # Version command
│   ├── upgrade.go        # In-place upgrade to the latest release (sha256-verified)
│   ├── help.go           # Custom help template
│   ├── embed.go          # Embedded web assets
│   └── embed_stub.go     # Build stub for embed
├── internal/
│   ├── server/           # Legacy HTTP server (SPA only)
│   └── api/              # API server (MCP + REST + Registry)
│       ├── api.go        # Server setup and route registration
│       ├── auth.go       # Gateway authentication middleware
│       ├── registry.go   # Registry CRUD endpoints
│       ├── vault.go      # Vault REST API endpoints
│       ├── pins.go       # Schema pins REST API endpoints
│       └── stack.go      # Stack library endpoints (list, save, initialize)
├── pkg/
│   ├── config/           # Stack YAML parsing
│   │   ├── types.go      # Stack, Agent, Resource structs
│   │   ├── loader.go     # LoadStack() function
│   │   ├── expand.go     # Variable expansion (env + vault)
│   │   └── validate.go   # Validation rules
│   ├── dockerclient/     # Docker client interface
│   │   └── interface.go  # Interface definition for mocking
│   ├── logging/          # Logging utilities
│   │   ├── discard.go    # Discard logger
│   │   ├── buffer.go     # In-memory circular log buffer for API
│   │   ├── redact.go     # Secret redaction in log output
│   │   └── structured.go # Structured slog handler with buffering
│   ├── runtime/          # Workload orchestration (runtime-agnostic)
│   │   ├── interface.go  # WorkloadRuntime interface + types
│   │   ├── orchestrator.go # High-level Up/Down/Status
│   │   ├── factory.go    # Runtime factory registration
│   │   ├── compat.go     # Backward compatibility types
│   │   ├── depgraph.go   # Dependency graph for startup ordering
│   │   ├── detect.go     # Runtime detection (Docker/Podman socket probing, version query)
│   │   └── docker/       # Docker implementation
│   │       ├── driver.go     # DockerRuntime (implements WorkloadRuntime)
│   │       ├── init.go       # Factory registration
│   │       ├── client.go     # Docker client creation
│   │       ├── container.go  # Container lifecycle
│   │       ├── network.go    # Network management
│   │       ├── image.go      # Image pulling
│   │       └── labels.go     # Container naming/labels
│   ├── builder/          # Image building
│   │   ├── types.go      # BuildOptions, BuildResult
│   │   ├── cache.go      # ~/.gridctl/cache management
│   │   ├── git.go        # Git clone/update (thin wrapper over pkg/git)
│   │   ├── docker.go     # Docker build
│   │   └── builder.go    # Main builder
│   ├── git/              # Shared git helpers for skills + builder
│   │   ├── clone.go      # Clone, Fetch, Checkout, ResolveRef, HeadCommit, ListTags
│   │   ├── auth.go       # Auther interface + DetectProtocol: NoAuth, HTTPSTokenAuth, SSHAgentAuth, SSHKeyFileAuth
│   │   ├── errors.go     # Sentinel errors + ClassifyError (maps go-git errors to ErrAuth*, ErrNotFound, ErrHostKeyMismatch, etc.)
│   │   └── redact.go     # RedactURL, RedactString, RedactError - scrubs PATs and embedded userinfo
│   ├── output/           # CLI output formatting
│   │   ├── output.go     # Printer, banner display, and progress helpers
│   │   ├── styles.go     # Color schemes
│   │   └── table.go      # Table rendering
│   ├── controller/       # Deploy orchestration
│   │   ├── controller.go # Main deploy/destroy logic
│   │   ├── daemon.go     # Daemon mode (background process)
│   │   ├── gateway_builder.go # MCP gateway construction
│   │   └── server_registrar.go # MCP server registration
│   ├── jsonrpc/          # JSON-RPC 2.0 types
│   │   └── types.go      # Request, Response, Error types
│   ├── provisioner/      # LLM client provisioning (link/unlink)
│   │   ├── claude.go     # Claude Desktop provisioner
│   │   ├── cursor.go     # Cursor provisioner
│   │   ├── windsurf.go   # Windsurf provisioner
│   │   └── ...           # Other client provisioners
│   ├── reload/           # Hot reload support
│   │   ├── reload.go     # Reload handler, Initialize() for stackless cold-load, and result types
│   │   ├── diff.go       # Stack diff computation
│   │   └── watcher.go    # File watcher for --watch mode
│   ├── state/            # Daemon state management
│   │   └── state.go      # ~/.gridctl/state/, ~/.gridctl/logs/, StacksDir()
│   ├── mcp/              # MCP protocol
│   │   ├── types.go      # JSON-RPC, MCP types, AgentClient interface
│   │   ├── client.go     # HTTP transport client
│   │   ├── stdio.go      # Stdio transport client (Docker attach)
│   │   ├── process.go    # Local process transport client (host process)
│   │   ├── openapi_client.go # OpenAPI-backed MCP client
│   │   ├── sse.go        # SSE server (northbound)
│   │   ├── session.go    # Session management
│   │   ├── router.go     # Tool routing (name → ReplicaSet)
│   │   ├── replica_set.go # Replica pool (round-robin / least-connections dispatch + restart backoff 1s → 30s cap, ±25% jitter)
│   │   ├── gateway.go    # Protocol bridge logic, per-replica health monitor + reconnect
│   │   ├── handler.go    # HTTP handlers
│   │   ├── expand.go     # Environment variable expansion
│   │   ├── codemode.go       # Code mode orchestrator
│   │   ├── codemode_tools.go # Meta-tool definitions (search, execute)
│   │   ├── codemode_search.go # Tool search index
│   │   ├── codemode_sandbox.go # goja JavaScript sandbox
│   │   └── codemode_transpile.go # esbuild ES2020+ → ES2015 transpilation
│   ├── skills/           # Remote skill management (import, update, lockfile)
│   │   ├── config.go     # Skill source configuration (+ SourceAuth declarative block)
│   │   ├── fingerprint.go # Content fingerprinting for change detection
│   │   ├── importer.go   # Remote skill import (git clone/pull) + AuthConfig + CredentialResolver
│   │   ├── lockfile.go   # Lock file for pinned skill refs (+ CredentialRef per source)
│   │   ├── origin.go     # Origin tracking per skill (+ CredentialRef sidecar field)
│   │   ├── remote.go     # Remote git operations (CloneAndDiscover, FetchAndCompare)
│   │   ├── scanner.go    # Registry directory scanner
│   │   └── updater.go    # Skill update orchestration
│   ├── format/           # Output format converters
│   │   ├── format.go     # Format() dispatcher (toon, csv, json, text)
│   │   ├── toon.go       # TOON v3.0 converter (key-value, nested, tabular)
│   │   └── csv.go        # CSV converter (sorted headers, encoding/csv)
│   ├── token/            # Token counting
│   │   └── counter.go    # Counter interface + heuristic implementation (4 bytes/token)
│   ├── metrics/          # Metrics accumulation
│   │   ├── accumulator.go # Atomic session/per-server counters, ring buffer time buckets, format savings
│   │   └── observer.go   # Observer adapter bridging ToolCallObserver to counter + accumulator
│   ├── tracing/          # Distributed tracing (OpenTelemetry)
│   │   ├── provider.go   # OTel trace provider with in-process exporter
│   │   ├── buffer.go     # Ring buffer (1000 traces) with filter API (server, errors, min_duration)
│   │   ├── carrier.go    # W3C TraceContext propagation carrier for MCP transport boundaries
│   │   └── config.go     # Tracing configuration types
│   ├── vault/            # Secrets vault
│   │   ├── types.go      # Secret, Set, EncryptedVault types
│   │   ├── crypto.go     # XChaCha20-Poly1305 + Argon2id envelope encryption
│   │   └── store.go      # CRUD, lock/unlock, variable sets, import/export
│   ├── pins/             # TOFU schema pinning (rug pull protection)
│   │   ├── types.go      # PinRecord, ServerPins, status constants
│   │   ├── store.go      # PinStore: load/save (atomic), VerifyOrPin, Approve, Reset
│   │   └── adapter.go    # GatewayAdapter: bridges PinStore to SchemaVerifier interface
│   ├── registry/         # Agent Skills registry (agentskills.io) - dispatches to typed handlers via pkg/agent/
│   │   ├── types.go      # AgentSkill, SkillFile, ItemState, HandlerLanguage (none|go|ts)
│   │   ├── frontmatter.go # SKILL.md parsing (YAML frontmatter + markdown body)
│   │   ├── validator.go  # agentskills.io spec validation
│   │   ├── store.go      # Directory-based persistent store; walker recognises *.go and *.ts siblings of SKILL.md, exposes Store.HandlerPath(name)
│   │   └── server.go     # MCP server interface for registry; SetSkillRegistry + SetTSDispatcher route typed-skill CallTool
│   └── agent/            # Code-first agent runtime (graph composition + LLM provider abstraction)
│       ├── agent.go      # Public type surface: Graph[I,O], Runnable[I,O], StreamReader[T], ToolInfo, ChatRequest/Response/Chunk, Message, ToolCall, ToolResult, ChatModel
│       ├── toolcaller.go # ToolCaller interface (alias of mcp.ToolCaller); ToolCallResult type alias
│       ├── internal/eino/ # Boundary layer - only place github.com/cloudwego/eino is referenced; enforced by scripts/check-eino-boundary.sh
│       ├── gateway/      # Adapter: NewToolCaller(*mcp.Gateway) → agent.ToolCaller
│       ├── compose/      # Approval gate primitive: NewGate(run_id, recorder, registry, notifier) returns sandbox.Approver-compatible Gate
│       ├── orchestrator/ # Single-writer multi-agent: Orchestrator[State], Handoff[State, Out], ParallelHandoff[State, Out] (clamped at 4)
│       ├── persist/      # JSONL run ledger at ~/.gridctl/runs/<run_id>.jsonl; Store, Recorder, Read/Stream/List/Summary, BuildResumePlan, global event Bus
│       ├── runner/       # Skill-run orchestration: opens the JSONL ledger, dispatches through an Executor interface, records EventRunStarted/Completed/Error
│       ├── runtime/      # Process-wide handle aggregating run store, approval registry, sandbox, dev server, and active LLM provider behind mcp.AgentRuntime
│       ├── sandbox/      # TS skill loader: esbuild transpile + per-call goja with tool()/llm()/parallel()/handoff()/approval() bindings + skill.body/name globals
│       ├── skill/        # Typed Skill SDK: Definition, Registry, Define[I, O], MustDefine, RunContext with SkillBody()/SkillName(), WithSkillBody context helper
│       ├── dev/          # Visual IDE backend - code is canon, the IDE never writes back to source
│       │   ├── parser/   # Go AST + TS regex/lexer; emits flat node list of recognised primitives
│       │   ├── watcher/  # Recursive fsnotify watcher with 200ms coalescing debounce
│       │   ├── devserver/ # HTTP routes for /api/agent/dev/{skills,events}
│       │   └── scaffold/ # Renders `agent init` starter (SKILL.md + skill.ts + agent.json) for ts/go/prompt-only flavors
│       └── llm/          # LLM provider abstraction (net/http + encoding/json only)
│           ├── llm.go    # Provider type alias of agent.ChatModel
│           ├── anthropic/ # Anthropic Messages API (Generate + Stream + tools.go + messages.go)
│           ├── openai/   # OpenAI Chat Completions API
│           ├── google/   # Google Gemini Generative Language API
│           ├── gateway/  # Prefix-routing provider that dispatches by model name
│           └── observed/ # ChatModel wrapper that adds OTel spans + pricing.CalculateBreakdown + metrics.Accumulator.RecordCost (synthetic "llm:<provider>" server name)
├── web/                  # React frontend (Vite)
├── examples/             # Example topologies
│   ├── getting-started/  # Basic examples
│   ├── transports/       # Transport-specific examples
│   ├── access-control/   # Tool filtering and security examples
│   ├── code-mode/        # Code mode (search + execute meta-tools) examples
│   ├── gateways/         # Gateway configuration examples
│   ├── platforms/        # Platform-specific examples
│   ├── secrets-vault/    # Vault secrets and variable sets
│   └── _mock-servers/    # Mock MCP servers for testing
└── tests/
    └── integration/      # Integration tests (build tag: integration)
        ├── transport_test.go         # TestMain, mock-server harness, freePort/startMockServer/waitForPort
        ├── gateway_lifecycle_test.go # Gateway register/unregister/health monitor/shutdown
        ├── hot_reload_test.go        # Reload handler: add/remove/modify servers
        ├── runtime_test.go           # Full stack lifecycle, resources, networks
        ├── podman_test.go            # Podman rootless networking
        ├── skills_private_git_test.go # Skill import via private git over HTTPS PAT and SSH agent
        ├── openapi_test.go           # OpenAPI spec parsing + auth
        ├── replica_kill_one_test.go       # Kill one replica, verify exclusion + survivors
        ├── replica_all_down_test.go       # All replicas down → structured error
        ├── replica_restart_storm_test.go  # Backoff prevents reconnect spin
        ├── replica_stackless_mode_test.go # Multi-replica register in stackless mode
        └── replica_mixed_counts_test.go   # 1-replica + 3-replica server in one gateway
```

## Private Git Auth

Gridctl clones private git repositories in two places - skill imports (`pkg/skills`) and MCP server source builds (`pkg/builder`). Both go through the shared `pkg/git` package.

**Layering:**

```
cmd/gridctl/skill.go              internal/api/skills.go
    │ --auth-token / --vault-key       │ { "auth": { method, token, credentialRef, ... } }
    └──────────────┬───────────────────┘
                   │ skills.AuthConfig
                   ▼
            pkg/skills/importer.go          pkg/builder/git.go
                   │ BuildAuther()                 │
                   ▼                               ▼
                                  pkg/git/auth.go (Auther interface)
                                  ├── NoAuth                (public / ambient)
                                  ├── HTTPSTokenAuth        (PAT → http.BasicAuth)
                                  ├── SSHAgentAuth          (ambient SSH_AUTH_SOCK)
                                  └── SSHKeyFileAuth        (on-disk key)
                                           │
                                           ▼
                              go-git transport.AuthMethod → clone/fetch
```

**Auther interface.** `pkg/git.Auther.AuthFor(url)` returns the `transport.AuthMethod` go-git uses. Each implementation validates its own inputs eagerly (`ErrEmptyToken`, `ErrProtocolMismatch`, `ErrSSHAgentMissing`) so a misconfigured auth fails fast rather than falling through to an unauthenticated clone that returns a cryptic "not found". `pkg/git.DetectProtocol(url)` classifies URLs as `HTTPS`, `SSH`, `Local`, or `Unknown`; implementations reject URLs whose protocol doesn't match their mechanism.

**Resolver threading - `${vault:KEY}` is resolved once, at the boundary.**

- The **CLI** (`cmd/gridctl/skill.go`) resolves `--vault-key GIT_TOKEN` into a raw token in `buildAuthConfigFromFlags` before calling `Importer.Import`. It also registers a `skills.CredentialResolver` on the importer so `skill update` can re-resolve stored references automatically.
- The **API** (`internal/api/skills.go`) does the same inside `AuthRequest.toAuthConfig`, consulting the live `vault.Store` on every request so rotated secrets take effect immediately.
- `skills.AuthConfig` carries both the transient `Token` and the opaque `CredentialRef` (e.g. `${vault:GIT_TOKEN}`). The importer passes `CredentialRef` through to `Origin.CredentialRef` and `LockedSource.CredentialRef`; the `Token` never leaves the request/command lifecycle.

**Credential-never-persisted-raw invariant.** Raw tokens and SSH passphrases live in exactly one place on disk: the encrypted vault (`pkg/vault`). Nothing else writes them.

- `Origin.CredentialRef` and `LockedSource.CredentialRef` store only the reference string.
- `skills.yaml` (`SourceAuth.CredentialRef`) stores only the reference string.
- Error paths go through `git.RedactError` before reaching logs, API responses, or CLI stderr. `git.RedactString` scrubs known PAT patterns (`ghp_…`, `github_pat_…`, `glpat-…`) and embedded URL userinfo; `git.RedactURL` strips userinfo from a bare URL. Callers should prefer `credential_ref` + vault over embedding tokens in URLs, since redaction is a defense-in-depth layer, not a substitute for structured credential handling.

**Error classification.** `git.ClassifyError` maps raw go-git errors into sentinels (`ErrAuthRequired`, `ErrAuthFailed`, `ErrNotFound`, `ErrHostKeyMismatch`, `ErrSSHAgentMissing`). The CLI turns these into human hints via `printSkillAuthHint`; the API maps them to HTTP status codes via `gitErrorStatus` (401, 404, 400, 500).

**Scope.** v1 ships HTTPS PAT (via vault or ephemeral flag / env) and SSH-via-agent; SSH key-file auth is wired but lacks a stricter host-key policy - `SSHKeyFileAuth.KnownHostsPath` is reserved for follow-up work. Out of scope for v1: OAuth device flow, GitHub App tokens, OS keychain, 1Password/Bitwarden passthrough, credential templates, Git LFS auth.

## Build Commands

```bash
make build           # Build frontend and backend
make build-web       # Build React frontend only
make build-go        # Build Go binary only
make dev             # Run Vite dev server
make clean           # Remove build artifacts
make deps            # Install dependencies
make run             # Build and run
make test            # Run unit tests
make test-coverage   # Run tests with coverage report
make test-integration # Run integration tests (requires Docker)
make generate        # Regenerate mock files (requires mockgen)
make mock-servers    # Build and run mock MCP servers for examples
make clean-mock-servers # Stop and remove mock MCP servers
```

## Installation & Distribution

Three install paths are supported. They all consume the same GoReleaser
artifacts; only the front-end mechanism differs.

| Path | Mechanism | Updates via |
|---|---|---|
| Curl one-liner | `install.sh` served from `raw.githubusercontent.com/gridctl/gridctl/main/install.sh` | `gridctl upgrade` (in-place, atomic rename) |
| Homebrew | `gridctl/homebrew-tap` formula auto-published by GoReleaser post-release | `brew upgrade gridctl/tap/gridctl` |
| Source | `git clone && make build` | `git pull && make build` |

`gridctl upgrade` detects Homebrew-managed binaries (path contains
`/Cellar/`, `/homebrew/`, or `/linuxbrew/`) and defers to `brew upgrade`
unless `--force` is passed.

### Release artifact contract

Both `install.sh` and `cmd/gridctl/upgrade.go` consume two
artifacts produced by GoReleaser. **Renaming either breaks both
consumers silently for the next release.**

| Artifact | Source | Consumed by |
|---|---|---|
| `gridctl_<version>_<os>_<arch>.tar.gz` | `.goreleaser.yaml` `archives.name_template` | install.sh `download()`, upgrade.go `extractGridctlBinary()` |
| `checksums.txt` | `.goreleaser.yaml` `checksum.name_template` | install.sh `verify_checksum()`, upgrade.go `verifySHA256()` |

Version strings in archive names omit the leading `v` (e.g.
`gridctl_0.1.0-beta.6_darwin_arm64.tar.gz`); release tags include it
(e.g. `v0.1.0-beta.6`). Both consumers handle the conversion explicitly.

### Pre-release vs stable

While the project is in `v0.1.0-beta.x`, both consumers resolve "latest"
via the GitHub API path
`https://api.github.com/repos/gridctl/gridctl/releases?per_page=1`,
which returns the most recent release **including pre-releases**.

When stable `v0.1.0` ships, the contract changes: switch both consumers
to the lighter `releases/latest` redirect (which excludes pre-releases)
so the curl installer and `gridctl upgrade` serve stable users a stable
build. Affected lines:

- `install.sh` - `resolve_version()`
- `cmd/gridctl/upgrade.go` - `fetchLatestTag()`

Both files have a `TODO` comment marking the call site.

### Smoke test

`.github/workflows/install-smoke.yaml` exercises the full lifecycle on
Ubuntu and macOS (install → re-run for idempotency → `gridctl upgrade
--check` → `--uninstall` → `--uninstall --purge`) plus a separate
`shellcheck -s sh` job. Triggers:

- PRs touching `install.sh` or the workflow itself
- Pushes to `main` touching `install.sh`
- Weekly cron (Monday 08:00 UTC) - surfaces release-artifact drift

## CLI Usage

```bash
# Start the web UI and API server without a stack (stackless mode)
./gridctl serve
./gridctl serve --port 8180 --foreground

# Start a stack (runs as daemon, returns immediately)
./gridctl apply examples/getting-started/mcp-basic.yaml

# Start in stackless mode (same as serve; stack loaded later via wizard)
./gridctl apply

# Start with options
./gridctl apply stack.yaml --port 8180 --no-cache

# Run in foreground with verbose output (for debugging)
./gridctl apply stack.yaml --foreground

# Watch for changes and auto-reload
./gridctl apply stack.yaml --watch

# Apply and auto-link all detected LLM clients
./gridctl apply stack.yaml --flash

# Check running gateways and containers
./gridctl status

# Connect an LLM client to the gateway
./gridctl link

# Remove gridctl from an LLM client
./gridctl unlink

# Hot reload a running stack
./gridctl reload

# Stop a specific stack (gateway + containers)
./gridctl destroy examples/getting-started/mcp-basic.yaml

# Manage secrets
./gridctl vault set API_KEY
./gridctl vault list
./gridctl vault import .env

# View distributed traces
./gridctl traces
./gridctl traces <trace-id>
./gridctl traces --follow
./gridctl traces --server github --errors

# Surface cost-reduction findings (unused servers and tools)
./gridctl optimize
./gridctl optimize --stack mcp-basic
./gridctl optimize --min-impact 0.10
./gridctl optimize --format json

# Manage schema pins (TOFU rug pull protection)
./gridctl pins list
./gridctl pins verify
./gridctl pins verify --exit-code
./gridctl pins approve github
./gridctl pins reset github

# Spec-driven skill development
./gridctl test my-skill            # Run acceptance criteria (exit 0/1/2)
./gridctl activate my-skill        # Promote from draft to active
./gridctl skill validate my-skill  # Validate skill definition and frontmatter

# Upgrade an existing standalone install in place
./gridctl upgrade --check          # Report current vs. latest version
./gridctl upgrade --yes            # Non-interactive (CI / cron); refuses without --yes in non-TTY shells
./gridctl upgrade --version v0.1.0-beta.6   # Pin a specific tag (allows downgrades)
./gridctl upgrade --force          # Bypass Homebrew detection and the up-to-date short-circuit
```

### Command Reference

#### `gridctl apply <stack.yaml>`

Starts containers and MCP gateway for a stack.

| Flag | Short | Description |
|------|-------|-------------|
| `--foreground` | `-f` | Run in foreground with verbose output (don't daemonize) |
| `--port` | `-p` | Port for MCP gateway (default: 8180) |
| `--base-port` | | Base port for MCP server host port allocation (default: 9000) |
| `--no-cache` | | Force rebuild of source-based images |
| `--quiet` | `-q` | Suppress progress output (show only final result) |
| `--verbose` | `-v` | Print full stack as JSON |
| `--watch` | `-w` | Watch stack file for changes and hot reload |
| `--flash` | | Auto-link detected LLM clients after apply |
| `--code-mode` | | Enable gateway code mode (replaces tools with search + execute meta-tools) |
| `--no-expand` | | Disable environment variable expansion in OpenAPI spec files |

#### `gridctl plan <stack.yaml>`

Compares the stack spec against running state and shows a structured diff.

| Flag | Short | Description |
|------|-------|-------------|
| `--yes` | `-y` | Auto-approve and apply changes |
| `--auto-approve` | | Auto-approve and apply changes (CI/CD equivalent of `-y`) |
| `--format` | | Output format: `json` for machine-readable output |

#### `gridctl validate <stack.yaml>`

Validates the stack spec including config schema, transport rules, and skill definitions. Exit codes: `0` valid, `1` validation error, `2` infrastructure error.

| Flag | Short | Description |
|------|-------|-------------|
| `--format` | | Output format: `json` for machine-readable output |

#### `gridctl export`

Reverse-engineers a `stack.yaml` from the currently running deployment.

| Flag | Short | Description |
|------|-------|-------------|
| `--output` | `-o` | Output directory (default: stdout) |
| `--format` | | Output format: `yaml` (default) or `json` |

#### `gridctl destroy <stack.yaml>`

Stops the gateway daemon and removes all containers for a stack.

#### `gridctl status`

Shows running gateways and containers.

| Flag | Short | Description |
|------|-------|-------------|
| `--stack` | `-s` | Filter by stack name |

#### `gridctl link [client]`

Connect an LLM client to the gridctl gateway. Without arguments, detects installed clients and presents a selection list.

Supported clients: claude, claude-code, cursor, windsurf, vscode, gemini, opencode, continue, cline, anythingllm, roo, zed, goose

| Flag | Short | Description |
|------|-------|-------------|
| `--port` | `-p` | Gateway port (default: auto-detect from running stack, else 8180) |
| `--all` | `-a` | Link all detected clients at once |
| `--name` | `-n` | Server name in client config (default: "gridctl") |
| `--dry-run` | | Show what would change without modifying files |
| `--force` | | Overwrite existing gridctl entry even if present |

#### `gridctl unlink [client]`

Remove gridctl from an LLM client's MCP configuration. Without arguments, detects linked clients and presents a selection.

| Flag | Short | Description |
|------|-------|-------------|
| `--all` | `-a` | Unlink from all clients |
| `--name` | `-n` | Server name to remove (default: "gridctl") |
| `--dry-run` | | Show what would change without modifying files |

#### `gridctl reload [stack-name]`

Triggers a hot reload of the stack configuration. The stack must be running with the `--watch` flag, or use this command to manually trigger a reload. If no stack name is provided, reloads all running stacks.

#### `gridctl serve`

Starts the API server and web UI in stackless mode - no stack file required. Stack-dependent endpoints return 503 until a stack is loaded via the wizard. Vault and wizard endpoints are always available.

| Flag | Short | Description |
|------|-------|-------------|
| `--port` | `-p` | Port for the API server and web UI (default: 8180) |
| `--foreground` | `-f` | Run in foreground (don't daemonize) |

`gridctl apply` (with no arguments) is equivalent to `gridctl serve`.

#### `gridctl vault <subcommand>`

Manage secrets stored in `~/.gridctl/vault/`. Secrets can be referenced in stack YAML via `${vault:KEY}` syntax.

| Subcommand | Description |
|------------|-------------|
| `set <KEY>` | Store a secret (prompts for value, or use `--value`) |
| `get <KEY>` | Retrieve a secret (masked by default, use `--plain`) |
| `list` | List all secret keys with set assignments |
| `delete <KEY>` | Delete a secret (`--force` to skip confirmation) |
| `import <file>` | Import from .env or .json (`--format` to override auto-detection) |
| `export` | Export all secrets (`--format env\|json`, `--plain` for unmasked) |
| `lock` | Encrypt vault with a passphrase |
| `unlock` | Decrypt vault for the session |
| `change-passphrase` | Re-encrypt vault with a new passphrase |
| `sets list` | List variable sets |
| `sets create <name>` | Create a variable set |
| `sets delete <name>` | Delete a variable set |

**Flags:**

| Flag | Subcommand | Description |
|------|------------|-------------|
| `--value` | `set` | Non-interactive secret value |
| `--set` | `set` | Assign secret to a variable set |
| `--plain` | `get`, `export` | Show unmasked value |
| `--force` | `delete` | Skip confirmation prompt |
| `--format` | `import`, `export` | File format: `env` or `json` |

The `GRIDCTL_VAULT_PASSPHRASE` environment variable can provide the passphrase non-interactively for `lock`, `unlock`, and `change-passphrase` commands. When the vault is locked, all other vault commands auto-prompt for the passphrase.

#### `gridctl pins <subcommand>`

Manage TOFU schema pins that protect against rug pull attacks (CVE-2025-54136 class). On first apply, SHA256 hashes of all tool definitions are pinned to `~/.gridctl/pins/{stackName}.json`. Subsequent applies and reconnects verify tool definitions against stored hashes and surface drift.

| Subcommand | Description |
|------------|-------------|
| `list` | Table of all servers: tool count, status (pinned/drift/approved), last verified timestamp |
| `verify [server]` | Show pin verification status for all servers or a specific server |
| `approve <server>` | Re-pin current tool definitions for a server, clearing drift status |
| `reset <server>` | Delete pins for a server (re-pinned on next apply) |

**Flags:**

| Flag | Subcommand | Description |
|------|------------|-------------|
| `--stack` | all | Stack name (auto-detected when only one stack is running) |
| `--exit-code` | `verify` | Exit 1 if any server has drift (CI use case) |

#### `gridctl optimize`

Scan the running gateway and print cost-reduction findings - unused servers and unused tools in v1 - with a measured weekly USD impact and a paste-ready YAML remediation. The CLI requires the gateway to be running; it never reads client-side session files.

| Flag | Short | Description |
|------|-------|-------------|
| `--stack` | `-s` | Stack to query (auto-detected when only one stack is running) |
| `--min-impact` | | Filter findings below this weekly USD impact (info findings always shown) |
| `--severity` | | Comma-separated severity allowlist: `info,warn,critical` |
| `--format` | | Output format: `json` for machine-readable `OptimizeReport` (default: table) |

Exit codes: `0` no findings or info-only, `1` at least one `warn`/`critical` finding, `2` infrastructure error (gateway unreachable, ambiguous or unknown stack).

#### `gridctl test <skill-name>`

Run acceptance criteria for a skill against the running gateway. The runner POSTs to `/api/registry/skills/{name}/test`; the server hands each criterion to an LLM judge when a chat provider is configured (set `ANTHROPIC_API_KEY` in the vault), otherwise falls back to a deterministic adapter that reads explicit `PASS:` / `FAIL:` markers — useful in CI when criteria are fixture-encoded.

| Flag | Short | Description |
|------|-------|-------------|
| `--stack` | `-s` | Stack to test against (auto-detect if only one running) |
| `--format` | | Output format: `json` for machine-readable `TestReport` (default: table) |
| `--dry-run` | | List criteria without evaluating them |
| `--criterion` | | Zero-based index of a single criterion to evaluate (default: all) |

Exit codes: `0` all criteria passed, `1` one or more criteria failed, `2` infrastructure error (gateway unreachable, skill not found, criterion index out of range).

Acceptance criteria in `SKILL.md` frontmatter are free-form Given/When/Then prose; the LLM judge reads them directly. The deterministic adapter requires criteria to start with `PASS:` or `FAIL:` and reports `error` severity for ambiguous prose, so production prose criteria do not pretend to pass against a missing provider.

#### `gridctl activate <skill-name>`

Transition a skill from draft to active state. Skills with a Go or TypeScript handler must have `acceptance_criteria` defined before activation is permitted.

Exit codes: `0` activated, `1` blocked by missing acceptance criteria or validation error.

### Daemon Mode

By default, `gridctl apply` runs the MCP gateway as a background daemon:
- Waits until all MCP servers are initialized before returning (up to 60s timeout)
- State stored in `~/.gridctl/state/{name}.json`
- Logs written to `~/.gridctl/logs/{name}.log`
- Use `--foreground` (-f) to run interactively with verbose output

### State Files

Gridctl stores daemon state in `~/.gridctl/`:

```
~/.gridctl/
├── state/              # Daemon state files
│   └── {name}.json     # PID, port, start time per stack
├── logs/               # Daemon log files
│   └── {name}.log      # stdout/stderr from daemon
├── stacks/             # Saved stack library (created by wizard Save & Load)
│   └── {name}.yaml     # Saved stack specs (loaded via POST /api/stack/initialize)
├── vault/              # Secrets vault (0700 permissions)
│   ├── secrets.json    # Plaintext secrets (when unlocked/unencrypted)
│   └── secrets.enc     # Encrypted secrets (when locked)
├── pins/               # TOFU schema pin files (one per stack)
│   └── {name}.json     # SHA256 hashes of tool definitions per server
└── cache/              # Build cache
    └── ...             # Git repos, Docker contexts
```

## MCP Gateway

When `gridctl apply` runs, it:
1. Parses the stack YAML
2. Creates Docker network
3. Builds/pulls images
4. Starts containers with host port bindings (9000+)
5. Registers MCP servers with the gateway
6. Starts HTTP server with MCP endpoint

**Endpoints:**
- **MCP:** `POST /mcp` (JSON-RPC), `GET /sse` + `POST /message` (SSE for Claude Desktop; `/message` returns `410 Gone` and is retained only for legacy clients)
- **API:** `/api/status`, `/api/mcp-servers`, `/api/mcp-servers/{name}/restart`, `/api/mcp-servers/{name}/tools` (PUT), `/api/tools`, `/api/logs`, `/api/clients`, `/api/reload`, `/api/metrics/tokens`, `/api/metrics/cost`, `/api/optimize`, `/api/agent/runs` (GET list, POST launch), `/api/agent/runs/events/stream` (global SSE bus across every active run), `/api/agent/runs/{run_id}[/events|resume|approve]`, `/api/agent/dev/{skills|events}`, `/api/playground/{auth|chat|stream}`, `/api/telemetry/inventory`, `/api/telemetry` (DELETE), `/api/mcp-servers/{name}/telemetry` (PATCH), `/health`, `/ready`
- **Stack Library:** `/api/stacks` (list/save), `/api/stack/initialize` (cold-load a saved stack into a stackless daemon)
- **Vault:** `/api/vault`, `/api/vault/status`, `/api/vault/unlock`, `/api/vault/lock`, `/api/vault/sets`, `/api/vault/import`
- **Pins:** `/api/pins` (list all), `/api/pins/{server}` (get), `/api/pins/{server}/approve` (POST), `/api/pins/{server}` (DELETE)
- **Registry:** `/api/registry/status`, `/api/registry/skills[/{name}]`, `/api/registry/skills/{name}/files[/{path}]`, `/api/registry/skills/validate`, `/api/registry/skills/{name}/test`
- **Traces:** `/api/traces`, `/api/traces/{traceId}`
- **Web UI:** `GET /`

**Logs API:**
- `GET /api/logs` - Returns structured gateway logs as JSON array
- Query params: `lines` (default 100), `level` (filter by log level)
- Response: `LogEntry[]` with fields: `level`, `ts`, `msg`, `component`, `trace_id`, `attrs`

**Clients API:**
- `GET /api/clients` - Returns status of detected LLM clients (name, detected, linked, transport)

**Reload API:**
- `POST /api/reload` - Triggers hot reload of the stack configuration (requires `--watch` flag on apply)

**MCP Server Control API:**
- `POST /api/mcp-servers/{name}/restart` - Restart an individual MCP server connection

**Token Metrics API:**
- `GET /api/metrics/tokens?range=1h` - Historical time-series token data (range: 30m, 1h, 6h, 24h, 7d)
- `DELETE /api/metrics/tokens` - Clear all token metrics
- `GET /api/status` includes `token_usage` (session totals, per-server breakdown, format savings)

**Optimize API:**
- `GET /api/optimize?stack={name}&min_impact=0.10&severity=warn,critical` - Returns an `OptimizeReport{findings, health_score, generated_at}` derived from the live gateway state, accumulator snapshot, and (when wired) the agent persistence store. Each finding carries `id`, `heuristic` (`unused_server`, `unused_tool`, `schema_overhead`, `format_savings_shortfall`, `expensive_model_on_cheap_task`, `unbounded_loop`, `oversized_prompt`, `untyped_handoff`, `need_more_data`), `severity`, `title`, `summary`, `server`, `tool`, `impact_usd_per_week`, `remediation` (YAML or shell snippet), and `detected_at`. Returns `404` when `stack` does not match the active stack and `503` when the metrics accumulator is not configured.

**Registry API:**
- `GET /api/registry/status` - Returns skill counts
- `GET /api/registry/skills` - List all skills
- `POST /api/registry/skills` - Create a skill
- `GET/PUT/DELETE /api/registry/skills/{name}` - CRUD for individual skills
- `POST /api/registry/skills/{name}/activate` - Activate a skill
- `POST /api/registry/skills/{name}/disable` - Disable a skill
- `GET /api/registry/skills/{name}/files` - List files in skill directory
- `GET/PUT/DELETE /api/registry/skills/{name}/files/{path}` - File management
- `POST /api/registry/skills/validate` - Validate SKILL.md content

**Skill Test API:**
- `POST /api/registry/skills/{name}/test` - Run acceptance criteria for a skill
  - Returns `SkillTestResult` with `skill`, `passed`, `failed`, `skipped`, and `results[]`
  - Each result has `criterion`, `passed`, `skipped`, `skip_reason`, and `actual`
  - HTTP 400 if the skill has no acceptance criteria defined
  - HTTP 404 if skill not found

**Vault API:**
- `GET /api/vault/status` - Returns `{locked, encrypted, secrets_count?, sets_count?}`
- `POST /api/vault/unlock` - Unlock encrypted vault (body: `{passphrase}`)
- `POST /api/vault/lock` - Encrypt vault with passphrase (body: `{passphrase}`)
- `GET /api/vault` - List secrets (keys and set assignments, no values)
- `POST /api/vault` - Create secret (body: `{key, value, set?}`)
- `GET /api/vault/{key}` - Get secret value
- `PUT /api/vault/{key}` - Update secret value
- `DELETE /api/vault/{key}` - Delete secret
- `GET /api/vault/sets` - List variable sets with counts
- `POST /api/vault/sets` - Create variable set
- `DELETE /api/vault/sets/{name}` - Delete variable set
- `PUT /api/vault/{key}/set` - Assign secret to a set
- `POST /api/vault/import` - Bulk import (body: `{secrets: {key: value, ...}}`)

When the vault is locked, all endpoints except `status`, `unlock`, and `lock` return HTTP 423 (Locked).

**Traces API:**
- `GET /api/traces` - List recent traces (ring buffer, newest first)
  - Query params: `server` (filter by server name), `errors=true` (errors only), `min_duration` (e.g. `100ms`, `1s`), `limit` (default 100)
  - Response: `TraceRecord[]` with fields: `trace_id`, `operation`, `start_time`, `duration_ms`, `span_count`, `is_error`, `spans`
- `GET /api/traces/{traceId}` - Get a single trace with full span detail
  - Response: `TraceRecord` with `spans[]` - each span has `span_id`, `name`, `start_time`, `duration_ms`, `is_error`, `attrs` (OTel attributes)

**Stack Library API:**
- `GET /api/stacks` - List saved stacks from `~/.gridctl/stacks/`; returns `{stacks: [{name, path, size, modTime}]}`
- `POST /api/stacks` - Save a stack spec to the library; body: `{name, content}` (YAML string); returns `{name, path}`
- `POST /api/stack/initialize` - Cold-load a named saved stack into a running stackless daemon; body: `{name}`; returns 409 (`StackAlreadyActiveError`) if a stack is already running

**`/ready` behavior:**
- Returns `200 OK` when a stack is fully initialized
- Returns `503 Service Unavailable` in stackless mode (no stack loaded yet)

**Tool prefixing:** Tools are prefixed with server name to avoid collisions:
- `server-name__tool-name` (e.g., `itential-mcp__get_workflows`)

**Replica sets:** Each registered server is a `ReplicaSet` in the router - a pool of 1..N `AgentClient` replicas sharing one server name and one tool namespace. Set `replicas: N` (and optionally `replica_policy: round-robin | least-connections`) in `mcp-servers[]` to spawn N independent processes. Validation caps at 32 and rejects replicas on `external` / `openapi` transports. Per-replica health monitor pings each replica independently, excludes failures from dispatch, and reconnects with exponential backoff (1s → 30s cap, ±25% jitter, reset on success). When every replica is unhealthy, tool calls fail with `no healthy replicas: <server>`. Every log line and trace span on the tool-call path carries a `replica_id`; `gridctl status --replicas` and `/api/stack/health` expose the per-replica breakdown. See [docs/scaling.md](docs/scaling.md).

**Autoscale:** A server can replace static `replicas: N` with a reactive `autoscale:` block - same transport rules, same replica-set plumbing, but with a per-set controller in `pkg/mcp/autoscaler` that spawns/reaps replicas based on rolling-median in-flight load (`min`, `max`, `target_in_flight`, `scale_up_after`, `scale_down_after`, `warm_pool`, `idle_to_zero`). `autoscale` and `replicas` are mutually exclusive on the same server. Live snapshots are published on `/api/status` and `/api/mcp-servers` as `autoscale?: AutoscaleStatus` (current, target, median, lastDecision, lastScaleUp/DownAt); `gridctl status --replicas` surfaces the same via an `AUTOSCALE` column. See [docs/scaling.md#autoscaling](docs/scaling.md#autoscaling).

## Agent Runtime Architecture

The code-first agent runtime (`pkg/agent/`) implements a three-layer mental model that the rest of the codebase reasons in terms of:

1. **Gateway** (existing, `pkg/mcp/`): MCP protocol bridge that aggregates tools from heterogeneous downstream MCP servers behind a single endpoint. Owns tool prefixing (`server__tool`), replica routing, vault auth, schema pinning, format conversion, tracing, and per-tool metrics. Unchanged by the agent runtime - every agent tool call still flows through `Gateway.CallTool`.
2. **Agent Runtime** (new, `pkg/agent/`): Typed graph composition (via the `internal/eino/` adapter), an LLM provider abstraction (`llm/anthropic`, `llm/openai`, `llm/google`, prefix-routed by `llm/gateway`), the Skill SDK (`skill/`), the TS sandbox (`sandbox/`), the single-writer orchestrator (`orchestrator/`), JSONL run persistence and time-travel resume (`persist/`), the approval gate primitive (`compose/`), and the IDE backend (`dev/`). The `internal/eino/` boundary is enforced by `scripts/check-eino-boundary.sh` - no `github.com/cloudwego/eino` types appear outside that directory.
3. **Skill Registry** (existing, recontextualised, `pkg/registry/`): Discovery, packaging, remote import via git, lockfile, and source-only fingerprinting. The walker recognises `*.go` and `*.ts` siblings of `SKILL.md`; `pkg/registry.Server.Tools()` and `Server.CallTool()` expose registered typed skills as MCP tools, so local execution (`gridctl run <skill>`) and remote execution (an upstream MCP client invoking via the gateway, including a second gridctl pointed at the first) share one code path.

Skills are typed Go (`skill.Define[I, O]("name", "description", run)` - input and output structs with `jsonschema` tags) or TypeScript (transpiled via the existing esbuild path, executed in `pkg/agent/sandbox/`'s goja runtime with `tool()`, `llm()`, `parallel()`, `handoff()`, `approval()` bindings). Both flavours register through the same registry and surface as MCP tools through the gateway - there is no "internal" vs "external" execution mode.

**Observability wiring.** Tracing, pricing, and metrics are wired through the same primitives the gateway uses:

- **Tracing** (`pkg/tracing/`): every orchestrator handoff (`agent.orchestrator.handoff`) and parallel batch (`agent.orchestrator.parallel`) opens an OTel span under tracer `gridctl.agent.orchestrator`. LLM calls wrapped through `pkg/agent/llm/observed/` open `agent.llm.generate` / `agent.llm.stream` spans under tracer `gridctl.agent.llm` carrying `gen_ai.*` attributes; spans attach to existing `mcp.routing` parent spans when invoked from a tool-call path.
- **Pricing** (`pkg/pricing/`): every LLM call records cost via `pricing.CalculateBreakdown(model, Usage)` - at the playground service site (`internal/api/playground.go`) for the chat path, and inside `pkg/agent/llm/observed.Provider` for any agent-runtime call site that wraps its `ChatModel`.
- **Metrics** (`pkg/metrics/`): cost is recorded via `Accumulator.RecordCost(serverName, replicaID, breakdown)` directly with synthetic server names (`llm:anthropic`, `llm:openai`, `llm:google`, `llm:unknown`) - never via MCP envelope spoofing.
- **Optimize** (`pkg/optimize/`): three agent-runtime heuristics - `unbounded_loop`, `oversized_prompt`, `untyped_handoff` - read aggregated `RunStat` records derived from `pkg/agent/persist/` and surface in `gridctl optimize` output when their thresholds fire.
- **Global event bus** (`pkg/agent/persist/bus.go`): every JSONL event also fans out to an in-process bus so SSE consumers (`GET /api/agent/runs/events/stream`, the Web UI's global runs stream) see live updates across **every** active run without subscribing per-run. Per-run SSE remains the source of truth for one run's timeline; the global stream is the cross-run observability surface.

## Stack YAML Schema

Gridctl supports two network modes:
- **Simple mode** (default): Single network, all containers join automatically
- **Advanced mode**: Multiple networks with explicit container assignment

### Simple Mode (Single Network)

```yaml
version: "1"
name: my-stack

gateway:                              # Optional: gateway-level configuration
  allowed_origins:                    # CORS origins (defaults to ["*"])
    - "http://localhost:3000"
  auth:                               # Gateway authentication
    type: bearer                      # "bearer" or "api_key"
    token: "${GATEWAY_TOKEN}"         # Expected token (supports env var expansion)
    # header: "X-API-Key"            # Custom header for api_key type
  code_mode: "on"                     # Replace tools with search + execute meta-tools ("off" | "on")
  code_mode_timeout: 30               # Code execution timeout in seconds (default: 30)
  output_format: toon                 # Default output format for tool results: "json" (default), "toon", "csv", "text"
  security:                           # Optional: security settings
    schema_pinning:
      enabled: true                   # Enable TOFU schema pinning (default: true)
      action: warn                    # "warn" (log and continue) or "block" (reject tool calls on drift)

secrets:                              # Optional: auto-inject vault secrets by set
  sets:                               # Variable sets to inject into all container env
    - production                      # Secrets in this set added to env (explicit values take precedence)

network:                              # Optional: single network
  name: my-network                    # Defaults to {name}-net
  driver: bridge                      # Defaults to bridge

mcp-servers:
  # HTTP transport (default) - for MCP servers with HTTP endpoints
  - name: http-server
    image: ghcr.io/org/mcp-server:latest
    port: 3000                        # Required for HTTP transport
    transport: http                   # Optional, default is "http"
    env:
      API_KEY: "${ENV_VAR}"           # Environment variable expansion
      SECRET: "${vault:MY_SECRET}"    # Vault secret reference (fails if missing)

  # Stdio transport - for MCP servers using stdin/stdout
  - name: stdio-server
    image: ghcr.io/org/stdio-mcp:latest
    transport: stdio                  # Uses Docker attach for stdin/stdout
    output_format: csv                # Per-server output format override (overrides gateway default)
    pin_schemas: false                # Disable schema pinning for this server (default: inherits gateway setting)
    # port not required for stdio transport

  # External MCP server (no container, connects to existing URL)
  - name: external-api
    url: https://api.example.com/mcp  # External URL (no image/source)
    transport: http                   # "http" or "sse"

  # Local process MCP server (no container, runs on host)
  - name: local-tools
    command: ["./my-mcp-server"]      # Command to run (relative to stack dir)
    # transport: stdio                # Implicit for local process
    env:
      LOG_LEVEL: debug                # Environment vars merged with host env

  # SSH MCP server (connects to remote host via SSH)
  - name: remote-tools
    command: ["/opt/mcp/server"]      # Command to run on remote host
    ssh:
      host: "192.168.1.50"            # SSH hostname or IP
      user: "mcp"                     # SSH username
      # port: 22                      # Optional, defaults to 22
      # identityFile: "~/.ssh/id_ed25519"  # Optional, uses SSH agent by default

  # Build from source
  - name: custom-server
    source:
      type: git                       # or "local"
      url: https://github.com/org/repo
      ref: main                       # branch, tag, or commit
      dockerfile: Dockerfile          # optional
    port: 3000
    build_args:
      DEBUG: "true"

  # OpenAPI-backed MCP server (transforms OpenAPI spec to MCP tools)
  - name: api-server
    openapi:
      spec: https://api.example.com/openapi.json  # URL or local file path
      baseUrl: https://api.example.com            # Override server URL from spec
      auth:
        type: bearer                              # "bearer" or "header"
        tokenEnv: API_TOKEN                       # Env var containing bearer token
        # For header auth: header: "X-API-Key", valueEnv: "API_KEY"
      operations:
        include: ["getUser", "listItems"]         # Whitelist operation IDs
        # exclude: ["deleteUser"]                 # Or blacklist operation IDs

resources:                            # Non-MCP containers (databases, etc.)
  - name: postgres
    image: postgres:16
    env:
      POSTGRES_PASSWORD: secret
```

### Tool Filtering

Use the `tools` field on MCP servers to whitelist which tools are exposed system-wide - unauthorized tools never enter the gateway:

```yaml
mcp-servers:
  - name: github
    image: ghcr.io/github/github-mcp-server:latest
    transport: stdio
    tools: ["get_file_contents", "search_code", "list_commits", "get_issue"]
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_PERSONAL_ACCESS_TOKEN}"
```

### Advanced Mode (Multiple Networks)

Use `networks` (plural) to define multiple isolated networks. Each container must specify which network to join.

```yaml
version: "1"
name: isolated-stack

networks:                             # Multiple networks (advanced mode)
  - name: public-net
    driver: bridge
  - name: private-net
    driver: bridge

mcp-servers:
  - name: frontend-agent
    image: ghcr.io/org/frontend-mcp:latest
    port: 3000
    network: public-net               # Required in advanced mode

  - name: backend-agent
    image: ghcr.io/org/backend-mcp:latest
    port: 3001
    network: private-net

resources:
  - name: postgres
    image: postgres:16
    network: private-net              # Required in advanced mode
    env:
      POSTGRES_PASSWORD: secret
```

### Network Mode Rules

| `network` (singular) | `networks` (plural) | Behavior |
|---------------------|---------------------|----------|
| Not set | Not set | Creates `{name}-net`, all containers join |
| Set | Not set | Creates specified network, all containers join |
| Not set | Set | Creates all networks, containers MUST specify `network` |
| Set | Set | **Error**: cannot use both |

In simple mode, the `network` field on individual containers is ignored.

## Skills

Skills are the gateway's primary extension surface. The legacy declarative YAML `workflow:` block was removed in PR #581 and is formally disowned by Constitution Article IX. Today there are **three flavors**, all under `~/.gridctl/registry/skills/<name>/`; the registry walker (`pkg/registry/store.go`) classifies them by **file presence** - there is no `kind:` field in the frontmatter, and we do not plan to add one (the frontmatter stays agentskills.io-compliant).

| Flavor | Required files | Surfaces as | Runtime |
|---|---|---|---|
| **Prompt-only** (data layer) | `SKILL.md` | MCP prompt **and** MCP tool envelope | None - the markdown body is the artifact |
| **Code (TypeScript)** (logic layer) | `SKILL.md` + `skill.ts` + `agent.json` | MCP tool (typed input/output) | In-process `goja` runtime + `esbuild` transpile (`pkg/agent/sandbox/`) |
| **Code (Go)** (logic layer) | `SKILL.md` + `skill.go` + `skill_test.go` | MCP tool (typed input/output) | Go plugin (`go build -buildmode=plugin`); loaded by `pkg/controller/go_plugins.go` |

`state: active` skills are dispatchable; `acceptance_criteria` (`GIVEN ... WHEN ... THEN ...`) drive `gridctl test <skill>`. Code skills must define `acceptance_criteria` before `gridctl activate` will promote them.

The same upstream MCP client cannot tell the three flavors apart at call time - that is the invariant `docs/skills.md` protects.

### Prompt-only skills (data layer)

Reach for prompt-only when the skill is **prose** - a severity matrix the model applies verbatim, a runbook surfaced to upstream clients (Claude Desktop, IDE, CLI). Scaffold with `gridctl agent init --prompt-only`; edit `SKILL.md`; the next registry refresh picks up the change. No build, no restart of anything except the registry refresh.

### Code skills - TypeScript (sandboxed via goja + esbuild)

```typescript
// skill.ts - default-export an async function
import { llm, skill, tool } from "@gridctl/agent";

export default async function (input: { topic: string }): Promise<{ summary: string }> {
  const research = await tool("github__list_issues", { repo: input.topic });
  const reply = await llm({
    model: "claude-sonnet-4-6",
    system: skill.body,  // hybrid pattern: SKILL.md body drives the system prompt
    messages: [{ role: "user", content: `Summarise: ${research}` }],
  });
  return { summary: reply.content ?? "" };
}
```

Sandbox bindings (installed in `pkg/agent/sandbox/bindings.go`):

| Binding | Purpose |
|---|---|
| `tool(name, args)` | Invoke any allowed MCP tool through the gateway (whitelisting + tracing + cost recording all apply). Accepts both prefixed (`server__tool`) and unprefixed names; unprefixed resolves against `AllowedTools` if exactly one suffix matches. |
| `llm(req)` | Issue an `agent.ChatRequest` via the wired `ChatModel`. Wrap the wired model in `pkg/agent/llm/observed/` to record cost. |
| `parallel(items, fn)` | Concurrent map with a soft cap of `DefaultMaxParallel = 4`. The orchestrator's `HardMaxParallel = 4` is the structural ceiling. |
| `handoff(name, input)` | Dispatch another skill through the same `SkillCaller` adapter the gateway exposes. Local execution and remote (over-MCP) execution share one code path. |
| `approval(prompt)` | Suspend the run on `pkg/agent/compose.Gate` until a CLI / web / MCP consumer responds. Auto-approves with `automatic: true` when no `Approver` is wired. |
| `skill.body`, `skill.name` | Read-only globals that mirror Go's `ctx.SkillBody()` / `ctx.SkillName()`. Drives the hybrid pattern from TS. |

### Code skills - Go (typed via the Skill SDK)

The typed runner signature is `func(ctx skill.RunContext, input I) (O, error)`. `RunContext` embeds `context.Context` and adds `SkillBody() string` plus `SkillName() string`; both are captured at `Define` time so reads are a single pointer chase with no per-call I/O.

```go
package main

import "github.com/gridctl/gridctl/pkg/agent/skill"

type Input  struct { Topic string `json:"topic" jsonschema:"required"` }
type Output struct { Summary string `json:"summary"` }

func run(ctx skill.RunContext, in Input) (Output, error) {
    // tool() and llm() equivalents are first-class Go calls into the orchestrator/provider.
    return Output{Summary: "..."}, nil
}

// New is the constructor the plugin entry point hands to the Registry.
func New() *skill.Definition {
    return skill.MustDefine[Input, Output]("research", "Research a topic", "", run)
}

// RegisterSkill is the fixed plugin entry point the gateway looks up
// via plugin.Lookup after plugin.Open. Missing or mis-typed → load skipped.
func RegisterSkill(reg *skill.Registry) error { return reg.Register(New()) }

func main() {} // satisfies `go build` for compile-checks; never invoked
```

`skill.Define[I, O](name, description, body, run)` infers the input JSON Schema from `I`'s `jsonschema` struct tags at registration time and returns a `*Definition` the registry lifts into an `mcp.Tool` envelope. `MustDefine` is the panicking variant for package-init code. The fourth argument - the post-frontmatter `SKILL.md` body - is positional; pass `""` for programmatic registrations (tests, fixtures). The gateway-builder plugin loader re-decorates each loaded `Definition` with the on-disk body via `skill.WithSkillBody(ctx, body)` so authors writing `New()` with `""` still see the on-disk body through `ctx.SkillBody()`.

### The hybrid pattern

A code skill can read its own `SKILL.md` body and feed it to an LLM as the system prompt. Code drives the graph; prose drives the behavior. Edit the markdown, change runtime behavior, no code change.

```go
func run(ctx skill.RunContext, in TriageInput) (TriageOutput, error) {
    req := agent.ChatRequest{
        Model:    "claude-sonnet-4-6",
        System:   ctx.SkillBody(),       // the post-frontmatter markdown
        Messages: []agent.Message{ /* ... */ },
    }
    // ...
}
```

`examples/registry/items/incident-triage-hybrid/` is the byte-checked reference; `skill_test.go` asserts `buildRequest(...).System` equals the on-disk body verbatim. The TS counterpart is `examples/registry/items/triage-ts/`.

### Operational sharp edges (Go plugins)

The Go plugin path has real operational constraints - kept on `docs/skills.md` for the operator debugging a `plugin.Open` failure:

- **Host/plugin Go version must match.** The daemon's `runtime.Version()` and the building toolchain version are recorded in `dist/manifest.json` as `go_version` plus `go_mod_hash` (sha256 of the resolved `go.mod`). At gateway start `pkg/controller/go_plugins.go` reads the manifest **before** calling `plugin.Open` and skips with an actionable warn on mismatch.
- **Plugins cannot be unloaded.** `plugin.Open` is one-way. Hot-reload only refreshes the TS path; Go plugins refresh on daemon start. Don't try to "hot reload" Go skills.
- **Linux and macOS only.** Windows daemons walk past Go-handler skills via a stub loader; `gridctl agent build` for a Go skill on Windows returns a pre-flight error before invoking the toolchain.
- **`RegisterSkill` is the contract.** `plugin.Lookup("RegisterSkill")` must return `func(*skill.Registry) error`. `gridctl agent validate <skill>` parses `skill.go` via `go/parser` and catches the missing-symbol case before a `go build` round-trip.
- **One broken plugin does not block gateway start.** Per-skill load failures log at warn and the loop continues; the skill surfaces as a missing tool at call time.

### Multi-agent orchestration

`pkg/agent/orchestrator.New[State](caller, initial)` returns an `Orchestrator[State]` whose State is mutable only through `Apply(func(*State) error)` and readable only through `Snapshot()` (deep copy via JSON round-trip). `Handoff[State, Out](ctx, o, call)` and `ParallelHandoff[State, Out](ctx, o, calls)` dispatch subagents through `agent.ToolCaller` - the same surface `*skill.Registry` and the gateway adapter both satisfy. Subagents never receive `*State`; single-writer enforcement is structural. Parallel handoffs are clamped at `HardMaxParallel = 4`; requests above the ceiling clamp with a logged warning.

### Run persistence and time-travel resume

Every run writes a JSONL ledger to `~/.gridctl/runs/<run_id>.jsonl` (`pkg/agent/persist/`). Event types: `run_started`, `run_completed`, `node_enter`, `node_exit`, `tool_call`, `tool_result`, `llm_call`, `llm_chunk`, `structured_output`, `approval_request`, `approval_response`, `error`. `gridctl runs resume <run_id> [--from-step <node_id>]` rebuilds state from the ledger and continues execution. A global SSE stream (`GET /api/agent/runs/events/stream`) fans out every event across active runs to the Web UI's Runs workspace and BottomPanel Runs tab so the live grid stays in sync across workspaces.

### CLI surface

| Command | Purpose |
|---|---|
| `gridctl agent init [DIR]` | Scaffold a starter skill. `--lang ts` (default) or `--lang go`; `--prompt-only` is mutually exclusive with `--lang`. Idempotent unless `--force`. |
| `gridctl agent dev [--root <dir>] [--port 8181]` | IDE dev server: AST graphs, file-watcher SSE, click-to-`$EDITOR` jumps, trace overlay. Read-only (code is canon). |
| `gridctl agent validate <skill>` | Static check: SKILL.md state, TS transpile via esbuild, Go `RegisterSkill` symbol via `go/parser` (no toolchain invocation). |
| `gridctl agent build <skill>` | Compile + write `dist/manifest.json`. TS: esbuild → `skill.js`. Go: `go build -buildmode=plugin` → `skill.so` + manifest guardrail fields (`go_version`, `go_mod_hash`). |
| `gridctl run <skill> [--input @file.json \| - \| '<json>'] [--format json] [--quiet] [--run-id <id>]` | Execute a TS skill in-process and stream typed events; records to `~/.gridctl/runs/<run_id>.jsonl`. Go-handler skills require daemon registration via the gateway-builder plugin loader. |
| `gridctl runs list [--limit N]` | Recent runs (table or `--format json`). |
| `gridctl runs inspect <run_id>` | Typed event timeline. |
| `gridctl runs trace <run_id>` | OTel-shaped JSON projection of the JSONL ledger. |
| `gridctl runs resume <run_id> [--from-step <node_id>]` | Time-travel resume from the last checkpoint. |
| `gridctl runs approve <run_id> [--decision approve\|reject] [--reason <text>]` | Resolve a pending approval gate (calls `POST /api/agent/runs/<id>/approve`). |

### Reference

- `pkg/agent/skill/skill.go` - `Definition`, `Registry`, `Invoker`, the two-layer (runtime-facing / author-facing) mental model.
- `pkg/agent/skill/typed.go` - `RunContext`, `TypedRunner[I, O]`, `Define[I, O]`, `MustDefine`, `WithSkillBody`.
- `pkg/agent/sandbox/bindings.go` - `tool()`, `llm()`, `parallel()`, `handoff()`, `approval()`, and the `skill` global.
- `pkg/agent/sandbox/sandbox.go` - `Bindings`, single-shot event loop, `DefaultTimeout`, `MaxSourceSize`.
- `pkg/controller/go_plugins.go` - `loadGoSkillPlugins`: manifest-guardrail-then-`plugin.Open`; called from `gateway_builder.go:Build()`.
- `pkg/controller/gateway_builder.go` - `makeDispatcherBindings` (per-call body resolution for the TS sandbox path).
- `examples/registry/items/triage-ts/`, `triage-go/`, `incident-triage-hybrid/` - the three reference skills.
- `docs/skills.md` - the canonical three-flavor reference, including all operational sharp edges.

## Code Conventions

### Go

- Use standard library when possible
- Error handling: return errors, don't panic
- Logging: use `log/slog` with `SetLogger()` pattern (silent by default, enable via CLI flags)
- Context: pass context.Context for cancellation
- Testing: table-driven tests preferred
- Interfaces: define interfaces for external dependencies (like Docker) to enable mocking
- Documentation: all exported functions, types, and methods MUST have a godoc comment

### TypeScript/React

- Functional components with hooks
- State management: Zustand
- Data fetching: Custom polling hooks with fetch API
- Styling: Tailwind CSS
- **UI Design Guidelines:** See [web/AGENTS.md](web/AGENTS.md) for the "Obsidian Observatory" design system, color palette, and component patterns.

### Commit Messages

Format: `<type>: <subject>`

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `perf`

### Labels for Docker Resources

All managed resources use these labels:
- `gridctl.managed=true`
- `gridctl.stack={name}`
- `gridctl.mcp-server={name}` (for MCP server containers)
- `gridctl.resource={name}` (for resource containers)

## Testing Requirements

### Required for All PRs

1. **Unit Tests**: New exported functions must have tests
2. **Coverage**: Maintain existing coverage levels
3. **Naming**: Use `TestFunctionName_Scenario` pattern
4. **Table-Driven**: Preferred for multiple test cases

### Test Locations

Tests follow `*_test.go` convention adjacent to source files. Integration tests in `tests/integration/` require build tag `integration`.

### Mocks

Generated mocks (via `go.uber.org/mock/mockgen`, regenerate with `make generate`):
- `MockAgentClient`: pkg/mcp/mock_agent_client_test.go (generated from `AgentClient` interface)
- `MockWorkloadRuntime`: pkg/runtime/mock_runtime_test.go (generated from `WorkloadRuntime` interface)

Hand-rolled mocks (state-based fakes):
- `MockDockerClient`: pkg/runtime/docker/mock_test.go (fake with state, error injection, call tracking)
- HTTP handlers: use `net/http/httptest`

### Test Cleanup

Tests MUST leave the system in a clean state. Use `t.Cleanup()` or `defer` to remove any files, containers, or resources created during a test. A test that leaves behind state can cause non-deterministic failures in subsequent runs.

### Running Tests

```bash
make test                                    # Unit tests
make test-coverage                           # With coverage report
go test -tags=integration ./tests/integration/...  # Integration tests
```

## Important Notes

See [CONSTITUTION.md](CONSTITUTION.md) for the immutable governance articles that apply to all contributions and cannot be overridden by any feature, prompt, or contributor preference.

- Never commit API keys or secrets
- Keep dependencies minimal
- Prefer simple solutions over clever ones
