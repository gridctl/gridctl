# CLI Reference

Commands are grouped by domain. Run `gridctl <command> --help` for the full flag set; the tables below cover the high-value flags an operator reaches for daily.

## Contents

- [Stack lifecycle](#stack-lifecycle)
- [LLM clients](#llm-clients)
- [Skills - remote import (git)](#skills---remote-import-git)
- [Skills - authoring](#skills---authoring)
- [Runs](#runs)
- [Vault](#vault)
- [Pins (TOFU schema pinning)](#pins-tofu-schema-pinning)
- [Traces](#traces)
- [Optimize](#optimize)
- [Telemetry](#telemetry)
- [Upgrade](#upgrade)

## Stack lifecycle

| Command | Purpose |
|---|---|
| `gridctl validate <stack.yaml>` | Validate stack YAML (exit `0`/`1`/`2`); `--format json` for machine-readable output. |
| `gridctl plan <stack.yaml>` | Preview changes against running state; `-y` / `--auto-approve` to apply. |
| `gridctl apply <stack.yaml>` | Start containers and the MCP gateway. Flags: `-f` foreground, `-p` port, `--base-port`, `--watch`, `--flash`, `--code-mode`, `--no-cache`, `--no-expand`, `-v` JSON dump, `-q` quiet, `--log-file <path>`. |
| `gridctl reload [stack-name]` | Hot reload a running stack's spec. |
| `gridctl destroy <stack.yaml>` | Stop and remove all containers for the stack. |
| `gridctl export` | Reverse-engineer `stack.yaml` from running state; `-o <dir>` write to directory, `--format json`. |
| `gridctl serve` | Start the web UI and API without managing a stack (stackless mode). |
| `gridctl stop` | Stop the stackless gridctl daemon. |
| `gridctl status` | Show running stacks; `--replicas` expands to one row per replica. |
| `gridctl info` | Show the detected container runtime (Docker/Podman). |
| `gridctl version` | Print version information. |

## LLM clients

| Command | Purpose |
|---|---|
| `gridctl link [client]` | Connect an LLM client to the gateway; `--all` for every detected client, `--dry-run` to preview. |
| `gridctl unlink [client]` | Remove gridctl from an LLM client's config. |

## Skills - remote import (git)

| Command | Purpose |
|---|---|
| `gridctl skill list` | List skills in the registry (`--remote` for imported skills only). |
| `gridctl skill add <repo-url>` | Import skills from a git repository. Auth flags: `--auth-token <pat>` (ephemeral HTTPS PAT, CI), `--vault-key <key>` (resolves from `${vault:KEY}`), `--ssh-key <path>` (SSH). |
| `gridctl skill update [name]` | Update imported skills (all when name omitted). |
| `gridctl skill remove <name>` | Remove an imported skill. |
| `gridctl skill pin <name> <ref>` | Pin a skill to a specific git ref. |
| `gridctl skill info <name>` | Show origin and update status. |
| `gridctl skill try <repo-url>` | Temporarily import a skill for evaluation. |
| `gridctl skill validate <name>` | Validate a skill definition. |
| `gridctl test <skill-name>` | Run a skill's `acceptance_criteria` (exit `0`/`1`/`2`); `--dry-run` lists criteria without executing, `--criterion <n>` scopes to one criterion, `--format json` for machine output. |

## Skills - authoring

| Command | Purpose |
|---|---|
| `gridctl agent init [DIR]` | Scaffold a starter skill. `--lang ts` (default), `--lang go`, or `--prompt-only` (mutually exclusive with `--lang`). |
| `gridctl agent dev --root <dir>` | Start the IDE dev server (canvas + watcher + trace overlay) on port `8181`. |
| `gridctl agent validate <skill>` | Static-check a skill's manifest and handler symbols. |
| `gridctl agent build <skill>` | Compile: esbuild for TS, `go build -buildmode=plugin` for Go. Writes `dist/manifest.json`. |

## Runs

| Command | Purpose |
|---|---|
| `gridctl run <skill>` | Execute a typed skill and stream events. Input: `--input '<json>'` inline, `--input @file.json`, or `--input -` for stdin. Output: `--format json` (NDJSON + summary), `--quiet`. |
| `gridctl runs list` | Recent runs (`--limit N`, `--format json`). |
| `gridctl runs inspect <run_id>` | Typed event timeline. |
| `gridctl runs trace <run_id>` | OTel-shaped JSON projection. |
| `gridctl runs resume <run_id>` | Time-travel resume from the last checkpoint; `--from-step <node_id>` to override. |
| `gridctl runs approve <run_id>` | Resolve a pending approval gate. `--decision approve\|reject`, `--reason "<text>"`. |

## Vault

| Command | Purpose |
|---|---|
| `gridctl vault set <key>` | Store a secret (interactive prompt, or `--value`); `--set <name>` to assign to a variable set. |
| `gridctl vault get <key>` | Retrieve a secret (masked; `--plain` to unmask). |
| `gridctl vault list` | List all vault keys with set assignments. |
| `gridctl vault delete <key>` | Remove a secret (`--force` to skip confirmation). |
| `gridctl vault import <file>` | Import from `.env` or `.json` (`--format` to override auto-detection). |
| `gridctl vault export` | Export secrets (`--format env\|json`, `--plain` to unmask). |
| `gridctl vault lock` / `unlock` | Encrypt / decrypt the vault. |
| `gridctl vault change-passphrase` | Re-encrypt with a new passphrase. |

## Pins (TOFU schema pinning)

| Command | Purpose |
|---|---|
| `gridctl pins list` | Status of all pinned servers. |
| `gridctl pins verify [server]` | Verify pins (`--exit-code` for CI: exit `1` on drift). |
| `gridctl pins approve <server>` | Re-pin current tool definitions, clearing drift. |
| `gridctl pins reset <server>` | Delete pins (re-pinned on next apply). |

## Traces

| Command | Purpose |
|---|---|
| `gridctl traces` | Show recent distributed traces (table view). |
| `gridctl traces <trace-id>` | Span waterfall for a single trace. |
| `gridctl traces --follow` | Stream traces as they arrive. |
| `gridctl traces --server <name>` | Filter by MCP server name. |
| `gridctl traces --errors` | Show only error traces. |
| `gridctl traces --min-duration 100ms` | Filter by minimum duration. |
| `gridctl traces --json` | Output as JSON. |

## Optimize

| Command | Purpose |
|---|---|
| `gridctl optimize` | Surface unused servers and tools with weekly USD impact. |
| `gridctl optimize --stack <name>` | Pick a specific stack when more than one is running. |
| `gridctl optimize --min-impact 0.10` | Filter findings below a weekly USD impact threshold (info findings always shown). |
| `gridctl optimize --severity warn,critical` | Allowlist by severity. |
| `gridctl optimize --format json` | Machine-readable `OptimizeReport` (exit `0`/`1`/`2`). |

## Telemetry

Inspect and manage opt-in telemetry persistence under `~/.gridctl/telemetry/`. Operates directly on on-disk files; does not require a running daemon. Persistence itself is configured per-stack and per-server in the stack YAML.

| Command | Purpose |
|---|---|
| `gridctl telemetry status [stack]` | List the on-disk telemetry inventory. Walks every stack when no argument is given; `--json` for machine-readable output. |
| `gridctl telemetry wipe [stack]` | Delete persisted telemetry files. `--server <name>` and `--signal <logs\|metrics\|traces>` scope the wipe; `-y` / `--yes` skips the prompt. |
| `gridctl telemetry tail <stack> <server>` | Follow the active `<signal>.jsonl` file (lumberjack rotations detected automatically). `--signal <logs\|metrics\|traces>` is required. |

## Upgrade

| Command | Purpose |
|---|---|
| `gridctl upgrade` | Check + prompt + upgrade (standalone install). |
| `gridctl upgrade --check` | Only check for updates; do not install. |
| `gridctl upgrade --yes` | Non-interactive (CI / cron). |
| `gridctl upgrade --version <tag>` | Install a specific release tag (allows downgrades). |
| `gridctl upgrade --force` | Bypass Homebrew detection and the up-to-date short-circuit. |

---

Back to the [docs index](README.md) or the [project README](../README.md).
