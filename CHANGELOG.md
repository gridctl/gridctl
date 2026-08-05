# Changelog

All notable changes to gridctl will be documented in this file.

## [Unreleased]

### Features

- Pack provenance chips (experimental): pack ownership now shows wherever pack-managed resources appear, not only in the Packs segment. Agent projection status rows (`GET /api/project/agents/status`) and per-fragment context status rows (`GET /api/context`) gain an additive `pack` field carrying the applying pack's name, which the engines already recorded but never surfaced. In the web UI, a shared chip (unifying the two shipped hand copies) adds `pack: <name>` provenance to the skill inspector, skill cards, the Library table, and agent cards through the existing client-side join, and, straight from the wire, to the Connections client detail's agent projection rows (matching the Wiring section's badge) and the Global Context dialog's per-fragment status lines. Every chip is display plus a deep link to the pack detail; pack management verbs stay in the Packs segment (#1080)

- Packs REST surface (experimental): the full pack verb set is now available over HTTP, backed by a new `pkg/packops` engine extracted from the CLI (the CLI keeps its exact output and exit codes). `GET /api/packs` lists installed packs with identity, origin, per-kind counts, and attention; `GET /api/packs/{name}` adds per-resource state rows, refusing with a 409 naming both repos when two sources claim one pack name instead of picking nondeterministically. `POST /api/packs` imports from git behind the blocking security scan, and unlike the CLI it refuses the whole import with a 409 carrying the flagged resources before any write when findings lack trust; a POST against an already-imported origin is the documented update path. `POST /api/packs/preview` resolves a manifest read-only for import review; `POST /api/packs/{name}/apply` takes `clients`, `force`, and `dry_run` with full CLI flag parity; `DELETE /api/packs/{name}` supports a dry-run cascade preview and force. The pack record now persists the manifest's description and author so list views never re-clone, and `skills.lock.yaml` mutations are protected by a cross-process file lock so concurrent operations (CLI, API server, pack verbs) serialize instead of silently losing updates (#1078)

- Fragment-aware context adopt over REST (experimental): `POST /api/context/adopt/{slug}` accepts an optional body scoping the adopt in fragments mode. `{"fragment": "<name>"}` pulls one hand-edited projected file back into its source fragment on identity multi-file targets (the REST twin of `gridctl ctx adopt <client> [fragment]`), and `{"into": "<name>"}` captures a compiled target's edited body into a designated fragment, creating it when absent (the twin of `--into`). An empty body keeps the whole-client behavior. Refusals are now typed sentinels mapped to proper statuses (409 for the three adopt refusals with the engine's prose on the wire, 404 for an unknown fragment, 400 for a bad capture name) instead of a blanket 500. `GET /api/context` rows for multi-file clients gain a structured `fragments` array listing every out-of-sync fragment with its own state, so a drifted fragment can no longer hide a stale one behind the single summary string (#1076)

- Connections client health hub (experimental): the Connections workspace grows from a single-column link-toggle list into a per-client health hub. A resizable client rail (attention-first ordering with a drift filter, brand icons, link badges, the familiar inline connect toggle, and keyboard navigation) sits beside a selected-client detail pane answering "what did gridctl configure on this client, and is it still true?": collapsible sections that auto-expand on drift cover the gateway entry's full wiring ownership state with inline Re-link and Adopt (refusals render the engine's reason verbatim), global context sync with inline Sync/Unsync and a Review deep link into the Global Context dialog, agent projections with per-row render class linking into the Library (non-target clients say so explicitly instead of showing blank space), an access-scope summary deep-linking into the Tools per-client editor (`/tools?client=<slug>`), and live session activity attributed by the sessions API's new client identity. Ownership state and live connectivity render as two separately labeled axes, never one merged health light; sessions the UI cannot attribute appear in their own labeled bucket with synthesized identities, stateless-generation clients read "sessionless" rather than "0 sessions", and the status bar reads the same session array as the workspace so the two counts cannot diverge. Deep links: `?client=<slug>` selection, and the creation wizard's Client Link route now lands on a spotlighted detected-unlinked client via `?spotlight=unlinked`. The staged Review & Apply flow is unchanged and stays workspace-wide (#1074)

- Wiring ownership REST (experimental): `GET /api/project/wiring/status` exposes the full six-state ownership vocabulary (`in-sync`, `stale`, `drifted`, `target-missing`, `foreign`, `missing`) with per-row detail, remediation, and pack tag — the complete form of the fact `/api/clients` collapses into one `drifted` boolean — and `POST /api/project/wiring/adopt` wraps the take-ownership verb that was previously CLI-only, returning 409 with the engine's reason on refusal. The link endpoint's conflict message now offers the API adopt route alongside the CLI. Session entries from `GET /api/sessions` gain client identity (`clientName`, `clientVersion`, and a normalized `accessId` that matches provisioner client slugs), so sessions can be attributed to linked clients. Groundwork for the Connections client health hub (#1074)

### Fixed

- Closed the delivery gaps a post-ship audit found across the Library and Connections surfaces: `docs/project-status.md` no longer claims packs have no web UI (the Packs segment and pack REST shipped); agent projection rows in the Library now link each client row to that client in Connections, matching the reverse deep link Connections always had; the Connections Context section now lists a client's out-of-sync context fragments with their states and pack provenance chips instead of a summary line only; and the Wiring section's pack badge is now the same navigable chip the Agents rows use (#1082)

- The Global Context dialog's Review flow no longer dead-ends in fragments mode. On a multi-file client the drift dialog compared a summary string instead of file contents and its Adopt button called the whole-client endpoint, which the engine correctly refuses on multi-file targets, so every review ended in an error toast. The dialog now reviews drift per fragment with a real diff, adopts losslessly on identity renders, explains lossy renders with working alternatives instead of a dead button, and offers fragment capture on compiled targets (#1076)

- The status bar's session count and `GET /api/sessions` no longer diverge. The two surfaces read different stores with different lifecycles: the gateway's session manager is swept every 5 minutes at a 30-minute idle cutoff, while the streamable transport kept its own map that only shrank when a client sent a graceful `DELETE /mcp` — which crashed CLIs and closed tabs never do, so that count only grew (a 7 vs 21 divergence was observed on one load). The transport now defers to the gateway's manager as the single source of truth: expired sessions are excluded from listings and lazily torn down, both counts agree by construction, and a client posting on an expired session receives the spec's 404 and re-initializes cleanly instead of continuing unattributed (#1074)

- Agents in the Library workspace (experimental): the web UI's Library gains a `Skills | Agents` segmented control (URL-synced as `?kind=agent`; the retired `/agent` route now lands there), making CLI-imported agents first-class in the UI. The Agents segment shows a source-grouped catalog with per-client projection chips, an always-visible KPI strip (Total, Projected, Drifted, Experimental; deliberately no lifecycle or usage KPIs, since agents have neither), a "Sync N stale agents" pill, and an inspector with Overview (frontmatter, with `tools` and `model` called out as per-client-translated), Body, and Projection tabs. Projection rows speak the CLI's exact state vocabulary through a shared StatePill (extracted from the Global Context dialog so context and agent rows stay one color language), carry the render class and dropped-keys detail, and offer sync, unsync, and drift review inline; adopting a hand-edit is offered on the identity target with the backup named up front, while lossy render targets get the refusal spelled out with its two real alternatives as working buttons, never a disabled control with a tooltip. The single-file editor saves the whole AGENT.md byte-verbatim behind the same blocking security scan imports run, rendering findings inline on a refusal. The import wizard now discovers agents alongside skills, lists them by name in the browse and review steps (names are cheap at agent scale), and passes an explicit agent selection to the importer — which the source-add API now accepts (`selectedAgents`), since a skill selection alone deliberately skips agents (#1071)

- Agents REST surface (experimental): imported agent definitions are now manageable over HTTP, closing the "agents have no REST endpoints" limitation ahead of the Library UI. `GET /api/registry/agents` lists installed agents (bodies omitted by default, `?full=1` for complete shapes; passthrough frontmatter rides in `extra` as an ordered `{key, value}` array, never an object, because key order is part of the verbatim-projection contract), `GET /api/registry/agents/{name}` returns one agent with `body` and `raw`, `PUT /api/registry/agents/{name}` edits the file as a whole (re-parse, rename refusal, then the same blocking security scan imports run, whose findings return 409 with no trust override over REST, then a byte-verbatim atomic write, so identity projections never read an edit as manufactured drift), and `DELETE` removes the agent through the importer so origin and lock entries leave with it. A new `/api/project/agents/*` namespace exposes the projection engine: `status` (per-client rows with the shared state vocabulary plus `render: identity|lossy` and `experimental`), `sync`, `unsync` (refuses an empty request), and `adopt`, whose lossy-render refusals surface the engine's full reason as a 409 rather than a generic error. Source add and update responses already carried imported-agent detail; sync results now count refreshed agents (`importedAgents`), and the source preview response gains `agents` and `malformedAgents` so a mixed repo previews both kinds (#1071)

- Rules fragment library in the web UI (experimental): the Global Context dialog now recognizes fragments mode and swaps the single-file editor for a fragment rail in composition (filename-lexicographic) order, with add and delete affordances, a globs badge for path-scoped fragments, and the same markdown/preview split, marker validation, and sync-all flow feeding whichever fragment is selected; a dirty draft blocks fragment switching so typing is never silently discarded. The per-client strip gains a mode chip (`multi-file` / `compiled`) beside each state, and the single-file editor gains a deliberate "Fragments" affordance that names the first fragment and states the AGENTS.md-to-`fragments/00-default.md` migration before writing anything, mirroring `ctx add` (#1046)

- Rules fragment library (experimental): `gridctl ctx` can opt into a multi-file fragment store under `~/.gridctl/context/fragments/*.md` (optional `description` / `paths:` frontmatter). `ctx add <name>` activates the mode on first use by migrating AGENTS.md to `fragments/00-default.md` with a backup; read-only commands never migrate. Composition is filename-lexicographic. Multi-file passthrough targets are Claude Code, VS Code Copilot (`applyTo` from `paths:`), Cline, and Roo, each fragment file individually lockfile-owned so shared rules directories never claim user files; every other single-file target receives a compiled document with `<!-- Source: -->` attribution and the existing size-cap hard errors. Status gains a `mode` column (`single-file` / `multi-file` / `compiled`); adopt is lossless per-fragment on multi-file identity renders and refuses on compiled targets unless `--into <fragment>`. Packs activate the `rules:` manifest slot (`rules/*.md` or `fragments/*.md` in the pack repo; empty means none): rule installs pass the same blocking security scan and `--trust` gate as skills and agents, never overwrite a differing local fragment, surface a fragments-mode migration explicitly, and only pack-shipped fragments carry the pack tag, so cascade remove can never retract a user fragment. New CLI: `ctx list|add|rm`, fragment-aware `edit`/`diff`/`adopt`; REST: `/api/context/fragments` (#1046)

- Skill governance in the web UI (experimental): the Pins workspace gains a Servers | Skills toggle in its rail, extending the existing review grammar to skill-document pins: skills list drift-first under the same attention filter, and a drifted skill's detail pane shows a semantic summary line, a line-level prose diff of the canonical SKILL.md (non-printables escaped per line), the per-file added/removed/modified list, and advisory findings, with Approve bound to the reviewed diff's composite hash (a stale-content 409 reloads the diff for re-review) and a reason input required when unresolved findings exist; Reset mirrors the server flow behind the same confirm-dialog pattern. Drifted skills imported from one git source group under a source header in the rail. The Library shows one compact governance indicator per row/card only when attention is needed (pin drift, warn-or-critical findings, or a policy denial; quiet skills show nothing), the skill detail panel gains a Governance section (factual origin `Local` / `Imported: repo@ref`, pin state with a Review-in-Pins deep link, finding counts, policy verdict) plus a "Blocked by policy" chip that names the matching rule while the lifecycle badge stays untouched, and the status-bar pin badges now count skill drift and findings (server-only stacks read exactly as before), deep-linking to `/pins?kind=skill`. Skill pins ride the existing 3-second poll in their own progressive-disclosure block with rising-edge toasts (#1045)

- Skill governance pins (experimental): registry skill documents now get the same trust-on-first-use treatment tool definitions get from schema pins. The daemon silently pins every skill on first sight, recording per-file SHA-256 digests over the whole document set — the canonical `SKILL.md` (hashed on its parse-rendered form, so frontmatter normalization from imports, editor saves, and projection never manufactures false drift) plus every supporting file (dotfiles, `.origin.json`, and temp files excluded) — along with factual provenance (`local`, or `git` with repo/ref/commit from the origin sidecar). Any later content change flips the skill to "pin drift" (a distinct fact from the Library's sync drift), which persists until a human approves or resets: the daemon never auto-clears it, and the pin file lives at `~/.gridctl/pins/skills/<stack>.json`, outside the watched registry tree, so pin writes can never feed back into the disk watcher. The pins poisoning heuristics (P001–P005) now scan skill names, descriptions, and bodies via a new `ScanSkill` entry point, with findings persisted on the record as strictly advisory data — the deterministic hash is the gate, and approving a skill with unresolved findings requires a `--reason`, persisted for later reviewers. A new `gridctl skill pins list|verify|diff|approve|reset` family mirrors the `gridctl pins` contract (`--format json` with `schema_version`, exit `0`/`1`/`2`, opt-in `--fail-on-findings`, `approve --expect` bound to the reviewed diff's composite hash; distinct from `skill pin`, which pins a git source ref), backed by `/api/skill-pins` endpoints with the same 409-on-stale-hash concurrency, and registry API responses gain a `governance` object (source, origin, pin status, finding counts, policy verdict) for the upcoming Pins-workspace UI. Separately, an optional top-level `skills:` block in stack.yaml adds a global exposure policy: allow/deny name globs (deny wins, then allow, then `default:`) filtering which skills the gateway serves via prompts/resources and which projection sync places — denied skills keep their registry state and stay visible in the API flagged with the matching rule, recorded projections are skipped with a pointer rather than removed, `gridctl apply` warns for every active skill the policy hides, `gridctl validate` flags a default-deny block with no allow list, and edits hot-reload. Omitting the block preserves today's behavior exactly, and per-client skill scoping remains deferred (#1045)

- Packs (experimental): a git repo carrying a `gridctl-pack.yaml` manifest (apiVersion gridctl.dev/v1alpha1, kind: Pack) now imports and applies as one unit. `gridctl pack add <repo-url>` clones through the same origin pipeline as `skill add` (security scan, `--trust` gate) and imports exactly the manifest's selection of skills and agents — agent-level selection is new plumbing this release adds to the importer, with the legacy contract preserved: a plain skill selection still skips agents. `gridctl pack apply` projects the selection through the existing engines and, when the manifest declares `wiring: true`, ensures the gateway entry through the wiring ownership manager (skipping with a hint when no gateway is running); every projection is tagged with the pack name in the unified project lockfile, a resource tagged by a different pack is refused, and apply is additive, never transactional (`Applied N/M` summary, exit 1 on any skip). `gridctl pack status` reports the shared state vocabulary per resource plus `unresolved` rows for manifest selections the repo does not ship. `gridctl pack remove` cascades in dependency order — projections unsynced, wiring records removed through the ownership manager, then registry entries, then the pack record — keeping drifted projections with a remediation hint unless `--force` and trimming the pack record on partial removal. The import lockfile stamps schema version 2 only when a pack record exists, so setups without packs keep downgrade freedom. Manifest field names align with the Claude Code plugin.json family; the `rules:` field selects context rule fragments (see Rules fragment library). No enable/disable state, no inter-pack dependencies, no marketplace, no web UI (#1034)

- Multi-client agent renders (experimental): `gridctl skill project sync --kind agent` now projects imported agents to three rendered targets alongside the identity Claude Code copy, where those clients are detected: OpenCode (`~/.config/opencode/agents/<name>.md`, emitted as a `mode: subagent` definition), GitHub Copilot (`~/.copilot/agents/<name>.agent.md` — Copilot's global agents directory; contrary to earlier wording, Copilot does not read `~/.claude/agents`), and Gemini CLI (`~/.gemini/agents/<name>.md`). Renders are deterministic and explicitly lossy: frontmatter keys a dialect cannot express (Claude `tools` on OpenCode, and `model`, `hooks`, `mcpServers`, vendor keys everywhere) are dropped and named in sync output, `skill project status` gains a RENDER column (`identity` or `lossy`), and adopt is refused on rendered targets with a pointer at the identity projection, so client-dialect content can never flow back into the canonical store. Cursor needs no target: it reads `~/.claude/agents` natively. Hand-edited rendered files still report drifted and are never overwritten without `--force`, including by the daemon reconcile (#1034)

- Wiring ownership (experimental): `gridctl link` and `unlink` now record what they write instead of guessing what they wrote. Every link stores the client, config path, entry name, and an RFC 8785 canonical hash of the written value in the unified project lockfile (`~/.gridctl/project.lock.yaml`, hash only, never the value, since entries can carry secrets in env blocks; a short hash history means a gridctl upgrade changing its own written shape never reads as user drift). Unlink deletes an entry only when its current value is one gridctl recorded (or `--force`, which unlink now accepts for hand-edited entries); entries gridctl never wrote are never deleted, with or without force, closing the long-standing hazard where a user's own localhost entry could be removed by name match. A pre-existing entry identical to what gridctl would write is adopted silently; anything else foreign is refused with an adopt hint, and the old localhost/npx shape heuristic now serves only that one-time migration hint. The new `gridctl project` verb family (`sync|status|unsync|adopt --kind wiring`, with `--format json` and exit codes `0`/`1`/`2`) exposes the state matrix: `in-sync`, `stale` (gateway port or entry shape changed), `drifted`, `target-missing` (distinguishing a removed key from a wiped config file, the failure mode first-party clients have shipped), `foreign`, and `missing`. Every link surface records ownership identically (CLI, `apply --flash`, the stack `link:` reconcile during apply, and the web UI, whose Connections workspace gains a Drifted badge), `gridctl doctor` gains advisory per-client wiring rows ("detected but not linked", drift), and the daemon never auto-reconciles wiring. Existing links predate the lockfile and show as `foreign` until relinked or adopted once (#1031)

- Agent resource kind (experimental): `gridctl skill add <repo-url>` now also discovers Claude Code subagent definitions matching the `agents/*.md` convention (at the repo root or any subdirectory root, the plugin-shaped layout), so a repo shipping `skills/` plus `agents/` imports as a unit. Each agent lands verbatim at `~/.gridctl/registry/agents/<name>/AGENT.md` with the same `.origin.json` sidecar, security scan (`--trust` gate covers bodies and frontmatter values), lockfile tracking, and drift-safe `skill update` semantics skills get; agent names are validated (lowercase letters, digits, hyphens; no colons, which Claude Code refuses) and duplicate names within a batch fail those items with an error naming both sources. `gridctl skill project sync --kind agent` projects every imported agent to `~/.claude/agents/<name>.md` as a single copied file with dedicated-file ownership: a pre-existing hand-authored file is refused without `--force` and backed up under `~/.gridctl/project-backups/agent/` with it, a hand-edited projection reports drifted, and `adopt --kind agent` pulls the edit back into the canonical store instead. `skill project status` gains agent rows (marked experimental), every row in `skill project` JSON output now carries a `kind` field, `skill list`/`remove`/`info` take `--kind agent`, and the daemon reconciles agent projections alongside skills. The import lockfile (`~/.gridctl/skills.lock.yaml`) gains a schema `version` field with a newer-version guard; version-less files migrate on read. Claude Code is the only render target in this slice (identity render; Cursor also reads `~/.claude/agents` natively), and there is no web UI surface yet (agents are CLI-only for now) (#1020)

- MCP protocol generation in the web UI: mixed-generation fleets show a Generation row in the Stack canvas sidebar next to the existing Protocol row (suppressed when every server speaks the same generation, since a uniform label carries no signal), and the Connections workspace gains a read-only Live sessions card listing each active session with its negotiated protocol version and generation. The card is deliberately separate from the client rows above it: those are declared links (config-file state), while sessions are transport state, and stateless-generation clients (2026-07-28) are sessionless by design and never appear there (#1018)

- MCP transport dual-stack: the gateway now speaks both protocol generations concurrently, per peer, in both directions: the handshake era (2025-11-25 and earlier: `initialize`, `Mcp-Session-Id` sessions) and the new stateless era (2026-07-28: per-request `_meta`, `server/discover`, MRTR). Downstream, each server's generation is auto-negotiated by probing `server/discover` and falling back to the legacy handshake on anything not positively modern (auth challenges and probe 5xx reject outright rather than misclassifying); the verdict is re-derived on reconnect and restart, and an optional per-server `protocol_generation: auto | handshake | stateless` knob pins mis-probing peers. Upstream, `POST /mcp` (and group endpoints) accepts stateless requests with no session, implements `server/discover`, validates the mirrored `Mcp-Method`/`Mcp-Name` headers (base64 sentinel decoding included) with the spec's `-32020`/`-32021`/`-32022` error codes, and derives per-request identity from `_meta` clientInfo so access scoping, budgets, telemetry, and tracing behave identically on both paths. Results are bridged between generations: `resultType` is synthesized for legacy servers, `ttlMs`/`cacheScope` aggregate as min-across-fleet (zero when any server is legacy) and private-unless-all-public, and MRTR `requestState` relays byte-exact through a routing envelope that survives gateway tool renaming, with retries never cached. Cross-generation MRTR is deliberately out of scope and reports a clear error. The tasks extension proxies opaquely when exactly one stateless server declares it. `listChanged` is no longer advertised on any generation (gridctl never emitted the notification; advertising it was a conformance violation). Generation is visible in `gridctl status --json`, `/api/mcp-servers` (`protocolGeneration`), `/api/sessions` (`entries` with per-session tags), a `gridctl doctor` per-server check, a negotiation-time log line, and an `mcp.protocol.generation` trace attribute. Schema-pin fingerprints are untouched by design (the new result fields live outside tool definitions), so a fleet changing generations shows zero pin drift. The official `@modelcontextprotocol/conformance` suite is wired into `tests/integration/` with a checked-in expected-failures baseline and a `task test:conformance` task (#1018)

- Experimental feature flags: a new optional top-level `experimental:` block in stack.yaml enables registered experimental flags by name, backed by a typed registry (`pkg/flags`) with lifecycle stages (experimental, graduated, removed) and a graduation-deadline test so flags cannot rot silently. Everything defaults to off, so an unchanged stack.yaml behaves identically. Unknown flag names warn at `gridctl apply` and `gridctl validate` listing the valid names, and graduated or removed names warn with a specific migration message — never an error, so a stack written against a newer gridctl still deploys. Each flag can be overridden per process with `GRIDCTL_EXPERIMENTAL_<NAME>` (strconv.ParseBool vocabulary; unset defers to YAML, unparseable warns). Enabled flags surface in `GET /api/status` (`features` plus `feature_details`), `gridctl status --json`, and the web UI as a read-only "N experimental" status-bar chip with per-flag rows in the spec pane; flags are configured in stack.yaml only and cannot be toggled from the UI. Edits to the block hot-reload without container restarts. The first registered flag, `transport_dual_stack`, is reserved for the MCP transport work and currently has no effect. The flag lifecycle is documented in CONTRIBUTING.md, and `GRIDCTL_NO_SKILL_UPDATE_CHECK` now accepts the same boolean vocabulary (previously only `"1"`) (#1021)

- `gridctl skill project adopt <skill> --client <slug>`: a hand-edited copy projection (Antigravity's forced copies, or `--copy` elsewhere) can now be pulled back into the registry skill instead of being clobbered by `--force`. Adopt backs up the registry's `SKILL.md` as `SKILL.md.pre-<sha>` (the same convention forced updates use), writes the changed files back, and re-syncs that one (skill, client) pair to in-sync; other clients projecting the skill go stale until the next sync. The adopted content counts as local edits, so `gridctl skill update` refuses to overwrite it without `--force`. Symlinked projections are refused (the registry copy is the source of truth), as are empty or invalid projected files. Exit codes follow the family convention: `0` adopted, `1` nothing to adopt, `2` infrastructure (#1019)

### Changed

- `gridctl pack status` rule rows now report per-client projection state once a pack is applied (in-sync, stale, drifted, or target-missing per client), instead of only store-level presence. Coverage is per fragment-file projection: multi-file clients get one row per client, while a compiled client's whole-document state stays in `gridctl ctx status`. A rule that was imported but never projected keeps its store-presence row, and a missing rule still reports `missing` with the re-run hint. Multi-pack output is now sorted by pack name, and a pack name claimed by two imported sources now refuses `pack status <name>`, `pack apply`, and `pack remove` with both repos named (exit 2) instead of acting on a nondeterministic pick (#1078)

- Projection state now lives in one unified lockfile, `~/.gridctl/project.lock.yaml`, shared by `gridctl ctx` and `gridctl skill project` and owned by the new `pkg/project` engine. The lockfile is keyed by destination path, so one destination can never have two owners, and versioned in two tiers: a breaking `version` readers refuse when newer, and an additive `revision` whose unknown fields survive rewrites and re-syncs by older binaries of the same version. Legacy `skillsync.lock.yaml` and `context.lock.yaml` files migrate automatically on the first sync after upgrading: both are backed up to `~/.gridctl/project-migration-backup/<timestamp>/`, then replaced with version-2 tombstones so a downgraded gridctl fails loudly ("written by a newer gridctl version") instead of silently diverging; restore the backup if you downgrade deliberately. Context operations now hold the same cross-process lock skill projections always had, closing a CLI-vs-daemon race. `gridctl doctor` gains a `project.lockfile` check reporting which generation is in use, and CLI behavior is otherwise byte-identical, enforced by characterization tests (#1019)

- Code mode is now a stable feature: the `gateway.code_mode` and `gateway.code_mode_timeout` settings and the `--code-mode` flag are no longer labeled experimental. Behavior is unchanged (#1021)

- The project task runner is now Task (https://taskfile.dev): `task build`, `task test`, `task lint`, and friends replace the Makefile targets, with namespaced names for grouped work (`build:web`, `test:integration`, `pricing:update`, `mock:servers`). Run `task --list` for the catalog. A transitional Makefile shim keeps every old `make <target>` name working while muscle memory catches up. `task test` and `task test:integration` now run with the race detector to match CI, `task lint` covers both golangci-lint and the frontend lint, a failed frontend build now fails `task build:web` instead of silently staging stale assets, and the `gridctl validate` hint for unknown pricing models points at `task pricing:update` (#1014)

### Bug Fixes

- `gridctl link continue` now writes a key Continue actually reads. The entry went to `experimental.mcpServers`, but Continue's `config.json` schema defines `experimental.modelContextProtocolServers` — and because the schema does not restrict additional properties, the wrong key was accepted and silently ignored. Linking reported success and the gateway never appeared in Continue. The transport shape was already correct (`{"transport": {"type": "streamable-http", "url": …}}` matches the schema's streamable-http variant), so only the key name was wrong. Reads consult both keys so an entry written before the correction is still discoverable, `unlink` clears both, and an emptied list now removes the key instead of leaving `"mcpServers": null` behind. **Existing Continue links need one migration:** the entry is inert today, so run `gridctl link continue` (adopts and records ownership), then `gridctl unlink continue`, then `gridctl link continue` — or delete the `experimental.mcpServers` block from `~/.continue/config.json` and link once. A plain relink reports "already had a matching entry" without rewriting, because ownership adoption compares the entry value and cannot see that the key differs (#1066)

- **Behavior change:** gridctl now refuses to start when the bind is widened past loopback with no `gateway.auth` configured, where it previously logged a warning and served anyway. Enforcement is keyed to the user's own act of widening the bind, following Elasticsearch's bootstrap checks: a loopback gateway — the default since the previous release — stays permissive and silent, and only a deliberately network-reachable one is gated, so the common path gains no friction. The refusal names every way forward rather than stating the refusal alone (configure `gateway.auth`, return to a loopback bind, or set the override), since a bare failure reads as gridctl being broken and the user cannot guess which of three unrelated settings is responsible. The override is `--insecure-allow-unauthenticated` plus a matching `gateway.insecure_allow_unauthenticated` field; both exist because a flag can be dropped by whatever wraps the process, and it warns on every start rather than going quiet once set. `examples/gateways/gateway-remote.yaml` widens the bind and now carries the required `auth` block. Authentication remains optional on loopback: requiring it there would break every CLI subcommand that calls the local API for no security gain (#1060)

- `gridctl pack add` can now update a pack rule whose content changed upstream. Rule fragments recorded only their names in the import lockfile, with no per-rule content hash, so the installer's only available check was comparing incoming bytes against whatever was on disk — which cannot tell "the pack changed this rule" apart from "the user edited it." Both were refused identically, meaning the documented update path (re-run `pack add`) worked for rules that never changed and failed for exactly the ones that did. `LockedPack` now carries per-rule provenance (upstream path and content hash, mirroring how skills and agents are already recorded in the same file), and the installer distinguishes three cases: unchanged, changed upstream with no local edit (updated, and reported as such), and locally modified (skipped, now saying so). Lockfiles written before this load unchanged, with their rules treated as unknown provenance and handled exactly as before until their next `pack add`. Separately, `foreignPackTags` did not consider context fragments, so the cross-pack ownership refusal documented for packs was not enforced for rules; it is now (#1061)

- **Behavior change:** gridctl now binds `127.0.0.1` instead of every interface, and DNS rebinding protection covers the whole HTTP surface rather than only `/mcp`. The listen address was built as `fmt.Sprintf(":%d", port)`, which has no host part and so bound `0.0.0.0` and `::` — putting roughly 90 REST routes, `/sse`, and the web UI on the local network with no authentication, including a variables endpoint that returns secrets in plaintext when the vault is unlocked. It also meant any container gridctl launches could reach the gateway through its `host-gateway` alias. Three defaults here contravened Article XII and XIII of the constitution. Loopback is now the default; `--bind <addr>`, `--bind-all`, and a `gateway.bind` field opt back in, with the flags taking precedence, and a startup warning fires when the bind is non-loopback with no `gateway.auth` configured. The Host check added for `/mcp` last release now runs as mux-wide middleware sharing one implementation, with `/health` and `/ready` exempt so the daemon parent can still poll them. Downstream container ports are published on the same address rather than a hardcoded `0.0.0.0`. Remote access is still supported — see `examples/gateways/gateway-remote.yaml`, which now opts in explicitly — and SSH forwarding remains the recommended path for single-machine access. Authentication stays opt-in for now; making it mandatory on a widened bind is tracked separately (#1058)

- The MCP endpoint now validates the `Host` header, closing the DNS rebinding gap on `/mcp`. `validateOrigin` checked only `Origin`, which browsers omit entirely on same-origin GET and HEAD requests — and after a DNS rebind the attacker's page *is* same-origin from the browser's point of view, so there was nothing to validate. `Host` is the value a browser must set to the attacker's domain and that page scripts cannot override, which is why it is the primary control (Origin remains as defense in depth). Requests arriving over loopback whose `Host` is not a loopback name are now rejected with 403; applicability is decided from the address the request actually arrived on rather than the configured bind address, so loopback clients stay guarded even when the daemon listens on `0.0.0.0`, while deliberately network-exposed deployments keep working. Loopback names must also match the listener's port, and lookalikes such as `127.0.0.1.evil.com` or `0.0.0.0` are rejected. A new optional `gateway.allowed_hosts` accepts extra Host values for reverse-proxy and container deployments; unset means loopback-only, and wildcards are deliberately not supported. This clears the MCP conformance suite's `localhost-host-rebinding-rejected` check, whose failure had been carried as an expected-failure baseline entry. Scope: this covers the MCP endpoint for loopback-arriving connections. The REST API and `/sse` remain ungated, and gridctl still binds all interfaces by default with authentication off unless configured — that default-exposure posture is tracked separately and is the larger remaining issue (#1056)

- Every remaining hardcoded amber class in the web UI now resolves through the theme system. Sixty-eight raw `amber-*` Tailwind classes across 22 files styled warning notices, draft and drift badges, the vault secret marker, and the variable type badges; raw palette classes do not re-key per theme, so all of them were tuned against the dark theme alone and rendered at roughly 1.2:1 in light. They now use semantic tokens: genuinely actionable states (the runtime-failure notice, context drift, telemetry disabled, unencrypted secrets, sync-overwrite warnings) map to `status-pending`, lifecycle and drift badges to the same family, and the vault secret marker is neutral since its `Lock` and `Eye` icons already carry the distinction. The registry lifecycle color map, which existed in three divergent copies across the state badge, the Stack canvas node, and the Library KPI legend, is now defined once. An ESLint rule fails the build on raw `amber-*` and `yellow-*` classes so the convention documented in the components README is enforced rather than merely written down; it catches literals inside `cn()` composition and template literals, and exempts the chart palette and tests (#1053)

- The light theme's "running" status color no longer fails contrast as text. `--color-status-running` was emerald-600, which measured 3.33:1 for small text on its own tinted background, so the active-state badge could not reach the WCAG AA 4.5:1 threshold once it stopped using a raw palette class. It re-keys to emerald-800 for light only, reaching 5.22:1. Solid uses are all indicator dots with no text on them, so they only gain contrast, and the dark theme value is unchanged (#1053)

- The light theme's warm accent is now legible. Its primary and pending tokens shared the single hex `#b45309`, which fell below the WCAG AA 4.5:1 threshold as small text on the theme's warm-tinted surfaces: the active workspace tab measured 3.96:1 on every route, the Library KPI tiles 3.84:1, and solid primary buttons 4.32:1. The Library's "Sync sources (N updates)" pill was worse at 1.21:1, effectively invisible, because it hardcoded a raw `amber-300` class that bypasses the theme system and had only ever been checked against the dark theme. Light-theme primary re-keys to `#92400e` with its light and dark ramp steps moving alongside it, so gradients keep a visible step and button hovers stay perceptible, and pending re-keys to `#854d0e` so "brand accent" and "needs attention" stop being the same color in light mode (a collision the dark theme never had). Both Sync-sources affordances move onto brand tokens rather than the warning family, matching the sidebar's existing "N updates" pill: an available update is informational and optional, not a warning. The dark theme is unchanged (#1053)

- `gridctl link` now writes streamable-HTTP `/mcp` endpoints for every client; legacy HTTP+SSE `/sse` shapes are no longer written anywhere (they are removal-eligible upstream as of 2026-08-18). Cursor, Windsurf, and Zed entries move from `/sse` URLs to `/mcp` (Zed's HTTP transport is streamable-only, so its old entries never connected on current Zed), VS Code moves from `"type": "sse"` to `"type": "http"`, Goose from `type: sse` to `type: streamable_http`, AnythingLLM from `"sse"` to `"streamable"`, Continue from `"sse"` to `"streamable-http"`, and Roo Code drops the `transportType` key its current schema rejects in favor of the required `"type": "streamable-http"`. Existing `/sse` entries are still recognized as gridctl's own and update in place on the next `gridctl link` (or `link --all`) without `--force`; nothing is migrated behind your back. The Connections transport label now reads "native HTTP" for every non-bridge client (#1031)

- The background skill-projection reconcile no longer trusts an empty registry. The registry store reads a missing or unreadable registry directory as "no skills", and the daemon's post-refresh reconcile treated every projection as orphaned in that state: it deleted the projected entries from client skill directories and rewrote the projection lockfile empty, silently. Reconcile now refuses to remove anything when the store reports zero active skills while projections are recorded, logging a warning instead; explicit `gridctl skill project sync` and `unsync` retain full authority, so deliberately clearing the registry still has a supported cleanup path. The same hardening reaches the test suite, which could previously trigger this exact removal against a developer's real home directory: the reconcile home is now injected at the controller boundary, saved-stack API handlers and the pin store honor injected paths for every write, and each package whose tests can reach home-resolving code sandboxes `HOME` for the whole test run (#1023)

- Skill import no longer strips frontmatter keys it does not model. The registry parser decoded SKILL.md frontmatter into a fixed set of fields and re-emitted only those, so client extensions like `argument-hint`, `disable-model-invocation`, `hooks`, and `model` silently vanished from every imported copy, from re-syncs, from renames, and from web editor saves. Unknown keys are now captured into an `extra` field, preserved verbatim through parse, render, the JSON API, and the editor's save path (values exact; ordering may normalize), and never interpreted by gridctl. Registries written before the fix heal on their next `gridctl skill update`, which re-imports the upstream file with the keys intact (#1009)

- `gridctl skill add` and `gridctl skill update` now actually install upstream changes. The per-repo clone cache under `~/.gridctl/cache/repos/` was fetched on every run but the fetched commits never reached the checked-out worktree, so unpinned sources reported "already up to date" forever while branch-pinned sources reported "update available" on every run and reinstalled the same stale content. Updates now land the worktree on the freshly fetched remote-tracking tip for every ref shape (unpinned default branch, branch names, tags, and semver constraints, whose new matching tags were previously never detected), falling back to a fresh shallow clone when the fetched objects are incomplete. Cache mutations are serialized per repository, and offline behavior is unchanged: a failed fetch still degrades to the cached content with a warning. No manual cache clearing is needed after upgrading; the first `skill update` installs correctly. One exception: a skill whose upstream version was reviewed and skipped as locally-edited during a web sync recorded that version as seen, so it needs one `gridctl skill update --force <name>` to install it (#1010)

## [0.1.0-beta.15] - 2026-07-28


### Features

- Library workspace next wave, five additions. An **Import** action in the Library header opens the skill import wizard directly, so adding a git source no longer routes through the generic Create Resource flow (the registry sidebar already had this entry point; the workspace did not). The inspector warns when a skill's instructions reference `scripts/`, `references/`, or `assets/` paths that ship no installed files, which is the silent failure mode for packages imported before supporting-file install landed: the agent follows a step that invokes a file which is not on disk. The warning names only the directories actually absent, offers a one-click sync for a git-owned skill, and keys on path shape rather than prose, so a body that merely says "run the build scripts" does not trip it; it also stays silent while the file list is unknown, since absence of data is not evidence of a broken package. A **Stale** usage facet joins Never used, counting active skills whose last call falls outside a selected lookback window, with the 24-hour, seven-day, and 30-day vocabulary shared with Tools Audit rather than forked, round-tripping through `?window=`. It follows the same nullable-count contract as Never used and adds a second unknowability condition beyond the cold-window gate: a 30-day question cannot be answered by three days of tracking, so the card renders a dash, goes inert, and suggests a shorter window. A **Select mode** toggle pins the multi-select checkboxes visible instead of revealing them on hover, persisted alongside the compact-rows preference; any live selection still implies it, so the toggle is a discoverability aid rather than a gate. Finally, the inspector's Modified chip becomes a named button that opens the same upstream diff the editor's Compare action uses. The selected skill's instructions now load once per selection rather than on Instructions-tab open, since the package check needs them too; one request serves both, and the tab opens without a spinner

- Least-privilege secret injection and Variables workspace trust: a `secrets.sets` entry can now be written as a mapping that names the workloads it reaches (`- name: github-creds` with `servers:` and/or `resources:`), so a set injects only where it is needed instead of into every MCP server and resource. Scoping is opt-in per entry and an entry left as a bare set name keeps the original fan-out, so existing stacks are unaffected; unscoped entries also survive a load and save cycle as bare names rather than being rewritten as mappings. Naming a server or resource the stack does not declare is now a validation error, as is listing a scoped set more than once (repeated entries inject the union of their scopes, so a bare entry beside a scoped one silently widens it back to a fan-out; repeating a bare name stays valid, since that was accepted before scoping existed and is idempotent), and the previously unvalidated `secrets:` block is checked for the first time. Scoping mistakes fail closed rather than widening access: a misspelled key (`server:` for `servers:`) is rejected outright instead of being silently dropped and leaving the entry unscoped, and an explicitly empty scope means no workloads rather than all of them, reported as an error since a set that injects nowhere is an unfinished edit. The usage index tracks scoping in lockstep: a scoped set contributes one consumer per receiving workload (carrying `target` and `targetKind` alongside the unchanged set `name`), those rows are clickable through to the Stack canvas node, and several targets of one set fold into a single expandable row rather than repeating. The Variables workspace gains a drift banner naming stack references that no stored variable satisfies, with a Create action that seeds the import preview with the missing keys, turning an apply-time `missing variable(s)` failure into an authoring-time warning; references carrying a default (`${var:KEY:-fallback}`) are valid config and never reported, and a locked vault reports nothing rather than flagging everything. Three single-select chips (Explicit refs / Set-injected / Not referenced, with counts over the whole vault) narrow the list and round-trip through `?ref=`, disabling themselves when the usage index is unknown so a failed fetch never reads as "nothing is referenced". The import preview gains a "Skip all conflicts" toggle so a bulk import can avoid every overwrite in one click, and the inspector shows when each value last changed. New endpoint `GET /api/var/drift` (canonical path only, never mirrored onto the deprecated `/api/vault` surface)

- Tools workspace next wave: with Audit on, filter chips (All / Used / Unused / Disabled, with per-state counts) narrow the list, and a Destructive risk chip (available regardless of Audit) narrows to tools whose server reports `destructiveHint: true`; a sort control orders by name, most recent use, call count, or estimated cost (picking a usage sort loads the usage snapshot even with Audit off; the default keeps server-advertised order and an active search keeps relevance order). Facets round-trip through `?filter=`, `?sort=`, and `?risk=` and persist across sessions (URL wins). Rows and the detail rail surface MCP tool annotations: compact chips for declared hints and a Hints section that spells them out with the spec-mandated "reported by the server, not verified" framing (unannotated tools state the pessimistic default instead of a blank). Fleet actions gain "Disable unused" plus a hand-picked multi-server scope: the plan derives from the audit window's usage, previews exact per-server tool lists with the `observedSince` caveat, refuses any server whose whitelist would empty (an empty whitelist means expose-all), and applies atomically with one reload. The header shows "Optimize suggests N unused tools (7d)" with a jump into Audit whenever the optimize `unused_tool` heuristic has findings (never rendering "0" on a young gateway). The Access modal gains the per-server tool axis from the Stack Access Lens (All / Custom checklists sharing the same flatten/seed state model, empty-Custom saves blocked, untouched axis preserved on save). `GET /api/tools/catalog?include=all` serves the pre-whitelist inventory so disabled tools keep descriptions, schemas, and annotations in the UI; the parameterless response is unchanged. The dirty server's rail badge shows the draft count while editing, the center column widens to `max-w-4xl`, and a user guide lands at `docs/tools-workspace.md`

- Metrics drill-down, Overview home, and optimize handshake: selecting a server now refocuses the center charts on that server's token and cost series with the fleet as a dashed context line, a focused-share line under the KPI cards ("atlassian: 12k tokens · $0.42 · 34% of window"), entity-named chart titles and aria summaries, and a "Focused" chip that clears back to the fleet view; clients focus the cost chart only (no per-client token series exists — the gap is stated, never papered over), tools keep fleet charts with an honest note, and an entity with no samples in the window keeps fleet charts plus a note instead of a fabricated flat line. Overview becomes a real home: Top Servers and Top Tools previews (five rows, cost-ranked, row click or View all jumps into the scope) and a "Savings opportunities" card showing the top optimize findings by weekly impact with severity chips — a finding click deep-links to the affected server or server-qualified tool row, and an info-only report collapses to one quiet line. The sidebar's Optimize section and the savings card now share one polling implementation and cadence. The Models scope earns its keep with a Model Breakdown table (share, cost, entities using the model, provenance mix) derived from the same single-tier aggregation as the mix bars so the two can never disagree. The Tools scope gains a search input (`/` focuses it), server facet chips, and a priced/unpriced facet — URL-synced as `?q=`/`?server=`/`?priced=`, driving keyboard navigation from the filtered list, with a result count and a distinct filtered-empty state. The center column widens for chart legibility, and stack servers with no recorded traffic now appear in the Per-Server breakdown as zero rows (with unknown cost as an em dash) so an unused server is selectable rather than absent

- Pins review actions and findings ergonomics: the drift panel gains copy (the full `live_server_hash` for `gridctl pins approve --expect`), export (the rendered diff as JSON for audit), and re-verify (read-only recompute; also available on clean servers) actions, and approving five or more changes or any drift carrying warn-or-critical findings now interposes a confirmation that restates the change counts and findings before re-pinning (small clean drifts stay one click). Resetting a server's pins moves into the UI behind a clearly separated danger action with a blast-radius confirmation naming the server and consequences. In the pinned-records table, findings collapse behind their summary badges (click to expand) and a Findings-only toggle filters to flagged tools with a visible count; `?view=findings` deep links land with the filter active, and the choice persists. Full hashes appear on hover wherever shortened hashes render, and the command palette gains a Pins section (approve, re-verify, export, copy hash, filters, and reset), scoped to the workspace

- Pins review core: drift review is faster to read and land on. Changed descriptions render a word-level diff (removed tokens tinted on the old row, added tokens on the new row, every token still escaped) instead of two paragraphs to eye-diff, backed by a shared dependency-free diff module; the review column widens while a drifted server is selected, and on wide panes each drifted tool splits into review prose on the left and its schema panels on the right, so JSON walls no longer interrupt the reading flow (stacked as before on narrow panes). Every schema panel gains an expand button opening a full-viewport overlay with line numbers, add/remove counts, and soft-wrapped long lines, so property descriptions wider than the inline column (over half of real pinned schemas) are readable without dragging a horizontal scrollbar; pins recorded before schema capture open a single-pane viewer of the new schema instead of a comparison against nothing The rail gains an attention-only filter (on by default whenever any server has drift or warn-or-critical findings; the explicit choice persists) with an all-clear state, and rows carry the tool count and last-verified age; Enter moves focus from the rail to the Approve button. Deep links gain section targeting via `?view=drift|findings|tools`: the status-bar chips, the drift and findings toasts, and the Stack canvas drift annotation (now a link) all land on the affected server with the relevant section scrolled into view. Also fixes the "pinned before schema capture" panel rendering pretty-printed schemas as one line of literal `\n` sequences

- Resizable Traces workspace layout: the trace list and waterfall now share a draggable, persisted split (list collapsible to zero via the separator or a new `[` shortcut that keeps the selected trace and URL intact), and span detail moves from a fixed right column to a height-resizable bottom drawer under the waterfall, so opening attributes no longer steals timeline width; the drawer height persists separately and its sections reflow into columns at drawer widths. The span-name column in the waterfall gains its own drag handle (18–45%, persisted), Escape now closes the span drawer first and the waterfall second, double-clicking any panel separator resets that split, and the list's Client/Server/Spans/copy columns now drop based on actual list width instead of selection state. Separators match the grip style, keyboard resize, and layout persistence of the Logs and Metrics workspaces, in both the main window and the detached popout

- MCP-native Logs workspace: the shared log surface gains click-to-filter source badges with a removable `source:` chip, a one-click Errors toggle that remembers and restores the previous level selection (round-tripping the `level=none` sentinel), search that also matches structured attr values with match spans highlighted in the message, and an expand panel rebuilt around promoted MCP fields (tool, server, client, replica, duration, error) with copy message, copy raw, and copy trace ID actions, a full-width trace chip that pivots to the waterfall, and the remaining attrs collapsed under "Other attributes". Tool-call log lines now carry the calling `client` in attrs (omitted when no client identity is on the context), and `GET /api/logs` serves a `{logs, total, bufferCapacity}` envelope so the view can label its window honestly against the ring (`docs/api-reference.md` updated). A view-options popover holds a 200/500/1000 window size control (URL-synced as `?n=` and carried through the popout hand-off), 1m/5m/15m time presets over buffered timestamps (anchored to the last completed load so the window freezes while paused, composing with the view-local Clear watermark), a line-wrap toggle, and opt-in relative timestamps with the absolute value on hover. The list virtualizes above 200 rows while preserving follow-tail, gains keyboard navigation (j/k or arrows, Enter expands, `/` focuses search, Esc collapses) keyed by entry identity so live refreshes never teleport the cursor, with a footer hint strip and grid semantics for assistive tech. The filtered view exports as JSONL or TXT, the source rail pivots per server to Metrics, the workspace and detached window now render one shared control cluster (Live/Paused indicator included; the detached footer pulse respects reduced motion), preferred source, level set, wrap, relative time, and window size persist across sessions (URL params always win, and Clear filters resets the persisted source and levels), the command palette gains a Logs section (Errors only, clear filters, wrap, copy, export, window sizes, open detached), and the URL param `?agent=` is renamed to `?source=` with the legacy name honored permanently on read

- MCP-native Traces workspace: the trace list now leads with the tool call, not the trace ID. New Tool and Client columns (promoted onto the summary API from span attributes), relative timestamps with absolute hover, a duration heat bar scaled to the visible page, and a Tool calls | All segment control that hides infrastructure traces by default with a visible "N infra hidden" count; trace IDs move to a hover copy action. Live streaming gains a Pause toggle (display freeze only; collection continues), and a filters popover holds min-duration, 5m/15m/1h/buffer time presets, and clear. The waterfall gains a timeline ruler, self-time on parent spans, a critical-path highlight for multi-span traces, and header actions to copy the trace ID, copy a deep link, and export the trace as spec-shaped OTLP JSON (new `GET /api/traces/{traceId}/otlp`, camelCase keys and hex IDs). Span detail promotes MCP fields first (tool, server, client, transport, replica, model, tokens, and a cost pill from `gen_ai.cost.usd`), collapses the rest under "Other attributes", and surfaces the error message from exception events on failed spans. The list supports keyboard navigation (arrows or j/k, Enter opens, Esc closes) with a footer hint, selection stays sticky across live refreshes, the segment and server filters persist across sessions and deep-link via `?seg=`, the result count shows ring-buffer occupancy against `max_traces` (amber at 90%), and distinct empty states cover hidden-infra, filtered-out, evicted-trace, and tracing-disabled (`GET /api/traces` now reports `tracingEnabled`, `bufferSize`, and `bufferCapacity`). The waterfall pivots to Metrics for the same server alongside the existing Logs pivot. Gateway spans additionally emit the draft-semconv `gen_ai.tool.name` attribute

- The telemetry persistence dot on Stack canvas server cards no longer renders in its "off" state: persistence is disabled by default, so the gray marker sat permanently on every server card as noise. The outlined "pending" ring (persistence enabled but no files written yet, a silent-failure signal) still renders, and per-server details remain in the telemetry sidebar

- Declarative client linking: a top-level `link:` block in stack.yaml lists the LLM clients this stack's gateway should be connected to, and `gridctl apply` reconciles it once the gateway is healthy — so a committed stack file fully describes servers and clients ("clone, apply, clients wired"). Entries are a bare slug (`- claude`) or an object with `group` (links the group endpoint, entry name defaults to `gridctl-<group>`), `client_id`, and `name`. Reconcile is additive and idempotent: already-linked clients are silent no-ops, not-installed clients warn and skip (stack files travel between machines), conflicting foreign entries warn with a `--force` hint, and removing an entry never unlinks anything (removal stays explicit via `gridctl unlink` or the new `gridctl destroy --unlink`, which removes the declared entries on teardown). Validation enforces known slugs, one entry per client, and existing group references; with a `link:` block, `apply --flash` is ignored with a notice; foreground applies reconcile through a new gateway post-ready callback (which also fixes `--flash` never firing under `-f`); `gridctl plan` shows pending link actions in a separate section (JSON: `clientLinks`). The web UI gains a Connections workspace (Cmd+9) listing every supported client with declared/detected/linked badges and staged link toggles applied behind a per-client config diff preview, backed by new `POST`/`DELETE /api/clients/{slug}/link` and a preview endpoint that keep stack.yaml and client configs in lockstep; `GET /api/clients` now reports `declared` and `linkEntry`. The creation wizard grows a Client Link card that opens the workspace

- Compact cards are now the Stack canvas default: nodes render the consolidated view (name, status, token count) unless the full-card view is toggled on via the canvas toolbar or the palette's "Toggle compact cards". Existing installs pick up the new default once; toggling afterward persists as before

- The bottom slide-up panel is removed: its content now lives in top-level workspaces (Logs, Traces, Metrics, and Pins), and the Spec view relocates into the Stack workspace as a slide-over pane opened by the status-bar Spec chip, the command palette, or `/stack?spec=1`. Every workspace gains the vertical space the panel row previously reserved. Cmd+J, formerly the panel toggle, now jumps to the Logs workspace; the "Open Logs", "Open Traces", "Open Spec Editor", and per-trace palette commands deep-link the matching workspaces, "Open Variables" navigates to the Variables workspace, and a server's "View Logs" inspector action lands on the Logs workspace filtered to that server. Detached popout windows are unchanged

- Logs and Traces are now top-level workspaces (Cmd+7 / Cmd+8) alongside Stack, Library, Variables, Tools, Metrics, and Pins. The Logs workspace shows the aggregate stream from the gateway and every server with no node selection required: a source rail (all sources / gateway / per server) filters the same stream client-side, each line in the all-sources view is badged with its origin, and severity, search, and source state live in the URL (`?agent=`, `?level=`, `?q=`) so views are shareable. The Traces workspace carries the full trace list, waterfall, and span detail with URL-synced selection (`?trace=`). The two correlate: a log line with a trace ID pivots to its trace, and a trace's waterfall pivots to the logs for that trace. Detached popouts move to `/logs-window` and `/traces-window` (existing window handles keep working), workspace nav pills collapse to icons below 1360px so all eight fit, and the bottom-panel tabs are unchanged

- Full-view client cards on the Stack canvas are now horizontal: the client icon sits on the left with the name, transport, and linked status stacked beside it in a wider, shorter card, replacing the centered column layout and its dead space above the icon. Compact cards are unchanged

- Automated npm advisory fixes: a scheduled `NPM Audit Fix` workflow runs `npm audit fix` against `web/` weekly (and on demand via `workflow_dispatch`) and opens or updates a single reviewable PR when the lockfile changes, listing the advisories it addresses. Only semver-compatible updates are applied; the frontend CI job's `npm audit --audit-level=high` gate is unchanged, this keeps main passing it so fresh advisories stop failing unrelated PRs

- Tool groups in the web UI: the Tools workspace gains a Groups panel (header button, shown only when a `groups:` block exists) listing each group with its member count, description, copyable endpoint URL, and a link-command hint; selecting a group shows the exposed surface per member tool — the renamed name with its canonical origin, the rewritten description beside the downstream original so operators see exactly what a group client's model sees, and annotation chips for declared hints. Tool rows in the workspace carry compact group badges for members. `GET /api/groups` additionally returns a `members` array with each exposed tool's post-rewrite name, description, merged annotations, and origin. Stacks without groups are visually unchanged

- Tool groups: a `groups:` block in stack.yaml defines named cross-server tool bundles, each served at its own MCP endpoint `/groups/{name}/mcp` and linkable per client with `gridctl link <client> --group <name>` (entry name defaults to `gridctl-<name>`). Membership is servers plus tools minus exclusions; per-tool overrides rename tools at the exposure boundary, rewrite descriptions, and inject typed MCP annotation hints (`read_only_hint`, `destructive_hint`, `idempotent_hint`, `open_world_hint`). Renames exist only at that boundary: dispatch, per-client scoping, limits, schema pins, and telemetry keep operating on canonical `server__tool` names, calls outside a group's surface get a model-readable denial naming the group, and code mode on a group session searches and executes only the group surface. Downstream tool annotations now pass through to clients on every endpoint (they were silently dropped before). Pin fingerprints are untouched by rewrites; `gridctl pins diff` and the diff API flag groups whose description overrides touch a drifted tool, and a rename whose original name still appears in an active skill warns at startup. Validation covers membership references, rename collisions, and the client-side 64-character tool-name budget; edits hot-reload. Surfaces: `gridctl groups` (table, `--verbose`, `--plain`, `--format json`/`--json`, exit codes 0/2) and `GET /api/groups`

- Native skills projection: `gridctl skill project sync <skill>` places selected active registry skills into native client skill directories, so skills work in clients that never fetch MCP prompts (Antigravity, Grok Build) and auto-trigger in clients that read the AgentSkills format from disk. Three targets: `agents` (`~/.agents/skills/`, the vendor-neutral interop dir read by Zed, Goose, OpenCode, VS Code, and Grok Build; always available, created on first projection), `claude-code` (`~/.claude/skills/`), and `antigravity` (`~/.gemini/config/skills/`, always copied since Antigravity's symlink discovery is unverified). Skills are symlinked into the registry by default so edits propagate without a re-sync; `--copy` materializes copies with tree-hash drift detection instead. Nothing is projected until explicitly named (a deliberate divergence from `ctx sync`'s all-by-default: projecting every active skill would flood client discovery context), and the projection set lives in `~/.gridctl/skillsync.lock.yaml` with per-entry ownership, so destinations gridctl did not create are never clobbered silently (`--force` backs up, then replaces) and `unsync` removes only gridctl-created artifacts, backing up copies first. The set reconciles under the daemon after every registry refresh: deactivating, deleting, or updating a projected skill removes or refreshes its projections automatically; the lockfile is flock-guarded so the CLI and daemon never corrupt it. Companion verbs: `skill project status` (SKILL / CLIENT / CHANNEL / STATE / TARGET with in-sync / stale / drifted / target-missing) and `skill project unsync`; all three support `--format json`/`--json` with a `schema_version`, `--plain` where tabular, `--dry-run` where mutating, and 0/1/2 exit codes. The MCP prompt channel is unchanged; docs/skills.md now carries a per-client matrix of which channel reaches which client

- Limit consumption in the Metrics workspace: budgets declared under `limits:` render as consumption-vs-cap bars on the matching per-client, per-server, and per-tool breakdown rows (normal accent, amber past the warn threshold, red when exceeded; a fresh window shows a real $0.00, never the unknown-cost dash), a Limits panel on the overview scope lists every configured budget and rate limit (including entries whose scope has no traffic yet, elevated states first), and a status-bar chip appears when any limit is near or over its cap and jumps to Metrics. The detached metrics window carries the same overlay. Stacks without a `limits:` block are visually unchanged

- Budget caps and rate limits: a declarative `limits:` block in stack.yaml is enforced at tool-call dispatch. Budgets cap attributed dollar spend per client, server, or tool over calendar-aligned daily/weekly/monthly windows (local time; weekly starts Monday), with an optional `warn_at_percent` soft tier; rate limits are token buckets (`calls_per_minute` plus `burst`) on the same scopes. Enforcement is check-then-settle: calls are admitted against recorded spend and settle their own cost after completion, so in-flight calls can overshoot a cap by their own cost and the next call is denied. Denials return in-band tool errors carrying the cap, consumption, reset time, and retry guidance so agent LLMs stop retrying; rate denials include a retry-after hint. Budget spend persists in a ledger under `~/.gridctl/limits/` (independent of telemetry persistence), so a daemon restart never refills a spent budget, and `limits:` edits hot-reload with current-window spend carried over (raising a cap never resets its counter). Code-mode sandbox calls pass through the same enforcement. Budgets govern attributed cost only (unpriced calls settle nothing); rate limits are the documented backstop. Surfaces: `gridctl limits` (table, `--plain`, `--format json`/`--json`, exit codes 0/1/2) and `GET /api/limits`

- Server catalog and discovery: `gridctl search [query]` and `gridctl add <name>` install MCP servers by name instead of hand-written `command`/`args`/`env`. The catalog merges a curated set embedded in the binary (15 vetted servers with correct inputs and secret flags) with the official MCP Registry (registry.modelcontextprotocol.io, API v0.1), fetched on demand and cached for an hour under `~/.gridctl/cache/catalog` with stale-cache and curated-only fallbacks so search never fails because the registry is down. `add` resolves curated names first, then full reverse-DNS registry names, prompts for required inputs (masked secrets are stored in the variable store so stack.yaml only carries `${var:KEY}` references; unset required values become `${var:KEY}` placeholders with a `gridctl var set` hint), renders an import-style plan, and appends through the same backed-up, comment-preserving, validated write path as `gridctl import`. Registry entries map onto stack blocks per package type (`oci` to container images, `npm` to `npx`, `pypi` to `uvx`, remotes to external URL servers with bearer/header auth); `mcpb`, `nuget`, and `cargo` fail with a clear unsupported-type error. Deleted registry entries never appear, deprecated ones warn and require confirmation, and registry results are labeled as community publications rather than vetted entries. Flags: `--source curated|registry|all` and `--plain` on search; `--yes`, `--dry-run`, `--file`, `--name`, `--no-vault` on add; `--format json` or `--json` on both, with exit codes 0/1/2

- Catalog picker in the add-server wizard: the MCP Server template step gains a Templates/Catalog toggle whose catalog view searches the same merged curated-plus-registry catalog as `gridctl search` (new `GET /api/catalog` endpoint, with stale-cache and curated-only degradation when the registry is unreachable). Entries show source-tier badges (curated entries are vetted; registry entries are labeled community publications), deprecation and unsupported-package markers, and required inputs; selecting one pre-fills the existing form, so the YAML preview and review step match what `gridctl add` writes for the same entry, with secret inputs pre-filled as `${var:KEY}` vault references rather than literals

- `gridctl import [client]`: the reverse of `gridctl link`. Scans installed LLM clients for existing MCP server definitions (read-only; client configs are never modified), normalizes the per-client dialects (mcpServers/servers/mcp/context_servers/mcp_servers/extensions keys, Continue's array form, url/serverUrl/uri/httpUrl, transport spellings, Goose's cmd/envs, JSONC and BOM tolerance), unwraps `npx mcp-remote <url>` bridges and Windows `cmd /c` wrappers into direct URL servers, and appends selected servers to stack.yaml through the same comment-preserving atomic write path the web wizard uses, with a `.gridctl-backup-<timestamp>` taken first and the post-import stack validated before a byte lands on disk. Identical servers found in several clients import once with provenance (`found in cursor, claude, vscode`), the gateway's own entry is filtered out, name collisions default to skip (interactive rename or overwrite offered), and plaintext secret-looking env values are offered into the variable store as `${var:KEY}` references with genuine references (`${env:...}`, `${input:...}`, `op://...`) preserved untouched; secret values never appear in output. Flags: `--all`, `--dry-run`, `--yes`, `--file`, `--name`, `--no-vault`, `--format json` (pure-JSON stdout with a schema_version) with exit codes 0/1/2. Claude Code detection now honors `CLAUDE_CONFIG_DIR`


- Poisoning-aware pins: tool definitions are scanned with local heuristics when a pin is first taken and when drift is presented for approval, covering hidden-instruction phrases (P001), sensitive-file references (P002), sensitive-action language (P003), suspicious emphasis words (P004), hidden Unicode with Tags-block payloads decoded as evidence (P005), and cross-server tool shadowing (P006). Matching runs on normalized text (invisible characters stripped, NFKC, homoglyph and leetspeak folding) so common evasion does not defeat it, and quoted matches are downgraded so tools that document attack phrases are not flagged as attacks. Findings are advisory (info/warn/critical with confidence and matched snippets, always escaped) and surface beside the drift diff in `gridctl pins diff` text and JSON output (schema_version 2), `GET /api/pins/{server}/diff`, pinned records, the Pins workspace (per-tool finding cards, a status-bar findings chip, and a one-time toast), and the add-server wizard's discovered-tools step. Nothing blocks: exit codes are unchanged unless `pins diff --fail-on-findings warn|critical` is passed, the Approve action stays available, and hashes are untouched. Configurable via `schema_pinning.scan` (default on) and `scan_ignore: [codes]`


- Native authentication for external URL servers via an optional `auth:` block: static `bearer` tokens and custom `header` values, plus full OAuth 2.1 brokering (`type: oauth`) that replaces the `npx mcp-remote` bridge. gridctl discovers the authorization server (RFC 9728/8414), registers a client (RFC 7591 with `application_type: native`, or pre-registered `client_id`/`client_secret` for servers like Slack that refuse dynamic registration), runs the authorization-code + PKCE S256 browser flow through a callback on the gateway's own port, validates `iss` (RFC 9207), sends RFC 8707 resource indicators on both legs, and stores tokens encrypted at rest under `~/.gridctl/oauth/` keyed by server URL so one login serves every connected client and survives daemon restarts. Rotating refresh tokens are persisted before first use; a rejected refresh self-heals into a `needs auth` state instead of retry-looping. New `gridctl auth login|logout|status|reset` command group (`--no-browser` and `--manual` for SSH, `--format json` with 0/1/2 exit codes), `/api/auth/servers` and per-server login/wait/logout/reset endpoints, `authStatus` on server status payloads, and an actionable `needs auth` state (never an error) across `gridctl status`, `apply` hints, and tool-call error messages. The add-server wizard probe reuses stored tokens and reports `needs_auth` distinctly

- Declare authentication for external URL servers directly in the add-server wizard: the External URL form gains an Authentication section (bearer token, custom header, or OAuth 2.1) whose secret fields nudge toward `${var:KEY}` references, the YAML preview emits the matching `auth:` block, and Test Connection probes with the declared credentials (auth secrets are scrubbed from probe error messages alongside env values). The authorize flow is also polished: a Cancel button during the waiting phase, closed-popup detection that resets to idle, a clickable "Open authorization page" link when the popup is blocked, a remote-daemon CLI hint, an aborted wait long-poll on unmount, and canvas nodes pending authorization now render amber chrome only instead of co-rendering the red health strip. Auth REST endpoints are documented in the API reference, and troubleshooting covers `gridctl auth reset` plus the token-store threat model

- Surface downstream OAuth authorization in the web UI: servers pending authorization render as an amber "needs auth" state on the Stack canvas (never as an error) with a key indicator on the node, the server sidebar gains an Authorization section (status, issuer, scopes, token expiry, Authorize/Re-authorize and Sign out, with a copyable URL fallback when the popup is blocked), the gateway sidebar and status bar show a pending-authorization count that jumps to the first pending server, the add-server wizard renders a distinct requires-authorization notice instead of a generic probe error, and a one-time toast fires when a server transitions into the pending state

### Refactoring

- Removed the gateway version string from the header, where it sat beside the wordmark competing with the brand mark and the workspace switcher for the app's most valuable real estate. It already appeared on the gateway node in the Stack canvas and in the gateway inspector sidebar, and those are the accurate homes for it: the version is a property of the gateway reported over `/api/status`, so beside the wordmark it read as the version of the web UI itself. This is the third header removal in the same direction, after the gateway-name chip, connection state, and server counts moved to the status bar. Nothing changes when the gateway is unreachable, since the header string was already conditional on gateway info being present; while connected, the version is now reached by way of the Stack workspace or `gridctl version`

- Removed the header Variables panel, a second, weaker rendering of the same vault that could be opened while already standing on the Variables workspace. It never received the usage index, so it showed no consumer counts anywhere, and its delete confirmation omitted the consumer count and bulk-injection warning the workspace shows, making it the less safe of two delete paths for identical data. The header gear that opened it is gone entirely, since the workspace switcher sits in the same header and Variables has been a first-class tab since the bottom-panel removal; the creation wizard's Secret tile now navigates to the workspace instead of opening the panel


- Rename the Topology workspace to Stack in the web UI: tab label, command palette ("Go to Stack"), document title, and cross-references now match the `stack.yaml` / `gridctl` vocabulary the backend adopted when `--topology` became `--stack`. The tab icon changes from a network glyph to a layers glyph to match the label. The route moves from `/topology` to `/stack`; old `/topology` bookmarks redirect

### Bug Fixes

- `GET /api/stack/export` no longer corrupts `${var:KEY}` references. The API's secret sanitizer skipped values already prefixed `${vault:` but not `${var:`, so a canonical store reference was mistaken for a raw secret and rewritten to `${vault:<workload>_KEY}`, naming a key absent from the variable store and breaking an export followed by a re-apply. Values that are already store references now survive in whichever form they were written, and newly sanitized secrets are emitted in canonical `${var:}` form, matching `gridctl export`. No secret was ever exposed: the defect substituted one reference for another, and genuine raw secrets were still sanitized. The CLI export path carried the correct logic already; this brings its API twin in line

- Library workspace bug sweep, five defects. The "Never used" KPI no longer reports the whole active catalog on a freshly started gateway: a usage window younger than 24 hours (matching the backend's own minimum observation period) cannot support the claim, so the card renders a dash instead of a count, goes inert, and explains on hover that tracking has just started and those skills have not been idle, only unobserved. A stale or hand-typed `?usage=unused` is inert under the same condition rather than selecting every active skill, the bulk action bar carries the tracking-window caveat while the window is cold, and bulk Disable now confirms before it writes (previously only bulk Delete did, so one Select all plus Disable from a misleading facet could take the whole library out of service in two clicks with no confirmation). A registry where every skill genuinely is unused after a long window still shows its count: the discriminator is elapsed time, not an empty usage map. `useSkillUsage` now reports `fetchedAt` and `error` so loading, unavailable, cold, and live are four distinguishable states

- Icon-only buttons now expose an accessible name. `IconButton` set `title` but no `aria-label` and rendered a bare `<svg>`, so 276 buttons on `/library` were announced as just "button" and were unreachable by role; `title` is not reliably exposed as an accessible name across assistive technology. Every call site already passed a tooltip, so the name comes from there, with a new `ariaLabel` prop for the cases where the two should differ. `title` is unchanged, so hover text and existing title-based queries keep working. The skill editor's instructions textarea, previously unlabeled, gains a name as well

- `GET /api/registry/skills` no longer ships every skill's Markdown body. Bodies were about 860 KB of a 970 KB response on a registry of 89 skills, and the web UI refetches that list every three seconds on every workspace, so the catalog cost roughly a gigabyte an hour and a Markdown-sized JSON parse on the main thread per cycle, for content no list view reads. The surfaces that render instructions (the inspector's Instructions tab, the skill editor, and the sidebar's expand-row preview) now fetch the single skill on demand, which is both smaller and exact: the preview is one request per expanded row, not 89 previews on every poll. Per Article VIII the original shape stays reachable at `?full=1`. The editor blocks saving until the body has loaded or has failed to load, so an unhydrated editor can never write an empty body over a skill's instructions

- A skill in a flat directory no longer reports its own directory name as its category. `skillCategory('docx')` returned `"docx"`, so on a flat registry every skill became its own one-member category, Group-by-Category degenerated into a list, and Details showed a Category row restating the skill. A category now comes from an explicit `category` in the skill's frontmatter metadata, or from the first segment of a nested `dir`, and is absent otherwise; the Details row, the table's Category column, and the Group-by-Category option all hide when nothing has one

- The skill inspector's file tree gains a sort control (name, size, or path). Files rendered in backend directory-walk order, which is neither stable nor useful for finding one. Ordering applies within each directory bucket, and the control is hidden for a skill with a single file

- Skill import no longer drops supporting files: `gridctl skill add` and `gridctl skill update` now install each skill's `scripts/`, `references/`, and `assets/` directories alongside its `SKILL.md`, plus top-level `LICENSE`, `NOTICE`, and `COPYING` files. Previously only the rendered `SKILL.md` was written, so a package whose body instructs the agent to run a bundled script (Anthropic's `docx`, `pptx`, `xlsx`, and `pdf` among them) installed clean, reported "Files 0", rendered its full instructions, and then failed at use time with a missing-file error, producing degraded output rather than a visible break. The copy is an allowlist rather than an exclusion list, so a repository-root skill never drags in `.git`, a skill never absorbs a nested skill beside it, and files outside those paths are not copied; symlinks are skipped, per-file size (5 MiB) and per-skill file count (500) are capped, and anything left out is surfaced as an import warning. Executable bits are preserved, since that is what makes a bundled script runnable. `ImportedSkill` gains `filesCopied`. Re-import replaces the three managed directories wholesale so upstream deletions do not linger, and leaves everything else (`SKILL.md`, its timestamped backups, and the origin sidecar) untouched; files added by hand under a managed directory are replaced on the next sync, since the registry cannot distinguish them from imported ones. Known gap: drift detection and fingerprinting still hash `SKILL.md` alone, so edits to installed supporting files are not yet reported as drift and a forced update backs up only `SKILL.md`. Existing installs pick up their missing files on the next `gridctl skill update --force`

- The skill security scan now covers installed supporting files, not just the `SKILL.md` body, and runs against the clone before anything is written, so a skill rejected by the scan leaves no partial install behind. Every text file being installed is scanned, not just recognized script extensions: a git clone only carries 644/755, so an extension allowlist would never look at a non-executable payload, and reference documents are prose an agent reads and acts on once a skill is projected. Binary content is skipped by content sniff rather than by extension. Only high-severity matches block an import; lower-severity matches are reported as findings without gating, because the pattern set is tuned for prose and shell snippets and fires routinely on ordinary Python. Verified against `anthropics/skills`: all 17 packages still import without `--trust`. Relatedly, `gridctl skill update` and the API sync handlers no longer pass `trust` unconditionally: every sync previously refreshed skill content with the scan gate disabled by construction, which was harmless while only the body was scanned and is not once executable files are installed. Syncs now fail closed and report findings; `gridctl skill update --trust` is the opt-out

- `gridctl skill add --rename` now validates its argument. The value becomes both the skill name and the destination directory, and it was previously unchecked, so `--rename ../../x` resolved outside the registry root. Combined with the supporting-file installer above, that would have made a mistyped rename delete `scripts/`, `references/`, and `assets/` under an arbitrary directory. The rename is rejected up front and the resolved destination is asserted to stay within the skills root

- Skill file counts are now recursive. `countSupportingFiles` walked only the direct children of `scripts/`, `references/`, and `assets/`, so a package with `scripts/office/*.py` reported a small fraction of its real contents: Anthropic's `docx` counted 3 files against 59. The Library's Files column and the Files sort now reflect actual package size

- Variables workspace honesty and edit gaps: the usage index served by `GET /api/var/usage` now synthesizes a `secrets-set` consumer for every variable a `secrets.sets` block injects into server env (keys and set names only, never values; a locked vault degrades to explicit references), so set-injected secrets no longer count as "Unreferenced", the inspector's orphan callout with its Delete shortcut only appears for true orphans, consumer rows render the injection ("set: dev · injected into server env"), and the delete confirmation warns about the bulk injection it previously suppressed for exactly those keys. A failed usage fetch now reports "Unreferenced" as unknown instead of counting every variable as unreferenced. The inspector's edit mode gains the type selector and Secret/Plaintext toggle the create form always had (validating against the selected type, with a visibility warning when flipping a secret to plaintext) and seeds the form with the fetched stored value, which also makes explicit empty values savable for types whose validator permits them without ever silently wiping an unrevealed secret. The "Move to set…" select gains an "Unassigned (remove from set)" option (backed by the existing empty-set API; leaving an actively injected set asks for confirmation), the sets rail gains an Unassigned pill (`?set=__none__`), and "Jump to Stack" now actually navigates to the Stack workspace with the node selected and the sidebar open instead of toasting "open Stack to inspect". Search additionally matches consumer server names and field paths ("zapier" finds `ZAPIER_MCP_TOKEN`), consumer field paths carry plain-language tooltips ("argument 5 of the server command in stack.yaml" for `command[4]`), re-locking an encrypted vault shows lock-mode copy instead of first-encrypt copy, first-time encryption enforces an 8-character minimum with a 12+ recommendation (re-lock accepts existing shorter passphrases), unlock failures distinguish a wrong passphrase from transport errors, and a dismissible banner nudges toward encryption while secrets sit unencrypted on disk

- Tools workspace trust and audit fixes: saving an empty tool selection is now refused at every save path (an empty whitelist means expose-all, so Clear then Save silently persisted "disable everything" as "expose everything", including tools just unchecked, and the post-save refresh re-checked every box so the inversion looked like a clean save); the Save button disables on an empty draft and the count line switches to a danger warning instead of the neutral "empty means all tools exposed" help text, in both the Tools workspace and the sidebar editor. Leaving the workspace with unsaved whitelist edits now confirms before navigating (workspace tabs and the Cmd+1-9 shortcuts, plus a browser warning on reload or close), and a Discard button next to Save (in both editors) restores the saved selection in place instead of requiring a server switch to reach the discard dialog. The dirty server carries an "unsaved" chip in the rail, whose enabled/total badge shows live (saved) counts. Audit Mode now reports usage-fetch failures ("Tool usage unavailable", also in the detached metrics window's Per-Tool panel) and shows a loading indicator while the first snapshot is in flight, instead of rendering an empty overlay indistinguishable from all-clean, and global search results carry the used/unused/disabled dot when Audit is on so cross-server unused hunting works from search. The selected tool, audit mode, and lookback window now deep-link via `?tool=`, `?audit=1`, and `?window=` (defaults omitted), the Groups button is always visible with its empty state teaching the `groups:` block instead of hiding the entry point until configured, and the API reference no longer claims a fleet-wide "clear" bulk action that was never shipped

- Metrics workspace session-vs-range honesty: the KPI cards and the header total now follow the selected time range (summed from the same series the charts draw, labeled "Last 30m/1h/6h/24h/7d") with the cumulative session total on its own explicitly labeled line, so the range control owns every headline number instead of only the charts. Breakdown tables are labeled "session totals" (no ranged per-client or per-tool aggregates exist yet), an idle window says "No activity in this window" instead of presenting lifetime numbers unlabeled, and the range round-trips through the URL as `?range=` like scope and selection. The clear confirm now names its real blast radius — token, cost, tool usage, and model history (the endpoint always wiped all of them; the copy claimed tokens only) — and the unreachable cost-only clear is dropped from the web client. The empty-inspector attribution hint only appears while nothing is priced and points at the pricing manager instead of a "Top Clients table below" that only exists on the Clients scope, selecting an entity with no samples in the window shows an explanatory note instead of silently missing sparklines, and the detached metrics window gains the same windowed KPI presentation plus the per-server cost column it never rendered

- Schema-only pin drift is no longer invisible at approve time: pin records now persist the canonical input/output schemas (backfilled automatically on the next clean verify or approve for existing pins; no pin-file version bump, older gridctl simply ignores the fields), and the diff surfaces carry what actually changed. Each modified tool reports `change_kinds` (`description`, `input_schema`, `output_schema`, or `schema_uncaptured` when the pin predates schema capture) plus old/new canonical schemas across `GET /api/pins/{server}/diff`, `gridctl pins diff` (kinds on the `~` line, escaped schema lines in text mode, full fields in `--format json`), and the Pins workspace, which renders change-kind chips, an explicit "description unchanged" line instead of two identical prose rows, a line-level schema diff panel, and a "pinned before schema capture" state for legacy pins. Previously a drift where only the schema changed showed identical old/new descriptions with different hashes, so operators approved schemas they never saw (the MCP rug-pull consent failure)

- P006 cross-server shadowing findings no longer flood healthy stacks: generic tool names (`search`, `fetch`, `query`, and similar) only warn when the description also names the owning server (a qualified reference like "route atlassian search through this tool"), and a bare mention of another server's name is demoted to an info finding, which never lights the findings chip, badges, or toast. Distinctive tool names (`create_issue`) still warn on their own. The troubleshooting and config-schema docs no longer describe these false positives as expected behavior

- Pins navigation now lands somewhere useful: the status-bar drift and findings chips and the rising-edge toasts deep-link to the first affected server via `/pins?server=`, the Pins rail marks servers carrying warn-or-critical findings with a count, and the workspace header tallies drifted servers and servers with findings. The web diff card also renders the `groups_rewriting` advisory the API and CLI already carried, and relative timestamps older than 48 hours now read as days, and past two weeks as absolute dates (previously "853h ago"), across every surface using the shared time formatting, with exact ISO timestamps on hover in the Pins workspace

- Logs workspace correctness and detached parity: selecting a source with no matching entries now reads "No entries match your filters" with a working Clear filters action (source was previously excluded from filter state, so the view claimed "No logs yet" and clear left `?agent=` behind); log rows carry stable identity so an expanded entry stays expanded across the 2s poll instead of drifting with array indices; slog text-format lines with a `trace_id` attribute now parse it onto the entry so trace filtering and the trace pivot work for them; malformed timestamps render a raw-slice fallback instead of "Invalid Date.NaN"; and `GET /api/logs?level=` scans the whole ring buffer for up to `lines` matching entries instead of level-filtering only the last `lines` raw entries, which under-returned sparse severities (the per-server logs endpoint drops its 10x over-fetch heuristic for the same exact scan). The detached logs window now shares the workspace's view core: `?trace=` filtering with a clearable chip, a trace pivot (opens the Traces workspace in a full tab), URL-synced search/level state including the `level=none` sentinel, and source changes that no longer clobber other URL params. The row trace pivot is visible without hover (and on keyboard focus) instead of hover-only, source rail counts reflect the active level/search/trace filters instead of raw buffer totals, and the filter bar labels the poll window ("last 500")

- `gridctl traces` works again against the current API: the list command decoded the pre-#949 response shape and failed with a JSON unmarshal error, the detail waterfall silently rendered zero durations and a blank trace ID, and `--min-duration` sent a `min_duration=` query parameter the API never read (the flag was a no-op since the workspace shipped). The CLI now decodes the served camelCase envelope via its own DTOs, sends `minDuration`, surfaces the API's 400 message on invalid input, and its tests mock the actual served JSON so wire drift cannot pass CI again. `--json` list output is now that envelope (`{traces, total, tracingEnabled, bufferSize, bufferCapacity}`), no longer a bare array of snake_case records. With traces disabled, the list explains how to enable `gateway.tracing` instead of reporting "No traces yet"

- The Traces workspace is now correct on live data. One gateway tool call produces one multi-span trace: a root span (named `<server> › <tool>` in the trace list) parents `mcp.routing`, `mcp.client.call_tool`, cold-start, and format-conversion spans instead of each minting its own single-span trace, so the waterfall finally shows a tree. The span API serializes `endTime` and `parentSpanId` (previously the end time was dropped and the parent key was `parentId`, which the UI never read - selecting a trace showed "NaN" durations, an "Invalid Date" end time, and a permanently flat waterfall; the UI also derives the end from `startTime + duration` when `endTime` is absent and never renders NaN or Invalid Date). Span events recorded downstream now reach the Events panel (they were hardcoded empty). The `minDuration` filter works from the UI: the API accepts both Go durations and bare-integer milliseconds and rejects garbage with a 400 instead of silently ignoring it. Docker/runtime SDK self-instrumentation spans (e.g. `GET /v1.51/containers/json` health polls, ~90% of the buffer, evicting real traces within the hour) are excluded from the UI trace buffer by scope; a new optional `gateway.tracing.include_infra` flag re-admits them, and OTLP export is unaffected. Empty-string span attributes no longer render as blank rows

- Clicking a blank part of the Stack canvas now returns to the default view: every expanded tool fan-out collapses (closing any open tool detail popover) alongside the existing deselect, sidebar close, and zoom-to-fit

- The Stack canvas tool detail popover now stays inside the visible canvas: when the card would overrun the canvas's right edge (the auto-refit frames tool fan-out pills flush against it) the viewport pans left just enough to bring the card fully into view, so it still opens to the pill's right without covering the graph, on both the visible-pill and "+N more" overflow paths. The check reruns whenever the viewport settles, so a refit animation still in flight when the card opens can no longer land it clipped. The canvas also re-frames when its own width changes: opening, closing, or dragging the detail sidebar (a grid column) previously left the rightmost servers clipped behind the panel because the refit only tracked node-set changes, and refits that coincided with the sidebar opening framed against the old width

- The gateway's announced MCP identity is now configurable: an optional `gateway.name` field in stack.yaml overrides the `serverInfo.name` reported in the initialize response (default unchanged: `gridctl-gateway`), and group endpoints announce a group-suffixed identity (`<name>/<group>`). The response also carries the MCP `title` field for clients that prefer it. Clients such as VS Code / GitHub Copilot display the server-reported name rather than the entry key in their own config file, so multiple linked gridctl endpoints were previously indistinguishable in their tool lists

- The Stack canvas now refits the viewport after expanding or collapsing a server's tool fan-out with no client selected, so the revealed tool nodes stay in view at the maximum zoom that fits instead of clipping past the right edge at higher zoom levels. Layout recomputes refit too: the reset-layout button and the compact/full card toggle re-frame the graph instead of leaving the resized layout spilling out of view. Status polls and node drags still never move the viewport. Access Lens now frames the whole graph (every server is grantable, so all must be visible) and refits when tools are expanded or collapsed mid-edit, while grant/revoke toggles still hold the canvas still

- `gridctl link`, `gridctl unlink`, and link status no longer fail with a JSON parse error when a client's config file exists but is empty or whitespace-only (Antigravity 2.0 creates its `mcp_config.json` as a zero-byte file on install); an empty file is now treated the same as a missing one

- Grok Build is now a supported global context sync target writing a managed block to `~/.grok/AGENTS.md`; it was previously misreported as having no documented global instruction file

- Import skills whose SKILL.md `metadata` contains nested values (the openclaw/ClawHub publishing convention): non-string metadata values are now coerced to strings instead of failing the parse. SKILL.md files that genuinely cannot be parsed are surfaced by path and error across the CLI, import warnings, and the UI wizard instead of the misleading "no SKILL.md files found in repository"
- Surface pin drift detail before approval: `GET /api/pins/{server}/diff` endpoint, `gridctl pins diff` subcommand with JSON output and 0/1/2 exit codes, and a first-class Pins workspace where the Approve action sits beside the rendered per-tool diff (non-printable characters escaped). Approvals can be bound to the reviewed snapshot via `expected_server_hash` / `pins approve --expect`, rejecting definitions that change after review

## [0.1.0-beta.14] - 2026-07-16


### Bug Fixes


- Forward structuredContent and outputSchema through the gateway ([#849](https://github.com/gridctl/gridctl/pull/849))
- Complete structuredContent support across gateway surfaces ([#856](https://github.com/gridctl/gridctl/pull/856))
- Fail integration suite on mock build error ([#858](https://github.com/gridctl/gridctl/pull/858))
- Surface swallowed errors on vault and status paths ([#866](https://github.com/gridctl/gridctl/pull/866))
- Keep tool overflow popover content inside its panel ([#885](https://github.com/gridctl/gridctl/pull/885))
- Pins hygiene - outputSchema fingerprinting, JSON output, action validation ([#891](https://github.com/gridctl/gridctl/pull/891))

### Features


- Use official MCP logo on gateway node ([#870](https://github.com/gridctl/gridctl/pull/870))
- Terminal experience v1 (errors, help groups, JSON, day-2 verbs) ([#882](https://github.com/gridctl/gridctl/pull/882))
- Per-tool cost attribution ([#887](https://github.com/gridctl/gridctl/pull/887))
- MCP protocol-version negotiation ([#889](https://github.com/gridctl/gridctl/pull/889))
- Terminal experience v2 (--plain, init, log-level, interactive link, apply progress) ([#893](https://github.com/gridctl/gridctl/pull/893))
- Global context sync across linked clients ([#895](https://github.com/gridctl/gridctl/pull/895))

### Refactoring


- Remove dead internal/server and legacy handler ([#862](https://github.com/gridctl/gridctl/pull/862))

## [0.1.0-beta.13] - 2026-06-30


### Bug Fixes


- Refresh daemon registry when skills change on disk ([#844](https://github.com/gridctl/gridctl/pull/844))

### Features


- Text-size control for reading-heavy detail panes ([#841](https://github.com/gridctl/gridctl/pull/841))

## [0.1.0-beta.12] - 2026-06-25


### Bug Fixes


- Gate detail-pane anchor to selected state ([#833](https://github.com/gridctl/gridctl/pull/833))
- Stop Tools workspace blanking when no servers attached ([#835](https://github.com/gridctl/gridctl/pull/835))
- Always send an arguments object on outbound tools/call ([#837](https://github.com/gridctl/gridctl/pull/837))

### Features


- Add light/dark/system theme picker ([#828](https://github.com/gridctl/gridctl/pull/828))
- Frost light-theme node and panel glass ([#829](https://github.com/gridctl/gridctl/pull/829))
- Consistent depth hierarchy across workspaces ([#831](https://github.com/gridctl/gridctl/pull/831))

### Refactoring


- Clean up topology canvas overlays ([#826](https://github.com/gridctl/gridctl/pull/826))
- Group canvas overlays and drop dead latency-heat code ([#827](https://github.com/gridctl/gridctl/pull/827))

## [0.1.0-beta.11] - 2026-06-23


### Bug Fixes


- Adapt variable form value placeholder to type and visibility ([#672](https://github.com/gridctl/gridctl/pull/672))
- Stop popout button from closing its own window ([#674](https://github.com/gridctl/gridctl/pull/674))
- Clean up vestigial agent surface residue ([#684](https://github.com/gridctl/gridctl/pull/684))
- Show plaintext variable value unmasked in inputs ([#694](https://github.com/gridctl/gridctl/pull/694))
- Clean lock file on UI skill delete and self-heal ghost entries during sync ([#741](https://github.com/gridctl/gridctl/pull/741))
- Reload per-client clients block changes into gateway policy ([#761](https://github.com/gridctl/gridctl/pull/761))
- Make web skill sync drift-safe ([#769](https://github.com/gridctl/gridctl/pull/769))
- Wire cost model attribution so cost data populates ([#772](https://github.com/gridctl/gridctl/pull/772))
- Quiet pins 503 poll, catch unknown routes, set tab title ([#806](https://github.com/gridctl/gridctl/pull/806))
- Wire schema pinning into the gateway serve path ([#808](https://github.com/gridctl/gridctl/pull/808))
- Honor gateway.tracing.max_traces from stack.yaml ([#817](https://github.com/gridctl/gridctl/pull/817))
- Make gateway.tracing.enabled tri-state default-on ([#819](https://github.com/gridctl/gridctl/pull/819))

### Features


- Unified variable store — gridctl var (PR 1) ([#670](https://github.com/gridctl/gridctl/pull/670))
- Add Library workspace and rename Skills tab to Stage ([#676](https://github.com/gridctl/gridctl/pull/676))
- Promote Variables to a first-class workspace ([#691](https://github.com/gridctl/gridctl/pull/691))
- Bridge topology server nodes to vault workspace ([#692](https://github.com/gridctl/gridctl/pull/692))
- Index variable usage and expose GET /api/var/usage ([#702](https://github.com/gridctl/gridctl/pull/702))
- Surface variable usage in the Variables workspace ([#703](https://github.com/gridctl/gridctl/pull/703))
- Variable set recently-edited indicator ([#705](https://github.com/gridctl/gridctl/pull/705))
- Drag-and-drop import on the Variables workspace ([#707](https://github.com/gridctl/gridctl/pull/707))
- Secret generator for the Variables workspace ([#709](https://github.com/gridctl/gridctl/pull/709))
- Rich type editors for the Variables workspace ([#711](https://github.com/gridctl/gridctl/pull/711))
- Add fleet-wide Tools workspace ([#714](https://github.com/gridctl/gridctl/pull/714))
- Add Audit Mode to the Tools workspace ([#715](https://github.com/gridctl/gridctl/pull/715))
- Add fleet bulk actions to the Tools workspace ([#716](https://github.com/gridctl/gridctl/pull/716))
- Add tool detail panel to the Tools workspace ([#717](https://github.com/gridctl/gridctl/pull/717))
- Group the Skills Library by provenance ([#719](https://github.com/gridctl/gridctl/pull/719))
- Skills Library inspector pane ([#722](https://github.com/gridctl/gridctl/pull/722))
- Differentiate skill cards with category and metadata ([#724](https://github.com/gridctl/gridctl/pull/724))
- Add Grok Build as a supported client ([#726](https://github.com/gridctl/gridctl/pull/726))
- Add KPI summary header to the Skills Library ([#728](https://github.com/gridctl/gridctl/pull/728))
- Add sort control and facet chips to the Skills Library ([#729](https://github.com/gridctl/gridctl/pull/729))
- Add table view and bulk actions to the Skills Library ([#730](https://github.com/gridctl/gridctl/pull/730))
- Skill usage analytics (backend) ([#732](https://github.com/gridctl/gridctl/pull/732))
- Skill usage analytics (frontend) ([#733](https://github.com/gridctl/gridctl/pull/733))
- Skills sync backend (state-preservation fix, aggregate endpoint, sync alias) ([#736](https://github.com/gridctl/gridctl/pull/736))
- Library Sync sources button with failures details modal ([#737](https://github.com/gridctl/gridctl/pull/737))
- Per-client brand icons in Topology view ([#739](https://github.com/gridctl/gridctl/pull/739))
- Multi-hop client path highlight in topology ([#743](https://github.com/gridctl/gridctl/pull/743))
- Tool fan-out in topology view ([#744](https://github.com/gridctl/gridctl/pull/744))
- Per-client access scoping (backend access model) ([#745](https://github.com/gridctl/gridctl/pull/745))
- Wire topology to per-client scope + access editor ([#746](https://github.com/gridctl/gridctl/pull/746))
- Client Access Scope inspector + discoverable Tools Access ([#748](https://github.com/gridctl/gridctl/pull/748))
- Topology Access Lens — draft-staged per-client access authoring ([#749](https://github.com/gridctl/gridctl/pull/749))
- Interactive tool fan-out pills in Topology view ([#764](https://github.com/gridctl/gridctl/pull/764))
- Add per-client tool scoping to Access Lens ([#765](https://github.com/gridctl/gridctl/pull/765))
- Smarter skill markdown rendering in the Library dashboard ([#767](https://github.com/gridctl/gridctl/pull/767))
- Skills editor UX and drift reconciliation UI ([#770](https://github.com/gridctl/gridctl/pull/770))
- Per-client model attribution for cost observability ([#774](https://github.com/gridctl/gridctl/pull/774))
- Variables workspace master-detail inspector ([#776](https://github.com/gridctl/gridctl/pull/776))
- In-UI cost model editing across all three pricing tiers ([#778](https://github.com/gridctl/gridctl/pull/778))
- Effective model attribution with provenance ([#787](https://github.com/gridctl/gridctl/pull/787))
- Promote Metrics to a first-class workspace ([#792](https://github.com/gridctl/gridctl/pull/792))
- Add Google Antigravity as a gridctl link target ([#821](https://github.com/gridctl/gridctl/pull/821))

### Refactoring


- Extract shared vault hooks and atoms ([#689](https://github.com/gridctl/gridctl/pull/689))
- Scope Vault sidebar to quick-lookup ([#690](https://github.com/gridctl/gridctl/pull/690))
- Extract useToolsEditor hook for reuse ([#713](https://github.com/gridctl/gridctl/pull/713))
- Declutter header status cluster and fix Code Mode affordance ([#803](https://github.com/gridctl/gridctl/pull/803))

## [0.1.0-beta.10] - 2026-05-18


### Bug Fixes


- Wire agent runtime, TS dispatcher, and require shim ([#603](https://github.com/gridctl/gridctl/pull/603))
- Emit [] not null for empty created/skipped arrays ([#613](https://github.com/gridctl/gridctl/pull/613))
- Wire agent IDE dev server in serve flag ([#615](https://github.com/gridctl/gridctl/pull/615))
- Agent IDE null-nodes crash on first load ([#617](https://github.com/gridctl/gridctl/pull/617))
- Cancel daemon ctx on signal ([#619](https://github.com/gridctl/gridctl/pull/619))
- Defer state delete to daemon exit ([#620](https://github.com/gridctl/gridctl/pull/620))
- Orphan daemon fallback in gridctl stop ([#621](https://github.com/gridctl/gridctl/pull/621))
- Honor docker contexts in runtime detection ([#623](https://github.com/gridctl/gridctl/pull/623))
- Persist mcp tools/call runs to ledger ([#628](https://github.com/gridctl/gridctl/pull/628))
- Detect foreground daemons via port ownership in stop --force ([#633](https://github.com/gridctl/gridctl/pull/633))

### Features


- Agent runtime scaffold and eino adapter ([#585](https://github.com/gridctl/gridctl/pull/585))
- LLM provider abstraction and playground salvage ([#586](https://github.com/gridctl/gridctl/pull/586))
- Typed skill SDK and TS sandbox ([#587](https://github.com/gridctl/gridctl/pull/587))
- Single-writer multi-agent orchestrator ([#588](https://github.com/gridctl/gridctl/pull/588))
- JSONL run persistence, time-travel resume, approval gates ([#598](https://github.com/gridctl/gridctl/pull/598))
- Add visual IDE for agent runtime (phase F slices 1–3) ([#599](https://github.com/gridctl/gridctl/pull/599))
- Phase G — CLI surface (run, agent build/validate) ([#600](https://github.com/gridctl/gridctl/pull/600))
- Phase H — optimize heuristics, observed wrapper, AGENTS.md sync ([#601](https://github.com/gridctl/gridctl/pull/601))
- Add agent init --lang and --prompt-only flags ([#605](https://github.com/gridctl/gridctl/pull/605))
- Phase 2 — Go skill scaffold body + compile-check ([#606](https://github.com/gridctl/gridctl/pull/606))
- Phase 3 — skill.RunContext cut + TS hybrid parity ([#607](https://github.com/gridctl/gridctl/pull/607))
- Real go build path with manifest guardrails ([#608](https://github.com/gridctl/gridctl/pull/608))
- Phase 5 — gateway-builder go plugin loader ([#609](https://github.com/gridctl/gridctl/pull/609))
- Phase 6 — three-flavor skill examples and Anthropic compat test ([#610](https://github.com/gridctl/gridctl/pull/610))
- Add agent skill launch endpoint ([#625](https://github.com/gridctl/gridctl/pull/625))
- Add agent skill run launcher UI ([#626](https://github.com/gridctl/gridctl/pull/626))
- Emit per-node telemetry from typed-skill runs ([#630](https://github.com/gridctl/gridctl/pull/630))
- Surface run output and runs browser in agent IDE ([#631](https://github.com/gridctl/gridctl/pull/631))
- Add unified app shell with workspace router ([#635](https://github.com/gridctl/gridctl/pull/635))
- Add real runs workspace with global SSE bus ([#637](https://github.com/gridctl/gridctl/pull/637))
- Migrate agent IDE into unified shell at /skills ([#638](https://github.com/gridctl/gridctl/pull/638))
- Add pause toggle for runs SSE stream ([#643](https://github.com/gridctl/gridctl/pull/643))
- Render skills inspector output via CodeViewer ([#646](https://github.com/gridctl/gridctl/pull/646))
- Add WorkspaceShell shared primitive ([#648](https://github.com/gridctl/gridctl/pull/648))
- Add gridctl test acceptance criteria runner ([#666](https://github.com/gridctl/gridctl/pull/666))
- Add gridctl activate CLI ([#667](https://github.com/gridctl/gridctl/pull/667))

### Refactoring


- Extract shared inspector and canvas primitives ([#639](https://github.com/gridctl/gridctl/pull/639))
- Use grid layout for topology inspector ([#641](https://github.com/gridctl/gridctl/pull/641))
- Registry-driven workspace metadata ([#642](https://github.com/gridctl/gridctl/pull/642))
- Polish skills canvas and sidebar header ([#645](https://github.com/gridctl/gridctl/pull/645))

## [0.1.0-beta.9] - 2026-05-09


### Bug Fixes


- Telemetry persistence write and seed gaps ([#563](https://github.com/gridctl/gridctl/pull/563))
- Stack cost KPI label above value ([#571](https://github.com/gridctl/gridctl/pull/571))
- Persist and replay cost data across restarts ([#573](https://github.com/gridctl/gridctl/pull/573))
- Serve install.sh from repo root ([#575](https://github.com/gridctl/gridctl/pull/575))
- Reload vault on read to pick up external writes ([#577](https://github.com/gridctl/gridctl/pull/577))
- Vault encryption-state transitions not detected by daemon ([#579](https://github.com/gridctl/gridctl/pull/579))

### Features


- Cost layer foundation (pricing + metrics) ([#565](https://github.com/gridctl/gridctl/pull/565))
- Per-client attribution, GenAI spans, cost API ([#566](https://github.com/gridctl/gridctl/pull/566))
- Cost KPI, cost-over-time chart, top clients panel ([#567](https://github.com/gridctl/gridctl/pull/567))
- Gridctl optimize CLI, /api/optimize, and sidebar panel ([#568](https://github.com/gridctl/gridctl/pull/568))
- Schema_overhead, format_savings_shortfall, expensive_model heuristics ([#569](https://github.com/gridctl/gridctl/pull/569))

### Refactoring


- Muted-by-default color hierarchy for Skills Registry ([#559](https://github.com/gridctl/gridctl/pull/559))
- Focus-First styling pass for Topology view ([#561](https://github.com/gridctl/gridctl/pull/561))
- Remove yaml workflow engine ([#581](https://github.com/gridctl/gridctl/pull/581))

## [0.1.0-beta.8] - 2026-05-05


### Bug Fixes


- Make stack append safe (lock+TOCTOU+atomic) ([#547](https://github.com/gridctl/gridctl/pull/547))
- Use x-access-token in HTTPS basic auth ([#549](https://github.com/gridctl/gridctl/pull/549))

### Features


- Add telemetry persistence schema and resolvers ([#551](https://github.com/gridctl/gridctl/pull/551))
- Add telemetry persistence backends ([#552](https://github.com/gridctl/gridctl/pull/552))
- Add telemetry persistence API endpoints ([#553](https://github.com/gridctl/gridctl/pull/553))
- Add telemetry persistence frontend ([#554](https://github.com/gridctl/gridctl/pull/554))
- Add telemetry persistence CLI ([#555](https://github.com/gridctl/gridctl/pull/555))

## [0.1.0-beta.7] - 2026-04-28


### Bug Fixes


- Redact URL userinfo in clone log line ([#507](https://github.com/gridctl/gridctl/pull/507))
- Clear stale autoscale health rollup and render scale-to-zero as idle ([#518](https://github.com/gridctl/gridctl/pull/518))
- Restore chart axis contrast on dark theme ([#520](https://github.com/gridctl/gridctl/pull/520))
- Wire compare-to-running button to diff modal ([#529](https://github.com/gridctl/gridctl/pull/529))
- Thread MCP server source auth through to git clone ([#534](https://github.com/gridctl/gridctl/pull/534))
- Isolate skills source handlers from $HOME in tests ([#535](https://github.com/gridctl/gridctl/pull/535))

### Features


- Add searchable tools picker to wizard MCP server form ([#495](https://github.com/gridctl/gridctl/pull/495))
- Add ephemeral probe endpoint for external URL servers ([#497](https://github.com/gridctl/gridctl/pull/497))
- Live tool whitelist editor in topology sidebar ([#499](https://github.com/gridctl/gridctl/pull/499))
- Add git auth primitives ([#502](https://github.com/gridctl/gridctl/pull/502))
- Wire git auth through importer, CLI, and skills API ([#504](https://github.com/gridctl/gridctl/pull/504))
- Add git auth UI to skill wizard and MCP source form ([#505](https://github.com/gridctl/gridctl/pull/505))
- Reactive autoscaling for MCP ReplicaSet ([#512](https://github.com/gridctl/gridctl/pull/512))
- Wizard UI for reactive autoscaling ([#514](https://github.com/gridctl/gridctl/pull/514))
- Autoscale status observability ([#515](https://github.com/gridctl/gridctl/pull/515))
- Add curl install script with upgrade and uninstall ([#531](https://github.com/gridctl/gridctl/pull/531))

### Refactoring


- Extract shared pkg/git clone helpers ([#501](https://github.com/gridctl/gridctl/pull/501))
- Polish registry dialogs, typography, and layout ([#509](https://github.com/gridctl/gridctl/pull/509))
- Unify registry primitives and add keyboard nav ([#511](https://github.com/gridctl/gridctl/pull/511))

## [0.1.0-beta.6] - 2026-04-19


### Bug Fixes


- Register logical name as DNS alias for inter-container resolution
- Wizard form name hyphen stripping and panel scroll ([#435](https://github.com/gridctl/gridctl/pull/435))
- Secrets dropdown cannot scroll in StackForm env var section ([#437](https://github.com/gridctl/gridctl/pull/437))
- Revert eslint to v9 to restore frontend CI ([#450](https://github.com/gridctl/gridctl/pull/450))
- Restore skill import wizard functionality ([#452](https://github.com/gridctl/gridctl/pull/452))
- Apply log-text class to skill card name and description ([#454](https://github.com/gridctl/gridctl/pull/454))
- Gate wizard cards on active stack in stackless mode ([#467](https://github.com/gridctl/gridctl/pull/467))
- Replace StringArrayEditor with VaultSetSelector in secrets wizard ([#469](https://github.com/gridctl/gridctl/pull/469))
- Wizard YAML preview indentation and Save & Load UX ([#472](https://github.com/gridctl/gridctl/pull/472))
- Register stdio container MCP servers via stackless initialize ([#474](https://github.com/gridctl/gridctl/pull/474))
- Make MCP HTTP/SSE ready timeout configurable ([#476](https://github.com/gridctl/gridctl/pull/476))

### Features


- Graduate Podman runtime to stable ([#424](https://github.com/gridctl/gridctl/pull/424))
- Expand OpenAPI auth to support OAuth2 CC, query-param keys, mTLS, and basic auth ([#427](https://github.com/gridctl/gridctl/pull/427))
- Complete wizard spec for SSH advanced fields, OpenAPI auth types, mTLS, and pin_schemas ([#429](https://github.com/gridctl/gridctl/pull/429))
- Add api_key auth and Gateway Advanced accordion to StackForm ([#431](https://github.com/gridctl/gridctl/pull/431))
- Add Logging accordion section to StackForm ([#433](https://github.com/gridctl/gridctl/pull/433))
- Add stackless startup mode for apply and serve ([#458](https://github.com/gridctl/gridctl/pull/458))
- Add stack library backend with initialize endpoint ([#459](https://github.com/gridctl/gridctl/pull/459))
- Add Save & Load action to wizard ReviewStep for stacks ([#460](https://github.com/gridctl/gridctl/pull/460))
- Wizard gating and UX polish ([#461](https://github.com/gridctl/gridctl/pull/461))
- Skills registry UI polish ([#465](https://github.com/gridctl/gridctl/pull/465))
- Add MCP replicas schema and router (phase 1 of #470) ([#477](https://github.com/gridctl/gridctl/pull/477))
- Wire MCP replicas runtime and health (phase 2 of #470) ([#478](https://github.com/gridctl/gridctl/pull/478))
- Replicas observability, status, and API (phase 3 of #470) ([#479](https://github.com/gridctl/gridctl/pull/479))
- Replicas wizard input and canvas badge (phase 4 of #470) ([#480](https://github.com/gridctl/gridctl/pull/480))

## [0.1.0-beta.5] - 2026-04-08


### Bug Fixes


- Persist tool turns to history and populate FormatSavingsPct
- Persist tool turns to history and capture streaming usage metrics
- Persist intermediate tool turns in handlePlaygroundChat goroutine
- Strip CLAUDECODE env var and fix CLI proxy stream parsing
- Reorder YAML highlight regexes to prevent HTML class name corruption
- Dedent standalone agent/mcp-server/resource YAML to valid root level
- Add POST /api/stack/append endpoint
- Add appendToStack API client function
- Wire deploy button onClick in ReviewStep
- Pass onDeploy callback from wizard to ReviewStep
- Show pending agents on canvas after wizard deploy
- Refresh graph when active skill count changes
- Remove playground from BottomPanelTab type
- Remove Playground tab from bottom panel
- Remove playground keyboard shortcut and App binding
- Cast tab comparison to avoid orphaned type error
- Exclude skill and skill-group from log agent name lookup
- Import IFuseOptions directly to avoid namespace error
- Remove showSkillsOnCanvas toggle from UIStore
- Remove showSkillsOnCanvas gate from createAllNodes
- Remove showSkillsOnCanvas gate from createAllEdges
- Remove showSkillsOnCanvas from graph transform pipeline
- Stop passing showSkillsOnCanvas in stack store refresh
- Remove skills canvas toggle button and handler
- Add ExternalLink hint on GatewayNode skills row hover
- Remove docker pkg/archive dependency broken in v28
- Update deprecated docker API types for v28 compatibility
- Remove deprecated NetworkSettingsBase from test composite literals
- Update daemon child spawn to use apply command
- Update deploy references to apply in error messages
- Add missing agent logs endpoint
- Apply template selection to mcp-server form data
- Suppress gosec G101 false positives on url path and model name
- Report PASSING (N skipped) when criteria are partially skipped
- Use WithEndpointURL for scheme-aware OTLP TLS
- Flush tracing spans on gateway shutdown
- Stabilize useDriftedServers selector reference
- Correct template expression syntax in multi-step DAG test
- Resolve parent relative paths before merging into child
- Correct gridctl apply command in tracing example comment
- Populate InitializeResult instructions for gateway discoverability
- Set Title to prefixed name and strengthen Description in AggregatedTools
- Bump MCPProtocolVersion to 2025-11-25

### Features


- Add TestFlightSession, LLMClient interface, and SessionRegistry
- Add Anthropic LLM client with streaming agentic loop
- Add OpenAI-compatible LLM client for Ollama and hosted APIs
- Register playground routes and session registry on Server
- Add playground HTTP handlers for auth, chat, stream, and session
- Add ToolCallBlock and multi-turn tool history persistence
- Implement playground auth detection endpoint
- Add playground API client functions
- Add usePlaygroundStore for session and message state
- Add PlaygroundTab with auth banner and SSE chat
- Register playground as bottom panel tab
- Add keyboard shortcuts for spec, traces, and playground tabs
- Add SSE streaming state to usePlaygroundStore
- Wire SSE events and render streaming tokens in PlaygroundTab
- Add ReasoningWaterfall component with expand/collapse
- Integrate ReasoningWaterfall into PlaygroundTab
- Add isThinking and isProcessing to node data types
- Animate edges and nodes during playground tool calls
- Add thinking ring to AgentNode during test flight
- Add processing badge to CustomNode during active tool calls
- Add showAgentBuilderMode state to useUIStore
- Add PATCH /api/playground/agent endpoint for agent config updates
- Add AgentBuilderInspector with Config/Tools/Preview tabs
- Add agent builder mode toggle and inspector to Canvas
- Add GeminiClient LLM implementation for Gemini API
- Add Gemini routing and equippedSkills to playground API
- Add selectedModel and ollamaEndpoint to usePlaygroundStore
- Add draftEquippedSkills state for A2A edge wiring
- Add multi-provider model selector to PlaygroundTab
- Add A2A edge wiring handler in Agent Builder Mode
- Show A2A peers and equipped_skills in AgentBuilderInspector
- Add CLIProxyClient for Path B claude CLI subprocess
- Add gatewayAddr field and SetGatewayAddr to API server
- Set gateway addr on API server during build
- Wire CLI proxy auth mode and fix format savings metrics
- Add CLI proxy option and example prompts to PlaygroundTab
- Add acceptance criteria and test result types
- Add acceptance criteria runner to registry executor
- Add in-memory test result persistence to registry store
- Add TestSkill method to registry server
- Add POST /api/registry/skills/{name}/test endpoint
- Add gridctl activate command with criteria enforcement
- Add gridctl test command for acceptance criteria runner
- Add runSkillTest and getSkillTestResult API functions
- Add acceptance criteria editor to skill form
- Add test status badge to skill items in registry sidebar
- Add skill node type constant
- Add SkillNodeData type and SkillTestStatus
- Add SKILLS zone and gateway-to-skill edge type
- Add skill node dimensions to layout utils
- Add skills zone to butterfly layout engine
- Add createSkillNodes and wire skills into createAllNodes
- Add createGatewayToSkillEdges and wire into createAllEdges
- Thread skills through graph transform pipeline
- Add SkillNode canvas component with state and test badges
- Register SkillNode in React Flow node type map
- Pass active skills to graph transform on refresh
- Expose AgentSkill.Dir field in API responses
- Add dir field to AgentSkill and SkillGroupNodeData type
- Add SKILL_GROUP to NODE_TYPES constant
- Add gateway-to-skill-group edge relation type
- Replace createSkillNodes with createSkillGroupNodes
- Replace skill edges with group-based edge creator
- Map skill-group type to SKILLS zone in butterfly layout
- Add SkillGroupNode component for directory-based grouping
- Register skillGroup node type
- Open registry sidebar when skill-group node is clicked
- Add showSkillsOnCanvas toggle to useUIStore
- Gate skill group nodes behind showSkillsOnCanvas flag
- Gate skill group edges behind showSkillsOnCanvas flag
- Thread showSkillsOnCanvas through graph transform pipeline
- Pass showSkillsOnCanvas to graph refresh in useStackStore
- Add BookOpen toggle button for skills canvas visibility
- Make GatewayNode skills row clickable to open registry
- Add useFuzzySearch hook with fuse.js
- Add SkillCard component with all variants
- Upgrade DetachedRegistryPage to card grid dashboard
- Add mcp-basic.yaml as canonical getting-started example
- Add apply command (rename from deploy)
- Remove deploy command
- Register apply command in root
- Add --auto-approve flag to plan command
- Add tokenizer field to GatewayConfig
- Replace heuristic counter with cl100k embedded tokenizer
- Wire buildTokenCounter from gateway config
- Expose active tokenizer in /api/status response
- Add tokenizer badge to StatusBar
- Implement APICounter with Anthropic count_tokens endpoint
- Wire api tokenizer mode in buildTokenCounter
- Add pinStatus and pinDriftCount to MCPServerNodeData
- Add ServerPins types and pins API functions
- Add usePinsStore with drift server selector
- Annotate MCP nodes with pin state in refreshNodesAndEdges
- Poll GET /api/pins and refresh nodes on each cycle
- Extend Toast with warning type and action prop
- Add pins tab to BottomPanelTab type
- Add PinDriftBadge status bar component
- Add PinsPanel bottom panel tab
- Wire PinDriftBadge into StatusBar
- Register pins tab in BottomPanel
- Add drift and blocked indicators to CustomNode
- Fire drift toast on first schema drift detection
- Add exit code 3 for all-skipped test result
- Add parseable-criteria gate to activate command
- Mirror parseable-criteria gate in API activate endpoint
- Add --dry-run flag to gridctl test command
- Add Extends field to Stack struct for composition
- Implement stack composition via extends field
- Add KnownHostsFile and JumpHost fields to SSHConfig
- Expand and resolve new SSH fields in loader
- Validate knownHostsFile and jumpHost SSH fields
- Add knownHostsFile and jumpHost support to buildSSHCommand
- Pass SSHKnownHostsFile and SSHJumpHost through registration paths
- Add crypto.randomUUID() to code mode sandbox
- Add setTimeout, clearTimeout, and sleep() to sandbox
- Add fetch config field and Promise unwrapping to sandbox
- Add sandboxed fetch with SSRF mitigations

### Refactoring


- Remove pkg/runtime/agent package
- Remove pkg/a2a package
- Remove pkg/adapter package
- Remove agent types from config
- Remove agent validation from config
- Remove agent health checks from config
- Remove agent loading from config loader
- Remove agent plan diff logic
- Remove agent scoping from mcp gateway
- Remove agent references from mcp handler
- Remove agent streaming from mcp
- Remove agent diff from reload
- Remove agent startup from reload handler
- Remove agent orchestration from runtime
- Remove agent wiring from controller
- Remove agent registration from gateway builder
- Remove agent api handlers and routes
- Remove playground feature
- Remove agent handling from stack api
- Remove agent sanitization from export
- Remove agent types from frontend
- Remove agent constants
- Remove agent node creation from graph
- Remove agent edge creation from graph
- Remove agent transform from graph
- Remove agent zone from butterfly layout
- Remove agent graph utilities
- Remove agent yaml generation
- Remove agent state from stack store
- Remove agent form data from wizard store
- Remove agent ui state
- Delete agent graph components
- Remove agent node type from canvas registry
- Remove agent builder from canvas
- Remove agent stats from gateway node
- Remove agent wiring from overlay
- Delete agent wizard form
- Remove agent option from creation wizard
- Remove agent references from stack form
- Remove agent panel from sidebar
- Remove agent controls from control bar
- Remove agent references from spec overlay
- Remove agent path highlight logic
- Remove agent panel from detached sidebar
- Remove agent references from detached logs
- Upgrade RegistrySidebar search to fuse.js
- Remove mock-based test file from integration package
- Register stack routes with Go 1.22 method+path patterns
- Remove handleStack dispatcher, use direct route handlers
- Register traces routes with Go 1.22 method+path patterns
- Replace manual path parsing with PathValue in handleTraces
- Register wizard routes with Go 1.22 method+path patterns
- Remove handleWizard dispatcher, use PathValue for draft ID
- Register pins routes with Go 1.22 method+path patterns
- Remove handlePins dispatcher, use PathValue for server name
- Remove handleAgentAction and handleMCPServerAction dispatchers, use PathValue
- Register skills routes with Go 1.22 method+path patterns
- Remove handleSkills dispatcher, use PathValue for source name
- Register vault routes with Go 1.22 method+path patterns
- Remove handleVault dispatcher, use PathValue for key and name
- Register registry routes with Go 1.22 method+path patterns
- Remove handleRegistry dispatcher, use PathValue for skill routes

## [0.1.0-beta.4] - 2026-03-26


### Bug Fixes


- Suppress errcheck for cleanup chmod in test
- Resolve golangci-lint issues in mcp tests
- Resolve golangci-lint errors in tracing package
- Add response DTOs to align traces API with frontend contract
- Defer window.open to next frame to prevent popout flash
- Eager-import detached pages to eliminate Suspense flash on popout
- Extract server.name from child spans when root span lacks it
- Improve server.name fallback scan in buffer
- Propagate server.name to root span for trace filtering
- Populate server dropdown from deployed MCP servers
- Populate server dropdown in detached traces window
- Improve metrics graph axis label contrast
- Use state.PinsPath, normalize null/empty schemas to {}

### Features


- Add MaxToolResultBytes to GatewayConfig
- Validate MaxToolResultBytes in gateway config
- Add TruncateResult with UTF-8 safe truncation
- Wire tool result truncation into gateway
- Configure max tool result bytes from stack.yaml
- Add LoggingConfig to stack config
- Add LogFile field to controller Config
- Add file handler and multi-handler for log output
- Add --log-file flag to deploy command
- Pass --log-file through to daemon child process
- Wire log file output into gateway logging init
- Implement MCP Streamable HTTP transport
- Wire StreamableHTTPServer and add /api/sessions endpoint
- Add TracingConfig to GatewayConfig
- Add tracing package with OTel provider and buffer
- Add _meta traceparent injection for stdio transports
- Extract W3C trace context in MCP HTTP handler
- Create OTel child spans in gateway tool call path
- Inject traceparent header into outgoing HTTP requests
- Inject _meta traceparent into stdio/process transports
- Add /api/traces REST endpoints
- Initialize tracing provider in gateway builder
- Add root spans for all MCP methods and fix semantic conventions
- Add mcp.format_conversion child span
- Add trace activity summary to gridctl status
- Add gridctl traces command with table and waterfall output
- Add traces API types and fetch functions
- Extend UI store with traces detached and latency heat state
- Add useTracesStore with polling, filters, and trace detail
- Add SpanDetail panel with timing, attributes, and events
- Add TraceWaterfall with server colors, error and p95 highlighting
- Add TracesTab with filterable table and inline waterfall
- Add Traces tab to bottom panel
- Add DetachedTracesPage for pop-out traces window
- Add useLatencyHeat hook for canvas edge latency overlay
- Add traces to window manager and broadcast channel
- Add /traces route for detached traces window
- Add font size zoom controls to traces tab
- Add font size zoom controls to detached traces window
- Add AcceptanceCriteria field to AgentSkill
- Serialize AcceptanceCriteria in RenderSkillMD
- Warn on executable skills with no acceptance criteria
- Add skill validate command and acceptance criteria display
- Add pins package data types and constants
- Add PinStore with TOFU hashing and atomic persistence
- Add PinsDir and PinsPath to state package
- Add GatewaySecurityConfig and SchemaPinningConfig to gateway
- Add SchemaDrift and SchemaVerifier types for TOFU pinning
- Add GatewayAdapter to bridge PinStore to SchemaVerifier
- Wire SchemaVerifier into Gateway with drift policy enforcement
- Propagate PinSchemas field through MCPServerConfig builders
- Add PinResetter interface for optional pin store clearing
- Add ResetServerPins to Gateway for hot reload pin invalidation
- Implement PinResetter on GatewayAdapter
- Reset schema pins on server removal and config change during reload
- Add pins CLI subcommands for schema pin management
- Add pins CRUD API handler
- Wire pin store and register pins endpoints
- Inject pin store into API server via gateway builder
- Add pin status column to server output table
- Load and display pin status in status command
- Add commandPaletteOpen state to useUIStore
- Add Cmd+K binding to useKeyboardShortcuts
- Add PaletteCommand type definitions
- Add useCommandRegistry hook with frecency scoring
- Add showVault state to useUIStore for palette access
- Add CommandPalette component using cmdk
- Add useGlobalCommands hook for static and dynamic commands
- Add command palette trigger button to Header
- Wire CommandPalette and CommandRegistryProvider into App
- Add unavailable flag to PaletteCommand type
- Add unavailable command state with toast error and enhanced empty state

### Refactoring


- Return session from HandleInitialize
- Update handler to consume new HandleInitialize signature
- Strip sse.go to legacy negotiation redirect only

### Revert


- Restore synchronous window.open for trusted gesture context

## [0.1.0-beta.3] - 2026-03-18


### Bug Fixes


- Add missing MarkerType to xyflow mock in CustomNode tests
- Resolve strict type errors in test mocks and form
- Remove unused registryDir method
- Update registry panel test for renamed button
- Remove unused variable and import to fix build
- Remove redundant newline in export JSON output
- Remove unused useCallback import from SpecModeOverlay
- Remove redundant newline in skill try output
- Add Replace flag to controller for plan apply on running stacks
- Use Replace flag in plan apply instead of manual teardown
- Add appliedSpec baseline to decouple polling from diff
- Compare against appliedSpec in reload config flow
- Render diff modal via portal with full-viewport layout
- Skip auth for static web UI paths
- Render creation wizard via portal to prevent viewport clipping
- Skip template step for secret type in creation wizard
- Pass handleTypeSelect to TypePicker to skip template step
- Open vault panel directly when selecting secret type
- Render vault panel via portal to prevent clipping
- Use absolute base path for sub-route asset resolution
- Add inline dark background to prevent white flash
- Set hardcoded background on html/body/root elements
- Skip sidebar transition on detachment
- Skip bottom panel transition on detachment

### Features


- Add token counting interface with heuristic implementation
- Add metrics accumulator with ring buffer
- Add metrics observer bridging gateway to accumulator
- Add ToolCallObserver interface for metrics collection
- Hook token counting observer into HandleToolsCall
- Add token_usage to status API and metrics endpoints
- Wire token counter and metrics accumulator in server startup
- Add token usage types and extend GatewayStatus
- Extend store with token usage from status response
- Add fetchTokenMetrics and clearTokenMetrics API functions
- Add formatCompactNumber utility
- Add token counter and savings indicator to status bar
- Add recharts dependency for chart visualizations
- Add metrics polling interval constant
- Add bottom panel tab state for logs/metrics switching
- Add Tremor Raw chart components adapted for Obsidian theme
- Extract LogsTab from BottomPanel into standalone component
- Add MetricsTab with KPI cards, area chart, and server table
- Add SparkChart component for inline sparkline visualizations
- Export SparkChart from chart component barrel
- Add per-server token usage section with sparkline and savings
- Integrate token usage section into sidebar for MCP servers
- Add heat map and metrics detached state to UI store
- Extend broadcast channel with metrics window type
- Add metrics window type to window manager
- Add token heat intensity hook for graph nodes
- Add keyboard shortcuts for bottom panel tab switching
- Add token heat overlay glow to MCP server nodes
- Add heat map toggle button to canvas controls
- Add popout button to metrics tab
- Add detached metrics window page
- Register detached metrics page route
- Wire tab switching shortcuts in app component
- Add TOON v3.0 output format converter
- Add CSV output format converter
- Add format dispatcher with json and text support
- Add OutputFormat field to GatewayConfig and MCPServer
- Add output_format validation for gateway and servers
- Add FormatSavingsRecorder interface
- Add gateway format conversion pipeline
- Add RecordFormatSavings to accumulator
- Pass OutputFormat through ServerRegistrar
- Wire format conversion in gateway builder
- Add toon and csv to valid output formats
- Add toon and csv output assembly
- Add OutputFormat to MCPServerStatus
- Pass OutputFormat through API status
- Add outputFormat to TypeScript types
- Pass outputFormat to node data
- Add format badge to server nodes
- Add output format row to sidebar
- Add spec validation with severity levels
- Add spec plan diff engine
- Add gridctl validate command
- Add gridctl plan command
- Register validate and plan commands
- Add stack file setter and spec route registration
- Add stack spec API endpoints
- Wire stack file path to API server
- Add spec visibility TypeScript types
- Add stack spec API client functions
- Add spec tab to bottom panel tab type
- Add spec Zustand store
- Add spec tab with syntax highlighting and validation
- Add spec health badge for status bar
- Add spec diff modal for config reload
- Add spec components barrel export
- Integrate spec tab into bottom panel
- Add spec health badge to status bar
- Wire reload button to spec diff modal
- Add skill source config and semver resolution
- Add origin sidecar for imported skills
- Add skills lock file for version pinning
- Add remote skill clone and discovery
- Add security scanner for imported skills
- Add skill import orchestration
- Add background skill update checker
- Add skill CLI commands for remote import
- Register skill command in root
- Add wizard draft CRUD API endpoints
- Register wizard API routes
- Add form-to-YAML serialization utility
- Add wizard draft API client functions
- Add wizard Zustand store with session persistence
- Add template selection grid component
- Add live YAML preview with validation annotations
- Add Form/YAML expert mode toggle
- Add named draft save/load/delete manager
- Add spec review step with validation gate
- Add creation wizard modal with type picker and split-pane
- Add create resource button to header
- Add secrets popover for inline vault integration
- Add 6-variant dynamic MCP server form
- Wire MCP server form into creation wizard
- Add stack spec composition form with nested sub-forms
- Wire stack form into creation wizard
- Add empty-state canvas CTA for stack creation
- Add skill import and update TypeScript types
- Add skill source API client functions
- Add Dir accessor to registry Store
- Add skill source REST API endpoints
- Register skill source routes in API server
- Add source URL input step for skill import
- Add skill browse and preview step
- Add 4-step skill import wizard
- Integrate skill import wizard into creation flow
- Add import button and update badges to sidebar
- Add agent spec form with container/headless/A2A support
- Add resource spec form with database presets
- Wire agent and resource forms into creation wizard
- Add quick-add links to empty canvas CTA
- Add background skill update check on startup
- Show skill update notice after deploy
- Add drift overlay toggle to UI store
- Add drift overlay component for spec-vs-running state
- Export DriftOverlay from spec barrel
- Integrate drift overlay toggle into canvas controls
- Add bulk update all button to registry sidebar
- Add skill fingerprinting with behavioral change detection
- Add fingerprint field to skill origin tracking
- Add fingerprint field to skill lock entries
- Integrate fingerprint computation into skill import and update
- Add gridctl export command for spec reverse-engineering
- Enhance skill try with countdown display and signal handling
- Add export, secrets-map, and recipes API endpoints
- Add spec mode, wiring mode, and heatmap toggles to UI store
- Add export, secrets-map, and recipes API client functions
- Add canvas spec mode overlay with ghost and warning nodes
- Add secret heatmap overlay with color-coded shared secrets
- Add wiring mode overlay for agent-server connections
- Integrate spec mode, wiring mode, and heatmap into canvas
- Add stack recipe picker with category filtering
- Add transport compatibility advisor for wizard
- Integrate transport advisor into MCP server form
- Add vaultDetached state to UI store
- Add vault to detached window sync type
- Add vault window management with instant detach
- Add vault route with dark suspense fallback
- Rewrite vault panel with search, resize, and popout
- Add detached vault page for pop-out window

### Refactoring


- Convert BottomPanel to tabbed container for logs and metrics
- Fix staticcheck QF1003, QF1012, ST1005, ST1023 issues

## [0.1.0-beta.2] - 2026-03-11


### Bug Fixes


- Skip vault ref validation when no vault provided
- Auto-unlock vault with env passphrase on deploy
- Pass vault context through reload handler
- Wire vault store into hot reload handler

## [0.1.0-beta.1] - 2026-03-09


### Bug Fixes


- Remove unused type flagged by linter
- Make security scans non-blocking in CI
- Lower controller coverage threshold to 59%
- Resolve TypeScript errors in GatewayPanel test
- Remove unused import in LogViewer test
- Remove unused imports in hooks test

### Features


- Add workflow types to registry package
- Render workflow fields in SKILL.md
- Add workflow DAG builder with cycle detection
- Add workflow validation rules
- Add template engine for skill workflows
- Add ToMCPTool method for executable skills
- Add workflow executor engine
- Integrate executor with registry server
- Wire executor into gateway builder
- Add workflow REST API endpoints
- Add parallel execution, retry, and timeout to workflow executor
- Pass executor options through server constructor
- Add workflow TypeScript types
- Add workflow API functions
- Add workflow text zoom and blink CSS
- Add workflow Zustand store
- Add workflow detached window state
- Add workflow font size zoom hook
- Support workflow window in broadcast channel
- Add workflow window config to manager
- Add StepNode React Flow component
- Add WorkflowGraph DAG visualization
- Add WorkflowInspector step detail panel
- Add WorkflowRunner test panel
- Add WorkflowPanel composition component
- Add workflow tab to SkillEditor
- Add detached workflow pop-out page
- Add workflow route to app router
- Add workflow YAML sync utilities
- Add toolbox palette with drag-and-drop
- Add editable step inspector panel
- Add editable workflow canvas
- Add visual designer composition layer
- Add Code/Visual/Test mode toggle
- Add generalized useTextZoom hook with container props
- Add useContainerWidth hook for responsive layout
- Add workflow keyboard shortcuts hook
- Update workflow pop-out window to 1200x800
- Add execution history and last arguments to workflow store
- Add workflow execution animations and text zoom CSS
- Add execution animations and custom memo comparator to StepNode
- Add edge dash-flow animation for active workflow edges
- Add execution history, error recovery, and dimmed history cards
- Add workflow empty state with template insertion
- Add empty canvas hint for visual designer
- Add responsive layout with container width breakpoints
- Enhance detached workflow with mode toggle and execution sync
- Add workflow badge and quick-open button to skill list
- Add executable badge to registry node in topology graph
- Add VaultDir helper to state package
- Add vault secret type definition
- Add vault store with CRUD and atomic writes
- Add unified expansion with vault resolution
- Add vault value redaction to log handler
- Load vault and wire into deploy pipeline
- Pass vault to gateway for redaction and API
- Add vault store and routes to API server
- Add vault REST API endpoints
- Add vault CLI commands
- Register vault command in CLI
- Add variable set types to vault package
- Add variable set operations to vault store
- Add VaultSetLookup interface for set injection
- Add Secrets config type for variable sets
- Inject variable set secrets into container env
- Wire vault set injection into deploy flow
- Add vault set REST API endpoints
- Add vault sets CLI commands and --set flag
- Add vault API client functions
- Add vault Zustand store
- Add vault management slide-over panel
- Wire settings button to vault panel
- Add encrypted vault types for envelope encryption
- Add XChaCha20-Poly1305 envelope encryption
- Integrate encryption into vault store
- Add vault lock, unlock, and change-passphrase CLI commands
- Add HTTP 423 Locked status constant
- Add vault status, lock, and unlock API endpoints
- Add vault encryption API client functions
- Add lock state management to vault store
- Add vault passphrase unlock prompt component
- Integrate lock/unlock flow into vault panel
- Add skills fields to GatewayNodeData and remove RegistryNodeData
- Pass registry status to gateway node data
- Add skills stat row with monochromatic icon style
- Add embedded prop to RegistrySidebar
- Add GatewaySidebar with embedded registry
- Wire GatewaySidebar into sidebar dispatch
- Add search filtering to registry sidebar
- Add NeedsDocker and IsContainerBased predicates to config
- Defer Ping and EnsureNetwork behind NeedsDocker guard
- Skip Docker status query for non-container stacks
- Graceful destroy when Docker is unavailable
- Show gateway status when Docker is unavailable
- Add compact height constants for all node types
- Add compact option to layout types
- Support compact dimensions in getNodeDimensions
- Pass compact state through butterfly layout engine
- Thread compact option through transform pipeline
- Add compactCards toggle to UI store
- Read compact state when calculating layout
- Add compact rendering to CustomNode
- Add compact rendering to AgentNode
- Add compact rendering to ClientNode
- Add compact cards toggle button to canvas toolbar
- Add runtime detection module for Docker and Podman
- Add NewWithInfo factory for runtime-aware orchestrator creation
- Add runtime-aware host alias and error messages to orchestrator
- Add NewDockerClientWithHost for explicit socket selection
- Add runtime info support to DockerRuntime driver
- Add runtime-aware host alias and SELinux volume labels
- Register runtime-aware orchestrator factory
- Add runtime detection and selection to controller
- Use runtime-aware host alias in reload handler
- Add --runtime persistent flag for runtime selection
- Pass runtime flag from deploy command to controller
- Add gridctl info subcommand for runtime diagnostics
- Print runtime info and rootless warning at deploy startup
- Add individual MCP server restart API and UI

### Refactoring


- Migrate useLogFontSize to delegate to useTextZoom
- Migrate useWorkflowFontSize to delegate to useTextZoom
- Integrate workflow keyboard shortcuts hook
- Use unified expansion in stack loader
- Replace popup window configs with simple tab-based navigation
- Simplify PopoutButton using IconButton component
- Remove redundant tooltip prop from PopoutButton usage
- Remove redundant tooltip prop from sidebar PopoutButton
- Remove redundant tooltip prop from registry PopoutButton
- Remove gateway-to-registry edge
- Remove registry status from edge creation
- Remove gateway-to-registry edge relation type
- Remove registry exports from graph index
- Remove registry zone assignment from layout
- Remove registry dimensions from layout utils
- Remove registry node type and layout constants
- Remove standalone RegistryNode component
- Remove registry from node type registry
- Rename NeedsDocker to NeedsContainerRuntime
- Replace Docker-specific strings with runtime-agnostic text
- Use runtime-agnostic error messages in destroy
- Use runtime-agnostic error message in status

## [0.1.0-alpha.11] - 2026-02-27


### Bug Fixes


- Update stale unlink command help text
- Reject HTML responses and warn on OpenAPI 3.1 compat errors
- Check w.Write return values in tests

### Features


- Add OpenCode provisioner for link/unlink
- Register OpenCode in provisioner registry
- Add OpenCode case to simulateLink
- Add code_mode fields to GatewayConfig
- Add code_mode validation rules
- Add esbuild transpiler for code mode
- Add tool search index for code mode
- Add goja sandbox with tool bindings
- Add search and execute meta-tool defs
- Add code mode orchestrator
- Integrate code mode into gateway
- Add CodeMode to controller config
- Wire code mode config to gateway
- Add --code-mode flag to deploy command
- Add code_mode to /api/status response
- Show code mode in gridctl status
- Add Code Mode column to gateway table
- Add code_mode to frontend types
- Extract codeMode in stack store
- Pass codeMode through graph transform
- Pass codeMode to gateway node data
- Add Code Mode badge to gateway node
- Add Code Mode indicator to status bar

## [0.1.0-alpha.10] - 2026-02-23


### Bug Fixes


- Use streamable HTTP endpoint for Claude Desktop bridge
- Use streamable HTTP endpoint for Cline bridge

## [0.1.0-alpha.9] - 2026-02-19


### Bug Fixes


- Remove unused toolNames helper function
- Update CORS methods and registry comment
- Check return value of w.Write for errcheck
- Handle legacy prompt type in detached editor
- Support recursive skill discovery in nested directories
- Sort skills list for deterministic API responses
- Sort router clients and tools by name
- Sort MCP server statuses by name
- Sort A2A agent lists for stable ordering
- Sort unified agent statuses by name
- Use dedicated registry window for popout
- Add zoom controls and scalable text to sidebar

### Features


- Replace registry types with AgentSkill for agentskills.io spec
- Add skill validator per agentskills.io spec
- Add SKILL.md frontmatter parser and renderer
- Replace skill editor with markdown split-pane layout
- Add file tree browser for skill directories
- Integrate file tree into skill editor
- Improve skills editor UX with resizable panes and larger inputs
- Enlarge detached editor window for better editing
- Add Dir field to AgentSkill for nested path tracking
- Add registryDetached state to UI store
- Add registry type to broadcast channel
- Add registry window management support
- Add dedicated detached registry page
- Add detached registry route

### Refactoring


- Migrate store to directory-based SKILL.md layout
- Update registry server for AgentSkill types
- Remove step-based executor for markdown skills
- Update API endpoints for skills-only registry
- Directory-based skill storage with file management
- Serve agent skills as prompts instead of tools
- Remove ToolCaller from registry server constructor
- Update resource URI scheme to skills://registry/
- Remove executor placeholder file
- Add file management and validation endpoints
- Replace prompt/skill types with AgentSkill model
- Update API client for agent skills registry
- Simplify registry store to skills-only
- Remove prompt fetching from polling hook
- Update registry node to skills-only counts
- Update registry edge condition for skills-only
- Display skills-only counts in registry node
- Remove obsolete prompt editor component
- Remove obsolete skill test runner component
- Rewrite skill editor for AgentSkill model
- Simplify registry sidebar to skills-only
- Update detached editor for skills-only
- Replace sidebar tabs with single skills list view
- Add agent skills sublabel to registry node
- Replace chunk size suppression with vendor splitting
- Lazy-load detached page routes

## [0.1.0-alpha.8] - 2026-02-16


### Bug Fixes


- Use stable ID keys in prompt editor arguments
- Use stable ID keys in skill editor steps and inputs
- Clarify registry node counts with active/total format
- Correct gateway port in multi-agent example docs

### Features


- Add registry types for prompts and skills
- Add file-based registry store with YAML persistence
- Add ToolCaller interface for decoupled tool execution
- Implement ToolCaller on Gateway
- Add registry server implementing AgentClient
- Add registry server field and accessors to API server
- Wire registry server into gateway build pipeline
- Add registry REST API handlers for prompts and skills
- Wire registry routes and enrich status endpoint
- Add MCP prompts and resources protocol types
- Implement PromptProvider interface on registry server
- Add gateway handlers for prompts and resources
- Route prompts and resources methods in HTTP handler
- Route prompts and resources methods in SSE server
- Add registry TypeScript types and node data
- Add registry API client functions
- Add registry Zustand store
- Integrate registry polling into data fetch cycle
- Add registry node type and layout dimensions
- Add gateway-to-registry edge relation type
- Add createRegistryNode with progressive disclosure
- Add gateway-to-registry edge creation
- Pass registry status through graph transform
- Assign registry node to Zone 2 in layout
- Add registry node dimensions to layout utils
- Export registry node and edge functions
- Include registry status in graph refresh
- Trigger graph refresh on registry visibility change
- Add registry graph node component
- Register registry node type in React Flow
- Add registry sidebar with prompts, skills, status tabs
- Route registry node selection to RegistrySidebar
- Add reusable modal component
- Add toast notification system
- Add prompt editor modal
- Add skill editor modal with tool chain builder
- Wire modal editors into registry sidebar
- Add toast container to app layout
- Implement skill CallTool with timeout and state validation
- Add skill execution engine with template resolution
- Add skill test run REST API endpoint
- Add ToolCallResult types for skill test runs
- Add testRegistrySkill API function
- Add skill test runner modal
- Add delete, activate/disable, and test run actions
- Add editorDetached state to UI store
- Add editor type to broadcast channel sync
- Add editor window config and detach handlers
- Add expandable, popout, and flush modes to modal
- Add popout and expand props to prompt editor
- Add popout and expand props to skill editor
- Add detached editor page for popout window
- Register /editor route for detached editor
- Wire popout handlers for prompt and skill editors

## [0.1.0-alpha.7] - 2026-02-12


### Bug Fixes


- Add session cap with eviction and count method
- Add periodic session cleanup to MCP gateway
- Add TTL-based cleanup for A2A tasks
- Add periodic A2A task cleanup to gateway
- Wire cleanup goroutines into deploy lifecycle
- Check HandleInitialize error in session count test
- Add context cancellation to stdio transport reader goroutine
- Add context cancellation to process transport reader goroutines
- Add missing docker factory import in integration tests
- Use Ping to verify Docker availability in test
- Remove unused setupMockAgentClientWithCallTool
- Remove empty branch flagged by staticcheck SA9003
- Validate agent identity on SSE tools requests
- Reorder shutdown to broadcast before closing HTTP
- Drain pending requests on all readResponses exit paths
- Drain pending requests on all ProcessClient exit paths
- Data race in ProcessClient between readResponses and Reconnect
- Data race in StdioClient between readResponses and Reconnect
- Add client count display to gateway node
- Use mcpServers wrapper and native SSE for AnythingLLM provisioner
- Upgrade Cursor provisioner to native SSE transport
- Align client nodes with agents in butterfly layout
- Split agent layout dimensions into width and height
- Use separate agent width and height for layout
- Left-align nodes within zones using max width
- Match left-side edges to right-side style
- Only preserve user-dragged node positions
- Use single centered input handle on gateway
- Widen agent node to match client width
- Match client handle size to other nodes
- Wire RedactingHandler into gateway logging chain
- Redact secrets in verbose output and orchestrator logs
- Restrict daemon log file permissions to 0600
- Restrict state file permissions to 0600

### Features


- Add reload package for config hot reload
- Add reload API endpoint and handler support
- Add --watch flag and hot reload integration
- Add reload CLI command
- Add MaxRequestBodySize constant for body limits
- Add GatewayConfig with allowed_origins to stack schema
- Add env var expansion for gateway allowed_origins
- Add body size limit and remove inline CORS from MCP handler
- Add body size limit and remove inline CORS from SSE handler
- Add body size limit and remove inline CORS from A2A handler
- Refactor CORS middleware to accept configurable origins
- Thread allowed origins from stack config to API server
- Add AuthConfig struct to gateway config
- Add validation rules for auth config
- Expand env vars in auth token config
- Add auth middleware for bearer and API key
- Wire auth middleware into HTTP handler
- Add HasAgent method for identity validation
- Validate X-Agent-Name against known agents
- Thread auth config from stack to API server
- Expose session and task counts in status API
- Extend gateway Close to drain client connections
- Add Close method to SSE server
- Add Close method to API server
- Add graceful HTTP shutdown with connection draining
- Add agent identity tracking to SSE sessions
- Include agent identity in MCP_ENDPOINT URL
- Include agent identity in reload MCP_ENDPOINT
- Add SetServerMeta method to gateway
- Add Pingable interface for health checks
- Add Ping method to StdioClient
- Add Ping method to ProcessClient
- Add Ping method to OpenAPIClient
- Add health monitor to gateway
- Expose health status in API responses
- Wire up health monitor in deploy command
- Add health fields to frontend types
- Show health status in graph nodes
- Add Reconnectable interface for MCP clients
- Add reconnection support to StdioClient
- Add reconnection support to ProcessClient
- Trigger reconnection from health monitor
- Add SSE shutdown broadcast notification
- Add shared formatRelativeTime utility
- Add health indicator to MCP server nodes
- Add health details to sidebar status section
- Show unhealthy server count in header
- Show unhealthy count in status bar
- Add openapi fields to MCP server types
- Pass openapi fields through graph node mapping
- Add OpenAPI icon and type badge to graph node
- Add OpenAPI label and spec display to sidebar
- Add session and task count fields to gateway types
- Store session and task counts from status response
- Thread session and task counts through graph transform
- Pass session and task counts to gateway node data
- Display session and A2A task counts in gateway node
- Show session count in status bar
- Add reload API function and result type
- Add reload button with notification to header
- Add auth token management and 401 detection to API layer
- Add auth state store for gateway authentication
- Detect auth errors in polling and pause during auth
- Add auth prompt overlay component
- Integrate auth prompt into app layout
- Differentiate network errors from HTTP errors in polling
- Add SSE shutdown event listener hook
- Add contextual error overlay and shutdown notification
- Add client provisioner registry and interface
- Add platform detection helpers
- Add JSONC read/write with comment detection
- Add config file backup before modification
- Add mcp-remote bridge and npx detection
- Add shared link/unlink logic for MCP clients
- Add Claude Desktop provisioner
- Add Cursor provisioner
- Add Windsurf provisioner
- Add VS Code provisioner
- Add Continue provisioner
- Add Cline provisioner
- Add AnythingLLM provisioner
- Add Roo Code provisioner
- Add link command for LLM client configuration
- Add unlink command to remove client config
- Register link and unlink commands
- Add --flash flag and post-deploy link hint
- Add YAML read/write utilities for provisioner system
- Add httpConfig bridge helper for HTTP-native clients
- Add GatewayHTTPURL, Port field, and register new provisioners
- Extend DryRunDiff for YAML and add new provisioner cases
- Add Claude Code provisioner with custom detection
- Add Gemini CLI provisioner
- Add Zed Editor provisioner
- Add Goose provisioner with YAML config support
- Pass Port in link opts and update supported clients list
- Pass Port in flash link opts for HTTP-native clients
- Add AllClientInfo method for client detection status
- Add /api/clients endpoint for LLM client status
- Wire provisioner registry to API server
- Add ClientStatus and ClientNodeData types
- Add fetchClients API function
- Add client node dimensions and type constant
- Add client zone and edge relation type
- Add client node creation functions
- Add client-to-gateway edge creation
- Add client zone to butterfly layout
- Add client node dimensions to layout utils
- Thread clients through graph transform pipeline
- Re-export client graph functions
- Add ClientNode component for linked LLM clients
- Register client node type
- Add LLM client support to sidebar
- Add clients state to stack store
- Poll /api/clients endpoint
- Add client path highlighting
- Add RedactingHandler for secret redaction in logs

### Refactoring


- Add MCP protocol version and timeout constants
- Use named constants in HTTP MCP client
- Use named constants in stdio MCP client
- Use named constants in process MCP client
- Use named constants in MCP gateway
- Add A2A timeout constant
- Use named timeout constant in A2A client
- Use named constants in A2A adapter
- Use named constant for daemon shutdown grace
- Use named constant for reload HTTP timeout
- Add shared JSON-RPC 2.0 types package
- Re-export JSON-RPC types from shared package in mcp
- Re-export JSON-RPC types from shared package in a2a
- Add Logger field to BuildOptions
- Add LoggerSetter and propagate logger to runtime
- Add logger to DockerRuntime
- Pass logger through builder adapter
- Replace fmt.Printf with slog in git operations
- Replace fmt.Printf with slog in image building
- Initialize and pass logger in builder
- Replace fmt.Printf with slog in image pulling
- Replace fmt.Printf with slog in A2A gateway
- Pass logger to A2A gateway constructor
- Add ClientBase with shared state and accessor methods
- Embed ClientBase in HTTPClient
- Embed ClientBase in StdioClient
- Embed ClientBase in ProcessClient
- Embed ClientBase in OpenAPIClient
- Move label constants from compat to interface
- Use UpResult and Orchestrator directly in CLI
- Remove compat layer after consumer migration
- Remove hand-rolled AgentClient mock
- Add RPCClient base with transporter interface
- Embed RPCClient in HTTP transport client
- Embed RPCClient in stdio transport client
- Embed RPCClient in process transport client
- Remove JSON-RPC type re-exports from mcp package
- Remove JSON-RPC type re-exports from a2a package
- Use jsonrpc types directly in client_base
- Use jsonrpc types directly in mcp handler
- Use jsonrpc types directly in SSE server
- Use jsonrpc types directly in HTTP client
- Use jsonrpc types directly in stdio client
- Use jsonrpc types directly in process client
- Use jsonrpc types directly in a2a handler
- Use DefaultPingTimeout in HTTP client Ping
- Add controller package with Config and StackController
- Add DaemonManager for fork and readiness
- Add ServerRegistrar for MCP server registration
- Add GatewayBuilder for gateway lifecycle
- Slim deploy.go to thin CLI layer over controller
- Remove AnythingLLM special case from simulateLink

## [0.1.0-alpha.6] - 2026-02-04


### Bug Fixes


- Prevent selection glow bleedthrough on agent badges
- Add null safety for nodes and edges arrays
- Add null safety for mcpServers array
- Add null safety for mcpServers and resources arrays
- Add null safety for logs array
- Add null safety for tools and whitelist arrays
- Add null safety for graph node creation
- Scale log grid columns with font size and add text wrapping

### Features


- Add kin-openapi dependency for OpenAPI parsing
- Add OpenAPI config types for MCP server definition
- Support env var expansion and path resolution for OpenAPI specs
- Add validation rules for OpenAPI MCP server configuration
- Register OpenAPI clients in MCP gateway
- Implement OpenAPI client for MCP tool transformation
- Handle OpenAPI servers in orchestrator
- Add OpenAPI fields to runtime compatibility types
- Handle OpenAPI transport in deploy command
- Add POSIX-style environment variable expansion for OpenAPI specs
- Add NoExpand config option to OpenAPIClientConfig
- Apply env var expansion when loading local OpenAPI specs
- Add --no-expand flag to disable env var expansion in OpenAPI specs
- Add ResizeHandle component for draggable panel resizing
- Implement CSS Grid layout with resizable panels
- Add BroadcastChannel hook for cross-window sync
- Add window manager hook for detached windows
- Add PopoutButton component for panel headers
- Add detached window state tracking to UIStore
- Add detached logs page with node selector
- Add detached sidebar page with node selector
- Add React Router with detached panel routes
- Add popout button to Sidebar header
- Add popout button to BottomPanel header
- Add in-memory circular log buffer for API
- Add structured slog handler with buffering
- Add /api/logs endpoint for structured gateway logs
- Integrate structured logging with buffer handler
- Add fetchGatewayLogs API function
- Add structured log viewer with filtering
- Add detached logs and sidebar pages
- Add shared log types and parsing utilities
- Add shared LogLine component
- Add shared LevelFilter component
- Add useLogFontSize hook with persistence
- Add ZoomControls component
- Add barrel export for log components
- Add logger support to HTTP MCP client
- Add logger support to stdio MCP client
- Add logger support to process MCP client
- Add logger support to OpenAPI MCP client
- Inject loggers into clients and log tool calls
- Parse Docker timestamps and slog text format in log viewer
- Expand env vars in command, url, and a2a-agent fields
- Capture process stderr and log at warn level
- Add init timing, readiness, and access denial logging
- Share log buffer with orchestrator in foreground mode
- Add Chrome DevTools MCP platform example
- Add Context7 MCP platform example

### Refactoring


- Simplify UI store for panel state management
- Simplify Sidebar to fill parent container
- Simplify BottomPanel to fill grid cell
- Use shared log components and add zoom controls
- Use shared log components and add zoom controls

## [0.1.0-alpha.5] - 2026-01-29


### Bug Fixes


- Correct GitHub admonition syntax in README

### Features


- Add Butterfly layout engine for hub-and-spoke visualization
- Add path highlighting hook for agent selection
- Integrate path highlighting into Canvas component

### Refactoring


- Add graph layout type definitions
- Add graph utility functions
- Add Dagre layout engine implementation
- Extract node factory functions to graph module
- Extract edge creation with relation metadata
- Add graph transformation orchestration
- Add graph module public exports
- Extract tool parsing utilities
- Simplify transform.ts to re-export graph module
- Remove legacy layout module

## [0.1.0-alpha.4] - 2026-01-28


### Refactoring


- Rename Topology struct to Stack in config types
- Rename LoadTopology to LoadStack
- Update validate to use Stack terminology
- Rename TopologyName/TopologyFile to StackName/StackFile
- Update runtime interface for Stack terminology
- Update orchestrator for Stack terminology
- Update runtime compat for Stack terminology
- Rename LabelTopology to LabelStack
- Update container for Stack terminology
- Update docker driver for Stack terminology
- Update docker network for Stack terminology
- Update a2a client comment for Stack
- Rename topology parameter to stack in builder
- Rename topologyName to stackName in API
- Update deploy command for Stack terminology
- Update destroy command for Stack terminology
- Rename --topology flag to --stack in status
- Update root help text for Stack terminology
- Rename useTopologyStore to useStackStore
- Update App.tsx for useStackStore
- Update Canvas for useStackStore
- Update Header for useStackStore
- Update Sidebar for useStackStore
- Update StatusBar for useStackStore
- Update BottomPanel for useStackStore
- Update ToolList for useStackStore
- Update usePolling for useStackStore

## [0.1.0-alpha.3] - 2026-01-27


### Bug Fixes


- Remove duplicate v prefix from gateway node version display
- Wait for MCP servers to initialize before returning from deploy
- Remove changelog generation from release workflow

### Features


- Add ASCII banner with two-tone coloring
- Add colored CLI help with Obsidian Observatory theme
- Display banner on version command
- Add SetVersion method to gateway
- Pass version to gateway on deploy
- Add brand logo asset
- Replace header icon with brand logo
- Add ToolSelector type for agent-level tool filtering
- Add tool whitelist filtering to HTTP MCP client
- Add tool whitelist filtering to stdio MCP client
- Add tool whitelist filtering to process MCP client
- Add agent-level tool filtering to gateway
- Return full ToolSelector in agent status API
- Pass tool whitelist to MCP servers on deploy
- Add tool filtering example
- Add ToolSelector type to frontend
- Add whitelist filtering to ToolList component
- Add Access section to agent sidebar
- Add amber color theme for terminal output
- Add output package with printer and banner
- Add summary tables for workloads and gateways
- Use output package in deploy command

### Refactoring


- Update mergeEquippedSkills for ToolSelector type
- Update validation for ToolSelector type
- Update compat types for ToolSelector
- Update orchestrator for ToolSelector type
- Update graph transform for ToolSelector
- Use output package in version command
- Use output package in status command
- Use output package in destroy command

## [0.1.0-alpha.2] - 2026-01-23


### Refactoring


- Update module path to github.com/gridctl/gridctl
- Rename cmd/agentlab to cmd/gridctl
- Update import paths and branding in Go packages
- Update web UI branding to Gridctl

## [0.1.0-alpha.1] - 2026-01-21


### Bug Fixes


- Correct handle positions and remove translate-y hover
- Remove translate-y hover to prevent clipping
- Remove translate-y hover to prevent clipping
- Add overflow visible to prevent React Flow clipping
- Position agents on right side of gateway
- Check json decode errors in A2A handler tests
- Add volume mount support to ContainerConfig
- Pass volumes from Resource config to container
- Add SSE response parsing and session tracking to MCP client
- Correct Itential MCP server transport configuration
- Use json.RawMessage for MCP tool input schema
- Serialize A2A skill input schema to json.RawMessage
- Use Record<string, unknown> for tool inputSchema
- Handle generic inputSchema in ToolList component
- Check error return from Process.Kill
- Handle write error in health endpoint
- Change tool name delimiter from :: to __ for client compatibility
- Skip SSE notifications when parsing tool call responses
- Return friendly message for nodes without container logs
- Add liveness health check and readiness endpoint
- Start HTTP server before MCP registration
- Correct tool name delimiter to match backend

### Features


- Add topology configuration types
- Add topology YAML loader
- Add topology validation rules
- Add Docker client interface for mocking
- Add Docker client wrapper
- Add container naming and labels
- Add Docker network management
- Add Docker image pulling
- Add container lifecycle management
- Add high-level runtime orchestration
- Add daemon state management
- Add MCP protocol types and JSON-RPC
- Add HTTP transport MCP client
- Add stdio transport MCP client
- Add MCP session management
- Add MCP tool routing with prefixes
- Add MCP protocol bridge gateway
- Add MCP HTTP request handlers
- Add SSE server for MCP clients
- Add image builder types
- Add build cache management
- Add git clone and update for builds
- Add Docker image building
- Add source-to-image builder
- Add legacy HTTP server
- Add unified API server with MCP and REST
- Add embedded web assets for production
- Add up command for topology deployment
- Add down command for topology teardown
- Add status command for topology info
- Add HTML entry point
- Add Vite logo asset
- Add React logo asset
- Add global CSS styles
- Add TypeScript type definitions
- Add classname utility
- Add UI constants
- Add API client for backend
- Add topology to React Flow transform
- Add topology state store
- Add UI state store
- Add keyboard shortcuts hook
- Add polling hook for status updates
- Add Badge component
- Add Button component
- Add IconButton component
- Add StatusDot component
- Add ControlBar component
- Add LogViewer component
- Add ToolList component
- Add Header layout component
- Add Sidebar layout component
- Add StatusBar layout component
- Add React Flow node type registry
- Add CustomNode for agent visualization
- Add GatewayNode for gateway visualization
- Add React Flow Canvas component
- Add React app entry point
- Add main App component
- Add bottom panel state management to UI store
- Add collapsible bottom panel for log viewing
- Add Cmd/Ctrl+J shortcut for bottom panel toggle
- Integrate bottom panel into main layout
- Add Agent struct to topology configuration
- Add validation rules for agent configuration
- Add env expansion and path resolution for agents
- Add agent label constant and helper function
- Add agent container lifecycle management
- Add agent status to API response
- Add agent support to deploy command
- Add MCP_ENDPOINT injection for agent containers
- Add agent access control to MCP gateway
- Add X-Agent-Name header support for tool access control
- Register agents with gateway for access control
- Add runtime and prompt fields for headless agents
- Add validation for headless agent schema
- Add AgentStatus and AgentNodeData types
- Add tertiary color palette for agent nodes
- Add agent nodes and edges to graph transform
- Add agents state to topology store
- Add circular AgentNode component
- Register AgentNode in React Flow node types
- Add agent count to gateway node display
- Add agent color to minimap node display
- Add agent-specific details to sidebar
- Add Command field to Agent config struct
- Pass agent Command to container config
- Add A2A protocol package with types, client, and gateway
- Add A2A configuration types to topology config
- Add validation for A2A config and remote agents
- Integrate A2A gateway into deployment
- Add A2A API endpoints to HTTP server
- Add A2A agent types to web frontend
- Add A2A layout constants
- Add A2A agent node and edge transformation
- Add A2A agent state to topology store
- Add A2AAgentNode component with teal theme
- Register A2AAgentNode in node types
- Add A2A agent edge coloring
- Add A2A agent count to gateway node
- Add A2A agent details to sidebar
- Populate equipped_skills from uses field
- Add cycle detection for agent dependencies
- Add dependency graph with topological sort
- Start agents in dependency order
- Add A2A-to-MCP adapter for agent skills
- Register A2A agent adapters on deploy
- Add dagre layout with LR hierarchy
- Unified agent node with variant styling
- Add logging package with discard handler
- Add structured logging to MCP gateway
- Add structured logging to runtime operations
- Add host.docker.internal mapping to containers
- Configure structured logging in deploy command
- Add tool name delimiter constant to frontend
- Add SSE transport type constant
- Add URL field and IsExternal helper for MCP servers
- Add validation for external MCP servers
- Skip container creation for external MCP servers
- Add SSE transport handling and External field to gateway
- Register external MCP servers and preserve on daemon restart
- Add transport and external fields to API response
- Add SSE transport and external field to frontend types
- Pass external field from API to node data
- Add transport icon and color utility functions
- Add violet styling and External badge for external servers
- Add external server styling to sidebar details
- Add mock MCP server for testing external servers
- Add example topology for external MCP servers
- Add IsLocalProcess helper for config detection
- Add validation for local process MCP servers
- Add ProcessClient for local stdio MCP servers
- Add local process support to MCP gateway
- Add local process fields to MCPServerInfo
- Register local process servers in deploy command
- Add LocalProcess field to API status response
- Add localProcess field to frontend types
- Include localProcess in MCP server node data
- Add local process indicator to MCP server nodes
- Add local process MCP server example
- Add SSH config type for remote MCP servers
- Add SSH config loading and env expansion
- Add SSH MCP server validation rules
- Add SSH transport support in MCP gateway
- Register SSH MCP servers with gateway
- Pass SSH config to runtime during deploy
- Expose SSH host in MCP server status API
- Add SSH fields to MCP server status types
- Add workload type to container status response
- Add --base-port flag for MCP server ports
- Add mock-servers and clean-mock-servers make targets
- Add configurable PORT param to mock-servers target
- Add GoReleaser configuration
- Add version command with ldflags
- Update release workflow for GoReleaser

### Refactoring


- Simplify CustomNode with clean design patterns
- Simplify GatewayNode with clean design patterns
- Integrate bottom panel and remove log viewer overlay
- Rename up command to deploy
- Rename down command to destroy
- Remove old up and down commands
- Register deploy and destroy commands
- Add equipped_skills field to agent config
- Filter A2A adapters from MCP server status
- Unify agent status with A2A info
- Unify AgentStatus and AgentNodeData types
- Remove A2A_AGENT node type constant
- Unify agent nodes with arrowhead edges
- Remove separate a2aAgents state
- Remove A2AAgentNode from registry
- Delete deprecated A2AAgentNode component
- Update minimap colors for unified agents
- Unified agent details in sidebar
- Change tool name delimiter from -- to ::
- Simplify MCP client result unmarshaling
- Simplify stdio client result unmarshaling
- Update parsePrefixedToolName for :: delimiter
- Remove unused LOCAL_PROCESS_STYLES constant
- Define WorkloadRuntime interface for runtime abstraction
- Add Orchestrator for runtime-agnostic workload management
- Add factory functions for runtime instantiation
- Add backward compatibility types and helpers
- Implement DockerRuntime as WorkloadRuntime
- Remove legacy runtime implementation files
- Update deploy command for new runtime API
- Update destroy command for new runtime API
- Update status command for new runtime API
- Update API server for new runtime types
- Update state management for new runtime types
- Enhance health endpoint to verify MCP server initialization
- Add file locking and graceful daemon shutdown
- Replace sleep with health polling on deploy
- Add locking to destroy command
- Remove unused A2A capability fields
- Change default gateway port to 8180
