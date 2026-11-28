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
gridctl import opencode --kind context --dry-run   # preview adoption without overwriting
gridctl ctx sync --dry-run           # preview per-client changes
gridctl ctx sync                     # propagate to every available client
gridctl ctx status                   # per-client sync state
```

`gridctl import <client> --kind context` adopts a client file only when no canonical file exists and fragments mode is off. It never passes `--force`. An existing canonical file is skipped; overwrite it with `gridctl ctx init --import <client> --force`. A home that already has `fragments/` is skipped too, because a canonical file written there would be ignored by `gridctl ctx sync`. Pass `--context-fragment <name>` to add a fragment instead. That flag is explicit: the first fragment migrates the single-file store, the same way `gridctl ctx add` does. A missing file, a file empty after stripping gridctl-managed content, or content the canonical write would refuse is skipped on dry run and on apply. That skip exits 1 when context is the only requested kind, and 0 when another kind was requested.

The web UI offers the same surface as the Library's Context segment (`/library?kind=context`), reachable from the kind switch on every Library segment, the command palette ("Go to Global Context"), the Global Context tile in the Create new wizard, and the Connections "Review" action. First run shows the adoption-first setup in the center pane: existing client files are listed with their paths and sizes, and the first one found is preselected over the starter template. After that, the editor takes over with a resizable markdown and preview split, a formatting toolbar, live marker validation, sync-all, a three-way drift dialog, and a right-rail inspector with Clients and Details tabs (`?tab=details`). The Clients tab keeps every client's projection state, mode chip, and actions visible beside the editor without a collapsible strip above it, and a one-shot `?client=<slug>` link from Connections opens the drift review when the target is unambiguous or spotlights that client's row before removing the parameter. The Details tab displays read-only metadata and pin status with a "Review in Pins" link. The drift dialog's Adopt, Capture, and Overwrite actions stay disabled until the diff loads; the diff shows added and removed lines with non-printable characters escaped, and a failed load shows the error with Retry and the `gridctl ctx diff`, `adopt`, and `sync --force` commands that match the actions offered for that client. The editor's Import action reopens the source picker at any time to replace the canonical file from a client file or the template (a timestamped backup precedes the write; `gridctl ctx init --import <client> --force` is the CLI equivalent). In fragments mode the segment adds a fragment rail in composition order (add, delete, and a globs badge for path-scoped fragments) beside the editor pane, syncs the selected fragment to the URL as `?fragment=<name>`, and the single-file editor's Fragments action performs the same explicit migration as `ctx add`. Saving a canonical file or fragment whose pin drifts adds a "Review in Pins" action to the success toast. Removing a fragment asks first. The confirm names the fragment and states that a backup is saved outside the fragments directory, and that a sync afterward drops its projections. The backup is written under `~/.gridctl/project-backups/context/`. Leaving the segment with unsaved edits, in either mode, asks before discarding them on a kind switch or any guarded shell navigation, and reload or tab close prompts while those edits are pending. A failed first load shows "Global context unavailable" with Retry; a failed refresh keeps the editor under a banner with Retry. In fragments mode, Add stays disabled until the fragment list loads, and a failed reload keeps the loaded list and the open editor. Unsaved edits belong to the fragment they were typed in: switching fragments, adding one, or changing `?fragment=` in the URL is refused with a warning until you save, or use Revert, which asks before restoring the fragment's saved content. The same operations are exposed over REST; see the [API reference](api-reference.md#global-context).

## Write strategies

Each client receives the canonical content through the safest mechanism it supports. gridctl never takes unmarked ownership of a file the user also writes.

| Strategy | Clients | Mechanism |
|---|---|---|
| Dedicated file | Claude Code (`~/.claude/rules/gridctl.md`), Roo Code, Continue, VS Code Copilot | gridctl owns a whole file inside a rules directory the client reads. Zero merge risk. |
| Import shim | Gemini CLI (`~/.gemini/GEMINI.md`), Goose (`~/.config/goose/.goosehints`) | One `@`-import line referencing the canonical file is inserted; the rest of the file is never reordered or rewritten. Canonical edits flow through the reference without re-syncing. |
| Managed block | OpenCode, Zed, Cline, Grok Build (`~/.grok/AGENTS.md`), Antigravity, Windsurf | The full file is written when absent; when user content exists, a `<!-- BEGIN GRIDCTL MANAGED -->` … `<!-- END GRIDCTL MANAGED -->` block is inserted and only that block is ever rewritten. Windsurf's `global_rules.md` has a 6,000-character limit; oversized content is refused with a count. |

Not syncable, reported honestly in `ctx status` instead of worked around: Claude Desktop (instructions live in the app UI), Cursor (global User Rules are app-internal storage), AnythingLLM (UI/API only), and LM Studio (system prompt is per-chat in the app UI). Antigravity's global path rests on unofficial sourcing rather than published client documentation, so `ctx status` marks it `unofficial path`: the projection is supported, but the path may move without an upstream release note.

Every write is preceded by a timestamped backup (`<file>.gridctl-backup-<ts>`, three retained) and performed atomically. Managed content carries a header naming the source and the edit command, so a reader landing in the file knows where changes belong.

## Drift, staleness, and adoption

`ctx status` distinguishes two kinds of "out of date":

- **stale**: the canonical file changed since the last sync; the client's copy is intact but behind. `gridctl ctx sync` refreshes it.
- **drifted**: the client's managed content was hand-edited. Sync skips drifted targets with guidance instead of silently overwriting. Resolve with `gridctl ctx diff <client>` to inspect, `gridctl ctx adopt <client>` to make the edit the new canon, or `gridctl ctx sync --force <client>` to restore the canon.

For CI or a shell prompt, `gridctl ctx sync --check` performs no writes and exits `1` when anything is drifted, stale, or missing.

`ctx status` enumerates every known client, synced or not; `gridctl skill project status` lists only recorded projections. The asymmetry is deliberate and mirrors what each command manages: one canon that targets all clients by default versus an explicit per-skill allow-list.

## Fragment pins

Pin drift is a fact about the source, separate from the two client facts above. A write pass (`gridctl ctx sync`, `gridctl ctx add`, `gridctl ctx edit`, and `gridctl pack apply`) silently records a digest the first time it sees a fragment, or the single `AGENTS.md` when fragments mode is off. A later content change stays pin drift until a human approves or resets it. Projection warns and continues. It does not refuse, and `ctx status`, `ctx sync`, `ctx sync --check`, and `ctx diff` keep their existing exit codes. An unreadable or newer pin file warns on those commands and on the context document routes; it does not change their exits or status codes. `ctx pins` and `/api/context/pins` still fail on that file.

| Term | What moved | What to do |
|---|---|---|
| stale | The canonical source changed since the last sync. The client copy is intact but behind. | `gridctl ctx sync` |
| drifted | The client's managed content was hand-edited. | `gridctl ctx diff <client>`, then adopt or `gridctl ctx sync --force` |
| pin drift | The fragment (or `AGENTS.md`) changed since its content pin. | `gridctl ctx pins diff <name>`, then `gridctl ctx pins approve <name> --expect <composite_hash>` |

Pins live at `~/.gridctl/pins/context/global.json` (version `1`). Approval history lives at `~/.gridctl/pins/approvals/context/global.json`. The record's `approvals` list is rebuilt from that log, newest first, and keeps the last 10 entries. Reset deletes the pin and leaves the log. The hash is `c1:` over CRLF-normalized bytes with trailing newlines trimmed, so a pin and the projection lockfile agree on whether content changed. Advisory poisoning findings (P001–P005) ride on the record and never change the hash. The `gateway.security.schema_pinning.scan` and `scan_ignore` knobs govern this scanner when exactly one stack is deployed. Zero stacks, more than one stack, or a stack that does not load keep the defaults: scan on, nothing ignored. A nil `scan` is on. Approving over findings requires `--reason`. Repeat `--evidence` with an opaque reference. References are stored as text and never fetched. A credential-shaped reference, reason, or approver name is refused.

For CI, `gridctl ctx pins verify` exits `0` when every pin matches, `1` on pin drift or when `--fail-on-findings warn|critical` sees findings at or above that severity, and `2` on an infrastructure error (home unresolvable, unreadable or newer pin file, unknown fragment, not pinned). An unreadable pinned fragment is drift (exit 1), not missing. `gridctl ctx pins diff` exits `2` in that case. That check does not replace `ctx sync --check`, which still reports client drift, staleness, and missing targets, and does not write pins.

A pack rule is pinned on `gridctl pack apply`, not on `pack add`. A later upstream edit is recorded as pin drift on the next `pack apply`. `pack add --trust` bypasses the import scan only and does not approve a pin. Edits made through `ctx edit` or the web editor are not auto-approved. The first fragment add renames a pin for `AGENTS.md` to `00-default` and keeps `pinned_at`. Approval history is copied onto `00-default`; the old subject stays in the log. An existing `00-default` pin is left in place.

`ctx status` and `ctx sync` print one summary line when any fragment is drifted, missing, or unpinned: `Pin drift: <names>; missing: <names>; unpinned: <names> (review with 'gridctl ctx pins diff')`, omitting empty parts. Their JSON documents gain a `pins` object (`drifted`, `missing`, `unpinned`). See [ctx pins](cli-reference.md#global-context).

In the web UI, the Pins workspace's Context tab (`/pins?kind=context&fragment=<name>`) reviews a drifted fragment as one line diff of the pinned and current content, with findings on the current content, a reason (required over findings), and evidence references. Approve sends the `composite_hash` of the diff on screen; when the content changed since that diff, the tab reloads it for re-review. Approve asks for confirmation first when the current content has a warn or critical finding, when the diff has 50 or more changed lines, or when the diff shown is cut off at its render limit; the confirmation names each reason and, for a cut-off diff, `gridctl ctx pins diff <name>`. Documents too large to align line by line (about 500 lines per side) show the pinned text as removed followed by the current text as added, and the full current document is offered below the diff. Reset sits in a Danger section: it deletes the pin record and keeps the approval history, and the next context sync re-pins whatever is on disk without a diff. The pin review opened from the Library's Context segment shows the same detail and links to Pins.

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

Packs can ship fragments via `rules: [names]` in `gridctl-pack.yaml` (files under `rules/*.md` or `fragments/*.md` in the pack repo; see the [Packs guide](packs.md)). Only the fragments a pack shipped are tagged with its name; your own fragments are projected alongside them but never claimed. `pack remove` still retracts projections by pack tag and removes the store file of every rule the pack record lists, whether or not a projection existed. A locally modified rule, including one with no recorded provenance, is kept unless `--force`. An unreadable rule is kept even with `--force`. A compiled client document is refreshed by the next `gridctl ctx sync`. If that removal leaves the fragment store empty, that sync reports that no fragments exist and leaves the compiled document in place. Fragment CRUD is also available over REST; see the [API reference](api-reference.md#global-context) endpoints under `/api/context/fragments`.

## Removal

`gridctl ctx unsync [client|--all]` removes what gridctl manages and nothing else: dedicated files are deleted, shim lines and managed blocks are stripped, multi-file fragment projections are removed individually, and files gridctl created are removed entirely. User-owned content and unrecorded files in shared rules directories survive byte-for-byte.
