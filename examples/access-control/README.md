# 🔐 Access Control

Examples demonstrating tool filtering and per-client scoping for least-privilege access.

## 📄 Examples

| File | Description |
|------|-------------|
| `tool-filtering.yaml` | Server-level tool filtering (applies to all clients) |
| `per-client-scoping.yaml` | Per-client allow-lists via the `clients:` block |

## 💡 Concepts

### Server-Level Filtering

The `tools` field on an MCP server whitelists which tools are exposed to all clients. Tools not in the list are never loaded into the gateway - they don't appear in tool listings and cannot be called:

```yaml
mcp-servers:
  - name: restricted-server
    image: my-mcp:latest
    port: 3000
    tools: ["read", "list"]   # Only these tools reach the gateway
```

This is the primary mechanism for reducing context window consumption and preventing tool confusion.

### Per-Client Scoping

The top-level `clients:` block restricts which servers and tools each *connecting client* can reach, independent of the global server-level filter. It follows Kubernetes NetworkPolicy semantics: omitting the block keeps today's behavior (every client sees everything), while adding it opts into least-privilege per client.

```yaml
clients:
  default: deny            # unlisted clients see nothing (or set "allow")
  profiles:
    cursor:
      servers: [github]    # cursor reaches only the github server
    claude-code:
      tools:               # claude-code reaches only these prefixed tools
        - github__search-repos
```

The scope is enforced on `tools/list`, `tools/call`, and the code-mode search/execute surface. Bind a linked client to a profile with a stable identifier:

```bash
gridctl link cursor --client-id cursor
```

See [docs/config-schema.md](../../docs/config-schema.md#clients-per-client-access-scoping) for the full reference, including client-identity reconciliation and reload semantics.

## 💻 Usage

```bash
gridctl apply examples/access-control/tool-filtering.yaml
gridctl apply examples/access-control/per-client-scoping.yaml
```
