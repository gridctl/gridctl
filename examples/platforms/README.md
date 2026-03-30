# 📦 Platforms

Third-party MCP servers that Gridctl runs as containers.

## 📄 Examples

| File | Platform | Description |
|------|----------|-------------|
| `atlassian-mcp.yaml` | Atlassian | Official Atlassian Rovo MCP server for Jira, Confluence, Compass |
| `chrome-devtools-mcp.yaml` | Chrome DevTools | Browser automation, debugging, and performance tracing |
| `context7-mcp.yaml` | Context7 | Up-to-date library documentation and code examples |
| `github-mcp.yaml` | GitHub | Official GitHub MCP server for repos, issues, PRs |
| `zapier-mcp.yaml` | Zapier | Integrate with 8000+ apps through Zapier automation |

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

### chrome-devtools-mcp.yaml

Requires Google Chrome and Node.js v20.19+ installed on the host.

### context7-mcp.yaml

Requires Node.js installed. Optionally, create a free API key for higher rate limits:

```bash
# Get API key at https://context7.com/dashboard
export CONTEXT7_API_KEY=your_api_key
```

### github-mcp.yaml

Create a GitHub Personal Access Token:

```bash
# Create token at https://github.com/settings/tokens
export GITHUB_PERSONAL_ACCESS_TOKEN=ghp_xxxxxxxxxxxx
```

### zapier-mcp.yaml

Requires a Zapier account and Node.js installed. OAuth authentication is handled via browser flow on first use.

## 💻 Usage

```bash
gridctl apply examples/platforms/atlassian-mcp.yaml
gridctl apply examples/platforms/chrome-devtools-mcp.yaml
gridctl apply examples/platforms/context7-mcp.yaml
gridctl apply examples/platforms/github-mcp.yaml
gridctl apply examples/platforms/zapier-mcp.yaml
```

## 🔗 References

- [Atlassian Rovo MCP Server](https://github.com/atlassian/atlassian-mcp-server)
- [Chrome DevTools MCP](https://github.com/ChromeDevTools/chrome-devtools-mcp)
- [Context7 MCP](https://github.com/upstash/context7)
- [GitHub MCP Server](https://github.com/github/github-mcp-server)
- [Zapier MCP](https://github.com/zapier/zapier-mcp)
