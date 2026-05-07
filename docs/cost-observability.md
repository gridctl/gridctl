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

## What is not yet exposed

PR 1 ships the cost foundation only. The following surfaces land in subsequent PRs:

- `GET /api/metrics/cost` time-series endpoint.
- `cost` field on `/api/status`.
- Per-client attribution and OTel GenAI span attributes (`gen_ai.cost.usd`, `gen_ai.usage.cache_read.input_tokens`, etc.).
- Cost KPI card and chart in the Web UI Metrics tab.
- `gridctl optimize` CLI with cost-driven findings.

Until those land, cost data is recorded but not user-visible. The `pkg/metrics` accumulator's `CostSnapshot()` and `QueryCost()` methods return the data for in-process consumers.
