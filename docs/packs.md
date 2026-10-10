# Packs

A pack is a git repository carrying a `gridctl-pack.yaml` manifest at its root: a versioned selector over the repo's skills, agents, rule fragments, gateway wiring, and an optional stack file, so one import configures a whole team setup. Skills and agents may also come from git repositories named under `sources:`. Rules and the stack still come from the pack repository ([Sources](#sources)). Packs are a thin composition layer. `pack add` runs the same origin pipeline as `gridctl skill add` for skills and agents (security scan, `--trust` gate, drift-safe updates); rule fragments get the same blocking scan and `--trust` gate, and `pack add` refreshes a rule whose content changed upstream, provided you have not edited it locally. A rule you have edited is reported as locally modified and left alone; take the pack's version with `gridctl ctx rm <name>` followed by `pack add`, which discards your copy. Rules installed before gridctl recorded per-rule provenance are treated as locally modified until their next `pack add` records it. `skill update` still does not cover rules; `pack add` is their update path. `pack apply` drives the same projection engines as `gridctl skill project sync` (see the [Skills guide](skills.md)), `gridctl ctx sync` (see [Global Context Sync](global-context.md)), and `gridctl project sync --kind wiring`, scoped to the pack. When the manifest names a stack, that apply starts the gateway from a pinned checkout first ([Carrying a stack](#carrying-a-stack)). Every projection a pack applies is tagged with the pack name in `~/.gridctl/project.lock.yaml`, which is what makes `pack status` and cascade removal exact. Per-command flags and exit codes are in the [CLI reference](cli-reference.md#packs).

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
stack: stack.yaml                    # optional stack file in this repository; empty means no gateway
variables:
  GITHUB_TOKEN:
    required: true
    secret: true
    type: string
    description: Token used by the GitHub tools
    docs: https://docs.github.com/authentication
```

`gridctl.dev/v1alpha1` is the pre-1.0 spelling of the same schema and stays accepted indefinitely, so packs authored before the graduation import unchanged; write `gridctl.dev/v1` in new manifests.

Skills follow the `SKILL.md` convention and agents the `agents/*.md` convention, exactly as plain skill repos do — a pack repo is a skill repo plus a manifest. Rule fragments live under `rules/*.md` or `fragments/*.md` (filename base is the fragment name). Names the manifest selects but the matching repository does not ship are reported as `unresolved` (exit 1) and kept in the pack record so status stays honest. A string entry is matched against the pack repository. A `{name, source}` entry is matched against that source. Unlike skills/agents, an empty `rules:` list means **none** (rules are opt-in). A rule whose name collides with a local fragment of different content is skipped, never overwritten; identical content installs idempotently. A first rule install that activates fragments mode migrates AGENTS.md with an explicit printed message, exactly like `ctx add`.

Field names follow the Claude Code plugin.json family where the semantics match, so a pack maps onto that ecosystem rather than fighting it. The word "bundle" is deliberately avoided: the MCP ecosystem uses it for `.mcpb`, a single-server archive format.

The optional `variables:` map uses the same value-free declaration fields as
`stack.yaml`: `required`, `secret`, `type`, `description`, and `docs`, with
defaults of false, true, and string for the first three fields. Import records
the declarations and lists unmet required keys with `gridctl var set KEY`
commands. It never imports a value, writes the variable store, or prompts.
A carried stack is pinned and deployed, not edited. Packs without declarations,
a stack record, or an unresolved-detail map retain the version-two lock shape.
Version three is used when declarations must be represented and the file has
no stack record, no unresolved-detail map, and no SSH fields. A source imported with `--ssh-key` raises the stamp to version four when
the file has no stack record and no unresolved-detail map. A resolved
stack, or an unresolved stack that records a detail, stamps version five
on the whole import lockfile, so a reader that only understands SSH fields
refuses the file instead of dropping the stack key or the detail map. That
refusal also blocks unrelated skill updates in the same file. Remove the
pack with this build before downgrading; an older binary cannot, because
it refuses the file. A pack with neither a stack record nor an
unresolved-detail map keeps the previous stamp. A pack that pins external
sources, or a member source recorded for one, stamps version 7 on the whole
import lockfile. An older gridctl refuses that file, including unrelated
skill updates. Remove the pack with this build before downgrading. Packs
without `sources:` keep their previous stamp.

## Sources

A pack can select skills and agents from other git repositories. Declare each repository once under `sources:`, then point a selection at it with `{name, source}`. A string entry is still a name from the pack repository. Empty `skills:` or `agents:` still means every resource discovered in the pack repository only. That import-all skips a skill that already exists under another source instead of overwriting it. External sources are never import-all. A skill or agent name may appear only once when `sources:` is declared. The error names the name and both owners. Packs without `sources:` are not checked for repeated string names.

```yaml
sources:
  netops:
    repo: git@gitlab.example.com:network/netops-copilot.git
    ref: v2.3.0
    auth:
      method: ssh-key
      ssh_user: git
  skillsbench:
    repo: https://github.com/benchflow-ai/skillsbench
    ref: 1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b
    path: tasks/dapt-intrusion-detection/environment/skills
skills:
  - incident-triage
  - { name: noc-l2-agent, source: netops }
agents:
  - { name: neteng-reviewer, source: netops }
```

`repo` must be `https://`, `http://`, `ssh://`, or `user@host:path`. `git://`, `file://`, and filesystem paths are refused. One repository URL per source, and one source per repository: two refs of one URL cannot share the clone cache. Source names use the pack name rules. `path` is a relative directory inside that repository. `rules:` and `stack:` still come only from the pack repository. A source whose clone root contains `gridctl-pack.yaml` is refused: packs cannot depend on packs. A mapping selection must include both `name` and `source`; a missing key is a parse error that names the line. Validation errors exit 2 and name the offending key, or the selection line. Accepted `auth.method` values are `ssh-key`, `ssh-agent`, and `token`. A manifest `auth` block cannot carry `ssh_key_path`; that key is refused by name.

`gridctl pack add` clones the pack, then each source in name order (a Go map does not keep manifest order, and order is not a contract). It resolves each selection against the matching clone, scans every clone, and imports the pack repository first. Each external source is then imported with exactly the names resolved for it, which may be none. Text output prints `Sources:` after the import line. A pinned source is `<name> <repo>@<ref> (<short sha>)`, or `<name> <repo> (<short sha>)` when `ref` is empty. A failed source is `<name> <repo>: <error>`, and an authentication failure also prints the same hint as `skill add`. A source that fails to clone, is itself a pack, whose recorded commit cannot be restored, or whose import returns an error becomes `unresolved` (`skill/<name>` or `agent/<name>`). If the clone cache moves between clone and import, the recorded commit is checked out again. A head that still differs is unresolved, and the detail names the expected and actual short SHAs, rather than importing the wrong tree. The rest still import, the pack record is still written with that source listed and empty lists, and the command exits 1. A declared source that fails is exit 1 even when no selection names it. An earlier source's resources stay. A later failure does not roll back earlier sources. Re-run `pack add` to reconcile. Printed source URLs drop embedded userinfo. The lockfile keeps the authored URL, matching the primary pack source.

Auth for each source, first match wins: `--source-auth <name>=vault-key:<KEY>` or `--source-auth <name>=ssh-key:<path>`; stored auth on the member lock source; the manifest `auth` block (`credential_ref` resolves through the variable store, `ssh-key` without a path means ssh-agent with `ssh_user`, and `token` without `credential_ref` is an error for that source); the caller's `--ssh-key` when the source URL is SSH and nothing above applied; otherwise ambient. `--vault-key`, `--auth-token`, and `--auth-token-stdin` apply to the pack repository only. They are never sent to an external host. A persisted member records a vault reference or an ssh key path, never a token or passphrase. An unknown `--source-auth` name is a warning: `source-auth "<name>" does not match a declared source`. The web wizard does not collect per-source credentials; it uses stored member auth and the manifest `auth` block. Pass `sourceAuth` on `POST /api/packs` when a source needs a one-shot override. An unknown name there is the same warning, not an HTTP error.

`pack status` adds a `source` row per pinned source, after the stack row when one exists and before skills, in name order. `in-sync` means pinned as recorded, not verified current. `stale` is attention: the background update cache lists a newer commit for one of that source's skills, and the remediation is `re-run 'gridctl pack add <pack-repo>'` (the pack repository, not the external source). The cache skips any origin with a `credential_ref` and walks skills only, never agents, so a vault-token source or an agents-only source never reports `stale`. Those rows stay `in-sync` and append `, freshness not checked`. A source that failed at add is still listed that way: the error was on the add output, and the status row does not keep it. Its detail is the repository and ref, with an empty short SHA when the clone never recorded one. `skill update <member-skill>` refreshes the member's recorded names only, even when the external repository ships more. It does not import the rest of that repository. `skill add <other-repo> --source-name <pack>/<source>` is refused when the repository differs, and the error names the pack. Moving a declared source to a new repository under the same name is the same refusal, including from `pack add`. The member lock source keeps the pinned URL, and status does not advertise the URL that was never pinned. Run `gridctl pack remove <pack>`, then `gridctl pack add`, to point that source at the new repository. A source is still a repository the pack author chose, pinned by commit. There is no index. `GET /api/skills/sources` lists member sources under their `<pack>/<source>` key with `packMember` set. They are not hidden. A full `pack remove` drops those lock sources.

## Verbs

| Command | Purpose |
|---|---|
| `gridctl pack add <repo-url>` | Clone, read the manifest, and import exactly its selection into the registry (`--ref`, `--path`, `--trust`, `--dry-run`, `--format json`, repeatable `--source-auth`). `--path` scopes discovery to a subdirectory, matching the REST `path` field; the manifest is always read from the repository root. A `stack:` entry is resolved against the clone root, not `--path`. Auth flags for the pack repository: `--vault-key <key>`, `--auth-token-stdin`, `--auth-token <pat>`, `--ssh-key <path>`. `--source-auth` is the per-source override. Exit `0` clean, `1` partial (unresolved, skipped, or a failed source), `2` infrastructure. |
| `gridctl pack apply <name>` | Start a carried stack from its pinned checkout, then project skills and agents through the projection engines, rule fragments through `ctx` (pack-tagged), and (when `wiring: true`) the gateway entry through the wiring ownership manager, scoped to `clients:`. A skill or agent from an external source is projected with the pack tag, the same as one from the pack repository. Wiring for a stack-carrying pack uses that daemon's port and never another running gateway. Packs without `stack:` keep the previous "no running gateway" skip. Additive, never transactional (`Applied N/M`). `-p` / `--port` (default 8180), `--force` (replaces a same-named daemon whose state file is outside `~/.gridctl/packs/<name>/`; a daemon under that directory is replaced without `--force`), `--dry-run`, `--clients`, `--format json`. |
| `gridctl pack status [name]` | Per-resource state in the shared vocabulary (in-sync, stale, drifted, target-missing, foreign, missing) plus `unresolved` rows. A carried stack is the first row: `in-sync`, `stale`, `drifted`, `missing` (not attention), or `target-missing`. Pinned external sources are `source` rows after that, before skills. Exit `0`/`1`/`2`. |
| `gridctl pack remove <name>` | Stop a daemon whose state file is under this pack's checkout, delete `~/.gridctl/packs/<name>/`, then cascade removal: projections unsynced from client trees (rule fragment projections by pack tag only), wiring records removed through the ownership manager (entries gridctl did not record are never deleted), then the pack's registry skills, agents, and installed fragments, then the pack record. A full removal also drops member lock sources (`<pack>/<source>`), including names those member records still own after a failed re-add. `--dry-run` adds `would-remove` rows for those sources only when nothing is kept. A drift-kept pack does not report them. A same-named daemon running from anywhere else is left running and gets no stack row on a real remove; the checkout is still deleted. `--dry-run` reports `would-remove` for that checkout and says the daemon is left running. A failed stop, or a remove that cannot stop a running pack-owned daemon, leaves the pack record and checkout in place so remove can be retried. Drifted projections are kept with a remediation hint unless `--force`; a partial removal trims the pack record to what stayed, including each external source's recorded names. `--dry-run`, `--format json`. |

## Private pack repositories

A pack is a git repository, so a private one needs credentials the same way an imported skill source does. `gridctl pack add` takes the same flags as `gridctl skill add` for the pack repository. `--vault-key`, `--auth-token`, and `--auth-token-stdin` are never sent to an external source. `--ssh-key` also applies to an SSH source that resolved no other auth. `--source-auth` is the per-source override ([Sources](#sources)).

- `--vault-key <key>`: resolves the token from a `${var:KEY}` vault entry. Prefer this for HTTPS. The reference is persisted, and a later `pack add` with no auth flags reuses it, including wiring-only packs and rules.
- `--auth-token-stdin`: reads the token from stdin, keeping it out of shell history and out of the process list. `--auth-token -` does the same thing. The token is not persisted.
- `--auth-token <pat>`: an ephemeral HTTPS token, kept for CI ergonomics. Passing a literal value prints a warning, because the value lands in your shell history and is visible to anyone who can run `ps`. The token is not persisted.
- `--ssh-key <path>`: an SSH private key path. The absolute path is persisted (never key material), and a later `pack add` with no auth flags reuses it, including wiring-only packs and rules. Set `GRIDCTL_SSH_KEY_PASSPHRASE` if the key is encrypted; the passphrase is re-read from the environment and is not stored.

A pack imported with `--vault-key GIT_TOKEN` records `${var:GIT_TOKEN}` in the import lockfile and in each resource's origin sidecar, never the token value, and a later `gridctl skill update` or `gridctl pack add` with no auth flags re-resolves it. A pack imported with `--ssh-key` records the absolute key path in those same files, never the key or the passphrase. A pack imported with a literal or piped token records neither, by design, so its next update falls back to ambient credentials.

Over REST, `POST /api/packs` and `POST /api/packs/preview` accept the same optional `auth` object the skill source endpoints take. Omit it on a repository that was already imported with a `--vault-key` reference or an `--ssh-key` path and the stored auth is used automatically, which is how the web UI's update dialog previews a private pack without asking for anything. A relative `sshKeyPath` is rejected, because the daemon's working directory is not the caller's. A token in that object is not sent to an external source. `sourceAuth` is the per-source override; omitted sources use stored member auth, then the manifest `auth` block.

### The daemon and ssh-agent

`gridctl apply` and `gridctl serve` daemonize by re-spawning with the environment of the shell that launched them, so the daemon has a usable `SSH_AUTH_SOCK` only if that shell did, and a long-running daemon can outlive the agent it inherited. Every import driven from the web UI or the REST API runs in the daemon's environment, not in the shell of whoever is using the browser. If you rely on an agent, start it before the daemon and restart the daemon after restarting the agent.

gridctl does not read `~/.ssh/config`. Per-host `IdentityFile` entries have no effect, which is why an SSH URL that works with the `git` CLI can still fail here. For a private pack the dependable options are an HTTPS URL with `--vault-key`, or `--ssh-key` naming the key explicitly.

## Carrying a stack

An optional `stack:` field names a stack file inside the same repository (`stack: stack.yaml`). The path is slash-separated, relative, and must stay inside the repo. `pack add` reads it without expanding variables or vault values. An inline credential in a recognized sensitive field, a missing file, or an `extends` chain or source path that leaves the repository is recorded as unresolved (`stack:<path>`) and the rest of the pack still imports. Nothing from the carried stack is expanded against the vault at add time. Relative `file:` paths and volume sources that start with `./` or `../` resolve against the stack file in the pinned checkout when that daemon starts. An absolute `file` path is kept as written and is not rewritten into the checkout. A `~` volume source still resolves against the home directory, not the checkout. `pack add` refuses a config `file` that escapes the repository, is missing (a directory counts as missing), or needs `~` or variable expansion before it can be anchored. That row is unresolved (`stack:<path>`) and the rest of the pack still imports.

When the stack resolves, `pack add` copies the pinned commit to `~/.gridctl/packs/<pack-name>/<commit-sha>/`. `pack apply` starts the daemon from that checkout, never from the shared clone cache. The copy skips symlinks, refuses paths that resolve outside the clone, and fails the import of the stack (not the rest of the pack) if the repository exceeds 5 MiB per file, 5000 files, or 64 MiB total. Directories are mode `0700`; files are `0600`, or `0700` when the source was executable. A later `pack add` of the same commit reuses the checkout when the stack file is still there.

`pack apply` runs the stack step first. It starts the daemon, leaves it unchanged when the running state file already points at the pinned stack, or replaces a daemon whose state file lies under `~/.gridctl/packs/<pack-name>/`. That last case is a path heuristic: the state file records `StackFile` and no pack owner, so a daemon you started yourself with `gridctl apply` on a path inside that directory is replaced too. A same-named daemon running from any other path is skipped (`skipped-running`, detail `stack '<name>' is already running from <path>`) unless you pass `--force`. Required pack variables that are unset skip the launch (`skipped-variables`) and list one `gridctl var set KEY` per key. `--dry-run` reports `would-start`, `would-replace`, or `unchanged` and does not call the launcher. Deploy writes nothing of its own to stdout; the pack table is the outcome.

Wiring uses that daemon's port on `started`, `replaced`, and `unchanged`, and on dry-run `would-start` or `would-replace` when a port is already known. A dry-run start with no known port reports `would-link` and does not call the wiring engine. Every other stack outcome skips wiring with detail `pack stack is not running` and remediation `resolve the stack row above, then re-run 'gridctl pack apply <name>'`.

`pack status` puts the stack row first. `in-sync` means the running `StackFile` is the pinned path. `stale` means it is running from an older checkout under the pack directory (attention; the detail names that commit when known, and the remediation is `re-run 'gridctl pack apply <name>'`). `drifted` means a same-named daemon is running from any other path (attention; the detail names that path). `missing` means nothing is running and is not attention. `target-missing` means the checkout or stack file is gone (attention; remediation `re-run 'gridctl pack add'`). A stack-only pack reports applied when the stack is `in-sync` or `stale`. A projected pack without a stack is unchanged.

After a successful replace, other commit directories under that pack that no running daemon references are removed. `pack remove` stops a daemon only when its `StackFile` is under the pack checkout, then deletes `~/.gridctl/packs/<pack-name>/`. A stop or checkout-delete failure returns before the pack record is trimmed, so the next remove can retry. A same-named daemon running from elsewhere is not stopped. A real remove gets no stack row and still deletes the pack checkout, because that daemon's stack file is not under it. `--dry-run` reports `would-remove` for the checkout and says the running daemon is not pack-owned and is left running.

The launcher is CLI-only. `POST /api/packs/{name}/apply` and the web UI return `skipped-unavailable` with detail `stack deploy is not available over this interface` and remediation `run 'gridctl pack apply <name>' from the CLI`. They do not start the gateway. `DELETE /api/packs/{name}` cannot stop a daemon either. If a pack-owned daemon is running, that row is `skipped-unavailable` with remediation `gridctl destroy <name>`, and the daemon, checkout, and pack record all stay so `gridctl pack remove` can finish the job. A REST dry-run still reports `would-remove` and a stop for that daemon; the preview does not mean REST can perform the stop. Stop the daemon with `gridctl pack remove` before removing the pack from the web UI. A carried stack's own `link:` block is ignored; pack `wiring:` and `clients:` apply instead, and add warns `stack declares link:; pack wiring (wiring:/clients:) applies instead and link: is ignored under pack apply`. `gridctl status`, `gridctl destroy <name>`, `gridctl reload`, and `gridctl logs` work on the pinned checkout the same way they work on a stack you applied yourself. Updates go through replace, not hot reload.

## Interplay with the standalone verbs

Packs add bookkeeping, not a second write path. `gridctl skill project sync`, `gridctl skill update`, and `gridctl project sync --kind wiring` keep working on pack-managed resources; a plain re-sync never strips the pack tag. One pack owns a resource at a time: applying a pack over a resource tagged by another pack refuses that resource until one manifest gives it up.

## REST and web UI

Projection and import are available over HTTP: `GET /api/packs` (list), `GET /api/packs/{name}` (detail with per-resource state rows), `POST /api/packs` (add from git, behind the same blocking security scan; also the update path against an already-imported origin; a resolved stack is pinned and not started), `POST /api/packs/preview` (read-only manifest resolution, no checkout), `POST /api/packs/{name}/apply` (`clients`, `force`, and `dry_run` only; no `port`, and no daemon start), and `DELETE /api/packs/{name}` (cascade preview and removal; no daemon stop). See the [API reference](api-reference.md#packs). The web UI's Library workspace surfaces installed packs in its Packs segment, lists pinned external sources under the origin (name, repository, ref, and short commit), labels a preview resource from an external source as `<name> from <source>`, and shows the stack row and its remediation. The import wizard does not collect per-source credentials. It has no deploy button.

Status rows for rules report per-client projection state once a pack is applied (drift and staleness per fragment-file projection; a compiled client's whole-document state stays in `gridctl ctx status`), with a store-presence row for a rule that was imported but never projected.

## What packs deliberately do not have

No enable/disable state (imported and projected are the only states), no inter-pack dependencies (an external source is a git repository the pack author declared, not another pack, and it is not discovered), no interactive configuration prompts (secrets flow through the existing `${var:KEY}` vault mechanism), no gateway start or stop from the REST API or the web UI, and no marketplace or index (`pack add` points at a git repo you chose, with the same trust gate as `skill add`, and each external source is pinned by commit).

See `examples/portable-pack/` for a complete pack repo layout.
