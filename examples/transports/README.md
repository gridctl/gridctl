# 🔌 Transports

Examples demonstrating different MCP transport types.

## 📄 Examples

| File | Transport | Description |
|------|-----------|-------------|
| `local-mcp.yaml` | stdio | Run MCP servers as local host processes |
| `ssh-mcp.yaml` | ssh+stdio | Connect to MCP servers on remote machines via SSH |
| `external-mcp.yaml` | http, sse | Connect to external HTTP and SSE MCP servers |

## ⚙️ Prerequisites

### Quick Setup (Recommended)

Build and start all mock servers with one command:

```bash
make mock-servers
```

This builds `mock-stdio-server` and starts `mock-mcp-server` on ports 9001 (HTTP) and 9002 (SSE).

### Manual Setup

<details>
<summary>Click to expand manual instructions</summary>

#### local-mcp.yaml

Build the mock stdio server:

```bash
cd examples/_mock-servers/local-stdio-server
go build -o mock-stdio-server .
```

#### external-mcp.yaml

Start the mock MCP server:

```bash
cd examples/_mock-servers/mock-mcp-server
go run main.go -port 9001           # HTTP mode
go run main.go -port 9002 -sse      # SSE mode
```

</details>

### ssh-mcp.yaml

Requires SSH access to a remote host running an MCP server.

## 💻 Usage

```bash
gridctl deploy examples/transports/local-mcp.yaml
gridctl deploy examples/transports/ssh-mcp.yaml
gridctl deploy examples/transports/external-mcp.yaml
```
