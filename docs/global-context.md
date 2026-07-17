# Global Context Sync

`gridctl ctx` maintains one canonical global agent-context file and syncs it to every linked client, so cross-project preferences (coding style, commit conventions, tone, tool preferences) are written once instead of duplicated by hand into `~/.claude/CLAUDE.md`, `~/.gemini/GEMINI.md`, and a dozen peers that drift apart.

Scope boundary: this manages only the **global** (user-level) layer. Per-project `AGENTS.md` files belong in each repository under version control, are read natively by most clients, and gridctl never touches them.

## The canonical file

The source of truth lives at `~/.gridctl/context/AGENTS.md`, plain markdown per the [agents.md](https://agents.md) spec. Because it is a spec-named file, AGENTS.md-native tools can read it directly, and it can be symlinked into a dotfiles repository for version control. A lock file (`context.lock.yaml`) beside it records what was written to each client and from which canonical revision.

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

## Removal

`gridctl ctx unsync [client|--all]` removes what gridctl manages and nothing else: dedicated files are deleted, shim lines and managed blocks are stripped, and files gridctl created are removed entirely. User-owned content survives byte-for-byte.
