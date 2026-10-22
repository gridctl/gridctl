# Usage Observability

Gridctl measures the token traffic that flows through the gateway. Ordinary successful dispatches count arguments and results with the configured tokenizer, accumulating per server, per replica, per client, and per (server, tool) pair, alongside cumulative call counts and last-used timestamps. Because the gateway also performs output format conversion, it measures the savings directly: each converted result is counted before and after conversion, so the reported format savings come from the gateway's own observed traffic, not a projection.

## What the gateway measures

Input tokens are counted on tool-call arguments and output tokens on tool-result text. Non-text payloads are not counted. Tool-call counts are attributed to the server that handled the call, the replica that served it, the calling client (the verified principal in OAuth mode, otherwise the declared client identity), and the individual tool. Call counts and last-called timestamps are kept per (server, tool) pair, and registry skills get usage tracking for `prompts/get`. Downstream prompt gets are not attributed. When `output_format: toon` or `csv` is active, the format-savings tally records original tokens, formatted tokens, and the saved difference.

## Tokenizers

Token counting defaults to an embedded `cl100k_base` BPE tokenizer (`gateway.tokenizer: embedded`). Claude's vocabulary is unpublished, so `cl100k_base` counts are an approximation for Claude models, typically within 10–15% for English and code content. That is accurate enough for the comparisons this data exists for: ranking servers, spotting waste, and trending over time.

For exact counts, set `gateway.tokenizer: api` to route counting through Anthropic's `count_tokens` endpoint, with the key from `gateway.tokenizer_api_key` or the `ANTHROPIC_API_KEY` environment variable. On any API error the counter falls back to the embedded tokenizer rather than dropping the measurement.

### Sensitive-call counting

The internal sensitive-call classification uses trusted server construction
metadata, with no stack setting or caller annotation. The experimental A2A adapter
uses this path for every call; other sources retain ordinary counting. Classified
calls skip payload-bearing observer callbacks and the configured tokenizer,
including API-backed counting. They retain local estimates
at four bytes per token, attributed to server, replica, and operation category
(`send`, `task_get`, `task_cancel`, or `skill`), without client or individual skill
attribution. Failures also contribute usage. Format conversion is skipped for
these calls. See [capability and sensitive-call primitives](capability-primitives.md#observations-and-diagnostics)
for the observer contract and counting boundary.

## Where usage surfaces

The **Metrics workspace** in the web UI charts token throughput over time and breaks totals down by server, replica, client, and tool, alongside call counts, format savings, and rate-limit state.

`GET /api/metrics/tokens` returns the token time series (aggregate and per-server) over a selectable range, and `GET /api/tools/usage` returns per-tool call counts, last-called timestamps, and token totals. `GET /api/status` carries the session's aggregate token usage and format savings. See the [REST API Reference](api-reference.md).

`gridctl optimize` analyzes gateway-observed data and prints findings with a projected weekly token impact: unused servers, unused tools, schema overhead, and format-savings shortfalls, each with a paste-ready YAML remediation. Impact figures (`impact_tokens_per_week` in `--format json`) are tokens per week: the schema heuristics project schema tokens assuming roughly 500 prompts per week (a tool schema rides every prompt the client sends), and the format-savings shortfall normalizes its measured savings over the observation window. `--min-impact` filters findings below a token threshold; `info` findings are always retained.

Rate limits (`limits.rate_limits` in `stack.yaml`) cap calls per minute per client, server, or tool, enforced at dispatch with no token accounting required. `gridctl limits` and `GET /api/limits` show every configured limit and its state.

## Run records

Opt-in `runs.enabled` stores one metadata-only final disposition per returning gateway `tools/call`. Metrics remain aggregate usage. Traces remain sampled timing detail. Runs remain retained dispositions and stay available when tracing is disabled.

Records are saved after dispatch returns. Recording is best-effort. Attempts interrupted by a crash may leave no record, and older records may have been removed by retention or wipe. A transport failure does not prove the remote action did not execute. Input-required is the disposition of one round, not human approval.

A2A `input-required` and `auth-required` are remote states inside an ordinary
completed tool result, not the run's `input_required` disposition. Run records
cannot recover task or context handles. See [A2A result delivery](config-schema.md#tools-and-capability-delivery).

A policy denial is recorded as disposition `denied`, stage `gate`, reason `policy_blocked`. Rate-limit denials stay `gate_denied`. An outer `search_tools` or `call_tool` attempt is stage `discovery` with reason `search_tools` or `call_tool`. The inner dispatch is a separate attempt with `parent_attempt_id` set. The outer frame records no token usage of its own. The allowlist is generated IDs, timestamps, total dispatch duration, bounded target names, disposition/stage/reason, optional replica and sampled trace IDs, and optional attribution labels. Argument and result values, hashes, code, raw errors, tokens, headers, and host paths are excluded. Names and labels may still be sensitive. OAuth run records retain the declared display label separately from the verified raw principal in `access_label`; traces and usage attribute to that principal. Subjects can contain URL-like text or base64 punctuation. Stored access labels remain limited to 128 bytes, with invalid UTF-8 and control characters omitted. Labels in other auth modes are caller-declared.

Default retention is seven days and 100 MiB of logical record bytes per stack, including the active file and rotated segments. Age pruning runs at writer start and hourly while the process is up, compacting old records out of the active file. Physical filesystem overhead is extra. Wipe is stack-wide, is not secure erasure, and does not remove exports or backups. Disabling recording does not delete retained history. A stalled disk syscall can outlive the two-second shutdown drain; remaining queued events are counted as dropped.

Query with `gridctl runs list`, `GET /api/runs`, or the Runs tab beside Traces. Live and offline sources are explicit; a failed live request never falls back to disk. See [Run records](config-schema.md#run-records) and [CLI runs](cli-reference.md#runs).

## Diagnostic privacy and migration

Gateway logs, trace names/attributes, usage identifiers, and run metadata mask
recognizable typed capability strings before recording them. Policy checks still
use the original names and labels. Recognition does not cover arbitrary encoded
or split secrets, and ordinary payload-bearing observers retain their existing
access. Sensitive executions exclude payloads instead of relying on recognition.

Code-mode failure logs now contain a local category, such as
`code_execution_failed`, rather than a parser excerpt or thrown value. The
requesting caller still receives execution errors and console output. Correlate
diagnostics with generated attempt/trace IDs and outcome categories; do not join
records using secret-bearing names or parse raw code-error log text. Structured
log attributes containing recognized secrets may become sanitized JSON strings.
These diagnostic-output changes are included in v1.0.0-rc.2; log wording is not
a versioned contract. See [diagnostic troubleshooting](troubleshooting.md#code-mode-error-details-are-missing-from-logs).

## OpenTelemetry attributes

Span attributes follow the OpenTelemetry MCP semantic conventions where those conventions name a key, and use a `gridctl.*` key otherwise. The old keys are still emitted until 1.1.0 so existing dashboards keep working. A span that sets a renamed key also sets every legacy alias of that key, including an alias that previously appeared only on a different span. Seeded traces on disk keep the old keys forever; the trace buffer, `gridctl traces`, per-server file routing, the Traces waterfall, and Span detail read the new key first and fall back to the old one.

| Old key | New key | Span | Removal |
|---------|---------|------|---------|
| `server.name` | `gridctl.server.name` | root, downstream, routing, format conversion, relay, autoscaler | 1.1.0 |
| `mcp.server.name` | `gridctl.server.name` | downstream token stamp | 1.1.0 |
| `mcp.server` | `gridctl.server.name` | relay | 1.1.0 |
| `mcp.replica.id` | `gridctl.replica.id` | root, downstream, routing, autoscaler | 1.1.0 |
| `mcp.client.name` | `gridctl.client.name` | root, downstream | 1.1.0 |
| `mcp.protocol.generation` | `gridctl.protocol.generation` | downstream | 1.1.0 |
| `mcp.tool.name`, `tool.name` | `gen_ai.tool.name` | root, downstream, routing | 1.1.0 |
| `output.format` | `gridctl.output.format` | format conversion | 1.1.0 |
| `mcp.autoscale.direction` | `gridctl.autoscale.direction` | autoscaler | 1.1.0 |
| `mcp.autoscale.reason` | `gridctl.autoscale.reason` | autoscaler | 1.1.0 |
| `gen_ai.usage.input_tokens` | `gridctl.usage.input_tokens` | downstream | 1.1.0 |
| `gen_ai.usage.output_tokens` | `gridctl.usage.output_tokens` | downstream | 1.1.0 |
| `gen_ai.usage.cache_read.input_tokens` | `gridctl.usage.cache_read_tokens` | downstream | 1.1.0 |
| `gen_ai.usage.cache_creation.input_tokens` | `gridctl.usage.cache_creation_tokens` | downstream | 1.1.0 |

`gen_ai.usage.*` is emitted only as that legacy alias during the window. After 1.1.0 the UI fallback for those four keys goes away with the alias. `mcp.session.id` is never emitted. Token counts are tokenizer estimates, not a model call, so they live under `gridctl.usage.*`.

Keys with no old name include `mcp.method.name`, `gen_ai.operation.name` (`execute_tool` on a tool call), `mcp.protocol.version` (the negotiated version), `error.type`, `rpc.response.status_code`, `server.address`, and `server.port`. `server.address` and `server.port` come from the endpoint URL for HTTP, SSE, OpenAPI, and A2A, and from `SSHHost` and `SSHPort` for SSH. An unset SSH port is recorded as 22. A URL with no explicit port omits `server.port`.

`network.transport` keeps its key. Only the values change, so an alias cannot preserve the old label. Dashboards that filter on `stdio` or `http` need to move to `pipe` or `tcp` now. `gridctl.transport` keeps the gridctl label.

| Old `network.transport` | New `network.transport` | `gridctl.transport` | Removal |
|-------------------------|-------------------------|---------------------|---------|
| `stdio` | `pipe` | `stdio` | 1.1.0 |
| `process` | `pipe` | `process` | 1.1.0 |
| `http` | `tcp` | `http` | 1.1.0 |
| `sse` | `tcp` | `sse` | 1.1.0 |
| `ssh` | `tcp` | `ssh` | 1.1.0 |
| (OpenAPI was `http`) | `tcp` | `openapi` | 1.1.0 |
| (A2A was `http`) | `tcp` | `a2a` | 1.1.0 |

`error.type` is a partial adoption of the convention, not a copy of the runs vocabulary. When the downstream returned a JSON-RPC error whose code classifies as an error, the value is the decimal string of that code (for example `-32603`). The codes `-32700`, `-32600`, `-32601`, `-32602`, and `-32002` do not set `error.type`; `rpc.response.status_code` still records them. A downstream result with `isError` is `tool_error`. Every other failure uses the runs disposition when one was recorded (`denied`, `routing_failed`, `transport_error`, `cancelled`, `timeout`, and so on), and the runs reason only when no failure disposition was set (`deadline_exceeded`, `context_canceled`). There is no `canceled` value. The runs constant is `cancelled`.

The root tool-call span is kind SERVER and is named `tools/call` before routing and `tools/call <tool>` after. The downstream span is kind CLIENT with the same name. Routing stays `mcp.routing`, format conversion stays `mcp.format_conversion`, and cold start stays `mcp.autoscale.cold_start`. Span names are not a versioned contract. `GET /api/traces` returns that root name in `operation`. The Traces list still shows the tool name, with the span name only as a fallback. `gridctl traces` detail prints the span name, and its transport sub-line prints `network.transport` (`pipe` or `tcp` on a new trace). A sensitive execution (an A2A server, or a server flagged for sensitive calls) sets `gen_ai.tool.name` to the operation category (`skill`, `send`, `task_get`, or `task_cancel`) on every span and metric row, and omits `gridctl.client.name`. The resolved tool name is not exported.

### Trace context

Upstream extraction prefers `params._meta`. A valid `traceparent` there is the parent of the root span. When `_meta` has none, the HTTP `traceparent` header is the parent. When both are present, `_meta` is the parent and the header context is a span link on that request's root only. A code-mode inner call does not inherit the link. Handshake sessions and the stateless path both extract.

Every downstream transport injects `traceparent`, `tracestate`, and `baggage`. Stdio and process inject them in `params._meta`. Streamable HTTP and SSE inject them in both the request headers and `params._meta`, composed with the stateless-era keys when that era is in use. OpenAPI and A2A inject headers only, because the far end is not MCP.

Baggage is forwarded verbatim and capped at 8192 bytes. A larger value is dropped whole, and the drop is counted in a debug log that records the byte count, not the value. Baggage is never written to span attributes, logs, run records, or the trace buffer, and it does not affect routing.

`gateway.tracing` and `gateway.metrics.otlp` are read at startup. Changing either requires a restart. An unreachable collector does not block startup or tool calls.

### Metrics export

`gateway.metrics.otlp` exports HTTP/protobuf metrics. There is no gRPC protocol and no Prometheus scrape endpoint. The resource is the same three attributes as traces: `service.name` (the stack name, or `gridctl-gateway`), `service.version`, and `service.instance.id`. Attribute values pass through the same redaction as trace attributes. A sensitive execution uses the operation category for `gen_ai.tool.name` and omits `gridctl.client.name`.

| Instrument | Unit | Attributes |
|------------|------|------------|
| `mcp.server.operation.duration` | `s` | `mcp.method.name`, `gen_ai.operation.name`, `gen_ai.tool.name`, `gridctl.server.name`, `gridctl.client.name`, `error.type` |
| `mcp.client.operation.duration` | `s` | `mcp.method.name`, `gen_ai.operation.name`, `gen_ai.tool.name`, `gridctl.server.name`, `gridctl.transport`, `network.transport`, `server.address`, `server.port`, `error.type` |
| `gridctl.gateway.tool.calls` | `{call}` | `gridctl.server.name`, `gen_ai.tool.name`, `gridctl.client.name`, `gridctl.dispatch.disposition`, `gridctl.dispatch.stage`, `gridctl.dispatch.reason` |
| `gridctl.gateway.tokens` | `{token}` | `gridctl.usage.direction` (`input`, `output`, `cache_read`, or `cache_creation`), `gridctl.server.name`, `gen_ai.tool.name`, `gridctl.client.name` |
| `gridctl.gateway.replicas` | `{replica}` | `gridctl.server.name`, `gridctl.replica.state` (`healthy`, `unhealthy`, or `restarting`). The value is the count of replicas in that state |
| `gridctl.runs.dropped` | `{record}` | `gridctl.runs.drop_reason` (omitted when runs are disabled) |
| `gridctl.otlp.export.failures` | `{failure}` | `gridctl.otlp.signal` (`traces`, `metrics`, or `unknown`) |

Duration histograms use buckets 0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 30, 60, 120, and 300 seconds. `mcp.client.operation.duration` is recorded only when the downstream call was made. A denial after a replica was selected, including a schema-pin block, does not add a sample. Denials, rate limits, and policy rejections are rows of `gridctl.gateway.tool.calls`. Export failures log at warn with the endpoint host and signal, at most once per minute per distinct message, and increment the failure counter. An error that does not carry the `trace export:` or `metric export:` prefix is counted as `unknown`. Shutdown flushes both signals after the HTTP server stops and before the log router closes.

## Metrics persistence

Opt-in metrics persistence is unchanged: with `telemetry.persist.metrics: true`, the gateway appends diff snapshots to `~/.gridctl/telemetry/<stack>/<server>/metrics.jsonl` and restores cumulative counters from disk on startup. Files written while the removed cost layer was active carry extra keys (`cost_diff`, `cost_total`, `model_cost`); the decoder is non-strict and ignores them, so old files load cleanly and lose nothing but the dollar figures.

## The dollar-cost layer was removed

Earlier releases priced tool calls in USD against an embedded LiteLLM rate snapshot, with model attribution declared in `stack.yaml` and dollar budget caps under `limits:`. That layer has been removed. The gateway sits below the LLM client: it never sees the prompt, the client's actual model choice, or the provider invoice, so every dollar figure was an estimate of a fraction of a related quantity. Cost attribution belongs at the LLM proxy layer, where real requests and models are visible; gridctl reports observed calls and token counts or estimates.

The removed configuration fields (`gateway.default_model`, per-server `model:`, top-level `client_models:`, and `limits.budgets`) are unknown keys. `gridctl validate`, apply, and reload reject them and name the path, key, and line. Delete the keys. They no longer load. Leftover budget ledger files under the gridctl state directory (`~/.gridctl/limits/`) are orphaned and harmless, and can be deleted at any time.
