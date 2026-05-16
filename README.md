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
  <a href="SECURITY.md"><img src="https://img.shields.io/badge/Security-Policy-f59e0b?style=flat-square" alt="Security Policy"></a>
  <a href="https://www.bestpractices.dev/projects/12295"><img src="https://www.bestpractices.dev/projects/12295/badge" alt="OpenSSF Best Practices"></a>
</p>

---

![Gridctl](assets/gridctl.gif)

Gridctl aggregates tools from multiple [MCP](https://modelcontextprotocol.io/) servers into a single gateway. Connect Claude Desktop - or any MCP client - to your grid through one endpoint and start building.

Define your stack in YAML. Apply with one command. Done.

```bash
gridctl apply stack.yaml
```

> [!NOTE]
> **Inspiration** - This project was heavily influenced by [Containerlab](https://containerlab.dev), a project I've used heavily over the years to rapidly prototype repeatable environments for the purpose of validation, learning, and teaching. Just like Containerlab, Gridctl is designed for fast, ephemeral, stateless, and disposable environments.

## ⚡️ Why Gridctl

MCP servers are everywhere. Running them shouldn't require a PhD in container orchestration. Or, is the MCP server not running in a container? Is a single endpoint exposed behind an existing platform? Is another team hosting and managing an MCP server that is on a different machine on the same network? Different transport types, methods of hosting, and `.json` files start to accumulate like dust. 

I originally built this project to have a way to leverage a single configuration in my application, that I never have to update, while still building various combinations of MCP servers for rapid prototyping and learning.

I would rather be building than juggling ports, tracking environment variables, and hoping everything with my setup is ready for the next demo. My client now connects once and accesses everything over `localhost:8180/sse` by default.

```yaml
version: "1"
name: stack

mcp-servers:

  # Build GitHub MCP locally (instantiate in Docker container)
  - name: github
    image: ghcr.io/github/github-mcp-server:latest
    transport: stdio
    tools: ["get_file_contents", "search_code", "list_commits", "get_pull_request"]
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_PERSONAL_ACCESS_TOKEN}"

  # Connects to external SaaS/Cloud Atlassian Rovo MCP Server (breaks out into OAuth to connect)
  - name: atlassian
    command: ["npx", "mcp-remote", "https://mcp.atlassian.com/v1/sse"]

  # Turn any REST API into MCP tools via OpenAPI spec
  - name: my-api
    openapi:
      spec: https://api.example.com/openapi.json
      baseUrl: https://api.example.com
```

Three servers. Three different transports. One endpoint. Navigate to [localhost:8180](localhost:8180) to visualize the stack 👉

![Gridctl Interface](assets/gridctl-ui.gif)


## 🪛 Installation

### Quick install (macOS, Linux, WSL2)

```bash
curl -fsSL https://raw.githubusercontent.com/gridctl/gridctl/main/install.sh | sh
```

Installs the latest release to `~/.local/bin/gridctl`. The script verifies the
release checksum and prints the install path and next steps.

The script can be inspected before running:

```bash
curl -fsSL https://raw.githubusercontent.com/gridctl/gridctl/main/install.sh | less
```

> **Windows**: install [WSL2](https://learn.microsoft.com/en-us/windows/wsl/install), then run the command above inside your Linux distribution.

![Install Gridctl](assets/install.gif)

### Package managers

<details>
<summary><strong>Homebrew</strong> (macOS, Linux)</summary>

```bash
brew install gridctl/tap/gridctl
```

Update with `brew upgrade gridctl/tap/gridctl`.

</details>

### Other options

<details>
<summary><strong>Pre-built binaries</strong></summary>

Download the tarball for your platform from the [releases page](https://github.com/gridctl/gridctl/releases),
verify it against `checksums.txt`, extract, and place `gridctl` on your `PATH`.

</details>

<details>
<summary><strong>Build from source</strong></summary>

Requires Go 1.26+ and Node 20+.

```bash
git clone https://github.com/gridctl/gridctl
cd gridctl && make build
./gridctl --help
```

</details>

### Updating

```bash
gridctl upgrade            # check + prompt + upgrade (standalone install)
gridctl upgrade --check    # only check; do not install
gridctl upgrade --yes      # non-interactive (CI)
gridctl upgrade --version v0.1.0-beta.6   # install a specific version
```

If gridctl was installed via Homebrew, `gridctl upgrade` detects that and recommends `brew upgrade gridctl/tap/gridctl` instead.

### Uninstalling

```bash
# Standalone install
curl -fsSL https://raw.githubusercontent.com/gridctl/gridctl/main/install.sh | sh -s -- --uninstall

# Also remove the config directory at ~/.gridctl
curl -fsSL https://raw.githubusercontent.com/gridctl/gridctl/main/install.sh | sh -s -- --uninstall --purge

# Homebrew install
brew uninstall gridctl/tap/gridctl
```

## 🐋 Container Runtime

Gridctl requires a container runtime for workloads that run in containers (MCP servers with `image` and resources). Docker is detected by default; [Podman](https://podman.io) is also fully supported.

### Runtime Detection

Gridctl auto-detects your runtime by probing sockets in this order:

1. `$DOCKER_HOST` (if set)
2. `/var/run/docker.sock` (Docker)
3. `/run/podman/podman.sock` (Podman rootful)
4. `$XDG_RUNTIME_DIR/podman/podman.sock` (Podman rootless)

Override detection with the `--runtime` flag or `GRIDCTL_RUNTIME` environment variable:

```bash
gridctl apply stack.yaml --runtime podman
# or
GRIDCTL_RUNTIME=podman gridctl apply stack.yaml
```

### Using Podman

```bash
# Install Podman (macOS)
brew install podman
podman machine init
podman machine start

# Install Podman (Linux)
sudo apt install podman        # Debian/Ubuntu
sudo dnf install podman        # Fedora/RHEL

# Enable the Podman socket (Linux rootless)
systemctl --user enable --now podman.socket

# Verify gridctl detects Podman
gridctl info
```

Podman 4.0+ is required for rootless multi-container networking (netavark + aardvark-dns). Podman 4.7+ is recommended for full `host.containers.internal` support. Older versions fall back to the Docker-compatible `host.docker.internal` alias. SELinux volume labels (`:Z`) are applied automatically when Podman is running on an SELinux-enforcing system.

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

## 🎬 Features

### Stack as Code

Fast, consistent, ephemeral, flexible, and version controlled! Many practitioners use different combinations of MCP servers depending on what they are working on. Being able to instantiate, from a single file, the various combinations needed for the right task, saves time in _development_ and _prototyping_. The `stack.yaml` file is where you define this.

### Spec-Driven Workflow

The `stack.yaml` file has always been your source of truth. Now you have the full lifecycle tooling to match - validate before you commit, preview before you apply, and detect the moment your environment drifts from what's in version control:

```bash
gridctl validate stack.yaml    # Lint and schema-check the spec (exit 0/1/2)
gridctl plan stack.yaml        # Diff against running state - see exactly what changes
gridctl apply stack.yaml       # Apply the spec
gridctl export                 # Reverse-engineer stack.yaml from a running stack
gridctl test <skill>           # Run acceptance criteria for a skill (exit 0/1/2)
gridctl activate <skill>       # Promote a skill from draft to active
```

Drift detection runs in the background: the canvas flags servers that are running but absent from your spec, and declarations in your spec that haven't been deployed - so your YAML and your environment stay in sync. Need to build a stack from scratch? Start the UI with `gridctl serve`, use the visual spec builder to compose your stack through a guided wizard, then **Save & Load** it directly into the running daemon - no YAML file required to get started.

Code skills (those with a `skill.ts` or `skill.go` handler) must define `acceptance_criteria` in `SKILL.md` frontmatter before `gridctl activate` will promote them - ensuring every deployed skill has a machine-checkable definition of done.

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
| **OpenAPI Spec** | `openapi.spec` | Any REST API with an OpenAPI spec |

### Context Window Optimization _(access control)_

Are you paying for your own tokens for learning? Even if you aren't, being optimized is critical for not overloading that context window! Reducing the number of tools and scoping things correctly significantly reduces the likelihood of _"tool confusion"_ - where a given LLM selects a similarly named tool from the wrong server.

Use the `tools` filter in the _stack.yaml_ file to whitelist exactly which tools each server exposes. `gridctl` filters this list *before* it reaches the LLM:

```yaml
mcp-servers:
  - name: github
    image: ghcr.io/github/github-mcp-server:latest
    transport: stdio
    tools: ["get_file_contents", "search_code", "list_commits", "get_issue", "get_pull_request"]
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_PERSONAL_ACCESS_TOKEN}"
```

This GitHub server only exposes read-only tools. Write operations like `create_issue` and `create_pull_request` are hidden from all clients.

### Output Format Conversion

Tool call results default to JSON. Set `output_format` at the gateway or per-server level to convert structured responses into TOON or CSV before they reach the client - reducing token consumption by 25–61% for tabular and key-value data.

```yaml
gateway:
  output_format: toon      # Default for all servers: json, toon, csv, text

mcp-servers:
  - name: analytics
    image: my-org/analytics:latest
    port: 8080
    output_format: csv      # Override: this server returns CSV
```

| Format | Best For | Savings |
|--------|----------|---------|
| `toon` | Key-value pairs, nested objects | ~25–40% |
| `csv` | Tabular / array-of-objects data | ~40–61% |
| `text` | Raw passthrough (no conversion) | - |
| `json` | Default (no conversion) | - |

Non-JSON responses and payloads over 1MB are passed through unchanged. Per-server settings override the gateway default.

### Code Mode

When a stack exposes dozens of tools, context window consumption grows fast. Code Mode replaces all individual tool definitions with two meta-tools - `search` and `execute` - reducing context overhead by 99%+. LLM agents discover tools via search, then call them through JavaScript executed in a sandboxed [goja](https://github.com/nicholasgasior/goja) runtime.

```yaml
gateway:
  code_mode: "on"
  code_mode_timeout: 30     # Execution timeout in seconds (default: 30)
```

Or enable via CLI flag:

```bash
gridctl apply stack.yaml --code-mode
```

The sandbox provides `mcp.callTool(serverName, toolName, args)` for synchronous tool calls and `console.log/warn/error` for output capture. Modern JavaScript syntax (arrow functions, destructuring, template literals) is supported via esbuild transpilation. See [`examples/code-mode/`](examples/code-mode/) for a working example.

### Skills

Skills are the primary way to extend the gateway's behavior. They live in `~/.gridctl/registry/skills/<name>/` and come in three flavors - file presence is the discriminator, not a `kind:` field in the frontmatter:

| Flavor | Files | Surfaces as | Runtime |
|---|---|---|---|
| **Prompt-only** | `SKILL.md` | MCP prompt + tool | None - the markdown body is the artifact |
| **Code (TypeScript)** | `SKILL.md` + `skill.ts` + `agent.json` | MCP tool (typed I/O) | `goja` + `esbuild` sandbox (in-process) |
| **Code (Go)** | `SKILL.md` + `skill.go` + `skill_test.go` | MCP tool (typed I/O) | Go plugin (`go build -buildmode=plugin`) |

**Prompt-only** skills are prose the LLM consumes verbatim - runbooks, severity matrices, personas. The markdown body is delivered to upstream clients (Claude Desktop, IDE, CLI) without an intermediate handler.

**Code skills** are logic - they call `tool()`, `llm()`, `parallel()`, `handoff()`, and `approval()` bindings. The TS path runs in-process via a `goja` JavaScript sandbox with `esbuild` transpilation; the Go path compiles into a `.so` plugin the gateway loads at start. Both register through one `*skill.Registry`, so a TS skill, a Go skill, and an upstream MCP client all reach the same skill through one code path.

**Hybrid pattern** - a code skill can read its own `SKILL.md` body at runtime (`skill.body` in TS, `ctx.SkillBody()` in Go) and feed it to an LLM as the system prompt. Code drives the graph; prose drives the behavior. Edit the markdown, change runtime behavior, no code change.

Skills have three lifecycle states: **draft** (stored, not exposed), **active** (discoverable via MCP), and **disabled** (hidden without deletion). See [docs/skills.md](docs/skills.md) for the full three-flavor reference and [`examples/registry/items/`](examples/registry/items/) for the canonical reference set - `triage-ts/`, `triage-go/`, and `incident-triage-hybrid/`.

```bash
# Scaffold a starter skill in the current directory
gridctl agent init --name my-skill                  # TS (default)
gridctl agent init --lang go --name my-skill        # Go
gridctl agent init --prompt-only --name my-skill    # Prompt-only

# Validate and compile
gridctl agent validate my-skill                     # static check (manifest + handler symbols)
gridctl agent build my-skill                        # esbuild for TS, `go build -buildmode=plugin` for Go

# Run a skill end-to-end and stream typed events
gridctl run my-skill --input '{"topic": "..."}'

# Iterate live with the canvas + trace overlay
gridctl agent dev --root .
```

### Skill Runs and Time-Travel Resume

Every skill invocation writes a JSONL event ledger to `~/.gridctl/runs/<run_id>.jsonl`. Inspect, trace, and resume:

```bash
gridctl runs list                                   # recent runs (newest first)
gridctl runs inspect <run_id>                       # typed event timeline
gridctl runs trace <run_id>                         # OTel-shaped JSON projection
gridctl runs resume <run_id> [--from-step <node>]   # rehydrate state and continue
gridctl runs approve <run_id> --decision approve    # resolve a pending approval gate
```

The Web UI's **Runs** workspace renders the same ledger as a live grid, inspector, and waterfall - fed by a shared SSE stream that stays open across every workspace so the in-flight badge and BottomPanel Runs tab update in real time.

### Private Repositories

Both `gridctl skill add` and MCP server `source` blocks can clone private git repositories. Credentials come from one of three places, in priority order:

1. **Vault reference** _(recommended)_ - the raw token stays in the encrypted vault; only a `${vault:KEY}` reference is persisted to the skill origin / lock file.
2. **Ephemeral flag** - `--auth-token <PAT>` for one-shot CI use; never written to disk.
3. **Ambient environment** - SSH URLs use ssh-agent + `~/.ssh/known_hosts`; HTTPS URLs fall back to `GITHUB_TOKEN` if set.

```bash
# Public repo (unchanged)
gridctl skill add https://github.com/acme/public-skills

# Private HTTPS with a vault-stored PAT (re-resolved on every update)
gridctl vault set GIT_TOKEN ghp_xxxxxxxxxxxxxxxxxxxx
gridctl skill add https://github.com/acme/private-skills --vault-key GIT_TOKEN

# Private HTTPS with an ephemeral PAT (CI use; not persisted)
gridctl skill add https://github.com/acme/private-skills --auth-token "$GIT_TOKEN"

# Private SSH via ambient ssh-agent (no flags)
gridctl skill add git@github.com:acme/private-skills.git
```

`--auth-token` and `--vault-key` are mutually exclusive. Both flags also work on `gridctl skill try`. `gridctl skill update` automatically re-resolves any stored `${vault:KEY}` reference.

In the web wizard, the "Add skill source" step has an inline, collapsible **Authentication** card. It stays collapsed for public repos and auto-expands when a scan returns an auth-class error, offering two modes: pick an existing vault secret or paste a one-shot token. The same subsection is available in the MCP server form when the source type is `git`.

Raw tokens are never written outside the encrypted vault - neither to the skill origin nor the lock file. Error and log paths strip embedded URL userinfo (`https://TOKEN@host/...`) and known PAT patterns (`ghp_…`, `github_pat_…`, `glpat-…`) before they reach the API or CLI.

### Cost Optimize

`gridctl optimize` scans the running gateway and prints findings with a measured weekly USD impact and a paste-ready YAML remediation. The PR-4 heuristics flag **unused servers** (registered but no calls observed in the lookback window) and **unused tools** (a server is active but a specific tool has not been called in the window and is not already excluded). On a fresh gateway with less than 24h of data, `optimize` returns a single info finding so reports never over-fire.

```bash
gridctl optimize                          # styled findings table
gridctl optimize --format json            # machine-readable OptimizeReport
gridctl optimize --min-impact 0.10        # filter low-impact findings (info findings always shown)
gridctl optimize --severity warn,critical # narrow to actionable findings
```

Exit codes follow the standard CLI contract: `0` no findings or info-only, `1` at least one warn/critical finding, `2` infrastructure error (gateway unreachable, wrong stack name). The Web UI surfaces the same findings inside the Gateway sidebar's `Optimize` panel.

### Distributed Tracing

Every tool call through the gateway is captured as an OpenTelemetry trace. Spans record transport type, server name, duration, and error state. The last 1000 traces are kept in a ring buffer and are queryable via CLI or the Web UI.

```bash
# List recent traces
gridctl traces

# Inspect a single trace as a span waterfall
gridctl traces <trace-id>

# Stream traces in real time
gridctl traces --follow
```

The Web UI includes a Traces tab in the bottom panel with an interactive waterfall view, span detail panel, and a pop-out window. Canvas edges light up with latency heat based on recent trace data.

## 📚 CLI Reference

Commands are grouped by domain. Run `gridctl <command> --help` for the full flag set; the tables below cover the high-value flags an operator reaches for daily.

### Stack lifecycle

| Command | Purpose |
|---|---|
| `gridctl validate <stack.yaml>` | Validate stack YAML (exit `0`/`1`/`2`); `--format json` for machine-readable output. |
| `gridctl plan <stack.yaml>` | Preview changes against running state; `-y` / `--auto-approve` to apply. |
| `gridctl apply <stack.yaml>` | Start containers and the MCP gateway. Flags: `-f` foreground, `-p` port, `--base-port`, `--watch`, `--flash`, `--code-mode`, `--no-cache`, `--no-expand`, `-v` JSON dump, `-q` quiet, `--log-file <path>`. |
| `gridctl reload [stack-name]` | Hot reload a running stack's spec. |
| `gridctl destroy <stack.yaml>` | Stop and remove all containers for the stack. |
| `gridctl export` | Reverse-engineer `stack.yaml` from running state; `-o <dir>` write to directory, `--format json`. |
| `gridctl serve` | Start the web UI and API without managing a stack (stackless mode). |
| `gridctl stop` | Stop the stackless gridctl daemon. |
| `gridctl status` | Show running stacks; `--replicas` expands to one row per replica. |
| `gridctl info` | Show the detected container runtime (Docker/Podman). |
| `gridctl version` | Print version information. |

### LLM clients

| Command | Purpose |
|---|---|
| `gridctl link [client]` | Connect an LLM client to the gateway; `--all` for every detected client, `--dry-run` to preview. |
| `gridctl unlink [client]` | Remove gridctl from an LLM client's config. |

### Skills - remote import (git)

| Command | Purpose |
|---|---|
| `gridctl skill list` | List skills in the registry (`--remote` for imported skills only). |
| `gridctl skill add <repo-url>` | Import skills from a git repository. Auth flags: `--auth-token <pat>` (ephemeral HTTPS PAT, CI), `--vault-key <key>` (resolves from `${vault:KEY}`), `--ssh-key <path>` (SSH). |
| `gridctl skill update [name]` | Update imported skills (all when name omitted). |
| `gridctl skill remove <name>` | Remove an imported skill. |
| `gridctl skill pin <name> <ref>` | Pin a skill to a specific git ref. |
| `gridctl skill info <name>` | Show origin and update status. |
| `gridctl skill try <repo-url>` | Temporarily import a skill for evaluation. |
| `gridctl skill validate <name>` | Validate a skill definition. |
| `gridctl test <skill-name>` | Run acceptance criteria (exit `0`/`1`/`2`). |
| `gridctl activate <skill-name>` | Promote a skill from draft to active. |

### Skills - authoring

| Command | Purpose |
|---|---|
| `gridctl agent init [DIR]` | Scaffold a starter skill. `--lang ts` (default), `--lang go`, or `--prompt-only` (mutually exclusive with `--lang`). |
| `gridctl agent dev --root <dir>` | Start the IDE dev server (canvas + watcher + trace overlay) on port `8181`. |
| `gridctl agent validate <skill>` | Static-check a skill's manifest and handler symbols. |
| `gridctl agent build <skill>` | Compile: esbuild for TS, `go build -buildmode=plugin` for Go. Writes `dist/manifest.json`. |

### Runs

| Command | Purpose |
|---|---|
| `gridctl run <skill>` | Execute a typed skill and stream events. Input: `--input '<json>'` inline, `--input @file.json`, or `--input -` for stdin. Output: `--format json` (NDJSON + summary), `--quiet`. |
| `gridctl runs list` | Recent runs (`--limit N`, `--format json`). |
| `gridctl runs inspect <run_id>` | Typed event timeline. |
| `gridctl runs trace <run_id>` | OTel-shaped JSON projection. |
| `gridctl runs resume <run_id>` | Time-travel resume from the last checkpoint; `--from-step <node_id>` to override. |
| `gridctl runs approve <run_id>` | Resolve a pending approval gate. `--decision approve\|reject`, `--reason "<text>"`. |

### Vault

| Command | Purpose |
|---|---|
| `gridctl vault set <key>` | Store a secret (interactive prompt, or `--value`); `--set <name>` to assign to a variable set. |
| `gridctl vault get <key>` | Retrieve a secret (masked; `--plain` to unmask). |
| `gridctl vault list` | List all vault keys with set assignments. |
| `gridctl vault delete <key>` | Remove a secret (`--force` to skip confirmation). |
| `gridctl vault import <file>` | Import from `.env` or `.json` (`--format` to override auto-detection). |
| `gridctl vault export` | Export secrets (`--format env\|json`, `--plain` to unmask). |
| `gridctl vault lock` / `unlock` | Encrypt / decrypt the vault. |
| `gridctl vault change-passphrase` | Re-encrypt with a new passphrase. |

### Pins (TOFU schema pinning)

| Command | Purpose |
|---|---|
| `gridctl pins list` | Status of all pinned servers. |
| `gridctl pins verify [server]` | Verify pins (`--exit-code` for CI: exit `1` on drift). |
| `gridctl pins approve <server>` | Re-pin current tool definitions, clearing drift. |
| `gridctl pins reset <server>` | Delete pins (re-pinned on next apply). |

### Traces

| Command | Purpose |
|---|---|
| `gridctl traces` | Show recent distributed traces (table view). |
| `gridctl traces <trace-id>` | Span waterfall for a single trace. |
| `gridctl traces --follow` | Stream traces as they arrive. |
| `gridctl traces --server <name>` | Filter by MCP server name. |
| `gridctl traces --errors` | Show only error traces. |
| `gridctl traces --min-duration 100ms` | Filter by minimum duration. |
| `gridctl traces --json` | Output as JSON. |

### Optimize

| Command | Purpose |
|---|---|
| `gridctl optimize` | Surface unused servers and tools with weekly USD impact. |
| `gridctl optimize --stack <name>` | Pick a specific stack when more than one is running. |
| `gridctl optimize --min-impact 0.10` | Filter findings below a weekly USD impact threshold (info findings always shown). |
| `gridctl optimize --severity warn,critical` | Allowlist by severity. |
| `gridctl optimize --format json` | Machine-readable `OptimizeReport` (exit `0`/`1`/`2`). |

### Upgrade

| Command | Purpose |
|---|---|
| `gridctl upgrade` | Check + prompt + upgrade (standalone install). |
| `gridctl upgrade --check` | Only check for updates; do not install. |
| `gridctl upgrade --yes` | Non-interactive (CI / cron). |
| `gridctl upgrade --version <tag>` | Install a specific release tag (allows downgrades). |
| `gridctl upgrade --force` | Bypass Homebrew detection and the up-to-date short-circuit. |

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

## 📙 Examples

| Example | What It Shows |
|:--------|:--------------|
| [`mcp-basic.yaml`](examples/getting-started/mcp-basic.yaml) | Stack with multiple MCP servers and tool filtering |
| [`tool-filtering.yaml`](examples/access-control/tool-filtering.yaml) | Server-level tool access control |
| [`local-mcp.yaml`](examples/transports/local-mcp.yaml) | Local process transport |
| [`ssh-mcp.yaml`](examples/transports/ssh-mcp.yaml) | SSH tunnel transport |
| [`external-mcp.yaml`](examples/transports/external-mcp.yaml) | External HTTP/SSE servers |
| [`gateway-basic.yaml`](examples/gateways/gateway-basic.yaml) | Gateway to an existing MCP server |
| [`gateway-remote.yaml`](examples/gateways/gateway-remote.yaml) | Remote access to Gridctl from other machines |
| [`github-mcp.yaml`](examples/platforms/github-mcp.yaml) | GitHub MCP server integration |
| [`atlassian-mcp.yaml`](examples/platforms/atlassian-mcp.yaml) | Atlassian Rovo (Jira, Confluence) integration |
| [`zapier-mcp.yaml`](examples/platforms/zapier-mcp.yaml) | Zapier automation platform integration |
| [`chrome-devtools-mcp.yaml`](examples/platforms/chrome-devtools-mcp.yaml) | Chrome DevTools browser automation |
| [`context7-mcp.yaml`](examples/platforms/context7-mcp.yaml) | Up-to-date library documentation |
| [`openapi-basic.yaml`](examples/openapi/openapi-basic.yaml) | Turn a REST API into MCP tools via OpenAPI spec |
| [`openapi-auth.yaml`](examples/openapi/openapi-auth.yaml) | OpenAPI with bearer token and API key auth |
| [`code-mode-basic.yaml`](examples/code-mode/code-mode-basic.yaml) | Gateway code mode with search + execute meta-tools |
| [`registry-basic.yaml`](examples/registry/registry-basic.yaml) | Skills registry with a single server |
| [`registry-advanced.yaml`](examples/registry/registry-advanced.yaml) | Cross-server skills |
| [`triage-ts`](examples/registry/items/triage-ts/SKILL.md) | TypeScript code skill (sandboxed `goja` + `esbuild`) with the hybrid pattern |
| [`triage-go`](examples/registry/items/triage-go/SKILL.md) | Typed Go code skill via `skill.Define[I, O]` |
| [`incident-triage-hybrid`](examples/registry/items/incident-triage-hybrid/SKILL.md) | Hybrid pattern: Go handler reads its own `SKILL.md` body as the LLM system prompt |
| [`code-review`](examples/registry/items/code-review/SKILL.md) | Prompt-only skill - SKILL.md only, no handler |
| [`agent-basic.yaml`](examples/getting-started/agent-basic.yaml) | Stack with a skills registry attached to the gateway |
| [`multi-agent-skills.yaml`](examples/multi-agent/multi-agent-skills.yaml) | Multi-agent orchestrator handing off between skills |
| [`vault-basic.yaml`](examples/secrets-vault/vault-basic.yaml) | Reference vault secrets with `${vault:KEY}` syntax |
| [`vault-sets.yaml`](examples/secrets-vault/vault-sets.yaml) | Auto-inject grouped secrets via variable sets |
| [`otlp-jaeger.yaml`](examples/tracing/otlp-jaeger.yaml) | Export traces to Jaeger via OTLP |

## 📐 Stability

| Feature | Status | Compatibility |
|---------|--------|---------------|
| MCP gateway (stdio, SSE, HTTP) | Stable | Backward compatible in 0.x |
| Container orchestration (Docker) | Stable | Backward compatible in 0.x |
| Config schema (servers, resources) | Stable | Backward compatible in 0.x |
| Auth middleware (bearer, API key) | Stable | Backward compatible in 0.x |
| Hot reload | Stable | Backward compatible in 0.x |
| Vault secrets | Stable | Backward compatible in 0.x |
| Web UI | Stable | No API guarantee (internal) |
| Output format conversion | Stable | Backward compatible in 0.x |
| Token usage metrics | Stable | Backward compatible in 0.x |
| Stack validation (validate) | Stable | Backward compatible in 0.x |
| Stack planning (plan) | Stable | Backward compatible in 0.x |
| Static replicas | Stable | Backward compatible in 0.x |
| Reactive autoscaling | Experimental | May change without notice |
| Code mode | Experimental | May change without notice |
| Podman runtime | Stable | Backward compatible in 0.x |
| Skills registry (prompt-only) | Stable | Backward compatible in 0.x |
| Skill acceptance criteria (test) | Experimental | May change without notice |
| Stack export (export) | Experimental | May change without notice |
| Spec drift detection | Experimental | May change without notice |
| Visual spec builder | Experimental | May change without notice |
| Skills import (skill add) | Experimental | May change without notice |
| Distributed tracing | Experimental | May change without notice |
| Typed skill SDK (Go, TS) | Experimental | May change without notice |
| Go plugin skill loader | Experimental | May change without notice |
| Agent IDE (`gridctl agent dev`) | Experimental | May change without notice |
| JSONL run ledger + resume | Experimental | May change without notice |
| Multi-agent orchestrator | Experimental | May change without notice |
| LLM provider abstraction | Experimental | May change without notice |
| Cost observability | Experimental | May change without notice |

## ⚠️ Known Limitations

- Podman rootless multi-container networking requires `netavark` and `aardvark-dns` (Podman 4.0+); `pasta`/`slirp4netns` are egress-only transports and are not used for inter-container communication
- Code mode sandbox has no filesystem access (by design)
- Skills registry is local-only with no remote discovery
- Web UI requires a modern browser (no IE11 support)

## 📖 Documentation

- [Skills](docs/skills.md) - the three-flavor skill model (prompt-only, TS, Go), the hybrid pattern, and Go-plugin sharp edges
- [Configuration Reference](docs/config-schema.md) - every field in `stack.yaml`
- [REST API Reference](docs/api-reference.md) - all gateway endpoints
- [Scaling](docs/scaling.md) - static replicas, reactive autoscaling, and the trade-offs
- [Cost Observability](docs/cost-observability.md) - LLM pricing, per-client attribution, and the `optimize` heuristics
- [Troubleshooting](docs/troubleshooting.md) - common issues and resolutions

## 🤝 Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). We welcome PRs for new transport types, example stacks, and documentation improvements.

## 🪪 License

[Apache 2.0](LICENSE)

---

<p align="center">
  <sub>Built for engineers who'd rather be building and hate the absence of repeatable environments!</sub>
</p>
