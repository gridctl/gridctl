# 🔐 Access Control

Examples demonstrating tool filtering and least-privilege access patterns.

## 📄 Examples

| File | Description |
|------|-------------|
| `tool-filtering.yaml` | Server-level and agent-level tool filtering |

## 💡 Concepts

### Two-Level Filtering

Gridctl supports restricting tool access at two levels:

1. **Server-Level** - Restrict which tools a server exposes to the gateway:
```yaml
mcp-servers:
  - name: restricted-server
    image: my-mcp:latest
    port: 3000
    tools: ["read", "list"]   # Only these tools are visible
```

2. **Agent-Level** - Restrict which tools a specific agent can access:
```yaml
agents:
  - name: viewer-agent
    image: my-agent:latest
    uses:
      - server: restricted-server
        tools: ["read"]       # Further restricted per agent
```

### Filtering Precedence

Server-level filtering applies first, then agent-level filtering narrows further. An agent can never access tools that the server doesn't expose.

## 💻 Usage

```bash
gridctl deploy examples/access-control/tool-filtering.yaml
```
