# Hooks

A hook is a directory `hooks/<name>/` in a pack repository. It holds `hook.json` and the scripts that file names. `gridctl pack add` imports the hooks the manifest selects into `~/.gridctl/registry/hooks/<name>/`, scans them, and pins the bytes. gridctl does not run the scripts. The client does, later, with the user's privileges.

Projection into a client settings file is not available in this release. Import and pins are. A later release places the scripts and patches the client's settings file. Until then, `pack status` reports an imported hook as present in the registry and does not write a client file.

## hook.json

`hook.json` is a JSON object. The only accepted top-level keys are `name`, `description`, `dialect`, and `hooks`. Any other top-level key is refused by name.

| Field | Rule |
|---|---|
| `name` | Required. Lowercase letters, digits, and hyphens. Must equal the directory name. A mismatch is a malformed hook, named in a warning, and is not imported. |
| `description` | Required, non-empty. |
| `dialect` | Required. The only accepted value is `claude-code`. |
| `hooks` | Required object. Keys are Claude Code event names matching `^[A-Z][A-Za-z]+$`. Values are arrays of matcher groups. |

A matcher group is `{ "matcher"?: string, "hooks": [handler, ...] }`. `matcher` is optional. Unknown keys on a matcher group are refused by name.

A handler must have `type: "command"`. `http`, `mcp_tool`, `prompt`, and `agent` are refused, and the error names the type. A handler must not carry `env`, `headers`, `url`, `allowedEnvVars`, or `shell`. Each is refused by name. `shell` is refused because a projected handler runs in exec form. A `shell` key would send the pinned script back through a shell.

`command` must be a `./`-relative, slash-separated path with no `..` segment. It must resolve to a regular, executable file inside the hook directory. Symlinks, directories, absolute paths, and files outside the directory are refused. An inline shell string is refused with `hook handler commands must reference a script shipped in the hook directory`.

These handler keys pass through: `args`, `timeout`, `if`, `statusMessage`, `async`, `asyncRewake`, and `onFailure`. `args` must be an array of strings. `if` must be a string. Any other handler key passes through and is named in an import warning: `hook <name>: unrecognized handler key "<key>" passed through`.

### The `if` field

Confirmed against the Claude Code hooks reference (https://code.claude.com/docs/en/hooks). `if` is a permission-rule pattern the client matches against the tool call, for example `Bash(git *)`. It is not code the client evaluates. A string is stored as written. A non-string is refused by name, the same way `shell` is.

Any string anywhere in `hook.json` that contains `${var:`, `${secret:`, or `${vault:` is refused: `hook <name>: variable and secret references are not allowed in a hook definition`. Plain `$VAR` and `${VAR}` pass through unresolved. gridctl does not resolve them.

Hook directory contents follow the own-directory skill install rules. Dot-prefixed entries, symlinks, irregular files, and nested skill or hook directories are skipped with a warning. Executable modes are preserved. The per-file cap is 5 MiB, the file-count cap is 500, and the total-size cap is 50 MiB, the same values as skills.

## Pack selection

`gridctl-pack.yaml` takes an optional `hooks:` list of names from the pack repository. Empty or absent means none. Hooks are opt-in, like `rules:`. A `{name, source}` mapping is a validation error: hooks come only from the pack repository, not from external `sources:`. A selected name the repository does not ship is `unresolved` (`hooks:<name>`) and the pack add stays partial (exit 1).

A `hooks:` key is accepted on `gridctl.dev/v1` and `gridctl.dev/v1alpha1` with no warning.

`gridctl skill add` does not import hooks. When it discovers `hooks/<name>/hook.json` it prints `N hook definition(s) not imported; hooks import through a pack manifest (gridctl pack add)` and otherwise exits as it does today.

A pack record with a non-empty `Hooks` list stamps `skills.lock.yaml` version 9. Files without one keep their previous stamp. An older gridctl refuses a version 9 file with the existing "written by a newer gridctl" error. `gridctl-pack.lock.yaml` is unchanged. Hooks do not enter its digest.

## Import, scan, and trust

`gridctl pack add` copies the hook directory to `~/.gridctl/registry/hooks/<name>/` and writes a `.origin.json` sidecar (repo, ref, path, commit, imported time, content hash, installed hash, credential reference or key path, never a token).

The danger-pattern scanner runs over `hook.json` and every text file in the directory, sniffed by content rather than extension. Any finding of any severity blocks the import unless `--trust`. A warning such as `chmod 777` blocks a hook the same way a danger finding does. That is the agent rule, not the supporting-file rule. Every finding, including one accepted with `--trust`, is stored on the pin record.

`gridctl skill update` of a pack repository re-imports its hooks under the same scan and `--trust` gate. It refuses to overwrite a hook whose registry bytes differ from the installed hash unless `--force`. A forced overwrite keeps the previous `hook.json` as `hook.json.pre-<sha>`.

## Pins

Hook pins live at `~/.gridctl/pins/hooks.json` (file version `1`). They are not stack-scoped. The import that installs a hook writes the pin. A later `pack add` or `skill update` that is accepted re-pins. A hand edit is pin drift until approved. That path is also the tool pin file for a stack named `hooks`. Importing hooks on that stack, or pinning tools for it, overwrites the other store. Approval history is separate, at `~/.gridctl/pins/approvals/hooks/global.json`.

Each record carries `definition_hash` (SHA-256 of `hook.json`), per-file digests, the `hook.json` text, provenance, findings, and status (`pinned` or `drift`). A file whose version is newer than `1` is refused with the newer-gridctl error. Version `1` and a missing file load.

```bash
gridctl skill pins list --kind hook
gridctl skill pins verify --kind hook
gridctl skill pins diff block-force-push --kind hook
gridctl skill pins approve block-force-push --kind hook --expect <composite_hash> --reason "reviewed the script"
gridctl skill pins reset block-force-push --kind hook
```

`--format json` includes `schema_version`. Exit `0` is clean, `1` is drift or findings at or above `--fail-on-findings`, and `2` is infrastructure. `--reason` is required when the record carries unresolved findings. Repeat `--evidence` to attach an opaque reference. Those references are stored as text and never fetched. Approval history is appended to `~/.gridctl/pins/approvals/hooks/global.json` before the pin file is rewritten.

`--stack` does not apply. `gridctl skill pins list --kind hook` succeeds on a machine with no deployed stack. `gridctl skill pins list --kind hook --stack x` exits 2 with `--stack does not apply to hook pins` and does not resolve a stack.

## What this release does not do

gridctl does not execute a hook, translate it into another client's dialect, or write a client settings file. `gridctl skill project sync --kind hook` is not a command in this release.
