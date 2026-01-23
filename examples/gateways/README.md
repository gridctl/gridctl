# 🔒 Gateways

Examples where Gridctl acts as a gateway to existing infrastructure.

## 📄 Examples

| File | Description |
|------|-------------|
| `gateway-basic.yaml` | Basic gateway to an existing MCP server |
| `gateway-remote.yaml` | Expose gateway for remote Claude Desktop access |

## 🔧 Pattern

These examples use `url:` to connect to MCP servers already running elsewhere:

```yaml
mcp-servers:
  - name: external-mcp
    url: http://localhost:8000/mcp
    transport: http
```

For running MCP servers **as containers**, see [📦 platforms/](../platforms/).

## 🔗 gateway-basic.yaml

Basic example connecting to any MCP server running locally.

### Prerequisites

An MCP server running and accessible via HTTP or SSE.

### Usage

```bash
# Update the url in the file to match your MCP server
gridctl deploy examples/gateways/gateway-basic.yaml
```

## 🖥️ gateway-remote.yaml

Exposes Gridctl's gateway on all interfaces for remote MCP clients.

### Prerequisites

1. An MCP server running (e.g., Qdrant MCP, Itential dev-stack)
2. Port 8180 accessible from the remote machine (check firewall)

### Usage

```bash
# Deploy on the server
gridctl deploy examples/gateways/gateway-remote.yaml

# Find server IP
ip addr show | grep "inet " | grep -v 127.0.0.1
```

### Client Configuration

On the remote machine, configure Claude Desktop:

```json
{
  "mcpServers": {
    "gridctl": {
      "command": "npx",
      "args": ["mcp-remote", "http://<SERVER_IP>:8180/sse", "--allow-http", "--transport", "sse-only"]
    }
  }
}
```

### 📂 Config File Locations

| OS | Path |
|----|------|
| Linux | `~/.config/Claude/claude_desktop_config.json` |
| macOS | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Windows | `%APPDATA%\Claude\claude_desktop_config.json` |
