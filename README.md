<p align="center">
  <img alt="gridctl" src="assets/gridctl.png" width="420">
</p>

<p align="center">
  <strong>MCP gateway with a built-in skill library.</strong>
</p>

<p align="center">
  <em>One YAML. One endpoint. Every MCP server plus the skills you author alongside them.</em>
</p>

<p align="center">
  <a href="https://github.com/gridctl/gridctl/releases"><img src="https://img.shields.io/github/v/release/gridctl/gridctl?include_prereleases&style=flat-square&color=f59e0b" alt="Release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-f59e0b?style=flat-square" alt="License"></a>
  <a href="https://github.com/gridctl/gridctl/actions"><img src="https://img.shields.io/github/actions/workflow/status/gridctl/gridctl/gatekeeper.yaml?style=flat-square&label=build" alt="Build"></a>
  <a href="https://goreportcard.com/report/github.com/gridctl/gridctl"><img src="https://goreportcard.com/badge/github.com/gridctl/gridctl?style=flat-square" alt="Go Report"></a>
  <a href="SECURITY.md"><img src="https://img.shields.io/badge/Security-Policy-f59e0b?style=flat-square" alt="Security Policy"></a>
  <a href="https://www.bestpractices.dev/projects/12295"><img src="https://www.bestpractices.dev/projects/12295/badge" alt="OpenSSF Best Practices"></a>
</p>

---

![Gridctl](assets/gridctl.gif)

Gridctl aggregates tools from [MCP](https://modelcontextprotocol.io/) servers into a single gateway and serves [Agent Skills](https://agentskills.io) as MCP prompts to upstream clients. Define your stack in YAML, apply with one command, and connect Claude Desktop — or any MCP client — through one endpoint.

```bash
gridctl apply stack.yaml
```

Designed for fast, ephemeral, stateless environments — inspired by [Containerlab](https://containerlab.dev).

## ⚡️ Why gridctl

MCP servers are everywhere — different transports, different hosting models, different `.json` files accumulating like dust. Skills are a separate sprawl on top. Switching projects shouldn't mean rewriting every client config.

Gridctl gives you one declarative file for everything you want connected, one local endpoint your client talks to, and a UI that shows you what's actually running. Build fast, throw it away, rebuild it tomorrow.

```yaml
version: "1"
name: stack

mcp-servers:

  # Containerized stdio MCP server
  - name: github
    image: ghcr.io/github/github-mcp-server:latest
    transport: stdio
    tools: ["get_file_contents", "search_code", "list_commits", "get_pull_request"]
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_PERSONAL_ACCESS_TOKEN}"

  # External SaaS MCP server (OAuth flow)
  - name: atlassian
    command: ["npx", "mcp-remote", "https://mcp.atlassian.com/v1/sse"]

  # Any REST API as MCP tools via OpenAPI
  - name: my-api
    openapi:
      spec: https://api.example.com/openapi.json
      baseUrl: https://api.example.com
```

Three servers, three transports, one endpoint. Navigate to [localhost:8180](http://localhost:8180) to visualize the stack 👉

![Gridctl Interface](assets/gridctl-ui.gif)

## 🪛 Install

```bash
curl -fsSL https://raw.githubusercontent.com/gridctl/gridctl/main/install.sh | sh
```

Installs the latest release to `~/.local/bin/gridctl`. Full instructions for Homebrew, pre-built binaries, building from source, container runtime setup, and updating/uninstalling are in the [Installation guide](docs/installation.md).

## 🚦 Quick Start

```bash
# Apply the example stack
gridctl apply examples/getting-started/skills-basic.yaml

# Check what's running
gridctl status

# Open the web UI
open http://localhost:8180

# Clean up
gridctl destroy examples/getting-started/skills-basic.yaml
```

## 🖥️ Connect LLM Application

The easiest way to connect is with `gridctl link`, which auto-detects installed LLM clients and injects the gateway configuration:

```bash
gridctl link              # Interactive: detect and select clients
gridctl link claude       # Link a specific client
gridctl link --all        # Link all detected clients at once
```

Supported clients: Claude Desktop, Claude Code, Cursor, Windsurf, VS Code, Gemini, OpenCode, Continue, Cline, AnythingLLM, Roo, Zed, Goose

<details>
<summary>Manual configuration</summary>

#### Most Applications
```json
{
  "mcpServers": {
    "gridctl": {
      "url": "http://localhost:8180/sse"
    }
  }
}
```

#### Claude Desktop
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

Restart Claude Desktop after editing. All tools from your stack are now available.

</details>

## 🎬 Features

### Stack as Code

Declarative, version-controlled MCP environments. Validate before you commit, plan before you apply, and detect the moment your environment drifts from what's in version control. Drift detection runs in the background — the canvas flags servers running but absent from your spec, and declarations in your spec that haven't been deployed.

```bash
gridctl validate stack.yaml    # Lint and schema-check the spec (exit 0/1/2)
gridctl plan stack.yaml        # Diff against running state
gridctl apply stack.yaml       # Apply the spec
gridctl export                 # Reverse-engineer stack.yaml from a running stack
```

Learn more → [Configuration Reference](docs/config-schema.md)

### `gridctl optimize` & Cost Observability

Every tool call is priced against an embedded snapshot of LiteLLM model rates. `gridctl optimize` scans the running gateway and surfaces actionable findings with weekly USD impact — unused servers, unused tools, schema overhead, format-conversion shortfalls, and expensive-model-on-cheap-task patterns — plus a paste-ready YAML remediation for each.

```bash
gridctl optimize                          # styled findings table
gridctl optimize --format json            # machine-readable OptimizeReport
gridctl optimize --severity warn,critical # narrow to actionable findings
```

Learn more → [Cost Observability](docs/cost-observability.md)

### Output Format Conversion

Tool call results default to JSON. Set `output_format` at the gateway or per-server level to convert structured responses into `TOON` or `CSV` before they reach the client — reducing token consumption by **25–61%** for tabular and key-value data. Non-JSON responses and payloads over 1 MB are passed through unchanged.

```yaml
gateway:
  output_format: toon      # Default for all servers: json, toon, csv, text

mcp-servers:
  - name: analytics
    output_format: csv     # Override per server
```

Learn more → [Configuration Reference](docs/config-schema.md)

### Skill Library

Every `SKILL.md` in your registry surfaces to upstream MCP clients as a prompt. Author in the Library workspace in the web UI (or via `gridctl skill *` on the CLI), activate, and the prompt becomes available to Claude Desktop, Claude Code, Cursor, Codex — anything that speaks MCP.

```bash
gridctl skill list                        # Show what's in the registry
gridctl skill add <git-repo>              # Import skills from a remote repo
gridctl activate my-skill                 # Promote a draft → active
```

Skills follow the [agentskills.io specification](https://agentskills.io) — author them as plain markdown with frontmatter and they work with every skill-aware client, not just gridctl.

Learn more → [Skills guide](docs/skills.md)

## 📙 Examples

| Example | What It Shows |
|:--------|:--------------|
| [`mcp-basic.yaml`](examples/getting-started/mcp-basic.yaml) | Stack with multiple MCP servers and tool filtering |
| [`local-mcp.yaml`](examples/transports/local-mcp.yaml) | Local process and SSH-tunneled MCP transports |
| [`openapi-basic.yaml`](examples/openapi/openapi-basic.yaml) | Turn a REST API into MCP tools via OpenAPI spec |
| [`code-mode-basic.yaml`](examples/code-mode/code-mode-basic.yaml) | Gateway code mode with search + execute meta-tools |
| [`github-mcp.yaml`](examples/platforms/github-mcp.yaml) | GitHub MCP server integration |
| [`registry-basic.yaml`](examples/registry/registry-basic.yaml) | Skills registry with a single server |
| [`var-basic.yaml`](examples/secrets-vault/var-basic.yaml) | Reference variable-store secrets with `${var:KEY}` syntax |
| [`otlp-jaeger.yaml`](examples/tracing/otlp-jaeger.yaml) | Export traces to Jaeger via OTLP |

## 📖 Documentation

- **Getting started** — [Installation](docs/installation.md)
- **Reference** — [CLI](docs/cli-reference.md) · [Configuration](docs/config-schema.md) · [REST API](docs/api-reference.md)
- **Guides** — [Skills](docs/skills.md) · [Scaling](docs/scaling.md) · [Cost Observability](docs/cost-observability.md)
- **Operations** — [Project Status](docs/project-status.md) · [Troubleshooting](docs/troubleshooting.md)

Full index at [`docs/`](docs/README.md).

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). PRs welcome for new transport types, example stacks, and documentation improvements.

## 🪪 License

[Apache 2.0](LICENSE)

---

<p align="center">
  <sub>Built for engineers who'd rather be building and hate the absence of repeatable environments.</sub>
</p>
