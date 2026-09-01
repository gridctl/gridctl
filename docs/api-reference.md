# REST API Reference

The gridctl gateway exposes a REST API for managing stacks, secrets, skills, packs, schema and skill pins, the global context (including rule fragments), agent projection and client wiring, usage telemetry and traces, optimize findings, daemon reset, and MCP protocol interactions. By default the gateway listens on port `8180`.

## Authentication

When `gateway.auth` is configured, authentication is required for `/api/*`, `/mcp`, `/sse`, `/message`, and `/.well-known/*` paths. Exempt: `/health`, `/ready`, CORS preflight (`OPTIONS`) requests, the static web UI (`GET /`), `/oauth/callback`, and the group MCP endpoints (`/groups/{name}/mcp`, `/groups/{name}/sse`).

**Bearer token:**
```bash
curl -H "Authorization: Bearer <token>" http://localhost:8180/api/status
```

**API key:**
```bash
curl -H "X-API-Key: <token>" http://localhost:8180/api/status
```

Token comparison uses constant-time equality to prevent timing attacks.

---

## Endpoints

### Health & Readiness

#### `GET /health`

Liveness check. Returns `200 OK` immediately without checking MCP server status.

**Auth:** No

```bash
curl http://localhost:8180/health
```

```
OK
```

#### `GET /ready`

Readiness check. Returns `200 OK` only when all MCP servers are connected and initialized. Returns `503 Service Unavailable` in two cases: any MCP server is not yet ready, or the daemon is running in stackless mode (no stack loaded yet - use `POST /api/stack/initialize` or the wizard to load one).

**Auth:** No

```bash
curl http://localhost:8180/ready
```

```
OK
```

---

### Status & Monitoring

#### `GET /api/status`

Returns the overall gateway status including servers, resources, sessions, and optional features.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/status
```

**Response:**
```json
{
  "gateway": {
    "name": "my-stack",
    "version": "0.1.0"
  },
  "mcp-servers": [
    {
      "name": "github",
      "transport": "stdio",
      "endpoint": "stdio://github",
      "initialized": true,
      "toolCount": 5,
      "tools": ["get_file_contents", "search_code", "list_commits", "get_issue", "get_pull_request"],
      "external": false,
      "localProcess": false,
      "ssh": false,
      "sshHost": "",
      "openapi": false,
      "openapiSpec": "",
      "healthy": true,
      "lastCheck": "2025-01-15T10:30:00Z",
      "healthError": "",
      "autoscale": {
        "min": 1,
        "max": 8,
        "current": 2,
        "target": 3,
        "targetInFlight": 3,
        "medianInFlight": 9,
        "lastScaleUpAt": "2025-01-15T10:29:12Z",
        "lastDecision": "up"
      }
    }
  ],
  "resources": [
    {
      "name": "postgres",
      "image": "postgres:16",
      "status": "running"
    }
  ],
  "sessions": 3,
  "registry": {
    "total": 5,
    "active": 3,
    "draft": 1,
    "disabled": 1
  },
  "code_mode": "on",
  "token_usage": {
    "session": {
      "input_tokens": 12400,
      "output_tokens": 8200,
      "total_tokens": 20600
    },
    "per_server": {
      "github": { "input_tokens": 8000, "output_tokens": 5000, "total_tokens": 13000 },
      "analytics": { "input_tokens": 4400, "output_tokens": 3200, "total_tokens": 7600 }
    },
    "format_savings": {
      "original_tokens": 25000,
      "formatted_tokens": 20600,
      "saved_tokens": 4400,
      "savings_percent": 17.6
    }
  }
}
```

| Field | Type | Description |
|-------|------|-------------|
| `gateway` | object | Gateway name, version, and active `tokenizer` (`embedded` or `api`, omitted when unset) |
| `mcp-servers` | []object | Status of each MCP server |
| `resources` | []object | Resource container status |
| `sessions` | int | Active Streamable HTTP session count (see [`/api/sessions`](#get-apisessions)) |
| `stack_name` | string | Active stack name (omitted in stackless mode) |
| `home` | string | Resolved home directory this daemon runs under (omitted when unresolvable); lets CLI subcommands detect a `GRIDCTL_HOME` mismatch |
| `registry` | object | Registry skill counts (omitted if empty) |
| `code_mode` | string | Code mode status (omitted if `"off"`) |
| `token_usage` | object | Token usage metrics (omitted if no metrics accumulator) |

**Token usage fields:**

| Field | Type | Description |
|-------|------|-------------|
| `session` | object | Aggregate token counts (`input_tokens`, `output_tokens`, `total_tokens`) |
| `per_server` | map | Token counts keyed by server name |
| `per_client` | map | Token counts keyed by normalized MCP client name (omitted when no per-client traffic has been observed) |
| `format_savings` | object | Savings from output format conversion (`original_tokens`, `formatted_tokens`, `saved_tokens`, `savings_percent`) |

**MCP server status** includes `outputFormat` (string, omitted when unset) showing the configured output format for each server and `autoscale` (object, omitted when the server has no autoscale block) described under [`/api/mcp-servers`](#get-apimcp-servers). Each registered server also reports `protocolVersion` (string, omitted when the server did not report one or has no MCP handshake, as with OpenAPI adapters) carrying the MCP protocol version negotiated at initialize, and `protocolGeneration` (string, `"handshake"` or `"stateless"`, omitted for OpenAPI adapters) carrying the resolved MCP protocol generation. `/api/sessions` responses carry `entries`, one `{id, generation, protocolVersion}` object per active session, alongside the legacy bare `sessions` ID list. A server that failed gateway registration (unreachable endpoint, initialize failure, or unsupported protocol version) still appears in the list with `registrationFailed: true`, `healthy: false`, the failure reason in `healthError`, `initialized: false`, and no replicas, so declared servers are never silently absent. A retryable failure (the server was not reachable) is not terminal: the gateway re-attempts registration on the health-monitor cadence with exponential backoff, `healthError` carries a `retrying in Ns` hint while the loop runs, and the row flips to a normal registered server once the backend becomes reachable. Authorization failures and configuration errors are not retried, and `POST /api/mcp-servers/{name}/restart` on a retrying server forces an immediate attempt instead of returning 404.

**Experimental flag fields** appear at the top level when any experimental flag is enabled (via the stack's `experimental:` block or a `GRIDCTL_EXPERIMENTAL_*` env override), and are omitted otherwise:

| Field | Type | Description |
|-------|------|-------------|
| `features` | map | Each enabled flag name mapped to `true` — the capability-bit view for UI gating |
| `feature_details` | array | `{name, stage, description}` display metadata for the same flags, sorted by name. Read-only: flags are configured in `stack.yaml`, never toggled over the API |

#### `GET /api/sessions`

Returns the active Streamable HTTP MCP sessions. The count agrees with the `sessions` field of [`/api/status`](#get-apistatus) by construction: both surfaces report the gateway's session manager, and transport records for sessions the manager has expired (idle past the cleanup cutoff; clients that crash never send the graceful `DELETE /mcp`) are excluded and torn down rather than accumulating.

Each entry carries the session's client identity: `clientName` and `clientVersion` are the client-supplied `clientInfo` from initialize, and `accessId` is the normalized identifier that matches provisioner client slugs, so callers can attribute a session to a linked client by string equality.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/sessions
```

**Response:**
```json
{
  "count": 1,
  "sessions": ["sess-abc123"],
  "entries": [
    {
      "id": "sess-abc123",
      "generation": "handshake",
      "protocolVersion": "2025-11-25",
      "clientName": "claude-code",
      "clientVersion": "2.1.0",
      "accessId": "claude-code"
    }
  ]
}
```

#### `GET /api/mcp-servers`

Returns MCP server status details. Response fields match the `mcp-servers[]` entries under [`/api/status`](#get-apistatus).

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/mcp-servers
```

**Autoscale fields** - servers configured with an `autoscale` block (see [config-schema.md#autoscale](config-schema.md#autoscale)) include an `autoscale` object in their status:

| Field | Type | Description |
|-------|------|-------------|
| `min` | int | Configured floor |
| `max` | int | Configured ceiling |
| `current` | int | Running replica count at the last controller tick |
| `target` | int | Desired replica count at the last controller tick |
| `targetInFlight` | int | Configured per-replica in-flight target |
| `medianInFlight` | int | Rolling median in-flight request count across healthy replicas |
| `lastScaleUpAt` | string | RFC3339 timestamp of the most recent scale-up (omitted when none) |
| `lastScaleDownAt` | string | RFC3339 timestamp of the most recent scale-down (omitted when none) |
| `lastDecision` | string | `"up"`, `"down"`, or `"noop"` - what the controller just decided |
| `warmPool` | int | Configured warm-pool (omitted when 0) |
| `idleToZero` | bool | Configured scale-to-zero (omitted when false) |

#### `GET /api/tools`

Returns all aggregated tools from registered MCP servers.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/tools
```

#### `GET /api/tools/catalog`

Returns the full downstream tool inventory (each tool's raw description and input schema) for the web console, regardless of code mode. Read-only and informational: it does not change what MCP clients see from `tools/list`. The response shape matches [`/api/tools`](#get-apitools).

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/tools/catalog
```

**Response:**
```json
{
  "tools": [
    {
      "name": "github__get_file_contents",
      "description": "Get file contents from a repository",
      "inputSchema": { "type": "object", "properties": {} }
    }
  ]
}
```

#### `GET /api/tools/usage`

Returns per-(server, tool) usage observed by the gateway: cumulative call count, last-called timestamp, and token counts. Powers the Tools workspace **Audit Mode** (which separates actively-used, configured-but-unused, and disabled tools), the Tools detail panel's Usage section, and the Metrics workspace's Tools scope.

Usage is recorded for both direct tool calls and tools invoked through code mode's `execute` (both flow through the same observer). For servers with metrics persistence enabled, the data is restored from disk on startup so it survives gateway restarts; otherwise it reflects activity since the last gateway start.

`observedSince` is when this gateway process began recording. With persistence enabled, restored counts and timestamps may predate it; clients should treat tools absent from `servers` (or with no `lastCalledAt`) as "no recorded calls" rather than asserting a longer disuse history than `observedSince` supports.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/tools/usage
```

**Response:**
```json
{
  "observedSince": "2026-05-20T10:00:00Z",
  "servers": {
    "github": {
      "create_issue": { "calls": 42, "lastCalledAt": "2026-05-24T09:13:00Z", "inputTokens": 5120, "outputTokens": 18400 },
      "list_repos": { "calls": 3, "lastCalledAt": "2026-05-21T08:00:00Z", "inputTokens": 240, "outputTokens": 900 }
    }
  }
}
```

`servers` is an object keyed by server name; each value maps unprefixed tool names to their stats. Tools that have never been called are omitted. `inputTokens` and `outputTokens` are the cumulative tokens of the tool's own calls (omitted when zero). Returns `503` when no metrics accumulator is configured.

#### `GET /api/skills/usage`

Returns per-skill cumulative `prompts/get` usage observed by the gateway: a call count and the last-called timestamp for each registry skill that has been served. Powers the Skills Library's usage labelling. The data is seeded from disk on startup when metrics persistence is enabled, so it survives gateway restarts; otherwise it reflects activity since the last gateway start.

`observedSince` is when this gateway process began recording; with persistence enabled, restored counts may predate it, so the Library uses it to label the young-tracking-window case rather than calling a skill unused. Both `observedSince` and a skill's `lastCalledAt` are rendered as `null` (not omitted) when no value exists, keeping the join shape stable.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/skills/usage
```

**Response:**
```json
{
  "observedSince": "2026-05-20T10:00:00Z",
  "skills": {
    "code-review": { "calls": 17, "lastCalledAt": "2026-05-24T09:13:00Z" },
    "release-notes": { "calls": 2, "lastCalledAt": null }
  }
}
```

`skills` is always a non-nil object (`{}` when nothing has been served). Returns `503` when no metrics accumulator is configured.

#### `GET /api/logs`

Returns structured log entries from the gateway log buffer.

**Auth:** Yes

| Query Param | Type | Default | Description |
|-------------|------|---------|-------------|
| `lines` | int | `100` | Number of recent log entries to return |
| `level` | string | - | Comma-separated level filter (e.g., `"ERROR,WARN"`) |

```bash
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8180/api/logs?lines=50&level=ERROR,WARN"
```

**Response:**
```json
{
  "logs": [
    { "level": "INFO", "ts": "2026-07-24T10:00:00.000Z", "msg": "tool call finished", "trace_id": "abc123", "attrs": { "server": "github", "tool": "create_issue", "client": "claude-code", "replica_id": 0 } }
  ],
  "total": 812,
  "bufferCapacity": 1000
}
```

`logs` is the requested window of buffered entries in chronological order. `total` is the number of entries currently in the ring buffer and `bufferCapacity` its maximum, so callers can label the window honestly against retention. When no log buffer is configured, `logs` is `[]` and both counters are `0`.

When `level` is set, the whole buffer is scanned newest-first for up to `lines` entries of the requested levels, so sparse severities are returned even when the most recent entries are all other levels. The web UI's Logs workspace polls this endpoint (window size selectable as 200, 500, or 1000 entries, 500 by default) and filters client-side.

Tool-call log lines carry `server`, `tool`, `replica_id`, and (when the caller is identified) `client` in `attrs`, plus a top-level `trace_id` when tracing is enabled, for correlation with `/api/traces`.

#### `GET /api/clients`

Returns detected LLM clients and their link status.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/clients
```

**Response:**
```json
[
  {
    "name": "Claude Desktop",
    "slug": "claude",
    "detected": true,
    "linked": true,
    "transport": "sse",
    "configPath": "/Users/user/Library/Application Support/Claude/claude_desktop_config.json",
    "effectiveScope": {
      "configured": true,
      "unscoped": false,
      "servers": ["github"],
      "tools": ["github__search-repos", "github__create-issue"]
    }
  }
]
```

`effectiveScope` is the backend-computed per-client access scope when a
`clients:` block is configured (servers and prefixed tools the client can
reach). It is absent when no scoping is in effect.

When the stack has a `link:` block, declared clients additionally carry
`declared: true` and a `linkEntry` object with the declared options (`group`,
`clientId`, `name`), so the UI can render desired state (declared) next to
actual state (linked). For a declared entry whose resolved server name differs
from the default (a `group` or `name` override), `linked` reflects that
resolved entry name.

`notes` is a string array of client-specific post-link guidance declared by
the client's provisioner (currently only LM Studio), omitted when empty. The
Connections detail pane renders it verbatim.

#### `POST /api/clients/{slug}/scope/preview`

Computes what committing a per-client access-scope draft would do, without touching the stack file. Returns the exact YAML patch the matching `PUT .../scope` would write plus a per-client impact summary, so the UI's commit gate can render the consequences (and block a lockout) before saving.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"servers": ["github"], "tools": []}' \
  http://localhost:8180/api/clients/cursor/scope/preview
```

**Request body:** same shape as the scope `PUT` (`servers` and `tools` allow-lists). At least one of `servers` or `tools` must be set.

**Response (200):**
```json
{
  "client": "cursor",
  "profileKey": "cursor",
  "createsBlock": true,
  "lockout": false,
  "totalServers": 4,
  "totalTools": 43,
  "diff": "--- stack.yaml\n+++ stack.yaml\n@@ ...",
  "selected": {
    "name": "Cursor",
    "slug": "cursor",
    "beforeServers": 4,
    "afterServers": 1,
    "beforeTools": 43,
    "afterTools": 12,
    "lostServers": ["gitlab", "jira", "slack"],
    "gainedServers": []
  },
  "affected": []
}
```

`createsBlock` is `true` when no `clients:` block exists yet, in which case `affected` lists the other linked clients that flip to default-deny. `lockout` is `true` when the resulting scope would leave the client able to reach nothing.

**Errors:** `400 invalid_client` (empty after normalization) or a missing scope axis; `422` (`unknown_server`/`unknown_tool`) when the draft references a server or tool the gateway does not know; `503` when no stack file is configured or the gateway is unavailable.

#### `PUT /api/clients/{slug}/scope`

Sets a client's server-level access profile in the `clients:` block of the live
stack YAML and triggers a hot reload. The slug is normalized to the stable
profile key used for enforcement. The write is atomic and conflict-detected.

**Auth:** Yes

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"servers": ["github"], "tools": []}' \
  http://localhost:8180/api/clients/cursor/scope
```

**Request body:** `servers` and `tools` are allow-lists; an empty `tools`
leaves the client unrestricted within its allowed servers.

**Response (200):**
```json
{
  "client": "cursor",
  "profileKey": "cursor",
  "servers": ["github"],
  "tools": [],
  "reloaded": true,
  "reloadedAt": "2026-05-29T17:00:00Z"
}
```

**Errors:** `422` (`unknown_server`/`unknown_tool`) when the scope references a
server or tool the gateway does not know; `409` (`stack_modified`) when the
stack file changed on disk since it was read; `502` (`reload_failed`) when the
write succeeded but the hot reload failed.

---

### Token Metrics

#### `POST /api/clients/{slug}/link`

Links a client to the gateway (writing its own config file, exactly as
`gridctl link` would) and declares it in the stack's `link:` block, so the UI
and stack.yaml stay in lockstep. The dual write is ordered: the stack patch is
precomputed first (a malformed stack rejects the request with no host write),
then the client config is written, then the stack file. These endpoints write
files in the operator's home directory — the same local-operator capability
the vault and stack-editing endpoints already assume.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"group": "dev", "clientId": "cursor"}' \
  http://localhost:8180/api/clients/cursor/link
```

**Request body (all optional):** `group` links the tool group's endpoint (the
entry name defaults to `gridctl-<group>`), `clientId` binds the link to a
`clients:` access profile, `name` overrides the server entry name.

**Response (200):**
```json
{
  "client": "cursor",
  "serverName": "gridctl-dev",
  "linked": true,
  "declared": true,
  "configPath": "/home/user/.cursor/mcp.json"
}
```

`alreadyLinked: true` is added when the client config already carried the
identical entry (declaring adopts it).

**Errors:** `404 unknown_client`; `422 client_not_detected`; `409
link_conflict` when a foreign entry occupies the target name (nothing is
written); `500 stack_not_updated` when the client config was written but the
stack file was not (external edit or write failure — both facts are in the
message; nothing is rolled back); `503` when no stack file is configured.

#### `DELETE /api/clients/{slug}/link`

Removes the client's gateway entry and its `link:` declaration, unlink-first.
The declared entry fixes the server name to remove; an undeclared client falls
back to the default. Returns `404` when the client is neither linked nor
declared, and `500 stack_not_updated` when the unlink succeeded but the stack
write did not.

**Auth:** Yes

#### `POST /api/clients/{slug}/link/preview`

Computes what linking would change without writing anything: the client config
before and after, plus the unified diff of the stack.yaml `link:` patch.

**Auth:** Yes

**Response (200):**
```json
{
  "client": "cursor",
  "serverName": "gridctl",
  "configPath": "/home/user/.cursor/mcp.json",
  "before": "{ ... current config ... }",
  "after": "{ ... config with gridctl entry ... }",
  "stackDiff": "--- stack.yaml\n+++ stack.yaml\n@@ ..."
}
```

#### `GET /api/metrics/tokens`

Returns historical token usage time-series data.

**Auth:** Yes

| Query Param | Type | Default | Description |
|-------------|------|---------|-------------|
| `range` | string | `"1h"` | Time range: `"30m"`, `"1h"`, `"6h"`, `"24h"`, `"7d"` |

```bash
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8180/api/metrics/tokens?range=1h"
```

**Response:**
```json
{
  "range": "1h",
  "interval": "1m",
  "data_points": [
    {
      "timestamp": "2026-03-12T10:00:00Z",
      "input_tokens": 1200,
      "output_tokens": 800,
      "total_tokens": 2000
    }
  ],
  "per_server": {
    "github": [
      {
        "timestamp": "2026-03-12T10:00:00Z",
        "input_tokens": 1200,
        "output_tokens": 800,
        "total_tokens": 2000
      }
    ]
  }
}
```

#### `DELETE /api/metrics/tokens`

Clears all accumulated token metrics.

**Auth:** Yes

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/metrics/tokens
```

**Response:**
```json
{"status": "ok", "message": "Token metrics cleared"}
```

---

### Optimize

#### `GET /api/optimize`

Returns an `OptimizeReport` derived from gateway-observed data: the registered server list, per-server token totals, and per-(server, tool) call counts. Heuristics cover unused servers, unused tools, schema overhead, and format-savings shortfalls; gateways with less than 24h of observation get a single `info` finding ("need more data") so reports never over-fire.

**Auth:** Yes

| Query Param | Type | Default | Description |
|-------------|------|---------|-------------|
| `stack` | string | - | Active stack name. `404` if it does not match. |
| `min_impact` | int | `0` | Drop findings whose projected weekly token impact is below this threshold. `info` findings are always retained. |
| `severity` | string | - | Comma-separated allowlist of `info`, `warn`, `critical`. |

```bash
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8180/api/optimize?min_impact=5000"
```

**Response:**
```json
{
  "findings": [
    {
      "id": "unused-server-github",
      "heuristic": "unused_server",
      "severity": "warn",
      "title": "Unused server: github",
      "summary": "Server 'github' has registered 12 tools but no calls have been observed.",
      "server": "github",
      "impact_tokens_per_week": 18000,
      "remediation": "# Remove the server entirely:\nmcp-servers:\n  # delete the entry for: github\n",
      "detected_at": "2026-05-07T12:00:00Z"
    }
  ],
  "health_score": 90,
  "generated_at": "2026-05-07T12:00:00Z"
}
```

`impact_tokens_per_week` is the projected weekly token saving: schema heuristics assume roughly 500 prompts per week, and `format_savings_shortfall` normalizes its measured savings over the observation window. Findings that cannot be projected report `0`. Returns `503` when no metrics accumulator is configured; `404` when `stack` does not match the active stack.

---

### Groups

#### `GET /api/groups`

Returns every tool group declared under `groups:` in stack.yaml, resolved against the live tool surface. Backs `gridctl groups`. Always `200`: with no groups configured the payload carries `configured: false` and an empty array. Each group also serves MCP at `GET|POST|DELETE /groups/{name}/mcp` (and a negotiation hint at `GET /groups/{name}/sse`); unknown group names return `404` before any session is created.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/groups
```

**Response:**
```json
{
  "configured": true,
  "groups": [
    {
      "name": "release",
      "description": "Release engineering bundle",
      "endpoint": "/groups/release/mcp",
      "member_count": 12,
      "tools": ["create_issue", "github__search_code", "gitlab__create_merge_request"],
      "overrides": {"github__create_issue": "create_issue"}
    }
  ]
}
```

`tools` are the exposed (post-rename) names; `overrides` maps canonical member names to their renames (empty string for description- or annotation-only overrides). The pins drift endpoint (`GET /api/pins/{server}/diff`) adds a `groups_rewriting` array to any drifted tool whose description a group rewrites.

---

### Limits

#### `GET /api/limits`

Returns the state of every rate limit declared under `limits:` in stack.yaml. Backs `gridctl limits` and the Metrics workspace. Always `200`: with no limits configured the payload carries `configured: false` and an empty `entries` array.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/limits
```

**Response:**
```json
{
  "configured": true,
  "entries": [
    {
      "kind": "rate",
      "scope": "server",
      "key": "github",
      "state": "ok",
      "rate": {
        "calls_per_minute": 30,
        "burst": 10
      }
    }
  ]
}
```

`kind` is always `"rate"`; it stays on the wire so consumers written against the earlier mixed budget/rate era keep parsing. `state` is `ok` or `exceeded` (the token bucket is currently empty). Hot-reload edits to the `limits:` block are reflected on the next request.

---

### Traces

Read the gateway's in-memory distributed-trace buffer. Each trace captures the spans for one upstream operation (tool call, prompt, etc.).

#### `GET /api/traces`

Returns recent trace summaries, newest first.

**Auth:** Yes

| Query Param | Type | Default | Description |
|-------------|------|---------|-------------|
| `server` | string | - | Filter to traces for this server |
| `errors` | bool | `false` | When `true`, return only traces that contain an error |
| `minDuration` | string | - | Minimum duration: a Go duration (e.g. `"250ms"`, `"2s"`) or a bare integer in milliseconds (e.g. `500`). Unparseable values return `400` |
| `limit` | int | `100` | Maximum number of traces to return |

```bash
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8180/api/traces?errors=true&limit=20"
```

**Response:**
```json
{
  "traces": [
    {
      "traceId": "a1b2c3",
      "rootSpanId": "root-1",
      "operation": "github › create_issue",
      "tool": "create_issue",
      "client": "claude-code",
      "server": "github",
      "startTime": "2026-05-29T17:00:00.123456789Z",
      "duration": 142,
      "spanCount": 3,
      "hasError": false,
      "status": "ok"
    }
  ],
  "total": 1,
  "tracingEnabled": true,
  "bufferSize": 42,
  "bufferCapacity": 1000
}
```

`duration` is in milliseconds. `status` is `"ok"` or `"error"`. `tool` is the resolved tool name (empty when the call failed before routing); `client` is the connecting client's name (empty when the client did not identify itself). `bufferSize` and `bufferCapacity` describe ring-buffer occupancy against `gateway.tracing.max_traces`. When no trace buffer is configured the envelope is empty with `"tracingEnabled": false`, which distinguishes disabled tracing from an enabled but quiet buffer.

#### `GET /api/traces/{traceId}`

Returns the full span tree for a single trace.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/traces/a1b2c3
```

**Response:**
```json
{
  "traceId": "a1b2c3",
  "spans": [
    {
      "spanId": "root-1",
      "parentSpanId": "",
      "name": "github › create_issue",
      "startTime": "2026-05-29T17:00:00.123456789Z",
      "endTime": "2026-05-29T17:00:00.265456789Z",
      "duration": 142,
      "status": "ok",
      "attributes": { "server.name": "github", "mcp.tool.name": "create_issue" },
      "events": []
    },
    {
      "spanId": "span-2",
      "parentSpanId": "root-1",
      "name": "mcp.client.call_tool",
      "startTime": "2026-05-29T17:00:00.128456789Z",
      "endTime": "2026-05-29T17:00:00.260456789Z",
      "duration": 132,
      "status": "ok",
      "attributes": { "server.name": "github", "tool.name": "create_issue" },
      "events": [
        { "name": "retry", "timestamp": "2026-05-29T17:00:00.150456789Z", "attributes": { "reason": "backoff" } }
      ]
    }
  ]
}
```

`parentSpanId` is empty for root spans. `endTime` is RFC3339Nano and may be absent for traces persisted before it was serialized; clients should derive `startTime + duration` in that case. Returns `404` when the trace ID is not in the buffer.

#### `GET /api/traces/{traceId}/otlp`

Returns a single trace as an OTLP/JSON `TracesData` document, suitable for an OTel Collector `file` receiver or any OTLP JSON decoder. Follows the OTLP/JSON encoding rules: lowerCamelCase keys, `traceId`/`spanId` as hex strings, and nanosecond timestamps as JSON strings. Served with a `Content-Disposition: attachment` header.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/traces/a1b2c3/otlp -o trace.json
```

Returns `404` when the trace ID is not in the buffer or tracing is disabled.

---

### Hot Reload

#### `POST /api/reload`

Triggers a configuration reload from the stack file. Requires the gateway to have been started with `--watch`.

When a source-based server changes, reload resolves and builds its desired
image before unregistering or stopping the running server. A resolution or
build failure is reported for that server while its old workload remains in
place; a successful build is passed to every replacement or autoscaled replica.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/reload
```

**Response (success):**
```json
{
  "success": true,
  "message": "Reload complete",
  "added": ["new-server"],
  "removed": [],
  "modified": ["existing-server"]
}
```

**Response (no changes):**
```json
{
  "success": true,
  "message": "No changes detected"
}
```

**Response (error):**
```json
{
  "success": false,
  "message": "validation errors:\n  - mcp-servers[0].port: must be a positive integer"
}
```

Returns `503` if reload is not enabled (gateway started without `--watch`).

---

### Reset

REST half of `gridctl reset`: removes everything gridctl placed on the machine (projected skills, agents, and context rules, owned gateway entries in client MCP configs, and every stack's daemons, containers, and networks). The default tier preserves `~/.gridctl`; `purge: true` deletes it too. Both endpoints share the same engine and guards as the CLI; the web UI's Reset dialog is built on them.

**Transport guards (both endpoints):** requests are accepted only from loopback connections; a request carrying an `Origin` header must be same-origin; and `Content-Type: application/json` is required (so the request can never be a CORS simple request). Violations return `403` (or `415` for the content type). These guards apply on top of gateway auth.

#### `POST /api/reset/preview`

Computes the reset inventory without writing anything and issues the single-use confirm token the execute call must present.

**Auth:** Yes (plus the transport guards above)

**Request:**
```json
{ "purge": false, "force": false }
```

**Response:**
```json
{
  "confirm_token": "<single-use token>",
  "confirm_phrase": "/Users/you/.gridctl",
  "doc": { "...": "the dry-run result document, grouped per client" }
}
```

The token is bound to the exact `purge` and `force` combination it was issued for and expires after five minutes. `confirm_phrase` is the resolved state-directory path a purge must echo back; it is a deliberate-attention gate printed by the preview, not a secret.

#### `POST /api/reset`

Executes the reset. Requires a live preview token; purge additionally requires the resolved-path confirm phrase, so the UI gate is server-enforced rather than decorative.

**Auth:** Yes (plus the transport guards above)

**Request:**
```json
{
  "purge": true,
  "force": false,
  "confirm_token": "<from the preview>",
  "confirm_phrase": "/Users/you/.gridctl"
}
```

**Response:** the full result document, flushed before the serving daemon dismantles itself (the daemon sits inside the blast radius, so teardown of its own process, state file, and any purge removal is deferred until after the response is written).

**Errors:**
- `403` / `415` - transport guard violations (non-loopback, cross-origin, wrong content type)
- `409` - a reset is already running
- `422` - missing, expired, already-used, or wrong-tier confirm token (re-run the preview), or a purge whose `confirm_phrase` does not equal the resolved path
- `503` - reset engine not initialized

---

### Stack Management

Endpoints for validating, inspecting, and editing the active stack spec. Most write paths use the same lock + hash + atomic-write pattern as the tool-whitelist editor: concurrent external edits surface as `409 stack_modified`, and a successful write may trigger a hot reload (`502 reload_failed` when the YAML saved but reload failed).

#### `POST /api/stack/validate`

Validates a stack YAML body without saving. Matches `gridctl validate` semantics (env expansion, defaults, full rule set).

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/yaml" \
  --data-binary @stack.yaml \
  http://localhost:8180/api/stack/validate
```

**Response:** `ValidationResult` JSON (`valid`, `errorCount`, `warningCount`, `issues[]`).

#### `GET /api/stack/plan`

Compares the on-disk stack file against the running state and returns a plan diff. Powers the canvas drift indicator.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/stack/plan
```

Returns `503` when no stack is deployed.

#### `GET /api/stack/health`

Returns aggregate spec health: validation status, drift vs running state, dependency resolution, and per-replica health for multi-replica servers.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/stack/health
```

**Response:**
```json
{
  "validation": { "status": "valid", "errorCount": 0, "warningCount": 0 },
  "drift": { "status": "in-sync" },
  "dependencies": { "status": "resolved" },
  "replicas": {
    "github": [
      { "replicaId": 0, "state": "healthy", "inFlight": 0, "uptimeSeconds": 3600 }
    ]
  }
}
```

`validation.status` is `valid`, `warnings`, or `errors`. `drift.status` is `in-sync`, `drifted`, or `unknown`.

#### `GET /api/stack/spec`

Returns the raw `stack.yaml` content for the active stack.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/stack/spec
```

**Response:**
```json
{ "path": "/path/to/stack.yaml", "content": "version: \"1\"\n..." }
```

#### `GET /api/stack/export`

Returns the active stack as sanitized exportable YAML (sensitive env values replaced with `${var:KEY}` placeholders; values already written as `${var:KEY}` or the deprecated `${vault:KEY}` alias are left untouched).

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/stack/export
```

**Response:**
```json
{ "content": "version: \"1\"\n...", "format": "yaml" }
```

#### `GET /api/stack/recipes`

Returns built-in stack templates for the wizard.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/stack/recipes
```

**Response:** JSON array of `{id, name, description, category, spec}` objects.

#### `GET /api/catalog`

Searches the server catalog: the curated set embedded in the binary merged with MCP Registry results (curated first, deduped by registry namespace). Backs the wizard's catalog picker; same data as `gridctl search`. The endpoint never fails because the registry is down; degraded results carry `registry_error` or `stale` instead.

**Auth:** Yes

**Query parameters:**

| Parameter | Description |
|---|---|
| `q` | Search query. Empty lists the curated catalog only; the registry is not contacted. |
| `source` | `curated`, `registry`, or `all` (default `all`). |

```bash
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8180/api/catalog?q=postgres"
```

**Response:** `{query, source, stale?, registry_error?, servers: [...]}` where each server is a full catalog entry (`name`, `title`, `description`, `tier`, `status`, `install`, `inputs`, ...). Secret input defaults are always empty.

#### `POST /api/stack/append`

Appends an `mcp-server` or `resource` snippet to the live `stack.yaml`. The snippet is validated before write; comments and key ordering elsewhere in the file are preserved.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"resourceType": "mcp-server", "yaml": "name: new-server\nimage: alpine\n..."}' \
  http://localhost:8180/api/stack/append
```

`resourceType` must be `mcp-server` or `resource`. Returns `422` with a `validation` object when the post-append stack would be invalid.

#### `POST /api/stack/initialize`

Cold-loads a saved stack into a stackless daemon (`gridctl serve`). Starts the file watcher when one is configured.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-stack"}' \
  http://localhost:8180/api/stack/initialize
```

Loads `~/.gridctl/stacks/<name>.yaml`. Returns `409` when a stack is already loaded; `400` with per-server `errors[]` when initialization fails.

#### `PATCH /api/stack/telemetry`

Updates the top-level `telemetry:` block in the live stack YAML (persist defaults and retention). Returns a refreshed telemetry inventory in the response.

**Auth:** Yes

```bash
curl -X PATCH -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"persist": {"logs": true, "metrics": true}, "retention": {"max_size_mb": 50}}' \
  http://localhost:8180/api/stack/telemetry
```

At least one `persist` or `retention` field must be set. Omitted sub-fields are left unchanged.

---

### Stack Library

Saved stacks live under `~/.gridctl/stacks/` as `<name>.yaml`.

#### `GET /api/stacks`

Lists saved stacks.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/stacks
```

**Response:**
```json
{ "stacks": [{ "name": "my-stack", "path": "/Users/me/.gridctl/stacks/my-stack.yaml" }] }
```

#### `POST /api/stacks`

Saves a stack YAML to the library.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "my-stack", "yaml": "version: \"1\"\nname: my-stack\n..."}' \
  http://localhost:8180/api/stacks
```

`name` must match `[a-zA-Z0-9_-]+`. Returns `400` when the YAML does not parse as a valid stack.

---

### Wizard Drafts

Persists in-progress wizard form state under `~/.gridctl/cache/wizard-drafts/`.

#### `GET /api/wizard/drafts`

Lists saved wizard drafts, newest first.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/wizard/drafts
```

**Response:** JSON array of `{id, name, resourceType, formData, createdAt, updatedAt}`.

#### `POST /api/wizard/drafts`

Creates a new wizard draft.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "GitHub MCP", "resourceType": "mcp-server", "formData": {}}' \
  http://localhost:8180/api/wizard/drafts
```

**Response:** `201 Created` with the draft object (server-generated `id`).

#### `DELETE /api/wizard/drafts/{id}`

Deletes a wizard draft.

**Auth:** Yes

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/wizard/drafts/abc123
```

**Response:** `204 No Content`

---

### MCP Server Control

#### `POST /api/mcp-servers/{name}/restart`

Restarts an individual MCP server connection. For container-based servers (stdio transport), this restarts the Docker container and re-establishes the MCP session. For external servers (HTTP/SSE), this re-initializes the MCP handshake and refreshes tools. For process-based servers (local, SSH), this kills and restarts the process.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/mcp-servers/github/restart
```

**Response:**
```json
{
  "status": "restarted",
  "server": "github"
}
```

**Errors:**
- `404` - Server name not found in gateway
- `500` - Restart failed (container error, connection timeout, etc.)

#### `GET /api/mcp-servers/{name}/logs`

Returns structured log entries from the gateway log buffer filtered to the named server.

**Auth:** Yes

| Query Param | Type | Default | Description |
|-------------|------|---------|-------------|
| `lines` | int | `100` | Number of recent log entries to return for this server |

```bash
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8180/api/mcp-servers/github/logs?lines=50"
```

The response is the same JSON array of buffered entries as [`/api/logs`](#get-apilogs), limited to entries tagged with the requested server. The buffer is scanned newest-first for up to `lines` matching entries, so servers with a small share of the buffer still get their full history within the ring. Returns an empty array (`[]`) when no log buffer is configured.

#### `PUT /api/mcp-servers/{name}/tools`

Updates an MCP server's tool whitelist in the live `stack.yaml` and triggers a hot reload. Powers the live tool whitelist editor in the Stack sidebar. The YAML write is atomic; concurrent external edits surface as `409` so the UI can re-fetch without clobbering changes.

**Auth:** Yes

**Request:**
```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"tools": ["get_file_contents", "search_code"]}' \
  http://localhost:8180/api/mcp-servers/github/tools
```

The body must be a JSON object with a `tools` field. An empty array (`[]`) clears the filter and exposes all server tools. The request body is capped at 64 KiB.

**Response:**
```json
{
  "server": "github",
  "tools": ["get_file_contents", "search_code"],
  "reloaded": true,
  "reloadedAt": "2025-01-15T10:30:00Z"
}
```

`reloaded` is `false` when the daemon is running without live-reload; the UI should hint the user to run `gridctl reload` manually. `reloadedAt` is omitted in that case.

**Errors:**
- `400 unknown_tool` - Tool name not advertised by the server (whitelist is stale)
- `400` - Body missing `tools` array, or contains an empty tool name
- `404` - Server not found in the stack file
- `409 stack_modified` - Stack file changed on disk between read and write
- `502 reload_failed` - YAML written but hot reload failed
- `503` - No stack file configured (stackless mode)

#### `PUT /api/mcp-servers/tools`

Applies tool-whitelist changes to **multiple** servers in one atomic `stack.yaml` write and triggers a **single** hot reload, the fleet-bulk counterpart to the per-server endpoint above. Powers the Tools workspace Fleet actions (fleet-wide expose-all and hide-by-pattern), where applying N servers via N single-server calls would cost N reloads.

**Transaction semantics: all-or-nothing.** Every server's tools are validated before anything is written; if any tool is unknown the whole batch is rejected (`400 unknown_tool`, naming the offending server) and the stack file is left untouched. This prevents a half-applied fleet edit. The reload runs once after the single write.

**Auth:** Yes

**Request:**
```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"servers": [
        {"name": "github", "tools": ["search_code"]},
        {"name": "atlassian", "tools": []}
      ]}' \
  http://localhost:8180/api/mcp-servers/tools
```

The body must be a JSON object with a non-empty `servers` array; each entry needs a `name` and a `tools` array (`[]` clears that server's whitelist = expose all). Server names must be unique within the batch. The body is capped at 512 KiB.

**Response:**
```json
{
  "servers": [
    { "server": "github", "tools": ["search_code"] },
    { "server": "atlassian", "tools": [] }
  ],
  "reloaded": true,
  "reloadedAt": "2025-01-15T10:30:00Z"
}
```

`reloaded`/`reloadedAt` follow the single-server rules (one reload for the whole batch; `false` without live-reload).

**Errors:**
- `400 unknown_tool` - A tool name is not advertised by its server (message names the server); nothing written
- `400` - Body missing/empty `servers`, an entry missing `name`/`tools`, a duplicate server, or an empty tool name
- `404` - A named server is not in the stack file; nothing written
- `409 stack_modified` - Stack file changed on disk between read and write; nothing written
- `502 reload_failed` - YAML written but hot reload failed
- `503` - No stack file configured (stackless mode)

#### `POST /api/servers/probe`

Probes an external URL (or any other) MCP server configuration ephemerally and returns its advertised tool list, without registering it with the gateway. Powers the wizard's "Discover tools" button on the MCP server form.

**Auth:** Yes

**Request:**
```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "X-Session-ID: wizard-1" \
  -d '{"name":"remote","url":"https://mcp.example.com/sse"}' \
  http://localhost:8180/api/servers/probe
```

The body mirrors the MCP server config (`name`, `image`, `source`, `url`, `port`, `transport`, `command`, `env`, `build_args`, `network`, `ssh`, `openapi`, `tools`, `output_format`, `ready_timeout`, `replicas`, `auth`). The `auth` block uses the stack YAML shape (`type`, `token`, `header`, `value`, `scopes`, `client_id`, `client_secret`) so Test Connection can probe protected external servers; a `type: oauth` server with no stored broker tokens returns the `needs_auth` code. The body is capped at 64 KiB.

`X-Session-ID` is optional; when absent, the remote address is used for per-session accounting. Concurrency is capped at **3 in-flight probes per session** and **10 globally** - excess requests get `429` (session) or `503` (global) with `Retry-After: 3`.

**Response:**
```json
{
  "tools": [
    {
      "name": "fetch_url",
      "description": "Fetch the contents of a URL",
      "inputSchema": { "type": "object", "properties": { "url": {"type": "string"} } }
    }
  ],
  "probedAt": "2025-01-15T10:30:00Z",
  "cached": false
}
```

**Error envelope:**
```json
{ "error": { "code": "invalid_config", "message": "...", "hint": "..." } }
```

Error codes:
- `invalid_config` (400) - Body malformed or required fields missing
- `rate_limited` (429 / 503) - Session or global probe cap exceeded
- `unsupported_transport` (503) - Probe is not configured on this daemon
- `internal` (500) - Unexpected probe failure
- Other codes (422) - Probe ran but the upstream rejected the handshake

Env-var values and auth secrets (`auth.token`, `auth.value`, `auth.client_secret`) present in the request body are scrubbed from error messages and hints to avoid leaking secrets.

#### `POST /api/openapi/operations`

Parses an OpenAPI document and returns every operation it contains, without registering anything with the gateway. Powers the wizard's "Load operations" button on the OpenAPI Configuration section. A sibling of the probe rather than part of it: the probe returns `[]mcp.Tool`, which discards the method, path, and tags an operations picker filters on.

**Auth:** Yes

**Request:**
```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "X-Session-ID: wizard-1" \
  -d '{"spec":"https://petstore3.swagger.io/api/v3/openapi.json"}' \
  http://localhost:8180/api/openapi/operations
```

The body takes `spec` (a URL or a path on the gateway host) and an optional `tls` block (`certFile`, `keyFile`, `caFile`, `insecureSkipVerify`), mirroring the `openapi` block of the YAML schema. There is deliberately **no auth block**: specs are fetched unauthenticated on the deployed path too, so accepting credentials would imply a capability the gateway does not have. The body is capped at 64 KiB.

`X-Session-ID` behaves as it does for the probe. Concurrency is capped at **3 in-flight loads per session** and **10 globally**, with `Retry-After: 3` on rejection. Successful parses are cached for 5 minutes, keyed on the spec reference and TLS material.

External `$ref` resolution is **disabled** on this path (it stays enabled for deploy), because the endpoint is reachable from the browser against an arbitrary operator-supplied URL and following references would let a hostile spec drive further daemon-side requests. kin-openapi enforces this by rejecting the whole document, so a multi-file spec fails to preview with a `$ref`-specific hint while still deploying normally.

**Response:**
```json
{
  "title": "Swagger Petstore",
  "version": "1.0.17",
  "operations": [
    {
      "operation_id": "pets.list",
      "tool_name": "pets_list",
      "method": "GET",
      "path": "/pets",
      "summary": "List all pets",
      "tags": ["pet"],
      "deprecated": false
    },
    { "method": "POST", "path": "/health", "skipped": true, "skip_reason": "no_operation_id" }
  ],
  "skipped_count": 1,
  "loaded_at": "2025-01-15T10:30:00Z",
  "cached": false
}
```

`operation_id` and `tool_name` are both returned on purpose: `openapi.operations.include` / `exclude` match the raw `operation_id`, while `tool_name` is the sanitized identifier advertised over MCP. They differ whenever an ID contains characters outside `[a-zA-Z0-9_-]`, so anything persisted into `stack.yaml` must use `operation_id`. Operations that cannot become tools are returned as `skipped` rows with a `skip_reason` (`no_operation_id` or `unusable_tool_name`) rather than being omitted, and enumeration is shared with the deployed tool builder so preview and deploy cannot disagree.

**Error envelope:**
```json
{ "error": { "code": "fetch_failed", "message": "...", "hint": "..." } }
```

Error codes:
- `invalid_request` (400) - Body malformed or `spec` empty
- `needs_auth` (422) - The spec URL answered 401/403
- `fetch_failed` (422) - Host unreachable, local path missing, or the URL served a docs page instead of the document
- `parse_failed` (422) - Document served but not valid OpenAPI 3.x, including a disallowed external `$ref` or an unexpanded `${VAR}` in the path
- `rate_limited` (429 / 503) - Session or global load cap exceeded
- `internal` (500 / 503) - Unexpected failure, or preview not configured on this daemon

#### `PATCH /api/mcp-servers/{name}/telemetry`

Updates the per-server `telemetry.persist` overrides in the live stack YAML. Each signal (`logs`, `metrics`, `traces`) can be set to `true`, `false`, or `null` (clear the override and inherit the stack default). Send `persist: null` to remove the entire per-server telemetry block.

**Auth:** Yes

```bash
curl -X PATCH -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"persist": {"logs": true, "metrics": null}}' \
  http://localhost:8180/api/mcp-servers/github/telemetry
```

**Response:** `{success: true, inventory: [...]}` — same inventory shape as `GET /api/telemetry/inventory`.

**Errors:** `404` when the server is not in the stack; `409 stack_modified`; `502 reload_failed`; `503` when no stack file is configured.

---

### Downstream Server Authorization (OAuth)

These endpoints drive OAuth 2.1 brokering for external URL servers declared with `auth: {type: oauth}` in `stack.yaml`. They power the sidebar Authorize flow in the web UI and the `gridctl auth` command group. The `/api/*` endpoints return `501` when OAuth brokering is disabled on the daemon (the encrypted token store failed to initialize); with brokering enabled but no OAuth-configured servers, `GET /api/auth/servers` returns an empty list. `/oauth/callback` is mounted only when brokering is enabled.

#### `GET /api/auth/servers`

Returns per-server downstream authorization state for every OAuth-configured server.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/auth/servers
```

**Response:**
```json
[
  {
    "server": "notion",
    "resource": "https://mcp.notion.com/mcp",
    "status": "needs_auth",
    "issuer": "https://auth.notion.com",
    "scopes": ["read", "write"],
    "expiry": "2026-07-19T12:00:00Z"
  }
]
```

`status` is `authorized` or `needs_auth`. `issuer`, `scopes`, and `expiry` are present only when a grant is stored.

#### `POST /api/servers/{name}/auth/login`

Starts the authorization-code flow for a server: discovers the authorization server, registers or reuses a client, and returns the URL the browser must open plus the single-use `state` token that keys the flow.

**Auth:** Yes

**Request:** optional JSON body `{"timeoutSeconds": 300}`; an empty body keeps the broker default. The timeout is capped at 15 minutes.

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{}' http://localhost:8180/api/servers/notion/auth/login
```

**Response:**
```json
{
  "authorize_url": "https://auth.notion.com/authorize?client_id=...&state=...",
  "state": "b64-opaque-state"
}
```

**Errors:** `502` when discovery or client registration fails.

#### `GET /api/servers/{name}/auth/wait?state=...`

Long-polls until the flow keyed by `state` completes, fails, or times out. Resolving with `200` means authorized; the UI uses this to flip from "Waiting for provider" to done.

**Auth:** Yes

**Response:** `{"status": "authorized"}`

**Errors:** `400` when `state` is missing; `502` when the flow failed or timed out.

#### `POST /api/servers/{name}/auth/manual`

Completes a flow from a pasted redirect URL - the `--manual` path for sessions where the browser cannot reach the daemon's callback (e.g. over SSH).

**Auth:** Yes

**Request:**
```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"redirectUrl": "http://localhost:8180/oauth/callback?code=...&state=..."}' \
  http://localhost:8180/api/servers/notion/auth/manual
```

**Response:** `{"status": "authorized"}`

**Errors:** `400` when `redirectUrl` is missing; `502` when the code exchange fails.

#### `POST /api/servers/{name}/auth/logout`

Revokes (best effort) and deletes the stored grant for a server.

**Auth:** Yes

**Response:** `{"status": "logged_out"}`

#### `POST /api/servers/{name}/auth/reset`

Deletes the stored grant **and** the cached dynamic client registration for a server. Use this when logins keep failing after the provider rotated or deleted the OAuth app - the next login re-registers from scratch.

**Auth:** Yes

**Response:** `{"status": "reset"}`

#### `GET /oauth/callback`

The authorization-code redirect target. Mounted **outside** the inbound auth middleware - the provider's browser redirect cannot carry a gateway bearer token - and authenticated by the flow's single-use `state` parameter instead. Serves a small HTML page that closes the popup. Not called directly by API clients.

---

### Telemetry Persistence

Inspect and manage on-disk telemetry files under `~/.gridctl/telemetry/`. Complements the `gridctl telemetry` CLI.

#### `GET /api/telemetry/inventory`

Returns one record per `(server, signal)` pair that has at least one file on disk.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/telemetry/inventory
```

**Response:**
```json
[
  {
    "server": "github",
    "signal": "logs",
    "path": "/Users/me/.gridctl/telemetry/my-stack/github/logs.jsonl",
    "sizeBytes": 4096,
    "oldestTime": "2026-05-20T10:00:00Z",
    "newestTime": "2026-05-24T09:00:00Z",
    "fileCount": 1
  }
]
```

Returns `[]` when no stack is loaded or nothing has been persisted.

#### `DELETE /api/telemetry`

Wipes persisted telemetry files for the active stack.

**Auth:** Yes

| Query Param | Type | Description |
|-------------|------|-------------|
| `server` | string | Limit to one MCP server |
| `signal` | string | Limit to `logs`, `metrics`, or `traces` |

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" "http://localhost:8180/api/telemetry?server=github&signal=logs"
```

Both query params are optional; omitting both wipes every server and signal. **Response:** `{success: true, inventory: [...]}` with the post-wipe inventory.

---

### Variables (Secrets & Config)

The variable store holds secrets (encrypted at rest) and plaintext config, organized into variable sets for scoped injection. The canonical route prefix is `/api/var`; `/api/vault/*` remains as a deprecated alias (responses carry `Deprecation` and `Sunset` headers) and is removed at v1.0.

#### `GET /api/var/status`

Returns vault lock state and counts. Does not require the vault to be unlocked.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/status
```

**Response:**
```json
{
  "locked": false,
  "encrypted": true,
  "variables_count": 12,
  "secrets_count": 12,
  "sets_count": 2
}
```

The counts are only included when the store is unlocked. `variables_count` is
the canonical total; `secrets_count` is a compatibility alias with the same
value.

#### `POST /api/var/unlock`

Unlocks an encrypted vault with a passphrase.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/unlock \
  -H "Content-Type: application/json" \
  -d '{"passphrase": "my-secret-passphrase"}'
```

**Response:**
```json
{"status": "unlocked"}
```

#### `POST /api/var/lock`

Encrypts the vault with a passphrase.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/lock \
  -H "Content-Type: application/json" \
  -d '{"passphrase": "my-secret-passphrase"}'
```

**Response:**
```json
{"status": "locked"}
```

#### `GET /api/var`

Lists all variables with type, visibility, set assignment, value-free metadata,
and last-rotation time. Values are not included.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var
```

**Response:**
```json
[
  {
    "key": "DB_PASSWORD",
    "type": "string",
    "is_secret": true,
    "set": "production",
    "description": "Database password",
    "docs": "https://example.com/database-credentials",
    "last_rotated": "2026-08-28T18:30:00Z"
  },
  {"key": "LOG_LEVEL", "type": "string", "is_secret": false}
]
```

`description`, `docs`, `example`, `deprecated`, `set`, and `last_rotated` are
omitted when empty. An absent `last_rotated` means unknown, including variables
whose value has not changed since rotation tracking was added.

#### `POST /api/var`

Creates a variable. Variables default to `type: "string"` and
`is_secret: true` when those fields are omitted.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var \
  -H "Content-Type: application/json" \
  -d '{"key": "DB_PASSWORD", "value": "secret123", "type": "string", "is_secret": true, "set": "production", "description": "Database password"}'
```

**Response:** `201 Created`
```json
{"key": "DB_PASSWORD", "type": "string", "is_secret": true, "status": "created"}
```

Key names must match `[a-zA-Z_][a-zA-Z0-9_]*`.
Names beginning with `GRIDCTL_`, plus `OP_CONNECT_TOKEN` and
`OP_SERVICE_ACCOUNT_TOKEN`, are reserved internal credentials. Creating or
updating one returns `400 Bad Request` with the key name but never its value.

#### `GET /api/var/{key}`

Returns the full variable record, including its value and metadata.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/DB_PASSWORD
```

**Response:**
```json
{
  "key": "DB_PASSWORD",
  "value": "secret123",
  "type": "string",
  "is_secret": true,
  "set": "production",
  "description": "Database password",
  "last_rotated": "2026-08-28T18:30:00Z"
}
```

#### `PUT /api/var/{key}`

Partially updates a variable. Omitted fields retain their stored values. The
accepted fields are `value`, `type`, `is_secret`, `set`, `description`, `docs`,
`example`, and `deprecated`; an empty string clears an optional text field.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/DB_PASSWORD \
  -H "Content-Type: application/json" \
  -d '{"value": "new-secret"}'
```

**Response:**
```json
{"key": "DB_PASSWORD", "type": "string", "is_secret": true, "status": "updated"}
```

#### `DELETE /api/var/{key}`

Deletes a variable.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/DB_PASSWORD
```

**Response:** `204 No Content`

#### `GET /api/var/sets`

Lists all variable sets with member counts.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/sets
```

#### `POST /api/var/sets`

Creates a new variable set.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/sets \
  -H "Content-Type: application/json" \
  -d '{"name": "production"}'
```

**Response:** `201 Created`
```json
{"name": "production", "status": "created"}
```

Set names must match `[a-z0-9][a-z0-9-]*`.

#### `DELETE /api/var/sets/{name}`

Deletes a variable set.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/sets/staging
```

**Response:** `204 No Content`

#### `PUT /api/var/{key}/set`

Assigns or unassigns a variable to a variable set.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/DB_PASSWORD/set \
  -H "Content-Type: application/json" \
  -d '{"set": "production"}'
```

**Response:**
```json
{"key": "DB_PASSWORD", "set": "production", "status": "updated"}
```

#### `GET /api/var/usage`

Returns which stack nodes reference each `${var:KEY}` in the active stack. Derived from the loaded stack file only — no secret values, safe while the vault is locked.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/usage
```

**Response:**
```json
{
  "DB_PASSWORD": [
    { "kind": "resource", "name": "postgres", "field": "env.POSTGRES_PASSWORD" }
  ]
}
```

Returns `{}` when no stack is deployed.

Variables injected in bulk through `secrets.sets` also appear, as a synthetic
consumer of kind `secrets-set` whose `name` is the set. When that set is scoped
to named workloads, one such consumer is returned per receiving workload, with
`target` naming it and `targetKind` saying whether it is an `mcp-server` or a
`resource`. An unscoped set fans out to everything and returns a single
consumer with neither field set. `name` always holds the set name.

```json
{
  "GITHUB_TOKEN": [
    {
      "kind": "secrets-set",
      "name": "github-creds",
      "field": "secrets.sets",
      "target": "github",
      "targetKind": "mcp-server"
    }
  ]
}
```

#### `GET /api/var/drift`

Reports unresolved `${var:KEY}` references and value-free declaration
diagnostics for the active stack. An unresolved reference without a default
would fail deployment; a declaration-only diagnostic is advisory. Responses
contain keys, reference sites, declarations, and diagnostics, never values.

A reference carrying a default (`${var:KEY:-fallback}`) is valid config and is
never reported. Entries can include the stack's value-free `declaration` and
declaration `diagnostics`. When the store is locked or absent, declared keys are
returned with `"unknown": true` because membership cannot be checked; an
undeclared reference is not reported as missing without that evidence.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/drift
```

**Response:**
```json
[
  {
    "key": "GITLAB_API_TOKEN",
    "consumers": [
      { "kind": "mcp-server", "name": "gitlab", "field": "auth.token" }
    ],
    "declaration": {
      "required": true,
      "secret": true,
      "type": "string",
      "description": "GitLab API token"
    },
    "diagnostics": [
      { "code": "required_unset", "key": "GITLAB_API_TOKEN", "message": "required variable is unset", "consumers": 1 }
    ]
  }
]
```

Returns `[]` when no stack is deployed. Canonical path only: this endpoint is
not mirrored onto the deprecated `/api/vault` surface.

#### `POST /api/var/import`

Bulk imports variables. The metadata-preserving shape is `{"variables": [...]}`;
the legacy `{"secrets": {"KEY": "value"}}` map remains accepted and imports
each entry as a secret string.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/import \
  -H "Content-Type: application/json" \
  -d '{"variables": [{"key": "API_KEY", "value": "key123", "type": "string", "is_secret": true, "description": "Service API key"}, {"key": "DB_HOST", "value": "localhost", "type": "string", "is_secret": false}]}'
```

**Response:**
```json
{"imported": 2, "skipped": []}
```

Reserved internal credential keys are skipped while valid entries are imported.
The response names every skipped key in deterministic order and never includes
its value:

```json
{
  "imported": 1,
  "skipped": ["GRIDCTL_VAULT_PASSPHRASE", "OP_CONNECT_TOKEN"]
}
```

When the vault is locked, all endpoints except `status`, `unlock`, and `lock` return `423 Locked`:
```json
{
  "error": "vault is locked",
  "hint": "POST /api/var/unlock with passphrase"
}
```

---

### Schema Pins

Inspect and manage TOFU schema pins for MCP servers. Pins protect against rug pull attacks by detecting when an MCP server silently modifies its tool definitions. The pin store is automatically updated on deploy; these endpoints are for inspection and remediation.

#### `GET /api/pins`

Returns pin records for all servers in the deployed stack.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/pins
```

**Response:**
```json
{
  "github": {
    "server_hash": "abc123...",
    "pinned_at": "2026-03-24T09:14:22Z",
    "last_verified_at": "2026-03-24T09:14:22Z",
    "tool_count": 23,
    "status": "pinned",
    "tools": {
      "github__create_pull_request": {
        "hash": "def456...",
        "name": "github__create_pull_request",
        "pinned_at": "2026-03-24T09:14:22Z"
      }
    }
  }
}
```

**Status values:** `"pinned"` | `"drift"` | `"approved_pending_redeploy"`

Returns `503` if the pin store is not available (schema pinning disabled globally).

#### `GET /api/pins/{server}`

Returns the pin record for a specific server.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/pins/github
```

Returns `404` if no pins exist for that server.

#### `GET /api/pins/{server}/diff`

Recomputes the per-tool delta between a server's pinned definitions and its live tools. Read-only: viewing a diff never mutates pin state. `live_server_hash` fingerprints the live definitions; pass it back as `expected_server_hash` on approve to bind the approval to this reviewed snapshot.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/pins/github/diff
```

**Response:**
```json
{
  "server": "github",
  "status": "drift",
  "live_server_hash": "9f2c41...",
  "modified_tools": [
    {
      "name": "create_pull_request",
      "old_hash": "h2:def456...",
      "new_hash": "h2:0a1b2c...",
      "old_description": "Create a pull request.",
      "new_description": "Create a pull request.",
      "findings": [],
      "old_input_schema": "{\"required\":[\"title\"]}",
      "new_input_schema": "{\"required\":[\"title\",\"token\"]}",
      "change_kinds": ["input_schema"],
      "groups_rewriting": ["deploy-tools"]
    }
  ],
  "new_tools": [],
  "removed_tools": []
}
```

Fields on each modified tool:

- `change_kinds` names what changed: `description`, `input_schema`, `output_schema`, or `schema_uncaptured` (the pin predates schema capture, so the old schema is unrecoverable; review the new schema). `schema_uncaptured` appears alongside `description` when the prose also moved.
- `old_input_schema` / `new_input_schema` / `old_output_schema` / `new_output_schema` carry the canonical (key-sorted) schema serializations. `old_*` are omitted for pins recorded before schema capture. All schema fields are omitted when empty.
- `findings` are advisory poisoning-scan results for the new definition, including the cross-server shadowing check (`P006`).
- `groups_rewriting` names tool groups whose overrides rewrite this tool's description; those rewrites were written against the old upstream definition.

**Errors:**
- `404` - No pins found for that server, or server not found in gateway
- `500` - Diff computation or live-tool fingerprinting failed
- `503` - Pin store not available

#### `POST /api/pins/{server}/approve`

Re-pins the current live tool definitions for a server, clearing drift status. Fetches tools directly from the running gateway router.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/pins/github/approve
```

**Response:**
```json
{
  "server": "github",
  "tool_count": 23,
  "status": "approved"
}
```

**Errors:**
- `404` - No pins found for that server, or server not found in gateway
- `503` - Pin store not available

#### `DELETE /api/pins/{server}`

Deletes the pin record for a server. The server will be re-pinned on the next deploy.

**Auth:** Yes

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/pins/github
```

**Response:** `204 No Content`

**Errors:**
- `404` - No pins found for that server

---

### Skill Pins

TOFU content pins for registry skill documents: per-file digests over the canonical `SKILL.md` plus supporting files, drift review, and approval bound to a composite hash. Advisory poisoning findings ride on each record. See [skills.md](./skills.md#skill-pins-and-exposure-policy).

#### `GET /api/skill-pins`

Every skill pin, keyed by skill name. Returns `200` with an empty object when skill pinning is not wired (the UI polls this endpoint).

```json
{
  "incident-triage": {
    "skill_hash": "s1:9f2c…",
    "files": [{ "path": "scripts/run.sh", "digest": "s1:11ab…" }],
    "source": "git",
    "origin": { "repo": "https://github.com/acme/skills.git", "ref": "main", "commitSha": "abc123" },
    "pinned_at": "2026-08-01T10:00:00Z",
    "last_verified_at": "2026-08-01T10:05:00Z",
    "status": "pinned",
    "findings": []
  }
}
```

#### `GET /api/skill-pins/{name}`

One skill's pin record. `404` when unpinned, `503` when skill pinning is unavailable.

#### `GET /api/skill-pins/{name}/diff`

What changed since the pin, plus the `composite_hash` an approval must echo. Viewing a diff never persists anything.

```json
{
  "skill": "incident-triage",
  "status": "drift",
  "composite_hash": "4be1…",
  "old_document": "---\nname: incident-triage…",
  "new_document": "---\nname: incident-triage…",
  "added_files": [],
  "removed_files": [],
  "modified_files": ["scripts/run.sh"],
  "findings": []
}
```

#### `POST /api/skill-pins/{name}/approve`

Re-pins the current content. Optional body: `{ "expected_hash": "<composite_hash from the diff>", "reason": "<justification>" }`. `409` when the content changed since the reviewed diff; `400` when the content carries unresolved advisory findings and no reason is given; `404` when the skill is not in the registry.

#### `DELETE /api/skill-pins/{name}`

Deletes the pin record (`204`); the next registry refresh re-pins fresh. `404` when no pin exists.

Registry skill responses (`GET /api/registry/skills`, `GET /api/registry/skills/{name}`) additionally carry a `governance` object when known: `source` (`local` | `git`), `origin`, `pinStatus` (`pinned` | `drift`), `findingsCount`, `maxFindingSeverity`, and — when a `skills:` policy denies the skill — `policyDenied` with the matching `policyRule`.

Registry skill and agent responses also carry a `modelPreference` object when the item declares a model preference or a `model_preferences:` policy resolves one (absent otherwise, so older frontends see nothing new):

```json
"modelPreference": {
  "declared": { "value": "opus", "sourceKey": "model" },
  "resolved": { "value": "sonnet", "resolution": "override" },
  "honor": { "claude-code": "honored", "opencode": "dropped-on-render" }
}
```

`declared` is the author's frontmatter declaration (`sourceKey` is `model`, `metadata.preferred-model`, or `metadata.model`). `resolved` appears only when a loaded stack policy default or override decides the value; `resolution` names the winning source (`default` | `override`), and an author declaration a policy leaves untouched stays `declared`-only. `honor` maps projection target slugs to what each target does with the preference (`honored` | `ignored` | `unknown` | `dropped-on-render`).

### Global Context

Manage the canonical global agent-context file (`~/.gridctl/context/AGENTS.md`) and its projection into each linked client's global context location. Backs `gridctl ctx` and the web UI's Global Context dialog; see [Global Context Sync](global-context.md) for concepts (write strategies, drift, adoption). These endpoints are pure file operations against the gateway host's home directory and work in stackless mode.

#### `GET /api/context`

Returns the canonical content and per-client sync state.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/context
```

**Response:**
```json
{
  "canonical": {
    "path": "/home/user/.gridctl/context/AGENTS.md",
    "exists": true,
    "content": "# Global Agent Context\n..."
  },
  "needs_sync": false,
  "clients": [
    {
      "slug": "claude-code",
      "name": "Claude Code",
      "supported": true,
      "available": true,
      "strategy": "dedicated-file",
      "target_path": "/home/user/.claude/rules/gridctl.md",
      "state": "in-sync",
      "synced_at": "2026-07-15T13:22:12Z"
    },
    {
      "slug": "cursor",
      "name": "Cursor",
      "supported": false,
      "available": false,
      "state": "unsupported",
      "detail": "global User Rules are stored in app-internal storage; no supported file path"
    }
  ]
}
```

| Field | Type | Description |
|---|---|---|
| `canonical` | object | Canonical file path, existence, and content (`content` is empty when `exists` is false) |
| `needs_sync` | bool | True when any client is `stale`, `drifted`, or `target-missing` |
| `clients` | []object | One entry per known client |

Per-client fields: `slug`, `name`, `supported`, `available` (client detected on this machine), `unofficial` (the target path rests on unofficial sourcing rather than published client docs; omitted when false), `strategy` (`dedicated-file`, `import-shim`, or `block`; omitted for unsupported clients), `target_path`, `state`, `detail` (human-readable reason or hint), and `synced_at` (omitted when never synced).

In fragments mode, multi-file clients also carry a `fragments` array with one `{ "name", "state" }` entry per out-of-sync fragment (`stale`, `drifted`, or `target-missing`), plus `pack` naming the pack that applied the projection when one did. In-sync fragments are not listed, and the array is omitted when every fragment is in sync.

**State values:** `"in-sync"` | `"stale"` | `"drifted"` | `"target-missing"` | `"never-synced"` | `"unsupported"`

#### `PUT /api/context`

Saves the canonical content (creating the file when absent) and returns the same document as `GET /api/context`. A timestamped backup of the previous revision precedes the write.

**Auth:** Yes

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"content": "# Global Agent Context\n\n- Prefer rg over grep.\n"}' \
  http://localhost:8180/api/context
```

**Errors:**
- `400` - Empty content, or content containing a reserved gridctl marker (`<!-- BEGIN GRIDCTL MANAGED -->`, `<!-- END GRIDCTL MANAGED -->`, or the managed-header prefix)

#### `GET /api/context/scan`

Reports what already exists at each supported client's global context location, for the adoption-first setup flow. Never writes.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/context/scan
```

**Response:**
```json
{
  "entries": [
    {
      "slug": "claude-code",
      "name": "Claude Code",
      "path": "/home/user/.claude/CLAUDE.md",
      "exists": true,
      "size": 1189
    }
  ]
}
```

#### `POST /api/context/init`

Bootstraps the canonical file from a chosen source and returns the refreshed document. With `force`, replaces an existing canonical file (a timestamped backup is taken first) - this is what the web UI's Import action calls.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"source": "client", "client": "claude-code"}' \
  http://localhost:8180/api/context/init
```

| Field | Type | Description |
|---|---|---|
| `source` | string | `"template"` (starter draft), `"client"` (adopt a client's existing file), or `"file"` (adopt an arbitrary path) |
| `client` | string | Client slug; required when `source` is `"client"` |
| `path` | string | File path; required when `source` is `"file"` |
| `force` | bool | Overwrite an existing canonical file |

**Errors:**
- `400` - Invalid source, missing `client`/`path`, unknown client slug, or unsupported client
- `409` - Canonical file already exists and `force` is false

#### `POST /api/context/sync`

Projects the canonical context to clients. An empty (or absent) body syncs every available client.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"clients": ["gemini"], "dry_run": true}' \
  http://localhost:8180/api/context/sync
```

| Field | Type | Description |
|---|---|---|
| `clients` | []string | Client slugs to sync; omit for all available clients |
| `force` | bool | Overwrite drifted targets and repair corrupt managed blocks |
| `dry_run` | bool | Report what would change (with diffs) without writing |

**Response:**
```json
{
  "dry_run": false,
  "has_failures": false,
  "results": [
    {
      "slug": "gemini",
      "name": "Gemini CLI",
      "strategy": "import-shim",
      "target_path": "/home/user/.gemini/GEMINI.md",
      "action": "updated",
      "backup_path": "/home/user/.gemini/GEMINI.md.gridctl-backup-20260715-132212"
    }
  ]
}
```

**Action values:** `"created"` | `"updated"` | `"unchanged"` | `"skipped-drift"` | `"skipped-unavailable"` | `"error"`, plus `"would-create"` | `"would-update"` under `dry_run`

Unknown slugs, unsupported clients, and a missing canonical file abort the request (`400`/`404`); a per-client runtime failure becomes an `"error"` result row so earlier writes are still reported. Drifted targets are skipped (never silently overwritten) unless `force` is set.

#### `POST /api/context/adopt/{slug}`

Pulls a client's hand-edited managed content back into the canonical file, then re-syncs that client. Returns the refreshed document (other clients become `stale`). Only meaningful for dedicated-file and managed-block clients; import-shim clients reference the canonical file directly, so there is no copied content to adopt.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/context/adopt/opencode
```

In fragments mode the request may carry an optional JSON body that scopes the adopt. An empty or absent body keeps the whole-client behavior above.

| Field | Type | Description |
|---|---|---|
| `fragment` | string | Adopt one projected fragment file back into its source fragment. Only valid on multi-file clients whose render is the identity render (currently `claude-code`). |
| `into` | string | Capture a compiled (single-file) target's edited body into the named fragment, creating it when absent. Mirrors `gridctl ctx adopt <client> --into <name>`. |

Passing both `fragment` and `into` is a `400`.

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"fragment":"go-style"}' \
  http://localhost:8180/api/context/adopt/claude-code
```

Both scoped forms re-sync the client after the capture, so a successful adopt returns the document with that client back `in-sync` (other clients become `stale`).

**Errors:**
- `400` - Unsupported client, both `fragment` and `into` supplied, or an invalid `fragment` or `into` name
- `404` - Unknown client slug, no canonical file exists, or an unknown fragment
- `409` - Client was never synced or is not available; whole-client adopt on a multi-file target (adopt per fragment instead); per-fragment adopt on a lossy render; compiled target without `into`; import-shim target (nothing is copied); `fragment` or `into` while fragments mode is off. The body carries the engine's reason.

#### `POST /api/context/unsync/{slug}`

Removes what gridctl manages for one client and nothing else: dedicated files are deleted, shim lines and managed blocks are stripped. User-owned content is preserved.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/context/unsync/gemini
```

**Response:**
```json
{
  "slug": "gemini",
  "target_path": "/home/user/.gemini/GEMINI.md",
  "action": "removed-region"
}
```

**Action values:** `"removed-file"` (dedicated file or a file gridctl created deleted) | `"removed-region"` (shim line or managed block stripped) | `"already-gone"`

**Errors:**
- `404` - Unknown client slug
- `409` - Client was never synced

#### `GET /api/context/diff/{slug}`

Returns the unified diff between the canonical context and a client's managed content (empty when identical). Optional `?fragment=` scopes a multi-file client.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/context/diff/opencode
```

**Response:**
```json
{
  "slug": "opencode",
  "fragment": "",
  "diff": "--- canonical\n+++ opencode\n@@ -1,3 +1,3 @@\n..."
}
```

**Errors:**
- `400` - Unsupported client
- `404` - Unknown client slug, or no canonical file exists

#### `GET /api/context/fragments`

Lists rule fragments when fragments mode is active (`active: false` and an empty list otherwise). Each row includes name, description, paths, raw content, size, and lexicographic position.

#### `PUT /api/context/fragments/{name}`

Creates or overwrites a fragment. Body `{ "content": "..." }` installs that body (activating fragments mode if needed). Omitting content scaffolds a starter fragment via the same path as `gridctl ctx add`.

#### `DELETE /api/context/fragments/{name}`

Removes a fragment after writing a backup. Returns `{ "name", "backup" }`.

---

### Skill Sources

Manage git-imported skill dependencies (`skills.yaml` + lock file). Mirrors `gridctl skill *` operations for the Library workspace.

Auth for private repos accepts an optional `auth` object on mutating endpoints:

```json
{
  "method": "token",
  "token": "ghp_...",
  "credentialRef": "${var:GIT_TOKEN}",
  "sshKeyPath": "/path/to/key"
}
```

`credentialRef` is resolved against the live variable store; raw `token` values are transient and never persisted.

#### `GET /api/skills/sources`

Lists imported sources with skill entries, auto-update settings, drift markers, and cached update availability.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/skills/sources
```

#### `POST /api/skills/sources`

Imports skills (and agent definitions) from a git repository. `selected` restricts the import to named skills; `selectedAgents` restricts it to named agents. A skill selection alone deliberately skips agents (the importer's legacy contract), so a caller importing both kinds names both.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"repo": "https://github.com/org/skills", "ref": "main", "trust": false, "selected": ["code-review"], "selectedAgents": ["reviewer"]}' \
  http://localhost:8180/api/skills/sources
```

**Response:** `201 Created` with the import result (`imported`, `skipped`, `warnings`, plus `importedAgents` and `skippedAgents` when the repo ships agents). Git errors return `401`/`404`/`400` with redacted messages.

#### `POST /api/skills/sources/update`

Syncs every imported source in parallel (respects pinned refs). Optional body: `{force: true, skills: ["name"], auth: {...}}`.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/skills/sources/update
```

**Response:** `{sources[], syncedSources, updatedSkills, skippedSkills, failedSources, pinnedSources}`.

#### `GET /api/skills/updates`

Live-fetches upstream SHAs and returns pending update counts per source.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/skills/updates
```

#### `DELETE /api/skills/sources/{name}`

Removes a source and all skills it imported.

**Auth:** Yes

#### `POST /api/skills/sources/{name}/check`

Checks whether a source has upstream changes without applying them.

**Auth:** Yes

**Response:** `{source, currentSha, latestSha, hasUpdate}`.

#### `POST /api/skills/sources/{name}/update`

Applies available updates for one source. Locally edited (drifted) skills are skipped unless `force: true`.

**Auth:** Yes

#### `GET /api/skills/sources/{name}/preview`

#### `POST /api/skills/sources/{name}/preview`

Previews skills in a repo without importing. GET accepts `repo`, `ref`, and `path` query params; POST accepts the same fields plus optional `auth` in the body. When `repo` is omitted, the stored source URL is used.

**Auth:** Yes

**Response:** `{repo, ref, commitSha, skills: [{name, description, body, valid, errors, warnings, findings, exists}]}`.

#### `GET /api/skills/sources/{name}/skills/{skill}/diff`

Returns local vs upstream `SKILL.md` with a unified diff. Read-only.

**Auth:** Yes

**Response:** `{skill, local, upstream, unifiedDiff, drifted}`.

#### `POST /api/skills/sources/{name}/skills/{skill}/detach`

Detaches a skill from its source so sync no longer touches it.

**Auth:** Yes

**Response:** `{detached: "<skill>"}`.

#### `POST /api/skills/sources/{name}/skills/{skill}/reset`

Force-updates a single skill to upstream content, backing up the current file first.

**Auth:** Yes

---

### Registry (Agent Skills)

Manage reusable skills stored as SKILL.md files. Skills have three lifecycle states: `draft`, `active`, and `disabled`. The registry also holds imported agent definitions; see [Registry (Agents)](#registry-agents) below.

#### `GET /api/registry/status`

Returns registry summary counts.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/registry/status
```

**Response:**
```json
{
  "total": 5,
  "active": 3,
  "draft": 1,
  "disabled": 1
}
```

#### `GET /api/registry/skills`

Lists all skills.

**Auth:** Yes

**Query parameters:**

| Parameter | Description |
|---|---|
| `full` | `1` returns the unprojected skills, Markdown bodies included. Omit for the list shape below. |

The default response omits each skill's `body`. Bodies dominate the payload (on a
registry of 89 skills they were about 860 KB of a 970 KB response) and nothing in
a catalog view reads them. Fetch `GET /api/registry/skills/{name}` for a skill's
full instructions, or pass `?full=1` for the original shape.

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/registry/skills
```

**Response:**
```json
[
  {
    "name": "incident-triage",
    "description": "Triage incidents quickly",
    "license": "Apache-2.0",
    "compatibility": "Requires git",
    "metadata": {"author": "ops"},
    "allowedTools": "Bash(git:*) Read Write",
    "acceptanceCriteria": ["GIVEN an alert WHEN it is triaged THEN severity is set"],
    "state": "active",
    "fileCount": 2,
    "dir": "ops/incident-triage"
  }
]
```

| Field | Description |
|---|---|
| `name` | Skill name, unique within the registry |
| `description` | One-line summary from the frontmatter |
| `license` | Frontmatter `license`, omitted when unset |
| `compatibility` | Frontmatter `compatibility`, omitted when unset |
| `metadata` | Frontmatter `metadata` map, omitted when empty |
| `allowedTools` | Frontmatter `allowed-tools`, omitted when unset |
| `acceptanceCriteria` | Given/When/Then scenarios, omitted when empty |
| `state` | `draft`, `active`, or `disabled` |
| `fileCount` | Number of supporting files (`scripts/`, `references/`, `assets/`) |
| `dir` | Path relative to the skills root, omitted for a root-level skill |

#### `POST /api/registry/skills`

Creates a new skill.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/registry/skills \
  -H "Content-Type: application/json" \
  -d '{"name": "code-review", "description": "Review code changes", "state": "active", "content": "..."}'
```

**Response:** `201 Created` with skill JSON.

Returns `409 Conflict` if a skill with the same name already exists.

#### `POST /api/registry/skills/validate`

Validates SKILL.md content without saving.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/registry/skills/validate \
  -H "Content-Type: application/json" \
  -d '{"content": "---\nname: test\n---\n# Test Skill"}'
```

**Response:**
```json
{
  "valid": true,
  "errors": [],
  "warnings": [],
  "parsed": { ... }
}
```

#### `PUT /api/registry/skills/batch`

Sets the state of multiple skills in one request, then refreshes the registry router once. Only `active` and `disabled` are accepted (bulk actions enable or disable; they never set `draft`).

**Auth:** Yes

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"skills": [
        {"name": "code-review", "state": "active"},
        {"name": "release-notes", "state": "disabled"}
      ]}' \
  http://localhost:8180/api/registry/skills/batch
```

The body must include a non-empty `skills` array; each entry needs a `name` and a `state`. Skill names must be unique within the batch.

**Validation is all-or-nothing:** every entry is checked (known skill, valid state) before any write, so an unknown skill (`404`) or invalid state (`400`) rejects the whole batch with nothing changed. The write phase itself is best-effort: a mid-batch save failure (`500`) can leave earlier entries persisted.

**Response:**
```json
{
  "skills": [
    {"name": "code-review", "state": "active"},
    {"name": "release-notes", "state": "disabled"}
  ]
}
```

**Errors:**
- `400` - Empty `skills` array, an entry missing `name`, a duplicate skill, or an invalid state
- `404` - A named skill does not exist
- `503` - Registry not available

#### `GET /api/registry/skills/{name}`

Returns a specific skill.

**Auth:** Yes

#### `PUT /api/registry/skills/{name}`

Updates a skill. URL path name takes precedence over body name.

**Auth:** Yes

#### `DELETE /api/registry/skills/{name}`

Deletes a skill.

**Auth:** Yes

**Response:** `204 No Content`

#### `POST /api/registry/skills/{name}/activate`

Activates a disabled or draft skill.

**Auth:** Yes

#### `POST /api/registry/skills/{name}/disable`

Disables an active skill (hides without deleting).

**Auth:** Yes

#### `GET /api/registry/skills/{name}/files`

Lists files in a skill directory.

**Auth:** Yes

#### `GET /api/registry/skills/{name}/files/{path...}`

Reads a file from a skill directory. Content-Type is detected from file extension. The `{path...}` segment is variadic, so nested sub-paths (e.g. `references/api/spec.json`) are supported.

**Auth:** Yes

#### `PUT /api/registry/skills/{name}/files/{path...}`

Writes a file to a skill directory. Body is raw file content. Maximum 1MB. The `{path...}` segment is variadic, so nested sub-paths are supported (parent directories are created as needed).

**Auth:** Yes

**Response:** `204 No Content`

#### `DELETE /api/registry/skills/{name}/files/{path...}`

Deletes a file from a skill directory. The `{path...}` segment is variadic, so nested sub-paths are supported.

**Auth:** Yes

**Response:** `204 No Content`

---

### Registry (Agents)

Manage imported agent definitions (`~/.gridctl/registry/agents/<name>/AGENT.md`). Agents are single-file definitions projected into client directories; gridctl never executes them, and they are not gateway-routed MCP content. Agents enter the store through import (`gridctl skill add` or `POST /api/skills/sources`), so there is no create endpoint; PUT edits an existing agent.

Frontmatter keys other than `name` and `description` ride in `extra` as an ordered `{key, value}` array, never an object: the canonical file is projected verbatim to identity targets, so key order is part of the contract. `extra` is read-only display data; edits submit the whole file through `raw`.

#### `GET /api/registry/agents`

Lists installed agents. The default response omits each agent's `body` and `raw`; pass `?full=1` for the complete shapes.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/registry/agents
```

**Response:**
```json
[
  {
    "name": "code-reviewer",
    "description": "Reviews code for style and correctness",
    "source": "team-agents",
    "extra": [
      {"key": "tools", "value": "Read, Grep, Glob"},
      {"key": "model", "value": "sonnet"}
    ],
    "dir": "/home/user/.gridctl/registry/agents/code-reviewer"
  }
]
```

#### `GET /api/registry/agents/{name}`

Returns a single agent, `body` (markdown after the frontmatter) and `raw` (the verbatim file) included.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/registry/agents/code-reviewer
```

**Response:** the list shape plus `body` and `raw`.

#### `PUT /api/registry/agents/{name}`

Updates an agent's file. The body carries the whole file; the server re-parses it, refuses renames (frontmatter `name` must match the path), runs the blocking security scan, and writes the bytes verbatim. A scan finding returns `409` with the findings; there is no trust override over REST. Unknown agents are `404` (editing only, no upsert).

**Auth:** Yes

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/registry/agents/code-reviewer \
  -H "Content-Type: application/json" \
  -d '{"raw": "---\nname: code-reviewer\ndescription: Reviews code\n---\nReview the changed files.\n"}'
```

**Response:** the updated agent in the full GET shape.

#### `DELETE /api/registry/agents/{name}`

Removes an agent from the canonical store, including its origin sidecar and lock-file entry. The agent's projections are retired first (best-effort): projected copies leave client directories and the project lock, so no orphaned rows linger in status output.

**Auth:** Yes

**Response:** `204 No Content`

---

### Agent Projection

Per-client projection of imported agents: the REST face of `gridctl skill project --kind agent`, backed by the same engine and lockfile (`~/.gridctl/project.lock.yaml`). States use the shared projection vocabulary: `in-sync`, `stale`, `drifted`, `target-missing`. Each row carries `render` (`identity`: canonical bytes copied verbatim, currently Claude Code; `lossy`: client-dialect render that drops unmappable keys, currently OpenCode, Copilot, and Gemini CLI).

#### `GET /api/project/agents/status`

Returns every (agent, client) projection row.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/project/agents/status
```

**Response:**
```json
[
  {
    "agent": "code-reviewer",
    "client": "claude-code",
    "channel": "copy",
    "target": "/home/user/.claude/agents/code-reviewer.md",
    "render": "identity",
    "state": "in-sync",
    "pack": "team-pack",
    "synced_at": "2026-08-04T12:00:00Z"
  }
]
```

`pack` names the pack that applied the projection and is omitted for projections made outside a pack.

#### `POST /api/project/agents/sync`

Projects agents into detected client directories. All body fields are optional; an empty body syncs every agent to every available client. Undetected clients report `skipped-unavailable` rows, drifted projections report `skipped-drift` unless `force` is set (which backs up before overwriting), and lossy renders name the dropped frontmatter keys in `detail`.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/project/agents/sync \
  -H "Content-Type: application/json" \
  -d '{"agents": ["code-reviewer"], "clients": ["claude-code"], "force": false, "dry_run": false}'
```

#### `POST /api/project/agents/unsync`

Removes projected agent files. Name `agents` or set `all: true`; an empty request is refused (`400`) so a stray POST cannot silently strip every projection. Accepts the same `clients` filter and `dry_run`.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/project/agents/unsync \
  -H "Content-Type: application/json" \
  -d '{"agents": ["code-reviewer"]}'
```

**Response:** per-projection removal rows (`agent`, `client`, `target`, `action`, optional `backup_path`).

#### `POST /api/project/agents/adopt`

Pulls a hand-edited projected file back into the canonical store (identity targets only). The prior `AGENT.md` is backed up as `AGENT.md.pre-<sha>`, and the pair is force-resynced to in-sync. Refusals return `409` carrying the full reason: lossy render targets cannot flow back (the dialect dropped keys at render time), and invalid or renamed projected content is refused. Unknown agents are `404`, unknown clients `400`.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/project/agents/adopt \
  -H "Content-Type: application/json" \
  -d '{"agent": "code-reviewer", "client": "claude-code"}'
```

---

### Wiring Ownership

Per-client gateway-entry ownership: the REST face of `gridctl project status|adopt --kind wiring`, backed by the wiring ownership records in the unified project lockfile. States use the wiring vocabulary: `in-sync`, `stale` (gateway port or entry shape changed), `drifted` (edited since gridctl wrote it), `target-missing`, `foreign` (an entry at gridctl's name that gridctl never recorded), and `missing` (detected client, nothing recorded, nothing present). This is the full form of the fact `GET /api/clients` collapses into its single `drifted` boolean.

#### `GET /api/project/wiring/status`

Returns every (client, entry) ownership row with `state`, `detail`, `remediation`, and the applying `pack` tag when one exists.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/project/wiring/status
```

**Response:**
```json
[
  {
    "client": "claude",
    "name": "gridctl",
    "channel": "config-entry",
    "target": "/home/user/.claude.json",
    "state": "foreign",
    "detail": "entry was not recorded by gridctl",
    "remediation": "adopt to take ownership, or link --force to overwrite"
  }
]
```

#### `POST /api/project/wiring/adopt`

Records ownership of the entry's current value without rewriting it (the take-ownership verb for `foreign` and `drifted` entries, mirroring `gridctl project adopt --kind wiring`). `name` defaults to the gateway entry name. Refusals (nothing to adopt, client not detected) return `409` carrying the engine's full reason; unknown clients return `400`.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/project/wiring/adopt \
  -H "Content-Type: application/json" \
  -d '{"client": "claude"}'
```

**Response:** the adopt result (`client`, `name`, `target`, `action: "adopted"`).

---

### Model Routing

The REST face of `gridctl models status|sync|adopt|ack-restart|validate` (Experimental, like the CLI surface), backed by the models projection kind in the unified project lockfile. Read and reconcile only: the policy document itself is edited via `gridctl models edit`, never over REST. Sync and adopt are whole-policy operations; the engine has no per-target selection.

#### `GET /api/project/models/status`

Returns the status document: policy identity, a read-only routing summary projected from the parsed policy, and per-target rows. `targets` is variable-length: the `litellm-fragment` row always exists (state `never-synced` with no policy); the `litellm-include` and `opencode` rows appear only when declared in the policy or recorded in the lockfile. `restart_pending` on the fragment row is an annotation, never a drift state: it does not affect `needs_attention`. An unparseable policy is reported in `policy_error` with a `200`, never a `500`.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/project/models/status
```

**Response:**
```json
{
  "policy_path": "/home/user/.gridctl/models/policy.yaml",
  "policy_exists": true,
  "needs_attention": false,
  "routing": {
    "entry_model": "smart-router",
    "default_tier": "MEDIUM",
    "backends": ["qwen-local", "claude-sonnet"],
    "tiers": {"SIMPLE": "qwen-local", "MEDIUM": "qwen-local", "COMPLEX": "claude-sonnet", "REASONING": "claude-sonnet"}
  },
  "targets": [
    {
      "target": "litellm-fragment",
      "client": "litellm",
      "state": "in-sync",
      "restart_pending": true,
      "path": "/home/user/litellm/gridctl-models.yaml",
      "synced_at": "2026-08-24T12:00:00Z"
    }
  ]
}
```

#### `GET /api/project/models/validate`

Validates the policy and returns findings (`severity`, `field`, `message`), errors first. No policy is `404`; a policy that does not parse is `400`.

**Auth:** Yes

**Response:** `{"policy_path": "...", "valid": true, "issues": []}`

#### `POST /api/project/models/sync`

Projects the policy into every declared target in one pass. All body fields optional: `dry_run` previews without writing, `diff` attaches unified diffs to `would-update` rows, and `force` overwrites drifted and foreign targets (with a backup). The handler validates first: an invalid policy returns `409` with the findings (`{"error": ..., "issues": [...]}`), never a `500`. Drifted targets without `force` report `skipped-drift` rows in a `200`; the engine fails closed.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/project/models/sync \
  -H "Content-Type: application/json" \
  -d '{"dry_run": true, "diff": true}'
```

**Response:** per-target rows (`target`, `client`, `path`, `action`, optional `detail`, `backup_path`, `diff`, `error`). A fragment write carries the restart guidance in `detail`: LiteLLM reads config only at startup.

#### `POST /api/project/models/adopt`

Records the current on-disk state of every recorded target as gridctl-owned, clearing drift without touching any file. Covers the fragment and the OpenCode provider only; a removed include line is not adoptable and is restored only by a forced sync. Nothing synced yet is `409`.

**Auth:** Yes

**Response:** per-target rows (`target`, `client`, `path`, `action: "adopted"` or `"already-gone"`).

#### `POST /api/project/models/ack-restart`

Records that the user restarted LiteLLM since the last fragment write: the only way the restart-pending latch clears. gridctl never probes the process. Nothing synced yet is `409`.

**Auth:** Yes

**Response:** `{"acknowledged": true}`

---

### Packs

The REST face of `gridctl pack add|apply|status|remove`, plus a read-only preview for import flows. A pack is one git repository carrying a `gridctl-pack.yaml` manifest selecting skills, agents, context rule fragments, and optional gateway wiring (see the [Packs guide](packs.md)). Per-resource rows use the shared projection-state vocabulary (`in-sync`, `stale`, `drifted`, `target-missing`, `foreign`, `missing`), plus `unresolved` for manifest selections the repository does not ship.

#### `GET /api/packs`

Lists installed packs: identity, origin, per-kind resource counts, and aggregate attention. Never re-clones; everything comes from the import lockfile and the projection engines.

**Auth:** Yes

**Response:**
```json
{
  "packs": [
    {
      "name": "team-pack",
      "version": "1.0.0",
      "description": "Team conventions in one repo",
      "author": "Acme Platform",
      "origin": {
        "source": "team-pack",
        "repo": "https://github.com/acme/team-pack",
        "ref": "main",
        "commit_sha": "abc123...",
        "fetched_at": "2026-08-05T12:00:00Z"
      },
      "counts": { "skills": 3, "agents": 1, "rules": 2, "wiring": true },
      "unresolved": [],
      "needs_attention": false
    }
  ]
}
```

A pack name claimed by more than one imported source carries `"collision": true` with `collision_repos` listing them, and counts as attention.

#### `GET /api/packs/{name}`

One pack's identity (`info`, the list item's fields) plus its per-resource state rows and `needs_attention`. Skill, agent, and wiring rows are per-client; rule rows are per-client once applied (state joined from the pack-tagged projection lock entries and the context engine's per-fragment status; coverage is per fragment-file projection, so compiled clients' whole-document state stays on `GET /api/context`), with a single store-presence row for a rule that was imported but never projected.

**Auth:** Yes

**Errors:**
- `404` - Pack not imported
- `409` - Pack name claimed by multiple sources (the body names both repos)

#### `POST /api/packs`

Imports a pack from git, mirroring `gridctl pack add`: clone, manifest resolution (empty skill and agent lists select everything discovered; rules are opt-in), the blocking security scan, and rule-fragment installation. Unlike the CLI (which partially imports and reports per-resource skips), security findings without `trust` refuse the whole import with a `409` before any write, carrying the flagged resources, so the trust decision always precedes the import. The refusal covers the same gate the importer applies: SKILL.md bodies, supporting files (danger severity), agents, and rules.

**This is also the update path.** `pack add` is the documented update verb: a POST against an already-imported origin re-resolves the selection, refreshes rules whose content changed upstream, and leaves locally edited rules alone (reported in `doc.skipped`). There is no separate update endpoint.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/packs \
  -H "Content-Type: application/json" \
  -d '{"repo": "https://github.com/acme/team-pack", "trust": false}'
```

| Field | Type | Description |
|---|---|---|
| `repo` | string | Git repository URL (required) |
| `ref` | string | Branch, tag, or commit; default: the default branch |
| `path` | string | Subdirectory within the repository |
| `trust` | bool | Accept security findings (the CLI's `--trust`) |
| `dryRun` | bool | Resolve and report without importing |
| `auth` | object | Credentials for a private repository; same shape as the skill source endpoints (see below) |

**Response:** `201` with `{ "doc": <add document>, "notes": [...] }`. The document carries the resolved selection, `unresolved`, `skipped` (with reasons), and `warnings`; `notes` carries progress prose (rule updates, fragments-mode activation).

**Errors:**
- `400` - Missing repo, invalid body, or an unresolvable `auth.credentialRef`
- `409` - Security findings without trust: `{ "error", "pack", "findings": [{kind, name, findings}] }`; nothing was imported
- `422` - No `gridctl-pack.yaml` at the repository root, or no reachable ssh-agent (see [pack auth](#pack-authentication))

#### `POST /api/packs/preview`

Resolves a pack manifest against its repository without writing anything: manifest identity, the resolved selection per kind with per-resource scan findings, unresolved names, and warnings. The wizard's read-only review step.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/packs/preview \
  -H "Content-Type: application/json" \
  -d '{"repo": "https://github.com/acme/team-pack"}'
```

| Field | Type | Description |
|---|---|---|
| `repo` | string | Git repository URL (required) |
| `ref` | string | Branch, tag, or commit; default: the default branch |
| `path` | string | Subdirectory within the repository |
| `auth` | object | Credentials for a private repository (see below) |

**Errors:** `400` when `auth.credentialRef` cannot be resolved. `422` when the repository has no manifest (the body suggests the Skill import flow for plain skill repos), or when an SSH URL has no reachable ssh-agent.

#### Pack authentication

`POST /api/packs` and `POST /api/packs/preview` accept an optional `auth` object with the same shape and field names as the [skill source endpoints](#skill-sources):

```json
{
  "method": "token",
  "token": "ghp_...",
  "credentialRef": "${var:GIT_TOKEN}",
  "sshKeyPath": "/path/to/key"
}
```

`credentialRef` is resolved against the live variable store; raw `token` values are transient and never persisted. Only the reference is recorded, on the pack's imported source and on each resource's origin sidecar.

Omit `auth` entirely on a repository already imported with a reference and that stored reference is resolved automatically, which is how an update previews a private pack with no user input. Sending an empty object (`"auth": {}`) is an explicit request to use no credentials and suppresses the stored reference.

`POST /api/packs/{name}/apply` takes no `auth`: it projects already-imported material and never clones.

**The ssh-agent case.** An SSH URL with no reachable agent returns `422` with a structured body rather than a generic failure, because it is fixable by the caller and a token cannot fix it:

```json
{
  "error": "Pack preview failed: ssh agent not available: SSH_AUTH_SOCK is unset in this gridctl process...",
  "code": "ssh_agent_unavailable",
  "httpsEquivalent": "https://github.com/acme/team-pack"
}
```

`httpsEquivalent` is the server's rewrite of the SSH URL and is present only when the input was SSH-form. Clients should branch on `code`, not on the message. The daemon inherits `SSH_AUTH_SOCK` only from the shell that started it, so a browser-driven import cannot rely on the agent in the user's terminal; see [troubleshooting](troubleshooting.md#ssh-agent-not-available).

#### `POST /api/packs/{name}/apply`

Projects one pack with full CLI flag parity. Apply is additive and never transactional: each resource succeeds or skips independently, and the response reports every outcome.

**Auth:** Yes

| Field | Type | Description |
|---|---|---|
| `clients` | []string | Restrict wiring to these client slugs (the CLI's `--clients`) |
| `force` | bool | Overwrite drifted or foreign resources after backup (`--force`) |
| `dry_run` | bool | Report what would change without writing (`--dry-run`) |

An empty or absent body is a plain apply. **Response:** the apply document: `applied`, `total`, and per-resource `rows` (kind, name, client, action, detail, remediation). Drifted resources are skipped with a remediation hint unless forced; resources tagged by a different pack are refused with the owning pack named.

**Errors:** `404` - Pack not imported; `409` - Name collision.

#### `DELETE /api/packs/{name}`

Cascade removal in dependency order: pack-tagged projections are unsynced, pack-tagged wiring records removed through the ownership manager, then registry entries and the pack record. `?dry_run=1` returns the cascade preview (`would-remove` rows plus the drift-kept list) without executing; `?force=1` removes hand-edited projections too. A partial removal trims the pack record to the kept resources rather than deleting it, so the response's `kept` list is the truth about what remains.

**Auth:** Yes

**Errors:** `404` - Pack not imported; `409` - Name collision.

---

### MCP Protocol

The gateway speaks two protocol generations concurrently on `/mcp`, classified
per request:

- **Handshake generation** (`2025-11-25`, `2025-06-18`, `2025-03-26`,
  `2024-11-05`): the version is negotiated at `initialize` (a supported
  requested version is echoed back; any other value receives a successful
  response carrying the latest supported handshake version, and the client
  decides whether to disconnect), and a `Mcp-Session-Id` session carries
  identity. Post-initialize requests may send the `MCP-Protocol-Version`
  header; an absent header is accepted (the session-negotiated version
  applies), while an unsupported value is rejected with `400 Bad Request`
  naming the supported set. Malformed `initialize` params return a JSON-RPC
  `InvalidParams` error.
- **Stateless generation** (`2026-07-28`): no handshake and no sessions.
  Every request carries `_meta` with `io.modelcontextprotocol/protocolVersion`
  and `io.modelcontextprotocol/clientCapabilities` (`clientInfo` is optional),
  plus a matching `MCP-Protocol-Version` header and an `Mcp-Method` header
  mirroring the body method (`Mcp-Name` mirrors `params.name`/`params.uri`
  where present, plain or base64-sentinel encoded). Identity derives per
  request from `_meta` `clientInfo`, the `client` query parameter, or the
  `X-Gridctl-Client-Id` header. Rejections are typed at HTTP 400: missing or
  incomplete `_meta` is `-32602`, a header/body mismatch is `-32020`
  (HeaderMismatch), and an unsupported version is `-32022`
  (UnsupportedProtocolVersion) carrying the supported set. Unknown or removed
  methods (`ping`, `logging/setLevel`, `initialize`) return HTTP 404 with
  `-32601`, and results carry `resultType` plus `ttlMs`/`cacheScope` cache
  metadata on list and read responses.

#### `POST /mcp`

JSON-RPC 2.0 endpoint for MCP protocol operations.

**Auth:** Yes

**Supported methods:**

| Method | Description |
|--------|-------------|
| `initialize` | Initialize MCP session (handshake generation only) |
| `server/discover` | Server identity, versions, and capabilities (stateless generation) |
| `tools/list` | List available tools |
| `tools/call` | Call a tool (stateless generation adds MRTR relay: `input_required` results, `requestState`, `inputResponses`) |
| `prompts/list` | List available prompts |
| `prompts/get` | Get a specific prompt |
| `resources/list` | List available resources |
| `resources/read` | Read a specific resource |
| `resources/templates/list` | List resource templates (always empty; gridctl exposes no templated resources) |
| `tasks/get`, `tasks/update`, `tasks/cancel` | Tasks-extension proxy, when exactly one stateless server declares the extension |
| `ping` | Connectivity check (handshake generation only) |
| `notifications/initialized` | Client initialization notification (handshake generation only) |

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/mcp \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc": "2.0", "id": 1, "method": "tools/list"}'
```

**Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "tools": [
      {
        "name": "github__get_file_contents",
        "description": "Get file contents from a repository",
        "inputSchema": { ... }
      }
    ]
  }
}
```

Tool names are namespaced as `{server}__{tool}` to prevent collisions.

The streamable HTTP transport also serves two other verbs on `/mcp`, for the
handshake generation only (a request declaring the stateless generation
receives `405 Method Not Allowed`; that generation has no sessions or
server-initiated streams):

#### `GET /mcp`

Opens a server-to-client SSE stream for the session identified by the
`Mcp-Session-Id` header. Clients may send `Last-Event-ID` to resume a
disconnected stream.

**Auth:** Yes

#### `DELETE /mcp`

Terminates the session identified by the `Mcp-Session-Id` header.

**Auth:** Yes

#### `GET /sse` (legacy compatibility)

Compatibility shim for clients that still probe the retired SSE transport.
Emits a single `endpoint` event directing the client to the streamable
endpoint, then closes; there are no sessions and no keepalives:

```
event: endpoint
data: POST /mcp
```

**Auth:** Yes

#### `POST /message` (retired)

Always returns `410 Gone` with a message pointing at `POST /mcp`. The
session-based SSE message endpoint was retired with the legacy transport.

**Auth:** Yes

---

### Static Files (Web UI)

#### `GET /`

Serves the embedded web UI. All unmatched paths fall back to `index.html` for SPA routing. Static assets are served with appropriate content types.

**Auth:** Yes

---

## Error Responses

All API errors return JSON:

```json
{"error": "error message"}
```

**Status codes:**

| Code | Meaning |
|------|---------|
| `200` | Success |
| `201` | Resource created |
| `204` | Success, no content |
| `400` | Invalid input or request |
| `401` | Missing or invalid authentication |
| `404` | Resource not found |
| `405` | HTTP method not allowed |
| `409` | Resource conflict (e.g., duplicate name) |
| `423` | Vault is locked |
| `503` | Service unavailable (runtime not configured, reload not enabled) |

## CORS

The gateway sets CORS headers based on `gateway.allowed_origins`:

```
Access-Control-Allow-Origin: {origin}
Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization
Vary: Origin
```

`OPTIONS` preflight requests return `200 OK` with CORS headers without authentication.
