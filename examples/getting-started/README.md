# 🎯 Getting Started

Basic examples demonstrating core Gridctl features.

## 📄 Examples

| File | Description |
|------|-------------|
| `agent-basic.yaml` | MCP servers, agents, environment injection, tool access control |
| `skills-basic.yaml` | `equipped_skills` alias and agents-as-tools pattern |

## 🚀 Quick Start

```bash
# Deploy the basic agent demo
gridctl deploy examples/getting-started/agent-basic.yaml

# Check status
gridctl status
```

## 📖 What You'll Learn

- Defining MCP servers in a topology
- Creating agents that consume MCP tools
- Using `uses` to control tool access
- Environment variable injection (`MCP_ENDPOINT`)

## ℹ️ Note: Infrastructure vs Application Logic

These examples use **placeholder containers** (`alpine:latest` with `sleep`) to demonstrate
topology, networking, and access control—not actual agent or MCP server logic.

To see examples with real MCP servers:

| Example | What it demonstrates |
|---------|---------------------|
| `transports/local-mcp.yaml` | Stdio transport with working MCP server |
| `transports/external-mcp.yaml` | HTTP/SSE transport with external servers |
| `platforms/github-mcp.yaml` | Real third-party MCP server (GitHub) |
