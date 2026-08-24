# Model Routing Policy

`gridctl models` turns model routing for a self-hosted LiteLLM proxy into managed, versioned configuration: one policy document declares which backend serves which complexity tier, and gridctl projects it into the files LiteLLM and your coding clients actually read, with the same ownership, drift detection, and backup guarantees as skill and context projection.

Status: Experimental. The LiteLLM auto-router config surface is young and still moving upstream; gridctl types only its stable core and validates the rendered output by booting a pinned LiteLLM release in CI.

## What gridctl does and does not do

gridctl is a control plane here, never a data plane. It compiles and synchronizes configuration; OpenCode talks to LiteLLM directly for completions, LiteLLM picks the backend per request, and if gridctl is down inference is unaffected. There is no `/v1` endpoint in gridctl, no header stamping, and no model selection logic. This feature is also unrelated to two similarly named surfaces: the gateway's tool router (which routes MCP tool calls between servers) and the `model_preferences:` stack block (which hints a preferred model per skill; the models policy configures which backends serve which complexity tier in your proxy).

## The policy document

One document per gridctl home, at `~/.gridctl/models/policy.yaml`. Scaffold it with `gridctl models init --template` (three commented starters: `local-only`, `hybrid`, `cloud-primary`) or, better, from the LiteLLM config you already have:

```
gridctl models init --from-litellm ~/.litellm/config.yaml
gridctl models edit
gridctl models sync
# restart LiteLLM (sync prints the exact hint)
gridctl models ack-restart
gridctl models status
```

The schema:

```yaml
name: default
kind: models
description: Local for routine tiers, cloud for complex work

router:
  entry_model: smart-router     # the model name clients select
  default_tier: MEDIUM          # where unclassifiable requests land

backends:                       # references to model_name values in YOUR
  - qwen-local                  # LiteLLM config's model_list
  - claude-sonnet

tiers:                          # all four required, scalar values only
  SIMPLE: qwen-local
  MEDIUM: qwen-local
  COMPLEX: claude-sonnet
  REASONING: claude-sonnet

weights:                        # optional dimension_weights; coding agents
  tokenCount: 0.0               # carry huge system prompts, so ignore raw
  reasoningMarkers: 0.40        # token count
  technicalTerms: 0.25
  codePresence: 0.20
  simpleIndicators: 0.10
  multiStepPatterns: 0.05

passthrough:                    # auto-router keys gridctl does not model
  session_affinity: true        # ride through verbatim; typed keys win,
                                # and tiers may not be set here

clients:
  opencode:
    provider_id: litellm
    base_url: http://localhost:4000/v1
    api_key_env: LITELLM_KEY    # an env var NAME, never a literal key
    schema: detect              # v1 | v2 | detect (see below)

targets:
  litellm:
    config_path: ~/.litellm/config.yaml
    # fragment_path defaults to gridctl-models.yaml next to config_path
```

Backends are references, not definitions. The rendered fragment never contains `model_list` inventory: LiteLLM's `include:` directive extends `model_list` across files, so a backend re-emitted by gridctl would silently load-balance against the user's original entry with the same name. Your models stay declared exactly once, in your own config; validation warns when a tier references a name the parent does not declare.

## What sync writes

`gridctl models sync` touches three things, each with its own ownership mechanism and a timestamped backup:

1. **The fragment** (`gridctl-models.yaml`, next to your LiteLLM config): a wholly gridctl-owned file carrying exactly one `model_list` entry, the router (`auto_router/complexity_router` with `complexity_router_config`, and `complexity_router_default_model` mapped from `router.default_tier`). Regenerated wholesale on every sync; a hand edit is detected as drift and skipped with guidance.
2. **The include line** in your LiteLLM config: a single-line text edit, never a parse-and-rewrite, so your comments and formatting survive byte-for-byte. gridctl handles every `include:` shape (absent, block list, flow list, and a scalar, which is promoted to a list and restored on unsync). The written path is relative to your config's directory, matching how LiteLLM resolves includes.
3. **The OpenCode provider stanza**: an RFC 6902 patch on `opencode.json` that adds or replaces only the owned `provider.<id>` (or v2 `providers.<id>`) subtree. Everything else, comments included, survives byte-for-byte. gridctl never writes the top-level `model` key: you change that constantly through OpenCode's own picker, and owning it would turn every switch into drift. After a sync, select the `litellm/smart-router` model in OpenCode (status prints an info note when your default points elsewhere).

Keys never appear in any rendered output. The fragment references LiteLLM keys as `os.environ/...` only if your own config does (gridctl emits none); the OpenCode stanza carries `{env:LITELLM_KEY}` (v1) or an `env` list (v2), and validation hard-fails on anything that looks like a literal credential in the policy.

### OpenCode config generations

OpenCode's config schema renamed `provider` to `providers`, `npm` to `package`, and `options` to `settings` across generations, and newer versions drop custom providers without an `env` list. The renderer supports both shapes; `schema: detect` (the default) picks by which key your config file already uses, defaulting to v1 for an empty file. Pin `v1` or `v2` explicitly if detection guesses wrong.

## The restart contract

Writing the fragment does not change what the running proxy serves: LiteLLM reads its config only at startup, and there is no reload path (`systemctl reload` or `--reload` are not enough). gridctl is honest about this instead of pretending: sync output says the policy is not live yet and prints a restart command, and `status` annotates the fragment `restart-pending` until you run `gridctl models ack-restart` after actually restarting the proxy. The annotation is latched (a second sync never clears it) and never affects exit codes; gridctl never probes the process to guess.

If LiteLLM fails to start after a sync, remove the include line from your config (or run `gridctl models unsync`) and restart; the previous fragment content is in the timestamped backup next to it.

## Drift, adopt, force, and unsync

The same contract as every gridctl projection. `status` reports `in-sync`, `stale` (the policy changed since the last sync), `drifted` (the target was hand-edited), `target-missing`, and `never-synced`, with exit codes `0`/`1`/`2`; `sync --check` is the CI form. A drifted target is never silently overwritten: `gridctl models adopt` accepts the hand edit as the new owned state, `sync --force` overwrites it. A file at the fragment path that gridctl never created is foreign and refused without `--force`; a provider entry gridctl never wrote is likewise refused. `unsync` removes only what the lockfile attests gridctl wrote, restoring your files byte-for-byte outside the owned line and subtree, and never touches the policy document. `gridctl reset` covers all of it.

## Topology notes

- The fragment defaults to sitting next to your LiteLLM config so the include stays a bare relative path and the pair moves together.
- With LiteLLM on another machine, run sync where the config lives (a synced or mounted directory works), or pipe `gridctl models render --target litellm` over ssh yourself and manage the include line once by hand. gridctl does not push over the network.
- `api_base` values inside your LiteLLM `model_list` are evaluated on the LiteLLM host: `127.0.0.1` there means the proxy's own machine, which is exactly right when the model server runs next to it.
- LiteLLM's `include:` extends list keys but silently replaces top-level maps, which is why validation refuses `router_settings`, `fallbacks`, `general_settings`, and `litellm_settings` anywhere near the policy: those belong in your primary config, where they are yours alone.

## Command reference

See the [CLI reference](cli-reference.md#model-routing-models) for the full verb table (`init`, `edit`, `validate`, `render`, `sync`, `status`, `ack-restart`, `adopt`, `unsync`), flags, and exit codes.

## Web UI

The web UI's Connections workspace carries a Model routing dialog (the header action, or the Model routing section in OpenCode's detail pane) mirroring the reconcile half of the CLI: per-target status, the policy's routing summary, validation findings, drift review with diffs, whole-policy sync, adopt, and the restart acknowledgment. The REST endpoints behind it are documented in the [API reference](api-reference.md#model-routing) and inherit the Experimental tier. The policy document itself stays CLI-edited (`gridctl models init`, `gridctl models edit`); there is no browser editor by design, so the file remains the single source of truth.
