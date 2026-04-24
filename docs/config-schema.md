# Configuration Reference

This document describes every field in the gridctl stack YAML configuration.

## Stack

The root configuration object.

```yaml
version: "1"
name: my-stack
gateway: ...
secrets: ...
network: ...
networks: ...
mcp-servers: ...
agents: ...
resources: ...
a2a-agents: ...
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `version` | string | No | `"1"` | Configuration format version |
| `name` | string | **Yes** | — | Stack identifier. Used for container naming and network defaults |
| `gateway` | object | No | — | Gateway-level settings (auth, CORS, code mode) |
| `secrets` | object | No | — | Variable set references for automatic secret injection |
| `network` | object | No | See below | Single network configuration (simple mode) |
| `networks` | []object | No | — | Multiple network configurations (advanced mode) |
| `mcp-servers` | []object | No | — | MCP server definitions |
| `agents` | []object | No | — | Agent definitions |
| `resources` | []object | No | — | Supporting container definitions (databases, caches, etc.) |
| `a2a-agents` | []object | No | — | External A2A agent references *(experimental)* |

---

## Gateway

Optional gateway-level configuration for authentication, CORS, and code mode.

```yaml
gateway:
  allowed_origins:
    - "https://example.com"
  auth:
    type: bearer
    token: "${MY_TOKEN}"
  code_mode: "on"
  code_mode_timeout: 30
  output_format: toon
  tracing:
    export: otlp
    endpoint: http://localhost:4318
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `allowed_origins` | []string | No | `["*"]` | CORS allowed origins. Empty or unset allows all |
| `auth` | object | No | — | Authentication configuration |
| `code_mode` | string | No | `"off"` | Enable code mode: `"on"` or `"off"` *(experimental)* |
| `code_mode_timeout` | int | No | `30` | Code mode execution timeout in seconds. Must be >= 0 *(experimental)* |
| `output_format` | string | No | `"json"` | Default output format for tool call results: `"json"`, `"toon"`, `"csv"`, or `"text"`. Per-server `output_format` overrides this value |
| `security` | object | No | — | Security settings (see [Security](#security)) |
| `tokenizer` | string | No | `"embedded"` | Token counting mode: `"embedded"` (cl100k_base approximation) or `"api"` (exact counts via Anthropic `count_tokens` endpoint) |
| `tokenizer_api_key` | string | No | — | Anthropic API key for `tokenizer: api`. Falls back to `ANTHROPIC_API_KEY` env var. Supports `${VAR}` and `${vault:KEY}` references |
| `tracing` | object | No | — | Distributed tracing configuration (see [Tracing](#tracing)) |

### Auth

When configured, all requests (except `/health` and `/ready`) require a valid token.

```yaml
gateway:
  auth:
    type: bearer
    token: "${GATEWAY_TOKEN}"
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | string | **Yes** | — | Auth mechanism: `"bearer"` or `"api_key"` |
| `token` | string | **Yes** | — | Expected token value. Supports `${VAR}` and `${vault:KEY}` references |
| `header` | string | No | `"Authorization"` | Header name. Only applicable when type is `"api_key"` |

**Constraints:**
- `header` can only be set when `type` is `"api_key"`
- Token comparison uses constant-time equality to prevent timing attacks

### Security

Optional gateway-level security settings.

```yaml
gateway:
  security:
    schema_pinning:
      enabled: true
      action: warn
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `schema_pinning` | object | No | — | TOFU schema pinning configuration |

**Schema Pinning:**

Protects against rug pull attacks (CVE-2025-54136 class) by hashing tool definitions on first connect and verifying them on every subsequent reconnect or reload.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | bool | No | `true` | Enable schema pinning globally for the stack |
| `action` | string | No | `"warn"` | Drift response: `"warn"` logs the diff and continues; `"block"` rejects tool calls from the drifted server until approved |

Pin files are stored in `~/.gridctl/pins/{stackName}.json`. Use `gridctl pins` subcommands to inspect, approve, or reset pins. Per-server opt-out is available via the `pin_schemas: false` field on any `mcp-servers` entry.

### Tracing

Configures distributed tracing for the gateway. When omitted, tracing is enabled with defaults (in-memory ring buffer, no OTLP export). Completed traces are always available in the web UI Traces tab via the ring buffer.

```yaml
gateway:
  tracing:
    export: otlp
    endpoint: http://localhost:4318
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | bool | No | `true` | Enable or disable tracing |
| `sampling` | float | No | `1.0` | Head-based sampling rate `[0.0–1.0]`. `1.0` samples all traces |
| `retention` | string | No | `"24h"` | How long completed traces are kept in the ring buffer. Accepts Go duration strings (e.g. `"1h"`, `"24h"`) |
| `export` | string | No | `""` | Exporter type: `"otlp"` to enable OTLP HTTP export, or `""` to disable |
| `endpoint` | string | No | `""` | OTLP HTTP endpoint URL. Required when `export` is `"otlp"`. `http://` uses plain HTTP; `https://` uses TLS |
| `max_traces` | int | No | `1000` | Ring buffer capacity in number of traces |

**Example — local Jaeger:**

```yaml
gateway:
  tracing:
    export: otlp
    endpoint: http://localhost:4318
```

Start Jaeger: `docker run --rm -p 4318:4318 -p 16686:16686 jaegertracing/jaeger:latest`

**Example — Honeycomb or Grafana Cloud (HTTPS):**

```yaml
gateway:
  tracing:
    export: otlp
    endpoint: https://api.honeycomb.io/v1/traces
```

> Cloud backends typically require auth headers (e.g. `x-honeycomb-team` for Honeycomb).
> Use an OTel Collector as a proxy to inject headers without embedding credentials in `stack.yaml`.

---

## Secrets

References variable sets from the vault for automatic secret injection into containers.

```yaml
secrets:
  sets:
    - production
    - shared
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `sets` | []string | No | — | Variable set names to inject. Explicit `env` values take precedence |

---

## Network

Docker network configuration. Use either `network` (simple mode) or `networks` (advanced mode), not both.

### Simple Mode

```yaml
network:
  name: my-net
  driver: bridge
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | No | `"{stack.name}-net"` | Network name. Auto-generated from stack name if omitted |
| `driver` | string | No | `"bridge"` | Network driver: `"bridge"`, `"host"`, or `"none"` |

### Advanced Mode

```yaml
networks:
  - name: frontend
    driver: bridge
  - name: backend
    driver: bridge
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | **Yes** | — | Network name. Must be unique |
| `driver` | string | No | `"bridge"` | Network driver: `"bridge"`, `"host"`, or `"none"` |

**Constraints:**
- Cannot have both `network` and `networks` defined
- In advanced mode, all container-based servers, agents, and resources must specify a `network` field referencing a name from this list
- Duplicate network names are rejected

---

## MCP Servers

MCP server definitions. Each server must be exactly one type: container, external URL, local process, SSH, or OpenAPI.

### Container Server (image)

Runs an MCP server inside a Docker/Podman container from a pre-built image.

```yaml
mcp-servers:
  - name: github
    image: ghcr.io/github/github-mcp-server:latest
    transport: stdio
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_PERSONAL_ACCESS_TOKEN}"
```

### Container Server (source)

Builds and runs an MCP server from source code.

```yaml
mcp-servers:
  - name: my-server
    source:
      type: git
      url: https://github.com/org/repo.git
      ref: main
      dockerfile: Dockerfile
    port: 8080
```

### External URL Server

Connects to an existing MCP server at a URL (no container created).

```yaml
mcp-servers:
  - name: remote-server
    url: https://mcp.example.com/sse
```

### Local Process Server

Runs an MCP server as a local process on the host (stdio transport).

```yaml
mcp-servers:
  - name: local-server
    command: ["npx", "mcp-remote", "https://mcp.example.com/sse"]
```

### SSH Server

Runs an MCP server command over an SSH tunnel.

```yaml
mcp-servers:
  - name: remote-tools
    ssh:
      host: 10.0.0.5
      user: deploy
      port: 22
      identityFile: ~/.ssh/id_ed25519
      knownHostsFile: ~/.ssh/gridctl_known_hosts  # enables strict host key checking
      jumpHost: bastion.example.com               # route through a bastion host
    command: ["/usr/local/bin/mcp-server"]
```

### OpenAPI Server

Turns a REST API into MCP tools by parsing an OpenAPI specification.

```yaml
mcp-servers:
  - name: petstore
    openapi:
      spec: https://petstore3.swagger.io/api/v3/openapi.json
      baseUrl: https://petstore3.swagger.io/api/v3
      auth:
        type: bearer
        tokenEnv: PETSTORE_TOKEN
      operations:
        include:
          - listPets
          - getPetById
```

### All MCP Server Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | **Yes** | — | Unique server identifier |
| `image` | string | Conditional | — | Docker image (container servers) |
| `source` | object | Conditional | — | Build from source (see [Source](#source)) |
| `url` | string | Conditional | — | External server URL |
| `port` | int | Conditional | — | Container port for HTTP/SSE transport. Required for non-stdio container servers |
| `transport` | string | No | `"http"` | Transport mode: `"http"`, `"stdio"`, or `"sse"` |
| `command` | []string | Conditional | — | Container entrypoint override, local process command, or SSH remote command |
| `env` | map | No | — | Environment variables |
| `build_args` | map | No | — | Docker build-time arguments (container servers only) |
| `network` | string | Conditional | — | Network to join (required in advanced network mode) |
| `ssh` | object | Conditional | — | SSH connection config (see [SSH](#ssh)) |
| `openapi` | object | Conditional | — | OpenAPI spec config (see [OpenAPI](#openapi)) |
| `tools` | []string | No | — | Tool whitelist. Empty exposes all tools. The web wizard populates this from the live topology for running servers, and offers an optional probe of external-URL servers to discover their tools before deploy. Container / stdio / local-process / SSH / OpenAPI servers are curated from the topology sidebar after deploy. Editable live from the topology sidebar's Tools editor — `PUT /api/mcp-servers/{name}/tools` rewrites this field atomically and triggers a hot reload |
| `output_format` | string | No | — | Output format override: `"json"`, `"toon"`, `"csv"`, or `"text"`. Overrides `gateway.output_format` for this server |
| `pin_schemas` | bool | No | — | Override schema pinning for this server. `false` disables pinning regardless of gateway setting. Omit to inherit from `gateway.security.schema_pinning.enabled` |
| `ready_timeout` | duration | No | `30s` | Readiness wait for container-based HTTP/SSE servers. Accepts any `time.Duration` string (e.g. `"60s"`, `"2m"`). When a container does not become ready within this window, the container is stopped and removed so a retry starts clean. Ignored for stdio, external, local process, SSH, and OpenAPI servers |
| `ping_timeout` | duration | No | `5s` | Per-ping deadline used by the gateway health monitor. Accepts any `time.Duration` string (e.g. `"10s"`). Tune this when a server's real `Ping` latency can exceed 5s — e.g. HTTP upstreams with many tools or under autoscale spawn load where the default flakes into spurious `context deadline exceeded` errors. Applies to every pingable transport (HTTP, SSE, stdio, local process, SSH, OpenAPI) |
| `replicas` | int | No | `1` | Number of independent processes to spawn for this server. Values >1 load-balance JSON-RPC tool calls across replicas using `replica_policy`. Range: 1–32. Not supported for external URL or OpenAPI transports. Mutually exclusive with `autoscale`. See [Scaling](scaling.md) |
| `replica_policy` | string | No | `"round-robin"` | Dispatch policy when `replicas > 1` or `autoscale` is set: `"round-robin"` or `"least-connections"` |
| `autoscale` | object | No | — | Reactive autoscaling block. Mutually exclusive with `replicas`. Not supported for external URL or OpenAPI transports. See [Autoscale](#autoscale) |

**Type determination rules:**
- Must have exactly one of: `image`, `source`, `url`, `command` (alone), `ssh` + `command`, or `openapi`
- Multiple types in the same server definition is an error

**Transport constraints by type:**

| Server Type | Allowed Transports | Port | Network |
|-------------|-------------------|------|---------|
| Container (image/source) | `http`, `sse`, `stdio` | Required for http/sse | Required in advanced mode |
| External (url) | `http`, `sse` | Not allowed | Not allowed |
| Local process (command) | `stdio` | Not allowed | Not allowed |
| SSH (ssh + command) | `stdio` | Not allowed | Not allowed |
| OpenAPI (openapi) | Not applicable | Not allowed | Not allowed |

### Source

Build configuration for container images from source code.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | string | **Yes** | — | Source type: `"git"` or `"local"` |
| `url` | string | Conditional | — | Git repository URL (required for `git`, not allowed for `local`) |
| `ref` | string | No | `"main"` | Git ref — branch, tag, or commit (git sources only) |
| `path` | string | Conditional | — | Local path (required for `local`, not allowed for `git`). Relative paths are resolved from the stack file |
| `dockerfile` | string | No | `"Dockerfile"` | Dockerfile path relative to source root |

### SSH

SSH connection parameters for remote MCP servers.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `host` | string | **Yes** | — | Hostname or IP address |
| `user` | string | **Yes** | — | SSH username |
| `port` | int | No | `22` | SSH port (0–65535) |
| `identityFile` | string | No | — | Path to SSH private key. Supports `~` expansion. Falls back to SSH agent |
| `knownHostsFile` | string | No | — | Path to a known_hosts file. When set, enables `StrictHostKeyChecking=yes` instead of the default TOFU (`accept-new`). Supports `~` expansion. Pre-populate with `ssh-keyscan <host> >> <file>` |
| `jumpHost` | string | No | — | Bastion/jump host to route the connection through (`[user@]host[:port]`). Maps to the SSH `-J` flag |

### OpenAPI

OpenAPI specification configuration for API-backed MCP servers.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec` | string | **Yes** | — | URL or local file path to OpenAPI spec (JSON or YAML) |
| `baseUrl` | string | No | — | Override the base URL from the spec |
| `auth` | object | No | — | API authentication (see below) |
| `operations` | object | No | — | Operation filter (see below) |

**OpenAPI Auth:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | string | **Yes** | — | `"bearer"`, `"header"`, `"query"`, `"oauth2"`, or `"basic"` |
| `tokenEnv` | string | Conditional | — | Env var name for bearer token (required when type is `"bearer"`) |
| `header` | string | Conditional | — | Header name (required when type is `"header"`) |
| `valueEnv` | string | Conditional | — | Env var name for header/query value (required when type is `"header"` or `"query"`) |
| `paramName` | string | Conditional | — | Query parameter name (required when type is `"query"`) |
| `clientIdEnv` | string | Conditional | — | Env var name for OAuth2 client ID (required when type is `"oauth2"`) |
| `clientSecretEnv` | string | Conditional | — | Env var name for OAuth2 client secret (required when type is `"oauth2"`) |
| `tokenUrl` | string | Conditional | — | OAuth2 token endpoint URL (required when type is `"oauth2"`) |
| `scopes` | []string | No | — | OAuth2 scopes to request (optional, for type `"oauth2"`) |
| `usernameEnv` | string | Conditional | — | Env var name for username (required when type is `"basic"`) |
| `passwordEnv` | string | Conditional | — | Env var name for password (required when type is `"basic"`) |

**OpenAPI TLS (mTLS):**

Transport-layer TLS configuration. Can be combined with any `auth` type.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `certFile` | string | Conditional | — | Client certificate file path (required for mTLS, must be set with `keyFile`) |
| `keyFile` | string | Conditional | — | Client private key file path (required for mTLS, must be set with `certFile`) |
| `caFile` | string | No | — | Custom CA certificate file path for server verification |
| `insecureSkipVerify` | bool | No | `false` | Skip server certificate verification (not recommended for production) |

**Operations Filter:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `include` | []string | No | — | Operation IDs to include (whitelist) |
| `exclude` | []string | No | — | Operation IDs to exclude (blacklist) |

Cannot use both `include` and `exclude`.

### Autoscale

Reactive autoscaling block — replaces the static `replicas: N` field with a policy that spawns and reaps replicas based on live in-flight load. Supported on container, local-process, and SSH servers. Rejected on external URL and OpenAPI transports with a precise YAML-path validation error. `autoscale` and `replicas` are mutually exclusive on the same server.

```yaml
mcp-servers:
  - name: junos
    command: [.venv/bin/python, servers/junos-mcp-server/jmcp.py, --transport, stdio]
    replica_policy: least-connections
    autoscale:
      min: 1
      max: 8
      target_in_flight: 3
      scale_up_after: 30s
      scale_down_after: 5m
      warm_pool: 0
      idle_to_zero: false
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `min` | int | **Yes** | — | Floor on the replica count. Must be `>= 0`; must be `>= 1` unless `idle_to_zero` is true |
| `max` | int | **Yes** | — | Ceiling on the replica count. Must be `>= 1`, `<= 32`, and `>= min` |
| `target_in_flight` | int | **Yes** | — | Target per-replica in-flight request count. The scaler holds the rolling median at or below this. Must be `>= 1` |
| `scale_up_after` | duration | No | `30s` | Window the rolling median must stay above `target_in_flight` before spawning. Minimum `10s` |
| `scale_down_after` | duration | No | `5m` | Window the rolling median must stay below half the target before reaping. Minimum `1m` |
| `warm_pool` | int | No | `0` | Extra idle replicas kept above the load-derived target. `min + warm_pool` is the scale-down floor. `min + warm_pool <= max` |
| `idle_to_zero` | bool | No | `false` | When true, allows `min: 0` and reaps every replica after sustained idle. The first tool call after idle pays a cold-start penalty (see [docs/scaling.md#cold-start-penalty](scaling.md#cold-start-penalty)) |

Full decision-rule walkthrough, cold-start trade-offs, and observability details live in [docs/scaling.md#autoscaling](scaling.md#autoscaling). Live state is exposed via `/api/status` and `/api/mcp-servers` (see [api-reference.md](api-reference.md#get-apistatus)) and the `AUTOSCALE` column of `gridctl status --replicas`.

---

## Agents

Agents consume MCP tools and can communicate via A2A protocol. Two modes: container-based and headless.

### Container Agent

```yaml
agents:
  - name: code-reviewer
    image: my-org/reviewer:latest
    description: "Reviews pull requests"
    capabilities:
      - code-review
    uses:
      - server: github
        tools: ["get_file_contents", "get_pull_request"]
    env:
      AGENT_MODE: "review"
    a2a:
      enabled: true
      skills:
        - id: review-code
          name: "Review Code"
          description: "Analyze code for bugs and style issues"
          tags: ["code", "review"]
```

### Headless Agent

```yaml
agents:
  - name: assistant
    runtime: claude-code
    prompt: "You are a helpful coding assistant."
    uses:
      - github
```

### All Agent Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | **Yes** | — | Unique agent identifier |
| `image` | string | Conditional | — | Docker image (container agents) |
| `source` | object | Conditional | — | Build from source (see [Source](#source)) |
| `runtime` | string | Conditional | — | Headless runtime (e.g., `"claude-code"`) |
| `prompt` | string | Conditional | — | System prompt (required when `runtime` is set) |
| `description` | string | No | — | Agent description |
| `capabilities` | []string | No | — | Capability tags (informational) |
| `uses` | []ToolSelector | No | — | MCP servers or A2A agents this agent can access |
| `equipped_skills` | []ToolSelector | No | — | Alias for `uses` (merged during load) |
| `env` | map | No | — | Environment variables |
| `build_args` | map | No | — | Docker build-time arguments |
| `network` | string | Conditional | — | Network to join (required in advanced network mode) |
| `command` | []string | No | — | Override container entrypoint |
| `a2a` | object | No | — | A2A protocol configuration *(experimental)* |

**Mode constraints:**
- Must have exactly one of: `image`, `source`, or `runtime`
- `runtime` cannot be combined with `image` or `source`
- `prompt` is required when `runtime` is set

**Tool selectors** (`uses` / `equipped_skills`) support two formats:

```yaml
# String format (all tools from server)
uses:
  - server-name

# Object format (filtered tools)
uses:
  - server: server-name
    tools: ["tool1", "tool2"]
```

**Dependency rules:**
- References must point to defined MCP servers or A2A-enabled agents
- Self-references are not allowed
- Circular dependencies between agents are detected and rejected

### A2A Config *(experimental)*

Exposes an agent via the Agent-to-Agent protocol.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | bool | No | `true` (when block is present) | Enable A2A exposure |
| `version` | string | No | `"1.0.0"` | Agent version string |
| `skills` | []object | No | — | Skills this agent exposes |

**A2A Skill:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `id` | string | **Yes** | — | Unique skill identifier |
| `name` | string | **Yes** | — | Human-readable skill name |
| `description` | string | No | — | Skill description |
| `tags` | []string | No | — | Classification tags |

**Constraints:**
- Skill IDs must be unique within an agent
- Both `id` and `name` are required

---

## Resources

Supporting containers such as databases, caches, and other services.

```yaml
resources:
  - name: postgres
    image: postgres:16
    env:
      POSTGRES_PASSWORD: "${DB_PASSWORD}"
    ports:
      - "5432:5432"
    volumes:
      - "pgdata:/var/lib/postgresql/data"
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | **Yes** | — | Unique resource identifier |
| `image` | string | **Yes** | — | Docker image |
| `env` | map | No | — | Environment variables |
| `ports` | []string | No | — | Port mappings (e.g., `"5432:5432"`) |
| `volumes` | []string | No | — | Volume mounts (e.g., `"data:/var/lib/postgres"`) |
| `network` | string | Conditional | — | Network to join (required in advanced network mode) |

**Constraints:**
- Names must be unique and not conflict with MCP server or agent names
- `image` is always required

---

## A2A Agents *(experimental)*

External Agent-to-Agent agent references for connecting to remote A2A-compatible agents.

```yaml
a2a-agents:
  - name: remote-reviewer
    url: https://agents.example.com
    auth:
      type: bearer
      token_env: REMOTE_AGENT_TOKEN
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | **Yes** | — | Local alias for the remote agent |
| `url` | string | **Yes** | — | Base URL for the remote agent's A2A endpoint |
| `auth` | object | No | — | Authentication configuration |

**A2A Auth:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | string | No | — | `"bearer"`, `"api_key"`, or `"none"` |
| `token_env` | string | Conditional | — | Env var name for the token (required when type is set and not `"none"`) |
| `header_name` | string | No | `"Authorization"` | Header name for API key auth |

**Constraints:**
- Names must not conflict with MCP servers or local agents
- Duplicate A2A agent names are rejected

---

## Skill Sources

Skill sources are declared in `~/.gridctl/skills.yaml`. Each source points at a git repository that gridctl clones to discover `SKILL.md` files. Sources may be public or authenticated.

```yaml
defaults:
  auto_update: true
  update_interval: 24h

sources:
  - name: public-skills
    repo: https://github.com/acme/public-skills
    ref: main

  - name: private-skills
    repo: https://github.com/acme/private-skills
    ref: v1.2.0
    auth:
      method: token
      credential_ref: "${vault:GIT_TOKEN}"

  - name: private-ssh
    repo: git@github.com:acme/private-skills.git
    auth:
      method: ssh-agent
```

### All Skill Source Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | No | Derived from repo URL | Unique source name |
| `repo` | string | **Yes** | — | Git repository URL (HTTPS or SSH) |
| `ref` | string | No | Default branch | Branch, tag, or semver constraint (e.g. `^1.2`) |
| `path` | string | No | — | Subdirectory containing `SKILL.md` files |
| `auto_update` | bool | No | Inherits `defaults.auto_update` | Enable background updates for this source |
| `update_interval` | duration | No | Inherits `defaults.update_interval` | Poll interval (e.g. `1h`, `24h`) |
| `auth` | object | No | — | Authentication block for private repos (see [Auth](#skill-source-auth)) |

### Skill Source Auth

Declares how gridctl authenticates when cloning or fetching this repository. Raw tokens must **never** appear in `skills.yaml` — use `credential_ref` to point at a vault key.

```yaml
auth:
  method: token
  credential_ref: "${vault:GIT_TOKEN}"
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `method` | string | **Yes** | — | One of `"token"`, `"ssh-agent"`, `"ssh-key"`, or `"none"` |
| `credential_ref` | string | Conditional | — | `${vault:KEY}` reference resolved at clone/fetch time. Required for `"token"` |
| `ssh_user` | string | No | `git` | SSH username used with `"ssh-agent"` or `"ssh-key"` |
| `ssh_key_path` | string | Conditional | — | Path to a private key file. Required for `"ssh-key"` |

**Method behavior:**

| Method | Transport | Credential source | Persisted |
|--------|-----------|-------------------|-----------|
| `token` | HTTPS | `credential_ref` (vault) — resolved on every clone/fetch | Reference only |
| `ssh-agent` | SSH | Ambient `SSH_AUTH_SOCK` | None |
| `ssh-key` | SSH | `ssh_key_path` on disk (optionally decrypted with `GRIDCTL_SSH_KEY_PASSPHRASE`) | Path only |
| `none` / omitted | HTTPS or SSH | Ambient `GITHUB_TOKEN` env for HTTPS; `SSH_AUTH_SOCK` for SSH | None |

**Security rules:**

- Raw PAT or SSH key material must never appear in `skills.yaml`, the lock file, or origin sidecars.
- `credential_ref` is the only credential field persisted; the live vault is consulted on every remote operation so rotating a secret takes effect immediately.
- Prefer `credential_ref` over embedding credentials in the `repo` URL (`https://TOKEN@host/...`). Any userinfo or known PAT patterns that do leak into errors and logs are scrubbed by the redaction layer, but vault references keep them out of on-disk state entirely.
- The CLI equivalents are `--auth-token <pat>` (ephemeral), `--vault-key <key>` (persisted as `credential_ref`), and `--ssh-key <path>` on `skill add` / `skill try`.

---

## Variable Expansion

String values in the configuration support variable expansion:

| Pattern | Description |
|---------|-------------|
| `$VAR` | Simple environment variable reference |
| `${VAR}` | Braced environment variable reference |
| `${VAR:-default}` | Use default if variable is undefined or empty |
| `${VAR:+replacement}` | Use replacement if variable is defined and non-empty |
| `${vault:KEY}` | Vault secret reference (error if key not found) |

Variable expansion is applied to string values across all configuration sections including `env`, `token`, and `url` fields.

---

## Name Uniqueness

All names across servers, agents, and resources share a single namespace. The following conflicts are rejected:

- Duplicate names within MCP servers, agents, resources, or A2A agents
- An agent name matching an MCP server or resource name
- A resource name matching an MCP server name
- An A2A agent name matching a local agent or MCP server name
