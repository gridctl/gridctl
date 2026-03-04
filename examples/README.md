# Examples

Example stacks demonstrating Gridctl patterns and capabilities.

## 🚀 Quick Start

```bash
gridctl deploy examples/getting-started/agent-basic.yaml
```

## 📁 Categories

| Folder | Description |
|--------|-------------|
| [🎯 getting-started/](getting-started/) | Basic examples to get up and running |
| [🔌 transports/](transports/) | MCP transport types: local process, SSH, HTTP, SSE |
| [🤖 multi-agent/](multi-agent/) | Agent orchestration and A2A protocol |
| [📦 platforms/](platforms/) | Third-party MCP servers (container-based) |
| [🔗 openapi/](openapi/) | Turn REST APIs into MCP tools via OpenAPI specs |
| [🔐 access-control/](access-control/) | Tool filtering and security patterns |
| [⚡ code-mode/](code-mode/) | Reduce context window with search + execute meta-tools |
| [🔒 gateways/](gateways/) | Bridge to existing infrastructure |
| [🔑 secrets-vault/](secrets-vault/) | Vault secrets and variable sets |
| [📋 registry/](registry/) | Agent Skills registry ([agentskills.io](https://agentskills.io) spec) |
| [🧪 _mock-servers/](_mock-servers/) | Test servers for development |

## 🎬 Recommended Path

1. **Start here**: `getting-started/agent-basic.yaml` - stack, networking, access control (placeholder containers)
2. **Real MCP servers**: `transports/local-mcp.yaml` - actual MCP server logic via stdio transport
3. **Multi-agent**: `multi-agent/multi-agent-skills.yaml` - agents using other agents as tools
4. **Platforms**: `platforms/github-mcp.yaml` - third-party MCP servers
5. **OpenAPI**: `openapi/openapi-basic.yaml` - turn any REST API into MCP tools
6. **Registry**: `registry/registry-basic.yaml` - Agent Skills as MCP prompts
7. **Workflows**: `registry/items/workflow-basic/` - executable skill workflows

> **Note:** Getting-started examples use placeholder containers to focus on infrastructure concepts.
> Transport and platform examples include real MCP server implementations.

## 📊 Feature Matrix

| Example | Transports | Agents | A2A | External | OpenAPI | Registry | Workflows | Code Mode | Vault |
|---------|------------|--------|-----|----------|---------|----------|-----------|-----------|-------|
| agent-basic | - | ✅ | - | - | - | - | - | - | - |
| skills-basic | - | ✅ | ✅ | - | - | - | - | - | - |
| local-mcp | stdio | - | - | - | - | - | - | - | - |
| ssh-mcp | ssh+stdio | - | - | - | - | - | - | - | - |
| external-mcp | http, sse | - | - | ✅ | - | - | - | - | - |
| multi-agent-skills | - | ✅ | ✅ | - | - | - | - | - | - |
| basic-a2a | - | ✅ | ✅ | - | - | - | - | - | - |
| atlassian-mcp | sse | - | - | ✅ | - | - | - | - | - |
| chrome-devtools-mcp | stdio | - | - | ✅ | - | - | - | - | - |
| context7-mcp | stdio | - | - | ✅ | - | - | - | - | - |
| github-mcp | stdio | - | - | ✅ | - | - | - | - | - |
| zapier-mcp | stdio | - | - | ✅ | - | - | - | - | - |
| openapi-basic | openapi | - | - | - | ✅ | - | - | - | - |
| openapi-auth | openapi | - | - | - | ✅ | - | - | - | - |
| tool-filtering | - | ✅ | - | - | - | - | - | - | - |
| code-mode-basic | - | ✅ | - | - | - | - | - | ✅ | - |
| gateway-basic | http | - | - | ✅ | - | - | - | - | - |
| gateway-remote | http | - | - | ✅ | - | - | - | - | - |
| vault-basic | stdio | - | - | - | - | - | - | - | ✅ |
| vault-sets | - | - | - | - | - | - | - | - | ✅ |
| registry-basic | stdio | - | - | - | - | ✅ | - | - | - |
| registry-advanced | stdio | - | - | - | - | ✅ | - | - | - |
| workflow-basic | stdio | - | - | - | - | ✅ | ✅ | - | - |
| workflow-parallel | stdio | - | - | - | - | ✅ | ✅ | - | - |
| workflow-conditional | stdio | - | - | - | - | ✅ | ✅ | - | - |

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
