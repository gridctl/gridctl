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
| [Python runtime base](python-runtime/) | Manual derivatives of the Python MCP runtime foundation image |
| [Execution controls](execution/) | Opt-in container restrictions and per-replica enforcement evidence |
| [Security evidence](security-evidence/) | Partial, stale, suppressed, unknown, and N/A states for `gridctl doctor --security` |
| [Stack declaration policy](stack-declaration-policy/) | Offline `validate --policy` fixture, policy file, and CI workflow design |
| [🔗 openapi/](openapi/) | Turn REST APIs into MCP tools via OpenAPI specs |
| [A2A](a2a/) | Experimental outbound Agent Card adapter with secret task/context handles |
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
| [Run records](runs/) | Opt-in metadata-only persisted dispatch records |
| [📋 registry/](registry/) | Skills and agents registry ([agentskills.io](https://agentskills.io) spec) |
| [🧪 _mock-servers/](_mock-servers/) | Test servers for development |

## 🎬 Recommended Path

1. **Start here**: `getting-started/mcp-basic.yaml` - stack, networking, tool filtering (digest-pinned alpine placeholders)
2. **Real MCP servers**: `transports/local-mcp.yaml` - actual MCP server logic via stdio transport
3. **Platforms**: `platforms/github-mcp.yaml` - third-party MCP servers
4. **OpenAPI**: `openapi/openapi-basic.yaml` - turn any REST API into MCP tools
5. **Python sources**: `python-sources/pypi.yaml` - start with one exact package, then try `daily.yaml` for PyPI and Git together
6. **Registry**: `registry/registry-basic.yaml` - Skills as MCP prompts (imports also discover agents)
7. **Packs**: `portable-pack/` - one manifest importing skills, agents, and rules as a unit
8. **Scaling**: `autoscale/autoscale-basic.yaml` - reactive autoscaling of MCP replicas

> **Note:** Getting-started examples use digest-pinned `alpine:3.22` placeholders (`sleep`) to focus on infrastructure concepts, not MCP server logic.
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
| daily | stdio (generated containers) | Exact PyPI release and commit-pinned Git project in one stack |
| python-runtime/locked | stdio (custom Dockerfile) | Locked-project derivative of the Python runtime base with UID 10001 |
| python-runtime/hashed | stdio (custom Dockerfile) | Hashed-wheel derivative of the Python runtime base with UID 10001 |
| execution/stack | stdio (container) | Non-root echo server with finite resources, read-only root, bounded scratch, and network none |
| zapier-mcp | http (remote URL) | Hosted platform server with OAuth brokering |
| openapi-basic | openapi | REST API as MCP tools, operation filtering |
| openapi-auth | openapi | Bearer, header, query, OAuth2, basic auth, and mTLS |
| a2a/stack | A2A JSON-RPC | Off-by-default remote agent tools, card trust, and capability-based task access |
| tool-filtering | http (containers) | Server-level tool whitelists |
| per-client-scoping | http (containers) | `clients:` blocks restricting servers and tools per client |
| code-mode-basic | http (containers) | Search + execute meta-tools |
| gateway-basic | http | Gateway to an existing MCP server, tokenizer config |
| gateway-remote | http | Authenticated remote gateway access through HTTPS or an encrypted tunnel |
| var-basic | stdio, http | `${var:KEY}` references and value-free `variables:` declarations |
| var-sets | http (containers) | Variable sets fanning out to all workloads |
| var-sets-scoped | stdio | Variable sets scoped to named servers and resources |
| vault-basic | stdio, http | Deprecated `${vault:}` alias (regression fixture) |
| vault-sets | http (container) | Deprecated vault sets (regression fixture) |
| autoscale-basic | stdio | Reactive autoscaling with `autoscale:` |
| otlp-jaeger | - | Gateway OTLP trace export |
| runs/stack | - | Opt-in metadata-only persisted dispatch records |
| registry-basic | stdio | Skills as MCP prompts, single server |
| registry-advanced | stdio | Two servers; comments show cross-server `allowed-tools` |
| model-preferences | http | Model preference defaults for projected skills and agents |
| skills.yaml | - (skill sources) | Remote git skill sources for `gridctl skill update` |
| declarative-link | stdio (container) | `link:` block, `groups:` endpoints |
| portable-stack | http (containers) | Committable stack, all values from the variable store |
| portable-pack | - (pack manifest) | Skills, agents, rules, and wiring from one manifest |
| model-policy | - (models policy) | Router-only LiteLLM fragment, include line, OpenCode provider |
| declaration-policy-demo | http (container) | Offline `validate --policy` against digest-pinned images and pinning block |

## Dependency references

Runnable public images in these stacks are pinned to a reviewed multi-platform index digest with a version tag. Direct `npx` selectors use an exact release. Exact versions are not a transitive lock, and a digest is content-addressed, not publisher-verified.

Reviewed on 2026-09-14 (linux/amd64 and linux/arm64 unless noted):

| Selector | Pin | Notes |
|---|---|---|
| alpine:3.22 | sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce | Official index; also linux/arm/v6, arm/v7, 386, ppc64le, riscv64, s390x |
| python:3.13-alpine | sha256:7415fbc3c9e4979cc717d92377ab2bc7b2b4a2af1ac03cc52b5f3f88efedaf3a | Official index |
| postgres:16 | sha256:f1c3376c26f2609ab9f29f71f824103fe2fcd8ee0346485cb6122a4f93df6f94 | Official index |
| ghcr.io/github/github-mcp-server:v1.12.1 | sha256:0ba840c46a237879c8300e7fddb0b6347f20e029ccb9cbe2ce4a943daa1ff560 | linux/amd64, linux/arm64 |
| chrome-devtools-mcp | 1.9.0 | npm latest at review |
| @upstash/context7-mcp | 4.1.0 | npm latest at review |
| @playwright/mcp | 0.0.80 | npm latest at review |
| mcp-remote | 0.14.2 | npm latest at review; README Claude Desktop setup snippet |

Placeholder private or fake images stay as authored (`ghcr.io/org/...`, `example/fetch:1`, `my-mcp:latest`, `my/filesystem-mcp:latest`, `my-image:latest`). They are listed in `examples/reference-exceptions.txt` and must not be swapped for unrelated public software.

Excluded from this pin policy: commented sketches, local mock-server paths, host `sleep` commands, variable-only selectors, schematic `npx some-stdio-mcp-server` forms, API status payloads, and runtime-generated `gridctl link` client wiring. Public setup snippets, including the README Claude Desktop `npx` bridge, are pinned.

Owner: repository maintainers. Cadence: when adding or changing runnable examples or public setup snippets; review pins when promoting a new upstream release, and at least quarterly. Check with `task examples:refs` (requires `./gridctl`).

## 💻 Usage Pattern

Most examples follow the same deployment pattern. The [stack declaration policy](stack-declaration-policy/) fixture is evaluated with `gridctl validate --policy` and is not a deploy demo.

```bash
# Deploy a stack
gridctl apply examples/<category>/<file>.yaml

# View status
gridctl status

# Tear down
gridctl destroy examples/<category>/<file>.yaml
```
