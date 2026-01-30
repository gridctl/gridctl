# MCP Protocol Internals

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

## MCP Gateway

When `gridctl deploy` runs, it:
1. Parses the stack YAML
2. Creates Docker network
3. Builds/pulls images
4. Starts containers with host port bindings (9000+)
5. Registers agents with the MCP gateway
6. Starts HTTP server with MCP endpoint

## MCP Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/mcp` | POST | JSON-RPC 2.0 (initialize, tools/list, tools/call) |
| `/sse` | GET | SSE connection endpoint (for Claude Desktop) |
| `/message` | POST | Message endpoint for SSE sessions |

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/status` | GET | Gateway + agent status (unified agents with A2A info) |
| `/api/mcp-servers` | GET | List registered MCP servers |
| `/api/tools` | GET | List aggregated tools |
| `/health` | GET | Liveness check (returns 200 when HTTP server is running) |
| `/ready` | GET | Readiness check (returns 200 only when all MCP servers are initialized) |
| `/` | GET | Web UI (embedded React app) |

## A2A Protocol Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/.well-known/agent.json` | GET | Agent Card discovery (lists all local A2A agents) |
| `/a2a/{agent}` | GET | Get specific agent's Agent Card |
| `/a2a/{agent}` | POST | JSON-RPC endpoint (message/send, tasks/get, etc.) |

## Tool Prefixing

Tools are prefixed with server name to avoid collisions:
- `server-name__tool-name` (e.g., `itential-mcp__get_workflows`)

## Key Files

| File | Purpose |
|------|---------|
| `types.go` | JSON-RPC, MCP types, AgentClient interface |
| `client.go` | HTTP transport client |
| `stdio.go` | Stdio transport client (Docker attach) |
| `process.go` | Local process transport client (host process) |
| `sse.go` | SSE server (northbound) |
| `session.go` | Session management |
| `router.go` | Tool routing |
| `gateway.go` | Protocol bridge logic |
| `handler.go` | HTTP handlers |
