# Usage Observability

Gridctl measures the token traffic that flows through the gateway. Every tool call's arguments and result are counted with a real tokenizer, and the counts accumulate per server, per replica, per client, and per (server, tool) pair, alongside cumulative call counts and last-used timestamps. Because the gateway also performs output format conversion, it measures the savings directly: each converted result is counted before and after conversion, so the reported format savings come from the gateway's own observed traffic, not a projection.

## What the gateway measures

Input tokens are counted on tool-call arguments and output tokens on tool results, attributed to the server that handled the call, the replica that served it, the calling client (from the session's MCP `clientInfo`), and the individual tool. Call counts and last-called timestamps are kept per (server, tool) pair, and registry skills get the same treatment for `prompts/get` usage. When `output_format: toon` or `csv` is active, the format-savings tally records original tokens, formatted tokens, and the saved difference.

## Tokenizers

Token counting defaults to an embedded `cl100k_base` BPE tokenizer (`gateway.tokenizer: embedded`). Claude's vocabulary is unpublished, so `cl100k_base` counts are an approximation for Claude models, typically within 10–15% for English and code content. That is accurate enough for the comparisons this data exists for: ranking servers, spotting waste, and trending over time.

For exact counts, set `gateway.tokenizer: api` to route counting through Anthropic's `count_tokens` endpoint, with the key from `gateway.tokenizer_api_key` or the `ANTHROPIC_API_KEY` environment variable. On any API error the counter falls back to the embedded tokenizer rather than dropping the measurement.

## Where usage surfaces

The **Metrics workspace** in the web UI charts token throughput over time and breaks totals down by server, replica, client, and tool, alongside call counts, format savings, and rate-limit state.

`GET /api/metrics/tokens` returns the token time series (aggregate and per-server) over a selectable range, and `GET /api/tools/usage` returns per-tool call counts, last-called timestamps, and token totals. `GET /api/status` carries the session's aggregate token usage and format savings. See the [REST API Reference](api-reference.md).

`gridctl optimize` analyzes gateway-observed data and prints findings with a projected weekly token impact: unused servers, unused tools, schema overhead, and format-savings shortfalls, each with a paste-ready YAML remediation. Impact figures (`impact_tokens_per_week` in `--format json`) are tokens per week, projected assuming roughly 500 prompts per week, since a tool schema rides every prompt the client sends. `--min-impact` filters findings below a token threshold; `info` findings are always retained.

Rate limits (`limits.rate_limits` in `stack.yaml`) cap calls per minute per client, server, or tool, enforced at dispatch with no token accounting required. `gridctl limits` and `GET /api/limits` show every configured limit and its state.

## Metrics persistence

Opt-in metrics persistence is unchanged: with `telemetry.persist.metrics: true`, the gateway appends diff snapshots to `~/.gridctl/telemetry/<stack>/<server>/metrics.jsonl` and restores cumulative counters from disk on startup. Files written while the removed cost layer was active carry extra keys (`cost_diff`, `cost_total`, `model_cost`); the decoder is non-strict and ignores them, so old files load cleanly and lose nothing but the dollar figures.

## The dollar-cost layer was removed

Earlier releases priced tool calls in USD against an embedded LiteLLM rate snapshot, with model attribution declared in `stack.yaml` and dollar budget caps under `limits:`. That layer has been removed. The gateway sits below the LLM client: it never sees the prompt, the client's actual model choice, or the provider invoice, so every dollar figure was an estimate of a fraction of a related quantity. Cost attribution belongs at the LLM proxy layer, where real requests and models are visible; gridctl now reports what it can measure exactly, which is tokens and calls.

The removed configuration fields (`gateway.default_model`, per-server `model:`, top-level `client_models:`, and `limits.budgets`) are ignored by the non-strict YAML loader, so an existing stack that still declares them loads without error; the fields simply have no effect. Leftover budget ledger files under the gridctl state directory (`~/.gridctl/limits/`) are orphaned and harmless, and can be deleted at any time.
