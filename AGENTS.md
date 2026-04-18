# Gridctl Development Guide

## Project Overview

Gridctl is an MCP (Model Context Protocol) orchestration tool — "Containerlab for MCP Infrastructure".

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
│   │   ├── git.go        # Git clone/update
│   │   ├── docker.go     # Docker build
│   │   └── builder.go    # Main builder
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
│   │   ├── config.go     # Skill source configuration
│   │   ├── fingerprint.go # Content fingerprinting for change detection
│   │   ├── importer.go   # Remote skill import (git clone/pull)
│   │   ├── lockfile.go   # Lock file for pinned skill refs
│   │   ├── origin.go     # Origin tracking per skill
│   │   ├── remote.go     # Remote git operations
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
│   └── registry/         # Agent Skills registry (agentskills.io)
│       ├── types.go      # AgentSkill, SkillFile, ItemState, workflow types
│       ├── frontmatter.go # SKILL.md parsing (YAML frontmatter + markdown body)
│       ├── validator.go   # agentskills.io spec validation + workflow validation
│       ├── store.go      # Directory-based persistent store
│       ├── server.go     # MCP server interface for registry
│       ├── dag.go        # Workflow DAG builder (Kahn's algorithm, level grouping)
│       ├── template.go   # Template engine for workflow expressions
│       └── executor.go   # Workflow executor with parallel step dispatch
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
        ├── skills_executor_test.go   # Skill execution workflows (DAGs, timeouts)
        ├── openapi_test.go           # OpenAPI spec parsing + auth
        ├── replica_kill_one_test.go       # Kill one replica, verify exclusion + survivors
        ├── replica_all_down_test.go       # All replicas down → structured error
        ├── replica_restart_storm_test.go  # Backoff prevents reconnect spin
        ├── replica_stackless_mode_test.go # Multi-replica register in stackless mode
        └── replica_mixed_counts_test.go   # 1-replica + 3-replica server in one gateway
```

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

Validates the stack spec including config schema, transport rules, and workflow definitions. Exit codes: `0` valid, `1` validation error, `2` infrastructure error.

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

Starts the API server and web UI in stackless mode — no stack file required. Stack-dependent endpoints return 503 until a stack is loaded via the wizard. Vault and wizard endpoints are always available.

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

#### `gridctl test <skill-name>`

Run acceptance criteria for a skill against the running gateway.

| Flag | Short | Description |
|------|-------|-------------|
| `--stack` | `-s` | Stack to test against (auto-detect if only one running) |

Exit codes: `0` all criteria passed, `1` one or more criteria failed, `2` infrastructure error (gateway unreachable, skill not found).

Acceptance criteria in `SKILL.md` frontmatter must follow the `GIVEN <context> WHEN <action> THEN <assertion>` format. Criteria that don't match the pattern are skipped.

#### `gridctl activate <skill-name>`

Transition a skill from draft to active state. Executable skills (those with a `workflow` block) must have `acceptance_criteria` defined before activation is permitted.

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
- **MCP:** `POST /mcp` (JSON-RPC), `GET /sse` + `POST /message` (SSE for Claude Desktop)
- **API:** `/api/status`, `/api/mcp-servers`, `/api/mcp-servers/{name}/restart`, `/api/tools`, `/api/logs`, `/api/clients`, `/api/reload`, `/api/metrics/tokens`, `/health`, `/ready`
- **Stack Library:** `/api/stacks` (list/save), `/api/stack/initialize` (cold-load a saved stack into a stackless daemon)
- **Vault:** `/api/vault`, `/api/vault/status`, `/api/vault/unlock`, `/api/vault/lock`, `/api/vault/sets`, `/api/vault/import`
- **Pins:** `/api/pins` (list all), `/api/pins/{server}` (get), `/api/pins/{server}/approve` (POST), `/api/pins/{server}` (DELETE)
- **Registry:** `/api/registry/status`, `/api/registry/skills[/{name}]`, `/api/registry/skills/{name}/files[/{path}]`, `/api/registry/skills/validate`, `/api/registry/skills/{name}/workflow`, `/api/registry/skills/{name}/execute`, `/api/registry/skills/{name}/validate-workflow`, `/api/registry/skills/{name}/test`
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

**Workflow API:**
- `GET /api/registry/skills/{name}/workflow` - Get parsed workflow definition with DAG levels
- `POST /api/registry/skills/{name}/execute` - Execute a workflow skill (returns ToolCallResult)
- `POST /api/registry/skills/{name}/validate-workflow` - Dry-run validation without execution

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
  - Response: `TraceRecord` with `spans[]` — each span has `span_id`, `name`, `start_time`, `duration_ms`, `is_error`, `attrs` (OTel attributes)

**Stack Library API:**
- `GET /api/stacks` - List saved stacks from `~/.gridctl/stacks/`; returns `{stacks: [{name, path, size, modTime}]}`
- `POST /api/stacks` - Save a stack spec to the library; body: `{name, content}` (YAML string); returns `{name, path}`
- `POST /api/stack/initialize` - Cold-load a named saved stack into a running stackless daemon; body: `{name}`; returns 409 (`StackAlreadyActiveError`) if a stack is already running

**`/ready` behavior:**
- Returns `200 OK` when a stack is fully initialized
- Returns `503 Service Unavailable` in stackless mode (no stack loaded yet)

**Tool prefixing:** Tools are prefixed with server name to avoid collisions:
- `server-name__tool-name` (e.g., `itential-mcp__get_workflows`)

**Replica sets:** Each registered server is a `ReplicaSet` in the router — a pool of 1..N `AgentClient` replicas sharing one server name and one tool namespace. Set `replicas: N` (and optionally `replica_policy: round-robin | least-connections`) in `mcp-servers[]` to spawn N independent processes. Validation caps at 32 and rejects replicas on `external` / `openapi` transports. Per-replica health monitor pings each replica independently, excludes failures from dispatch, and reconnects with exponential backoff (1s → 30s cap, ±25% jitter, reset on success). When every replica is unhealthy, tool calls fail with `no healthy replicas: <server>`. Every log line and trace span on the tool-call path carries a `replica_id`; `gridctl status --replicas` and `/api/stack/health` expose the per-replica breakdown. See [docs/scaling.md](docs/scaling.md).

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

Use the `tools` field on MCP servers to whitelist which tools are exposed system-wide — unauthorized tools never enter the gateway:

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

## Skill Workflows

Skills with a `workflow` block in their YAML frontmatter become **executable** — they are exposed as MCP tools (not just prompts) and run deterministic multi-step tool orchestration through the gateway.

### Workflow SKILL.md Schema

```yaml
---
name: my-workflow
description: What the workflow does
allowed-tools: server__tool1, server__tool2
state: active

acceptance_criteria:
  - GIVEN a valid input WHEN the skill is called THEN the output contains expected data
  - GIVEN an invalid input WHEN the skill is called THEN an error is returned

inputs:
  param_name:
    type: string          # string | number | boolean | object | array
    description: Help text
    required: true
    default: "fallback"   # Optional default value
    enum: ["a", "b"]      # Optional allowed values

workflow:
  - id: step-one
    tool: server__tool1
    args:
      key: "{{ inputs.param_name }}"
    on_error: fail        # fail | skip | continue (default: fail)

  - id: step-two
    tool: server__tool2
    args:
      data: "{{ steps.step-one.result }}"
    depends_on: step-one
    timeout: "30s"
    retry:
      max_attempts: 3
      backoff: "2s"

output:
  format: merged          # merged | last | custom
  include: [step-one, step-two]
---

# My Workflow

Documentation for the workflow.
```

### Template Expressions

| Expression | Returns | Example |
|---|---|---|
| `{{ inputs.name }}` | Input parameter value | `{{ inputs.device_ip }}` |
| `{{ steps.id.result }}` | Full text result of a step | `{{ steps.fetch.result }}` |
| `{{ steps.id.json.path }}` | JSON path extraction | `{{ steps.api.json.data.name }}` |
| `{{ steps.id.is_error }}` | Boolean error flag | `{{ steps.validate.is_error }}` |

Whole expressions (`"{{ inputs.count }}"`) preserve types. Mixed text (`"Value: {{ inputs.count }}"`) converts to string.

### DAG Execution

Steps are organized into levels using topological sort (Kahn's algorithm). Steps in the same level run concurrently (bounded by `maxParallel`, default 4).

```
depends_on: step-a            # Single dependency
depends_on: [step-a, step-b]  # Multiple (all must complete)
# No depends_on               # Runs in Level 0 (first)
```

### Error Handling

| Policy | Behavior |
|---|---|
| `on_error: fail` | Halt entire workflow (default) |
| `on_error: skip` | Mark step + transitive dependents as skipped |
| `on_error: continue` | Store error, proceed (downstream can check `is_error`) |

### Conditional Execution

```yaml
condition: "{{ steps.validate.json.valid == true }}"
```

Steps with a false condition are skipped along with their dependents.

### Output Formats

| Format | Behavior |
|---|---|
| `merged` | Joins non-error step results with separator (default) |
| `last` | Returns only the last non-skipped step's result |
| `custom` | Uses `template` field with `{{ steps.*.result }}` expressions |

### Executor Limits

| Limit | Default |
|---|---|
| `maxParallel` | 4 concurrent steps per level |
| `maxResultSize` | 1MB per step result |
| `maxDepth` | 10 (nested skill composition) |
| `workflowTimeout` | 5 minutes |

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
