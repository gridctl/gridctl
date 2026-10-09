# Skills

Gridctl ships with a skill registry that delivers every active [`SKILL.md`](https://agentskills.io/specification) in your stack to upstream clients over two channels: as an MCP prompt served by the gateway, and (opt-in) as a file projection into each client's native skills directory via `gridctl skill project`. The Library workspace in the web UI is the authoring surface.

Skills are prose. Author them as markdown with agentskills.io-compliant frontmatter and store them in the registry directory. Which channel reaches a given client depends on the client: prompt-rendering clients (Gemini CLI, Cursor, Windsurf) see skills as invocable prompts, file-based clients (Antigravity, Grok Build) only see projected files, and several clients support both. See the per-client matrix below.

Agent Skills advertised by a remote [A2A Agent Card](config-schema.md#a2a) are
separate: the adapter exposes advisory `skill-*` message tools, not registry
documents, MCP prompts, or projected files.

## What a skill looks like

A skill is one directory under `~/.gridctl/registry/skills/<name>/`. `SKILL.md` is the only required file (frontmatter on top, markdown body below), and a prose skill needs nothing else. A skill may also ship supporting content the body refers to, in `scripts/`, `references/`, and `assets/`, which gridctl installs alongside it.

```markdown
---
name: incident-triage
description: Walk an SRE through the first 10 minutes of a production incident
state: active
---

# Incident triage

When an alert fires, work through this checklist in order. Don't skip steps even if you think you know the cause.

1. Confirm the alert is real. ...
2. Identify the blast radius. ...
3. Decide on a mitigation. ...
```

The frontmatter follows the [agentskills.io spec](https://agentskills.io/specification). gridctl adds one optional extension: `state:` (`draft` / `active` / `disabled`), which controls whether the registry serves the skill. Only `active` skills surface to MCP clients.

Frontmatter keys gridctl does not model (client extensions like `argument-hint` or `disable-model-invocation`) are preserved through import, sync, and editor saves. Gridctl never interprets them; they ride along so projected skills keep working in the clients that read them. Values and types are preserved; key ordering, comments, and scalar formatting (quoting, numeric representation) may normalize when gridctl rewrites the file.

## How skills reach the model

Two channels, complementary and per-client.

**MCP prompts (always on).** The registry implements the MCP `prompts/list` and `prompts/get` endpoints. A connected client that renders prompts sees every active skill as a prompt the user can invoke; `prompts/get` returns the post-frontmatter body verbatim. Prompts are user-invoked: the model does not discover them on its own. When the stack declares a `skills:` exposure policy, denied skills are filtered from both prompt and resource surfaces; see [Skill pins and exposure policy](#skill-pins-and-exposure-policy).

Downstream MCP servers that declare `prompts` or `resources` are aggregated beside the registry. Their prompts are named `<server>__<prompt>`. A prompt name that matches a registry skill keeps the registry entry and drops the downstream prompt. Resource URIs are not rewritten. Reads of `skills://registry/` and legacy `prompt://` still go to the registry. Client and group scoping apply to downstream servers, not to registry skills.

**File projection (opt-in).** `gridctl skill project sync <skill>` places selected active skills into native client skill directories, where clients that read skills from disk auto-trigger them from the frontmatter description. See [Projecting skills into clients](#projecting-skills-into-clients).

Not every linked client can use both channels, and some cannot use either:

| Client | MCP prompts | Projected files |
|---|---|---|
| Gemini CLI, Cursor, Windsurf | ✓ (slash commands / picker) | — (no projection target in v1) |
| Claude Code | ✓ | ✓ (`~/.claude/skills/`) |
| Zed, Goose, OpenCode, VS Code, Grok Build | varies | ✓ (`~/.agents/skills/`) |
| Antigravity | ✗ (tools-only MCP client) | ✓ (`~/.gemini/config/skills/`) |
| Claude Desktop | partial (prompt attachments) | ✗ (skills are account-level uploads) |
| AnythingLLM | ✗ (tools-only) | ✗ (plugin-based skills, no SKILL.md) |
| LM Studio | unverified (no documented prompt UI in chat) | ✗ (no skills directory; system prompt is per-chat) |

For Antigravity and Grok Build, projection is the only way gridctl skills reach the client at all.

There is no template expansion, no variable substitution, no execution layer. The body is the artifact. If you write `{{servername}}` in your skill, it surfaces to the client as the literal string `{{servername}}`; the client may choose to fill it in, but gridctl never does.

## Authoring in the Library workspace

The web UI's Library tab (⌘2 in the unified shell, also available as the detached `library-window` page) is the primary authoring surface.

- **List** every skill in the registry. Filter by state (`active` / `draft` / `disabled`) or by name.
- **Create** a new skill: gridctl prompts for the name, populates default frontmatter, and opens the editor on the body.
- **Edit** the body and frontmatter inline. The SkillEditor renders a side-by-side YAML form (for frontmatter) plus a markdown editor (for the body), with validation against the agentskills.io schema.
- **Activate / disable** a skill via the state badge. Disabled skills stay on disk but are dropped from `prompts/list` responses.
- **Delete** a skill: removes the directory from the registry.

The Library is backed by the REST endpoints under `/api/registry/skills/*` (see [`docs/api-reference.md`](./api-reference.md)). Everything you can do in the UI you can also do over HTTP.

The main Library and detached editor share [gateway credential re-entry](config-schema.md#browser-credentials). If a save succeeds but its follow-up registry refresh is rejected, the detached editor stays open for verification. Verification resumes the read without repeating the save.

## Authoring on the CLI

The same operations are exposed as CLI subcommands. Use these when scripting or working without the UI.

| Operation | Command |
|---|---|
| List skills | `gridctl skill list` |
| Show a skill's metadata | `gridctl skill info <name>` |
| Activate a draft skill | `gridctl activate <name>` |
| Validate a skill's frontmatter | `gridctl skill validate <name>` |
| Import skills from a git repo or local directory | `gridctl skill add <repo-url-or-directory>` |
| Import skills from a client home directory | `gridctl skill import <client>` (or `gridctl import <client> --kind skills,agents`) |
| Update imported skills (alias `sync`) | `gridctl skill update [name]` |
| Try a skill temporarily before importing | `gridctl skill try <repo-url>` |
| Pin an imported skill to a ref | `gridctl skill pin <name> <ref>` |
| Remove a skill | `gridctl skill remove <name>` |

See [`docs/cli-reference.md`](./cli-reference.md) for the full flag set.

## Git-imported skills

Skills don't have to be authored locally. `gridctl skill add <repo-url>` clones a remote repository, walks it for `SKILL.md` files, and pulls each one into the local registry. Pin to a ref with `gridctl skill pin`; refresh with `gridctl skill update` (also available as `gridctl skill sync` for parity with the Library page's "Sync sources" action). With no name argument, every imported skill is checked; pinned sources (tags like `v1.0.0` or full commit SHAs) are skipped unless updated explicitly. Sync preserves each skill's enable/disable state and refuses to overwrite locally-edited SKILL.md files unless `--force` is passed.

Import copies each skill's `scripts/`, `references/`, and `assets/` directories along with its `SKILL.md`, plus top-level `LICENSE`, `NOTICE`, and `COPYING` files, so a package whose instructions invoke a bundled script arrives able to run. Nothing else from the repository is copied: the copy is an allowlist, not an exclusion list, so a repository-root skill never drags in `.git` and a skill directory never absorbs a nested skill beside it. Symlinks are skipped, per-file size and per-skill file count are capped, and anything left out is reported as an import warning.

Re-importing replaces those three directories wholesale, so content deleted upstream does not linger. That means files you add by hand under `scripts/`, `references/`, or `assets/` are replaced on the next sync, since the registry has no way to tell a hand-added helper from an imported one. Everything outside them, including `SKILL.md` and its timestamped backups, is left alone.

Supported auth flows for private repos:

- `--vault-key <key>`: resolves the token from a `${var:KEY}` entry. The reference is persisted and re-resolved on a later update, which makes it the right form for a long-running daemon.
- `--auth-token-stdin`: reads an HTTPS personal access token from stdin, keeping it out of shell history and out of the process list. `--auth-token -` is equivalent. The token is not persisted.
- `--auth-token <pat>`: the same token as a literal argument, kept for CI ergonomics. A literal value prints a warning, because it lands in shell history and is visible to anyone who can run `ps`. The token is not persisted.
- `--ssh-key <path>`: SSH private key path. The absolute path is persisted (never the key material) and reused on a later update. Set `GRIDCTL_SSH_KEY_PASSPHRASE` for an encrypted key; the passphrase is re-read from the environment on every use and is not stored.

gridctl does not read `~/.ssh/config`, and a daemonized gridctl inherits `SSH_AUTH_SOCK` only from the shell that started it, so an SSH URL that works with the `git` CLI can still fail. See [troubleshooting](troubleshooting.md#ssh-agent-not-available).

### Reconciling local edits (web UI)

A `SKILL.md` imported from git can be edited in the Library workspace. An edited
file is "drifted" from its installed snapshot, and the same protection the CLI
applies (`gridctl skill update` refuses to overwrite a drifted skill unless
`--force`) now applies to the web API:

- `GET /api/skills/sources` reports drift: each source carries `driftedSkills`
  and each skill entry carries `hasLocalEdits`.
- `POST /api/skills/sources/{name}/update` and `POST /api/skills/sources/update`
  accept an optional body `{ "force": bool, "skills": [..] }`. Without `force`, a
  drifted skill is skipped (reported as `skipped: "local edits"`) while its
  version tracking is advanced to the latest upstream commit, so it stops showing
  as an available update but its on-disk content and drift status are preserved.
  With `force: true`, the current `SKILL.md` is copied to `SKILL.md.pre-<sha>`
  next to it before being overwritten.
- `GET /api/skills/sources/{name}/skills/{skill}/diff` returns the local vs
  upstream `SKILL.md` (plus a unified diff) without writing anything to disk.
- `POST /api/skills/sources/{name}/skills/{skill}/detach` removes the skill's
  origin sidecar and lock entry so it becomes local-only.
- `POST /api/skills/sources/{name}/skills/{skill}/reset` backs up and
  force-restores a single skill to its upstream content.

A local-directory source uses the same skip text and does not advance hashes. `--force` backs that file up as `SKILL.md.pre-local`. See [Local directories and client imports](#local-directories-and-client-imports).

Skill content is never changed by any of this beyond the explicit overwrite a
`reset` or `force` sync performs. Note that import and save do normalize
frontmatter formatting (field order, quoting) while preserving every key and
value, so the registry copy is not byte-identical to the upstream file.

## Local directories and client imports

`gridctl skill add <dir>` imports a directory that is not a git repository root. Discovery, validation, the security scan, size caps, and `--trust`, `--force`, `--no-activate`, and `--rename` behave as they do for git. A path that does not exist, or a file, is still treated as a git URL. A git repository root keeps the clone path. The directory is resolved to an absolute path. A path inside the gridctl home (`~/.gridctl`) is refused: `refusing to import from inside the gridctl home: <path>`. `--ref`, `--path`, and the auth flags are an error on this path (`local directories do not accept --ref, --path, or auth flags`).

The lock key defaults to the directory basename. Importing a known location root (a path in the table below, not a child of one) uses that row's source name instead. If another source already owns the name, the import fails before it writes: `source "skills" already records /other/path; pass --source-name to import this directory under another name`. `--source-name` chooses the key, including for a git URL whose derived name is already a local source. An explicit name that differs from the key already tracking that same directory is an error. Pack import has no equivalent flag and fails the collision before it writes.

Local origins record `kind: local`, the resolved path, and a content hash. They have no ref and no commit. `gridctl skill update` re-reads that path, including agents in a source that also has skills. An unchanged tree reports already up to date. `--dry-run` on a change reports `update available (local content changed)`. A changed `SKILL.md` or allowlisted supporting file is re-imported. A missing path, or a path that now resolves inside the gridctl home, is a per-entry error: a named update exits 1, and a bulk update warns and exits 0. Local sources are never pinned: `gridctl skill pin` exits 1 with `local sources have no refs to pin`. The background checker skips them, so a cached update badge does not cover a local directory.

When a skill's frontmatter name differs from its directory name, the local path installs under the directory name and warns: `name mismatch: frontmatter "x", directory "y"; installed as "y"`. The same fixture imported from git still installs under the frontmatter name. `gridctl skill list` shows `local-dir` in the Source column and the path in Repo. JSON adds `kind` (`git` or `local`) on imported rows. A skill with no origin stays `local` in that column and omits `kind`. `gridctl skill info` prints the path, content hash, and import time, plus `Client` and `Location` when the import recorded them.

`gridctl skill import <client>` reads home-scoped locations only. Project directories such as `.claude/skills` are not scanned; pass that path to `skill add`. `gridctl import <client> --kind skills,agents` runs the same importer, and `--kind all` also takes servers and context. `--trust` and `--no-activate` are accepted on both. Name-level `--select` and `--force` stay on `gridctl skill import`. A non-interactive `gridctl import` run without `--all` or `--yes` says to pass those flags.

| Client | Kind | Path | Source name |
|---|---|---|---|
| `claude-code` | skill | `~/.claude/skills` | `claude-code` |
| `claude-code` | agent | `~/.claude/agents` | `claude-code-agents` |
| `opencode` | skill | `~/.config/opencode/skills` | `opencode` |
| `opencode` | skill | `~/.claude/skills` | `claude-code` |
| `opencode` | skill | `~/.agents/skills` | `agents` |
| `opencode` | agent | `~/.config/opencode/agents` | listed and skipped |
| `agents` | skill | `~/.agents/skills` | `agents` |

Shared locations keep that row's source name whichever client you name. OpenCode agent files are skipped (`OpenCode agent dialect is not imported in this release`). Directories named `synced` or `anthropic-skills` under `~/.claude/skills` are skipped (`claude.ai sync directory`). A copy under the gridctl home, a path recorded in `project.lock.yaml`, or a directory that already carries `.origin.json` is skipped (`gridctl projection`). A dangling symlink is skipped (`dangling symlink`). A symlinked agent whose resolved parent is not named `agents` is skipped (`agent symlink target is not under an agents/ directory`). The same resolved skill or agent seen from a second client location is skipped (`duplicate`). A symlinked skill whose target is a direct child of a known skill location joins that root. Any other resolved skill target is its own root. External roots that share a basename are named `<name>-` plus the first eight hex characters of the path hash, and that name stays stable on re-import. `--dry-run` fills `source` with that key, including for an external root. An agent whose frontmatter `name` differs from the file name is selected by the file and installed under the frontmatter name.

```bash
gridctl skill add ~/skills/pcap-analysis
gridctl skill import claude-code --dry-run
gridctl skill import opencode --all
```

Text columns are `KIND NAME LOCATION ACTION`, with actions `imported`, `skipped: <reason>`, or `would import`. The summary counts distinct roots that imported something, not entries. `--dry-run` enumerates without writing or prompting. Without a terminal, pass `--all` or `--select`. Exit 0 means imported, dry-run, or nothing enumerated, including an all-skipped `--all` scan. Exit 1 means an unknown client, an unknown kind, a cancelled selection, an absent selected name, a source-name conflict, or every explicitly selected entry skipped on a real import. Exit 2 is an infrastructure error, including a newer lockfile. JSON (`--format json`) carries `schema_version`, `client`, `dry_run`, `entries` (`kind`, `name`, `location`, `source`, `action`, and `reason` when skipped), and `warnings`. `source` is the root's lock key and can differ from the client you named. Dry-run includes that key for external roots.

Importing a projection target, or a direct child of one (`~/.claude/skills`, `~/.agents/skills`, `~/.gemini/config/skills`, `~/.claude/agents`, `~/.config/opencode/agents`, `~/.copilot/agents`, `~/.gemini/agents`), or a path recorded in the project lock, prints one hint: `hint: projecting these to <client> would replace the originals; run 'gridctl skill project sync --client <client>' only after reviewing 'gridctl skill project status'`. A directory outside those roots does not. `~/.config/opencode/skills` is not a projection target, so importing it does not print that hint unless the path is also recorded.

A lockfile that contains a local source is stamped version 6. A lockfile without one keeps its previous stamp. An older gridctl refuses the whole file. Remove the local sources with this build before downgrading. A YAML `gridctl export --output` omits local sources from `skills.yaml` and prints `export: N local skill source(s) omitted from skills.yaml (local paths are not portable)`. JSON and stdout exports have no sidecar. `POST /api/skills/sources` accepts an absolute directory on the daemon host. The Library import wizard still accepts git URLs only.

## Projecting skills into clients

`gridctl skill project` syncs selected active skills into native client skill directories, so one managed library works in clients that never fetch MCP prompts and auto-triggers in clients that read skills from disk.

Nothing is projected by default. Unlike `gridctl ctx sync`, which projects one small file to every client, projecting all active skills would flood each client's skill discovery context, so the projection set is an explicit allow-list built by naming skills:

```bash
gridctl skill project sync incident-triage                      # every available target
gridctl skill project sync incident-triage --clients claude-code
gridctl skill project sync                                      # re-sync the recorded set
gridctl skill project status                                    # SKILL / CLIENT / CHANNEL / STATE / TARGET
gridctl skill project unsync incident-triage                    # remove one skill's projections
gridctl skill project unsync --all                              # remove everything
```

Three targets in v1:

| Slug | Directory | Channel | Notes |
|---|---|---|---|
| `agents` | `~/.agents/skills/` | symlink | Vendor-neutral interop dir (Zed, Goose, OpenCode, VS Code, Grok Build). Always available; created on first projection. |
| `claude-code` | `~/.claude/skills/` | symlink | Requires `~/.claude` to exist. |
| `antigravity` | `~/.gemini/config/skills/` | copy (forced) | Symlink discovery is unverified in Antigravity, so this target always copies. |

`skill project status` lists only recorded projections, unlike `gridctl ctx status`, which enumerates every known client including never-synced ones. The asymmetry is deliberate: the context canon targets all clients by default, while skill projection is an explicit allow-list, so an empty table here means "nothing projected", not "nothing detected".

Skill projection is CLI-only: there is no `/api/project/skills` REST surface. Agent projection does have one (`/api/project/agents/*`, see the [API reference](api-reference.md#agent-projection)); the skills half stays on the CLI and the daemon's own reconcile loop.

Skills are projected by symlink where possible: the link points into the registry, so registry edits propagate instantly and a projected skill can never drift. `--copy` materializes copies instead (and copy-forced targets always do); copies get tree-hash drift detection, and a hand-edited copy is skipped on sync until you decide with `--force` (overwrite after a timestamped backup) or `unsync` (remove it).

Ownership is tracked in `~/.gridctl/project.lock.yaml`, the unified projection lockfile shared with `gridctl ctx` (older installs migrate their `skillsync.lock.yaml` automatically on the next sync). A destination gridctl did not create (a skill installed by `npx skills`, or by hand) is never clobbered silently: sync skips it with guidance, `--force` backs it up first, and `unsync` refuses to touch it at all. Backups land under `~/.gridctl/skillsync-backups/<client>/<skill>/`, never inside the client's skills directory, so a backup can never surface in a client as a phantom skill. While the daemon runs, the projection set reconciles automatically after registry changes: deactivating, deleting, or updating a projected skill removes or refreshes its projections without a manual re-sync.

### Model preferences

Skill and agent authors can declare a model preference in frontmatter (top-level `model:`, or `metadata.preferred-model`/`metadata.model` for spec-portable placement). Gridctl surfaces the declaration in `gridctl skill list`, the REST API, and the Library (and, for rewritten projections, in `skill project status` as the `copy (model policy)` channel label and the JSON `model_value` field), together with a per-target honor matrix: Claude Code honors `model` on both skills (an inline, turn-scoped session override) and agents (a resolution input below the `CLAUDE_CODE_SUBAGENT_MODEL` env var and per-invocation parameters); the `agents` interop dir is consumer-dependent; Antigravity ignores it; rendered agent dialects drop it. A preference is exactly that: a durable default, never enforcement.

The stack-level [`model_preferences:` block](config-schema.md#model-preferences) adds policy: per-scope defaults and per-name overrides (raise or lower), applied when projections sync with `rewrite: true`. The rewrite lands in projected files only; the registry canonical never changes, so skill pins cannot drift from policy. Because a symlink cannot carry a rewrite, an affected skill projection is forced to copy channel and status names the reason (`copy (model policy)`); removing the policy (or `rewrite: false`) restores the symlink on the next policy-aware sync. The daemon applies the loaded stack's policy on every reconcile; on the CLI, pass `--stack <path>` to `skill project sync`. A sync without stack context never reverts a rewritten projection; status shows `model policy: unknown (no stack loaded)` until a policy-aware sync decides.

Pack interaction: packs re-import canonical skills and agents, and the next policy-aware projection re-stamps the operator's overrides, so a team default survives every pack update without hand-patching. Pack manifests do not carry model policy themselves; that stays operator stack config.

### Adopt a hand edit

`--force` is not the only way out of a drifted copy. When the edit is worth keeping, pull it back into the registry instead:

```bash
gridctl skill project adopt incident-triage --client antigravity
```

Adopt reads the projected copy, backs up the registry's current `SKILL.md` as `SKILL.md.pre-<sha>` (the same convention forced updates use), writes the changed files into the registry skill, and re-syncs that one (skill, client) pair so it returns to in-sync. Other clients projecting the skill go stale until the next `gridctl skill project sync`, which is correct: the canon changed. Note the singular `--client` flag: adopt operates on exactly one pair, unlike sync/unsync's `--clients`.

Adopted files count as local edits in the update flow: `gridctl skill update` sees the registry copy diverging from its import origin and refuses to overwrite it without `--force`, exactly as if the edit had been made in the registry directly. One hand-edit vocabulary, whichever side the edit landed on.

Symlinked projections have nothing to adopt (the registry copy is the source of truth; edits made through the link are already in the registry), and adopt refuses empty or invalid projected content rather than truncating a skill. Exit codes follow the family convention: `0` adopted, `1` nothing to adopt, `2` infrastructure error.

Projections carrying a model policy rewrite adopt safely: the model keys are policy-owned, so adopt restores them to the author's canonical declaration before write-back. A copy whose only delta is the policy rewrite adopts nothing, and a real edit adopts without ever writing the policy-resolved value into the registry.

Two caveats. Projecting the same skill to both `claude-code` and `agents` makes clients that scan both roots (Goose, OpenCode, VS Code) discover it twice; sync warns when you do this. And projection places the whole skill directory, including `scripts/`, on paths agents actively load, so only project skills whose supporting files you trust. The security scan runs at `skill add` and `skill update` time, not at projection time, and it is a pattern scan rather than a sandbox: it reads the `SKILL.md` body and any executable or script-extension file being installed, blocking on high-severity matches and reporting the rest as warnings. Treat installing a skill the way you would treat installing software: review what a package ships before projecting it.

## Skill pins and exposure policy

Skills are natural-language documents that agents obey, and after import nothing used to watch them: a git sync or an on-disk edit reached every linked client with no consent gate. Skill pins close that gap with the same trust-on-first-use model tool definitions get from [schema pins](./cli-reference.md#pins-tofu-schema-pinning).

**Pins.** When the daemon first observes a skill, it silently records per-file SHA-256 digests over the whole document set: the canonical `SKILL.md` (hashed on its parse-rendered form, so frontmatter normalization from imports and editor saves never manufactures drift) plus every supporting file. Any later content change flips the skill to **pin drift**, which persists until a human approves (re-pins) or resets — the daemon never auto-clears it. Pin drift is a different fact from the Library's sync drift (local edits vs the last git import); both can be true at once. Pins live at `~/.gridctl/pins/skills/<stack>.json`, and pinning has no toggle: silent first-pinning is its only effect until something changes.

**Advisory findings.** The poisoning heuristics that scan tool descriptions (P001–P005) also run over skill names, descriptions, and bodies, and persist on the pin record. Findings are advisory, never a gate: heuristic scanners are demonstrably bypassable, so the deterministic content hash is the enforcement mechanism and findings inform the human reviewing it. Prose trips heuristics more often than tool schemas do (a security-tutorial skill legitimately quotes attack phrasing); approving a skill that carries unresolved findings requires a `--reason`, persisted on the record. The `gateway.security.schema_pinning.scan` / `scan_ignore` knobs govern this scanner too.

**Review.** `gridctl skill pins list|verify|diff|approve|reset` mirrors the `gridctl pins` contract: `--format json` with a `schema_version`, exit codes `0` (clean) / `1` (pin drift) / `2` (infrastructure), an opt-in `--fail-on-findings warn|critical` gate for CI, and `approve --expect <composite_hash>` to bind an approval to the reviewed diff. The same surface exists at `/api/skill-pins`. Not to be confused with `gridctl skill pin <name> <ref>` (singular), which pins a git *source* to a ref.

**Exposure policy.** An optional `skills:` block in stack.yaml globally filters which registry skills are exposed and projected — see the [config schema](./config-schema.md#skills-exposure-policy). Denied skills disappear from `prompts/list`/`resources/list` and are skipped by projection sync, but they keep their registry state, stay visible in the Library and API flagged with the matching rule, and `gridctl apply` warns for every active skill the policy hides. Denial is never silent.

## Agent definitions

The import pipeline also understands Claude Code subagent definitions. `gridctl skill add` discovers any `agents/*.md` files (an `agents/` directory at the repo root or at any subdirectory root, the layout Claude Code plugin repos already use) alongside `SKILL.md` discovery, so a repo shipping `skills/` plus `agents/` imports as a unit:

```bash
gridctl skill add https://github.com/acme/agents
gridctl skill list --kind agent
```

Each imported agent lands verbatim at `~/.gridctl/registry/agents/<name>/AGENT.md` with the same `.origin.json` sidecar, lockfile tracking, and security scan skills get; the scan covers the agent body and frontmatter values (hooks and command strings live there), and findings gate the import behind the same `--trust` flow. An agent file must carry frontmatter with a `description`; the name comes from the `name` key or the filename stem, and must be lowercase letters, digits, and hyphens (no colons, which Claude Code refuses). Frontmatter beyond `name` and `description` (`tools`, `model`, `hooks`, `mcpServers`, `permissionMode`, vendor keys) passes through untouched: the stored file is byte-identical to the source.

Projection is per-kind and always a copy of the single file:

```bash
gridctl skill project sync --kind agent          # all imported agents
gridctl skill project sync --kind agent reviewer # or by name
```

Four targets exist, gated by client detection. `claude-code` is the identity target: the canonical bytes land verbatim at `~/.claude/agents/<name>.md`. (Cursor reads that directory too, so it gets agents for free and needs no target of its own. Verified against the shipped Cursor bundle rather than documentation: a path predicate matching `.claude/agents/` feeds Cursor's subagent descriptors, and gates their `deletable` flag off, so Cursor lists the agents but treats the files as read-only. Client behavior changes, so re-check that predicate before relying on it.) The `opencode` (`~/.config/opencode/agents/<name>.md`), `copilot` (`~/.copilot/agents/<name>.agent.md` — VS Code Copilot does not read `~/.claude/agents`; its global agents live here), and `gemini` (`~/.gemini/agents/<name>.md`) targets render the definition into each client's dialect. Renders are deterministic and lossy: frontmatter keys the dialect cannot express (Claude `tools` on OpenCode, `hooks`, `mcpServers`, `model`, vendor keys) are dropped and reported in sync output and the status RENDER column, and adopt is refused on rendered targets (adopt the `claude-code` projection instead, or unsync the pair and hand-maintain the client file). Ownership follows the dedicated-file model from `gridctl ctx`: a pre-existing hand-authored file at the destination is refused without `--force` (and backed up under `~/.gridctl/project-backups/agent/` with it), a hand-edited projection shows as drifted in `gridctl skill project status`, and `gridctl skill project adopt --kind agent <name> --client claude-code` pulls the edit back into the canonical store (backing up the prior `AGENT.md` as `AGENT.md.pre-<sha>`) instead of overwriting it. `gridctl skill update` then treats the adopted content as a local edit and refuses to clobber it without `--force`.

One Claude Code quirk worth knowing: it only watches agent directories that existed when the session started. If `~/.claude/agents` did not exist before the first `sync --kind agent`, restart Claude Code once to pick the agents up; subsequent syncs hot-reload.

The rendered dialects were verified against each client's documentation as of August 2026. They track formats gridctl does not own, so a client changing its format shows up as drift or a failed load in that client, never as damage to the canonical store.

### Agents in the web UI

The Library workspace carries a `Skills | Agents` segment (also reachable as `/library?kind=agent`). The Agents segment lists imported agents grouped by source with per-client projection chips, and each agent's inspector shows its frontmatter (with `tools` and `model` called out as per-client-translated), its body, and a Projection tab with the same state vocabulary the CLI prints: sync, unsync, and drift review per client, adopt on the identity target, and the lossy-render refusal spelled out with its alternatives when a rendered copy was hand-edited. Editing in the UI saves the whole file byte-verbatim behind the same blocking security scan imports run. The import wizard discovers agents alongside skills and lists them by name in the review step.

## What gridctl deliberately does not do

A short list of choices worth knowing about.

**Execution.** gridctl 0.1.x removed the typed-skill execution surface (TS sandbox, Go plugins, run ledger, approval gates, agent IDE). Skills are prose; upstream clients are responsible for using them. If you need an agent runtime, reach for LangGraph / CrewAI / AutoGen / OpenAI Agents SDK and let gridctl be the MCP gateway underneath. The retired surfaces were `gridctl agent {init,dev,build,validate}`, `gridctl run`, `gridctl runs *`, `/api/agent/*`, `/api/playground/*`, and the Stage / Runs / Playground UI workspaces.

**`kind:` in the frontmatter.** File presence used to be the discriminator between flavors. With execution removed there is only one skill flavor (prompt-only); a `kind:` field would carry no information. (Agents are a separate registry kind with their own `AGENT.md` convention, not a skill flavor.)

**Template expansion in the body.** The agentskills.io spec is permissive about body content; clients are free to interpret `{{...}}` placeholders however they like. gridctl does not template-expand them server-side; that policy belongs in the client, where the model and the conversation context live.

**A marketplace.** `gridctl skill add <git-repo>` is the closest thing, a per-repo distribution mechanism. There is no central index, by design; if you want to share skills, publish them as a git repo and others can `skill add` from it.

## References

- [agentskills.io specification](https://agentskills.io/specification): the SKILL.md schema gridctl reads.
- [`docs/api-reference.md`](./api-reference.md): the REST surface backing the Library workspace.
- [`docs/cli-reference.md`](./cli-reference.md): the CLI subcommands.
- [`docs/project-status.md`](./project-status.md): current stability tiers for skill features.
