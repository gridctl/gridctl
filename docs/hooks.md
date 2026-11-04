# Hooks

A hook is a directory `hooks/<name>/` in a pack repository. It holds `hook.json` and the scripts that file names. `gridctl pack add` imports the hooks the manifest selects into `~/.gridctl/registry/hooks/<name>/`, scans them, and pins the bytes. gridctl does not run the scripts. `gridctl pack apply` and `gridctl skill project sync --kind hook` copy the scripts to `~/.gridctl/hooks/<name>/` and patch matcher groups into Claude Code's user settings file. Execution belongs to the client.

`pack status` reports a projected hook per client. A hook that was imported and never projected is a store-presence row (`in-sync`, detail `imported; not projected`). A name recorded on the pack but missing from the registry is `missing`, with detail `not in the hook registry; re-run 'gridctl pack add'`. A directory that is present but whose `hook.json` no longer validates is `drifted`. The detail is the parse error. The remediation is `review with 'gridctl skill pins diff <name> --kind hook' or remove with 'gridctl skill remove <name> --kind hook'`. A hook that was skipped at add is not recorded, so status does not list it.

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

`gridctl skill remove <name> --kind hook` retires projections first, then deletes the registry directory and sidecar, trims the name from the pack record, and resets the pin. A drifted projection is kept and the command exits 1 with `hook <name> is drifted on <client>; run 'gridctl skill project unsync --kind hook <name> --force' before removing`. Removal checks that the directory exists. It does not require `hook.json` to validate. The source record stays when the pack still lists other hooks, rules, or a stack.

`gridctl pack remove` unsyncs pack-tagged hook projections, then deletes each pack hook's registry directory, sidecar, and pin record, and trims `Hooks`. A drifted projection is kept unless `--force`. `--dry-run` reports `would-remove` rows. User groups in the settings file are left in place.

## Projection

One target exists: slug `claude-code`, settings file `~/.claude/settings.json`, available when `~/.claude` is a directory. The path is the `~` template expanded against `GRIDCTL_HOME` when that variable is set, otherwise the OS home. A missing `~/.claude` is `skipped-unavailable` with detail `client not initialized on this machine`. Other clients are reported and not written. `gridctl pack apply` projects hooks. REST `POST /api/packs/{name}/apply` and the web UI do not, and they add no hook rows. A REST delete removes the registry hook and does not unsync settings groups or the script copy.

```bash
gridctl skill project sync --kind hook
gridctl skill project sync --kind hook block-force-push --clients claude-code
gridctl pack apply hook-pack
gridctl skill project status
gridctl skill project unsync --kind hook block-force-push
```

Exit `0` is a clean sync, `1` is any skipped or failed row, and `2` is infrastructure (an unknown client or hook, or a newer lockfile). With no imported hooks and `~/.claude` present, sync prints `No hooks imported yet. Run 'gridctl pack add <repo>' to import hooks.` and exits 0. With no imported hooks and `~/.claude` absent, sync still reports `skipped-unavailable` and exits 1.

Sync copies the registry hook directory to `~/.gridctl/hooks/<name>/` (directories `0700`, files `0600`, or `0700` when the source is executable). `.origin.json`, dot-prefixed entries, and `*.pre-*` backups are not copied. A previous copy whose tree hash differs from the recorded one is backed up under `~/.gridctl/project-backups/hook/<name>/` before it is replaced, unless that copy was hand-edited. A hand-edited copy is drift. A removed or empty script directory is not a hand edit. Status reports `target-missing` (`script directory is gone` when the directory is absent, `script copy is empty` when the directory has no files). The next sync restores the copy (`updated`, detail `the script copy was removed`) and leaves the settings file unchanged when the recorded groups still match.

Each matcher group is written under `hooks.<Event>` in the settings file. The handler `command` is the absolute path of the copied script. `args` is always present, including `[]` when the author set none, so Claude Code runs the handler in exec form and does not pass the home path through a shell. When the author set no `statusMessage`, sync writes `"statusMessage": "gridctl hook <name>"`. A `shell` key is refused at import, by name, because it would route the pinned script back through a shell.

The owned-group locator is the script path. A group under `hooks.<Event>` is a gridctl group for hook `<name>` when every handler has `type: "command"` and a `command` under that hook's script directory. The locator finds groups. It does not claim them. An unrecorded group, whatever its hash, is `skipped-foreign` with detail `settings groups already point at this hook's script directory but gridctl has no record of writing them` unless `--force`. A group with one handler under the script directory and one elsewhere is a hand edit and is `drifted`.

Before any settings write, sync verifies the pin. Registry bytes that do not match the approved pin are `skipped-pin-drift`. The remediation is `gridctl skill pins approve <name> --kind hook --expect <composite_hash>`. Neither the settings file nor the script copy changes until that approval. A settings file that `hujson` cannot parse is `skipped-unmanaged` with detail `settings file is not valid JSON or JSONC: <parse error>`. `--force` does not override that. A missing settings file is created with only a `hooks` object, and `created_file` is recorded. Unsync does not delete that file. It leaves an empty `hooks` object when the last gridctl group goes.

Every settings write is an RFC 6902 patch on the parsed hujson tree, then `Pack`. gridctl does not call `Format` or `Standardize` on the tree it writes. Comments, trailing commas, key order, and every byte outside the patched elements stay as they were found. A timestamped backup `settings.json.gridctl-backup-<ts>` is written beside the file first, and three are retained. The patch may append a group (`/hooks/<Event>/-`), replace a located group, remove a group on unsync, add an event array, or add the `hooks` key. Inserted groups are pretty-printed with the file's indent. A missing `hooks` key or event array is one pretty-printed value, placed on its own line, not a compact container glued to the previous value. The settings file's mode is preserved and is never widened past `0644`. The backup beside it is written with that same mode.

`project.lock.yaml` revision 4 records hook entries. `Path` is `<settings path>#hooks/<name>` so several hooks share one file. `script_dir` is the script copy. `hashes` is the canonical hash history of the owned groups, newest last, capped at five. An older revision 3 file loads. Any rewrite of a lower revision, including a hook sync, stamps 4. A higher revision is kept. An older gridctl keeps a `hook` entry as an unknown kind and does not remove it.

### Status and unsync

`gridctl skill project status` lists hook rows without a flag. Text columns are `HOOK (hook)`, `CLIENT`, `CHANNEL` (`merge-key`), `RENDER` (`identity`), `STATE`, and `TARGET` (the settings path). JSON rows carry `kind: "hook"`, `hook`, `client`, `channel`, `render`, `state`, `target`, `script_dir`, `pack`, `synced_at`, and optional `detail` and `remediation`.

| State | Meaning |
|---|---|
| `in-sync` | The settings groups and the script copy match what was synced, and the pin is clean. |
| `stale` | The registry bytes or the planned render differ from what was synced, including pin drift. The detail names which side moved. |
| `drifted` | The settings groups or the script copy were hand-edited. The detail names the script directory. |
| `target-missing` | The settings file, the located groups, or the script directory is gone, or `~/.claude` is absent. |

Exit `0` is clean, `1` is any row that needs attention, and `2` is infrastructure.

`unsync --kind hook` removes located groups whose hash is one gridctl recorded, drops an event array that became empty, and leaves the `hooks` object and every sibling byte untouched. It backs up and removes the script copy, then purges the record. Groups gridctl did not record are never deleted. `--force` removes located groups whose hash is not recorded. A recorded entry whose groups are already gone is `already-gone`. With no recorded projections, `unsync --kind hook --all` prints `Nothing to unsync.` and exits 0. A named unsync of a hook that is not projected exits 1 with `hook is not projected: <name> (check 'gridctl skill project status')`.

`gridctl skill project adopt --kind hook` exits 1 with `adopt is not supported for hooks in this release; use --force to rewrite or unsync to remove`.

### Clients this release does not write

| Slug | Native location |
|---|---|
| `gemini` | `~/.gemini/settings.json hooks key` |
| `cursor` | `~/.cursor/hooks.json` |
| `copilot` | `~/.copilot/hooks/*.json` |
| `windsurf` | `~/.codeium/windsurf/hooks.json` |
| `antigravity` | `~/.gemini/config/hooks.json` |
| `opencode` | `plugins only (~/.config/opencode/plugins/)` |

`--clients gemini` is one `skipped-unsupported` row whose detail is that location, and that sync exits 1 because the client was requested. `--clients nonsense` exits 2. A sync with no `--clients` prints one stderr note per detected client from that table: `note: <slug> (<native location>) has a hook mechanism but is not a projection target in this release`. That note does not change the exit code. `gridctl pack apply` prints one `skipped-unsupported` row per detected client in this table, counts those rows in `Applied N/M`, and does not exit 1 for them alone. Exit 1 stays reserved for rows the user can act on.

### requires.hooks

A skill may declare hooks in frontmatter:

```yaml
metadata:
  requires:
    hooks: [block-force-push]
```

Skill `metadata` is stored as strings. Nested values become compact JSON, so the registry round trip emits `requires` as `{"hooks":["block-force-push"]}`, not as the author's nested YAML. An agent declares the same key under `metadata` in its frontmatter. The agent reader uses the raw YAML node. A value that is not a string array is ignored. Do not put this list in a skill's `hooks` frontmatter key. That key is the Claude Code passthrough and is still dropped on rendered agent targets.

`skill project status` and `sync` append one detail line per required hook that is not `in-sync` for that client: `requires hook <name>: not enforced on <client> (<reason>)`. Reason is `not projected`, `drifted`, `stale`, `pin drift`, or `client has no hook target`. A missing target is reported as `not projected`. State, `needs_attention`, and exit codes do not change. Skill and agent JSON rows gain `requires_hooks` (declared names) and `unenforced_hooks` (names and reasons). Hook rows do not. If hook projection status cannot be read during sync, gridctl prints `warning: hook projection status unavailable: <err>` and skips those lines for that run. The sync exit code does not change.

Set `requires.gridctl` on a pack that selects hooks to the release that ships this projection. An older `pack.Parse` ignores `hooks:` and imports the pack without them.

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

`--stack` does not apply. `gridctl skill pins list --kind hook` succeeds on a machine with no deployed stack. `gridctl skill pins list --kind hook --stack x` exits 2 with `--stack does not apply to hook pins` and does not resolve a stack.

## What gridctl deliberately does not do

gridctl never executes a hook, a script, or `hook.json`. It stores, scans, pins, and places. It does not interpose in a client's hook lifecycle. It does not translate the Claude Code dialect into Gemini CLI, Cursor, Copilot, Windsurf, Antigravity, or OpenCode. Those clients are reported, not written. It does not adopt unrecorded groups. It does not reconcile hook projections from the daemon. A registry change stays `stale` until an explicit sync or `pack apply`. It does not project project-scope settings (`.claude/settings.json` and the other clients' project files). It does not import hooks from pack external `sources:`, from `skill add`, or from a user's existing settings file.

Content a client executes automatically on an event is scanned before installation, content-pinned, and projected only from approved bytes. The guarantees are the scoped lint rule that denies `os/exec` under `pkg/hooksync` and `pkg/hookpins`, the reconcile regression test, and the three designated scenarios `hooks-settings-merge-preserves-siblings`, `hooks-sync-projects-pinned-bytes-only`, and `hooks-reconcile-ignores-projections`. No constitution amendment is included in this change.
