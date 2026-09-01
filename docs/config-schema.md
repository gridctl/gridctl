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
variables: ...
network: ...
networks: ...
mcp-servers: ...
resources: ...
clients: ...
limits: ...
groups: ...
link: ...
skills: ...
model_preferences: ...
experimental: ...
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
| `variables` | map | No | - | Value-free variable prerequisites (see [Variable declarations](#variable-declarations)) |
| `network` | object | No | See below | Single network configuration (simple mode) |
| `networks` | []object | No | - | Multiple network configurations (advanced mode) |
| `mcp-servers` | []object | No | - | MCP server definitions |
| `resources` | []object | No | - | Supporting container definitions (databases, caches, etc.) |
| `clients` | object | No | - | Per-client access scoping (see [Clients](#clients-per-client-access-scoping)) |
| `limits` | object | No | - | Rate limits enforced at dispatch (see [Limits](#limits-rate-limits)) |
| `groups` | map | No | - | Named tool bundles, each at its own endpoint (see [Groups](#groups-tool-bundles)) |
| `link` | []string\|object | No | - | LLM clients `gridctl apply` links to this gateway (see [Link](#link-declared-clients)) |
| `skills` | object | No | - | Global skill exposure policy: allow/deny name globs (see [Skills](#skills-exposure-policy)) |
| `model_preferences` | object | No | - | Model preference defaults and overrides for skill and agent projections (see [Model Preferences](#model-preferences)) |
| `experimental` | object | No | - | Feature flags for experimental behavior (see [Experimental](#experimental-feature-flags)) |

---

## Gateway

Optional gateway-level configuration for authentication, CORS, and code mode.

```yaml
gateway:
  bind: "0.0.0.0"
  allowed_origins:
    - "https://example.com"
  allowed_hosts:
    - "gridctl.internal"
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
| `bind` | string | No | `127.0.0.1` | Address the HTTP listener binds. Loopback by default, so the API, web UI, and gateway are unreachable from other hosts and from containers. Set `0.0.0.0` to listen on every interface. The `--bind` and `--bind-all` flags override this. **A non-loopback bind requires `auth`** — gridctl refuses to start otherwise |
| `insecure_allow_unauthenticated` | bool | No | `false` | Permit a non-loopback bind with no `auth` configured. Without it gridctl refuses to start in that combination. Exists as a config field as well as the `--insecure-allow-unauthenticated` flag, because a flag can be dropped by whatever wraps the process (launchd, a Homebrew service, a Dockerfile `CMD`). Warns loudly on every start |
| `allowed_origins` | []string | No | `["*"]` | CORS allowed origins. Empty or unset allows all |
| `allowed_hosts` | []string | No | `[]` | Extra `Host` header values accepted on the MCP endpoint (DNS rebinding protection). Loopback hosts are always accepted, so unset means loopback-only. Set only when a reverse proxy or container hostname fronts the gateway. Unlike `allowed_origins`, `"*"` is **not** a wildcard here and matches nothing |
| `auth` | object | No | - | Authentication configuration |
| `code_mode` | string | No | `"off"` | Enable code mode: `"on"` or `"off"` |
| `code_mode_timeout` | int | No | `30` | Code mode execution timeout in seconds. Must be >= 0 |
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

When `scan` is on, every tool definition is checked at pin and drift time for injection signals: hidden-instruction phrases (`P001`), references to sensitive files (`P002`), sensitive-action language (`P003`), suspicious emphasis words (`P004`), hidden Unicode including decoded Tags-block payloads (`P005`), and cross-server tool shadowing (`P006`). P006 warns when a description names another server's distinctively named tool, or a generic tool name (`search`, `fetch`) qualified by its owning server's name; a bare mention of another server is info-tier only. Matching runs on Unicode-normalized text so zero-width, homoglyph, and leetspeak evasion does not defeat it, and quoted matches are downgraded so a tool that documents attack phrases is not flagged as one. Findings render beside the drift diff in `gridctl pins diff`, the diff API, and the Pins workspace; they inform the approve decision and never gate it. Static heuristics are one detection layer, not a complete defense: attacks carried in runtime tool output are invisible to any pin-time check.

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

References variable sets from the encrypted variable store (`gridctl var`) for automatic secret injection into containers. The `secrets:` field name is kept for compatibility; the store behind it is the unified variable store, and the deprecated `${vault:KEY}` reference syntax is a compatibility alias for `${var:KEY}` (see [Variable Expansion](#variable-expansion)).

```yaml
secrets:
  sets:
    - production
    - shared
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `sets` | []entry | No | - | Variable sets to inject. Explicit `env` values take precedence |

### Scoping a set to named workloads

An entry written as a bare name injects every member of that set into every MCP
server and every resource. To narrow it, write the entry as a mapping and name
the workloads that should receive it:

```yaml
secrets:
  sets:
    - shared                  # every server and resource
    - name: github-creds      # only the github server
      servers:
        - github
    - name: db-creds          # only the postgres resource
      resources:
        - postgres
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | Yes | - | Variable set name |
| `servers` | []string | No | - | MCP servers that receive this set |
| `resources` | []string | No | - | Resources that receive this set |

A scoped entry reaches exactly what it names and nothing else, so
`servers: [github]` injects into the `github` server and into no resources at
all. Naming a server or resource the stack does not declare is a validation
error, because a typo would otherwise withhold credentials silently and surface
later as an opaque runtime auth failure.

Scoping is per entry and opt-in. An entry with neither `servers` nor
`resources` keeps the original fan-out, so stacks written before scoping
existed behave identically.

Scoping mistakes fail closed rather than granting more access than intended.
A misspelled key (`server:` instead of `servers:`) is rejected outright rather
than silently ignored, since a dropped key would leave the entry unscoped. An
explicitly empty scope (`servers: []`) means "no workloads", not "all
workloads", and is reported as an error because a set that injects nowhere is
almost always an unfinished edit.

Sets are applied in the order they are listed, and the first entry to supply a
key wins, matching the rule that explicit `env` values in the stack file beat
injected ones. In practice a key belongs to at most one set, so entries rarely
collide.

A scoped set may be listed only once. Repeated entries inject the union of
their scopes, so a bare `- dev` sitting above `- name: dev` with `servers:
[github]` would fan the set out to every workload while reading as though it
were confined to one. Repeating a bare name is still allowed, since that was
valid before scoping existed and injecting the same set twice changes
nothing.

The Variables workspace reflects scoping: a scoped set's variables list the
workloads they actually reach, and each one links to that node on the Stack
canvas.

### Reserved internal credentials

The `GRIDCTL_` namespace is reserved for gridctl bootstrap and control-plane
settings. `OP_CONNECT_TOKEN` and `OP_SERVICE_ACCOUNT_TOKEN` are reserved as
well. These keys cannot be added to the variable store and are filtered from
store-derived set injection and local MCP process inheritance. A
`${var:GRIDCTL_*}` or `${vault:GRIDCTL_*}` reference stays literal and reports a
reserved-credential resolution error; it never falls back to an ambient value.
The two reserved 1Password keys follow the same rule.

Existing stores are not rewritten merely because they contain a key accepted
by an older gridctl version. Unrelated variables remain usable, and the legacy
entry can still be listed, diagnosed, and deleted. Move any value meant for a
downstream workload to a non-reserved name, update its references, and remove
the old record:

```bash
gridctl var delete GRIDCTL_OLD_KEY --force
```

This policy is a control-plane boundary rather than a general secret-name
filter. Names such as `GITHUB_TOKEN` remain valid store keys. Ordinary
`$GRIDCTL_SETTING` and `${GRIDCTL_SETTING}` interpolation also remains valid
for non-credential settings, while the exact bootstrap credential names never
expand into downstream stack configuration.

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
    volumes:
      - /path/to/workspace:/workspace:ro
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_PERSONAL_ACCESS_TOKEN}"
```

### Container Server (source)

Builds and runs an MCP server from a Dockerfile or a generated Python build.

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

For an exact public PyPI release, gridctl can generate a digest-pinned,
non-root Python image without a Dockerfile:

```yaml
mcp-servers:
  - name: fetch
    source:
      type: pypi
      package: mcp-server-fetch
      ref: 0.6.0
```

`type: pypi` implies `runtime: python`, and generated Python servers default to
stdio. See [`examples/python-sources/`](../examples/python-sources/) for a
copy-paste stack.

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

The `operations` filter runs at tool-generation time: an excluded operation never becomes a tool at all. List raw `operationId` values from the spec, not the generated tool names - the two differ whenever an ID contains characters outside `[a-zA-Z0-9_-]`, and a list of generated names matches nothing.

In the web wizard, the OpenAPI Configuration section's Operations Filter loads the spec on demand (`POST /api/openapi/operations`) and lets you search and select operations by ID, path, method, or tag, writing the raw IDs for you. Because this filter decides what is generated, what it removes cannot be restored from the runtime `tools` whitelist; `tools` is the reversible filter, applied after generation.

### All MCP Server Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `name` | string | **Yes** | - | Unique server identifier |
| `image` | string | Conditional | - | Docker image (container servers) |
| `source` | object | Conditional | - | Build from source (see [Source](#source)) |
| `url` | string | Conditional | - | External server URL |
| `port` | int | Conditional | - | Container port for HTTP/SSE transport. Required for non-stdio container servers |
| `transport` | string | No | `"http"` | Transport mode: `"http"`, `"stdio"`, or `"sse"`. Generated Python sources default to `"stdio"` |
| `command` | []string | Conditional | - | Container entrypoint override, local process command, or SSH remote command |
| `env` | map | No | - | Environment variables |
| `build_args` | map | No | - | Docker build-time arguments (container servers only) |
| `volumes` | []string | No | - | Container mounts in `host:container[:mode]` form. The host path or volume name must be non-empty, the container destination must be a clean absolute path, and mode may be `ro` or `rw`. Valid only for image and source containers; every static, reloaded, and autoscaled replica receives the mounts |
| `network` | string | Conditional | - | Network to join (required in advanced network mode) |
| `ssh` | object | Conditional | - | SSH connection config (see [SSH](#ssh)) |
| `openapi` | object | Conditional | - | OpenAPI spec config (see [OpenAPI](#openapi)) |
| `auth` | object | No | - | External-server authentication: `type: bearer`, `header`, or `oauth` (see [External Server Authentication](#external-server-authentication)). URL servers only |
| `tools` | []string | No | - | Tool whitelist. Empty exposes all tools. The web wizard populates this from the live stack for running servers, and offers an optional probe of external-URL servers to discover their tools before deploy. Container / stdio / local-process / SSH servers are curated from the Stack sidebar after deploy; OpenAPI servers are curated before deploy with the wizard's Operations Filter (see [OpenAPI](#openapi-server)). Editable live from the Stack sidebar's Tools editor - `PUT /api/mcp-servers/{name}/tools` rewrites this field atomically and triggers a hot reload |
| `output_format` | string | No | - | Output format override: `"json"`, `"toon"`, `"csv"`, or `"text"`. Overrides `gateway.output_format` for this server |
| `pin_schemas` | bool | No | - | Override schema pinning for this server. `false` disables pinning regardless of gateway setting. Omit to inherit from `gateway.security.schema_pinning.enabled` |
| `ready_timeout` | duration | No | `30s` | Readiness wait for container-based HTTP/SSE servers. Accepts any `time.Duration` string (e.g. `"60s"`, `"2m"`). When a container does not become ready within this window, the container is stopped and removed; re-provisioning it requires a reload or re-apply (the automatic registration retry loop covers external, local process, SSH, OpenAPI, and stdio servers, but cannot respawn a removed container). Ignored for stdio, external, local process, SSH, and OpenAPI servers, which always use the 30s default; a server unreachable past that window is retried automatically once it comes up |
| `ping_timeout` | duration | No | `5s` | Per-ping deadline used by the gateway health monitor. Accepts any `time.Duration` string (e.g. `"10s"`). Tune this when a server's real `Ping` latency can exceed 5s - e.g. HTTP upstreams with many tools or under autoscale spawn load where the default flakes into spurious `context deadline exceeded` errors. Applies to every pingable transport (HTTP, SSE, stdio, local process, SSH, OpenAPI) |
| `protocol_generation` | string | No | `"auto"` | MCP protocol generation for this server: `"auto"` probes `server/discover` and falls back to the legacy `initialize` handshake; `"handshake"` and `"stateless"` skip the probe and force one generation. An escape hatch for peers the probe misclassifies; leave absent for normal auto-negotiation |
| `replicas` | int | No | `1` | Number of independent processes to spawn for this server. Values >1 load-balance JSON-RPC tool calls across replicas using `replica_policy`. Range: 1–32. Not supported for external URL or OpenAPI transports. Mutually exclusive with `autoscale`. See [Scaling](scaling.md) |
| `replica_policy` | string | No | `"round-robin"` | Dispatch policy when `replicas > 1` or `autoscale` is set: `"round-robin"` or `"least-connections"` |
| `autoscale` | object | No | - | Reactive autoscaling block. Mutually exclusive with `replicas`. Not supported for external URL or OpenAPI transports. See [Autoscale](#autoscale) |
| `telemetry` | object | No | - | Per-server telemetry persistence overrides. See [Per-server Overrides](#per-server-overrides) |

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
| `type` | string | **Yes** | - | Source type: `"git"`, `"local"`, or `"pypi"` |
| `url` | string | Conditional | - | Git repository URL (required for `git`). PyPI always uses the official public index and rejects this field |
| `ref` | string | Conditional | `"main"` for git | Git branch, tag, or commit. For PyPI, required and must be an exact published PEP 440 version; `latest` and ranges are rejected |
| `path` | string | Conditional | - | Local source root (required for `local`, resolved from the stack file). For git sources with `runtime: python`, a clean checkout-relative source or project subdirectory |
| `project_path` | string | No | - | Clean relative Python project subdirectory below a local source root. Valid only for local sources with `runtime: python`, and used when gridctl generates the Dockerfile |
| `dockerfile` | string | No | - | Explicit Dockerfile path relative to the effective source root. Omission selects generated Python when `runtime: python`; otherwise legacy Dockerfile discovery checks `Dockerfile`, `dockerfile`, then `Containerfile` |
| `runtime` | string | No | - | Set to `"python"` to generate a Python image when `dockerfile` is omitted. Implied for `type: pypi`; no other value is accepted |
| `package` | string | Conditional | - | Public PyPI distribution name, required and valid only for `type: pypi` |
| `python` | string | No | compatible version | Python minor version: `3.10`, `3.11`, `3.12`, or `3.13`. Omission selects the lowest compatible version, or `3.12` when metadata has no usable constraint |
| `extras` | []string | No | - | Normalized Python extras to install, such as `[http, cli]` |
| `with` | []string | No | - | Additional validated PEP 508 dependency specifiers passed to uv |
| `packages` | []string | No | - | Conservative Debian package names installed with `apt-get`; values are sorted and deduplicated |
| `auth` | object | No | - | Authentication block for private git repositories (see [Source Auth](#source-auth)). Not accepted for PyPI |

Generated git and local builds require static package metadata in
`pyproject.toml` or `setup.py`; gridctl reads the files as data and never
imports project code on the host. Git `path` and local `project_path` must stay
inside their source roots and may not traverse through an escaping symlink.
The selected project is copied into a gridctl-owned temporary build context,
so generation does not modify the checkout or local source tree.

Command selection uses the server-level `command` first. Without an explicit
command, gridctl selects the package's only console script, or the one whose
normalized name matches the package. Zero or ambiguous scripts fail before the
image build and ask for an explicit command. A PyPI release whose selected
metadata artifact does not expose console scripts likewise needs `command`.

Generated Dockerfiles use digest-pinned Python slim and Astral uv images and
run as a dedicated non-root user. PyPI installs use the exact `package==ref`.
Git and local projects use `uv sync --locked --no-dev --no-editable` when an
`uv.lock` exists; without one, uv installs the local package and transitive
dependencies are not locked. Host Python and uv are not required.

Before generation, gridctl normalizes, sorts, and deduplicates extras,
additional dependencies, and Debian packages. Invalid values fail validation
before any Dockerfile instruction is generated.

An explicitly non-empty `dockerfile` always selects the custom Dockerfile path,
even when `runtime: python` is present. A default-named Dockerfile merely
appearing in the source tree does not override generated Python. Python-only
installation fields apply to generated builds, not custom Dockerfiles.

Before building a Git source, gridctl fetches current remote refs, resolves the
configured branch, tag, or commit to a full commit SHA, and checks out that
commit in an isolated builder worktree. Concurrent source builds therefore do
not mutate one another or the skill-import cache.

Resolved build plans mark branch refs as mutable while recording the immutable
commit used by the build. Git, local, and PyPI builds receive
content-addressed image tags rather than `latest`. The tag contains a readable
source pin plus a short digest of the effective build inputs. The digest covers
the selected source or artifact identity, Dockerfile, Python selection,
build arguments, server command, and target platform, so changing an
output-affecting input produces a different image identity. Stack planning and
hot reload use the same complete effective MCP-server comparison; Python
options, source auth, build arguments, commands, replica settings, and other
server fields are not silently ignored.

Apply resolves and builds one desired source image per logical server before
checking existing containers or creating replicas. Static replicas and
autoscaled spawns all use that image. An existing container whose image does
not match is replaced. Hot reload prepares a changed source before stopping
the running server, so a resolution or build failure leaves the old workload
and its applied declaration in place. A later reload retries the unchanged
desired declaration. Existing git and local stacks that omit `runtime` keep
legacy Dockerfile discovery behavior.

An unchanged source build is reused only when the image tag and its
`io.gridctl.build-input-digest` label match the resolved plan. Images created
before this label existed rebuild once; the readable tag alone is not accepted
as cache identity. `gridctl apply --no-cache` skips this lookup and rebuilds
without changing the declarative image tag. Built images also carry
`io.gridctl.source-digest` and, when available, sanitized OCI source/revision
and generator/base-image provenance labels. URL credentials, query strings,
fragments, and secret values are never included in those labels.

### Source Auth

Declares how gridctl authenticates when cloning a private git repository at build time. Raw tokens must **never** appear in `stack.yaml` - use `credential_ref` to point at a variable-store key, which is resolved against the live store on every clone.

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

---

## Limits (rate limits)

The optional top-level `limits:` block enforces call rates at tool-call
dispatch. Omitting the block preserves legacy behavior: nothing is ever
limited. Each entry scopes to exactly one of `client`, `server`, or `tool`.

```yaml
limits:
  rate_limits:
    - server: github             # exactly one of client / server / tool
      calls_per_minute: 30
      burst: 10                  # optional bucket capacity
    - tool: github__search_code
      calls_per_minute: 6
```

### Rate limit fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `client` / `server` / `tool` | string | One of | - | Scope key. `client` is the stable client identifier used by `clients.profiles`; `server` is a stack server name; `tool` is a prefixed name (`server__tool`) |
| `calls_per_minute` | int | Yes | - | Sustained rate; must be positive |
| `burst` | int | No | max(5, rate/6) | Token-bucket capacity: how many calls may land at once before the sustained rate applies |

### Enforcement semantics

Each entry is a token bucket checked at dispatch. A call that finds the
bucket empty is denied as an in-band tool error carrying the configured
rate, the scope that tripped, and retry guidance, so agent LLMs stop
retrying instead of burning tokens. Edits hot-reload without restarting any
server. Current state surfaces in `gridctl limits` and `GET /api/limits`.

Earlier releases also supported dollar `budgets:` in this block; that layer
has been removed (see [Usage Observability](usage-observability.md)). A
leftover `budgets:` key is ignored by the loader.

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

## Skills (exposure policy)

The optional top-level `skills:` block is a global exposure policy for registry skills: it filters which skills the gateway serves via MCP prompts/resources and which the daemon projects into client skill directories. Omitting the block preserves legacy behavior — every active skill is exposed.

Not to be confused with [Skill Sources](#skill-sources), which configures where git-imported skills come from (`~/.gridctl/skills.yaml`); this block decides what an already-registered skill may reach.

```yaml
skills:
  default: allow          # fate of a skill matching neither list ("allow" | "deny")
  allow:
    - "incident-*"        # skill-name globs admitted even under default: deny
  deny:
    - "*refund*"          # globs hidden from exposure; deny beats allow
```

### Fields

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `default` | string | no | `allow` | Fate of a skill matching neither list: `allow` or `deny`. |
| `allow` | list of globs | no | — | Skill-name patterns (path.Match syntax) admitted even under `default: deny`. |
| `deny` | list of globs | no | — | Skill-name patterns hidden from exposure. A deny match always wins. |

### Semantics

Evaluation order per skill name: a `deny` match denies (naming the glob as the rule), then an `allow` match admits, then `default` decides. A denied skill is excluded from `prompts/list`, `resources/list`, `prompts/get`/`resources/read` (indistinguishable from an absent skill on the wire), and projection sync — but denial is a filter, never a state change: the skill keeps its draft/active/disabled state, stays visible in the Library and registry API flagged with the matching rule, and `gridctl apply` prints a warning for every active skill the policy hides. Recorded projections of a newly denied skill are skipped and reported, never silently removed. Unparseable glob patterns are rejected at validation.

The policy is global. Per-client skill scoping remains deferred, matching the `clients:` block's documented tools-only scope. Edits hot-reload without container restarts. The block is not inherited across `extends` (matching `clients`/`groups`/`limits`).

## Model Preferences

The optional top-level `model_preferences:` block sets model preference defaults and overrides for skill and agent projections. It manages the `model:` frontmatter in files gridctl projects into client directories; it is a preference layer, not enforcement (clients resolve their own model, and env vars or per-invocation parameters outrank projected frontmatter), and it is unrelated to the removed cost-attribution `model:`/`default_model` fields; nothing here measures, estimates, or prices anything.

```yaml
model_preferences:
  skills:
    rewrite: true            # opt-in; default false = surfacing only, nothing on disk changes
    default: sonnet          # applied where the author declared nothing
    overrides:               # exact registry names; beats the author's declaration, raise or lower
      incident-triage: opus
      simple-formatter: haiku
  agents:
    rewrite: true
    default: sonnet
```

### Fields

Each scope (`skills`, `agents`) carries the same three fields:

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `rewrite` | bool | no | `false` | Opts the scope into projection rewrite. False keeps pure pass-through: preferences are surfaced in the UI, CLI, and API but no projected file changes. |
| `default` | string | no | - | Preference applied where the author declared nothing. High blast radius with `rewrite: true`: every projected skill without a declared model is rewritten and forced to copy channel. Prefer `overrides` for early adoption. |
| `overrides` | map | no | - | Exact registry name to preference, applied regardless of the author's declaration, in either direction. Unknown names warn but never error (the skill may arrive later via pack). |

Values are Claude Code model aliases (`default`, `best`, `fable`, `sonnet`, `opus`, `haiku`, `sonnet[1m]`, `opus[1m]`, `opusplan`, `inherit`) or full model IDs. Alias resolution is provider-conditional; gridctl never claims which concrete version an alias resolves to.

### Semantics

Resolution per name: an override beats the author's declaration beats `default`. With `rewrite: true`, `gridctl skill project sync` writes the resolved preference into the projected file's frontmatter, never into the registry canonical, which stays byte-identical (skill pins hash the canonical, so policy can never trip pin drift). A skill projection whose resolved preference differs from its stored frontmatter cannot stay a symlink (the link points at the canonical), so it is forced to copy channel with the reason shown in status output as `copy (model policy)`. Rendered agent dialects (OpenCode, Copilot, Gemini CLI) continue to drop `model` (their vocabularies are not Claude's), reported per sync and in the honor matrix; the rewrite applies to identity copies only.

Policy binding: the daemon compiles the block from its loaded stack and applies it on every projection reconcile, including reconciles triggered by hot reload and manual `gridctl reload`; `gridctl skill project sync --stack <path>` applies it from the CLI, and `skill project status --stack <path>` judges staleness against it. A sync running without stack context never reverts a rewritten projection (status shows `model policy: unknown (no stack loaded)`); the explicit off switch is `rewrite: false` (or removing the covering default/override), which reconciles projections back to pass-through and restores symlink channel on the next policy-aware sync. Edits hot-reload without container restarts. The block is not inherited across `extends` (matching `clients`/`groups`/`limits`/`skills`).

Advisory lint (never blocks a deploy): `model-preference-unknown-alias` (a value is neither a known alias nor shaped like a model ID), `model-preference-unhonored` (a preference resolves for targets that ignore or drop it), and `model-preference-portability` (top-level `model:` in a SKILL.md is rejected by spec-strict consumers outside Claude Code; `metadata` is the portable placement).

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
| `client` | string | **Yes** | - | Client slug (same set as `gridctl link`: claude, claude-code, cursor, windsurf, vscode, gemini, antigravity, opencode, grok, continue, cline, anythingllm, lmstudio, roo, zed, goose) |
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

## Experimental (feature flags)

The optional top-level `experimental:` block enables registered experimental
feature flags by name. Omitting the block preserves legacy behavior — every
flag defaults to off, and an unchanged stack.yaml behaves identically across
upgrades.

```yaml
experimental:
  some_flag: true  # hypothetical; see the Registered flags table below
```

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `<flag name>` | bool | No | `false` | Enables the named experimental flag. Keys are snake_case and spelled identically to the stable config key the feature graduates to |

Registered flags:

| Flag | Stage | Since | Description |
|------|-------|-------|-------------|
| `transport_dual_stack` | graduated | 0.1.0 | MCP 2026-07-28 transport dual-stack; always on, per-server pinning via `protocol_generation` |

Semantics:

- **Warnings, never errors.** An unknown flag name warns at `gridctl apply`
  and `gridctl validate` and is ignored; the warning lists the valid names
  when any experimental flags are registered, and says "no experimental
  flags are registered in this build" otherwise. A graduated or removed
  flag name warns with a specific migration message. A stack.yaml written
  against a newer gridctl still deploys on this one.
- **Env override.** Each flag can be overridden per process with
  `GRIDCTL_EXPERIMENTAL_<NAME>` (upper snake_case), accepting the
  `strconv.ParseBool` vocabulary: `1`, `t`, `T`, `TRUE`, `true`, `True`,
  `0`, `f`, `F`, `FALSE`, `false`, `False`. The env value beats the YAML
  value; an unset variable defers to YAML; an unparseable value warns and
  is ignored.
- **Visibility.** Enabled flags appear in `GET /api/status` as `features`
  (with display metadata in `feature_details`), in `gridctl status --json`,
  and as a read-only chip plus per-flag rows in the web UI. Flags cannot be
  toggled from the UI; stack.yaml stays user-owned.
- **Hot reload.** Editing the block on a running stack re-resolves flags
  without restarting any containers.
- **Lifecycle.** Flags graduate by promotion to a real config block; the
  full lifecycle (stages, deadlines, and the graduation clock) is
  documented in [CONTRIBUTING.md](../CONTRIBUTING.md#experimental-feature-flags).

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
- `credential_ref` is the only credential field persisted; the live variable store is consulted on every remote operation so rotating a secret takes effect immediately.
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

Variable expansion is applied to string values across all configuration sections including `env`, `token`, `url`, and MCP-server `volumes` entries.

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

Stored records may also carry value-free `description`, `docs`, `example`, and
`deprecated` metadata. JSON import and export use those field names. `.env`
round trips use JSON-quoted `# @description=`, `# @docs=`, `# @example=`, and
`# @deprecated=` comments immediately before the variable. A blank line or a
consumed assignment resets pending markers.

### Variable declarations

An optional top-level `variables:` map documents prerequisites without
supplying values:

```yaml
variables:
  GITHUB_TOKEN:
    required: true
    secret: true
    type: string
    description: Token used by the GitHub server
    docs: https://docs.github.com/authentication

mcp-servers:
  - name: github
    env:
      GITHUB_TOKEN: ${var:GITHUB_TOKEN}
```

Defaults are `required: false`, `secret: true`, and `type: string`.
Declarations are advisory: they never contain defaults or values, participate
in expansion, write the store, prompt, or make a previously valid apply fail.
Across `extends`, parent declarations are inherited, child documentation
replaces parent text only when supplied, `required: true` wins in either layer,
and conflicting explicit type or sensitivity declarations are rejected. A
plaintext declaration cannot weaken a stored secret.

---

## Name Uniqueness

All names across MCP servers and resources share a single namespace. The following conflicts are rejected:

- Duplicate names within MCP servers or resources
- A resource name matching an MCP server name
