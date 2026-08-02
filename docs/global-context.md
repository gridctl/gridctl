# Global Context Sync

`gridctl ctx` maintains one canonical global agent-context file and syncs it to every linked client, so cross-project preferences (coding style, commit conventions, tone, tool preferences) are written once instead of duplicated by hand into `~/.claude/CLAUDE.md`, `~/.gemini/GEMINI.md`, and a dozen peers that drift apart.

Scope boundary: this manages only the **global** (user-level) layer. Per-project `AGENTS.md` files belong in each repository under version control, are read natively by most clients, and gridctl never touches them.

## The canonical file

The source of truth lives at `~/.gridctl/context/AGENTS.md`, plain markdown per the [agents.md](https://agents.md) spec. Because it is a spec-named file, AGENTS.md-native tools can read it directly, and it can be symlinked into a dotfiles repository for version control. What was written to each client, and from which canonical revision, is recorded in `~/.gridctl/project.lock.yaml`, the unified projection lockfile shared with `gridctl skill project`. Context operations hold the same cross-process lock as skill projections, so the CLI, the daemon, and the web UI can never interleave lockfile writes.

Older installs kept this state in `~/.gridctl/context/context.lock.yaml`. It migrates automatically on the first sync after upgrading: both legacy lockfiles are backed up to `~/.gridctl/project-migration-backup/<timestamp>/`, the unified file is written, and each legacy file is replaced by a version-2 tombstone. The tombstone is what makes a downgrade loud rather than confusing: a pre-unification gridctl refuses it with "written by a newer gridctl version" instead of silently working from stale state. `gridctl doctor` reports which lockfile generation is in use.

Keep the file short. Every client loads it into every session; durable preferences belong here, project-specific guidance does not.

## Quick start

```bash
gridctl ctx init                     # scan clients, bootstrap the canon (writes nothing during the scan)
gridctl ctx init --import claude-code   # or adopt your existing CLAUDE.md as the canon
gridctl ctx sync --dry-run           # preview per-client changes
gridctl ctx sync                     # propagate to every available client
gridctl ctx status                   # per-client sync state
```

The web UI offers the same surface, reachable from the Library workspace header ("Global Context") and from a Global Context tile in the Create Resource wizard. First run shows the adoption-first setup: existing client files are listed with their paths and sizes, and the first one found is preselected over the starter template. After that, the editor takes over: a resizable markdown/preview split with a formatting toolbar and live marker validation, a collapsible per-client state strip that opens itself when anything needs attention, sync-all, and a three-way drift dialog. The editor's Import action reopens the source picker at any time to replace the canonical file from a client file or the template (a timestamped backup precedes the write; `gridctl ctx init --import <client> --force` is the CLI equivalent). The same operations are exposed over REST; see the [API reference](api-reference.md#global-context).

## Write strategies

Each client receives the canonical content through the safest mechanism it supports. gridctl never takes unmarked ownership of a file the user also writes.

| Strategy | Clients | Mechanism |
|---|---|---|
| Dedicated file | Claude Code (`~/.claude/rules/gridctl.md`), Roo Code, Continue, VS Code Copilot | gridctl owns a whole file inside a rules directory the client reads. Zero merge risk. |
| Import shim | Gemini CLI (`~/.gemini/GEMINI.md`), Goose (`~/.config/goose/.goosehints`) | One `@`-import line referencing the canonical file is inserted; the rest of the file is never reordered or rewritten. Canonical edits flow through the reference without re-syncing. |
| Managed block | OpenCode, Zed, Cline, Grok Build (`~/.grok/AGENTS.md`), Antigravity, Windsurf | The full file is written when absent; when user content exists, a `<!-- BEGIN GRIDCTL MANAGED -->` … `<!-- END GRIDCTL MANAGED -->` block is inserted and only that block is ever rewritten. Windsurf's `global_rules.md` has a 6,000-character limit; oversized content is refused with a count. |

Not syncable, reported honestly in `ctx status` instead of worked around: Claude Desktop (instructions live in the app UI), Cursor (global User Rules are app-internal storage), and AnythingLLM (UI/API only). Antigravity's global path rests on unofficial documentation and is flagged experimental.

Every write is preceded by a timestamped backup (`<file>.gridctl-backup-<ts>`, three retained) and performed atomically. Managed content carries a header naming the source and the edit command, so a reader landing in the file knows where changes belong.

## Drift, staleness, and adoption

`ctx status` distinguishes two kinds of "out of date":

- **stale**: the canonical file changed since the last sync; the client's copy is intact but behind. `gridctl ctx sync` refreshes it.
- **drifted**: the client's managed content was hand-edited. Sync skips drifted targets with guidance instead of silently overwriting. Resolve with `gridctl ctx diff <client>` to inspect, `gridctl ctx adopt <client>` to make the edit the new canon, or `gridctl ctx sync --force <client>` to restore the canon.

For CI or a shell prompt, `gridctl ctx sync --check` performs no writes and exits `1` when anything is drifted, stale, or missing.

`ctx status` enumerates every known client, synced or not; `gridctl skill project status` lists only recorded projections. The asymmetry is deliberate and mirrors what each command manages: one canon that targets all clients by default versus an explicit per-skill allow-list.

## Rule fragments (opt-in)

By default `ctx` manages a single AGENTS.md. `gridctl ctx add <name>` activates **fragments mode**: the store becomes `~/.gridctl/context/fragments/*.md`, each an ordinary markdown file with optional YAML frontmatter (`description`, `paths:` glob list). On first add, the existing AGENTS.md is backed up and becomes `fragments/00-default.md` (no special casing afterward). Read-only commands (`status`, `sync --dry-run`, `diff`, `list`) never create the directory or migrate.

Composition order is **filename-lexicographic**; numeric prefixes (`00-`, `10-`) are the ordering control. Per client, projection is one of three modes (shown in `ctx status` when fragments are active):

| Mode | Clients | Behavior |
|---|---|---|
| multi-file | Claude Code (`~/.claude/rules/`), VS Code Copilot (`~/.copilot/instructions/`), Cline (`~/Documents/Cline/Rules/`), Roo (`~/.roo/rules/`) | Each fragment is its own lockfile-owned file (`gridctl-<name>.md`, or `.instructions.md` for Copilot). Unrecorded sibling files in those directories are foreign and never claimed or deleted. |
| compiled | Every other single-file target (OpenCode, Zed, Windsurf, Gemini/Goose shims, …) | Fragments are concatenated with `<!-- Source: fragments/<file> -->` attribution comments into the existing dedicated/shim/block strategy. Size caps (e.g. Windsurf 6,000) stay hard errors. |
| single-file | (fragments mode off) | Today's behavior, byte-identical until the first `ctx add`. |

Glob metadata: Copilot receives `applyTo` transformed from `paths:`; Claude Code keeps `paths:` as its own dialect (user-scope path scoping has known open bugs upstream; gridctl writes the format but does not claim it works); plain multi-file clients drop `paths:` and report the drop at fragment granularity. Cursor remains unsupported (no on-disk global rules).

| Command | Purpose |
|---|---|
| `gridctl ctx add <name>` | Create a fragment; activates fragments mode on first use |
| `gridctl ctx list` | Name, description, paths, size, composition position (`--format json`) |
| `gridctl ctx rm <name>` | Delete a fragment (backup first); projections drop on next sync |
| `gridctl ctx edit <fragment>` | Edit one fragment (bare `ctx edit` lists names when fragments are active) |
| `gridctl ctx diff <client> [fragment]` | Scoped multi-file diff, or per-fragment summary |
| `gridctl ctx adopt <client> <fragment>` | Lossless multi-file adopt |
| `gridctl ctx adopt <client> --into <name>` | Capture a compiled target into one fragment (refused without `--into`) |

Packs can ship fragments via `rules: [names]` in `gridctl-pack.yaml` (files under `rules/*.md` or `fragments/*.md` in the pack repo). Only the fragments a pack shipped are tagged with its name; your own fragments are projected alongside them but never claimed, and `pack remove` retracts by tag only.

## Removal

`gridctl ctx unsync [client|--all]` removes what gridctl manages and nothing else: dedicated files are deleted, shim lines and managed blocks are stripped, multi-file fragment projections are removed individually, and files gridctl created are removed entirely. User-owned content and unrecorded files in shared rules directories survive byte-for-byte.
