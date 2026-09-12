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
gridctl apply examples/gateways/gateway-basic.yaml
```

## 🖥️ gateway-remote.yaml

Exposes Gridctl's gateway on all interfaces for remote MCP clients through HTTPS or an encrypted tunnel. Authentication does not encrypt traffic: do not expose plain HTTP port 8180 to remote clients.

### Prerequisites

1. An MCP server running (e.g., Qdrant MCP, Itential dev-stack)
2. A gateway token stored on the server with `gridctl var set GATEWAY_TOKEN`
3. A TLS-terminating reverse proxy forwarding to port 8180, with the backend firewalled so only the proxy can reach it, or an SSH tunnel

If the proxy forwards the public hostname in the `Host` header, add that exact hostname to `gateway.allowed_hosts`. Loopback hosts are accepted by default; valid credentials do not bypass Host validation. See [gateway configuration](../../docs/config-schema.md#gateway).

### Usage

```bash
# Deploy on the server
gridctl apply examples/gateways/gateway-remote.yaml
```

For SSH-only access, change `gateway.bind` in the example to `127.0.0.1` before applying it, and run `ssh -N -L 8180:localhost:8180 <host>` on the client machine. Connect the client to `http://localhost:8180/mcp` through that tunnel, retaining the configured authentication.

### Client Configuration

Gateway security settings are restart-only. A reload that changes authentication, bind, Host/Origin allowlists, or the insecure override returns `restart_required`, including mixed edits. The saved file stays on disk and the active credential stays in force until the gateway process is restarted with the original startup options. See [recovery](../../docs/troubleshooting.md#gateway-security-requires-a-restart); do not use workload destruction as an authentication recovery step.

For the web UI, select Bearer for this example. An API-key stack instead uses raw `Authorization` by default or its configured browser-supported custom header. Verification precedes saving. localStorage is readable by origin scripts; a window-only notice means refresh and new detached windows require re-entry. Keep the proxy backend private and connect through HTTPS or an encrypted tunnel.

Configure your MCP client's Streamable HTTP connection using the following values (replace `gateway.example.com` with your proxy's HTTPS hostname):

| Setting | Value |
|---------|-------|
| Endpoint | `https://gateway.example.com/mcp` |
| Header | `Authorization: Bearer <token>` |

Supply the token through the client's secure credential or environment settings, not a literal value in version-controlled configuration. The YAML example includes an `mcp-remote` bridge configuration for clients that require stdio. `/sse` only returns a legacy negotiation hint; it is not a persistent MCP transport.

Grouped endpoints at `/groups/{name}/mcp` require the same header on every request, including stream reconnection and session deletion. Neither a group name nor a session ID authenticates the caller. `gridctl link --group` selects an endpoint but does not provision credentials into managed client files.

Check the public probe and authenticated API separately, with `GATEWAY_TOKEN` supplied securely in the local shell environment:

```bash
curl https://gateway.example.com/health
curl -H "Authorization: Bearer ${GATEWAY_TOKEN}" https://gateway.example.com/api/status
```

The probe does not verify credentials. With auth configured, a missing or incorrect token on `/api/status` or a grouped endpoint returns HTTP 401. Native MCP clients may omit Origin; valid credentials do not bypass MCP Origin checks.

### 📂 Config File Locations

| OS | Path |
|----|------|
| Linux | `~/.config/Claude/claude_desktop_config.json` |
| macOS | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Windows | `%APPDATA%\Claude\claude_desktop_config.json` |
