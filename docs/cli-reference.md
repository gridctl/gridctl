# CLI Reference

Commands are grouped by domain, matching the groups in `gridctl --help`. Run `gridctl <command> --help` for the full flag set; the tables below cover the high-value flags an operator reaches for daily.

Global flags: `--home <dir>` (equivalent to `GRIDCTL_HOME`; see [Home directory override](#home-directory-override)) replaces the home directory every gridctl path derives from, `--runtime <docker|podman>` overrides runtime auto-detection, `--no-color` disables styled output, and `--log-level <debug|info|warn|error>` sets the minimum log level (logs go to stderr, so JSON stdout stays parseable). Color is also suppressed automatically when output is piped, when `NO_COLOR` is set ([no-color.org](https://no-color.org/)), or when `TERM=dumb`.

Machine-readable output: commands whose `--format` flag is a binary table-vs-JSON choice (among them `validate`, `plan`, `optimize`, `activate`, `search`, `add`, `skill list`, `skill pins`, `var list`, `pins list`, `pins verify`, the `pack`, `project`, and `skill project` families, and `ctx status|sync|list`) also accept `--json` as a boolean alias, and `status`, `info`, `doctor`, `open`, `traces`, and `telemetry status` support `--json` directly. `export` and `var export` keep `--format` only, since their format is multi-valued (`yaml|json`, `env|json`). JSON always goes to stdout with human messages on stderr. Every JSON schema, including `status`, `info`, and `doctor`, is backward compatible within the `0.x` line: fields may be added, never removed or retyped without a clearly-labeled release.

Plain tables: `status`, `search`, `skill list`, `pins list`, `optimize`, `telemetry status`, and the table-rendering `pack`, `project`, `ctx`, and `skill project` commands accept `--plain` to render tables without box-drawing (2+-space column separation, one record per line) for `grep`/`awk` pipelines. Piped table output degrades to plain automatically; the flag forces it on a terminal. `--plain` cannot be combined with `--json`. The `var` family keeps `--plain` as its pre-existing "show unmasked value" flag (`var get`, `var export`); `var list` therefore has no `--plain`, though it does accept `--format json` / `--json` and its piped table output still degrades to the plain style.

## Contents

- [Stack lifecycle](#stack-lifecycle)
- [Catalog](#catalog)
- [LLM clients](#llm-clients)
- [Packs](#packs)
- [Wiring ownership (project)](#wiring-ownership-project)
- [Global context](#global-context)
- [Model routing (models)](#model-routing-models)
- [Groups](#groups)
- [Skills](#skills)
- [Variables](#variables)
- [Pins (TOFU schema pinning)](#pins-tofu-schema-pinning)
- [Server authorization (OAuth)](#server-authorization-oauth)
- [Traces](#traces)
- [Optimize](#optimize)
- [Limits](#limits)
- [Telemetry](#telemetry)
- [System](#system)

## Stack lifecycle

| Command | Purpose |
|---|---|
| `gridctl init [dir]` | Scaffold a commented starter `stack.yaml` that passes `validate` as-is (no runtime started). `--name <name>` sets the stack name (default: directory name), `--force` overwrites an existing file, `--example <minimal\|skills>` picks the variant (`skills` adds an example `SKILL.md`). |
| `gridctl validate <stack.yaml>` | Validate stack YAML (exit `0`/`1`/`2`); `--format json` or `--json` for machine-readable output. With a `model_preferences:` block present, also emits the advisory findings `model-preference-unknown-alias`, `model-preference-unhonored`, and `model-preference-portability` (warnings only, never blocking). |
| `gridctl plan <stack.yaml>` | Preview changes against running state with Terraform-style colored `+`/`~`/`-` symbols. Source servers are resolved to immutable build actions showing declared and resolved identity, the content-addressed image tag, mutable-ref warning, and cache state (`cached`, `build`, or `unknown` when no runtime is reachable). `--show-dockerfile` includes the exact generated Python Dockerfile in text output and each JSON build action; without it, Dockerfile text is omitted. MCP server comparison covers the complete effective configuration, including source auth, build arguments, commands, volume mounts, replica settings, and autoscaling policy; omitted defaults and empty collections compare equal to their explicit equivalents. `-y` / `--auto-approve` to apply, `--format json` or `--json` for machine output. |
| `gridctl apply <stack.yaml>` | Start containers and the MCP gateway. Source-based servers include custom Dockerfiles and generated Python images for exact public PyPI releases or packaged git/local projects. They are resolved and built once per logical server before existing containers and replicas are reconciled; stale-image containers are replaced, while static and autoscaled replicas share the prepared image. Unchanged source images are reused only when their build-input label matches the resolved plan; `--no-cache` bypasses that identity lookup and forces a rebuild. Without a stack file, starts stackless mode (same as `serve`) and prints a notice. A stack `link:` block is reconciled once the gateway is healthy: each declared client is linked idempotently (not-installed clients warn and skip; with a `link:` block, `--flash` is ignored with a notice). Flags: `-f` foreground, `-p` port, `--bind <addr>` (default 127.0.0.1, loopback only), `--bind-all` (every interface, reachable from other hosts), `--insecure-allow-unauthenticated` (allow a non-loopback bind with no `gateway.auth` configured), `--base-port`, `-w` / `--watch`, `--flash`, `--code-mode`, `--no-cache`, `--no-expand`, `-v` verbose (print full stack as JSON), `-q` quiet, `--log-file <path>`. |
| `gridctl reload [stack-name]` | Hot reload a running stack's spec (accepts a stack name or file path). |
| `gridctl destroy <stack.yaml\|stack-name>` | Stop and remove all containers for the stack, by file or by the name shown in `gridctl status`. `--unlink` also removes the stack's declared `link:` entries from their client configs (default leaves client configs untouched); requires a loadable stack file, otherwise warns and skips. |
| `gridctl export` | Reverse-engineer `stack.yaml` from running state; `-o <dir>` write to directory, `--format yaml\|json` (default `yaml`). |
| `gridctl serve` | Start the web UI and API without managing a stack (stackless mode). Flags: `-f` foreground, `-p` port, and the same listen flags as `apply` (`--bind`, `--bind-all`, `--insecure-allow-unauthenticated`). |
| `gridctl stop` | Stop the stackless gridctl daemon. `--force` is only for orphans: when no state file exists but a gridctl process is still listening on the gateway port, `--force` terminates it; a normal stop never needs the flag. |
| `gridctl status` | Show running stacks; `-s` / `--stack` filters to one stack, `--replicas` expands to one row per replica, `--json` for machine-readable output. |
| `gridctl logs [stack]` | Tail the gateway daemon log (`~/.gridctl/logs/<stack>.log`), including source-build phase records tagged with `server` and `phase`. `-f` / `--follow` streams, `-n` / `--tail <N>` picks the line count (default 100), `--server <name>` switches to that containerized MCP server's stdout/stderr instead, and `-s` / `--stack <name>` names the stack explicitly. Stack auto-detected when exactly one is running. |

## Catalog

Install MCP servers by name instead of hand-writing `command`/`args`/`env`. The catalog merges two sources: a curated set embedded in gridctl (vetted entries with correct inputs and secret flags) and the official [MCP Registry](https://registry.modelcontextprotocol.io) (community publications, not vetted by gridctl). Registry responses are cached for an hour under `~/.gridctl/cache/catalog`; when the registry is unreachable, commands fall back to cached or curated results with a warning. `gridctl search` searches this install catalog; the `search` meta-tool a code-mode gateway exposes to LLM clients searches the running gateway's tools and is unrelated.

| Command | Purpose |
|---|---|
| `gridctl search [query]` | Search the catalog. Without a query, lists the curated set (the registry is not contacted). `--source <curated\|registry\|all>` picks sources (default `all`), `--format json` or `--json`, `--plain`. Deprecated registry entries are marked in the SOURCE column; entries whose package type has no stack mapping (mcpb, nuget, cargo) show `unsupported`. Exit `0` success (including no matches), `2` infrastructure error. |
| `gridctl add <name>` | Resolve a catalog entry (curated name like `github`, or a full registry name like `io.github.user/weather`) and append the matching server block to stack.yaml through the same backed-up, validated write path as `gridctl import`. Required inputs are prompted for; secret values are masked and stored in the variable store so the stack only carries `${var:KEY}` references, and unset required values are written as `${var:KEY}` placeholders with a `gridctl var set` hint. Supported install shapes: OCI images, npm (`npx`), pypi (`uvx`), and remote URLs with bearer/header auth. `--container` opts an eligible exact PyPI catalog package into a generated Python container instead of host `uvx`; it also accepts quoted `package==version`, quoted `uvx package==version`, or a GitHub URL suffixed by `#<40-character-commit>` or `@<40-character-commit>`. Unversioned packages, mutable Git refs, ambiguous uvx commands, and packages with unmappable arguments are refused. Catalog entries with filepath inputs are also refused because this command cannot declare the required host-to-container mount. The default remains host `uvx`. Other flags: `-y` / `--yes`, `--dry-run`, `-f` / `--file <stack.yaml>`, `-n` / `--name <name>`, `--no-vault`, `--format json` or `--json`. Exit `0` added, `1` cancelled, unknown name, or skipped collision, `2` infrastructure or validation error. |

## LLM clients

| Command | Purpose |
|---|---|
| `gridctl link [client]` | Connect an LLM client to the gateway; `--all` for every detected client, `--dry-run` to preview, `--name <name>` to set the server entry name (default `gridctl`), `--client-id <id>` to bind the link to a `clients:` access profile, `--group <name>` to link a tool group's endpoint (entry name defaults to `gridctl-<name>`), `--force` to overwrite a foreign or edited entry, `-p` / `--port <port>` to target a non-default gateway port (auto-detected from the running daemon, else 8180). Every link records ownership (config path, entry name, and a canonical value hash) in `~/.gridctl/project.lock.yaml`; an entry gridctl never recorded is refused unless it matches what gridctl would write (adopted silently) or `--force` is given. |
| `gridctl unlink [client]` | Remove gridctl from an LLM client's config; `-a` / `--all` for every client, `--name <name>` to target a non-default entry, `--group <name>` to remove a tool group's entry (targets `gridctl-<group>`, matching `link --group`), `--dry-run` to preview, `--force` to remove a recorded entry that was hand-edited. Only recorded entries are ever deleted: an entry gridctl did not write is never removed, with or without `--force` (adopt it first with `gridctl project adopt`). |
| `gridctl import [client]` | The reverse of link: scan installed clients for existing MCP server definitions and append selected ones to stack.yaml (client configs are read-only; the stack file is backed up first). Dedupes identical servers across clients with provenance, filters the gateway's own entry, skips name collisions in non-interactive runs (interactive runs prompt to skip, rename, or overwrite), and offers plaintext env secrets into the variable store as `${var:KEY}`. `-a` / `--all`, `--dry-run`, `-y` / `--yes`, `-f` / `--file <stack.yaml>`, `-n` / `--name <name>` (gateway entry name to exclude from the scan, matching `link --name`), `--no-vault`, `--format json` or `--json`. Exit `0` imported or nothing to do, `1` cancelled, `2` infrastructure or validation error. |

## Packs

`gridctl pack` imports and applies team packs: a git repo with a `gridctl-pack.yaml` manifest selecting skills, agents, rule fragments, and gateway wiring. See [`docs/packs.md`](./packs.md) for the manifest schema and semantics.

| Command | Purpose |
|---|---|
| `gridctl pack add <repo-url>` | Import a pack's selection into the registry (`--ref`, `--path`, `--trust`, `--dry-run`, `--format json` or `--json`). `--path` scopes resource discovery to a subdirectory; the manifest is still read from the repository root. Auth flags for private repos, same as `skill add`: `--vault-key <key>`, `--auth-token-stdin`, `--auth-token <pat>`, `--ssh-key <path>`. Exit `0` clean, `1` partial (unresolved or skipped), `2` infrastructure. |
| `gridctl pack apply <name>` | Project the pack through the existing engines, tagging every projection (`--force`, `--dry-run`, `--clients`, `--format json` or `--json`, `--plain`). `--clients <slugs>` restricts **wiring** only; skills, agents, and rules still apply to every detected client. Additive; `Applied N/M` summary; exit `0`/`1`/`2`. |
| `gridctl pack status [name]` | Per-resource state (shared vocabulary plus `unresolved`); exit `0`/`1`/`2`. `--format json` or `--json`, `--plain`. |
| `gridctl pack remove <name>` | Cascade removal: projections, wiring records, registry entries, then the pack record; drifted projections kept unless `--force` (`--dry-run`, `--format json` or `--json`). |

## Wiring ownership (project)

`gridctl project` manages recorded projections. The wiring kind (the only kind served here today; skills and agents stay under `gridctl skill project` for now) records ownership of the gateway entries `gridctl link` writes into client configs, so drift, adoption, and safe removal are decided from recorded state per Constitution Article XVI. All commands are pure file operations.

| Command | Purpose |
|---|---|
| `gridctl project sync --kind wiring` | Link every detected client (or `--clients <slugs>`) with ownership recorded; `--name`, `--group`, `--client-id`, `-p` / `--port`, `--force`, `--dry-run`, `--format json` or `--json`, `--plain`. Exit `0` clean, `1` a foreign or drifted entry was skipped, `2` infrastructure error. |
| `gridctl project status --kind wiring` | Per-entry ownership state: `in-sync`, `stale` (differs from what gridctl would write now), `drifted` (edited since gridctl wrote it), `target-missing` (entry or whole config file gone), `foreign` (gridctl-named entry never recorded, e.g. a pre-lockfile link), `missing` (client detected but not linked; advisory). Exit `0` clean, `1` attention needed, `2` error. `--format json` or `--json`, `--plain`, `-p` / `--port`. |
| `gridctl project adopt --kind wiring --client <slug>` | Record ownership of the entry's current value without rewriting it (`--name` for non-default entries). The take-ownership verb for hand edits and pre-lockfile links. Exit `0` adopted, `1` nothing to adopt, `2` error. |
| `gridctl project unsync --kind wiring --client <slug>` | Remove the recorded entry and purge its record (`--name`, `--force`, `--dry-run`, `--format json`). When the client is no longer detected, only the record is dropped. Foreign entries are never deleted. |

## Global context

`gridctl ctx` manages one canonical global agent-context file (`~/.gridctl/context/AGENTS.md`) and projects it into each linked client's global context location. Per-project AGENTS.md files stay version-controlled in their repos and are never touched. See [`docs/global-context.md`](./global-context.md) for strategies, drift handling, and per-client coverage. All `ctx` commands are pure file operations; no running gateway is required.

| Command | Purpose |
|---|---|
| `gridctl ctx init` | Scan every supported client's global context location and bootstrap the canonical file. `--import <client>` adopts an existing client file as canon, `--from <path>` adopts an arbitrary file, `--template` scaffolds the starter, `--force` overwrites an existing canonical file. The scan itself never writes. |
| `gridctl ctx status` | Per-client sync state (`in-sync`, `stale`, `drifted`, `target-missing`, `never-synced`, `unsupported`); in fragments mode also shows `mode` (`single-file` / `multi-file` / `compiled`). Exit `0` clean, `1` when anything needs attention, `2` on error. `--format json` or `--json`, `--plain`. |
| `gridctl ctx sync [client...]` | Project the canonical file (or fragments) to clients (all available clients when none named). `--dry-run` previews with diffs, `--force` overwrites drifted targets and repairs corrupt blocks, `--check` is CI mode (no writes, exit `1` on drift or pending sync), `--format json` or `--json`, `--plain`. Drifted targets are skipped with guidance, never silently overwritten; every write takes a timestamped backup. |
| `gridctl ctx add <name>` | Create a rule fragment; on first use activates fragments mode (migrates AGENTS.md to `fragments/00-default.md` with backup). Composition order is filename-lexicographic. |
| `gridctl ctx list` | List fragments with description, paths, size, and composition position. `--format json` or `--json`. |
| `gridctl ctx rm <name>` | Remove a fragment (backup first); projected client files drop on the next sync. |
| `gridctl ctx diff <client> [fragment]` | Unified diff between the canon (or one fragment) and a client's managed content (exit `0` identical, `1` differs, `2` error). Bare multi-file diff prints a per-fragment summary. |
| `gridctl ctx adopt <client> [fragment]` | Pull a client's hand edit back into the canon or a fragment. Multi-file clients require a fragment name; compiled targets refuse unless `--into <fragment>` captures the whole body. |
| `gridctl ctx unsync [client...]` | Remove managed artifacts (`--all` for every synced client; `--format json` or `--json` for machine output). Dedicated files and multi-file fragment projections are deleted; shim lines and managed blocks are stripped; user-owned content is preserved. |
| `gridctl ctx edit [fragment]` | Open the canonical file (or a fragment) in `$VISUAL`/`$EDITOR`, then print sync state. In fragments mode a name is required. |

## Model routing (models)

`gridctl models` manages one model routing policy document (`~/.gridctl/models/policy.yaml`) and projects it into a LiteLLM auto-router config fragment (plus the `include:` line referencing it from your own LiteLLM config) and an OpenCode provider stanza. gridctl never proxies inference: LiteLLM does the routing, gridctl only compiles and synchronizes configuration. Unrelated to the `model_preferences:` stack block (a per-skill model hint) and to the gateway's MCP tool router. See [`docs/model-policy.md`](./model-policy.md) for the policy schema, ownership model, and the restart contract. All `models` commands are pure file operations; no running gateway or LiteLLM is required. Experimental.

| Command | Purpose |
|---|---|
| `gridctl models init` | Scaffold the policy. `--template local-only\|hybrid\|cloud-primary` picks a commented starter (default `hybrid`); `--from-litellm <path>` scaffolds from an existing LiteLLM config (its `model_list` names become backend references, never copied inventory, and the config becomes the sync target); `--force` overwrites. |
| `gridctl models edit` | Open the policy in `$VISUAL`/`$EDITOR`, validate on close, and print next steps. |
| `gridctl models validate` | Check the policy: schema errors, tiers referencing undeclared backends, literal secrets (rendered output carries only env references), and LiteLLM keys that must stay in the parent config (`router_settings`, `fallbacks`: an included fragment silently replaces them). Warns about backends missing from the parent's `model_list`. Exit `0` clean (warnings alone stay `0`), `1` errors, `2` error. `--format json` or `--json`. |
| `gridctl models render` | Render one target to stdout (`-o <file>` to write): `--target litellm` (the router-only fragment) or `--target opencode` (the provider stanza). Never touches sync state; useful for piping to a remote host yourself. |
| `gridctl models sync` | Render the fragment, keep the `include:` line in your LiteLLM config pointing at it (a single-line edit; the rest of the file survives byte-for-byte, comments included), and write the OpenCode provider stanza (an RFC 6902 patch; your own keys, including the top-level `model` pick, are never touched). `--dry-run` previews (`--diff` adds unified diffs), `--force` overwrites drifted and foreign targets, `--check` is CI mode (no writes, exit `1` when anything is out of sync), `--format json` or `--json`. Every write takes a timestamped backup. Sync output states plainly that LiteLLM reads config only at startup and prints the restart hint. |
| `gridctl models status` | Per-target state (`in-sync`, `stale`, `drifted`, `target-missing`, `never-synced`) plus the `restart-pending` annotation on the fragment (set by sync, cleared only by `ack-restart`, never by another sync, and never affects the exit code). Exit `0` clean, `1` attention, `2` error. `--format json` or `--json`, `--plain`. |
| `gridctl models ack-restart` | Record that LiteLLM was restarted since the last fragment write. gridctl never probes the process to guess. `--format json` or `--json`. |
| `gridctl models adopt` | Accept hand edits of the fragment or the provider entry as the new owned state (clears drift without touching any file). `--format json` or `--json`. |
| `gridctl models unsync` | Remove the provider stanza, the include line (restoring a promoted scalar `include:` if that is what existed), and the fragment; everything outside gridctl's own writes survives byte-for-byte. `--force` also removes hand-edited targets; exit `1` when anything was kept or failed. The policy document itself is never touched. `--format json` or `--json`. |

## Groups

Named cross-server tool bundles declared under `groups:` in stack.yaml (see the [config schema](config-schema.md#groups-tool-bundles)), each served at `/groups/{name}/mcp`. Exit codes: `0` success (including no groups configured), `2` infrastructure error.

| Command | Purpose |
|---|---|
| `gridctl groups` | Table of groups with member counts (resolved against the live tool surface), override counts, and endpoints. `-s` / `--stack <name>` picks the stack (auto-detected when only one is running). Prints a sample `groups:` block when none is configured. |
| `gridctl groups --verbose` | Include each group's exposed (post-rename) tool names. |
| `gridctl groups --format json` | Machine-readable report; `--json` is an alias, `--plain` for tab-separated rows. |

## Skills

Skills are prose; the registry surfaces every active `SKILL.md` to prompt-rendering MCP clients as a prompt, and `skill project` places selected skills into native client skill directories for clients that read skills from disk. See [`docs/skills.md`](./skills.md) for the authoring guide and the per-client channel matrix.

The same import pipeline also handles agent definitions: `skill add` discovers `agents/*.md` files alongside `SKILL.md`, and the `--kind agent` flag on `skill list`, `skill remove`, `skill info`, and the `skill project` verbs that act on one kind (`sync`, `unsync`, `adopt`) operates on them. `skill project status` takes no `--kind`: it always reports both kinds in one table. `--kind` defaults to `skill` wherever it exists, so existing command lines behave exactly as before. In `skill project` JSON output every row carries a `kind` field (`"skill"` or `"agent"`); agent rows name their subject in an `agent` field where skill rows use `skill`.

| Command | Purpose |
|---|---|
| `gridctl skill list` | List skills in the registry (`--remote` for imported skills only, `--format json` or `--json` for machine output). Includes a Model column with each item's declared model preference. `--kind agent` lists imported agent definitions instead. |
| `gridctl skill add <repo-url>` | Import skills and agents from a git repository. `--ref` / `--path` pin branch or subdirectory; `--no-activate` imports as draft; `--trust` skips the security-scan confirmation (covers agent bodies and frontmatter too); `--force` overwrites existing skills and agents; `--rename <name>` renames on import (single skill only). Auth flags: `--vault-key <key>` (resolves from `${var:KEY}`; the only form re-resolved on later updates), `--auth-token-stdin` (reads the PAT from stdin), `--auth-token <pat>` (ephemeral HTTPS PAT for CI; a literal value warns because it lands in shell history), `--ssh-key <path>` (SSH). |
| `gridctl skill update [name]` | Update imported skills and agents (all when name omitted; name may be a skill or an agent); alias `gridctl skill sync`. `--dry-run` previews, `--trust` skips the security-scan confirmation for updated content, `--force` updates even when no change is detected. Locally edited files (including hand-edited `AGENT.md`) are refused without `--force`. |
| `gridctl skill remove <name>` | Remove an imported skill (`--kind agent` for an agent). |
| `gridctl skill pin <name> <ref>` | Pin a skill to a specific git ref. |
| `gridctl skill pins <verb>` | TOFU content pins for skill documents: `list`, `verify`, `diff <skill>`, `approve <skill>` (`--expect <composite_hash>`, `--reason` for skills with unresolved findings), `reset <skill>`. Same contract as `gridctl pins`: `--stack`, `--format json` or `--json`, exit `0`/`1`/`2`, opt-in `--fail-on-findings warn\|critical` on verify/diff. Distinct from `skill pin` (singular), which pins a git source ref. |
| `gridctl skill info <name>` | Show origin and update status (`--kind agent` for an agent). |
| `gridctl skill try <repo-url>` | Temporarily import a skill for evaluation (`--duration`, default `10m`, before auto-cleanup). Auth flags: `--vault-key <key>`, `--auth-token-stdin`, `--auth-token <pat>`, `--ssh-key <path>`. |
| `gridctl skill validate <name>` | Validate a skill definition. |
| `gridctl skill project sync [skill...]` | Project named active skills into native client skill directories (`--clients agents,claude-code,antigravity`; `--copy` for copies instead of symlinks; `--stack <path>` to apply that stack's `model_preferences:` policy; without it, projections a policy previously rewrote are preserved, never reverted; `--dry-run`, `--force`, `--format json` or `--json`, `--plain`; exit `0`/`1`/`2`). With no names, re-syncs the recorded projection set. `--kind agent` projects imported agents instead (all of them when no names are given; always copied): identity bytes to `~/.claude/agents/<name>.md`, and rendered client dialects to `~/.config/opencode/agents/`, `~/.copilot/agents/<name>.agent.md`, and `~/.gemini/agents/` where those clients are detected (lossy conversions reported per row). |
| `gridctl skill project status` | Per-projection state table for skills and agents (in-sync / stale / drifted / target-missing; rows on a client whose path is sourced unofficially are marked `unofficial path`; `--stack <path>` to judge staleness against that stack's `model_preferences:` policy; `--format json` or `--json`, `--plain`; exit `0`/`1`/`2`). Rewritten projections show `copy (model policy)` as their channel and carry `model_value` in JSON. |
| `gridctl skill project unsync [skill...]` | Remove projections gridctl created (`--all`, `--clients`, `--dry-run`, `--format json` or `--json`; `--kind agent` for agent projections). Copies are backed up before removal; unmanaged files are never touched. |
| `gridctl skill project adopt <skill>` | Pull a hand-edited copy projection back into the registry skill (`--client <slug>`, singular: adopt operates on one pair; `--format json` or `--json`; exit `0`/`1`/`2`). Backs up the registry `SKILL.md` as `.pre-<sha>`, re-syncs the pair to in-sync, and marks the skill locally edited for `skill update`. Symlinked projections are refused: the registry copy is already the source of truth. `--kind agent` adopts a hand-edited `~/.claude/agents/<name>.md` back into the canonical `AGENT.md` the same way; rendered targets (opencode, copilot, gemini) refuse adopt, since their dialects dropped keys at render time. |
| `gridctl activate <skill-name>` | Promote a skill from draft to active (exit `0`/`1`/`2`); `-s` / `--stack` to target a stack (auto-detected when only one runs), `--format json` or `--json` for machine output, `-q` / `--quiet` to suppress the success line. |

## Variables

The variable store holds both secrets (encrypted at rest, redacted in logs) and plaintext configuration. Reference entries from stack YAML with `${var:KEY}` (see [Variable Expansion](config-schema.md#variable-expansion)).

| Command | Purpose |
|---|---|
| `gridctl var set <key>` | Store a variable (interactive prompt, or `--value`). Secret by default (`--secret` makes that explicit); `--plaintext` for non-sensitive config visible in logs. `--type <string\|json\|list\|number\|bool>` tags the value's shape; `--set <name>` assigns it to a variable set. |
| `gridctl var get <key>` | Retrieve a variable (secrets masked; `--plain` to unmask). |
| `gridctl var list` | List all variables with type, visibility, and set assignment (`--format json` or `--json`). |
| `gridctl var explain <key>` | Show store lock state, value-free metadata, environment presence, store-first resolution verdict, declarations, and stack consumers without exposing the value (`--file`, `--format json` or `--json`; exit `0` resolved, `1` unset/denied/locked-partial, `2` operational failure). |
| `gridctl var run --set <name>\|--only <keys>\|--all -- <command> [args...]` | Execute a command directly with only the selected stored variables overlaid on its environment. Non-TTY output is exact-value redacted by default; `--no-redact` disables it, and `--redact` requires both outputs to be non-TTY. The child controls the exit code after spawn; setup failures exit `3`. Use an explicit shell command such as `sh -c '...'` when shell syntax is required. |
| `gridctl var scan [paths...]` | Scan working-tree text for exact stored secret values, or scan changed Git index blobs with `--staged` (`--format json` or `--json`; exit `0` clean, `1` findings, `2` incomplete or failed). Paths cannot be combined with `--staged`. |
| `gridctl var delete <key>` | Remove a variable (`--force` to skip confirmation). |
| `gridctl var import <file>` | Import from `.env` or `.json` (`--format` to override auto-detection; `# @public`, `# @type=`, `# @description=`, `# @docs=`, `# @example=`, and `# @deprecated=` markers tag entries). Reserved internal credential keys are skipped and named in warnings; valid entries are still imported. |
| `gridctl var export` | Export variables (`--format env\|json`, `--plain` to unmask). Reserved internal credential keys are always omitted, including from masked exports, and named without their values. |
| `gridctl var sets list` | List variable sets and their member counts. |
| `gridctl var sets create <name>` | Create a variable set. |
| `gridctl var sets delete <name>` | Delete a variable set (members are unassigned, not deleted). |
| `gridctl var lock` / `unlock` | Encrypt / decrypt the store (XChaCha20-Poly1305 + Argon2id). |
| `gridctl var change-passphrase` | Re-encrypt with a new passphrase. |

> `gridctl vault …` is a deprecated alias for `gridctl var …`, removed at v1.0. The `${vault:KEY}` reference syntax is likewise deprecated in favor of `${var:KEY}`.

`GRIDCTL_*`, `OP_CONNECT_TOKEN`, and `OP_SERVICE_ACCOUNT_TOKEN` are reserved for
gridctl bootstrap and control-plane credentials. They cannot be created or
updated in the variable store and are never delivered through variable sets,
exports, or local MCP process environments. Store files created by older
versions remain readable so an operator can diagnose and remove a legacy entry:

```bash
gridctl var delete GRIDCTL_VAULT_PASSPHRASE --force
```

Rename any value intended for a downstream workload to a non-reserved key and
update its `${var:KEY}` references before removal. Ordinary `$GRIDCTL_*` and
`${GRIDCTL_*}` environment interpolation remains available for non-credential
control settings; the exact bootstrap credential names never expand into stack
configuration.

`var scan` compares exact non-empty secret values of at least eight bytes. It
does not detect patterns, transformed values, multiline values, or repository
history. Working-tree scans honor `.gitignore`, do not follow symlinks, skip
binary files using an initial-sample NUL-byte check, and skip files larger than
10 MiB. Each file is capped at 100 findings. A pre-commit check can run
`gridctl var scan --staged`; use Gitleaks or TruffleHog as well when pattern and
history scanning are required.

## Pins (TOFU schema pinning)

All `pins` subcommands accept `--stack <name>` (auto-detected when only one stack is deployed).

| Command | Purpose |
|---|---|
| `gridctl pins list` | Status of all pinned servers; `--format json` or `--json` for machine output. |
| `gridctl pins verify [server]` | Verify pins (exit `0` clean, `1` on drift, `2` on infrastructure error); `--format json` or `--json` for machine output with a `has_drift` flag. |
| `gridctl pins diff [server]` | Per-tool before/after view of drifted definitions with change kinds on each modified tool (`[description]`, `[input_schema]`, `[output_schema]`, or `[schema_uncaptured]` for pins recorded before schema capture), old/new canonical schemas for schema changes, and poisoning-scan findings, control characters escaped; `--format json` carries the same schema fields plus `live_server_hash` for `approve --expect`. Exit `0` clean, `1` drift, `2` infrastructure error. `--fail-on-findings warn\|critical` additionally exits `1` when scan findings at or above that severity exist on pinned tools. |
| `gridctl pins approve <server>` | Re-pin current tool definitions, clearing drift; `--expect <hash>` binds the approval to a reviewed diff. |
| `gridctl pins reset <server>` | Delete pins (re-pinned on next apply). |

## Server authorization (OAuth)

Downstream authorization for external servers declared with `auth: {type: oauth}` in stack.yaml. gridctl acts as the OAuth client so one login serves every connected LLM client. Unrelated to the gateway's own inbound API auth (`gateway.auth`). All subcommands accept `--stack <name>` (auto-detected when only one stack is running).

| Command | Purpose |
|---|---|
| `gridctl auth login <server>` | Authorize a server in the browser. `--no-browser` prints the URL (forward the gateway port over SSH first); `--manual` accepts a pasted redirect URL when the browser cannot reach the daemon; `--timeout` bounds the wait (default 5m); `--format json` for machine output. |
| `gridctl auth status [server]` | Authorization state per server (exit `0` all authorized, `1` needs auth, `2` infrastructure error); `--format json` or `--json` for machine output. |
| `gridctl auth logout [server]` | Revoke (best effort) and delete stored tokens; `--all` for every server. |
| `gridctl auth reset <server>` | Delete tokens and the cached client registration; the next login starts clean. |

## Traces

| Command | Purpose |
|---|---|
| `gridctl traces` | Show recent distributed traces (table view). |
| `gridctl traces <trace-id>` | Span waterfall for a single trace. |
| `gridctl traces --follow` | Stream traces as they arrive. |
| `gridctl traces --stack <name>` | Query a specific stack (`-s` shorthand; defaults to the first running stack). |
| `gridctl traces --server <name>` | Filter by MCP server name. |
| `gridctl traces --errors` | Show only error traces. |
| `gridctl traces --min-duration 100ms` | Filter by minimum duration. |
| `gridctl traces --json` | Output as JSON. The list form emits the API envelope (`{traces, total, tracingEnabled, bufferSize, bufferCapacity}` with camelCase trace fields), not a bare array. |

## Optimize

| Command | Purpose |
|---|---|
| `gridctl optimize` | Surface unused servers and tools with projected weekly token impact (schema heuristics assume ~500 prompts/week). |
| `gridctl optimize --stack <name>` | Pick a specific stack when more than one is running. |
| `gridctl optimize --min-impact 5000` | Filter findings below a weekly token impact threshold (info findings always shown). |
| `gridctl optimize --severity warn,critical` | Allowlist by severity. |
| `gridctl optimize --format json` | Machine-readable `OptimizeReport` (exit `0`/`1`/`2`); `--json` is an alias. |

## Limits

Show the state of the rate limits declared under `limits:` in stack.yaml
(see the [config schema](config-schema.md#limits-rate-limits)).
Exit codes: `0` success (including no limits configured), `2` infrastructure
error (gateway unreachable).

| Command | Purpose |
|---|---|
| `gridctl limits` | Table of every rate limit (scope, rate, burst, state). Prints a sample `limits:` block when none is configured. |
| `gridctl limits --stack <name>` | Pick a specific stack when more than one is running. |
| `gridctl limits --format json` | Machine-readable status report; `--json` is an alias, `--plain` for tab-separated rows. |

## Telemetry

Inspect and manage opt-in telemetry persistence under `~/.gridctl/telemetry/`. Operates directly on on-disk files; does not require a running daemon. Persistence itself is configured per-stack and per-server in the stack YAML.

| Command | Purpose |
|---|---|
| `gridctl telemetry status [stack]` | List the on-disk telemetry inventory. Walks every stack when no argument is given; `--json` for machine-readable output. |
| `gridctl telemetry wipe [stack]` | Delete persisted telemetry files. `--server <name>` and `--signal <logs\|metrics\|traces>` scope the wipe; `-y` / `--yes` skips the prompt. |
| `gridctl telemetry tail <stack> <server>` | Follow the active `<signal>.jsonl` file (lumberjack rotations detected automatically). `--signal <logs\|metrics\|traces>` is required. |

## System

| Command | Purpose |
|---|---|
| `gridctl info` | Show runtime and environment facts: detected runtime (Docker/Podman), socket path, version, host alias, SELinux state, and rootless network stack. `--json` for machine output. Always exits 0; for judgments, use `doctor`. |
| `gridctl doctor` | Run opinionated environment checks with remediation hints: the active home directory and its source (`GRIDCTL_HOME` / `--home` / default), runtime detection, socket reachability, version floor, gateway port, `npx` and `uvx` availability, state directory hygiene, stale state files, vault status, the projection lockfile, client wiring state, and per-server MCP protocol generation (flagging mixed-generation fleets). Missing `uvx` is an advisory warning for host PyPI workflows; generated Python containers do not require host Python or uv. `--json` for a machine-readable report, `-q` to print only failures. Exit `0` (no errors), `1` (errors), `2` (doctor failed). |
| `gridctl open` | Open the web UI in the default browser (alias: `gridctl ui`). Port resolves from the first running stack; `-s` / `--stack` picks one, `-p` / `--port` overrides, `--path` sets the URL path, `--print` prints the URL only, `--json` emits `{"url": ...}`. |
| `gridctl reset` | Remove everything gridctl placed on this machine: projected skills, agents, and context rules in client directories, gateway entries gridctl owns inside shared client MCP configs, plus every stack's daemons, containers, and networks. The default tier preserves `~/.gridctl` (vault, oauth grants, pins, registry, cache, telemetry, logs); `--purge` deletes it as well, after a typed confirmation of the resolved path. Removal is lockfile-driven: hand-edited files are kept unless `--force`, and entries gridctl never created are never removed. A tar.gz backup is written before anything is deleted (fail-closed; there is no restore command, and the forward recovery path is `gridctl apply` / `gridctl pack add` / `gridctl link`). `--dry-run` previews, `--yes` for non-interactive use (never implies `--purge`), `--verbose` lists every artifact, `--format json`. Exit `0` clean, `1` partial (failed or kept items; re-run to retry), `2` infrastructure error or refused non-interactive confirmation. Built container images and named Docker volumes are not gridctl-owned the way labeled containers are and are left in place; clear them with `docker image prune` / `docker volume prune`. For one stack, use `destroy`. |
| `gridctl version` | Print version information. |
| `gridctl upgrade` | Check + prompt + upgrade (standalone install). `--check` only checks; `--yes` non-interactive (CI / cron); `--version <tag>` installs a specific release tag (allows downgrades); `--force` bypasses Homebrew detection and the up-to-date short-circuit. |

## Home directory override

Every gridctl path derives from one resolved home: `--home <dir>` > `GRIDCTL_HOME` > the OS home. The override is a home replacement, not a data-dir move: it relocates `~/.gridctl` AND the client projection targets (`~/.claude`, `~/.gemini`, and the rest), so `GRIDCTL_HOME=/tmp/demo gridctl apply` runs a fully isolated instance whose projections land under `/tmp/demo/.claude` and never touch the real client directories. That is the point: a demo, test, or CI home cannot damage real state, and `gridctl reset --purge` under it is confined by construction. To exercise real clients, run without the override. Two paths deliberately stay on the real home: `~` expansion inside user-authored `stack.yaml` values, and `~/.docker` runtime detection. State-mutating commands print `home: <dir> (GRIDCTL_HOME)` on stderr when the override is active; `status` and `doctor` always show the active home, and `status` warns when a daemon on the default port was started under a different home.

---

Back to the [docs index](README.md) or the [project README](../README.md).
