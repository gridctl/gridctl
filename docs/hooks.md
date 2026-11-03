# Hooks

A hook is a directory `hooks/<name>/` in a pack repository. It holds `hook.json` and the scripts that file names. `gridctl pack add` imports the hooks the manifest selects into `~/.gridctl/registry/hooks/<name>/`, scans them, and pins the bytes. gridctl does not run the scripts, and it does not write them into a client settings file.

Projection into a client settings file is not available. Import and pins are. `pack status` reports an imported hook as present in the registry and does not write a client file. The text row is `in-sync`. The detail line is `hook <name>: imported; projection is not available in this release`. JSON uses `state: "in-sync"` and the same detail. A name recorded on the pack but missing from the registry is `missing`, with detail `not in the hook registry; re-run 'gridctl pack add'`. A hook that was skipped at add is not recorded, so status does not list it.

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

`gridctl skill add` does not import hooks. When it imports skills or agents from a repository that also ships hook definitions, it prints `N hook definition(s) not imported; hooks import through a pack manifest (gridctl pack add)` and does not install them. A repository that has hook definitions and no importable skills or agents prints the same hint, then exits with the existing empty-repository error.

A successful import names a non-zero hook count in the import line: `Imported pack "<name>" (N skills, N agents, N hooks, wiring: yes)`. Rules and hooks are omitted from that line when the count is zero. `--dry-run` prints `Would import` and the same `Skipped:` line for a finding. It does not write the registry or the pin file. JSON omits `hooks` when none were imported.

A pack record with a non-empty `Hooks` list stamps `skills.lock.yaml` version 9. A skipped hook is not recorded, so that add keeps the previous stamp. Files without a recorded hook selection keep their previous stamp. An older gridctl refuses a version 9 file with `import lockfile was written by a newer gridctl version`. `gridctl-pack.lock.yaml` is unchanged. Hooks do not enter its digest.

## Import, scan, and trust

`gridctl pack add` copies the hook directory to `~/.gridctl/registry/hooks/<name>/` and writes a `.origin.json` sidecar (repo, ref, path, commit, imported time, content hash, installed hash, credential reference or key path, never a token).

The danger-pattern scanner runs over `hook.json` and every text file in the directory, sniffed by content rather than extension. Any finding of any severity skips that hook unless `--trust`. A warning such as `chmod 777` skips a hook the same way a danger finding does. That is the agent rule, not the supporting-file rule. The CLI prints `Skipped: <name> (hook): security findings detected (use --trust to proceed):` and exits 1. Other selected resources still import. The skipped hook is not installed and not pinned. With `--trust`, the hook imports, the finding is a `Warning:` line, and every finding is stored on the pin record. `POST /api/packs` is different: the same finding refuses the whole import with 409 before any write. See [Packs](api-reference.md#packs).

`gridctl skill update` of a skill imported from a pack repository re-imports that pack's hooks under the same scan and `--trust` gate. It prints `Updated hook` for each one it installs. It refuses to overwrite a hook whose registry bytes differ from the installed hash unless `--force`. A forced overwrite keeps the previous `hook.json` as `hook.json.pre-<sha>`. `pack add` may overwrite that drift and keeps the same backup.

## Inspect and remove

`gridctl skill list --kind hook` prints Name, Description, Events, Scripts, and Pin. JSON rows carry `kind: "hook"`, `name`, `description`, `events` (sorted), `scripts` (count), `source`, and `pin_status`. An empty registry prints `No hooks in registry. Import some with 'gridctl pack add <repo>'.`

`gridctl skill info <name> --kind hook` prints the origin, then Description, Events, Scripts, and Pin.

`gridctl skill remove <name> --kind hook` deletes the registry directory and sidecar, trims the name from the pack record, and resets the pin. There are no client projections to retire. The source record stays when the pack still lists other hooks, rules, or a stack.

`gridctl pack remove` deletes each pack hook's registry directory, sidecar, and pin record, then trims `Hooks`. `--dry-run` reports `would-remove` rows. It does not retire a client projection, because none was written.

`gridctl skill project sync --kind hook` is rejected before any projection work: exit 2, `unknown --kind "hook" (supported: skill, agent)`.

## Pins

Hook pins live at `~/.gridctl/pins/hooks.json` (file version `1`). They are not stack-scoped. The import that installs a hook writes the pin. A later `pack add` or `skill update` that is accepted re-pins. A hand edit is pin drift until approved. That path is also the tool pin file for a stack named `hooks`. Importing hooks on that stack, or pinning tools for it, overwrites the other store. Approval history is separate, at `~/.gridctl/pins/approvals/hooks/global.json`.

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

`--format json` includes `schema_version`. Exit `0` is clean, `1` is drift or findings at or above `--fail-on-findings`, and `2` is infrastructure. `--reason` is required when the record carries unresolved findings (`hook "<name>" carries unresolved findings; pass --reason to approve over them`). It is optional otherwise. Repeat `--evidence` to attach an opaque reference. Those references are stored as text and never fetched. Approval history is appended to `~/.gridctl/pins/approvals/hooks/global.json` before the pin file is rewritten. Approve prints `Approved content update for hook <name>`. It does not print a daemon attribution line. Reset prints `Reset pin for hook <name>` and leaves the approvals log.

`--stack` does not apply. `gridctl skill pins list --kind hook` succeeds on a machine with no deployed stack. `gridctl skill pins list --kind hook --stack x` exits 2 with `--stack does not apply to hook pins` and does not resolve a stack.

## What gridctl deliberately does not do

gridctl does not execute a hook and does not write a client settings file. `gridctl skill project --kind hook` is not a projection command.
