# Skills

Gridctl ships with a skill registry that delivers every active [`SKILL.md`](https://agentskills.io/specification) in your stack to upstream clients over two channels: as an MCP prompt served by the gateway, and (opt-in) as a file projection into each client's native skills directory via `gridctl skill project`. The Library workspace in the web UI is the authoring surface.

Skills are prose. Author them as markdown with agentskills.io-compliant frontmatter and store them in the registry directory. Which channel reaches a given client depends on the client: prompt-rendering clients (Gemini CLI, Cursor, Windsurf) see skills as invocable prompts, file-based clients (Antigravity, Grok Build) only see projected files, and several clients support both. See the per-client matrix below.

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

**MCP prompts (always on).** The registry implements the MCP `prompts/list` and `prompts/get` endpoints. A connected client that renders prompts sees every active skill as a prompt the user can invoke; `prompts/get` returns the post-frontmatter body verbatim. Prompts are user-invoked: the model does not discover them on its own.

**File projection (opt-in).** `gridctl skill project sync <skill>` places selected active skills into native client skill directories, where clients that read skills from disk auto-trigger them from the frontmatter description. See [Projecting skills into clients](#projecting-skills-into-clients).

Not every linked client can use both channels, and two cannot use either:

| Client | MCP prompts | Projected files |
|---|---|---|
| Gemini CLI, Cursor, Windsurf | ✓ (slash commands / picker) | — (no projection target in v1) |
| Claude Code | ✓ | ✓ (`~/.claude/skills/`) |
| Zed, Goose, OpenCode, VS Code, Grok Build | varies | ✓ (`~/.agents/skills/`) |
| Antigravity | ✗ (tools-only MCP client) | ✓ (`~/.gemini/config/skills/`) |
| Claude Desktop | partial (prompt attachments) | ✗ (skills are account-level uploads) |
| AnythingLLM | ✗ (tools-only) | ✗ (plugin-based skills, no SKILL.md) |

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

## Authoring on the CLI

The same operations are exposed as CLI subcommands. Use these when scripting or working without the UI.

| Operation | Command |
|---|---|
| List skills | `gridctl skill list` |
| Show a skill's metadata | `gridctl skill info <name>` |
| Activate a draft skill | `gridctl activate <name>` |
| Validate a skill's frontmatter | `gridctl skill validate <name>` |
| Import skills from a git repo | `gridctl skill add <repo-url>` |
| Update imported skills (alias `sync`) | `gridctl skill update [name]` |
| Pin an imported skill to a ref | `gridctl skill pin <name> <ref>` |
| Remove a skill | `gridctl skill remove <name>` |

See [`docs/cli-reference.md`](./cli-reference.md) for the full flag set.

## Git-imported skills

Skills don't have to be authored locally. `gridctl skill add <repo-url>` clones a remote repository, walks it for `SKILL.md` files, and pulls each one into the local registry. Pin to a ref with `gridctl skill pin`; refresh with `gridctl skill update` (also available as `gridctl skill sync` for parity with the Library page's "Sync sources" action). With no name argument, every imported skill is checked; pinned sources (tags like `v1.0.0` or full commit SHAs) are skipped unless updated explicitly. Sync preserves each skill's enable/disable state and refuses to overwrite locally-edited SKILL.md files unless `--force` is passed.

Import copies each skill's `scripts/`, `references/`, and `assets/` directories along with its `SKILL.md`, plus top-level `LICENSE`, `NOTICE`, and `COPYING` files, so a package whose instructions invoke a bundled script arrives able to run. Nothing else from the repository is copied: the copy is an allowlist, not an exclusion list, so a repository-root skill never drags in `.git` and a skill directory never absorbs a nested skill beside it. Symlinks are skipped, per-file size and per-skill file count are capped, and anything left out is reported as an import warning.

Re-importing replaces those three directories wholesale, so content deleted upstream does not linger. That means files you add by hand under `scripts/`, `references/`, or `assets/` are replaced on the next sync, since the registry has no way to tell a hand-added helper from an imported one. Everything outside them, including `SKILL.md` and its timestamped backups, is left alone.

Supported auth flows for private repos:

- `--auth-token <pat>`: an ephemeral HTTPS personal access token, suitable for CI.
- `--vault-key <key>`: resolves the token from a `${var:KEY}` entry; suitable for long-running daemons.
- `--ssh-key <path>`: SSH private key path.

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

Skill content is never changed by any of this beyond the explicit overwrite a
`reset` or `force` sync performs. Note that import and save do normalize
frontmatter formatting (field order, quoting) while preserving every key and
value, so the registry copy is not byte-identical to the upstream file.

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

Skills are projected by symlink where possible: the link points into the registry, so registry edits propagate instantly and a projected skill can never drift. `--copy` materializes copies instead (and copy-forced targets always do); copies get tree-hash drift detection, and a hand-edited copy is skipped on sync until you decide with `--force` (overwrite after a timestamped backup) or `unsync` (remove it).

Ownership is tracked in `~/.gridctl/project.lock.yaml`, the unified projection lockfile shared with `gridctl ctx` (older installs migrate their `skillsync.lock.yaml` automatically on the next sync). A destination gridctl did not create (a skill installed by `npx skills`, or by hand) is never clobbered silently: sync skips it with guidance, `--force` backs it up first, and `unsync` refuses to touch it at all. Backups land under `~/.gridctl/skillsync-backups/<client>/<skill>/`, never inside the client's skills directory, so a backup can never surface in a client as a phantom skill. While the daemon runs, the projection set reconciles automatically after registry changes: deactivating, deleting, or updating a projected skill removes or refreshes its projections without a manual re-sync.

### Adopt a hand edit

`--force` is not the only way out of a drifted copy. When the edit is worth keeping, pull it back into the registry instead:

```bash
gridctl skill project adopt incident-triage --client antigravity
```

Adopt reads the projected copy, backs up the registry's current `SKILL.md` as `SKILL.md.pre-<sha>` (the same convention forced updates use), writes the changed files into the registry skill, and re-syncs that one (skill, client) pair so it returns to in-sync. Other clients projecting the skill go stale until the next `gridctl skill project sync`, which is correct: the canon changed. Note the singular `--client` flag: adopt operates on exactly one pair, unlike sync/unsync's `--clients`.

Adopted files count as local edits in the update flow: `gridctl skill update` sees the registry copy diverging from its import origin and refuses to overwrite it without `--force`, exactly as if the edit had been made in the registry directly. One hand-edit vocabulary, whichever side the edit landed on.

Symlinked projections have nothing to adopt (the registry copy is the source of truth; edits made through the link are already in the registry), and adopt refuses empty or invalid projected content rather than truncating a skill. Exit codes follow the family convention: `0` adopted, `1` nothing to adopt, `2` infrastructure error.

Two caveats. Projecting the same skill to both `claude-code` and `agents` makes clients that scan both roots (Goose, OpenCode, VS Code) discover it twice; sync warns when you do this. And projection places the whole skill directory, including `scripts/`, on paths agents actively load, so only project skills whose supporting files you trust. The security scan runs at `skill add` and `skill update` time, not at projection time, and it is a pattern scan rather than a sandbox: it reads the `SKILL.md` body and any executable or script-extension file being installed, blocking on high-severity matches and reporting the rest as warnings. Treat installing a skill the way you would treat installing software: review what a package ships before projecting it.

## Agent definitions (experimental)

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

This writes `~/.claude/agents/<name>.md`. Claude Code is the only render target in this slice, but Cursor and VS Code Copilot read `.claude/agents` natively, so one projection serves all three. Ownership follows the dedicated-file model from `gridctl ctx`: a pre-existing hand-authored file at the destination is refused without `--force` (and backed up under `~/.gridctl/project-backups/agent/` with it), a hand-edited projection shows as drifted in `gridctl skill project status`, and `gridctl skill project adopt --kind agent <name> --client claude-code` pulls the edit back into the canonical store (backing up the prior `AGENT.md` as `AGENT.md.pre-<sha>`) instead of overwriting it. `gridctl skill update` then treats the adopted content as a local edit and refuses to clobber it without `--force`.

One Claude Code quirk worth knowing: it only watches agent directories that existed when the session started. If `~/.claude/agents` did not exist before the first `sync --kind agent`, restart Claude Code once to pick the agents up; subsequent syncs hot-reload.

## What gridctl deliberately does not do

A short list of choices worth knowing about.

**Execution.** gridctl 0.1.x removed the typed-skill execution surface (TS sandbox, Go plugins, run ledger, approval gates, agent IDE). Skills are prose; upstream clients are responsible for using them. If you need an agent runtime, reach for LangGraph / CrewAI / AutoGen / OpenAI Agents SDK and let gridctl be the MCP gateway underneath. The retired surfaces were `gridctl agent {init,dev,build,validate}`, `gridctl run`, `gridctl runs *`, `/api/agent/*`, `/api/playground/*`, and the Stage / Runs / Playground UI workspaces.

**`kind:` in the frontmatter.** File presence used to be the discriminator between flavors. With execution removed there is only one flavor (prompt-only); a `kind:` field would carry no information.

**Template expansion in the body.** The agentskills.io spec is permissive about body content; clients are free to interpret `{{...}}` placeholders however they like. gridctl does not template-expand them server-side; that policy belongs in the client, where the model and the conversation context live.

**A marketplace.** `gridctl skill add <git-repo>` is the closest thing, a per-repo distribution mechanism. There is no central index, by design; if you want to share skills, publish them as a git repo and others can `skill add` from it.

## References

- [agentskills.io specification](https://agentskills.io/specification): the SKILL.md schema gridctl reads.
- [`docs/api-reference.md`](./api-reference.md): the REST surface backing the Library workspace.
- [`docs/cli-reference.md`](./cli-reference.md): the CLI subcommands.
- [`docs/project-status.md`](./project-status.md): current stability tiers for skill features.
