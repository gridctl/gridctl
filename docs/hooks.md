# Hooks

A hook is a directory `hooks/<name>/` in a pack repository. It holds `hook.json` and the scripts that file names. `gridctl pack add` imports the hooks the manifest selects into `~/.gridctl/registry/hooks/<name>/`, scans them, and pins the bytes. gridctl does not run the scripts. `gridctl pack apply` and `gridctl skill project sync --kind hook` copy the scripts into a gridctl-owned directory per client and write each detected target: matcher groups patched into Claude Code's user settings file, and a generated plugin for OpenCode. Execution belongs to the client.

`pack status` and `GET /api/packs/{name}` report a projected hook per client. A hook that was imported and never projected is a store-presence row (`in-sync`, detail `imported; not projected`). A name recorded on the pack but missing from the registry is `missing`, with detail `not in the hook registry; re-run 'gridctl pack add'`. A directory that is present but whose `hook.json` no longer validates is `drifted`. The detail is the parse error. The remediation is `review with 'gridctl skill pins diff <name> --kind hook' or remove with 'gridctl skill remove <name> --kind hook'`. A hook that was skipped at add is not recorded, so status does not list it.

## hook.json

`hook.json` is a JSON object. The only accepted top-level keys are `name`, `description`, `dialect`, and `hooks`. Any other top-level key is refused by name (`hook <name>: unknown field "<key>"`).

| Field | Rule |
|---|---|
| `name` | Required. Lowercase letters, digits, and hyphens. Must equal the directory name. A mismatch is a malformed hook, named in a warning, and is not imported. |
| `description` | Required, non-empty. |
| `dialect` | Required. The only accepted value is `claude-code`. |
| `hooks` | Required object. Keys are Claude Code event names matching `^[A-Z][A-Za-z]+$`. Values are arrays of matcher groups. |

A matcher group is `{ "matcher"?: string, "hooks": [handler, ...] }`. `matcher` is optional and, when present, must be a string. Unknown keys on a matcher group are refused by name.

A handler must have `type: "command"`. `http`, `mcp_tool`, `prompt`, and `agent` are refused, and the error names the type (`hook <name>: handler type "<type>" is not supported`). A handler must not carry `env`, `headers`, `url`, `allowedEnvVars`, or `shell`. Each is refused by name (`hook <name>: handler key "<key>" is not allowed`). `shell` is refused because a `shell` key would route the command back through a shell, which a hook definition does not allow.

`command` must be a `./`-relative, slash-separated path with no `..` segment. It must resolve to a regular, executable file inside the hook directory. Symlinks, directories, absolute paths, and files outside the directory are refused. An inline shell string is refused with `hook handler commands must reference a script shipped in the hook directory`.

These handler keys pass through: `args`, `timeout`, `if`, `statusMessage`, `async`, `asyncRewake`, and `onFailure`. `args` must be an array of strings. `if` must be a string. Any other handler key passes through and is named in an import warning: `hook <name>: unrecognized handler key "<key>" passed through`.

### The `if` field

Confirmed against the Claude Code hooks reference (https://code.claude.com/docs/en/hooks). `if` is a permission-rule pattern the client matches against the tool call, for example `Bash(git *)`. It is not code the client evaluates. A string is stored as written. A non-string is refused by name (`hook <name>: handler key "if" must be a string`).

Any string anywhere in `hook.json` that contains `${var:`, `${secret:`, or `${vault:` is refused: `hook <name>: variable and secret references are not allowed in a hook definition`. Plain `$VAR` and `${VAR}` pass through unresolved. gridctl does not resolve them.

Hook directory contents follow the own-directory skill install rules. Dot-prefixed entries, symlinks, irregular files, and nested skill or hook directories are skipped with a warning. Executable modes are preserved. The per-file cap is 5 MiB, the file-count cap is 500, and the total-size cap is 50 MiB, the same values as skills.

## Pack selection

`gridctl-pack.yaml` takes an optional `hooks:` list of names from the pack repository. Empty or absent means none. Hooks are opt-in, like `rules:`. A `{name, source}` mapping is a validation error: `hooks come only from the pack repository; a {name, source} mapping is not allowed`. Hooks do not come from external `sources:`. A selected name the repository does not ship is `unresolved` (`hooks:<name>`) and the pack add stays partial (exit 1).

A `hooks:` key is accepted on `gridctl.dev/v1` and `gridctl.dev/v1alpha1` with no warning.

`gridctl skill add` does not import hooks. When it discovers `hooks/<name>/hook.json` files it prints `N hook definition(s) not imported; hooks import through a pack manifest (gridctl pack add)` and does not install them. The hint is printed when something is imported, when every skill or agent is skipped, and when the repository has hook definitions and no importable skills or agents. That last case then exits with the existing empty-repository error.

A successful import names a non-zero hook count in the import line: `Imported pack "<name>" (N skills, N agents, N hooks, wiring: yes)`. Rules and hooks are omitted from that line when the count is zero. `--dry-run` prints `Would import` and the same `Skipped:` line for a finding. It does not write the registry or the pin file. JSON omits `hooks` when none were imported.

A pack record with a non-empty `Hooks` list stamps `skills.lock.yaml` version 9. A skipped hook is not recorded, so that add keeps the previous stamp. Files without a recorded hook selection keep their previous stamp. An older gridctl refuses a version 9 file with `import lockfile was written by a newer gridctl version`. `gridctl-pack.lock.yaml` is unchanged. Hooks do not enter its digest.

## Import, scan, and trust

`gridctl pack add` copies the hook directory to `~/.gridctl/registry/hooks/<name>/` and writes a `.origin.json` sidecar (repo, ref, path, commit, imported time, content hash, installed hash, credential reference or key path, never a token).

The danger-pattern scanner runs over `hook.json` and every text file in the directory, sniffed by content rather than extension. Any finding of any severity skips that hook unless `--trust`. A warning such as `chmod 777` skips a hook the same way a danger finding does. That is the agent rule, not the supporting-file rule. The CLI prints `Skipped: <name> (hook): security findings detected (use --trust to proceed):` and exits 1. Other selected resources still import. The skipped hook is not installed and not pinned. With `--trust`, the hook imports, the finding is a `Warning:` line, and every finding is stored on the pin record. `POST /api/packs` is different: the same finding refuses the whole import with 409 before any write. See [Packs](api-reference.md#packs).

`gridctl skill update` of a pack repository re-imports that pack's hooks under the same scan and `--trust` gate. The name may be a skill, an agent, or a hook. It prints `Updated hook` for each one it installs. A hook that fails the scan or drift gate is printed as `Skipped: <name> (hook):` with the failing rule, and the registry copy is left unchanged. A named update that skips a hook exits 1. A bulk update prints the same line and still exits 0. It refuses to overwrite a hook whose registry bytes differ from the installed hash unless `--force`, including when the name is the hook. A forced overwrite keeps the previous `hook.json` as `hook.json.pre-<sha>`. `pack add` may overwrite that drift and keeps the same backup. A re-import also refreshes the executable bit on a script that already exists.

## Inspect and remove

`gridctl skill list --kind hook` prints Name, Description, Events, Scripts, and Pin. JSON rows carry `kind: "hook"`, `name`, `description`, `events` (sorted), `scripts` (count), `source`, and `pin_status`. A directory whose `hook.json` is missing or does not validate is still listed. Text description is `(broken)`, JSON adds `error` with the parse error, and a warning names `gridctl skill remove <name> --kind hook`. Pin status is still shown. An empty registry prints `No hooks in registry. Import some with 'gridctl pack add <repo>'.`

`gridctl skill info <name> --kind hook` prints the origin, then Description, Events, Scripts, and Pin. A definition that does not validate exits 1 with the parse error.

`gridctl skill remove <name> --kind hook` retires projections first, then deletes the registry directory and sidecar, trims the name from the pack record, and resets the pin. A row whose action is not `removed` or `already-gone` (`skipped-drift`, `skipped-unmanaged`, or `error`) keeps the hook. The command exits 1 with `hook <name> could not be unsynced on <client> (<action>); run 'gridctl skill project unsync --kind hook <name> --force' before removing`. If the projection manager cannot be built, the command exits 1 with `hook projection not available: <err>` and the registry hook stays. Removal checks that the directory exists. It does not require `hook.json` to validate. The source record stays when the pack still lists other hooks, rules, or a stack.

`gridctl pack remove` unsyncs pack-tagged hook projections, then deletes each pack hook's registry directory, sidecar, and pin record, and trims `Hooks` only when every unsync row for that hook is `removed` or `already-gone`. A hook with any other row (`skipped-unmanaged`, `skipped-drift`, or `error`) is kept whole (registry directory, pin, and pack record), listed in `kept`, and reported with the same refusal as `gridctl skill remove --kind hook`. `--force` clears drift only. `--dry-run` reports that keep instead of `would-remove`, and that preview exits 1. A hook that was never projected is still removed. User groups in the settings file are left in place.

## Projection

Two targets exist:

| Slug | Channel | Render | Destination | Script copy | Detected when |
|---|---|---|---|---|---|
| `claude-code` | `merge-key` | `identity` | matcher groups in `~/.claude/settings.json` | `~/.gridctl/hooks/<name>/` | `~/.claude` is a directory |
| `opencode` | `dedicated-file` | `lossy` | `~/.config/opencode/plugins/gridctl-hook-<name>.ts`, one file per hook | `~/.gridctl/hooks/<name>@opencode/` | `~/.config/opencode` is a directory |

Each path is the `~` template expanded against `GRIDCTL_HOME` when that variable is set, otherwise the OS home. A sync with no `--clients` writes every detected target. An undetected target is silent when another target is detected; when none is, each target is `skipped-unavailable` with detail `client not initialized on this machine`, as is a named `--clients` target that is not detected. Clients without a target are reported and not written (see [Clients gridctl does not write](#clients-gridctl-does-not-write)). `gridctl pack apply` and REST `POST /api/packs/{name}/apply` project hooks to every detected target and add one `skipped-unsupported` row per detected client without a target. `DELETE /api/packs/{name}` unsyncs those projections before it removes the registry hook. A hook whose unsync rows are not all `removed` or `already-gone` is kept, listed in `kept`, and reported with the same refusal as `gridctl skill remove --kind hook`. `force` clears drift only. `dry_run` reports the keep instead of `would-remove`. `DELETE /api/registry/hooks/{name}` uses the same refusal as `gridctl skill remove --kind hook`: `409` when a projection is not retired, and `503` (`Hook projection not available`) when the projection manager cannot be built. See [Hook Registry](api-reference.md#hook-registry), [Hook Projection](api-reference.md#hook-projection), and [Hook Pins](api-reference.md#hook-pins).

```bash
gridctl skill project sync --kind hook
gridctl skill project sync --kind hook block-force-push --clients claude-code
gridctl pack apply hook-pack
gridctl skill project status
gridctl skill project unsync --kind hook block-force-push
```

Exit `0` is a clean sync, `1` is any skipped or failed row (including `skipped-unexpressible`), and `2` is infrastructure (an unknown client or hook, or a newer lockfile). With no imported hooks and a detected target, sync prints `No hooks imported yet. Run 'gridctl pack add <repo>' to import hooks.` and exits 0. With no imported hooks and no detected target, sync still reports `skipped-unavailable` and exits 1.

Sync copies the registry hook directory into the target's script copy (directories `0700`, files `0600`, or `0700` when the source is executable). `.origin.json`, dot-prefixed entries, and `*.pre-*` backups are not copied. A previous copy whose tree hash differs from the recorded one is backed up under `~/.gridctl/project-backups/hook/<directory name>/` (`<name>` or `<name>@<client>`) before it is replaced, unless that copy was hand-edited. A hand-edited copy is drift. A removed or empty script directory is not a hand edit. Status reports `target-missing` (`script directory is gone` when the directory is absent, `script copy is empty` when the directory has no files). The next sync restores the copy (`updated`, detail `the script copy was removed`) and leaves the client file unchanged when it still matches.

Each client owns its own script copy. Claude Code keeps `~/.gridctl/hooks/<name>/`; every other target writes `~/.gridctl/hooks/<name>@<client>/`. `@` is outside the hook name charset, so no hook's copy collides with another hook's per-client copy. Each (hook, client) record names the directory it owns, and sync, status, unsync, and reset act on that directory only. Syncing one client never changes the bytes another client runs: after an approved script change is synced to `opencode` alone, the `claude-code` row is `stale` (`registry scripts differ from the synced copy`), not `drifted`, until it is synced too.

### Claude Code

Each matcher group is written under `hooks.<Event>` in the settings file. The handler `command` is the absolute path of the copied script. `args` is always present, including `[]` when the author set none, so Claude Code runs the handler in exec form and does not pass the home path through a shell. When the author set no `statusMessage`, sync writes `"statusMessage": "gridctl hook <name>"`. A `shell` key is refused at import, by name, because it would route the pinned script back through a shell.

The owned-group locator is the script path. A group under `hooks.<Event>` is a gridctl group for hook `<name>` when every handler has `type: "command"` and a `command` under that hook's script directory. The locator finds groups. It does not claim them. An unrecorded group, whatever its hash, is `skipped-foreign` with detail `settings groups already point at this hook's script directory but gridctl has no record of writing them` unless `--force`. A group with one handler under the script directory and one elsewhere is a hand edit and is `drifted`.

Before any settings write, sync verifies the pin. Registry bytes that do not match the approved pin are `skipped-pin-drift`. The remediation is `gridctl skill pins approve <name> --kind hook --expect <composite_hash>`. Neither the settings file nor the script copy changes until that approval. A settings file that `hujson` cannot parse is `skipped-unmanaged` with detail `settings file is not valid JSON or JSONC: <parse error>`. `--force` does not override that. A missing settings file is created with only a `hooks` object, and `created_file` is recorded. Unsync does not delete that file. When the last gridctl group goes and nothing remains under `hooks`, unsync removes the `hooks` key too, so a created file becomes an empty object.

Every settings write is an RFC 6902 patch on the parsed hujson tree, then `Pack`. gridctl does not call `Format` or `Standardize` on the tree it writes. Comments, trailing commas, key order, and every byte outside the patched elements stay as they were found. A timestamped backup `settings.json.gridctl-backup-<ts>` is written beside the file first, and three are retained. The patch may append a group (`/hooks/<Event>/-`), replace a located group, add an event array, or add the `hooks` key. On unsync it may remove a group, an emptied event array, or an emptied `hooks` key. Inserted groups are pretty-printed with the file's indent. A missing `hooks` key or event array is one pretty-printed value, placed on its own line, not a compact container glued to the previous value. The settings file's mode is preserved and is never widened past `0644`. The backup beside it is written with that same mode.

`project.lock.yaml` revision 4 records hook entries. For `claude-code`, `path` is `<settings path>#hooks/<name>` so several hooks share one file, and `channel` is `merge-key`. For a dedicated-file target, `path` and `config_path` are the generated file and `channel` is `dedicated-file`. `script_dir` is the script copy. `hashes` is the hash history of the owned region, newest last, capped at five: the canonical hash of the owned groups for `claude-code`, and `sha256:` plus the hex digest of the raw file bytes for a dedicated file. An older revision 3 file loads. Any rewrite of a lower revision, including a hook sync, stamps 4. A higher revision is kept. An older gridctl keeps a `hook` entry as an unknown kind and does not remove it.

The stored `channel` is for display. gridctl reads a record's channel from its target table by client slug on every sync, status, and unsync, never from the entry, so an entry rewritten by an older binary behaves the same after upgrade. No lockfile revision bump accompanies the OpenCode target.

### OpenCode

OpenCode has no hook file. It loads plugins, and discovers `.ts` and `.js` files placed directly under `~/.config/opencode/plugins/`. gridctl writes one generated plugin per hook there. It does not edit `opencode.json`, `opencode.jsonc`, or `cli.json`; discovery of the global plugins directory is the client's. A missing `plugins` directory is created at `0755`. The plugin is written atomically at mode `0644`: it carries script paths and arguments and no secret.

The plugin is render output, not pinned content. It is produced from a fixed template versioned by `openCodePluginTemplateVersion` (currently `1`), and its only inputs are the hook name, a JSON rule table, and the template version. The same pinned definition always yields the same bytes. The first line reads `// gridctl hook plugin v1 for hook "<name>". Generated from the pinned hook.json; do not edit, gridctl rewrites this file on sync.` Every value enters the plugin through `encoding/json`; no definition string is interpolated into code, and the hook name appears only in that header and in string literals. The template contains no `eval`, no string-built code, and no network access.

The plugin targets the OpenCode V2 plugin API (`import { Plugin } from "@opencode/plugin"`, `export default Plugin.define({ id: "gridctl.hook.<name>", async setup(ctx) { ... } })`). It registers `ctx.tool.hook("execute.before", ...)` for `PreToolUse` rules and `ctx.tool.hook("execute.after", ...)` for `PostToolUse` rules. A rule applies when its tool list is null (every tool) or contains the OpenCode tool ID. For each handler, in definition order, the plugin spawns the pinned script in exec form (`spawn(script, args, { shell: false })`) without blocking the server, writes a Claude-shaped payload to stdin, and collects up to one MiB each of stdout and stderr:

```json
{"hook_event_name": "PreToolUse", "session_id": "<sessionID>", "cwd": "<ctx.location.directory>", "tool_name": "Bash", "tool_input": {"command": "git push --force"}}
```

`tool_name` is the Claude name for the OpenCode tool. `PostToolUse` adds `tool_response` (the tool's result, or its error). For `read`, `write`, and `edit`, the adapter adds `file_path` beside OpenCode's `path` argument; every other argument key passes through unchanged. A `PreToolUse` call is denied when the script exits `2`, prints JSON with `hookSpecificOutput.permissionDecision` set to `deny`, or prints a top-level `decision` of `deny` or `block`. The plugin then throws `gridctl hook <name> denied <tool>: <reason>`, where the reason is `permissionDecisionReason`, `reason`, or the trimmed stderr, in that order, else `blocked by hook`. Any other outcome (spawn failure, another non-zero exit, stdout that is not JSON, or a timeout) is fail-open with one `console.warn` naming the hook, the script, and the outcome, matching Claude Code's non-blocking error behavior. `PostToolUse` results are ignored apart from that warning, and a `PostToolUse` exit `2` warns the same way because the tool has already run.

Every handler has a finite deadline. The handler's `timeout` (seconds) becomes `timeoutMs`; with no `timeout` the plugin uses 600 seconds (`openCodeDefaultTimeoutMs = 600000`), the default the Claude Code hooks reference documents for command hooks. A deadline above the largest `setTimeout` delay (2147483647 ms) is capped there. At the deadline the child is killed with `SIGKILL` and the call proceeds with a warning.

Only `PreToolUse` and `PostToolUse` map. A matcher must be absent, empty, `*`, or a plain `|` alternation of tool names in this table; any other token or pattern is unexpressible:

| Claude Code tool | OpenCode tool ID |
|---|---|
| `Bash` | `shell` |
| `Read` | `read` |
| `Write` | `write` |
| `Edit` | `edit` |
| `Glob` | `glob` |
| `Grep` | `grep` |
| `WebFetch` | `webfetch` |
| `WebSearch` | `websearch` |

A matcher group under any other event, or with an unmapped token or a regular expression matcher, is dropped for OpenCode and never approximated. A hook with no expressible group is one `skipped-unexpressible` row for `opencode` whose detail names each event or token (`event "Stop" has no opencode mapping`, `matcher token "Task" has no opencode tool`, `matcher "Notebook.*" is not a plain tool alternation`); nothing is written or recorded, and a direct sync exits 1. `gridctl pack apply` counts that row as applied, because the definition is not something the apply side can change. A hook with expressible and unexpressible groups is written with the expressible ones. Its row is `copied` or `updated` with `dropped groups: <event>; matcher "<token>"` in the detail, it does not by itself make the sync exit 1, and its status stays `in-sync` with the same detail, recomputed from the planned render on every call.

Handler keys the plugin does not carry are reported, never translated. `if` widens when the handler runs, so it is reported in its own words: `dropped if "Bash(git push *)": the handler runs on every matched call`. On every target except Claude Code, `if` is a widening drop: the handler runs on every call its matcher selects, so a script must re-check the condition itself. The Claude Code reference already requires that, because `if` is best-effort there. `statusMessage`, `async`, `asyncRewake`, `onFailure`, and any passthrough key are cosmetic and are reported as `lossy opencode render: dropped handler keys <k1>, <k2>`. These details appear on the sync row and on the `in-sync` status row.

Sync verifies the pin first, as for Claude Code: an unapproved registry change is `skipped-pin-drift`, and the plugin, the script copy, and the record stay as they were, even with `--force`. Ownership follows the agent projection decision table. With no record and no file, sync writes the plugin (`copied`). A file at the plugin path that gridctl did not record is `skipped-foreign` (`<path> exists but gridctl has no record of writing it; pass --force to claim it after a backup`); `--force` backs it up and claims it. A recorded file whose current hash is not in `hashes` is `skipped-drift` (`<path> was edited since gridctl wrote it`) and `drifted` in status; `--force` backs it up and rewrites it. A recorded file that is gone is rewritten (`updated`, detail `the projected file was removed`). Otherwise the row is `unchanged` when the planned bytes and the script copy match, else `updated`. Every replaced or removed plugin is backed up first under `~/.gridctl/project-backups/hook-file/opencode/<name>/<timestamp>-<basename>`, and three are retained.

`unsync --kind hook <name> --clients opencode` removes the plugin when its hash is recorded, or with `--force`, after a backup, then backs up and removes `~/.gridctl/hooks/<name>@opencode/` and purges the record. A missing plugin is `already-gone` and also purges. An unrecorded hash without `--force` is `skipped-drift`. The `claude-code` record and `~/.gridctl/hooks/<name>/` are untouched, and the reverse holds for `--clients claude-code`.

The template targets OpenCode V2 only. OpenCode 1 loads a different plugin package (`@opencode-ai/plugin`) and ignores this default export; gridctl does not detect the installed generation. The template was checked against the published `@opencode/plugin` 2.2.7 type declarations, which declare `execute.before` and `execute.after` callbacks of type `(input) => Promise<void> | void`. Manual acceptance remains: that a live OpenCode V2 session resolves the `@opencode/plugin` import for a discovered global `.ts` file with no `package.json` beside it, loads the plugin, and denies `git push --force` through the projected `block-force-push` hook, interactively and under `opencode run`, and what the model sees when `execute.before` throws.

### Rolling back past the OpenCode target

`gridctl upgrade --rollback` compares only the lockfile `version`, so it does not refuse a rollback to a release without the OpenCode target, and that binary reads the new records. It reports an `opencode` record as `drifted` (`settings file is not valid JSON or JSONC: ...`), its unsync returns `error` with `client is no longer a projection target`, and its unsync never deletes the plugin or `~/.gridctl/hooks/<name>@opencode/`. Its `gridctl reset --purge` still deletes `~/.gridctl`, script copy included, and leaves the plugin in `~/.config/opencode/plugins/` pointing at a missing script. Its next lockfile save stamps the entry `channel: merge-key` with a composite `path`; that changes nothing after re-upgrade, because the channel comes from the target table. Before rolling back, run `gridctl skill project unsync --kind hook --all --clients opencode` with the current binary, or re-upgrade later to retire the projection.

### Status and unsync

`gridctl skill project status` lists hook rows without a flag. Text columns are `HOOK (hook)`, `CLIENT`, `CHANNEL` (`merge-key` or `dedicated-file`), `RENDER` (`identity` or `lossy`), `STATE`, and `TARGET` (the settings path or the generated file). JSON rows carry `kind: "hook"`, `hook`, `client`, `channel`, `render`, `state`, `target`, `script_dir`, `pack`, `synced_at`, and optional `detail` and `remediation`.

| State | Meaning |
|---|---|
| `in-sync` | The settings groups or generated file and the script copy match what was synced, and the pin is clean. A lossy render's dropped groups and keys are in the detail. |
| `stale` | The registry bytes or the planned render differ from what was synced, including pin drift. The detail names which side moved. |
| `drifted` | The settings groups, the generated file, or the script copy were hand-edited. The detail names the script directory. |
| `target-missing` | The settings file, the located groups, the generated file, or the script directory is gone, or the client's detect directory is absent. |

Exit `0` is clean, `1` is any row that needs attention, and `2` is infrastructure.

`unsync --kind hook` removes located groups whose hash is one gridctl recorded, drops an event array that became empty, removes the `hooks` key when nothing remains under it, and leaves every sibling byte untouched. A `hooks` object that still holds a user group, a user event array, or any other member is kept. An empty one is removed whether or not gridctl created it (a deliberate exception to the removal rule in `CONSTITUTION.md` Article XVI, since an empty object carries no user content and the lockfile does not record who created it), so a file that had no `hooks` key before sync returns to its original bytes, except a trailing comma on the previous last member, which sync consumed as a separator and unsync does not restore (the result stays JWCC-equivalent). It backs up and removes the script copy, then purges the record. Groups gridctl did not record are never deleted. `--force` removes located groups whose hash is not recorded. A recorded entry whose groups are already gone is `already-gone`. With no recorded projections, `unsync --kind hook --all` prints `Nothing to unsync.` and exits 0. A named unsync of a hook that is not projected exits 1 with `hook is not projected: <name> (check 'gridctl skill project status')`.

`gridctl reset` unsyncs projected hooks after services are removed and daemons stop, and before the later deletions. It keys kept hooks by name: a hand edit on one client keeps that hook on every client unless `--force`, and each client's script copy goes only through that client's unsync. `--purge` retires in-sync settings groups and plugins before it deletes `~/.gridctl`. A hand-edited group or plugin is kept (`kept-drift`) unless `--force`, including under `--purge`. A hand-edited script copy is kept on the default tier; `--purge` still deletes it with `~/.gridctl`. A settings file that does not parse is reported as `failed` and left untouched; `--force` does not change that, and `--dry-run` shows the projection as `skipped`. The text preview lists that row under SKIPPED with its detail and does not count it as recreatable. The `hook` row's `script_dir` is the script copy. The pre-reset archive includes the settings file or plugin and that copy for a `would-remove` row.

`gridctl skill project adopt --kind hook` exits 1 with `adopt is not supported for hooks in this release; use --force to rewrite or unsync to remove`.

### Clients gridctl does not write

These clients read the Claude Code projection and have no separate target:

| Slug | Name | Native location | Detected when |
|---|---|---|---|
| `cursor` | Cursor | reads `~/.claude/settings.json` hooks (Cursor Settings → Agents → Third-Party Imports, on by default) | `~/.cursor` is a directory |
| `windsurf` | Windsurf (Devin Local) | `~/.config/devin/config.json` hooks key; reads `~/.claude/settings.json` hooks when `read_config_from.claude` is on (default) | `~/.codeium/windsurf`, `~/.config/devin`, or `~/.devin` is a directory |

These clients have a hook mechanism and are reported, not written:

| Slug | Native location | Detected when |
|---|---|---|
| `gemini` | `~/.gemini/settings.json hooks key` | `~/.gemini/settings.json` is a regular file |
| `copilot` | `~/.copilot/hooks/*.json` | `~/.copilot` is a directory |
| `antigravity` | `~/.gemini/config/hooks.json` | `~/.gemini/config` or `~/.gemini/antigravity` is a directory |

Gemini CLI is detected by its settings file, not by `~/.gemini`, because `~/.gemini` is also the parent of every Antigravity location: an Antigravity-only home gets no Gemini CLI note and no Gemini CLI row.

`--clients gemini` is one `skipped-unsupported` row whose detail is the native location, and that sync exits 1 because the client was requested. `--clients cursor` and `--clients windsurf` are `skipped-unsupported` with detail `covered by the claude-code projection; <native location>`. `--clients nonsense` exits 2. A sync with no `--clients` prints one stderr note per detected client: `note: <slug> reads the claude-code projection (~/.claude/settings.json); no separate target` for a covered client, else `note: <slug> (<native location>) has a hook mechanism but is not a projection target in this release`. That note does not change the exit code. `gridctl pack apply` prints one `skipped-unsupported` row per detected client in these tables, counts those rows in `Applied N/M`, and does not exit 1 for them alone. Exit 1 stays reserved for rows the user can act on. REST `unsupported_clients` rows carry `covered_by: "claude-code"` for a covered client.

### requires.hooks

A skill may declare hooks in frontmatter:

```yaml
metadata:
  requires:
    hooks: [block-force-push]
```

Skill `metadata` is stored as strings. Nested values become compact JSON, so the registry round trip emits `requires` as `{"hooks":["block-force-push"]}`, not as the author's nested YAML. An agent declares the same key under `metadata` in its frontmatter. The agent reader uses the raw YAML node. A value that is not a string array is ignored. Do not put this list in a skill's `hooks` frontmatter key. That key is the Claude Code passthrough and is still dropped on rendered agent targets.

`skill project status` and `sync` append one detail line per required hook that is not `in-sync` for that client: `requires hook <name>: not enforced on <client> (<reason>)`. Reason is `not projected`, `drifted`, `stale`, `pin drift`, or `client has no hook target`. A missing target is reported as `not projected`. A client with a hook target (`claude-code`, `opencode`) reports the hook's state there. `requires.hooks` reports enforcement of the expressible groups only: a hook that is `in-sync` on OpenCode with `dropped groups` in its detail counts as enforced, although the dropped groups do not run there. State, `needs_attention`, and exit codes do not change. Skill and agent JSON rows gain `requires_hooks` (declared names) and `unenforced_hooks` (names and reasons). Hook rows do not. If hook projection status cannot be read during sync, gridctl prints `warning: hook projection status unavailable: <err>` and skips those lines for that run. The sync exit code does not change.

Set `requires.gridctl` on a pack that selects hooks to `>=1.0.0-rc.5`, the release after 1.0.0-rc.4. An older `pack.Parse` ignores `hooks:` and imports the pack without them.

## Pins

Hook pins live at `~/.gridctl/pins/hooks/global.json` (file version `1`). They are not stack-scoped. The subdirectory keeps the file disjoint from the tool pin file of a stack named `hooks`. The import that installs a hook writes the pin. A later `pack add` or `skill update` that is accepted re-pins. A hand edit is pin drift until approved. Approval history is at `~/.gridctl/pins/approvals/hooks/global.json`.

Each record carries `definition_hash` (SHA-256 of `hook.json`), per-file digests, the `hook.json` text, provenance, findings, and status (`pinned` or `drift`). A file whose version is newer than `1` is refused with `hook pin file written by a newer gridctl`. Version `1` and a missing file load.

```bash
gridctl skill pins list --kind hook
gridctl skill pins verify --kind hook
gridctl skill pins diff block-force-push --kind hook
gridctl skill pins approve block-force-push --kind hook --expect <composite_hash> --reason "reviewed the script"
gridctl skill pins reset block-force-push --kind hook
```

`list` prints HOOK, STATUS, SOURCE, FILES, FINDINGS, and LAST VERIFIED. An empty store prints `No hook pins found.`, including on a machine with no deployed stack. `--format json` is an object with `schema_version` and `hooks`. Each record includes `composite_hash`, and `approvals` when any exist.

`verify` prints `hook pins verified` when every pin matches. `diff <name>` prints `<name> pin is current` when it matches. On drift it prints the definition hash change and `approve with: gridctl skill pins approve <name> --kind hook --expect <composite_hash> --reason "<why>"`. Diff JSON carries `schema_version`, `hook`, `status`, `composite_hash`, and `diff` when the bytes moved. It does not include `last_approval`.

`--format json` includes `schema_version`. Exit `0` is clean, `1` is drift or findings at or above `--fail-on-findings`, and `2` is infrastructure. `--reason` is required when the current scan still has findings (`hook "<name>" carries unresolved findings; pass --reason to approve over them`). It is optional when the current bytes scan clean. A clean approval clears findings recorded on an earlier pin. Repeat `--evidence` to attach an opaque reference. Those references are stored as text and never fetched. Approval history is appended to `~/.gridctl/pins/approvals/hooks/global.json` before the pin file is rewritten. Approve prints `Approved content update for hook <name>`. It does not print a daemon attribution line. Reset prints `Reset pin for hook <name>` and leaves the approvals log.

`--stack` does not apply. `gridctl skill pins list --kind hook` succeeds on a machine with no deployed stack. `gridctl skill pins list --kind hook --stack x` exits 2 with `--stack does not apply to hook pins` and does not resolve a stack. Diff, approve, and the composite-hash lookup refuse a name that is not lowercase letters, digits, and hyphens before they read a registry path.

## Hooks in the web UI

The Library workspace carries Skills, Agents, Hooks, Packs, and Proposals segments. Hooks are at `/library?kind=hook`. The list groups imported hooks by source and shows per-client projection chips. The inspector is read-only: edit a hook by re-importing its pack. The Definition tab shows the description, events, and matcher groups with the projected script path. The Projection tab shows one row per target projection, syncs and unsyncs that hook, reviews drift, and offers force sync after a backup. Clients without a target appear as muted chips from `unsupported_clients`, including clients that are not detected, with the native path in a tooltip. A covered client reads `<name> via claude-code`. The Pin tab shows pinned-versus-current state, advisory scan findings (code, file and line, and matched pattern, with non-printable characters escaped), added, removed, and modified files, a line diff of `hook.json`, and approve. A reason is required when findings exist. An unpinned hook says the next accepted import pins it, and approve stays disabled. The pack import wizard review step lists hooks by name and shows hook findings with non-printable characters escaped.

## What gridctl deliberately does not do

gridctl never executes a hook, a script, or `hook.json`. It stores, scans, pins, and places. It does not interpose in a client's hook lifecycle. It does not translate the Claude Code dialect into Gemini CLI, Copilot, or Antigravity; those clients are reported, not written. The OpenCode render carries `PreToolUse` and `PostToolUse` groups with mapped matchers and names everything else it drops; it does not translate `if`, regular expression matchers, or output JSON between dialects, and it does not render an OpenCode `policies` or `permissions` block. It does not adopt unrecorded groups. It does not reconcile hook projections from the daemon. A registry change stays `stale` until an explicit sync or `pack apply`. It does not project project-scope settings (`.claude/settings.json`, `.opencode/plugins/`, and the other clients' project files). It does not import hooks from pack external `sources:`, from `skill add`, or from a user's existing settings file.

Content a client executes automatically on an event is scanned before installation, content-pinned, and projected only from approved bytes. The guarantees are the scoped lint rule that denies `os/exec` under `pkg/hooksync` and `pkg/hookpins`, the reconcile regression test, and the four designated scenarios `hooks-settings-merge-preserves-siblings`, `hooks-sync-projects-pinned-bytes-only`, `hooks-reconcile-ignores-projections`, and `hooks-opencode-plugin-derives-from-pinned-definition`. No constitution amendment is included in this change.
