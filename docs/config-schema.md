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
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `allowed_origins` | []string | No | `["*"]` | CORS allowed origins. Empty or unset allows all |
| `auth` | object | No | — | Authentication configuration |
| `code_mode` | string | No | `"off"` | Enable code mode: `"on"` or `"off"` *(experimental)* |
| `code_mode_timeout` | int | No | `30` | Code mode execution timeout in seconds. Must be >= 0 *(experimental)* |

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
| `tools` | []string | No | — | Tool whitelist. Empty exposes all tools |

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
| `type` | string | **Yes** | — | `"bearer"` or `"header"` |
| `tokenEnv` | string | Conditional | — | Env var name for bearer token (required when type is `"bearer"`) |
| `header` | string | Conditional | — | Header name (required when type is `"header"`) |
| `valueEnv` | string | Conditional | — | Env var name for header value (required when type is `"header"`) |

**Operations Filter:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `include` | []string | No | — | Operation IDs to include (whitelist) |
| `exclude` | []string | No | — | Operation IDs to exclude (blacklist) |

Cannot use both `include` and `exclude`.

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
