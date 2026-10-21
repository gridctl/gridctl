# 🧪 Mock Servers

Test MCP servers for development purposes.

## 🚀 Quick Start

Build and run all mock servers with a single command:

```bash
task mock:servers
```

This builds both servers and starts `mock-mcp-server` on ports 9001 (HTTP) and 9002 (SSE).

To use custom ports:

```bash
task mock:servers PORT=3000
```

This starts HTTP on port 3000 and SSE on port 3001.

To stop and clean up:

```bash
task mock:clean
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

By default the server speaks the legacy handshake generation
(`initialize`, `ping`). Pass `-protocol 2026-07-28` to speak the
stateless generation instead: `server/discover` replaces the handshake
and ping, results carry `resultType` and cache metadata, and the removed
legacy methods answer `-32601`.

## 🌐 mock-mcp-server

A Go-based MCP server that supports HTTP and SSE transports.

### Run

```bash
cd examples/_mock-servers/mock-mcp-server

# HTTP mode (defaults to port 8080 when -port is omitted)
go run . -port 9001

# SSE mode
go run . -port 9002 -sse

# Stateless generation (2026-07-28: server/discover, no handshake)
go run . -port 9001 -protocol 2026-07-28

# Same generation, with tools.listChanged and subscriptions/listen
go run . -port 9001 -protocol 2026-07-28 -list-changed
```

With `-protocol 2026-07-28 -list-changed`, `server/discover` declares `tools.listChanged` and `subscriptions/listen` stays open until the client disconnects. `mutate_tools` emits a tagged `notifications/tools/list_changed` to each parked listener. Without `-list-changed`, that method is `-32601`. Handshake mode is unchanged: `-list-changed` still emits the notification on the POST SSE stream.

### OAuth Flags

The server can simulate an OAuth 2.1 protected resource for exercising `auth.type: oauth` brokering:

| Flag | Default | Description |
|------|---------|-------------|
| `-oauth` | off | Require OAuth 2.1 authorization |
| `-oauth-no-dcr` | off | Refuse dynamic client registration (501) |
| `-oauth-access-ttl` | 3600 | Access token lifetime in seconds |
| `-base-url` | `http://127.0.0.1:<port>` | Externally visible base URL |

### Usage

Used by `examples/transports/external-mcp.yaml`.
