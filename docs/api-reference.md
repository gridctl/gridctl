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

Readiness check. Returns `200 OK` only when all MCP servers are connected and initialized. Returns `503 Service Unavailable` in two cases: any MCP server is not yet ready, or the daemon is running in stackless mode (no stack loaded yet — use `POST /api/stack/initialize` or the wizard to load one).

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

Returns the overall gateway status including servers, agents, resources, sessions, and optional features.

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
      "healthError": ""
    }
  ],
  "agents": [
    {
      "name": "code-reviewer",
      "status": "running",
      "variant": "local",
      "image": "alpine:latest",
      "containerId": "abc123def456",
      "uses": [{"server": "github", "tools": ["get_file_contents"]}],
      "hasA2A": true,
      "role": "local",
      "url": "",
      "endpoint": "/a2a/code-reviewer",
      "skillCount": 2,
      "skills": ["review-code", "summarize-pr"],
      "description": "AI assistant for code review"
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
  "a2a_tasks": 0,
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
| `agents` | []object | Unified agent status (local + remote) |
| `resources` | []object | Resource container status |
| `sessions` | int | Active SSE session count |
| `a2a_tasks` | int | Active A2A task count (omitted if A2A disabled) |
| `registry` | object | Registry skill counts (omitted if empty) |
| `code_mode` | string | Code mode status (omitted if `"off"`) |
| `token_usage` | object | Token usage metrics (omitted if no metrics accumulator) |

**Token usage fields:**

| Field | Type | Description |
|-------|------|-------------|
| `session` | object | Aggregate token counts (`input_tokens`, `output_tokens`, `total_tokens`) |
| `per_server` | map | Token counts keyed by server name |
| `format_savings` | object | Savings from output format conversion (`original_tokens`, `formatted_tokens`, `saved_tokens`, `savings_percent`) |

**MCP server status** includes `outputFormat` (string, omitted when unset) showing the configured output format for each server.

#### `GET /api/mcp-servers`

Returns MCP server status details.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/mcp-servers
```

#### `GET /api/tools`

Returns all aggregated tools from registered MCP servers.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/tools
```

#### `GET /api/logs`

Returns structured log entries from the gateway log buffer.

**Auth:** Yes

| Query Param | Type | Default | Description |
|-------------|------|---------|-------------|
| `lines` | int | `100` | Number of recent log entries to return |
| `level` | string | — | Comma-separated level filter (e.g., `"ERROR,WARN"`) |

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
    "configPath": "/Users/user/Library/Application Support/Claude/claude_desktop_config.json"
  }
]
```

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

### Agent Control

#### `GET /api/agents/{name}/logs`

Returns container logs for an agent.

**Auth:** Yes

| Query Param | Type | Default | Description |
|-------------|------|---------|-------------|
| `lines` | int | `100` | Number of log lines to return |

```bash
curl -H "Authorization: Bearer $TOKEN" "http://localhost:8180/api/agents/code-reviewer/logs?lines=50"
```

**Response:** JSON array of log line strings.

Returns a friendly message (not an error) for non-container agents (external, SSH, local process, remote A2A).

#### `POST /api/agents/{name}/restart`

Restarts an agent container.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/agents/code-reviewer/restart
```

**Response:**
```json
{
  "status": "restarted",
  "agent": "code-reviewer"
}
```

#### `POST /api/agents/{name}/stop`

Stops an agent container.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/agents/code-reviewer/stop
```

**Response:**
```json
{
  "status": "stopped",
  "agent": "code-reviewer"
}
```

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
- `404` — Server name not found in gateway
- `500` — Restart failed (container error, connection timeout, etc.)

---

### Vault (Secrets Management)

The vault stores secrets locally with optional encryption. Secrets can be organized into variable sets for scoped injection.

#### `GET /api/vault/status`

Returns vault lock state and counts. Does not require the vault to be unlocked.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault/status
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

#### `POST /api/vault/unlock`

Unlocks an encrypted vault with a passphrase.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault/unlock \
  -H "Content-Type: application/json" \
  -d '{"passphrase": "my-secret-passphrase"}'
```

**Response:**
```json
{"status": "unlocked"}
```

#### `POST /api/vault/lock`

Encrypts the vault with a passphrase.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault/lock \
  -H "Content-Type: application/json" \
  -d '{"passphrase": "my-secret-passphrase"}'
```

**Response:**
```json
{"status": "locked"}
```

#### `GET /api/vault`

Lists all secret keys with their set assignments (values not included).

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault
```

**Response:**
```json
[
  {"key": "DB_PASSWORD", "set": "production"},
  {"key": "API_KEY"}
]
```

#### `POST /api/vault`

Creates a new secret.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault \
  -H "Content-Type: application/json" \
  -d '{"key": "DB_PASSWORD", "value": "secret123", "set": "production"}'
```

**Response:** `201 Created`
```json
{"key": "DB_PASSWORD", "status": "created"}
```

Key names must match `[a-zA-Z_][a-zA-Z0-9_]*`.

#### `GET /api/vault/{key}`

Returns a secret value.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault/DB_PASSWORD
```

**Response:**
```json
{"key": "DB_PASSWORD", "value": "secret123"}
```

#### `PUT /api/vault/{key}`

Updates a secret value.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault/DB_PASSWORD \
  -H "Content-Type: application/json" \
  -d '{"value": "new-secret"}'
```

**Response:**
```json
{"key": "DB_PASSWORD", "status": "updated"}
```

#### `DELETE /api/vault/{key}`

Deletes a secret.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault/DB_PASSWORD
```

**Response:** `204 No Content`

#### `GET /api/vault/sets`

Lists all variable sets with member counts.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault/sets
```

#### `POST /api/vault/sets`

Creates a new variable set.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault/sets \
  -H "Content-Type: application/json" \
  -d '{"name": "production"}'
```

**Response:** `201 Created`
```json
{"name": "production", "status": "created"}
```

Set names must match `[a-z0-9][a-z0-9-]*`.

#### `DELETE /api/vault/sets/{name}`

Deletes a variable set.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault/sets/staging
```

**Response:** `204 No Content`

#### `PUT /api/vault/{key}/set`

Assigns or unassigns a secret to a variable set.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X PUT -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault/DB_PASSWORD/set \
  -H "Content-Type: application/json" \
  -d '{"set": "production"}'
```

**Response:**
```json
{"key": "DB_PASSWORD", "set": "production", "status": "updated"}
```

#### `POST /api/vault/import`

Bulk imports secrets.

**Auth:** Yes | **Requires:** Vault unlocked

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/vault/import \
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
  "hint": "POST /api/vault/unlock with passphrase"
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
- `404` — No pins found for that server, or server not found in gateway
- `503` — Pin store not available

#### `DELETE /api/pins/{server}`

Deletes the pin record for a server. The server will be re-pinned on the next deploy.

**Auth:** Yes

```bash
curl -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8180/api/pins/github
```

**Response:** `204 No Content`

**Errors:**
- `404` — No pins found for that server

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

#### `GET /api/registry/skills/{name}/workflow`

Returns the parsed workflow definition as JSON, including DAG visualization data.

**Auth:** Yes

**Response:**
```json
{
  "name": "workflow-basic",
  "inputs": { ... },
  "workflow": [ ... ],
  "output": { ... },
  "dag": {
    "levels": [[...], [...]]
  }
}
```

#### `POST /api/registry/skills/{name}/execute`

Executes a workflow skill.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8180/api/registry/skills/workflow-basic/execute \
  -H "Content-Type: application/json" \
  -d '{"arguments": {"a": 1, "b": 2}}'
```

Returns `400` if the skill has no workflow definition.

#### `POST /api/registry/skills/{name}/validate-workflow`

Dry-run workflow validation without executing tools. Validates DAG structure, template resolution, conditions, and tool availability.

**Auth:** Yes

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:8180/api/registry/skills/workflow-basic/validate-workflow \
  -H "Content-Type: application/json" \
  -d '{"arguments": {"a": 1, "b": 2}}'
```

**Response:**
```json
{
  "valid": true,
  "dag": {"levels": [[...]]},
  "resolvedArgs": { ... },
  "errors": [],
  "warnings": []
}
```

---

### MCP Protocol

#### `POST /mcp`

JSON-RPC 2.0 endpoint for MCP protocol operations.

**Auth:** Yes

**Supported methods:**

| Method | Description |
|--------|-------------|
| `initialize` | Initialize MCP session |
| `tools/list` | List available tools (supports `X-Agent-Name` header for agent-scoped filtering) |
| `tools/call` | Call a tool (supports `X-Agent-Name` header for access control) |
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

| Query Param | Type | Description |
|-------------|------|-------------|
| `agent` | string | Optional agent identity for access control |

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

### A2A Protocol *(experimental)*

Agent-to-Agent protocol endpoints for inter-agent communication.

#### `GET /.well-known/agent.json`

A2A agent discovery. Returns all local agent cards.

**Auth:** Yes

```bash
curl -H "Authorization: Bearer $TOKEN" http://localhost:8180/.well-known/agent.json
```

**Response:**
```json
{
  "agents": [
    {
      "name": "code-reviewer",
      "description": "AI assistant for code review",
      "url": "http://localhost:8180/a2a/code-reviewer",
      "version": "1.0.0",
      "skills": [
        {
          "id": "review-code",
          "name": "Review Code",
          "description": "Analyze code for bugs and style issues"
        }
      ]
    }
  ]
}
```

#### `GET /a2a/{name}`

Returns the agent card for a specific A2A agent.

**Auth:** Yes

#### `POST /a2a/{name}`

A2A JSON-RPC endpoint for agent communication.

**Auth:** Yes

**Supported methods:** `message/send`, `tasks/get`, `tasks/list`, `tasks/cancel`

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
Access-Control-Allow-Headers: Content-Type, X-Agent-Name, Authorization
Vary: Origin
```

`OPTIONS` preflight requests return `200 OK` with CORS headers without authentication.
