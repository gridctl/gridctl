# 🎯 Getting Started

These examples exercise stack structure only. The first example that serves a tool is [`python-sources/pypi.yaml`](../python-sources/pypi.yaml).

## 📄 Examples

| File | Description |
|------|-------------|
| `mcp-basic.yaml` | Multiple MCP servers with tool filtering (structure only; no live tool) |
| `skills-basic.yaml` | MCP server with skills registry integration |

## 🚀 Quick Start

```bash
# Deploy the basic stack
gridctl apply examples/getting-started/mcp-basic.yaml

# Check status
gridctl status
```

## 📖 What You'll Learn

- Defining MCP servers in a stack
- Server-level tool filtering with `tools:`
- Environment variable expansion

## ℹ️ Note: Infrastructure vs Application Logic

These examples use **placeholder containers** (digest-pinned `alpine:3.22` with `sleep`) to demonstrate
stack structure and tool filtering - not actual MCP server logic.

To see examples with real MCP servers:

| Example | What it demonstrates |
|---------|---------------------|
| `python-sources/pypi.yaml` | The first example that serves a tool (`fetch`) |
| `transports/local-mcp.yaml` | Stdio transport with working MCP server |
| `transports/external-mcp.yaml` | HTTP/SSE transport with external servers |
| `platforms/github-mcp.yaml` | Real third-party MCP server (GitHub) |
