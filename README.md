<p align="center">
  <img alt="gridctl" src="assets/gridctl.png" width="420">
</p>

<p align="center">
  <strong>One endpoint. Dozens of AI tools. Zero configuration drift.</strong>
</p>

<p align="center">
  <a href="https://github.com/gridctl/gridctl/releases"><img src="https://img.shields.io/github/v/release/gridctl/gridctl?include_prereleases&style=flat-square&color=f59e0b" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-f59e0b?style=flat-square" alt="License"></a>
  <a href="https://github.com/gridctl/gridctl/actions"><img src="https://img.shields.io/github/actions/workflow/status/gridctl/gridctl/gatekeeper.yaml?style=flat-square&label=build" alt="Build"></a>
  <a href="https://goreportcard.com/report/github.com/gridctl/gridctl"><img src="https://goreportcard.com/badge/github.com/gridctl/gridctl?style=flat-square" alt="Go Report"></a>
</p>

---

![Gridctl](assets/gridctl.gif)

Gridctl aggregates tools from multiple [MCP](https://modelcontextprotocol.io/) servers into a single gateway. Connect Claude Desktop - or any MCP client - to your grid through one endpoint and start building.

Define your stack in YAML. Deploy with one command. Done.

```bash
gridctl deploy stack.yaml
```

> [!NOTE]
> **Inspiration** - This project was heavily influenced by [Containerlab](https://containerlab.dev), a project I've used heavily over the years to rapidly prototype repeatable environments for the purpose of validation, learning, and teaching. Just like Containerlab, Gridctl is designed for fast, ephemeral, stateless, and disposable environments.

## ⚡️ Why Gridctl

MCP servers are everywhere. Running them shouldn't require a PhD in container orchestration. Or, is the MCP server not running in a container? Is a single endpoint exposed behind an existing platform? Is another team hosting and managing an MCP server that is on a different machine on the same network? Different transport types, methods of hosting, and `.json` files start to accumulate like dust. 

I originally built this project to have a way to leverage a single configuration in my application, that I never have to update, while still building various combinations of MCP servers and Agents for rapid prototyping and learning.

I would rather be building than juggling ports, tracking environment variables, and hoping everything with my setup is ready for the next demo, no matter what servers or agents I'm using. My client now connects once and accesses everything over `localhost:8180/sse` by default.

```yaml
# This is all you need!
mcp-servers:

  # Build GitHub MCP locally (instantiate in Docker container)
  - name: github
    image: ghcr.io/github/github-mcp-server:latest
    transport: stdio
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_PERSONAL_ACCESS_TOKEN}"

  # Connects to external SaaS/Cloud Atlassian Rovo MCP Server (breaks out into OAuth to connect)
  - name: atlassian
    command: ["npx", "mcp-remote", "https://mcp.atlassian.com/v1/sse"]

  # Local filesystem via local process execution
  - name: filesystem
    command: ["npx", "-y", "@modelcontextprotocol/server-filesystem", "/Users/home/code/project"]
```

Three servers. Three different transports. One endpoint.

## Installation

```bash
# macOS / Linux
brew install gridctl/tap/gridctl
```

<details>
<summary>Other installation methods</summary>

```bash
# From source
git clone https://github.com/gridctl/gridctl
cd gridctl && make build

# Binary releases available at:
# https://github.com/gridctl/gridctl/releases
```

</details>

## 🚦 Quick Start

```bash
# Deploy the example stack
gridctl deploy examples/getting-started/skills-basic.yaml

# Check what's running
gridctl status

# Open the web UI
open http://localhost:8180

# Clean up
gridctl destroy examples/getting-started/skills-basic.yaml
```

## 🎬 Features

### Stack as Code

Fast, consistent, ephemeral, flexible, and version controlled! Many practitioners use different combinations of `MCP Servers` and `Agents` depending on what they are working on. Being able to instantiate, from a single file, the various combinations needed for the right task, saves time in _development_ and _prototyping_. The `stack.yaml` file is where you define this.

### Protocol Bridge

Aggregates tools from HTTP servers, stdio processes, SSH tunnels, and external URLs into a unified gateway. Automatic namespacing (`server__tool`) prevents collisions.

### Transport Flexibility

| Transport | Config | When to Use |
|:----------|:-------|:------------|
| **Container HTTP** | `image` + `port` | Dockerized MCP servers |
| **Container Stdio** | `image` + `transport: stdio` | Servers using stdin/stdout |
| **Local Process** | `command` | Host-native MCP servers |
| **SSH Tunnel** | `command` + `ssh.host` | Remote machine access |
| **External URL** | `url` | Existing infrastructure |

### Context Window Optimization _(access control)_

Are you paying for your own tokens for learning? Even if you aren't, being optimized is critical for not overloading that context window! Reducing the numbers of tools and scoping things out correctly, significantly reduces the likelihood of _"tool confusion"_ e.g., a given LLM selects a similarly named tool from the wrong server.

By using `uses` and `tools` filters in the _stack.yaml_ file, `gridctl` filters this list *before* it reaches the LLM. This way, you only get what you need. This is implemented at two levels:

#### Server-Level Filtering (`pkg/mcp/client.go`)

When `gridctl` initializes a connection to a downstream MCP server, it applies a whitelist during the `RefreshTools` phase.

```go
if len(c.toolWhitelist) > 0 {
    // Only tools in the whitelist are stored in the client's internal cache
    c.tools = filteredTools
}
```

#### Agent-Level Filtering (`pkg/mcp/gateway.go`)

The `Gateway` validates every tool list request and tool call against the agent's specific `ToolSelector` configuration.
- `HandleToolsListForAgent`: Filters the aggregated tool list dynamically based on the requesting agent's identity.
- `HandleToolsCallForAgent`: Provides a security layer by rejecting execution attempts for unauthorized tools, even if the model somehow knows the tool name.



#### Filtering in Action

**Server-Level Filtering** - Restrict which tools the server exposes to the gateway:

```yaml
mcp-servers:
  - name: github
    image: ghcr.io/github/github-mcp-server:latest
    transport: stdio
    tools: ["get_file_contents", "search_code", "list_commits", "get_issue", "get_pull_request"]
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_PERSONAL_ACCESS_TOKEN}"
```

This GitHub server only exposes read-only tools. Write operations like `create_issue` and `create_pull_request` are hidden from all agents.

**Agent-Level Filtering** - Further restrict which tools a specific agent can access:

```yaml
agents:
  - name: code-review-agent
    image: my-org/code-review:latest
    description: "Reviews pull requests and provides feedback"
    uses:
      - server: github
        tools: ["get_file_contents", "get_pull_request", "list_commits"]
```

This agent can only access three of the five tools exposed by the GitHub server - just enough to review code without searching the broader codebase.

### A2A Protocol

Limited [Agent-to-Agent](https://google.github.io/A2A/) protocol support. Expose your agents via `/.well-known/agent.json` or connect to remote A2A agents. Agents can use other agents as tools. `A2A` is still emerging, as is the common use-cases. This part of the project will continue to evolve in the future.

## 📚 CLI Reference

```bash
gridctl deploy <stack.yaml>          # Start containers and gateway
gridctl deploy <stack.yaml> -f       # Run in foreground (debug mode)
gridctl deploy <stack.yaml> -p 9000  # Custom gateway port
gridctl status                       # Show running stacks
gridctl destroy <stack.yaml>         # Stop and remove containers
```

## 🖥️ Connect LLM Application

Each LLM host, the client side application you use to connect the models and chat, will always keep the following configuration. The location of this file varies on the application. For instance, if using `Claude Desktop` on a Macbook, you would place the configuration here: `~/Library/Application Support/Claude/claude_desktop_config.json`:

### Most Applications
```json
{
  "mcpServers": {
    "gridctl": {
      "url": "http://localhost:8180/sse"
    }
  }
}
```

### Claude Desktop
```json
{
  "mcpServers": {
    "gridctl": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "http://localhost:8180/sse", "--allow-http", "--transport", "sse-only"]
    }
  }
}
```

Restart Claude Desktop. All tools from your stack are now available.

## 📙 Examples

| Example | What It Shows |
|:--------|:--------------|
| [`skills-basic.yaml`](examples/getting-started/skills-basic.yaml) | Agents with A2A protocol |
| [`tool-filtering.yaml`](examples/access-control/tool-filtering.yaml) | Server and agent-level access control |
| [`local-mcp.yaml`](examples/transports/local-mcp.yaml) | Local process transport |
| [`ssh-mcp.yaml`](examples/transports/ssh-mcp.yaml) | SSH tunnel transport |
| [`external-mcp.yaml`](examples/transports/external-mcp.yaml) | External HTTP/SSE servers |
| [`github-mcp.yaml`](examples/platforms/github-mcp.yaml) | GitHub MCP server integration |
| [`basic-a2a.yaml`](examples/multi-agent/basic-a2a.yaml) | Agent-to-agent communication |

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). We welcome PRs for new transport types, example stacks, and documentation improvements.

## 🪪 License

[Apache 2.0](LICENSE)

---

<p align="center">
  <sub>Built for engineers who'd rather be building and hate the absence of repeatable environments!</sub>
</p>
