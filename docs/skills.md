# Skills

Gridctl ships with a skill registry that delivers every active [`SKILL.md`](https://agentskills.io/specification) in your stack to upstream clients over two channels: as an MCP prompt served by the gateway, and (opt-in) as a file projection into each client's native skills directory via `gridctl skill project`. The Library workspace in the web UI is the authoring surface.

Skills are prose. Author them as markdown with agentskills.io-compliant frontmatter and store them in the registry directory. Which channel reaches a given client depends on the client: prompt-rendering clients (Gemini CLI, Cursor, Windsurf) see skills as invocable prompts, file-based clients (Antigravity, Grok Build) only see projected files, and several clients support both. See the per-client matrix below.

## What a skill looks like

A skill is one directory under `~/.gridctl/registry/skills/<name>/` containing a single `SKILL.md` file. Frontmatter on top, markdown body below.

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

The bytes served to agents are never changed by any of this beyond the explicit
overwrite a `reset` or `force` sync performs.

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

Skills are projected by symlink where possible: the link points into the registry, so registry edits propagate instantly and a projected skill can never drift. `--copy` materializes copies instead (and copy-forced targets always do); copies get tree-hash drift detection, and a hand-edited copy is skipped on sync until you decide with `--force` (overwrite after a timestamped backup) or `unsync` (remove it).

Ownership is tracked in `~/.gridctl/skillsync.lock.yaml`. A destination gridctl did not create (a skill installed by `npx skills`, or by hand) is never clobbered silently: sync skips it with guidance, `--force` backs it up first, and `unsync` refuses to touch it at all. Backups land under `~/.gridctl/skillsync-backups/<client>/<skill>/`, never inside the client's skills directory, so a backup can never surface in a client as a phantom skill. While the daemon runs, the projection set reconciles automatically after registry changes: deactivating, deleting, or updating a projected skill removes or refreshes its projections without a manual re-sync.

Two caveats. Projecting the same skill to both `claude-code` and `agents` makes clients that scan both roots (Goose, OpenCode, VS Code) discover it twice; sync warns when you do this. And projection places the whole skill directory, including `scripts/`, on paths agents actively load, so only project skills whose supporting files you trust (the import-time security scan runs at `skill add` time, not at projection time).

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
