# Tools Workspace

The Tools workspace (`/tools` in the web UI on `:8180`) is the control plane for the tool surface your gateway exposes: what each MCP server advertises, which of those tools upstream clients can call, which exposed tools are actually used, and how to shrink the surface safely.

## Layout

- **Server rail (left)**: every MCP server in the stack with an `enabled/total` badge. While you have unsaved edits, the active server's badge shows the draft count with an `unsaved` chip; saving or discarding restores the live count.
- **Center**: the whitelist editor for the selected server. The checkbox toggles a tool's exposure; clicking the row opens the tool in the detail rail. These are separate gestures on purpose, so inspecting a tool never changes what agents can call.
- **Detail rail (right)**: description, input schema, server-reported hints, and usage stats for the selected tool.
- **Header**: global search across every server, plus the Groups, Access, Fleet, and Audit entry points.

Everything an operator would put in a support link lives in the URL: `?server=`, `?q=`, `?tool=`, `?audit=1`, `?window=`, `?filter=`, `?sort=`, `?risk=`, and `?client=<slug>` (the Connections hub's deep link: opens the per-client access editor on that client once, then drops itself from the URL). Defaults are omitted, so a bare `/tools` stays canonical.

## Expose-all semantics

A server with no `tools:` whitelist in `stack.yaml` exposes every tool it advertises. An empty whitelist is not "expose nothing" (the YAML cannot express that); it is the same as no whitelist. The editor refuses to save an empty selection for exactly this reason: it would silently re-expose everything. To narrow a server, keep at least one tool enabled and save; to widen it back, use Select all or the Fleet "Expose all tools" action.

Saves write `stack.yaml` once and trigger a single reload. If the file changed on disk since the UI loaded it, the save is refused with a Reload file affordance instead of overwriting your edits.

## Audit Mode

Audit classifies every tool against a lookback window (24 hours, 7 days, or 30 days; 7 days is the default and matches the `unused_tool` heuristic in `gridctl optimize`):

- **used**: exposed, with at least one recorded call in the window.
- **unused**: exposed, with no recorded call in the window.
- **disabled**: not currently exposed; past activity is irrelevant.

Honesty rules to keep in mind when reading the overlay:

- Counts cover activity since the gateway process began recording (`Tracking since ...`). With metrics persistence enabled, restored counts may predate it, so "no recorded calls" is not proof of a longer disuse history.
- When optimize has findings, the header shows "Optimize suggests N unused tools (7d)" with a jump into Audit. Optimize skips servers with no traffic at all (they surface as `unused_server` findings instead) and always classifies against 7 days, so its count can be lower than the Audit overlay's.

### Filtering and sorting

With Audit on, filter chips narrow the list to All, Used, Unused, or Disabled, with per-state counts. The Destructive chip works with or without Audit and narrows to tools whose server reports `destructiveHint: true`. Sort by name, most recent use, or call count; the default keeps the server-advertised order, and an active search query keeps relevance order instead. Filter, sort, and risk choices persist across sessions and are shareable via the URL.

### Remediation

Two paths from "unused" to "disabled":

- **Per server**: with Audit on, the banner above the list offers "Disable N unused" for the selected server.
- **Fleet**: the Fleet panel's "Disable unused" action plans the same change across all servers, the active server, or a hand-picked subset, previews exactly which tools it will disable per server, and applies everything with one stack write and one reload.

Both paths refuse to empty a server's whitelist. A server whose every exposed tool is unused is skipped and reported, because an empty whitelist would re-expose everything.

## Tool hints (annotations)

MCP servers may annotate tools with `readOnlyHint`, `destructiveHint`, `idempotentHint`, and `openWorldHint`. The list shows compact chips (RO, RW, DESTR, SAFE, IDEM, OPEN, CLOSED) for declared hints, and the detail rail spells them out under Hints.

Treat these as claims, not guarantees: they are reported by the server and not verified by gridctl. Per the MCP spec, a tool that declares nothing should be treated as potentially destructive and open-world; the Hints section says so explicitly for unannotated tools.

## Fleet, Access, and Groups

- **Fleet**: bulk actions across servers - expose all, hide tools matching a glob pattern, or disable unused. Every action follows plan, then confirm, then a single reload.
- **Access**: per-client scoping. Without a `clients:` block in `stack.yaml`, every linked client reaches every server; creating one flips unlisted clients to deny-by-default, and the editor warns before you do that. Server grants and per-server tool selections are both editable here; the same tool axis is available in the Stack workspace's Access Lens.
- **Groups**: curated tool bundles served at `/groups/{name}/mcp`, configured via the `groups:` block. The Groups panel teaches the configuration when none exists yet.

Where they overlap: whitelists (this workspace) decide what the gateway exposes at all; Access decides which client sees which servers and tools; Groups publish named subsets at separate endpoints. A tool must survive all applicable layers for a client to call it.

## Related

- [Configuration Reference](config-schema.md) - `tools:`, `clients:`, and `groups:` blocks
- [Usage Observability](usage-observability.md) - the usage metrics behind the Tools data and `gridctl optimize`
- [REST API Reference](api-reference.md) - the endpoints backing this workspace
