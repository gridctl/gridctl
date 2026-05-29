# REST API Reference

The gridctl gateway exposes a REST API for managing stacks, secrets, skills, and MCP protocol interactions. By default the gateway listens on port `8180`.

## Authentication

When `gateway.auth` is configured, all endpoints except `/health` and `/ready` require authentication. CORS preflight (`OPTIONS`) requests are also exempt.

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
| `gateway` | object | Gateway name and version |
| `mcp-servers` | []object | Status of each MCP server |
| `resources` | []object | Resource container status |
| `sessions` | int | Active SSE session count |
| `stack_name` | string | Active stack name (omitted in stackless mode) |
| `registry` | object | Registry skill counts (omitted if empty) |
| `code_mode` | string | Code mode status (omitted if `"off"`) |
| `token_usage` | object | Token usage metrics (omitted if no metrics accumulator) |
| `cost` | object | USD cost snapshot (omitted when no cost has been recorded) |

**Token usage fields:**

| Field | Type | Description |
|-------|------|-------------|
| `session` | object | Aggregate token counts (`input_tokens`, `output_tokens`, `total_tokens`) |
| `per_server` | map | Token counts keyed by server name |
| `per_client` | map | Token counts keyed by normalized MCP client name (omitted when no per-client traffic has been observed) |
| `format_savings` | object | Savings from output format conversion (`original_tokens`, `formatted_tokens`, `saved_tokens`, `savings_percent`) |

**Cost fields:**

| Field | Type | Description |
|-------|------|-------------|
| `session` | object | Aggregate USD cost (`input_usd`, `output_usd`, `cache_read_usd`, `cache_write_usd`, `total_usd`) |
| `per_server` | map | USD cost keyed by server name |
| `per_replica` | map | USD cost keyed by `(server, replica_id)` (omitted when no replica-aware traffic has been observed) |
| `per_client` | map | USD cost keyed by normalized MCP client name (omitted when no per-client traffic has been observed) |

**MCP server status** includes `outputFormat` (string, omitted when unset) showing the configured output format for each server, and `autoscale` (object, omitted when the server has no autoscale block) described under [`/api/mcp-servers`](#get-apimcp-servers).

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

#### `GET /api/tools/usage`

Returns per-(server, tool) usage observed by the gateway: cumulative call count and the last-called timestamp. Powers the Tools workspace **Audit Mode**, which separates actively-used, configured-but-unused, and disabled tools.

Usage is recorded for both direct tool calls and tools invoked through code mode's `execute` (both flow through the same observer). For servers with metrics persistence enabled, the data is restored from disk on startup so it survives gateway restarts; otherwise it reflects activity since the last gateway start.

`observedSince` is when this gateway process began recording. With persistence enabled, restored counts and timestamps may predate it — clients should treat tools absent from `servers` (or with no `lastCalledAt`) as "no recorded calls" rather than asserting a longer disuse history than `observedSince` supports.

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
      "create_issue": { "calls": 42, "lastCalledAt": "2026-05-24T09:13:00Z" },
      "list_repos": { "calls": 3, "lastCalledAt": "2026-05-21T08:00:00Z" }
    }
  }
}
```

`servers` is an object keyed by server name; each value maps unprefixed tool names to their stats. Tools that have never been called are omitted. Returns `503` when no metrics accumulator is configured.

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

### Cost Metrics

#### `GET /api/metrics/cost`

Returns historical USD cost time-series data, mirroring the `/api/metrics/tokens` shape. Cost is computed at observation time using the active pricing source (LiteLLM by default) and never recomputed from stored token totals.

**Auth:** Yes

| Query Param | Type | Default | Description |
|-------------|------|---------|-------------|
| `range` | string | `"1h"` | Time range: `"30m"`, `"1h"`, `"6h"`, `"24h"`, `"7d"` |
| `per_client` | bool | `false` | When `true`, the response includes a `per_client` map grouping cost by the originating MCP client (e.g. `claude-code`, `cursor`). |

```bash
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8180/api/metrics/cost?range=24h&per_client=true"
```

**Response:**
```json
{
  "range": "24h",
  "interval": "1h",
  "data_points": [
    {"timestamp": "2026-05-07T00:00:00Z", "usd": 0.42}
  ],
  "per_server": {
    "github": [{"timestamp": "2026-05-07T00:00:00Z", "usd": 0.42}]
  },
  "per_client": {
    "claude-code": [{"timestamp": "2026-05-07T00:00:00Z", "usd": 0.30}],
    "cursor":      [{"timestamp": "2026-05-07T00:00:00Z", "usd": 0.12}]
  }
}
```

#### `DELETE /api/metrics/cost`

Clears recorded cost data while leaving token counters and the format-savings tally intact. Use this when rotating pricing sources or recovering from a bad pricing snapshot without losing token history.

**Auth:** Yes

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/metrics/cost
```

**Response:**
```json
{"status": "ok", "message": "Cost metrics cleared"}
```

---

### Optimize

#### `GET /api/optimize`

Returns an `OptimizeReport` derived from gateway-observed data: the registered server list, per-server token + cost totals, and per-(server, tool) call counts. v1 implements the `unused_server` and `unused_tool` heuristics; gateways with less than 24h of observation get a single `info` finding ("need more data") so reports never over-fire.

**Auth:** Yes

| Query Param | Type | Default | Description |
|-------------|------|---------|-------------|
| `stack` | string | - | Active stack name. `404` if it does not match. |
| `min_impact` | float | `0` | Drop findings whose weekly USD impact is below this threshold. `info` findings are always retained. |
| `severity` | string | - | Comma-separated allowlist of `info`, `warn`, `critical`. |

```bash
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8180/api/optimize?min_impact=0.10"
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
      "impact_usd_per_week": 0.27,
      "remediation": "# Remove the server entirely:\nmcp-servers:\n  # delete the entry for: github\n",
      "detected_at": "2026-05-07T12:00:00Z"
    }
  ],
  "health_score": 90,
  "generated_at": "2026-05-07T12:00:00Z"
}
```

Returns `503` when no metrics accumulator is configured; `404` when `stack` does not match the active stack.

---

### Hot Reload

#### `POST /api/reload`

Triggers a configuration reload from the stack file. Requires the gateway to have been started with `--watch`.

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

#### `PUT /api/mcp-servers/{name}/tools`

Updates an MCP server's tool whitelist in the live `stack.yaml` and triggers a hot reload. Powers the live tool whitelist editor in the topology sidebar. The YAML write is atomic; concurrent external edits surface as `409` so the UI can re-fetch without clobbering changes.

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

Applies tool-whitelist changes to **multiple** servers in one atomic `stack.yaml` write and triggers a **single** hot reload — the fleet-bulk counterpart to the per-server endpoint above. Powers the Tools workspace bulk actions (fleet-wide expose-all / clear / pattern filtering), where applying N servers via N single-server calls would cost N reloads.

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

The body mirrors the MCP server config (`name`, `image`, `source`, `url`, `port`, `transport`, `command`, `env`, `build_args`, `network`, `ssh`, `openapi`, `tools`, `output_format`, `ready_timeout`, `replicas`). The body is capped at 64 KiB.

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

Env-var values present in the request body are scrubbed from error messages and hints to avoid leaking secrets.

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
  "secrets_count": 12,
  "sets_count": 2
}
```

`secrets_count` and `sets_count` are only included when the vault is unlocked.

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

Lists all secret keys with their set assignments (values not included).

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var
```

**Response:**
```json
[
  {"key": "DB_PASSWORD", "set": "production"},
  {"key": "API_KEY"}
]
```

#### `POST /api/var`

Creates a new secret.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var \
  -H "Content-Type: application/json" \
  -d '{"key": "DB_PASSWORD", "value": "secret123", "set": "production"}'
```

**Response:** `201 Created`
```json
{"key": "DB_PASSWORD", "status": "created"}
```

Key names must match `[a-zA-Z_][a-zA-Z0-9_]*`.

#### `GET /api/var/{key}`

Returns a secret value.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/DB_PASSWORD
```

**Response:**
```json
{"key": "DB_PASSWORD", "value": "secret123"}
```

#### `PUT /api/var/{key}`

Updates a secret value.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/DB_PASSWORD \
  -H "Content-Type: application/json" \
  -d '{"value": "new-secret"}'
```

**Response:**
```json
{"key": "DB_PASSWORD", "status": "updated"}
```

#### `DELETE /api/var/{key}`

Deletes a secret.

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

Assigns or unassigns a secret to a variable set.

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

#### `POST /api/var/import`

Bulk imports secrets.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/var/import \
  -H "Content-Type: application/json" \
  -d '{"secrets": {"API_KEY": "key123", "DB_HOST": "localhost"}}'
```

**Response:**
```json
{"imported": 2}
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

### Registry (Agent Skills) *(experimental)*

Manage reusable skills stored as SKILL.md files. Skills have three lifecycle states: `draft`, `active`, and `disabled`.

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

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/registry/skills
```

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

#### `GET /api/registry/skills/{name}/files/{path}`

Reads a file from a skill directory. Content-Type is detected from file extension.

**Auth:** Yes

#### `PUT /api/registry/skills/{name}/files/{path}`

Writes a file to a skill directory. Body is raw file content. Maximum 1MB.

**Auth:** Yes

**Response:** `204 No Content`

#### `DELETE /api/registry/skills/{name}/files/{path}`

Deletes a file from a skill directory.

**Auth:** Yes

**Response:** `204 No Content`

---

### MCP Protocol

#### `POST /mcp`

JSON-RPC 2.0 endpoint for MCP protocol operations.

**Auth:** Yes

**Supported methods:**

| Method | Description |
|--------|-------------|
| `initialize` | Initialize MCP session |
| `tools/list` | List available tools |
| `tools/call` | Call a tool |
| `prompts/list` | List available prompts |
| `prompts/get` | Get a specific prompt |
| `resources/list` | List available resources |
| `resources/read` | Read a specific resource |
| `ping` | Connectivity check |
| `notifications/initialized` | Client initialization notification |

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

#### `GET /sse`

Server-Sent Events connection for bidirectional MCP communication.

**Auth:** Yes

The SSE connection sends an initial `endpoint` event with the URL for posting messages:

```
event: endpoint
data: http://localhost:8180/message?sessionId=abc123
```

Keepalive comments are sent every 30 seconds.

#### `POST /message`

Message endpoint for SSE clients. Accepts JSON-RPC requests and returns responses.

**Auth:** Yes

| Query Param | Type | Description |
|-------------|------|-------------|
| `sessionId` | string | Session ID from the SSE endpoint event |

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
