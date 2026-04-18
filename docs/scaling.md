# Scaling stdio Servers

Guidance for running multiple replicas of a single MCP server behind one gateway entry.

---

## When to Use Replicas

Run `replicas: N` when one stdio process per server is a liability. Typical symptoms:

- **Head-of-line blocking.** A single synchronous stdio server serializes every tool call across every connected user. Slow or SSH-heavy tools block unrelated calls.
- **Crash survivability.** One panic takes the server offline for everyone. Replicas are independent processes — a crash of replica-1 leaves replicas 0 and 2 serving requests.
- **Horizontal scaling under one namespace.** Operators want N processes for throughput without duplicating the server under `junos-a`, `junos-b`, `junos-c` and teaching every client about three tool namespaces.

Skip replicas when the downstream tool already holds a single shared resource (one database connection, one API token with strict rate limits) — N processes will contend on that resource without speedup.

---

## Supported Transports

| Transport | Replicas supported | Why |
|-----------|--------------------|-----|
| Container (image/source) | Yes | Each replica is its own container with a unique name |
| Local process (command) | Yes | Each replica is its own host process with its own pipes |
| SSH (ssh + command) | Yes | Each replica is its own SSH session |
| External URL | **No** | gridctl does not manage the process — scaling is the operator's responsibility on the remote end |
| OpenAPI | **No** | Stateless HTTP, no process to replicate — put a load balancer in front of the upstream API |

Setting `replicas > 1` on `external` or `openapi` servers fails validation with the path `mcp-servers[N].replicas` so the error is unambiguous.

---

## Configuration

```yaml
mcp-servers:
  - name: junos
    command:
      - .venv/bin/python
      - servers/junos-mcp-server/jmcp.py
      - --transport
      - stdio
    replicas: 3
    replica_policy: least-connections
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `replicas` | int | `1` | Number of independent processes to spawn. Range: 1–32 |
| `replica_policy` | string | `"round-robin"` | Dispatch policy: `"round-robin"` or `"least-connections"` |

Upper bound of 32 is a sanity check — higher values are almost always a config error.

---

## Dispatch Policies

### `round-robin` (default)

Each call advances a per-server cursor. Calls land on replicas `0, 1, 2, 0, 1, 2, …` skipping any currently-unhealthy replica. O(1) routing overhead.

Use when tool calls have similar cost and you want predictable, even distribution.

### `least-connections`

Each call goes to the healthy replica with the lowest in-flight count, breaking ties by lowest replica id. Counter increments on dispatch, decrements on response. O(N) routing overhead — negligible at the 32-replica cap.

Use when tool calls have highly variable runtimes (e.g. some take 50ms, others take 30s). Keeps slow calls from pinning a single replica while fast ones pile up.

---

## Operator Trade-offs

- **Memory multiplied by N.** Three Python MCP servers cost three Python runtimes and three sets of imported libraries. Measure before scaling past 3–4.
- **Shared external resources stay single-threaded.** If the server holds one DB connection or one API token, replicas share the same bottleneck. They give you crash survivability but no throughput gain.
- **Tool namespace is unchanged.** Clients still see one `junos__*` tool surface. The replica choice is invisible — never shows up in tool names, tool arguments, or response envelopes.
- **Schema pinning stays per-server.** All replicas must expose the same tool schema. If one replica's schema drifts, the gateway logs a WARN — treat it like any other schema-drift event.

---

## Health and Restart Behavior

Every replica is pinged independently on the gateway's health-check interval. A replica that fails a ping is:

1. Marked unhealthy and excluded from dispatch immediately.
2. Scheduled for reconnection with exponential backoff: **1s → 2s → 4s → 8s → 16s → 30s cap**, with ±25% jitter to prevent a fleet of replicas from resynchronizing their retries.
3. Returned to rotation on the first successful reconnect. The backoff resets — the next failure starts at 1s again.

If **every** replica is unhealthy, tool calls return a structured error naming the server and including the per-replica failure reason. Clients see `no healthy replicas` instead of a hanging call.

Restart storms are bounded by the backoff. A binary that crashes on startup will attempt reconnection a handful of times per minute, not once per health-check tick — the CPU cannot spin.

---

## Observability

Per-replica state is surfaced through every existing gridctl observability surface:

- **Logs.** Every log line on the tool-call path carries a `replica_id` field alongside the server name. Grep by `replica_id=1` to isolate one replica's story.
- **Traces.** Each tool-call span gets a `mcp.replica.id` attribute. Use it to filter a trace waterfall to the slow replica.
- **CLI status.** `gridctl status` rolls up replica health per server:
  ```
  NAME   TYPE             REPLICAS   STATE
  junos  local-process    3/3        healthy
  junos  local-process    2/3        degraded (replica-1 restarting, next in 4s)
  ```
  `gridctl status --replicas` expands to one row per replica with PID/container-id, uptime, and in-flight count.
- **REST API.** `/api/stack/health` includes a `replicas` array for every server with a replica set, each entry carrying `replicaId`, `state`, `inFlight`, `restartAttempts`, `nextRetryAt`, and the transport-specific handle (`pid` or `containerId`).
- **Metrics.** `pkg/metrics/accumulator.go` tracks per-replica counters. Per-server aggregates remain (they sum across replicas).

---

## Quick Examples

### Multi-user Python server with least-connections

```yaml
mcp-servers:
  - name: junos
    command:
      - .venv/bin/python
      - servers/junos-mcp-server/jmcp.py
    replicas: 3
    replica_policy: least-connections
```

### Container server with three replicas

```yaml
mcp-servers:
  - name: github
    image: ghcr.io/github/github-mcp-server:latest
    transport: stdio
    replicas: 3
    env:
      GITHUB_PERSONAL_ACCESS_TOKEN: "${GITHUB_PERSONAL_ACCESS_TOKEN}"
```

Each replica becomes a separate container. Container names are deterministic (`<stack>-<server>-replica-<id>`) so re-applying is idempotent.

### Rejected configurations

```yaml
mcp-servers:
  - name: remote
    url: https://mcp.example.com/sse
    replicas: 3                  # ❌ validation error: external transport cannot use replicas
```

Validation surfaces the exact path (`mcp-servers[N].replicas`) so CI can fail fast.
