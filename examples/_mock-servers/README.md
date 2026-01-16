# 🧪 Mock Servers

Test MCP servers for development purposes.

## 🚀 Quick Start

Build and run all mock servers with a single command:

```bash
make mock-servers
```

This builds both servers and starts `mock-mcp-server` on ports 9001 (HTTP) and 9002 (SSE).

To stop and clean up:

```bash
make clean-mock-servers
```

## 📂 Servers

| Directory | Transport | Description |
|-----------|-----------|-------------|
| `local-stdio-server/` | stdio | Mock MCP server for local process examples |
| `mock-mcp-server/` | http, sse | Mock MCP server for external connection examples |

## 🖥️ local-stdio-server

A Go-based MCP server that communicates via stdio (stdin/stdout JSON-RPC).

### Build

```bash
cd examples/_mock-servers/local-stdio-server
go build -o mock-stdio-server .
```

### Usage

Used by `examples/transports/local-mcp.yaml`.

## 🌐 mock-mcp-server

A Go-based MCP server that supports HTTP and SSE transports.

### Run

```bash
cd examples/_mock-servers/mock-mcp-server

# HTTP mode
go run main.go -port 9001

# SSE mode
go run main.go -port 9002 -sse
```

### Usage

Used by `examples/transports/external-mcp.yaml`.
