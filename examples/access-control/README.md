# 🔐 Access Control

Examples demonstrating tool filtering for least-privilege access.

## 📄 Examples

| File | Description |
|------|-------------|
| `tool-filtering.yaml` | Server-level tool filtering |

## 💡 Concepts

### Server-Level Filtering

The `tools` field on an MCP server whitelists which tools are exposed to all clients. Tools not in the list are never loaded into the gateway — they don't appear in tool listings and cannot be called:

```yaml
mcp-servers:
  - name: restricted-server
    image: my-mcp:latest
    port: 3000
    tools: ["read", "list"]   # Only these tools reach the gateway
```

This is the primary mechanism for reducing context window consumption and preventing tool confusion.

## 💻 Usage

```bash
gridctl apply examples/access-control/tool-filtering.yaml
```
