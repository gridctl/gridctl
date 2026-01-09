# Agentlab Development Guide

## Project Overview

Agentlab is an MCP (Model Context Protocol) orchestration tool - "Containerlab for AI Agents".

**Architecture:**
- Controller (Go): Reads topology.yaml, manages Docker containers
- Gateway (Go): Protocol bridge that aggregates tools from downstream agents
- UI (React + React Flow): Visualizes topology with real-time status

## Protocol Bridge Architecture

Agentlab's core value is acting as a **Protocol Bridge** between MCP transports:

```
                    ┌─────────────────────┐
                    │    Claude Desktop   │
                    │    (SSE Client)     │
                    └──────────┬──────────┘
                               │ SSE (GET /sse + POST /message)
                               ▼
                    ┌─────────────────────┐
                    │   Agentlab Gateway    │
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

**Southbound (to containers):**
- **Stdio**: Uses Docker container attach for stdin/stdout communication
- **HTTP**: Standard HTTP POST to container's /mcp endpoint

**Northbound (to clients):**
- **SSE**: Server-Sent Events for persistent connections (Claude Desktop)
- **HTTP POST**: Standard JSON-RPC 2.0 to /mcp endpoint

## Architecture Decision: Host Binary

Agentlab is distributed as a **host binary** (not a containerized gateway). This follows the same pattern as Containerlab, Terraform, Kind, and Docker Compose.

### Why Host Binary?

| Feature | Gateway as Container | Gateway as Binary |
|---------|---------------------|-------------------|
| Networking | Hard - must join every network | Easy - routes via Docker socket |
| Filesystem | Complex volume mounts | Native file access |
| Distribution | `docker run ...` | `brew install` / binary download |
| Updates | `docker pull` | `brew upgrade` / `agentlab update` |
| Precedent | Jenkins, Portainer | Containerlab, Docker Compose, Terraform |

### Docker Socket Access

When agentlab runs on the host, it has direct access to the Docker daemon:
- **Stdio Transport**: Uses `ContainerAttach` to pipe directly into container stdin/stdout - networks are irrelevant
- **HTTP Transport**: Containers publish ports to localhost (9000+) - no network joining required

### Multi-Network Routing

As a host binary, agentlab can route traffic between agents on **different Docker networks**:

```
┌─────────────┐     ┌─────────────┐
│  Network A  │     │  Network B  │
│  (Agent 1)  │     │  (Agent 2)  │
└──────┬──────┘     └──────┬──────┘
       │                   │
       │   Docker Socket   │
       └─────────┬─────────┘
                 │
       ┌─────────▼─────────┐
       │   agentlab binary   │
       │   (host machine)  │
       │                   │
       │  Routes JSON-RPC  │
       │  through memory   │
       └─────────┬─────────┘
                 │
       ┌─────────▼─────────┐
       │   localhost:8080  │
       │   (MCP Gateway)   │
       └───────────────────┘
```

This enables network isolation between agents while still allowing them to communicate through the MCP gateway.

### Better Developer Experience

- **File Access**: Reads `topology.yaml`, local source directories, `~/.ssh/` natively
- **Localhost**: Web UI at `localhost:8080` without port mapping complexity
- **Debugging**: Standard Go debugging tools work directly

## Implementation Status

| Phase | Description | Status |
|-------|-------------|--------|
| Phase 0 | Project skeleton, embedded React UI | Complete |
| Phase 1 | Topology parser, Cobra CLI | Complete |
| Phase 2 | Docker container orchestration | Complete |
| Phase 3 | Git-to-image builder | Complete |
| Phase 4 | MCP Gateway (aggregator) | Complete |
| Phase 5 | React Flow UI | Complete |
| Phase 6 | Release & packaging | Not started |

### Agent Platform Phases

| Phase | Description | Status |
|-------|-------------|--------|
| Agent Phase 1 | Agent primitive (config, runtime, API) | Complete |
| Agent Phase 2 | Sidecar injector (MCP_ENDPOINT, tool access control) | Complete |
| Agent Phase 3 | Headless agent schema (runtime, prompt fields) | Complete |
| Agent Phase 4 | Agent cards UI (circular nodes, purple theme) | Complete |
| A2A Phase 1 | A2A protocol types and client | Complete |
| A2A Phase 2 | A2A handler and gateway | Complete |
| A2A Phase 3 | Unified agent nodes (merged container + A2A status) | Complete |

## Directory Structure

```
agentlab/
├── cmd/agentlab/           # CLI entry point
│   ├── main.go           # Entry point
│   ├── root.go           # Cobra root command
│   ├── deploy.go         # Start topology + gateway
│   ├── destroy.go        # Stop containers
│   ├── status.go         # Show container status
│   └── embed.go          # Embedded web assets
├── internal/
│   ├── server/           # Legacy HTTP server
│   └── api/              # API server (MCP + REST)
├── pkg/
│   ├── config/           # Topology YAML parsing
│   │   ├── types.go      # Topology, Agent, Resource structs
│   │   ├── loader.go     # LoadTopology() function
│   │   └── validate.go   # Validation rules
│   ├── dockerclient/     # Docker client interface
│   │   └── interface.go  # Interface definition for mocking
│   ├── runtime/          # Docker orchestration
│   │   ├── client.go     # Docker client
│   │   ├── network.go    # Network management
│   │   ├── container.go  # Container lifecycle
│   │   ├── image.go      # Image pulling
│   │   ├── labels.go     # Container naming/labels
│   │   └── runtime.go    # High-level Up/Down
│   ├── builder/          # Image building
│   │   ├── types.go      # BuildOptions, BuildResult
│   │   ├── cache.go      # ~/.agentlab/cache management
│   │   ├── git.go        # Git clone/update
│   │   ├── docker.go     # Docker build
│   │   └── builder.go    # Main builder
│   ├── state/            # Daemon state management
│   │   └── state.go      # ~/.agentlab/state/ and ~/.agentlab/logs/
│   ├── mcp/              # MCP protocol
│   │   ├── types.go      # JSON-RPC, MCP types, AgentClient interface
│   │   ├── client.go     # HTTP transport client
│   │   ├── stdio.go      # Stdio transport client (Docker attach)
│   │   ├── sse.go        # SSE server (northbound)
│   │   ├── session.go    # Session management
│   │   ├── router.go     # Tool routing
│   │   ├── gateway.go    # Protocol bridge logic
│   │   └── handler.go    # HTTP handlers
│   └── a2a/              # A2A (Agent-to-Agent) protocol
│       ├── types.go      # A2A protocol types (AgentCard, Task, Message)
│       ├── client.go     # HTTP client for remote A2A agents
│       ├── handler.go    # HTTP handler for A2A endpoints
│       └── gateway.go    # A2A gateway (local + remote agents)
└── web/                  # React frontend (Vite)
```

## Build Commands

```bash
make build      # Build frontend and backend
make build-web  # Build React frontend only
make build-go   # Build Go binary only
make dev        # Run Vite dev server
make clean      # Remove build artifacts
make run        # Build and run
```

## CLI Usage

```bash
# Start a topology (runs as daemon, returns immediately)
./agentlab deploy examples/mcp-test.yaml

# Start with options
./agentlab deploy topology.yaml --port 8080 --no-cache

# Run in foreground with verbose output (for debugging)
./agentlab deploy topology.yaml --foreground

# Check running gateways and containers
./agentlab status

# Stop a specific topology (gateway + containers)
./agentlab destroy examples/mcp-test.yaml
```

### Command Reference

#### `agentlab deploy <topology.yaml>`

Starts containers and MCP gateway for a topology.

| Flag | Short | Description |
|------|-------|-------------|
| `--foreground` | `-f` | Run in foreground with verbose output (don't daemonize) |
| `--port` | `-p` | Port for MCP gateway (default: 8080) |
| `--no-cache` | | Force rebuild of source-based images |
| `--verbose` | `-v` | Print full topology as JSON |

#### `agentlab destroy <topology.yaml>`

Stops the gateway daemon and removes all containers for a topology.

#### `agentlab status`

Shows running gateways and containers.

| Flag | Short | Description |
|------|-------|-------------|
| `--topology` | `-t` | Filter by topology name |

### Daemon Mode

By default, `agentlab deploy` runs the MCP gateway as a background daemon:
- Returns immediately after starting
- State stored in `~/.agentlab/state/{name}.json`
- Logs written to `~/.agentlab/logs/{name}.log`
- Use `--foreground` (-f) to run interactively with verbose output

### State Files

Agentlab stores daemon state in `~/.agentlab/`:

```
~/.agentlab/
├── state/              # Daemon state files
│   └── {name}.json     # PID, port, start time per topology
├── logs/               # Daemon log files
│   └── {name}.log      # stdout/stderr from daemon
└── cache/              # Build cache
    └── ...             # Git repos, Docker contexts
```

## MCP Gateway

When `agentlab deploy` runs, it:
1. Parses the topology YAML
2. Creates Docker network
3. Builds/pulls images
4. Starts containers with host port bindings (9000+)
5. Registers agents with the MCP gateway
6. Starts HTTP server with MCP endpoint

**MCP Endpoints:**
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/mcp` | POST | JSON-RPC 2.0 (initialize, tools/list, tools/call) |
| `/sse` | GET | SSE connection endpoint (for Claude Desktop) |
| `/message` | POST | Message endpoint for SSE sessions |

**API Endpoints:**
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/status` | GET | Gateway + agent status (unified agents with A2A info) |
| `/api/mcp-servers` | GET | List registered MCP servers |
| `/api/tools` | GET | List aggregated tools |
| `/` | GET | Web UI (embedded React app) |

**A2A Protocol Endpoints:**
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/.well-known/agent.json` | GET | Agent Card discovery (lists all local A2A agents) |
| `/a2a/{agent}` | GET | Get specific agent's Agent Card |
| `/a2a/{agent}` | POST | JSON-RPC endpoint (message/send, tasks/get, etc.) |

**Tool prefixing:** Tools are prefixed with server name to avoid collisions:
- `server-name::tool-name` (e.g., `itential-mcp::get_workflows`)

## Topology YAML Schema

Agentlab supports two network modes:
- **Simple mode** (default): Single network, all containers join automatically
- **Advanced mode**: Multiple networks with explicit container assignment

### Simple Mode (Single Network)

```yaml
version: "1"
name: my-topology

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

agents:                               # Active agents that consume MCP tools
  - name: my-agent
    image: my-org/agent:latest
    description: "Agent description"
    capabilities:
      - code-analysis
      - automation
    uses:                             # MCP servers this agent can access
      - http-server
      - stdio-server
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
- `MCP_ENDPOINT` environment variable injected automatically (e.g., `http://host.docker.internal:8080`)
- Tool access control based on their `uses` field (can only access tools from listed servers)

```yaml
agents:
  - name: code-reviewer
    image: my-org/reviewer:v1
    description: "Reviews PRs and leaves comments"
    capabilities: ["code-analysis", "git-ops"]
    uses:
      - github-tools           # Can access tools from this MCP server
    command: ["python", "run.py"]
    env:
      MODEL_NAME: "claude-3-5-sonnet"

  # Headless agent (schema only, runtime not yet implemented)
  - name: headless-agent
    runtime: claude-code       # Uses built-in runtime instead of image
    prompt: |
      You are a helpful assistant that can use tools.
    uses:
      - github-tools

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
      token: "${A2A_TOKEN}"
      # header: "X-API-Key"     # for api_key type
```

### Advanced Mode (Multiple Networks)

Use `networks` (plural) to define multiple isolated networks. Each container must specify which network to join.

```yaml
version: "1"
name: isolated-topology

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
- Data fetching: React Query
- Styling: Tailwind CSS
- **UI Design Guidelines:** See [web/AGENTS.md](web/AGENTS.md) for the "Obsidian Observatory" design system, color palette, and component patterns.

### Commit Messages

Format: `<type>: <subject>`

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `perf`

### Labels for Docker Resources

All managed resources use these labels:
- `agentlab.managed=true`
- `agentlab.topology={name}`
- `agentlab.mcp-server={name}` (for MCP server containers)
- `agentlab.agent={name}` (for agent containers)
- `agentlab.resource={name}` (for resource containers)

## Testing Requirements

### Required for All PRs

1. **Unit Tests**: New exported functions must have tests
2. **Coverage**: Maintain existing coverage levels
3. **Naming**: Use `TestFunctionName_Scenario` pattern
4. **Table-Driven**: Preferred for multiple test cases

### Test Locations

| Package | Test Pattern |
|---------|--------------|
| pkg/mcp | router_test.go, gateway_test.go, session_test.go, handler_test.go |
| pkg/a2a | types_test.go, gateway_test.go, handler_test.go |
| pkg/runtime | runtime_test.go, container_test.go, network_test.go |
| pkg/builder | cache_test.go, builder_test.go |
| pkg/state | state_test.go |
| tests/integration | *_test.go (build tag: integration) |

### Mocks

- `MockDockerClient`: pkg/runtime/mock_test.go
- `MockAgentClient`: pkg/mcp/mock_test.go
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
