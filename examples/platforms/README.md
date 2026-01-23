# 📦 Platforms

Third-party MCP servers that Gridctl runs as containers.

## 📄 Examples

| File | Platform | Description |
|------|----------|-------------|
| `github-mcp.yaml` | GitHub | Official GitHub MCP server for repos, issues, PRs |
| `itential.yaml` | Itential | Itential Platform MCP server in dev-stack network |

## 🔧 Pattern

These examples use `image:` to run MCP servers as containers:

```yaml
mcp-servers:
  - name: github
    image: ghcr.io/github/github-mcp-server:latest
    transport: stdio
```

For connecting to **existing** MCP servers, see [🔒 gateways/](../gateways/).

## ⚙️ Prerequisites

### github-mcp.yaml

Create a GitHub Personal Access Token:

```bash
# Create token at https://github.com/settings/tokens
export GITHUB_PERSONAL_ACCESS_TOKEN=ghp_xxxxxxxxxxxx
```

### itential.yaml

Requires [itential-dev-stack](https://github.com/itential/itential-dev-stack) running (creates the `devstack` network).

## 💻 Usage

```bash
gridctl deploy examples/platforms/github-mcp.yaml
gridctl deploy examples/platforms/itential.yaml
```

## 🔗 References

- [GitHub MCP Server](https://github.com/github/github-mcp-server)
- [Itential MCP](https://github.com/itential/itential-mcp)
