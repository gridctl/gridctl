# 📦 Platforms

Third-party MCP servers that Gridctl runs as containers.

## 📄 Examples

| File | Platform | Description |
|------|----------|-------------|
| `atlassian-mcp.yaml` | Atlassian | Official Atlassian Rovo MCP server for Jira, Confluence, Compass |
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

### atlassian-mcp.yaml

Requires an Atlassian Cloud account. OAuth authentication is handled via browser flow on first use.

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
gridctl deploy examples/platforms/atlassian-mcp.yaml
gridctl deploy examples/platforms/github-mcp.yaml
gridctl deploy examples/platforms/itential.yaml
```

## 🔗 References

- [Atlassian Rovo MCP Server](https://github.com/atlassian/atlassian-mcp-server)
- [GitHub MCP Server](https://github.com/github/github-mcp-server)
- [Itential MCP](https://github.com/itential/itential-mcp)
