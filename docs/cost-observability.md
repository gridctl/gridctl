# Cost Observability

Gridctl prices every observed tool call against an embedded snapshot of LiteLLM's [`model_prices_and_context_window.json`](https://github.com/BerriAI/litellm/blob/main/model_prices_and_context_window.json). The cost layer lives in `pkg/pricing` (rate table + normalization) and `pkg/metrics` (parallel cost counters alongside the existing token counters). Cost surfaces in the REST API (`/api/status`, `/api/metrics/cost`), the Web UI Metrics tab, the `gridctl optimize` CLI, opt-in metrics persistence, and the `gen_ai.cost.usd` span attribute on tool-call traces.

## Model attribution (required for cost data)

Pricing a call requires knowing which model the tokens are billed against. The gateway sits below the LLM client and cannot observe the client's model choice — and the MCP protocol never carries it — so attribution is declared in `stack.yaml`. Resolution per tool call, highest precedence first:

| Precedence | Source | Declared where |
|---|---|---|
| 1 | Call-level usage metadata | Reported by the MCP server per call (no standardized wire shape yet; inert today) |
| 2 | Calling client's model | Top-level `client_models:` map |
| 3 | Target server's model | `model:` on the server entry |
| 4 | Stack-wide default | `gateway.default_model` |

The client tier exists because a model is a property of the calling client's session, not the tool server: when Claude Code on Opus and Gemini CLI on Gemini share the same servers, only a per-client declaration prices both correctly. `client_models:` is purely a pricing declaration — it never creates or implies access restrictions (that is the separate `clients:` block).

```yaml
gateway:
  default_model: claude-haiku-4-5   # floor for anything not declared below

client_models:
  claude-code: claude-opus-4-7      # this client's calls price as Opus
  gemini-cli: gemini-2.5-pro        # regardless of which server they hit

mcp-servers:
  - name: jira
    image: mcp/atlassian:latest
    model: claude-opus-4-7          # prices undeclared clients' calls to this server
```

Without any attribution, tokens and latency record normally but cost stays zero, and the dashboard's cost card shows a configuration hint instead of a number. Anonymous calls (no client identity on the session) skip the client tier and resolve via the server tier. Edits to any of these fields hot-reload through the file watcher without restarting any server; subsequent calls price against the updated mapping.

### Editing attribution in the UI

All three declared tiers are editable in the web UI without touching `stack.yaml`. The shared model picker is a searchable, provider-grouped combobox over the known-models list (`GET /api/pricing/models`); free-text IDs outside the list are accepted (best-effort pricing, soft warning, $0).

- **Metrics tab** (and the detached `/metrics` window): the Top Clients panel shows each declared client's model with `· client` provenance and edits inline; the per-server table shows `· server` pills or muted `default: <id>` inheritance and edits inline. Clients without a declaration aggregate whatever server/default rates their calls hit, so no single model is shown for them.
- **Pricing models manager**: a slide-over listing all three tiers in precedence order (clients, servers, gateway default). Opened from the Metrics toolbar's `$` button, the sidebar inspector's Pricing section, or the command palette ("Edit pricing models").
- **Creation wizard**: `gateway.default_model` (Pricing section) and per-server `model:` (Advanced section) can be set before the first apply.

Writes go through dedicated pricing endpoints (`PUT /api/clients/{slug}/model`, `PUT /api/mcp-servers/{name}/model`, `PUT /api/gateway/default-model`) that patch only the relevant key, preserve comments and ordering, and never create or touch a `clients:` access block. A concurrent external edit surfaces as a 409 conflict so the UI can re-fetch; successful saves hot-reload immediately.

Two limitations to keep expectations honest. A declared client model is a session-level default: the gateway cannot see a mid-session model switch (e.g. `/model` in Claude Code), so calls keep pricing at the declared model until the declaration changes. And validation is soft by design: model IDs unknown to the pricing snapshot, or `client_models` keys that are not normalized client IDs (`gridctl validate` warns and suggests the canonical form), produce warnings — never errors — and price as zero.

Cost figures are estimates — tokenizer-approximated counts multiplied by published list rates. They are built for comparing servers and clients, spotting waste, and trending over time, not for reconciling invoices.

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

- `cache_read_input_token_cost` - applied to `CallUsage.CacheReadTokens`.
- `cache_creation_input_token_cost` - applied to `CallUsage.CacheCreationTokens`.

Cache fields default to zero. When a tool result reports them via `_meta` (the MCP-spec extension point for usage metadata), the observer prices the components separately and records the breakdown alongside the per-call total.

## Swapping the pricing source

`pricing.Source` is a small interface (`Lookup`, `Name`). The default is `pricing.NewLiteLLMSource()` backed by the embedded JSON. Tests and alternate providers can install their own:

```go
pricing.SetSource(myFakeSource)
```

The package-level `Lookup` and `Calculate` functions read through the active source via an atomic pointer, so swaps are safe under concurrent readers.

## `gridctl optimize`

`gridctl optimize` analyzes the running gateway and prints actionable cost-reduction findings. The package ships **five heuristics** in `pkg/optimize`:

- **`unused_server`** - A server is registered in the stack but no tool calls have been observed from it. Removing it (or excluding all its tools) frees the JSON Schema overhead the server adds to every prompt. The reported weekly USD impact uses the server's observed per-token rate when available, otherwise falls back to a conservative default; the formula always derives from measured data.
- **`unused_tool`** - A server *is* receiving traffic but a specific tool it exposes has not been called inside the lookback window (default 7 days) and is not already excluded via the server's `tools:` filter. Remediation suggests adding the tool to the exclusion list.
- **`schema_overhead`** - A server's tool-list payload (the schema sent on every initialize / tools/list) is large relative to the value the server's tools have produced. The detector measures schema bytes off the live gateway tool list, applies a chars-per-token heuristic, and fires when the ratio of observed output tokens to schema tokens falls below the floor. Remediation prunes unused tools via the server's `tools:` filter.
- **`format_savings_shortfall`** - A server emits raw JSON (no `output_format`) while other servers in the same session have already demonstrated meaningful savings from converting to TOON or CSV. Projected impact = baseline savings rate × the candidate's measured output tokens × its measured per-token cost. Remediation adds `output_format: toon` (or `csv`) to the server entry.
- **`expensive_model_on_cheap_task`** *(informational)* - An Opus-tier model dominates traffic on a server whose calls average tiny prompts and tiny results. The detector either reads the rate directly when the gateway resolves a per-server model, or infers the rate from observed cost ÷ tokens. Severity is `info` because model selection lives client-side; gridctl can suggest the migration but cannot enforce it.

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

The data shape (`OptimizeReport`, `Finding`, `Stats`) is additive: every new heuristic surfaces through the existing `/api/optimize` endpoint, `gridctl optimize` CLI, and Sidebar Optimize panel without touching call sites.

### Limitations

- The pricing snapshot is best-effort, not a billing source of truth. Unknown models log a single `WARN` and are treated as zero-cost; their findings will under-report impact.
- The `unused_server` impact uses an estimated upper-bound on schema overhead and weekly prompt count when no usage signal is available. The number is conservative - a busy team easily exceeds it - but stays anchored to measured per-token cost when present.
- Per-tool tracking starts when the observer's tool-name path deploys; gateways running an older binary will emit no `unused_tool` or `expensive_model_on_cheap_task` findings until restarted.
- `schema_overhead` reads schema bytes off the live tool list at request time, applies a chars-per-token heuristic (~4), and projects an upper-bound weekly impact assuming the schema rides every prompt. Real savings depend on the consuming client's prompt cadence; the projection is conservative.
- `format_savings_shortfall` only fires when the session has already demonstrated meaningful savings from another server's `output_format: toon|csv` conversion. Until you've converted at least one server, the heuristic stays silent rather than guessing.
- `expensive_model_on_cheap_task` is informational only because gridctl cannot override a client's model choice. The finding helps you reason about which servers would benefit from a cheaper-model code path on the client side.
