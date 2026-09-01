# Examples

Example stacks demonstrating Gridctl patterns and capabilities.

## 🚀 Quick Start

```bash
gridctl apply examples/getting-started/mcp-basic.yaml
```

## 📁 Categories

| Folder | Description |
|--------|-------------|
| [🎯 getting-started/](getting-started/) | Basic examples to get up and running |
| [🔌 transports/](transports/) | MCP transport types: local process, SSH, HTTP, SSE, and external-server auth |
| [📦 platforms/](platforms/) | Third-party MCP servers: remote OAuth endpoints, containers, and host processes |
| [Python sources](python-sources/) | Generate Python containers from exact PyPI releases or packaged source projects |
| [🔗 openapi/](openapi/) | Turn REST APIs into MCP tools via OpenAPI specs |
| [🔐 access-control/](access-control/) | Tool filtering and per-client scoping |
| [⚡ code-mode/](code-mode/) | Reduce context window with search + execute meta-tools |
| [🔒 gateways/](gateways/) | Bridge to existing infrastructure |
| [🖇️ declarative-link/](declarative-link/) | Auto-link LLM clients on apply with a `link:` block |
| [🔑 secrets-vault/](secrets-vault/) | Encrypted variables and variable sets |
| [📈 autoscale/](autoscale/) | Reactive autoscaling of MCP server replicas |
| [🧳 portable-stack/](portable-stack/) | Stack that stays committable by keeping every per-environment value in the variable store |
| [🎒 portable-pack/](portable-pack/) | Pack repo: skills, agents, and rules behind one `gridctl-pack.yaml` |
| [🧭 model-policy/](model-policy/) | Model routing policy projected into LiteLLM and OpenCode config |
| [🔭 tracing/](tracing/) | Distributed tracing and OTLP export |
| [📋 registry/](registry/) | Skills and agents registry ([agentskills.io](https://agentskills.io) spec) |
| [🧪 _mock-servers/](_mock-servers/) | Test servers for development |

## 🎬 Recommended Path

1. **Start here**: `getting-started/mcp-basic.yaml` - stack, networking, tool filtering (placeholder containers)
2. **Real MCP servers**: `transports/local-mcp.yaml` - actual MCP server logic via stdio transport
3. **Platforms**: `platforms/github-mcp.yaml` - third-party MCP servers
4. **OpenAPI**: `openapi/openapi-basic.yaml` - turn any REST API into MCP tools
5. **Python sources**: `python-sources/pypi.yaml` - build an exact package into a container
6. **Registry**: `registry/registry-basic.yaml` - Skills as MCP prompts (imports also discover agents)
7. **Packs**: `portable-pack/` - one manifest importing skills, agents, and rules as a unit
8. **Scaling**: `autoscale/autoscale-basic.yaml` - reactive autoscaling of MCP replicas

> **Note:** Getting-started examples use placeholder containers to focus on infrastructure concepts.
> Transport and platform examples include real MCP server implementations.

## 📊 Feature Matrix

| Example | Transport | Demonstrates |
|---------|-----------|--------------|
| mcp-basic | http (containers) | Multiple servers, tool filtering |
| skills-basic | http (container) | Skills registry alongside a stack |
| local-mcp | stdio | Local host processes |
| ssh-mcp | ssh+stdio | Remote servers over SSH |
| external-mcp | http, sse | External URL servers |
| external-auth | http | External-server auth: oauth, bearer, header |
| atlassian-mcp | http (remote URL) | Hosted platform server with OAuth brokering |
| chrome-devtools-mcp | stdio (host process) | Browser automation via npx |
| context7-mcp | stdio (host process) | Library docs via npx |
| github-mcp | stdio (container) | Official containerized platform server |
| pypi | stdio (generated container) | Exact public PyPI release, automatic console command, pinned Python/uv bases |
| zapier-mcp | http (remote URL) | Hosted platform server with OAuth brokering |
| openapi-basic | openapi | REST API as MCP tools, operation filtering |
| openapi-auth | openapi | Bearer, header, query, OAuth2, basic auth, and mTLS |
| tool-filtering | http (containers) | Server-level tool whitelists |
| per-client-scoping | http (containers) | `clients:` blocks restricting servers and tools per client |
| code-mode-basic | http (containers) | Search + execute meta-tools |
| gateway-basic | http | Gateway to an existing MCP server, tokenizer config |
| gateway-remote | http | Gateway to a remote MCP deployment |
| var-basic | stdio, http | `${var:KEY}` references and value-free `variables:` declarations |
| var-sets | http (containers) | Variable sets fanning out to all workloads |
| var-sets-scoped | stdio | Variable sets scoped to named servers and resources |
| vault-basic | stdio, http | Deprecated `${vault:}` alias (regression fixture) |
| vault-sets | http (container) | Deprecated vault sets (regression fixture) |
| autoscale-basic | stdio | Reactive autoscaling with `autoscale:` |
| otlp-jaeger | - | Gateway OTLP trace export |
| registry-basic | stdio | Skills as MCP prompts, single server |
| registry-advanced | stdio | Two servers; comments show cross-server `allowed-tools` |
| model-preferences | http | Model preference defaults for projected skills and agents |
| skills.yaml | - (skill sources) | Remote git skill sources for `gridctl skill update` |
| declarative-link | stdio (container) | `link:` block, `groups:` endpoints |
| portable-stack | http (containers) | Committable stack, all values from the variable store |
| portable-pack | - (pack manifest) | Skills, agents, rules, and wiring from one manifest |
| model-policy | - (models policy) | Router-only LiteLLM fragment, include line, OpenCode provider |

## 💻 Usage Pattern

All examples follow the same deployment pattern:

```bash
# Deploy a stack
gridctl apply examples/<category>/<file>.yaml

# View status
gridctl status

# Tear down
gridctl destroy examples/<category>/<file>.yaml
```
