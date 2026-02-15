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
| [🔐 access-control/](access-control/) | Tool filtering and security patterns |
| [🔒 gateways/](gateways/) | Bridge to existing infrastructure |
| [📋 registry/](registry/) | Reusable MCP prompts and skill chains |
| [🧪 _mock-servers/](_mock-servers/) | Test servers for development |

## 🎬 Recommended Path

1. **Start here**: `getting-started/agent-basic.yaml` - stack, networking, access control (placeholder containers)
2. **Real MCP servers**: `transports/local-mcp.yaml` - actual MCP server logic via stdio transport
3. **Multi-agent**: `multi-agent/multi-agent-skills.yaml` - agents using other agents as tools
4. **Platforms**: `platforms/github-mcp.yaml` - third-party MCP servers
5. **Registry**: `registry/registry-basic.yaml` - reusable prompts and skill chains

> **Note:** Getting-started examples use placeholder containers to focus on infrastructure concepts.
> Transport and platform examples include real MCP server implementations.

## 📊 Feature Matrix

| Example | Transports | Agents | A2A | External | Registry |
|---------|------------|--------|-----|----------|----------|
| agent-basic | - | ✅ | - | - | - |
| skills-basic | - | ✅ | ✅ | - | - |
| local-mcp | stdio | - | - | - | - |
| ssh-mcp | ssh+stdio | - | - | - | - |
| external-mcp | http, sse | - | - | ✅ | - |
| multi-agent-skills | - | ✅ | ✅ | - | - |
| basic-a2a | - | ✅ | ✅ | - | - |
| atlassian-mcp | sse | - | - | ✅ | - |
| chrome-devtools-mcp | stdio | - | - | ✅ | - |
| context7-mcp | stdio | - | - | ✅ | - |
| github-mcp | stdio | - | - | ✅ | - |
| zapier-mcp | stdio | - | - | ✅ | - |
| tool-filtering | - | ✅ | - | - | - |
| gateway-basic | http | - | - | ✅ | - |
| gateway-remote | http | - | - | ✅ | - |
| registry-basic | stdio | - | - | - | ✅ |
| registry-advanced | stdio | - | - | - | ✅ |

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
