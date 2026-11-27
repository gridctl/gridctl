# Tools Workspace

The Tools workspace (`/tools` in the web UI on `:8180`) is the control plane for the tool surface your gateway exposes: what each MCP server advertises, which of those tools upstream clients can call, which exposed tools are actually used, and how to shrink the surface safely. You can call a tool from the detail rail with [Try it](#try-it).

## Layout

- **Server rail (left)**: every MCP server in the stack with an `enabled/total` badge. While you have unsaved edits, the active server's badge shows the draft count with an `unsaved` chip; saving or discarding restores the live count.
- **Center**: the whitelist editor for the selected server. The checkbox toggles a tool's exposure; clicking the row opens the tool in the detail rail. These are separate gestures on purpose, so inspecting a tool never changes what agents can call. From the keyboard, Tab from the filter input moves to the highlighted row, Space toggles that tool, and Enter opens its details; arrow keys, Home, and End move the highlight and focus together. The checkboxes are not tab stops, so the next Tab leaves the list. Each row is announced with its name, enabled or disabled state, groups, hints, policy verdict, and Audit state. A list of more than 200 tools renders only the rows near the scroll position.
- **Detail rail (right)**: a collapsed [Try it](#try-it) section for calling the tool, then the description, a parameters table (name, type, required, default, and allowed values) above a collapsible raw schema with a Copy button, server-reported hints, the policy verdict when one applies, and usage stats for the selected tool. The header has Copy name (copies the prefixed `server__tool` name), a pin chip that opens the server in Pins (pin status is per server, and the chip is absent for a tool the saved allow-list hides), and links to Traces and Metrics filtered to the tool. Metrics appears once the tool has recorded calls. Traces keeps a time range chosen on an earlier visit. With unsaved allow-list edits, these links ask before discarding them. Long descriptions are shortened with a count of the hidden characters and a Show more button, and descriptions are never rendered as markdown.
- **Header**: global search across every server, plus the Groups, Access, Fleet, and Audit entry points.

Global search matches the query anywhere in a tool's name or description, ignoring where in the text it appears, and tolerates about one typo. Tools whose name contains the query are listed before tools that match only in their description. Exact matches are highlighted in the server name, tool name, and description, and a description whose match falls late in the text is shown from shortly before the match. With focus in the search input, Up and Down move through the results (wrapping at the ends) and Enter opens the active one. Opening a result shows the tool on its server with a "Back to results" chip, which restores the same query, the result list, and the row you picked. Selecting a server in the rail, typing a new query, or dismissing the chip removes it.

A failed fetch is not an empty stack. With no servers loaded, the pane says "Tools unavailable" and offers Retry. When servers are already on screen, a banner names the failure and the editor stays mounted, including an unsaved whitelist edit. See [a workspace says a feature is unavailable](troubleshooting.md#a-workspace-says-a-feature-is-unavailable).

Everything an operator would put in a support link lives in the URL: `?server=`, `?q=`, `?back=` (the query a result pick left behind, offered as Back to results; ignored while `?q=` is set), `?tool=`, `?audit=1`, `?window=`, `?filter=`, `?sort=`, `?risk=`, and `?client=<slug>` (the Connections hub's deep link: opens the per-client access editor on that client once, then drops itself from the URL). Defaults are omitted, so a bare `/tools` stays canonical.

## Try it

The detail rail's Try it section calls the selected tool through `POST /api/tools/call`, the same dispatch path as `gridctl call` and MCP `tools/call`. It sends only `name` (`server__tool`), `arguments`, and the caller label `ui`, with the UI's existing gateway credential. Every gate applies: client scope, the allow-list, `policy:` rules, rate limits, routing, and schema pins. When `clients:` scopes callers, the `ui` label must be allowed there (add a `ui` profile), or use `gridctl call --as <client>` to call as another client. The UI has no way to choose another label. To call or search the live surface from the CLI, see [Live tools](cli-reference.md#live-tools).

Arguments come from a form built from the tool's input schema or from raw JSON; both edit the same object, and properties outside the schema are kept. Unset optional fields are not sent, and defaults appear as placeholders until you choose Insert defaults. The browser checks a subset of JSON Schema (types, required, enum, const, numeric and length bounds, pattern, item counts, nested properties, single-schema items, and `additionalProperties: false`) before sending. When a schema uses anything else, such as `$ref`, `oneOf`, or `format`, a note says those parts are not checked in the browser and the server validates the call. Integers beyond 2^53 lose precision in arguments and results.

A confirmation names the tool and shows the arguments before every call unless the server declares the tool `readOnlyHint: true` or `destructiveHint: false`. MCP treats a tool without annotations as potentially destructive, so it also asks.

Run is disabled, with the reason beside it, when the saved allow-list hides the tool, when the policy verdict is anything other than allow, or while the gateway credential awaits re-entry. Calls use the saved allow-list: an unsaved checkbox change shows a note and neither enables nor disables Run.

Results render as plain text only: text blocks in a monospace block (JSON pretty-printed), other blocks as a one-line summary, and never as images, links, HTML, or markdown. A denial or tool error is shown with the gateway's reason. Output beyond 20,000 characters is cut off until you press Show all, and Copy always copies the full result. "View trace" opens the call's trace when the gateway recorded one; otherwise "View traces for this tool" opens Traces filtered to the tool.

Cancel, or Escape inside the section while a call runs, drops the request. The tool may still have run on the server. Selecting another tool or closing the rail also drops an in-flight call. The last five argument sets per tool are kept in memory for the page session only, never in browser storage or the URL, and are cleared when the gateway credential changes.

## Expose-all semantics

A server with no `tools:` whitelist in `stack.yaml` exposes every tool it advertises. An empty whitelist is not "expose nothing" (the YAML cannot express that); it is the same as no whitelist. The editor refuses to save an empty selection for exactly this reason: it would silently re-expose everything. To narrow a server, keep at least one tool enabled and save; to widen it back, use Select all or the Fleet "Expose all tools" action.

The count line above the list tells the two cases apart: `All 107 tools exposed (no allow-list)` for a server with no saved whitelist, and `12 of 107 exposed` for one with a saved whitelist, even when that whitelist names every tool (a tool the server advertises later stays hidden). While you have unsaved edits it reads `13 of 107 selected (unsaved)`. The Stack sidebar's Tools editor uses the same wording.

Saves write `stack.yaml` once and trigger a single reload. If the file changed on disk since the UI loaded it, the save is refused with a Reload file affordance instead of overwriting your edits.

Unsaved edits belong to the server they were made on, and Save writes only to that server. If the active server changes under them (for example the server leaves the stack), a confirm offers Discard, plus Keep editing when the server still exists. The Stack sidebar's Tools editor follows the same rule. While another server is selected, the editor is locked: Save is disabled and the rows, Select all, and Clear do nothing until you return to the edited server or discard. Selecting another node, clicking the canvas background, pressing Escape, closing or popping out the inspector, the command palette's Inspect commands, the pending authorization badge and toast, and the inspector's View Logs, Variables, and Open in Tools workspace buttons ask before discarding the edits. Reloading or closing the tab with unsaved sidebar edits shows the browser's leave warning.

Leaving the Tools workspace with unsaved edits asks first, whichever shell control starts it: the workspace pills and keyboard shortcuts, the command palette, the status bar buttons and badges, a toast's View action, or the detail rail's pin chip and Traces and Metrics links. The "Discard unsaved changes" confirm names what is unsaved. Cancel keeps the page and the edits; "Discard & leave" restores the saved selection and opens the requested page. Browser Back and Forward are not covered.

Unchecking a tool that a group `tools:` entry or override names is refused. The checkbox reverts, and the notice names the group and tool (`Hidden by this change: groups ops (vault__read_secret)`), with a hint to remove it from the group or keep it exposed. The file is left unchanged. A profile `tools:` entry or a group `exclude` entry does not block the save. The Stack sidebar's Tools editor uses the same refusal. A Fleet batch that would hide such a tool is also refused and writes nothing; that panel shows the joined validation messages rather than the checkbox notice.

If that saved stack also changes effective gateway security, reload returns `restart_required`: the file retains the edit, but live tool and client scopes remain unchanged. Follow [gateway restart recovery](troubleshooting.md#gateway-security-requires-a-restart). Gateway credential re-entry resumes reads without automatically repeating a save or Fleet action.

## Audit Mode

Audit classifies every tool against a lookback window (24 hours, 7 days, or 30 days; 7 days is the default and matches the `unused_tool` heuristic in `gridctl optimize`):

- **used**: exposed, with at least one recorded call in the window.
- **unused**: exposed, with no recorded call in the window.
- **disabled**: not currently exposed; past activity is irrelevant.

Honesty rules to keep in mind when reading the overlay:

- Counts cover activity since the gateway process began recording (`Tracking since ...`). With metrics persistence enabled, restored counts may predate it, so "no recorded calls" is not proof of a longer disuse history.
- When optimize has findings, the header shows "Optimize suggests N unused tools (7d)" with a jump into Audit. Optimize skips servers with no traffic at all (they surface as `unused_server` findings instead) and always classifies against 7 days, so its count can be lower than the Audit overlay's.
- A2A usage groups advisory skill calls under `skill`, without individual skill attribution. An absent per-skill count is not evidence that a generated skill tool was unused. See [sensitive-call counting](usage-observability.md#sensitive-call-counting).

### Filtering and sorting

With Audit on, filter chips narrow the list to All, Used, Unused, or Disabled, with per-state counts. The Destructive chip works with or without Audit and narrows to tools whose server reports `destructiveHint: true`. Sort by name, most recent use, or call count; the default keeps the server-advertised order, and an active filter query keeps relevance order instead. While the filter has text, Sort is disabled with a "Relevance order" note beside it; clearing the filter re-enables it with the chosen order. Filter, sort, and risk choices persist across sessions and are shareable via the URL.

### Remediation

Two paths from "unused" to "disabled":

- **Per server**: with Audit on, the banner above the list offers "Disable N unused" for the selected server. It saves the saved allow-list minus the unused tools and nothing else: other unsaved checkbox changes stay unsaved and still count toward Save. If the save does not persist, the rows return to how they were before the click.
- **Fleet**: the Fleet panel's "Disable unused" action plans the same change across all servers, the active server, or a hand-picked subset, previews exactly which tools it will disable per server, and applies everything with one stack write and one reload.

Both paths refuse to empty a server's whitelist. A server whose every exposed tool is unused is skipped and reported, because an empty whitelist would re-expose everything.

## Tool hints (annotations)

MCP servers may annotate tools with `readOnlyHint`, `destructiveHint`, `idempotentHint`, and `openWorldHint`. The list shows compact chips (RO, RW, DESTR, SAFE, IDEM, OPEN, CLOSED) for declared hints, and the detail rail spells them out under Hints. The Legend button at the end of the list's filter row lists every chip with its full name and meaning, including the policy `BLOCKED` and `APPROVAL` chips.

Treat these as claims, not guarantees: they are reported by the server and not verified by gridctl. Per the MCP spec, a tool that declares nothing should be treated as potentially destructive and open-world; the Hints section says so explicitly for unannotated tools.

## Fleet, Access, and Groups

- **Fleet**: bulk actions across servers - expose all, hide tools matching a glob pattern, or disable unused. Every action follows plan, then confirm, then a single reload.
- **Access**: per-client scoping. Without a `clients:` block in `stack.yaml`, every linked client reaches every server; creating one flips unlisted clients to deny-by-default, and the editor warns before you do that. Server grants and per-server tool selections are both editable here; the same tool axis is available in the Stack workspace's Access Lens. An unsaved Access Lens draft, including a tool-only edit, asks before you leave the Stack workspace by the same controls; "Discard & leave" clears the draft and turns the lens off, and a navigation that stays in Stack does not ask. Downstream prompts and resources follow the server grant, not the tool selection.
- **Groups**: curated tool bundles served at `/groups/{name}/mcp`, configured via the `groups:` block. The Groups panel teaches the configuration when none exists yet.

A configured `policy:` block adds a `BLOCKED` chip (or `APPROVAL`, when the matched action is `require_approval`) beside the annotation chips. The title is `blocked by policy rule 1: vault__*` (one-based index and pattern), `blocked by policy: block_destructive`, or `blocked by policy default`. The detail rail repeats that text under Policy, and appends `(requires approval, enforced as block)` for an approval rule. An allow verdict has no chip. The chip is a verdict, not an edit. Changing the whitelist does not clear the policy. The gateway inspector in the Stack sidebar shows the same block as a read-only summary (default, rule count, and whether `block_destructive` is on) or `Policy: not configured`. It is not mounted on the per-server inspector.

Where they overlap: whitelists (this workspace) decide what the gateway exposes at all; the policy decides whether a listed tool may be called; Access decides which client sees which servers and tools; Groups publish named subsets at separate endpoints. Deferred discovery changes the MCP `tools/list` a client receives. This workspace stays on the full catalog. A tool must survive all applicable layers for a client to call it. Downstream prompts and resources follow server membership. A tool whitelist or a partial tool selection does not filter them. A group exposes a server's prompts and resources only when at least one of that server's tools is a member.

These filters are separate from [gateway authentication](config-schema.md#auth). When configured, the gateway credential is required on every grouped MCP request, just as on `/mcp`. OAuth uses the verified identity claim for client scope and binds sessions to that principal. In other modes, client selectors are self-declared, so client filters are guardrails for cooperating clients. Group names are not authenticated identities.

A tool that survives exposure filters can still be blocked by required container execution evidence. Open the server's collapsed Execution evidence section in the Stack inspector; MCP health and an enabled tool checkbox do not establish eligibility. [Execution controls](execution.md) also cover code-mode and deferred `call_tool` inner calls through the same gateway dispatch path.

## Related

- [Configuration Reference](config-schema.md) - `tools:`, `policy:`, `clients:`, `groups:`, and `tool_discovery`
- [Usage Observability](usage-observability.md) - the usage metrics behind the Tools data and `gridctl optimize`
- [REST API Reference](api-reference.md) - the endpoints backing this workspace
