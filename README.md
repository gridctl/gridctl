# Gridctl

[![License](https://img.shields.io/badge/License-Apache%202.0-f59e0b.svg)](LICENSE)

**MCP orchestration for AI agents.** Define your topology in YAML, deploy with one command.

Gridctl is a protocol bridge that aggregates tools from multiple [MCP](https://modelcontextprotocol.io/) servers into a single gateway endpoint. Connect Claude Desktop (or any MCP client) to dozens of tool servers through one SSE connection.

[Installation](#installation) | [Quick Start](#quick-start) | [Features](#features) | [Configuration](#topology-configuration) | [Examples](#examples)

---

## Installation

### Homebrew (macOS/Linux)

```bash
brew install gridctl/tap/gridctl
```

Or tap first:

```bash
brew tap gridctl/tap
brew install gridctl
```

### From Source

```bash
make build
```

---

## Quick Start

```bash
# Deploy a topology
gridctl deploy examples/getting-started/agent-basic.yaml

# Check status
gridctl status

# Open the web UI
open http://localhost:8180

# Tear down when done
gridctl destroy examples/getting-started/agent-basic.yaml
```

---

## Features

### Topology as Code

Define your entire MCP infrastructure in a single YAML file. Gridctl handles container orchestration, networking, and protocol translation.

### Protocol Bridge

Aggregates tools from multiple MCP servers into a unified gateway. Clients connect once and access all tools through a single endpoint with automatic namespacing (`server__tool`).

### Multiple Transports

Connect to MCP servers however they run:

| Transport | Configuration | Use Case |
|-----------|---------------|----------|
| **HTTP** | `image` + `port` | Containerized MCP servers with HTTP endpoints |
| **Stdio** | `image` + `transport: stdio` | Containerized MCP servers using stdin/stdout |
| **Local Process** | `command` | MCP servers running directly on host |
| **SSH** | `command` + `ssh.host` | MCP servers on remote machines |
| **External URL** | `url` | Existing MCP servers running elsewhere |

### Agent Access Control

Define which MCP servers each agent can access. Gridctl supports two levels of tool filtering:

- **Server-Level**: Restrict which tools a server exposes to all agents
- **Agent-Level**: Restrict which tools each agent can access from a server

Agents receive an injected `MCP_ENDPOINT` environment variable and can only see tools from their allowed servers.

### A2A Protocol Support

Built-in support for [Agent-to-Agent](https://google.github.io/A2A/) protocol. Expose local agents via `/.well-known/agent.json` or connect to remote A2A agents.

### Web UI

Real-time topology visualization powered by React Flow. Monitor container status, view registered tools, and inspect agent configurations.

---

## Topology Configuration

```yaml
version: "1"
name: my-topology

mcp-servers:
  # Containerized HTTP server
  - name: api-tools
    image: ghcr.io/org/mcp-server:latest
    port: 3000
    env:
      API_KEY: "${API_KEY}"

  # Containerized stdio server
  - name: cli-tools
    image: ghcr.io/org/cli-mcp:latest
    transport: stdio

  # Local process on host
  - name: local-tools
    command: ["./my-mcp-server"]

  # Remote via SSH
  - name: remote-tools
    command: ["/opt/mcp/server"]
    ssh:
      host: "192.168.1.50"
      user: "mcp"

  # External MCP server
  - name: remote-api
    url: https://api.example.com/mcp

agents:
  - name: my-agent
    image: my-org/agent:latest
    description: "Agent with selective tool access"
    uses:
      - api-tools                 # Full access to all tools
      - server: cli-tools         # Restricted access
        tools: ["read", "list"]   # Only these specific tools
    env:
      MODEL: "claude-3-5-sonnet"

resources:
  - name: postgres
    image: postgres:16
    env:
      POSTGRES_PASSWORD: secret
```

---

## CLI Reference

| Command | Description |
|---------|-------------|
| `gridctl deploy <topology.yaml>` | Start containers and gateway (daemon mode) |
| `gridctl deploy <topology.yaml> -f` | Start in foreground for debugging |
| `gridctl deploy <topology.yaml> -p 9000` | Use custom gateway port |
| `gridctl status` | Show running topologies and containers |
| `gridctl destroy <topology.yaml>` | Stop gateway and remove containers |

---

## Example Claude Desktop Configuration

Add to `~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "gridctl": {
      "url": "http://localhost:8180/sse"
    }
  }
}
```

Restart Claude Desktop. All tools from your topology will be available.

---

## Examples

| Example | Description |
|---------|-------------|
| [agent-basic.yaml](examples/getting-started/agent-basic.yaml) | Basic agent with MCP server access control |
| [tool-filtering.yaml](examples/access-control/tool-filtering.yaml) | Server and agent-level tool filtering |
| [local-mcp.yaml](examples/transports/local-mcp.yaml) | Local process MCP server (no container) |
| [external-mcp.yaml](examples/transports/external-mcp.yaml) | Connect to external HTTP/SSE MCP servers |
| [ssh-mcp.yaml](examples/transports/ssh-mcp.yaml) | MCP server over SSH tunnel |
| [basic-a2a.yaml](examples/multi-agent/basic-a2a.yaml) | Agent-to-agent protocol communication |

---

## Development

```bash
make build          # Build frontend and backend
make build-web      # Build React frontend only
make build-go       # Build Go binary only
make dev            # Run Vite dev server
make test           # Run tests
make test-coverage  # Run tests with coverage report
make clean          # Remove build artifacts
```

---

## License

[Apache 2.0](LICENSE)
