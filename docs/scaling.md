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

---

## Autoscaling

A server can replace static `replicas: N` with an `autoscale` block that lets
gridctl spawn and reap replicas reactively based on in-flight load. The same
transport rules apply — autoscale is supported on container, local-process,
and SSH servers, and rejected on external URL and OpenAPI transports.

```yaml
mcp-servers:
  - name: junos
    command:
      - .venv/bin/python
      - servers/junos-mcp-server/jmcp.py
      - --transport
      - stdio
    replica_policy: least-connections
    autoscale:
      min: 1                    # >= 0; must be >= 1 unless idle_to_zero is true
      max: 8                    # >= min; capped at 32
      target_in_flight: 3       # per-replica in-flight the scaler tries to hold below
      scale_up_after: 30s       # min 10s; default 30s
      scale_down_after: 5m      # min 1m; default 5m
      warm_pool: 0              # extra idle replicas kept above the load floor
      idle_to_zero: false       # when true, min may be 0 (scale-to-zero)
```

`autoscale` and `replicas` are **mutually exclusive** on the same server.
A server without an `autoscale` block behaves exactly as it did before; every
new field is optional at the YAML layer.

### Decision rules in plain prose

Every 10s the scaler samples the median in-flight request count across the
server's currently-healthy replicas, appends the sample to a rolling window
at least 30s long, and decides:

1. **Noop** — target is within the current set and no cooldown is pending.
2. **Scale up** — target > current AND the rolling median has exceeded
   `target_in_flight` for the full `scale_up_after` window. Spawns enough
   replicas in one tick to reach target, clamped at `max`. Cooldown: no
   further scale-up until `scale_up_after` has elapsed since the last one.
   *Exception:* if `current < min + warm_pool`, the cooldown is skipped so
   the warm pool recovers eagerly after a crash.
3. **Scale down** — target < current AND the rolling median has been below
   half the target for the full `scale_down_after` window. Reaps **at most
   one replica per tick**, preferring the replica with the lowest in-flight
   count (ties broken by oldest). Floored at `min + warm_pool`. Cooldown is
   tracked separately from scale-up.
4. **Idle-to-zero reap** — when `idle_to_zero: true` and `min: 0` and the
   rolling window has been zero for the full `scale_down_after`, reap every
   remaining replica through the same drain protocol.
5. **Cold start** — a tool call that arrives while the set is at zero
   replicas (`idle_to_zero: true` reaped everything) synchronously spawns
   the first replica before returning. The tool call blocks on that spawn.

Every decision emits exactly one structured log line with `direction`
(up/down/noop), `current`, `target`, `median_in_flight`, and `reason`
(`load` / `warm_pool` / `idle_to_zero` / `cooldown` / `cold_start`). Scale
actions also produce trace spans (`mcp.autoscale.spawn` and `mcp.autoscale.reap`).

Drain protocol: when a replica is picked for reap, it's removed from
dispatch immediately, then the scaler waits up to 30s for its in-flight
counter to reach zero before closing the client. Exceeding that deadline
puts the replica back in rotation and aborts this tick's scale-down.

### Operator trade-offs

- **Memory cost scales with `max`.** Three Python replicas cost three
  runtimes and three sets of imported libraries. Right-size `max` to your
  peak-demand replica count; a very high `max` is almost always a config
  error (the validator caps it at 32 as a sanity check).
- **Cold-start latency.** When `idle_to_zero: true`, the first tool call
  after an idle period pays the full transport-specific start cost (see the
  Cold Start Penalty section below).
- **WarmPool is an always-on buffer.** `min + warm_pool` is the floor the
  scaler refuses to cross on scale-down. Useful to absorb brief spikes
  without paying the scale-up cooldown every time.
- **Shared upstream resources stay bottlenecked.** If the server pool shares
  a single DB connection or API token, adding replicas distributes stdio
  pipes but doesn't speed up serialisation at the bottleneck.

### Cold Start Penalty

`idle_to_zero: true, min: 0` reaps every replica after `scale_down_after`
of zero traffic. The next tool call pays the full cold-start cost:

| Transport | Rough cold-start cost |
|-----------|-----------------------|
| Local process | 100ms–1s |
| SSH | 1–3s |
| Container (stdio) | 2–10s |
| Container (HTTP) | 5–30s (readiness poll) |

**Configure client-side timeouts to tolerate this.** Most MCP clients
(Claude Desktop, Cursor, Windsurf) default to 30–60s tool-call timeouts,
which may be tight for container cold starts. When enabling `idle_to_zero`
for a container-backed server, raise the client's tool-call timeout to at
least **60s** or keep a permanent warm replica (`min: 0, warm_pool: 1`).

### Hot reload

- Changes *inside* an existing `autoscale` block are applied as a policy
  update — the scaler swaps the policy atomically on the next tick and no
  in-flight tool calls are disrupted.
- Switching between `replicas: N` and `autoscale:` (or vice versa) is a
  full server restart, same as any other structural change.

### Observability

- **Logs** — one `"autoscale decision"` line per tick per autoscaled server;
  spawn/reap failures emit `autoscale spawn failed` / `autoscale reap failed`
  at WARN with `server`, `direction`, `current`, `target`, `error` attrs.
- **Traces** — `mcp.autoscale.spawn` / `mcp.autoscale.reap` spans with an
  `mcp.autoscale.direction` attribute for UI filtering.
- **CLI** — `gridctl status --replicas` shows an `AUTOSCALE` column rendering
  `min/current/max (target=N)` for autoscaled servers; blank for static ones.
- **REST** — `/api/mcp-servers` and the `/api/stack/health` payload include
  an `autoscale` object per server: `{min, max, current, target, targetInFlight,
  medianInFlight, lastDecision, lastScaleUpAt, lastScaleDownAt, warmPool,
  idleToZero}`.

