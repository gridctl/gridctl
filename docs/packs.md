# Packs

A pack is a git repository carrying a `gridctl-pack.yaml` manifest at its root: a versioned selector over the repo's skills, agents, rule fragments, and gateway wiring, so one import configures a whole team setup. Packs are a thin composition layer — `pack add` runs the same origin pipeline as `gridctl skill add` for skills and agents (security scan, `--trust` gate, drift-safe updates); rule fragments get the same blocking scan and `--trust` gate, and `pack add` refreshes a rule whose content changed upstream, provided you have not edited it locally. A rule you have edited is reported as locally modified and left alone; take the pack's version with `gridctl ctx rm <name>` followed by `pack add`, which discards your copy. Rules installed before gridctl recorded per-rule provenance are treated as locally modified until their next `pack add` records it. `skill update` still does not cover rules; `pack add` is their update path. `pack apply` drives the same projection engines as `gridctl skill project sync` (see the [Skills guide](skills.md)), `gridctl ctx sync` (see [Global Context Sync](global-context.md)), and `gridctl project sync --kind wiring`, scoped to the pack. Every projection a pack applies is tagged with the pack name in `~/.gridctl/project.lock.yaml`, which is what makes `pack status` and cascade removal exact. Per-command flags and exit codes are in the [CLI reference](cli-reference.md#packs).

## Manifest

```yaml
# gridctl-pack.yaml (repo root)
apiVersion: gridctl.dev/v1
kind: Pack
name: network-eng            # required: lowercase letters, digits, hyphens
version: 1.0.0               # optional metadata (plugin.json-aligned)
description: Network engineering team setup
author:
  name: Acme Networks
  url: https://example.com

skills: [incident-triage, bgp-lab]   # names from this repo; empty = all discovered
agents: [neteng-reviewer]            # same
wiring: true                         # ensure the gateway entry in client configs
clients: []                          # wiring scope; empty = all detected clients
rules: [team-style]                  # context fragments from rules/*.md or fragments/*.md (opt-in; empty = none)
```

`gridctl.dev/v1alpha1` is the pre-1.0 spelling of the same schema and stays accepted indefinitely, so packs authored before the graduation import unchanged; write `gridctl.dev/v1` in new manifests.

Skills follow the `SKILL.md` convention and agents the `agents/*.md` convention, exactly as plain skill repos do — a pack repo is a skill repo plus a manifest. Rule fragments live under `rules/*.md` or `fragments/*.md` (filename base is the fragment name). Names the manifest selects but the repo does not ship are reported as `unresolved` (exit 1) and kept in the pack record so status stays honest. Unlike skills/agents, an empty `rules:` list means **none** (rules are opt-in). A rule whose name collides with a local fragment of different content is skipped, never overwritten; identical content installs idempotently. A first rule install that activates fragments mode migrates AGENTS.md with an explicit printed message, exactly like `ctx add`.

Field names follow the Claude Code plugin.json family where the semantics match, so a pack maps onto that ecosystem rather than fighting it. The word "bundle" is deliberately avoided: the MCP ecosystem uses it for `.mcpb`, a single-server archive format.

## Verbs

| Command | Purpose |
|---|---|
| `gridctl pack add <repo-url>` | Clone, read the manifest, and import exactly its selection into the registry (`--ref`, `--trust`, `--dry-run`, `--format json`). Registry-side only. Exit `0` clean, `1` partial (unresolved or skipped), `2` infrastructure. |
| `gridctl pack apply <name>` | Project the pack: skills and agents through the projection engines, rule fragments through `ctx` (pack-tagged), and (when `wiring: true`) the gateway entry through the wiring ownership manager, scoped to `clients:`. Additive, never transactional: each resource succeeds or skips independently (`Applied N/M` summary), drifted resources skip with an adopt/`--force` hint, a resource tagged by a different pack is refused, and wiring skips with a hint when no gateway is running. `--force`, `--dry-run`, `--clients`, `--format json`. |
| `gridctl pack status [name]` | Per-resource state in the shared vocabulary (in-sync, stale, drifted, target-missing, foreign, missing) plus `unresolved` rows. Exit `0`/`1`/`2`. |
| `gridctl pack remove <name>` | Cascade removal in dependency order: projections unsynced from client trees (rule fragment projections by pack tag only), wiring records removed through the ownership manager (entries gridctl did not record are never deleted), then the pack's registry skills, agents, and installed fragments, then the pack record. Drifted projections are kept with a remediation hint unless `--force`; a partial removal trims the pack record to what stayed. `--dry-run`, `--format json`. |

## Interplay with the standalone verbs

Packs add bookkeeping, not a second write path. `gridctl skill project sync`, `gridctl skill update`, and `gridctl project sync --kind wiring` keep working on pack-managed resources; a plain re-sync never strips the pack tag. One pack owns a resource at a time: applying a pack over a resource tagged by another pack refuses that resource until one manifest gives it up.

## REST and web UI

The full verb set is available over HTTP: `GET /api/packs` (list), `GET /api/packs/{name}` (detail with per-resource state rows), `POST /api/packs` (add from git, behind the same blocking security scan; also the update path against an already-imported origin), `POST /api/packs/preview` (read-only manifest resolution), `POST /api/packs/{name}/apply` (with `clients`, `force`, and `dry_run`, matching the CLI flags), and `DELETE /api/packs/{name}` (with a dry-run cascade preview). See the [API reference](api-reference.md#packs). The web UI's Library workspace surfaces installed packs in its Packs segment.

Status rows for rules report per-client projection state once a pack is applied (drift and staleness per fragment-file projection; a compiled client's whole-document state stays in `gridctl ctx status`), with a store-presence row for a rule that was imported but never projected.

## What packs deliberately do not have

No enable/disable state (imported and projected are the only states), no inter-pack dependencies, no interactive configuration prompts (secrets flow through the existing `${var:KEY}` vault mechanism), and no marketplace indirection (`pack add` points at a git repo you chose, with the same trust gate as `skill add`).

See `examples/portable-pack/` for a complete pack repo layout.
