# Usage Observability

Gridctl measures the token traffic that flows through the gateway. Ordinary successful dispatches count arguments and results with the configured tokenizer, accumulating per server, per replica, per client, and per (server, tool) pair, alongside cumulative call counts and last-used timestamps. Because the gateway also performs output format conversion, it measures the savings directly: each converted result is counted before and after conversion, so the reported format savings come from the gateway's own observed traffic, not a projection.

## What the gateway measures

Input tokens are counted on tool-call arguments and output tokens on tool results, attributed to the server that handled the call, the replica that served it, the calling client (from the session's MCP `clientInfo`), and the individual tool. Call counts and last-called timestamps are kept per (server, tool) pair, and registry skills get the same treatment for `prompts/get` usage. When `output_format: toon` or `csv` is active, the format-savings tally records original tokens, formatted tokens, and the saved difference.

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

The allowlist is generated IDs, timestamps, total dispatch duration, bounded target names, disposition/stage/reason, optional replica and sampled trace IDs, and optional caller-declared labels. Argument and result values, hashes, code, raw errors, tokens, headers, URLs, and host paths are excluded. Names and labels may still be sensitive. Labels are not authenticated principals.

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
These Unreleased diagnostic-output changes require maintainer-owned major-release
scheduling under Article VIII. See [diagnostic troubleshooting](troubleshooting.md#code-mode-error-details-are-missing-from-logs).

## Metrics persistence

Opt-in metrics persistence is unchanged: with `telemetry.persist.metrics: true`, the gateway appends diff snapshots to `~/.gridctl/telemetry/<stack>/<server>/metrics.jsonl` and restores cumulative counters from disk on startup. Files written while the removed cost layer was active carry extra keys (`cost_diff`, `cost_total`, `model_cost`); the decoder is non-strict and ignores them, so old files load cleanly and lose nothing but the dollar figures.

## The dollar-cost layer was removed

Earlier releases priced tool calls in USD against an embedded LiteLLM rate snapshot, with model attribution declared in `stack.yaml` and dollar budget caps under `limits:`. That layer has been removed. The gateway sits below the LLM client: it never sees the prompt, the client's actual model choice, or the provider invoice, so every dollar figure was an estimate of a fraction of a related quantity. Cost attribution belongs at the LLM proxy layer, where real requests and models are visible; gridctl reports observed calls and token counts or estimates.

The removed configuration fields (`gateway.default_model`, per-server `model:`, top-level `client_models:`, and `limits.budgets`) are ignored by the non-strict YAML loader, so an existing stack that still declares them loads without error; the fields simply have no effect. Leftover budget ledger files under the gridctl state directory (`~/.gridctl/limits/`) are orphaned and harmless, and can be deleted at any time.
