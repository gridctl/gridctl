# Examples

Example stacks demonstrating Gridctl patterns and capabilities.

## 🚀 Quick Start

```bash
gridctl deploy examples/getting-started/mcp-basic.yaml
```

## 📁 Categories

| Folder | Description |
|--------|-------------|
| [🎯 getting-started/](getting-started/) | Basic examples to get up and running |
| [🔌 transports/](transports/) | MCP transport types: local process, SSH, HTTP, SSE |
| [📦 platforms/](platforms/) | Third-party MCP servers (container-based) |
| [🔗 openapi/](openapi/) | Turn REST APIs into MCP tools via OpenAPI specs |
| [🔐 access-control/](access-control/) | Tool filtering and security patterns |
| [⚡ code-mode/](code-mode/) | Reduce context window with search + execute meta-tools |
| [🔒 gateways/](gateways/) | Bridge to existing infrastructure |
| [🔑 secrets-vault/](secrets-vault/) | Vault secrets and variable sets |
| [📋 registry/](registry/) | Skills registry ([agentskills.io](https://agentskills.io) spec) |
| [🧪 _mock-servers/](_mock-servers/) | Test servers for development |

## 🎬 Recommended Path

1. **Start here**: `getting-started/mcp-basic.yaml` - stack, networking, tool filtering (placeholder containers)
2. **Real MCP servers**: `transports/local-mcp.yaml` - actual MCP server logic via stdio transport
3. **Platforms**: `platforms/github-mcp.yaml` - third-party MCP servers
4. **OpenAPI**: `openapi/openapi-basic.yaml` - turn any REST API into MCP tools
5. **Registry**: `registry/registry-basic.yaml` - Skills as MCP prompts
6. **Workflows**: `registry/items/workflow-basic/` - executable skill workflows

> **Note:** Getting-started examples use placeholder containers to focus on infrastructure concepts.
> Transport and platform examples include real MCP server implementations.

## 📊 Feature Matrix

| Example | Transports | Tool Filtering | External | OpenAPI | Registry | Workflows | Code Mode | Vault |
|---------|------------|----------------|----------|---------|----------|-----------|-----------|-------|
| mcp-basic | - | ✅ | - | - | - | - | - | - |
| local-mcp | stdio | - | - | - | - | - | - | - |
| ssh-mcp | ssh+stdio | - | - | - | - | - | - | - |
| external-mcp | http, sse | - | ✅ | - | - | - | - | - |
| atlassian-mcp | sse | - | ✅ | - | - | - | - | - |
| chrome-devtools-mcp | stdio | - | ✅ | - | - | - | - | - |
| context7-mcp | stdio | - | ✅ | - | - | - | - | - |
| github-mcp | stdio | - | ✅ | - | - | - | - | - |
| zapier-mcp | stdio | - | ✅ | - | - | - | - | - |
| openapi-basic | openapi | - | - | ✅ | - | - | - | - |
| openapi-auth | openapi | - | - | ✅ | - | - | - | - |
| tool-filtering | - | ✅ | - | - | - | - | - | - |
| code-mode-basic | - | - | - | - | - | - | ✅ | - |
| gateway-basic | http | - | ✅ | - | - | - | - | - |
| gateway-remote | http | - | ✅ | - | - | - | - | - |
| vault-basic | stdio | - | - | - | - | - | - | ✅ |
| vault-sets | - | - | - | - | - | - | - | ✅ |
| registry-basic | stdio | - | - | - | ✅ | - | - | - |
| registry-advanced | stdio | - | - | - | ✅ | - | - | - |
| workflow-basic | stdio | - | - | - | ✅ | ✅ | - | - |
| workflow-parallel | stdio | - | - | - | ✅ | ✅ | - | - |
| workflow-conditional | stdio | - | - | - | ✅ | ✅ | - | - |

## 💻 Usage Pattern

All examples follow the same deployment pattern:

```bash
# Deploy a stack
gridctl deploy examples/<category>/<file>.yaml

# Force recreate containers
gridctl deploy examples/<category>/<file>.yaml

# View status
gridctl status
```
