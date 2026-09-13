# 🔌 Transports

Examples demonstrating different MCP transport types.

## 📄 Examples

| File | Transport | Description |
|------|-----------|-------------|
| `local-mcp.yaml` | stdio | Run MCP servers as local host processes |
| `ssh-mcp.yaml` | ssh+stdio | Connect to MCP servers on remote machines via SSH |
| `external-mcp.yaml` | http, sse | Connect to external HTTP and SSE MCP servers |
| `external-auth.yaml` | http | Authenticate to external servers: OAuth brokering, bearer token, custom header |

## ⚙️ Prerequisites

### Quick Setup (Recommended)

Build and start all mock servers with one command:

```bash
task mock:servers
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

Local processes remain unsandboxed. Without `execution`, they inherit the daemon environment except reserved internal credentials. Opt into `mode: local` for an inheritance allowlist and absolute executable lookup; bare `npx`/`uvx` commands need an absolute executable or explicit `lookup: ambient_path`. Bootstrap stays under that contract. SSH rejects execution declarations, and stopping the local SSH client does not prove remote descendant cleanup. See [execution controls](../../docs/execution.md#local-environment-hygiene).

### external-auth.yaml

The bearer and header servers read their credentials from the variable store (`gridctl var set GITHUB_PAT`, `gridctl var set INTERNAL_API_KEY`); the OAuth server deploys in a "needs auth" state until you run `gridctl auth login notion`.

## 💻 Usage

```bash
gridctl apply examples/transports/local-mcp.yaml
gridctl apply examples/transports/ssh-mcp.yaml
gridctl apply examples/transports/external-mcp.yaml
gridctl apply examples/transports/external-auth.yaml
```

## Export a Running Stack

After building the mock server as described above, run these commands from the repository root with no other gridctl deployment running:

```bash
gridctl apply examples/transports/local-mcp.yaml
gridctl export
gridctl export --format json
gridctl export -o ./exported-stack
gridctl destroy examples/transports/local-mcp.yaml
```

The first two exports print one document to stdout with a review notice on stderr. The directory export writes `exported-stack/stack.yaml` and, if imported sources exist in the local skills lockfile, `skills.yaml`. It does not copy the mock executable. This example's stack-relative command remains `../_mock-servers/local-stdio-server/mock-stdio-server`; adjust it to the recipient's layout before applying the exported file.

References such as `${API_KEY}` or `${var:KEY}` stay unresolved. Recognized inline credentials and nonempty sensitive fallback/replacement operands block export rather than becoming invented placeholders. Review other authored literals before sharing. The web Stack spec view's Export YAML action uses the same policy, while raw spec content is unchanged. See [export semantics](../../docs/cli-reference.md#export-semantics).
