# Configuration Reference

This document describes every field in the gridctl stack YAML configuration.

## Stack

The root configuration object.

```yaml
version: "1"
name: my-stack
extends: base-stack.yaml
gateway: ...
logging: ...
telemetry: ...
secrets: ...
network: ...
networks: ...
mcp-servers: ...
resources: ...
clients: ...
client_models: ...
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `version` | string | No | `"1"` | Configuration format version |
| `name` | string | **Yes** | - | Stack identifier. Used for container naming and network defaults |
| `extends` | string | No | - | Path to a parent stack file this stack composes on top of |
| `gateway` | object | No | - | Gateway-level settings (auth, CORS, code mode) |
| `logging` | object | No | - | Log file output with rotation (see [Logging](#logging)) |
| `telemetry` | object | No | - | Opt-in disk persistence for logs/metrics/traces (see [Telemetry Persistence](#telemetry-persistence)) |
| `secrets` | object | No | - | Variable set references for automatic secret injection |
| `network` | object | No | See below | Single network configuration (simple mode) |
| `networks` | []object | No | - | Multiple network configurations (advanced mode) |
| `mcp-servers` | []object | No | - | MCP server definitions |
| `resources` | []object | No | - | Supporting container definitions (databases, caches, etc.) |
| `clients` | object | No | - | Per-client access scoping (see [Clients](#clients-per-client-access-scoping)) |
| `client_models` | map | No | - | Per-client model pricing attribution (see [Client Models](#client-models-pricing-attribution)) |
| `limits` | object | No | - | Budgets and rate limits enforced at dispatch (see [Limits](#limits-budgets-and-rate-limits)) |
| `groups` | map | No | - | Named tool bundles, each at its own endpoint (see [Groups](#groups-tool-bundles)) |
| `link` | []string\|object | No | - | LLM clients `gridctl apply` links to this gateway (see [Link](#link-declared-clients)) |

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
| `auth` | object | No | - | Authentication configuration |
| `code_mode` | string | No | `"off"` | Enable code mode: `"on"` or `"off"` *(experimental)* |
| `code_mode_timeout` | int | No | `30` | Code mode execution timeout in seconds. Must be >= 0 *(experimental)* |
| `default_model` | string | No | - | Model ID used to price tool calls for servers without their own `model` field (e.g. `"claude-opus-4-7"`). Enables cost observability; figures are estimates from the embedded LiteLLM rates, not billing truth. Empty disables cost attribution for servers without a per-server `model` |
| `output_format` | string | No | `"json"` | Default output format for tool call results: `"json"`, `"toon"`, `"csv"`, or `"text"`. Per-server `output_format` overrides this value |
| `maxToolResultBytes` | int | No | `65536` | Maximum size of a tool result in bytes before truncation. Results over the limit are truncated with a suffix noting the original size. `0` uses the default (64 KB) |
| `name` | string | No | `"gridctl-gateway"` | Identity announced to MCP clients in the initialize response (`serverInfo.name`). Some clients (VS Code / GitHub Copilot) display this instead of the entry key in their own config, so give distinct gateways distinct names. Group endpoints announce `<name>/<group>`. Requires a restart to propagate |
| `security` | object | No | - | Security settings (see [Security](#security)) |
| `tokenizer` | string | No | `"embedded"` | Token counting mode: `"embedded"` (cl100k_base approximation) or `"api"` (exact counts via Anthropic `count_tokens` endpoint) |
| `tokenizer_api_key` | string | No | - | Anthropic API key for `tokenizer: api`. Falls back to `ANTHROPIC_API_KEY` env var. Supports `${VAR}` and `${var:KEY}` references |
| `tracing` | object | No | - | Distributed tracing configuration (see [Tracing](#tracing)) |

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
| `type` | string | **Yes** | - | Auth mechanism: `"bearer"` or `"api_key"` |
| `token` | string | **Yes** | - | Expected token value. Supports `${VAR}` and `${var:KEY}` references |
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
      scan: true
      scan_ignore: []
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `schema_pinning` | object | No | - | TOFU schema pinning configuration |

**Schema Pinning:**

Protects against rug pull attacks (CVE-2025-54136 class) by hashing tool definitions (name, description, input schema, and output schema) on first connect and verifying them on every subsequent reconnect or reload.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `enabled` | bool | No | `true` | Enable schema pinning globally for the stack |
| `action` | string | No | `"warn"` | Drift response: `"warn"` logs the diff and continues; `"block"` rejects tool calls from the drifted server until approved |
| `scan` | bool | No | `true` | Run poisoning heuristics over tool definitions at pin and drift time; findings are advisory and never block anything |
| `scan_ignore` | string list | No | `[]` | Finding codes to suppress everywhere (e.g. `["P004"]`) |

**Poisoning scan:**

When `scan` is on, every tool definition is checked at pin and drift time for injection signals: hidden-instruction phrases (`P001`), references to sensitive files (`P002`), sensitive-action language (`P003`), suspicious emphasis words (`P004`), hidden Unicode including decoded Tags-block payloads (`P005`), and cross-server tool shadowing (`P006`). Matching runs on Unicode-normalized text so zero-width, homoglyph, and leetspeak evasion does not defeat it, and quoted matches are downgraded so a tool that documents attack phrases is not flagged as one. Findings render beside the drift diff in `gridctl pins diff`, the diff API, and the Pins workspace; they inform the approve decision and never gate it. Static heuristics are one detection layer, not a complete defense: attacks carried in runtime tool output are invisible to any pin-time check.

Pin files are stored in `~/.gridctl/pins/{stackName}.json`. Use `gridctl pins` subcommands to inspect, approve, or reset pins. Per-server opt-out is available via the `pin_schemas: false` field on any `mcp-servers` entry.

Pins recorded before output schemas were fingerprinted are upgraded in place: each pin verifies under the scheme it was recorded with, and clean pins are silently rewritten to the current scheme (which pins the output schema for the first time) on the next verify cycle. A fingerprint-scheme change never surfaces as drift.

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
| `include_infra` | bool | No | `false` | Admit spans from non-gridctl instrumentation scopes (e.g. Docker SDK HTTP self-instrumentation) into the UI trace buffer. OTLP export is unaffected |

**Example - local Jaeger:**

```yaml
gateway:
  tracing:
    export: otlp
    endpoint: http://localhost:4318
```

Start Jaeger: `docker run --rm -p 4318:4318 -p 16686:16686 jaegertracing/jaeger:latest`

**Example - Honeycomb or Grafana Cloud (HTTPS):**

```yaml
gateway:
  tracing:
    export: otlp
    endpoint: https://api.honeycomb.io/v1/traces
```

> Cloud backends typically require auth headers (e.g. `x-honeycomb-team` for Honeycomb).
> Use an OTel Collector as a proxy to inject headers without embedding credentials in `stack.yaml`.

---

## Logging

Optional log file output with automatic rotation. When `file` is set, logs are written to both the in-memory ring buffer (web UI) and the file simultaneously. This is distinct from [Telemetry Persistence](#telemetry-persistence), which captures per-server signals.

```yaml
logging:
  file: /var/log/gridctl.log
  maxSizeMB: 100
  maxAgeDays: 7
  maxBackups: 3
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `file` | string | No | - | Path to the log file. When set, logs are also written here |
| `maxSizeMB` | int | No | `100` | Maximum log file size in MB before rotation |
| `maxAgeDays` | int | No | `7` | Maximum days to retain rotated log files |
| `maxBackups` | int | No | `3` | Maximum number of compressed rotated files to keep |

---

## Telemetry Persistence

Opt-in disk persistence for the three signals gridctl already captures: logs, metrics, and traces. Without a `telemetry` block every signal stays ephemeral (today's behavior); the runtime ring buffers, web UI, and OTLP exporter are unaffected.

```yaml
telemetry:
  persist:
    logs: true
    metrics: false
    traces: true
  retention:
    max_size_mb: 100
    max_backups: 5
    max_age_days: 7
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `persist` | object | No | all `false` | Stack-global toggles for each signal. Per-server blocks override individual signals |
| `retention` | object | No | See below | Lumberjack rotation policy applied to every persisted signal file in this stack |

### Persist

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `logs` | bool | No | `false` | Persist logs to `<server>/logs.jsonl` (NDJSON of buffered slog entries) |
| `metrics` | bool | No | `false` | Persist metrics to `<server>/metrics.jsonl` (one diff snapshot per flush) |
| `traces` | bool | No | `false` | Persist traces to `<server>/traces.jsonl` (OTLP-JSON envelopes per the [OpenTelemetry File Exporter spec](https://opentelemetry.io/docs/specs/otel/protocol/file-exporter/)) |

### Retention

Controls lumberjack rotation. One block per stack - per-signal retention is intentionally out of scope at MVP.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `max_size_mb` | int | No | `100` | Active file size in MB before rotation. Must be `>= 1` |
| `max_backups` | int | No | `5` | Number of rotated siblings kept per signal. Must be `>= 1` |
| `max_age_days` | int | No | `7` | Maximum age of rotated siblings before deletion. Must be `>= 1` |

**Validation:**
- All three retention values must be positive integers within their hard caps (1 TiB, 1024 backups, 10 years).
- `max_size_mb * max_backups` exceeding 5 GB per server logs a soft-cap warning at apply time. Worst-case footprint per server is `(max_backups + 1) × max_size_mb` MB.

### Per-server Overrides

The `telemetry` field on any `mcp-servers` entry overrides individual signals for that server only. Each `*bool` field uses tri-state semantics:

| Value | Meaning |
|-------|---------|
| omitted | Inherit the stack-global value for this signal |
| `true` | Explicitly persist this signal regardless of the stack-global value |
| `false` | Explicitly do not persist this signal regardless of the stack-global value |

```yaml
telemetry:
  persist: { logs: true, metrics: true, traces: true }

mcp-servers:
  - name: github
    image: ghcr.io/github/github-mcp-server:latest
    telemetry:
      persist:
        traces: false   # noisy server: keep logs+metrics, drop traces
  - name: filesystem
    image: my/filesystem-mcp:latest
    telemetry:
      persist:
        logs: false     # PII risk: never persist logs for this server
```

Removing the per-server `telemetry` block entirely (or via `DELETE` semantics in the API) reverts every signal for that server to the stack-global default.

### Storage Layout

```
~/.gridctl/telemetry/<stack>/<server>/
  logs.jsonl                    # active file
  logs-2026-04-30T12-00-00.000.jsonl[.gz]   # rotated sibling
  metrics.jsonl
  traces.jsonl
```

- Files use mode `0600`; the `<stack>/<server>/` directories are `0700`. Matches the vault and state conventions.
- Rotation is performed by [lumberjack](https://github.com/natefinch/lumberjack) on size; the configured `max_age_days` and `max_backups` are enforced at rotation time.
- `traces.jsonl` is consumable as-is by the OTel collector's [`otlpjsonfilereceiver`](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/otlpjsonfilereceiver/README.md) for replay into a real backend.

### Inspection and Wipe

The `gridctl telemetry` CLI operates directly on these files:

| Command | Purpose |
|---------|---------|
| `gridctl telemetry status [stack] [--json]` | Inventory of on-disk telemetry across one or all stacks |
| `gridctl telemetry wipe [stack] [--server X] [--signal Y] [-y]` | Delete persisted files; scopes to server/signal when provided |
| `gridctl telemetry tail <stack> <server> --signal logs\|metrics\|traces` | `tail -f` the active signal file with lumberjack-rotation handling |

The same operations are available over the REST API (`GET /api/telemetry/inventory`, `DELETE /api/telemetry`) and through the web UI's header Persistence pill, sidebar Telemetry section, and graph node dot indicator.

**Default off in beta.** The feature stays opt-in until v0.2 stable - stacks without a `telemetry` block continue to behave exactly as today.

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
| `sets` | []string | No | - | Variable set names to inject. Explicit `env` values take precedence |

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
| `name` | string | **Yes** | - | Network name. Must be unique |
| `driver` | string | No | `"bridge"` | Network driver: `"bridge"`, `"host"`, or `"none"` |

**Constraints:**
- Cannot have both `network` and `networks` defined
- In advanced mode, all container-based servers and resources must specify a `network` field referencing a name from this list
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

#### External Server Authentication

External URL servers accept an optional `auth:` block. Three types are
supported: a static bearer token, a static custom header, and OAuth 2.1
brokering where gridctl runs the browser authorization flow itself and
manages token refresh for every connected client.

```yaml
mcp-servers:
  # Static bearer token (GitHub PATs, Stripe restricted keys, ...)
  - name: github
    url: https://api.githubcopilot.com/mcp/
    auth:
      type: bearer
      token: ${GITHUB_PAT}          # env or ${var:KEY} references

  # Static custom header
  - name: internal
    url: https://mcp.internal.example.com/mcp
    auth:
      type: header
      header: X-API-Key
      value: ${var:INTERNAL_API_KEY}

  # OAuth 2.1 brokering (Notion, Sentry, Atlassian, ...)
  - name: notion
    url: https://mcp.notion.com/mcp
    auth:
      type: oauth
      # Optional; defaults to the scopes the server advertises.
      scopes: []
      # Optional pre-registered client for authorization servers that do
      # not support dynamic client registration (e.g. Slack).
      client_id: ${NOTION_CLIENT_ID}
      client_secret: ${NOTION_CLIENT_SECRET}
```

| Field | Type | Applies to | Description |
|-------|------|------------|-------------|
| `type` | string | required | `bearer`, `header`, or `oauth` |
| `token` | string | bearer | Token sent as `Authorization: Bearer <token>` |
| `header` | string | header | Header name, e.g. `X-API-Key` |
| `value` | string | header | Header value |
| `scopes` | list | oauth | Scopes to request (default: server-advertised) |
| `client_id` | string | oauth | Pre-registered OAuth client ID (skips dynamic registration) |
| `client_secret` | string | oauth | Client secret, when the provider issued one |

With `type: oauth`, an unauthorized server deploys in a `needs auth` state
instead of failing the stack; run `gridctl auth login <name>` (or use the
web UI) to authorize once. Tokens are stored encrypted under
`~/.gridctl/oauth/` keyed by the server URL, refresh automatically, and
survive daemon restarts. This replaces the `npx mcp-remote` bridge for
OAuth-protected servers. See `gridctl auth --help`.

### Local Process Server

Runs an MCP server as a local process on the host (stdio transport).

```yaml
mcp-servers:
  - name: local-server
    command: ["npx", "some-stdio-mcp-server"]
```

For OAuth-protected remote servers, prefer an external URL server with
`auth: {type: oauth}` over wrapping `npx mcp-remote` in a local process;
gridctl brokers the flow natively with encrypted token storage.

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
| `name` | string | **Yes** | - | Unique server identifier |
| `image` | string | Conditional | - | Docker image (container servers) |
| `source` | object | Conditional | - | Build from source (see [Source](#source)) |
| `url` | string | Conditional | - | External server URL |
| `port` | int | Conditional | - | Container port for HTTP/SSE transport. Required for non-stdio container servers |
| `transport` | string | No | `"http"` | Transport mode: `"http"`, `"stdio"`, or `"sse"` |
| `command` | []string | Conditional | - | Container entrypoint override, local process command, or SSH remote command |
| `env` | map | No | - | Environment variables |
| `build_args` | map | No | - | Docker build-time arguments (container servers only) |
| `network` | string | Conditional | - | Network to join (required in advanced network mode) |
| `ssh` | object | Conditional | - | SSH connection config (see [SSH](#ssh)) |
| `openapi` | object | Conditional | - | OpenAPI spec config (see [OpenAPI](#openapi)) |
| `tools` | []string | No | - | Tool whitelist. Empty exposes all tools. The web wizard populates this from the live stack for running servers, and offers an optional probe of external-URL servers to discover their tools before deploy. Container / stdio / local-process / SSH / OpenAPI servers are curated from the Stack sidebar after deploy. Editable live from the Stack sidebar's Tools editor - `PUT /api/mcp-servers/{name}/tools` rewrites this field atomically and triggers a hot reload |
| `output_format` | string | No | - | Output format override: `"json"`, `"toon"`, `"csv"`, or `"text"`. Overrides `gateway.output_format` for this server |
| `pin_schemas` | bool | No | - | Override schema pinning for this server. `false` disables pinning regardless of gateway setting. Omit to inherit from `gateway.security.schema_pinning.enabled` |
| `ready_timeout` | duration | No | `30s` | Readiness wait for container-based HTTP/SSE servers. Accepts any `time.Duration` string (e.g. `"60s"`, `"2m"`). When a container does not become ready within this window, the container is stopped and removed so a retry starts clean. Ignored for stdio, external, local process, SSH, and OpenAPI servers |
| `ping_timeout` | duration | No | `5s` | Per-ping deadline used by the gateway health monitor. Accepts any `time.Duration` string (e.g. `"10s"`). Tune this when a server's real `Ping` latency can exceed 5s - e.g. HTTP upstreams with many tools or under autoscale spawn load where the default flakes into spurious `context deadline exceeded` errors. Applies to every pingable transport (HTTP, SSE, stdio, local process, SSH, OpenAPI) |
| `replicas` | int | No | `1` | Number of independent processes to spawn for this server. Values >1 load-balance JSON-RPC tool calls across replicas using `replica_policy`. Range: 1–32. Not supported for external URL or OpenAPI transports. Mutually exclusive with `autoscale`. See [Scaling](scaling.md) |
| `replica_policy` | string | No | `"round-robin"` | Dispatch policy when `replicas > 1` or `autoscale` is set: `"round-robin"` or `"least-connections"` |
| `autoscale` | object | No | - | Reactive autoscaling block. Mutually exclusive with `replicas`. Not supported for external URL or OpenAPI transports. See [Autoscale](#autoscale) |
| `telemetry` | object | No | - | Per-server telemetry persistence overrides. See [Per-server Overrides](#per-server-overrides) |
| `model` | string | No | - | Model ID used to price this server's tool calls (e.g. `"claude-opus-4-7"`). Overrides `gateway.default_model`. Enables cost observability for this server; figures are estimates from the embedded LiteLLM rates. Unknown model IDs log a single WARN and price as zero. Edits hot-reload without restarting the server. See [Cost Observability](cost-observability.md) |

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
| `type` | string | **Yes** | - | Source type: `"git"` or `"local"` |
| `url` | string | Conditional | - | Git repository URL (required for `git`, not allowed for `local`) |
| `ref` | string | No | `"main"` | Git ref - branch, tag, or commit (git sources only) |
| `path` | string | Conditional | - | Local path (required for `local`, not allowed for `git`). Relative paths are resolved from the stack file |
| `dockerfile` | string | No | `"Dockerfile"` | Dockerfile path relative to source root |
| `auth` | object | No | - | Authentication block for private git repositories (see [Source Auth](#source-auth)) |

### Source Auth

Declares how gridctl authenticates when cloning a private git repository at build time. Raw tokens must **never** appear in `stack.yaml` - use `credential_ref` to point at a vault key, which is resolved against the live vault on every clone.

```yaml
mcp-servers:
  - name: private-mcp
    source:
      type: git
      url: https://github.com/acme/private-mcp.git
      ref: main
      auth:
        method: token
        credential_ref: "${var:GIT_TOKEN}"
    port: 3000
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `method` | string | **Yes** | - | One of `"token"`, `"ssh-agent"`, `"ssh-key"`, or `"none"` |
| `credential_ref` | string | Conditional | - | `${var:KEY}` reference resolved at clone time. Required for `"token"` |
| `ssh_user` | string | No | `git` | SSH username used with `"ssh-agent"` or `"ssh-key"` |
| `ssh_key_path` | string | Conditional | - | Path to a private key file. Supports `~` expansion. Required for `"ssh-key"` |

**Method behavior:**

| Method | Transport | Credential source | Persisted |
|--------|-----------|-------------------|-----------|
| `token` | HTTPS | `credential_ref` (vault) - resolved on every clone | Reference only |
| `ssh-agent` | SSH | Ambient `SSH_AUTH_SOCK` | None |
| `ssh-key` | SSH | `ssh_key_path` on disk | Path only |
| `none` / omitted | HTTPS or SSH | Unauthenticated clone (public-repo path) | None |

**Security rules:**

- Raw PAT or SSH key material must never appear in `stack.yaml`. `credential_ref` is the only credential field persisted to YAML.
- The vault is consulted on every clone so rotating a secret takes effect immediately - there is no on-disk caching of the resolved token.
- Vault references survive `stack.yaml` extends/merges and variable expansion as opaque strings; they are only resolved at apply time inside the orchestrator.
- The skills registry uses the identical schema for `skills.yaml` source auth - see [Skill Source Auth](#skill-source-auth).

### SSH

SSH connection parameters for remote MCP servers.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `host` | string | **Yes** | - | Hostname or IP address |
| `user` | string | **Yes** | - | SSH username |
| `port` | int | No | `22` | SSH port (0–65535) |
| `identityFile` | string | No | - | Path to SSH private key. Supports `~` expansion. Falls back to SSH agent |
| `knownHostsFile` | string | No | - | Path to a known_hosts file. When set, enables `StrictHostKeyChecking=yes` instead of the default TOFU (`accept-new`). Supports `~` expansion. Pre-populate with `ssh-keyscan <host> >> <file>` |
| `jumpHost` | string | No | - | Bastion/jump host to route the connection through (`[user@]host[:port]`). Maps to the SSH `-J` flag |

### OpenAPI

OpenAPI specification configuration for API-backed MCP servers.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `spec` | string | **Yes** | - | URL or local file path to OpenAPI spec (JSON or YAML) |
| `baseUrl` | string | No | - | Override the base URL from the spec |
| `auth` | object | No | - | API authentication (see below) |
| `tls` | object | No | - | TLS / mTLS configuration (see OpenAPI TLS below) |
| `operations` | object | No | - | Operation filter (see below) |

**OpenAPI Auth:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | string | **Yes** | - | `"bearer"`, `"header"`, `"query"`, `"oauth2"`, or `"basic"` |
| `tokenEnv` | string | Conditional | - | Env var name for bearer token (required when type is `"bearer"`) |
| `header` | string | Conditional | - | Header name (required when type is `"header"`) |
| `valueEnv` | string | Conditional | - | Env var name for header/query value (required when type is `"header"` or `"query"`) |
| `paramName` | string | Conditional | - | Query parameter name (required when type is `"query"`) |
| `clientIdEnv` | string | Conditional | - | Env var name for OAuth2 client ID (required when type is `"oauth2"`) |
| `clientSecretEnv` | string | Conditional | - | Env var name for OAuth2 client secret (required when type is `"oauth2"`) |
| `tokenUrl` | string | Conditional | - | OAuth2 token endpoint URL (required when type is `"oauth2"`) |
| `scopes` | []string | No | - | OAuth2 scopes to request (optional, for type `"oauth2"`) |
| `usernameEnv` | string | Conditional | - | Env var name for username (required when type is `"basic"`) |
| `passwordEnv` | string | Conditional | - | Env var name for password (required when type is `"basic"`) |

**OpenAPI TLS (mTLS):**

Transport-layer TLS configuration. Can be combined with any `auth` type.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `certFile` | string | Conditional | - | Client certificate file path (required for mTLS, must be set with `keyFile`) |
| `keyFile` | string | Conditional | - | Client private key file path (required for mTLS, must be set with `certFile`) |
| `caFile` | string | No | - | Custom CA certificate file path for server verification |
| `insecureSkipVerify` | bool | No | `false` | Skip server certificate verification (not recommended for production) |

**Operations Filter:**

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `include` | []string | No | - | Operation IDs to include (whitelist) |
| `exclude` | []string | No | - | Operation IDs to exclude (blacklist) |

Cannot use both `include` and `exclude`.

### Autoscale

Reactive autoscaling block - replaces the static `replicas: N` field with a policy that spawns and reaps replicas based on live in-flight load. Supported on container, local-process, and SSH servers. Rejected on external URL and OpenAPI transports with a precise YAML-path validation error. `autoscale` and `replicas` are mutually exclusive on the same server.

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
| `min` | int | **Yes** | - | Floor on the replica count. Must be `>= 0`; must be `>= 1` unless `idle_to_zero` is true |
| `max` | int | **Yes** | - | Ceiling on the replica count. Must be `>= 1`, `<= 32`, and `>= min` |
| `target_in_flight` | int | **Yes** | - | Target per-replica in-flight request count. The scaler holds the rolling median at or below this. Must be `>= 1` |
| `scale_up_after` | duration | No | `30s` | Window the rolling median must stay above `target_in_flight` before spawning. Minimum `10s` |
| `scale_down_after` | duration | No | `5m` | Window the rolling median must stay below half the target before reaping. Minimum `1m` |
| `warm_pool` | int | No | `0` | Extra idle replicas kept above the load-derived target. `min + warm_pool` is the scale-down floor. `min + warm_pool <= max` |
| `idle_to_zero` | bool | No | `false` | When true, allows `min: 0` and reaps every replica after sustained idle. The first tool call after idle pays a cold-start penalty (see [docs/scaling.md#cold-start-penalty](scaling.md#cold-start-penalty)) |

Full decision-rule walkthrough, cold-start trade-offs, and observability details live in [docs/scaling.md#autoscaling](scaling.md#autoscaling). Live state is exposed via `/api/status` and `/api/mcp-servers` (see [api-reference.md](api-reference.md#get-apistatus)) and the `AUTOSCALE` column of `gridctl status --replicas`.

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
| `name` | string | **Yes** | - | Unique resource identifier |
| `image` | string | **Yes** | - | Docker image |
| `env` | map | No | - | Environment variables |
| `ports` | []string | No | - | Port mappings (e.g., `"5432:5432"`) |
| `volumes` | []string | No | - | Volume mounts (e.g., `"data:/var/lib/postgres"`) |
| `network` | string | Conditional | - | Network to join (required in advanced network mode) |

**Constraints:**
- Names must be unique and not conflict with MCP server names
- `image` is always required

---

## Clients (per-client access scoping)

The optional top-level `clients:` block restricts which servers and tools each
connecting client can reach. It follows Kubernetes NetworkPolicy semantics:

- **No `clients:` block** → every client sees every tool (the default, and the
  behavior of every stack written before this feature existed).
- **Block present** → a client matching a profile is limited to that profile's
  allow-list; a client matching no profile is governed by `default:`.

```yaml
clients:
  default: deny          # policy for unlisted clients: deny (default) or allow
  profiles:
    cursor:              # stable client identifier (see "Client identity" below)
      servers:           # allow-list of server names; empty = all servers
        - github
    claude-code:
      tools:             # allow-list of prefixed tool names; empty = all tools
        - github__search-repos
        - gitlab__list-issues
    team-bot:
      aliases:           # wire clientInfo.name values that map to this profile
        - "Custom Agent"
      servers:
        - github
```

### Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `default` | string | No | `deny` | Policy for clients matching no profile: `deny` or `allow` |
| `profiles` | map | No | - | Map of stable client identifier → allow-list |
| `profiles.<id>.servers` | []string | No | - | Allowed server names. Empty means all servers |
| `profiles.<id>.tools` | []string | No | - | Allowed prefixed tool names (`server__tool`). Empty means all tools within the allowed servers |
| `profiles.<id>.aliases` | []string | No | - | Raw `clientInfo.name` values that resolve to this profile |

A profile's effective scope is the intersection of its `servers:` and `tools:`
allow-lists with each server's own `tools:` whitelist. A profile with neither
`servers:` nor `tools:` is listed but unrestricted (sees everything). Unknown
server references (directly or via a tool prefix) fail config validation.

### Client identity

Enforcement keys on a **stable client identifier** that reconciles the wire
identity with the configuration and UI identity. It is resolved per session, in
priority order:

1. The `client` query parameter on the gateway URL, or the
   `X-Gridctl-Client-Id` header. `gridctl link --client-id <id>` embeds the
   query parameter into the URL it writes, so the identifier assigned at link
   time is exactly the one enforced and displayed.
2. A profile `aliases:` entry matching the connecting client's
   `clientInfo.name`.
3. The normalized `clientInfo.name` (the fallback heuristic).

All identifiers are normalized (lowercased, hyphenated) so configuration, the
wire, and the UI reconcile on one canonical form.

The identifier is self-declared by the connecting client (it sets its own
`client` parameter, header, or `clientInfo.name`). Per-client scoping is
therefore a least-privilege guardrail for cooperating clients, not an
authentication boundary against a hostile client that can choose its own
identity. Identity-based access control (IdP / OAuth / JWT) is out of scope.

### Scope coverage (v1)

Scoping covers **tools only**. Skills (served as MCP prompts) and resources
remain globally visible to every client. Extending scope to prompts and
resources is deferred.

### Reload semantics

The gateway re-resolves each client's scope from the live configuration on every
`tools/list` and `tools/call`. A `clients:` change applied via hot-reload (file
watch or `POST /api/reload`) therefore takes effect on the next request,
including for already-established sessions, with no restart required.

### Editing in the web UI

The Tools workspace has an **Access** button that opens a per-client editor:
pick which servers each linked client may reach and save. This writes a
server-level profile (`servers:` allow-list) to the `clients:` block via an
atomic, conflict-detected write and triggers a hot reload, so the Stack view
then reflects the new scope on the next poll. Saving the first profile creates
the `clients:` block, which flips clients you have not listed to the `default:`
policy (deny); the editor warns before that happens. Finer tool-level
allow-lists (`tools:`) are enforced by the gateway but edited directly in
stack.yaml.

Declaring which model a client runs for cost estimates is a separate,
access-inert concern — see [Client Models](#client-models-pricing-attribution)
below.

---

## Client Models (pricing attribution)

The optional top-level `client_models:` map declares which model each
connecting client runs, purely for cost attribution: tool calls from a
declared client are priced at that model's rates. This is **pricing, not
access** — the map never requires a `clients:` block, never restricts any
client, and has zero effect on the access policy described in
[Clients](#clients-per-client-access-scoping) above (which is access, not
pricing).

```yaml
gateway:
  default_model: claude-haiku-4-5   # pricing floor for anything not declared

client_models:
  claude-code: claude-opus-4-7
  gemini-cli: gemini-2.5-pro
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `client_models` | map | No | - | Stable client identifier → model ID used to price that client's tool calls. Keys are the same identifiers used by `clients.profiles` and shown on the Stack canvas (e.g. `claude-code`). Values are model IDs from the embedded LiteLLM pricing snapshot (e.g. `claude-opus-4-7`) |

Pricing resolution per tool call, highest precedence first: call-level usage
metadata (when a server reports one) > the calling client's `client_models`
entry > the target server's `model:` > `gateway.default_model`. Unknown model
IDs and keys that are not normalized client IDs surface as validation
warnings (never errors) and price as zero. Edits hot-reload without
restarting any server. All three tiers are editable in the web UI: inline in
the Metrics tab's client and server tables, or through the "Pricing models"
manager (Metrics toolbar, sidebar inspector, or command palette); see
[Cost Observability](cost-observability.md) for semantics and limitations.

---

## Limits (budgets and rate limits)

The optional top-level `limits:` block enforces spending caps and call rates
at tool-call dispatch. Omitting the block preserves legacy behavior: nothing
is ever limited. Both entry kinds scope to exactly one of `client`,
`server`, or `tool`.

```yaml
limits:
  budgets:
    - client: claude-code        # exactly one of client / server / tool
      max_usd: 5.00
      period: daily              # daily | weekly | monthly
      warn_at_percent: 80        # optional soft tier
    - server: github
      max_usd: 20
      period: weekly
  rate_limits:
    - server: github
      calls_per_minute: 30
      burst: 10                  # optional bucket capacity
    - tool: github__search_code
      calls_per_minute: 6
```

### Budget fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `client` / `server` / `tool` | string | One of | - | Scope key. `client` is the stable client identifier used by `clients.profiles` and `client_models`; `server` is a stack server name; `tool` is a prefixed name (`server__tool`) |
| `max_usd` | float | Yes | - | Dollar cap for the window; must be positive |
| `period` | string | Yes | - | `daily`, `weekly`, or `monthly`. Windows are calendar-aligned in the daemon's local timezone: daily resets at midnight, weekly on Monday 00:00, monthly on the 1st |
| `warn_at_percent` | int | No | - | 1-99. Crossing this percentage of the cap logs one WARN per window and surfaces a `warn` state in `gridctl limits` and `GET /api/limits` |

### Rate limit fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `client` / `server` / `tool` | string | One of | - | Scope key, same vocabulary as budgets |
| `calls_per_minute` | int | Yes | - | Sustained rate; must be positive |
| `burst` | int | No | max(5, rate/6) | Token-bucket capacity: how many calls may land at once before the sustained rate applies |

### Enforcement semantics

Enforcement is check-then-settle. A call is admitted against spend already
recorded; its own cost is settled after it completes, because cost is only
known once token usage is. Concurrent or in-flight calls can therefore
overshoot a cap by their own cost; the next matching call after the cap is
reached is denied. Denials are returned as in-band tool errors with the cap,
current consumption, reset time, and retry guidance, so agent LLMs stop
retrying instead of burning tokens.

**The attribution gap.** Budgets govern attributed cost only. A call is
priced when a model resolves for it (call-level usage metadata,
`client_models`, the server's `model:`, or `gateway.default_model`); a call
whose model cannot be priced records tokens but no dollars and therefore
spends outside every budget's sight. Rate limits need no pricing at all and
are the recommended backstop on any scope you cap.

Budget spend persists in a ledger under `~/.gridctl/limits/<stack>.json`
(independent of [Telemetry Persistence](#telemetry-persistence)), so a
daemon restart mid-window never refills a spent budget. Edits hot-reload:
current-window spend carries over for entries whose scope and period are
unchanged, and raising a cap never resets its counter. Consumption surfaces
in `gridctl limits` and `GET /api/limits`.

---

## Groups (tool bundles)

The optional top-level `groups:` block defines named cross-server tool
bundles, each served at its own MCP endpoint `/groups/{name}/mcp`. Groups
are the curation axis. The three axes compose in one sentence: the
per-server `tools:` whitelist narrows what exists, groups curate what an
endpoint shows, client scoping restricts what a client may touch, and all
three intersect. Omitting the block changes nothing; the default `/mcp`
endpoint always serves the full surface.

```yaml
groups:
  release:
    description: Release engineering bundle
    servers: [github]                      # include every tool of these servers
    tools: [gitlab__create_merge_request]  # include specific prefixed tools
    exclude: [github__delete_repo]         # subtract, applied last
    overrides:
      github__create_issue:
        name: create_issue                 # exposure-layer rename
        description: "File a release-blocking issue in the release repo."
        read_only_hint: false
        destructive_hint: true
```

### Group fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `description` | string | No | - | Shown in `gridctl groups` and the API |
| `servers` | []string | No | - | Include every tool of these stack servers |
| `tools` | []string | No | - | Include specific prefixed tool names |
| `exclude` | []string | No | - | Subtract prefixed tool names, applied after inclusion |
| `overrides` | map | No | - | Per-tool customization, keyed by canonical prefixed name; keys must be members |

Group names must match `^[a-z0-9][a-z0-9_-]{0,31}$`. A group must include at
least one server or tool.

### Override fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | No | - | Rename at the exposure boundary (flat, no `__`). Validated against the client-side 64-character `mcp__<group>__<name>` budget and for collisions within the group |
| `description` | string | No | - | Replace the tool's description verbatim |
| `read_only_hint` / `destructive_hint` / `idempotent_hint` / `open_world_hint` | bool | No | - | Inject or override MCP tool annotations. Unset hints pass the downstream server's own annotation through. A set hint is the operator vouching for the tool's behavior |

### Semantics

Renames exist only at the exposure boundary: dispatch, client scoping,
limits, schema pins, and telemetry always operate on canonical
`server__tool` names (an inbound renamed call is translated at the dispatch
entry, and the canonical name stays callable). Calls to tools outside a
group's surface are rejected with a model-readable error naming the group.
Client scoping applies on group sessions exactly as on `/mcp`: a tool the
caller's `clients:` profile excludes stays invisible and denied even under a
rename. Code mode on a group session searches and executes only the group's
surface, with renames shown server-prefixed so sandbox calls round-trip.

Schema-pin fingerprints hash the downstream definitions, so group rewrites
never cause drift; when an upstream tool does drift, `gridctl pins diff`
flags any group whose description override touches it, since the rewrite was
written against the old definition. A rename whose original tool name still
appears in an active skill logs a warning at startup.

Edits hot-reload: surfaces change on the next request, and connected clients
pick up membership changes on reconnect. Groups serve tools only; prompts
and resources remain globally visible (matching client scoping's v1
decision). Link a client to a group with `gridctl link <client> --group
<name>`; consumption appears in `gridctl groups` and `GET /api/groups`.

---

## Link (declared clients)

The optional top-level `link:` block declares which LLM clients should be
connected to this stack's gateway. `gridctl apply` reconciles it once the
gateway is healthy: each declared client is linked exactly as `gridctl link`
would, idempotently. Omitting the block preserves legacy behavior — linking
stays a manual step.

```yaml
link:
  - claude                # shorthand: client slug only
  - claude-code
  - client: cursor        # object form for options
    group: dev            # link the group endpoint; entry name defaults to gridctl-dev
    client_id: cursor     # stable identifier for a clients: access profile
    name: gridctl         # server entry name override in the client config
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `client` | string | **Yes** | - | Client slug (same set as `gridctl link`: claude, claude-code, cursor, windsurf, vscode, gemini, antigravity, opencode, grok, continue, cline, anythingllm, roo, zed, goose) |
| `group` | string | No | - | Tool group whose endpoint to link; must exist in `groups:`. The entry name defaults to `gridctl-<group>` |
| `client_id` | string | No | - | Stable client identifier embedded on the gateway URL for per-client access scoping. Not defaulted: existing imperative links carry no identifier, and defaulting one would conflict with them on first reconcile |
| `name` | string | No | `gridctl` | Server entry name in the client config |

Reconcile semantics:

- **Additive and idempotent.** Declared clients are linked if installed;
  already-linked clients are silent no-ops. Removing an entry never unlinks
  anything — removal stays explicit via `gridctl unlink`, `gridctl destroy
  --unlink`, or the Connections workspace.
- **Link if present.** A declared client that is not installed on this
  machine warns and skips; stack files travel between machines with
  different clients installed, so this is never an error.
- **Conflicts are never overwritten.** An existing foreign entry under the
  target name warns with a `gridctl link <client> --force` hint.
- **Apply-time only.** `link:` edits take effect on the next `gridctl
  apply`; the file watcher never writes client configs. `gridctl plan` shows
  pending link actions in a separate section (JSON: `clientLinks`).
- **One entry per client.** Linking two groups into the same client is not
  supported in v1.
- **Not inherited across `extends`** (matching `clients:`, `groups:`, and
  `limits:`).
- With a `link:` block present, `apply --flash` is ignored with a notice
  (the block owns the linking decision).

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
      credential_ref: "${var:GIT_TOKEN}"

  - name: private-ssh
    repo: git@github.com:acme/private-skills.git
    auth:
      method: ssh-agent
```

### All Skill Source Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | No | Derived from repo URL | Unique source name |
| `repo` | string | **Yes** | - | Git repository URL (HTTPS or SSH) |
| `ref` | string | No | Default branch | Branch, tag, or semver constraint (e.g. `^1.2`) |
| `path` | string | No | - | Subdirectory containing `SKILL.md` files |
| `auto_update` | bool | No | Inherits `defaults.auto_update` | Enable background updates for this source |
| `update_interval` | duration | No | Inherits `defaults.update_interval` | Poll interval (e.g. `1h`, `24h`) |
| `auth` | object | No | - | Authentication block for private repos (see [Auth](#skill-source-auth)) |

### Skill Source Auth

Declares how gridctl authenticates when cloning or fetching this repository. Raw tokens must **never** appear in `skills.yaml` - use `credential_ref` to point at a vault key.

```yaml
auth:
  method: token
  credential_ref: "${var:GIT_TOKEN}"
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `method` | string | **Yes** | - | One of `"token"`, `"ssh-agent"`, `"ssh-key"`, or `"none"` |
| `credential_ref` | string | Conditional | - | `${var:KEY}` reference resolved at clone/fetch time. Required for `"token"` |
| `ssh_user` | string | No | `git` | SSH username used with `"ssh-agent"` or `"ssh-key"` |
| `ssh_key_path` | string | Conditional | - | Path to a private key file. Required for `"ssh-key"` |

**Method behavior:**

| Method | Transport | Credential source | Persisted |
|--------|-----------|-------------------|-----------|
| `token` | HTTPS | `credential_ref` (vault) - resolved on every clone/fetch | Reference only |
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
| `${var:KEY}` | Variable store reference (error if key not found). Canonical syntax. |
| `${vault:KEY}` | Deprecated alias for `${var:KEY}`. Logs a one-shot warning per process. Removed at v1.0. |

Variable expansion is applied to string values across all configuration sections including `env`, `token`, and `url` fields.

### Variables vs Secrets

The variable store is unified: it holds both secrets and non-sensitive
configuration. The on-disk metadata distinguishes them:

| Stored as | CLI | Behaviour |
|-----------|-----|-----------|
| Secret (default) | `gridctl var set KEY` | Encrypted at rest when the store is locked; values are replaced with `[REDACTED]` in logs. |
| Plaintext | `gridctl var set KEY --value value --plaintext` | Stored alongside secrets but kept legible in logs and the web UI. |

Secrets and plaintext variables share the same lookup path: `${var:KEY}`
works for both. The unification means a `stack.yaml` can carry environment
knobs (region, cluster ID, account ID) without leaking them through redaction
fatigue, and without forcing the operator into a parallel `.env` workflow.

`gridctl var set --type {string|json|list|number|bool}` records a type
metadata field for each entry. PR 1 records the type only; PR 2 will wire
type-aware expansion so a `type=json` value can unmarshal directly into a
YAML mapping.

---

## Name Uniqueness

All names across MCP servers and resources share a single namespace. The following conflicts are rejected:

- Duplicate names within MCP servers or resources
- A resource name matching an MCP server name
