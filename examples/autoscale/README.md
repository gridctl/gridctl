# Reactive autoscaling

Gridctl can autoscale a local-process, SSH, or container-backed MCP server
based on live in-flight load. Enable it by replacing `replicas: N` with an
`autoscale` block on the server.

```yaml
mcp-servers:
  - name: junos
    command: [python, junos-server.py]
    autoscale:
      min: 1
      max: 8
      target_in_flight: 3
      scale_up_after: 30s
      scale_down_after: 5m
```

## What it does

Every 10s the scaler samples the median in-flight request count across healthy
replicas and decides:

- **Scale up** — when the rolling median stays above `target_in_flight` for
  the full `scale_up_after` window, spawn enough replicas to drive the median
  back under target. Clamped at `max`.
- **Scale down** — when the rolling median stays below half the target for
  the full `scale_down_after` window, reap one replica per tick, floored at
  `min + warm_pool`.
- **Cold start** — a tool call that arrives while `current == 0` (only
  possible under `idle_to_zero: true, min: 0`) synchronously spawns the first
  replica before the call returns.

Every decision emits one structured log line you can grep for:

```
level=INFO msg="autoscale decision" server=junos direction=up current=1 target=3 median_in_flight=9 reason=load
```

Scale actions also produce trace spans (`mcp.autoscale.spawn`, `mcp.autoscale.reap`)
carrying a `mcp.autoscale.direction` attribute, so the traces UI can filter
scale events.

## Observability

| Surface | What you'll see |
|---------|-----------------|
| `gridctl status --replicas` | New `AUTOSCALE` column: `min/current/max (target=N)` |
| `/api/mcp-servers` | `autoscale` object per server with current / target / median |
| Logs | One `"autoscale decision"` line per tick (INFO); spawn/reap failures at WARN |
| Traces | Spans under `gridctl.autoscaler` and `mcp.autoscale.*` |

## Operator trade-offs

- **Memory cost scales with `max`.** Three Python replicas cost three Python
  runtimes and three sets of imported libraries. Keep `max` close to the
  peak-demand replica count; a very high `max` is rarely useful and makes
  capacity planning harder.
- **Cold-start latency.** When `idle_to_zero: true`, the first tool call
  after an idle period pays the full transport-specific start cost (container
  boot or process spawn + MCP handshake). See the "Cold start penalty" section
  below; increase client timeouts when running cold.
- **WarmPool is the always-on buffer.** `min + warm_pool` is the floor the
  scaler refuses to cross on scale-down. Use it to absorb brief traffic
  spikes without paying scale-up cooldown each time.

## Cold start penalty

`idle_to_zero: true` reaps every replica after `scale_down_after` of idle
time. The first tool call after that adds:

| Transport | Cold-start cost (rough) |
|-----------|-------------------------|
| Local process | ~100ms–1s (Go/Python interpreter + imports) |
| SSH | ~1–3s (SSH handshake + process start) |
| Container (stdio) | ~2–10s (container create + attach + init) |
| Container (HTTP) | ~5–30s (container create + readiness poll) |

**Configure client timeouts to tolerate this.** Claude Desktop, Cursor, and
most MCP clients default to a 30–60s tool-call timeout; that's enough for
local/SSH but may be tight for container cold starts. When you enable
`idle_to_zero` for a container-backed server, raise the client's tool-call
timeout to **60s minimum** (or disable idle-to-zero for that server).

You can also use `warm_pool: 1` with `min: 0, idle_to_zero: true` to keep
one replica permanently warm — trading a small amount of idle memory for a
zero-cold-start guarantee.

## Mutually exclusive with `replicas: N`

A server can't have both `autoscale` and `replicas: N`. `gridctl validate`
rejects that configuration up front:

```
mcp-servers[0]: cannot set both 'replicas' and 'autoscale' on the same server
```

Switching between `replicas: 3` and an `autoscale` block on the same server
is a full restart at hot-reload time (different bookkeeping). Changes
*inside* an existing `autoscale` block are applied as a policy update without
dropping in-flight calls.

## Not supported

- External URL servers and OpenAPI servers — they're stateless from gridctl's
  point of view; scale them at the HTTP tier.
- Cross-daemon load balancing — decisions are local to one gridctl daemon.
- Predictive pre-warm — policies are reactive to current load only. Use
  `warm_pool` to keep a buffer above the load floor.
