# Gridctl Development Guide

## Project Overview

Gridctl is an MCP (Model Context Protocol) orchestration tool - "Containerlab for AI Agents".

**Architecture:**
- Controller (Go): Reads stack.yaml, manages Docker containers
- Gateway (Go): Protocol bridge that aggregates tools from downstream agents
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
              │   Agent A   │   │   Agent B   │
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
│  (Agent 1)  │     │  (Agent 2)  │
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

Network isolation between agents while allowing communication through the MCP gateway.

## Directory Structure

```
gridctl/
├── cmd/gridctl/           # CLI entry point
│   ├── main.go           # Entry point
│   ├── root.go           # Cobra root command + serve command
│   ├── deploy.go         # Start stack + gateway
│   ├── destroy.go        # Stop containers
│   ├── status.go         # Show container status
│   ├── link.go           # Connect LLM clients to gateway
│   ├── unlink.go         # Remove gridctl from LLM clients
│   ├── reload.go         # Hot reload stack configuration
│   ├── version.go        # Version command
│   ├── help.go           # Custom help template
│   ├── embed.go          # Embedded web assets
│   └── embed_stub.go     # Build stub for embed
├── internal/
│   ├── server/           # Legacy HTTP server (SPA only)
│   └── api/              # API server (MCP + REST + Registry)
│       ├── api.go        # Server setup and route registration
│       ├── auth.go       # Gateway authentication middleware
│       └── registry.go   # Registry CRUD endpoints
├── pkg/
│   ├── adapter/          # Protocol adapters
│   │   └── a2a_client.go # A2A client adapter
│   ├── config/           # Stack YAML parsing
│   │   ├── types.go      # Stack, Agent, Resource structs
│   │   ├── loader.go     # LoadStack() function
│   │   └── validate.go   # Validation rules
│   ├── dockerclient/     # Docker client interface
│   │   └── interface.go  # Interface definition for mocking
│   ├── logging/          # Logging utilities
│   │   ├── discard.go    # Discard logger
│   │   ├── buffer.go     # In-memory circular log buffer for API
│   │   └── structured.go # Structured slog handler with buffering
│   ├── runtime/          # Workload orchestration (runtime-agnostic)
│   │   ├── interface.go  # WorkloadRuntime interface + types
│   │   ├── orchestrator.go # High-level Up/Down/Status
│   │   ├── factory.go    # Runtime factory registration
│   │   ├── compat.go     # Backward compatibility types
│   │   ├── depgraph.go   # Dependency graph for startup ordering
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
│   │   ├── server_registrar.go # MCP server registration
│   │   └── gateway_builder_test.go
│   ├── jsonrpc/          # JSON-RPC 2.0 types
│   │   └── types.go      # Request, Response, Error types
│   ├── provisioner/      # LLM client provisioning (link/unlink)
│   │   ├── claude.go     # Claude Desktop provisioner
│   │   ├── cursor.go     # Cursor provisioner
│   │   ├── windsurf.go   # Windsurf provisioner
│   │   └── ...           # Other client provisioners
│   ├── reload/           # Hot reload support
│   │   ├── reload.go     # Reload handler and result types
│   │   ├── diff.go       # Stack diff computation
│   │   └── watcher.go    # File watcher for --watch mode
│   ├── state/            # Daemon state management
│   │   └── state.go      # ~/.gridctl/state/ and ~/.gridctl/logs/
│   ├── mcp/              # MCP protocol
│   │   ├── types.go      # JSON-RPC, MCP types, AgentClient interface
│   │   ├── client.go     # HTTP transport client
│   │   ├── stdio.go      # Stdio transport client (Docker attach)
│   │   ├── process.go    # Local process transport client (host process)
│   │   ├── openapi_client.go # OpenAPI-backed MCP client
│   │   ├── sse.go        # SSE server (northbound)
│   │   ├── session.go    # Session management
│   │   ├── router.go     # Tool routing
│   │   ├── gateway.go    # Protocol bridge logic
│   │   ├── handler.go    # HTTP handlers
│   │   └── expand.go     # Environment variable expansion
│   ├── a2a/              # A2A (Agent-to-Agent) protocol
│   │   ├── types.go      # A2A protocol types (AgentCard, Task, Message)
│   │   ├── client.go     # HTTP client for remote A2A agents
│   │   ├── handler.go    # HTTP handler for A2A endpoints
│   │   └── gateway.go    # A2A gateway (local + remote agents)
│   └── registry/         # MCP registry (prompts + skills)
│       ├── types.go      # Prompt, Skill, validation types
│       ├── store.go      # File-based persistent store
│       ├── executor.go   # Skill execution engine
│       └── server.go     # MCP server interface for registry
├── web/                  # React frontend (Vite)
├── examples/             # Example topologies
│   ├── getting-started/  # Basic examples
│   ├── transports/       # Transport-specific examples
│   ├── access-control/   # Tool filtering and security examples
│   ├── gateways/         # Gateway configuration examples
│   ├── multi-agent/      # Multi-agent orchestration examples
│   ├── platforms/        # Platform-specific examples
│   └── _mock-servers/    # Mock MCP servers for testing
└── tests/
    └── integration/      # Integration tests (build tag: integration)
        ├── orchestrator_test.go
        ├── runtime_test.go
        └── openapi_test.go
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
# Start a stack (runs as daemon, returns immediately)
./gridctl deploy examples/getting-started/agent-basic.yaml

# Start with options
./gridctl deploy stack.yaml --port 8180 --no-cache

# Run in foreground with verbose output (for debugging)
./gridctl deploy stack.yaml --foreground

# Watch for changes and auto-reload
./gridctl deploy stack.yaml --watch

# Deploy and auto-link all detected LLM clients
./gridctl deploy stack.yaml --flash

# Check running gateways and containers
./gridctl status

# Connect an LLM client to the gateway
./gridctl link

# Remove gridctl from an LLM client
./gridctl unlink

# Hot reload a running stack
./gridctl reload

# Stop a specific stack (gateway + containers)
./gridctl destroy examples/getting-started/agent-basic.yaml
```

### Command Reference

#### `gridctl deploy <stack.yaml>`

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
| `--flash` | | Auto-link detected LLM clients after deploy |
| `--no-expand` | | Disable environment variable expansion in OpenAPI spec files |

#### `gridctl destroy <stack.yaml>`

Stops the gateway daemon and removes all containers for a stack.

#### `gridctl status`

Shows running gateways and containers.

| Flag | Short | Description |
|------|-------|-------------|
| `--stack` | `-s` | Filter by stack name |

#### `gridctl link [client]`

Connect an LLM client to the gridctl gateway. Without arguments, detects installed clients and presents a selection list.

Supported clients: claude, claude-code, cursor, windsurf, vscode, gemini, continue, cline, anythingllm, roo, zed, goose

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

Starts the web UI server without managing any stack. Listens on port 8180 by default (override with `PORT` environment variable).

### Daemon Mode

By default, `gridctl deploy` runs the MCP gateway as a background daemon:
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
└── cache/              # Build cache
    └── ...             # Git repos, Docker contexts
```

## MCP Gateway

When `gridctl deploy` runs, it:
1. Parses the stack YAML
2. Creates Docker network
3. Builds/pulls images
4. Starts containers with host port bindings (9000+)
5. Registers agents with the MCP gateway
6. Starts HTTP server with MCP endpoint

**Endpoints:**
- **MCP:** `POST /mcp` (JSON-RPC), `GET /sse` + `POST /message` (SSE for Claude Desktop)
- **API:** `/api/status`, `/api/mcp-servers`, `/api/tools`, `/api/logs`, `/api/clients`, `/api/reload`, `/health`, `/ready`
- **Agents:** `/api/agents/{name}/logs`, `/api/agents/{name}/restart`, `/api/agents/{name}/stop`
- **A2A:** `/.well-known/agent.json`, `/a2a/` (list agents), `/a2a/{agent}` (GET card, POST JSON-RPC)
- **Registry:** `/api/registry/status`, `/api/registry/prompts[/{name}]`, `/api/registry/skills[/{name}]`
- **Web UI:** `GET /`

**Logs API:**
- `GET /api/logs` - Returns structured gateway logs as JSON array
- Query params: `lines` (default 100), `level` (filter by log level)
- Response: `LogEntry[]` with fields: `level`, `ts`, `msg`, `component`, `trace_id`, `attrs`

**Clients API:**
- `GET /api/clients` - Returns status of detected LLM clients (name, detected, linked, transport)

**Reload API:**
- `POST /api/reload` - Triggers hot reload of the stack configuration (requires `--watch` flag on deploy)

**Agent Control API:**
- `GET /api/agents/{name}/logs` - Returns container logs for an agent
- `POST /api/agents/{name}/restart` - Restart an agent container
- `POST /api/agents/{name}/stop` - Stop an agent container

**Registry API:**
- `GET /api/registry/status` - Returns prompt and skill counts
- `GET /api/registry/prompts` - List all prompts
- `POST /api/registry/prompts` - Create a prompt
- `GET/PUT/DELETE /api/registry/prompts/{name}` - CRUD for individual prompts
- `POST /api/registry/prompts/{name}/activate` - Activate a prompt
- `POST /api/registry/prompts/{name}/disable` - Disable a prompt
- `GET /api/registry/skills` - List all skills
- `POST /api/registry/skills` - Create a skill
- `GET/PUT/DELETE /api/registry/skills/{name}` - CRUD for individual skills
- `POST /api/registry/skills/{name}/activate` - Activate a skill
- `POST /api/registry/skills/{name}/disable` - Disable a skill
- `POST /api/registry/skills/{name}/test` - Execute a skill with test arguments

**Tool prefixing:** Tools are prefixed with server name to avoid collisions:
- `server-name__tool-name` (e.g., `itential-mcp__get_workflows`)

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

  # Stdio transport - for MCP servers using stdin/stdout
  - name: stdio-server
    image: ghcr.io/org/stdio-mcp:latest
    transport: stdio                  # Uses Docker attach for stdin/stdout
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

agents:                               # Active agents that consume MCP tools
  - name: my-agent
    image: my-org/agent:latest
    description: "Agent description"
    capabilities:
      - code-analysis
      - automation
    uses:                             # MCP servers this agent can access (alias: equipped_skills)
      - http-server                   # String format: all tools from server
      - server: stdio-server          # Object format: with tool filtering
        tools: ["read", "list"]       # Only these tools (agent-level filtering)
    command: ["python", "agent.py"]   # Optional: override container command
    env:
      MODEL_NAME: "claude-3-5-sonnet"

resources:                            # Non-MCP containers (databases, etc.)
  - name: postgres
    image: postgres:16
    env:
      POSTGRES_PASSWORD: secret
```

### Agents

Agents are active containers that consume tools from MCP servers. They receive:
- `MCP_ENDPOINT` environment variable injected automatically (e.g., `http://host.docker.internal:8180`)
- Tool access control based on their `uses` field (can only access tools from listed servers)

#### Tool Filtering

Gridctl supports two levels of tool filtering for implementing least-privilege access:

1. **Server-Level Filtering**: Use the `tools` field on MCP servers to restrict which tools are exposed system-wide
2. **Agent-Level Filtering**: Use the object format in `uses` to restrict which tools each agent can access

```yaml
mcp-servers:
  - name: file-server
    image: file-mcp:latest
    port: 3000
    tools: ["read", "list"]   # Server-level: only expose these tools to ALL agents

agents:
  - name: code-reviewer
    image: my-org/reviewer:v1
    description: "Reviews PRs and leaves comments"
    capabilities: ["code-analysis", "git-ops"]
    uses:
      - github-tools           # String format: all tools from this server
      - server: file-server    # Object format: agent-level filtering
        tools: ["read"]        # Only "read" tool (even though server exposes "read" and "list")
    command: ["python", "run.py"]
    env:
      MODEL_NAME: "claude-3-5-sonnet"

  # Headless agent - NOT YET IMPLEMENTED (schema validation only)
  # - name: headless-agent
  #   runtime: claude-code       # Uses built-in runtime instead of image
  #   prompt: |
  #     You are a helpful assistant that can use tools.
  #   uses:
  #     - github-tools

  # Agent with A2A capabilities
  - name: a2a-enabled-agent
    image: my-org/agent:v1
    a2a:                        # Exposes this agent via A2A protocol
      enabled: true
      version: "1.0.0"
      skills:
        - id: code-review
          name: Code Review
          description: "Review code for issues"
        - id: summarize
          name: Summarize
          description: "Summarize content"

# External A2A agents (remote agents accessible via URL)
a2a-agents:
  - name: external-agent
    url: https://example.com/agent
    auth:
      type: bearer              # or "api_key"
      token_env: "A2A_TOKEN"    # Environment variable containing the token
      # header_name: "X-API-Key" # for api_key type (default: "Authorization")
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

## Code Conventions

### Go

- Use standard library when possible
- Error handling: return errors, don't panic
- Logging: use `log/slog` with `SetLogger()` pattern (silent by default, enable via CLI flags)
- Context: pass context.Context for cancellation
- Testing: table-driven tests preferred
- Interfaces: define interfaces for external dependencies (like Docker) to enable mocking

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
- `gridctl.agent={name}` (for agent containers)
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

### Running Tests

```bash
make test                                    # Unit tests
make test-coverage                           # With coverage report
go test -tags=integration ./tests/integration/...  # Integration tests
```

## Important Notes

- Never commit API keys or secrets
- Keep dependencies minimal
- Prefer simple solutions over clever ones
