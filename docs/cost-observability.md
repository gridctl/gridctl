# Cost Observability

Gridctl prices every observed tool call against an embedded snapshot of LiteLLM's [`model_prices_and_context_window.json`](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json). The cost layer lives in `pkg/pricing` (rate table + normalization) and `pkg/metrics` (parallel cost counters alongside the existing token counters). Surfacing cost in the REST API, Web UI, and `gridctl optimize` CLI ships in follow-up PRs; this document describes the foundation that landed first.

## Refreshing pricing data

The pricing snapshot is embedded at build time via `//go:embed pkg/pricing/data/model_prices.json`. Refresh it when providers adjust rates or add new models:

```bash
make update-pricing
```

The target downloads the latest LiteLLM file. If the download fails, the existing snapshot is preserved (non-fatal). A weekly cadence is recommended; faster than that is rarely necessary because LiteLLM batches rate updates.

## Model-ID normalization

LiteLLM keys vary between provider-prefixed (`anthropic/claude-opus-4-7`), bare (`claude-opus-4-7`), and dated (`claude-opus-4-7-20260416`) forms. Clients emit any of these. `pricing.Lookup` normalizes incoming IDs in this order:

1. The exact form, lower-cased and provider-prefix stripped.
2. The same form with any trailing `-YYYYMMDD` date suffix removed (so `claude-opus-4-7-20260416` falls back to `claude-opus-4-7`).
3. A small alias table for IDs that diverge by more than the prefix/date heuristics handle (e.g. `claude-opus-4-latest`, `gpt-4-turbo`).

Unknown models log a single `WARN` per ID and are treated as zero-cost. The cost path is best-effort: pricing data is not a billing source of truth, and the gateway never fabricates rates for unrecognized models.

## Cache-read vs. input pricing

Anthropic's prompt-cache rates are roughly 10% (cache-read) and 125% (cache-write) of input rates. Conflating cache-read tokens with input tokens overstates cost by an order of magnitude on cache-heavy workloads. Gridctl prices each component separately:

- `cache_read_input_token_cost` — applied to `CallUsage.CacheReadTokens`.
- `cache_creation_input_token_cost` — applied to `CallUsage.CacheCreationTokens`.

Cache fields default to zero. When a tool result reports them via `_meta` (the MCP-spec extension point for usage metadata), the observer prices the components separately and records the breakdown alongside the per-call total.

## Swapping the pricing source

`pricing.Source` is a small interface (`Lookup`, `Name`). The default is `pricing.NewLiteLLMSource()` backed by the embedded JSON. Tests and alternate providers can install their own:

```go
pricing.SetSource(myFakeSource)
```

The package-level `Lookup` and `Calculate` functions read through the active source via an atomic pointer, so swaps are safe under concurrent readers.

## `gridctl optimize`

`gridctl optimize` analyzes the running gateway and prints actionable cost-reduction findings. PR 4 ships **two heuristics** in `pkg/optimize`:

- **`unused_server`** — A server is registered in the stack but no tool calls have been observed from it. Removing it (or excluding all its tools) frees the JSON Schema overhead the server adds to every prompt. The reported weekly USD impact uses the server's observed per-token rate when available, otherwise falls back to a conservative default; the formula always derives from measured data.
- **`unused_tool`** — A server *is* receiving traffic but a specific tool it exposes has not been called inside the lookback window (default 7 days) and is not already excluded via the server's `tools:` filter. Remediation suggests adding the tool to the exclusion list.

A single `info` finding ("need more data") is emitted on a gateway that has been running for less than the minimum observation window (default 24h) so reports never over-fire on freshly applied stacks. Findings are sorted by severity then weekly impact descending so the most actionable item renders first.

The CLI calls `GET /api/optimize` and renders the same `OptimizeReport` either as a styled table (default) or as JSON (`--format json`). Exit codes follow the standard CLI contract (`0`/`1`/`2`).

### What `gridctl optimize` does and does not see

`gridctl optimize` reads only gateway-observed data:

- The list of MCP servers and tools the gateway has registered (`gateway.Status()`).
- Per-server token + cost totals from the `pkg/metrics` accumulator.
- Per-(server, tool) call counts and last-called timestamps captured by the observer's tool-name attribution (PR 4 added the per-tool counter; gateways without per-tool data skip the `unused_tool` heuristic).

It deliberately does **not** read:

- Client-side session files (`~/.claude/projects/`, `~/.codex/sessions/`, Cursor's `state.vscdb`, etc.).
- Anything outside `~/.gridctl/` and the running gateway's process state.

PR 5 will add the `schema_overhead`, `format_savings_shortfall`, and `expensive_model_on_cheap_task` heuristics. The data shape (`OptimizeReport`, `Finding`) is additive, so future heuristics layer on without breaking call sites.

### Limitations

- The pricing snapshot is best-effort, not a billing source of truth. Unknown models log a single `WARN` and are treated as zero-cost; their findings will under-report impact.
- The `unused_server` impact uses an estimated upper-bound on schema overhead and weekly prompt count when no usage signal is available. The number is conservative — a busy team easily exceeds it — but stays anchored to measured per-token cost when present.
- Per-tool tracking starts when PR 4's observer change deploys; gateways running an older binary will emit no `unused_tool` findings until restarted.
