# Skills

Gridctl ships with a skill registry that exposes every active [`SKILL.md`](https://agentskills.io/specification) in your stack as an MCP prompt to upstream clients. The Library workspace in the web UI is the authoring surface; the gateway's `prompts/list` and `prompts/get` MCP endpoints are the serving surface.

Skills are prose. Author them as markdown with agentskills.io-compliant frontmatter, store them in the registry directory, and any MCP client connected to gridctl (Claude Desktop, Claude Code, Cursor, Codex, etc.) sees them as prompts the user can invoke.

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

The registry implements the MCP `prompts/list` and `prompts/get` endpoints. When an upstream client connects to gridctl, it sees every active skill as a prompt. The user picks the prompt; the client sends `prompts/get`; the gateway returns the post-frontmatter body verbatim.

There is no template expansion, no variable substitution, no execution layer. The body is the artifact. If you write `{{servername}}` in your skill, it surfaces to the client as the literal string `{{servername}}` — the client may choose to fill it in, but gridctl never does.

## Authoring in the Library workspace

The web UI's Library tab (⌘2 in the unified shell, also available as the detached `library-window` page) is the primary authoring surface.

- **List** every skill in the registry. Filter by state (`active` / `draft` / `disabled`) or by name.
- **Create** a new skill — gridctl prompts for the name, populates default frontmatter, and opens the editor on the body.
- **Edit** the body and frontmatter inline. The SkillEditor renders a side-by-side YAML form (for frontmatter) plus a markdown editor (for the body), with validation against the agentskills.io schema.
- **Activate / disable** a skill via the state badge. Disabled skills stay on disk but are dropped from `prompts/list` responses.
- **Delete** a skill — removes the directory from the registry.

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

- `--auth-token <pat>` — an ephemeral HTTPS personal access token, suitable for CI.
- `--vault-key <key>` — resolves the token from a `${var:KEY}` entry; suitable for long-running daemons.
- `--ssh-key <path>` — SSH private key path.

## What gridctl deliberately does not do

A short list of choices worth knowing about.

**Execution.** gridctl 0.1.x removed the typed-skill execution surface (TS sandbox, Go plugins, run ledger, approval gates, agent IDE). Skills are prose; upstream clients are responsible for using them. If you need an agent runtime, reach for LangGraph / CrewAI / AutoGen / OpenAI Agents SDK and let gridctl be the MCP gateway underneath. The retired surfaces were `gridctl agent {init,dev,build,validate}`, `gridctl run`, `gridctl runs *`, `/api/agent/*`, `/api/playground/*`, and the Stage / Runs / Playground UI workspaces.

**`kind:` in the frontmatter.** File presence used to be the discriminator between flavors. With execution removed there is only one flavor (prompt-only); a `kind:` field would carry no information.

**Template expansion in the body.** The agentskills.io spec is permissive about body content; clients are free to interpret `{{...}}` placeholders however they like. gridctl does not template-expand them server-side — that policy belongs in the client, where the model and the conversation context live.

**A marketplace.** `gridctl skill add <git-repo>` is the closest thing — a per-repo distribution mechanism. There is no central index, by design; if you want to share skills, publish them as a git repo and others can `skill add` from it.

## References

- [agentskills.io specification](https://agentskills.io/specification) — the SKILL.md schema gridctl reads.
- [`docs/api-reference.md`](./api-reference.md) — the REST surface backing the Library workspace.
- [`docs/cli-reference.md`](./cli-reference.md) — the CLI subcommands.
- [`docs/project-status.md`](./project-status.md) — current stability tiers for skill features.
