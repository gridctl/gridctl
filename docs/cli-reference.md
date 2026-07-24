# CLI Reference

Commands are grouped by domain, matching the groups in `gridctl --help`. Run `gridctl <command> --help` for the full flag set; the tables below cover the high-value flags an operator reaches for daily.

Global flags: `--runtime <docker|podman>` overrides runtime auto-detection, `--no-color` disables styled output, and `--log-level <debug|info|warn|error>` sets the minimum log level (logs go to stderr, so JSON stdout stays parseable). Color is also suppressed automatically when output is piped, when `NO_COLOR` is set ([no-color.org](https://no-color.org/)), or when `TERM=dumb`.

Machine-readable output: commands whose `--format` flag is a binary table-vs-JSON choice (`validate`, `plan`, `optimize`, `activate`, `search`, `add`, `skill list`, `var list`, `pins list`, and `pins verify`) also accept `--json` as a boolean alias, and `status`, `info`, `doctor`, `open`, `traces`, and `telemetry status` support `--json` directly. `export` and `var export` keep `--format` only, since their format is multi-valued (`yaml|json`, `env|json`). JSON always goes to stdout with human messages on stderr. The `status`, `info`, and `doctor` JSON schemas are experimental until 1.0.

Plain tables: `status`, `search`, `skill list`, `pins list`, `optimize`, and `telemetry status` accept `--plain` to render tables without box-drawing (2+-space column separation, one record per line) for `grep`/`awk` pipelines. Piped table output degrades to plain automatically; the flag forces it on a terminal. `--plain` cannot be combined with `--json`. The `var` family keeps `--plain` as its pre-existing "show unmasked value" flag (`var get`, `var export`); `var list` therefore has no formatting flag, though its piped output still degrades to the plain style.

## Contents

- [Stack lifecycle](#stack-lifecycle)
- [Catalog](#catalog)
- [LLM clients](#llm-clients)
- [Global context](#global-context)
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
| `gridctl validate <stack.yaml>` | Validate stack YAML (exit `0`/`1`/`2`); `--format json` or `--json` for machine-readable output. |
| `gridctl plan <stack.yaml>` | Preview changes against running state with Terraform-style colored `+`/`~`/`-` symbols; `-y` / `--auto-approve` to apply, `--format json` or `--json` for machine output. |
| `gridctl apply <stack.yaml>` | Start containers and the MCP gateway. Without a stack file, starts stackless mode (same as `serve`) and prints a notice. A stack `link:` block is reconciled once the gateway is healthy: each declared client is linked idempotently (not-installed clients warn and skip; with a `link:` block, `--flash` is ignored with a notice). Flags: `-f` foreground, `-p` port, `--base-port`, `-w` / `--watch`, `--flash`, `--code-mode`, `--no-cache`, `--no-expand`, `-v` verbose (print full stack as JSON), `-q` quiet, `--log-file <path>`. |
| `gridctl reload [stack-name]` | Hot reload a running stack's spec (accepts a stack name or file path). |
| `gridctl destroy <stack.yaml\|stack-name>` | Stop and remove all containers for the stack, by file or by the name shown in `gridctl status`. `--unlink` also removes the stack's declared `link:` entries from their client configs (default leaves client configs untouched); requires a loadable stack file, otherwise warns and skips. |
| `gridctl export` | Reverse-engineer `stack.yaml` from running state; `-o <dir>` write to directory, `--format yaml\|json` (default `yaml`). |
| `gridctl serve` | Start the web UI and API without managing a stack (stackless mode). |
| `gridctl stop` | Stop the stackless gridctl daemon; `--force` kills the process if graceful shutdown fails. |
| `gridctl status` | Show running stacks; `-s` / `--stack` filters to one stack, `--replicas` expands to one row per replica, `--json` for machine-readable output (experimental schema). |
| `gridctl logs [stack]` | Tail the gateway daemon log (`~/.gridctl/logs/<stack>.log`). `-f` / `--follow` streams, `-n` / `--tail <N>` picks the line count (default 100), `--server <name>` streams a containerized MCP server's logs instead. Stack auto-detected when exactly one is running. |

## Catalog

Install MCP servers by name instead of hand-writing `command`/`args`/`env`. The catalog merges two sources: a curated set embedded in gridctl (vetted entries with correct inputs and secret flags) and the official [MCP Registry](https://registry.modelcontextprotocol.io) (community publications, not vetted by gridctl). Registry responses are cached for an hour under `~/.gridctl/cache/catalog`; when the registry is unreachable, commands fall back to cached or curated results with a warning. `gridctl search` searches this install catalog; the `search` meta-tool a code-mode gateway exposes to LLM clients searches the running gateway's tools and is unrelated.

| Command | Purpose |
|---|---|
| `gridctl search [query]` | Search the catalog. Without a query, lists the curated set (the registry is not contacted). `--source <curated\|registry\|all>` picks sources (default `all`), `--format json` or `--json`, `--plain`. Deprecated registry entries are marked in the SOURCE column; entries whose package type has no stack mapping (mcpb, nuget, cargo) show `unsupported`. Exit `0` success (including no matches), `2` infrastructure error. |
| `gridctl add <name>` | Resolve a catalog entry (curated name like `github`, or a full registry name like `io.github.user/weather`) and append the matching server block to stack.yaml through the same backed-up, validated write path as `gridctl import`. Required inputs are prompted for; secret values are masked and stored in the variable store so the stack only carries `${var:KEY}` references, and unset required values are written as `${var:KEY}` placeholders with a `gridctl var set` hint. Supported install shapes: OCI images, npm (`npx`), pypi (`uvx`), and remote URLs with bearer/header auth. `-y` / `--yes`, `--dry-run`, `-f` / `--file <stack.yaml>`, `-n` / `--name <name>`, `--no-vault`, `--format json` or `--json`. Exit `0` added, `1` cancelled, unknown name, or skipped collision, `2` infrastructure or validation error. |

## LLM clients

| Command | Purpose |
|---|---|
| `gridctl link [client]` | Connect an LLM client to the gateway; `--all` for every detected client, `--dry-run` to preview, `--name <name>` to set the server entry name (default `gridctl`), `--client-id <id>` to bind the link to a `clients:` access profile, `--group <name>` to link a tool group's endpoint (entry name defaults to `gridctl-<name>`), `--force` to overwrite an existing entry, `-p` / `--port <port>` to target a non-default gateway port (auto-detected from the running daemon, else 8180). |
| `gridctl unlink [client]` | Remove gridctl from an LLM client's config; `-a` / `--all` for every client, `--name <name>` to target a non-default entry, `--dry-run` to preview. |
| `gridctl import [client]` | The reverse of link: scan installed clients for existing MCP server definitions and append selected ones to stack.yaml (client configs are read-only; the stack file is backed up first). Dedupes identical servers across clients with provenance, filters the gateway's own entry, skips name collisions in non-interactive runs (interactive runs prompt to skip, rename, or overwrite), and offers plaintext env secrets into the variable store as `${var:KEY}`. `-a` / `--all`, `--dry-run`, `-y` / `--yes`, `-f` / `--file <stack.yaml>`, `--no-vault`, `--format json` or `--json`. Exit `0` imported or nothing to do, `1` cancelled, `2` infrastructure or validation error. |

## Global context

`gridctl ctx` manages one canonical global agent-context file (`~/.gridctl/context/AGENTS.md`) and projects it into each linked client's global context location. Per-project AGENTS.md files stay version-controlled in their repos and are never touched. See [`docs/global-context.md`](./global-context.md) for strategies, drift handling, and per-client coverage. All `ctx` commands are pure file operations; no running gateway is required.

| Command | Purpose |
|---|---|
| `gridctl ctx init` | Scan every supported client's global context location and bootstrap the canonical file. `--import <client>` adopts an existing client file as canon, `--from <path>` adopts an arbitrary file, `--template` scaffolds the starter, `--force` overwrites an existing canonical file. The scan itself never writes. |
| `gridctl ctx status` | Per-client sync state (`in-sync`, `stale`, `drifted`, `target-missing`, `never-synced`, `unsupported`); exit `0` clean, `1` when anything needs attention, `2` on error. `--format json` or `--json`, `--plain`. |
| `gridctl ctx sync [client...]` | Project the canonical file to clients (all available clients when none named). `--dry-run` previews with diffs, `--force` overwrites drifted targets and repairs corrupt blocks, `--check` is CI mode (no writes, exit `1` on drift or pending sync), `--format json` or `--json`, `--plain`. Drifted targets are skipped with guidance, never silently overwritten; every write takes a timestamped backup. |
| `gridctl ctx diff <client>` | Unified diff between the canonical context and a client's managed content (exit `0` identical, `1` differs, `2` error). |
| `gridctl ctx adopt <client>` | Pull a client's hand edit back into the canonical file, then re-sync that client (other clients become stale). |
| `gridctl ctx unsync [client...]` | Remove managed artifacts (`--all` for every synced client). Dedicated files are deleted; shim lines and managed blocks are stripped; user-owned content is preserved. |
| `gridctl ctx edit` | Open the canonical file in `$VISUAL`/`$EDITOR`, then print sync state. |

## Groups

Named cross-server tool bundles declared under `groups:` in stack.yaml (see the [config schema](config-schema.md#groups-tool-bundles)), each served at `/groups/{name}/mcp`. Exit codes: `0` success (including no groups configured), `2` infrastructure error.

| Command | Purpose |
|---|---|
| `gridctl groups` | Table of groups with member counts (resolved against the live tool surface), override counts, and endpoints. Prints a sample `groups:` block when none is configured. |
| `gridctl groups --verbose` | Include each group's exposed (post-rename) tool names. |
| `gridctl groups --format json` | Machine-readable report; `--json` is an alias, `--plain` for tab-separated rows. |

## Skills

Skills are prose; the registry surfaces every active `SKILL.md` to prompt-rendering MCP clients as a prompt, and `skill project` places selected skills into native client skill directories for clients that read skills from disk. See [`docs/skills.md`](./skills.md) for the authoring guide and the per-client channel matrix.

| Command | Purpose |
|---|---|
| `gridctl skill list` | List skills in the registry (`--remote` for imported skills only, `--format json` or `--json` for machine output). |
| `gridctl skill add <repo-url>` | Import skills from a git repository. `--ref` / `--path` pin branch or subdirectory; `--no-activate` imports as draft; `--trust` skips the security-scan confirmation; `--force` overwrites existing skills; `--rename <name>` renames on import (single skill only). Auth flags: `--auth-token <pat>` (ephemeral HTTPS PAT, CI), `--vault-key <key>` (resolves from `${var:KEY}`), `--ssh-key <path>` (SSH). |
| `gridctl skill update [name]` | Update imported skills (all when name omitted); alias `gridctl skill sync`. `--dry-run` previews, `--force` updates even when no change is detected. |
| `gridctl skill remove <name>` | Remove an imported skill. |
| `gridctl skill pin <name> <ref>` | Pin a skill to a specific git ref. |
| `gridctl skill info <name>` | Show origin and update status. |
| `gridctl skill try <repo-url>` | Temporarily import a skill for evaluation (`--duration`, default `10m`, before auto-cleanup). Auth flags: `--auth-token <pat>`, `--vault-key <key>`, `--ssh-key <path>`. |
| `gridctl skill validate <name>` | Validate a skill definition. |
| `gridctl skill project sync [skill...]` | Project named active skills into native client skill directories (`--clients agents,claude-code,antigravity`; `--copy` for copies instead of symlinks; `--dry-run`, `--force`, `--format json` or `--json`, `--plain`; exit `0`/`1`/`2`). With no names, re-syncs the recorded projection set. |
| `gridctl skill project status` | Per-projection state table (in-sync / stale / drifted / target-missing; `--format json` or `--json`, `--plain`; exit `0`/`1`/`2`). |
| `gridctl skill project unsync [skill...]` | Remove projections gridctl created (`--all`, `--clients`, `--dry-run`, `--format json` or `--json`). Copies are backed up before removal; unmanaged files are never touched. |
| `gridctl activate <skill-name>` | Promote a skill from draft to active (exit `0`/`1`/`2`); `-s` / `--stack` to target a stack (auto-detected when only one runs), `--format json` or `--json` for machine output, `-q` / `--quiet` to suppress the success line. |

## Variables

The variable store holds both secrets (encrypted at rest, redacted in logs) and plaintext configuration. Reference entries from stack YAML with `${var:KEY}` (see [Variable Expansion](config-schema.md#variable-expansion)).

| Command | Purpose |
|---|---|
| `gridctl var set <key>` | Store a variable (interactive prompt, or `--value`). Secret by default (`--secret` makes that explicit); `--plaintext` for non-sensitive config visible in logs. `--type <string\|json\|list\|number\|bool>` tags the value's shape; `--set <name>` assigns it to a variable set. |
| `gridctl var get <key>` | Retrieve a variable (secrets masked; `--plain` to unmask). |
| `gridctl var list` | List all variables with type, visibility, and set assignment (`--format json` or `--json`). |
| `gridctl var delete <key>` | Remove a variable (`--force` to skip confirmation). |
| `gridctl var import <file>` | Import from `.env` or `.json` (`--format` to override auto-detection; `# @public` / `# @type=` markers tag entries). |
| `gridctl var export` | Export variables (`--format env\|json`, `--plain` to unmask). |
| `gridctl var sets list` | List variable sets and their member counts. |
| `gridctl var sets create <name>` | Create a variable set. |
| `gridctl var sets delete <name>` | Delete a variable set (members are unassigned, not deleted). |
| `gridctl var lock` / `unlock` | Encrypt / decrypt the store (XChaCha20-Poly1305 + Argon2id). |
| `gridctl var change-passphrase` | Re-encrypt with a new passphrase. |

> `gridctl vault …` is a deprecated alias for `gridctl var …`, removed at v1.0. The `${vault:KEY}` reference syntax is likewise deprecated in favor of `${var:KEY}`.

## Pins (TOFU schema pinning)

All `pins` subcommands accept `--stack <name>` (auto-detected when only one stack is deployed).

| Command | Purpose |
|---|---|
| `gridctl pins list` | Status of all pinned servers; `--format json` or `--json` for machine output. |
| `gridctl pins verify [server]` | Verify pins (exit `0` clean, `1` on drift, `2` on infrastructure error); `--format json` or `--json` for machine output with a `has_drift` flag. |
| `gridctl pins diff [server]` | Per-tool before/after view of drifted definitions with poisoning-scan findings, control characters escaped; `--format json` includes `live_server_hash` for `approve --expect`. Exit `0` clean, `1` drift, `2` infrastructure error. `--fail-on-findings warn\|critical` additionally exits `1` when scan findings at or above that severity exist on pinned tools. |
| `gridctl pins approve <server>` | Re-pin current tool definitions, clearing drift; `--expect <hash>` binds the approval to a reviewed diff. |
| `gridctl pins reset <server>` | Delete pins (re-pinned on next apply). |

## Server authorization (OAuth)

Downstream authorization for external servers declared with `auth: {type: oauth}` in stack.yaml. gridctl acts as the OAuth client so one login serves every connected LLM client. Unrelated to the gateway's own inbound API auth (`gateway.auth`). All subcommands accept `--stack <name>` (auto-detected when only one stack is running).

| Command | Purpose |
|---|---|
| `gridctl auth login <server>` | Authorize a server in the browser. `--no-browser` prints the URL (forward the gateway port over SSH first); `--manual` accepts a pasted redirect URL when the browser cannot reach the daemon; `--timeout` bounds the wait (default 5m). |
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
| `gridctl optimize` | Surface unused servers and tools with weekly USD impact. |
| `gridctl optimize --stack <name>` | Pick a specific stack when more than one is running. |
| `gridctl optimize --min-impact 0.10` | Filter findings below a weekly USD impact threshold (info findings always shown). |
| `gridctl optimize --severity warn,critical` | Allowlist by severity. |
| `gridctl optimize --format json` | Machine-readable `OptimizeReport` (exit `0`/`1`/`2`); `--json` is an alias. |

## Limits

Show consumption against the budgets and rate limits declared under
`limits:` in stack.yaml (see the [config schema](config-schema.md#limits-budgets-and-rate-limits)).
Exit codes: `0` all clear or no limits configured, `1` at least one budget
exceeded, `2` infrastructure error (gateway unreachable).

| Command | Purpose |
|---|---|
| `gridctl limits` | Table of every budget (spend, cap, window, state) and rate limit. Prints a sample `limits:` block when none is configured. |
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
| `gridctl doctor` | Run opinionated environment checks with remediation hints: runtime detection, socket reachability, version floor, gateway port, `npx` availability, state directory hygiene, stale state files, and vault status. `--json` for a machine-readable report, `-q` to print only failures. Exit `0` (no errors), `1` (errors), `2` (doctor failed). |
| `gridctl open` | Open the web UI in the default browser (alias: `gridctl ui`). Port resolves from the first running stack; `-s` / `--stack` picks one, `-p` / `--port` overrides, `--path` sets the URL path, `--print` prints the URL only, `--json` emits `{"url": ...}`. |
| `gridctl version` | Print version information. |
| `gridctl upgrade` | Check + prompt + upgrade (standalone install). `--check` only checks; `--yes` non-interactive (CI / cron); `--version <tag>` installs a specific release tag (allows downgrades); `--force` bypasses Homebrew detection and the up-to-date short-circuit. |

---

Back to the [docs index](README.md) or the [project README](../README.md).
