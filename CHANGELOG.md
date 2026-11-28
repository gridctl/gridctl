# Changelog

All notable changes to gridctl will be documented in this file.

## [Unreleased]

### Features

- The web UI Metrics tables show calls and export as CSV. The Tools table adds sortable Calls, Avg/call, and Last used columns (relative age, with the local date and time on hover). The Servers table adds the same three columns, summed from per-tool usage, plus a token column for the selected range headed with its window ("Last 1h tokens"), and a caption under the table names that column as the only one that follows the range control; every other number stays a session total. A server's Avg/call comes from per-tool usage and can differ slightly from Total divided by Calls. Values that are not loaded yet show a dash, never 0. The Clients table and the Overview previews keep Name, Input, Output, and Total; Clients have no call columns. An Export CSV button on the Clients, Servers, and Tools tables, in the workspace and the detached Metrics window, downloads the rows as displayed (current sort, and for Tools the current filter) as `gridctl-metrics-<kind>-<timestamp>.csv` with headers `server,tool,calls,input_tokens,output_tokens,total_tokens,avg_tokens_per_call,last_used` (Tools), `server,calls,window,window_tokens,input_tokens,output_tokens,total_tokens,avg_tokens_per_call,last_used` (Servers), and `client,input_tokens,output_tokens,total_tokens` (Clients). Names that begin with `=`, `+`, `-`, or `@` are prefixed with a single quote so spreadsheets do not read them as formulas. Keyboard selection in the breakdown tables now scrolls the selected row into view.
- More web UI surfaces fit narrow windows and browser zoom without clipping. When the Library header row is too narrow for every action (below 850 px of row, or 990 px while a "Sync sources" update pill shows), Import, Global Context, Sync sources, Refresh, and the popout move into a "More actions" menu beside New Skill; the menu works from the keyboard (arrow keys, Home, End, Escape, and Tab), and in a narrower row the actions wrap under the kind switch. The Metrics KPI cards show two per row in a narrow pane and four from 42 rem of pane width, in the main window and the detached Metrics window. The wizard's type picker, template grid, and stack resource presets reflow into fewer columns, and the skill browse step and catalog picker show the list above the preview when narrow. Toasts are never wider than the window minus a margin, long messages wrap, and in a window under 640 px wide the toast stack takes at most a third of the height and scrolls with the newest toast in view. The minimum supported window width is now 480 px: below it the workspace area shows "Window too narrow" with a "Continue anyway" button (the header and status bar stay usable), widening the window removes it, continuing keeps it hidden until the page reloads, and it never covers an open dialog.
- The web UI Logs view shows when it has stopped following the newest line. After scrolling up (or moving the keyboard cursor in a list that overflows), the toolbar pill reads "Live · not following" instead of "Live", and a floating button over the bottom of the stream reads "Jump to latest", or "N new · Jump to latest" once new lines arrive ("N+ new" when the line you left has rolled out of the window). Clicking the button, pressing End, or pressing `G` scrolls to the newest line and resumes following; scrolling back to the bottom still resumes too. In the Logs view End now jumps to the latest line and follows, where it previously selected the last row and stopped following. While paused the pill still reads "Paused". This applies to the Logs workspace and the detached logs window; the keyboard hint strip and shortcuts list gain the new keys.
- The web UI workspace rails now fit narrow windows and have a visible toggle. Below 900 px of workspace width the right (detail) rail opens as an overlay drawer over the center pane instead of a docked column, and below 640 px the left (navigation) rail does too. Selecting a row opens the detail drawer, choosing an item in the navigation drawer closes it, and Escape, the rail's toggle, or `[` and `]` close an open drawer. Each rail has a chevron toggle on the center pane's edge at every width ("Collapse left rail", "Expand right rail", and so on) that collapses or expands a docked rail. Layouts saved in a wide window are kept unchanged by a narrow session. This applies to Tools, Variables, Metrics, Library, Pins, Logs, and Connections; Stack and Traces are unchanged.
- The web UI Tools workspace can call a tool. The detail rail has a collapsed "Try it" section that builds arguments from a form generated from the tool's input schema or from raw JSON, checks them against a documented subset of JSON Schema before sending (and says when the schema goes beyond that subset), and sends them through `POST /api/tools/call` with the caller label `ui`, so every gateway gate applies. Tools not declared read-only or non-destructive ask for confirmation first. Run is disabled with the reason shown when the saved allow-list hides the tool, a policy verdict blocks it, or the gateway credential awaits re-entry; unsaved allow-list edits only add a note. Results render as plain text with the gateway's denial or tool error explained, are capped at 20,000 characters until "Show all", and can be copied in full. A call can be cancelled with Cancel or Escape. The last five argument sets per tool are kept in memory only. `POST /api/tools/call` responses gain an additive `trace_id` field when the call's root span was recorded, and the result links to that trace (or to Traces filtered to the tool when it is absent). `gridctl call` output is unchanged.
- The web UI Tools workspace detail rail is easier to read and links to the rest of the UI. A description longer than 480 characters or eight lines is shortened, followed by "Show more (N more characters)" with the exact number of hidden characters, and "Show less" collapses it again; descriptions stay plain text and are never rendered as markdown. A Parameters table lists each top-level parameter with its type, a `required` marker, its description, and `Default:` and `One of:` lines, with notes when the schema has unions, references, nested objects, or additional properties. The JSON schema moved into a "Raw schema" section that starts collapsed when the table can summarize it, with a Copy button that works while it is closed. The header adds Copy name for the prefixed `server__tool` name, a pin chip (Pinned, Server drift, Approved, or Not in pin set) that opens the server in Pins and stays hidden for a tool the saved allow-list excludes, a Traces link filtered to the tool, and a Metrics link once the tool has recorded calls. These links ask before discarding unsaved allow-list edits. A Legend button beside the list's filters explains every hint and policy chip. The list's count line now reads "All 107 tools exposed (no allow-list)", "12 of 107 exposed", or "13 of 107 selected (unsaved)" instead of "N of M enabled" with the "empty means all tools exposed" note, so a server with no allow-list is no longer confused with one whose allow-list includes every tool; the Stack sidebar's Tools editor uses the same wording.
- The web UI Runs tab now reads like the Traces tab beside it. The header is one status line ("Recording to <path>" with the stored size and file count, "Recording is off" with any size still on disk, or "Recording is on, but records are not being saved") instead of the recording note paragraph and the "Writer:" and "History:" labels; lost records read as "3 records not saved (queue_full: 3)" instead of "Loss queue_full: 3", and partial reads say "Some records could not be read". Refresh, Export, and Enable or Disable recording sit in a compact action row. Retention, Omit caller labels, and Wipe history moved into a "Recording settings" popover, and the recording, privacy, and retention notes into an "About run records" popover. Rows show the local date and time (with the ISO timestamp as the tooltip) instead of the raw timestamp, a formatted duration ("1.50s") instead of raw milliseconds, and the disposition as a colored badge. The run detail lists every recorded field in grouped tables (outcome, target, timing, IDs, caller, and record), where it previously showed seven fields; each ID has a copy button, a loaded parent or previous run opens on click, and a close button and Escape close it. Closing it from inside the detail returns focus to the run list, and opening a parent, previous, or nested run keeps focus in the detail. A run with a sampled trace offers "View trace", which opens the trace in the Traces tab (or its "Trace no longer in buffer" state when the in-memory buffer has evicted it), and "View logs"; the trace waterfall gains "View runs", which opens the Runs tab filtered by that trace ID. With recording off and nothing on disk, the tab explains what a run record is and how it differs from metrics and traces, and offers "Enable recording"; filtered, still-empty, and other empty cases have their own titles and actions. The workspace header reads "N run records loaded" instead of "N dispatch records". No REST, CLI, or stack file behavior changed.
- The web UI Runs tab has a shorter filter row. Server, Disposition, Tool, Client, and a Time range menu (Any time, Last 15 minutes, Last hour, Last 24 hours, Last 7 days, or Custom range) are always visible. Requested name, Access label, and the Attempt, Parent, Root, Previous, and Trace IDs moved into a "More filters" popover whose button counts the active ones, and each active hidden filter shows as a chip with a remove button. Custom range shows From and To date and time inputs in local time and notes when From is after To; the free-text RFC3339 Since and Until fields are gone. Text filters say they match exactly and send one request after a 300 ms pause, on Enter, or on blur, instead of one per keystroke; an unchanged value sends nothing. "Clear filters" resets every filter with one reload. The URL now carries the filters and the selected run, so a filtered view survives a reload and can be shared; a relative range is resolved again when the link is opened, and a selected run that is not in the results shows a notice with "Clear selection". Load older and Export reuse the parameters of the rows on screen, including the resolved start of a relative range. The Traces and Runs tabs no longer share URL keys: switching tabs drops the other view's keys, so a Traces server filter or selected trace no longer leaks into Runs, or the reverse. `/traces?view=runs&server=<name>` links keep working. The REST API is unchanged.
- The web UI Stack workspace keeps the selected node in the URL and remembers dragged node positions. `/stack?node=<id>` (for example `?node=mcp-github`, `?node=client-<slug>`, or `?node=gateway`) opens the Stack with that node selected and the inspector open once the gateway responds, so a reload keeps the selection and inspector and a copied link points at one server. Selecting, switching, and deselecting nodes updates the parameter in place without adding history entries, so Back still leaves the Stack in one step, and other parameters such as `?spec=1` are kept. A link to a node that is not in the stack shows one "Node \"<id>\" not found" error toast and the parameter is removed: after five seconds for a well-formed id, so clients that load later still resolve, and at once for a malformed one. Nodes you drag now stay where you left them after a reload. Positions are saved per stack in this browser's local storage (node ids and coordinates only), and Reset layout or switching between compact and full cards clears them for that stack.
- The web UI Stack workspace has a toolbar and a table view. The toolbar holds a name filter ("Filter by name"; `/` focuses it outside fields and dialogs, and Escape clears it, then leaves it), a "Needs attention" chip, a Status menu with Running, Error, Restarting, Needs authorization, Initializing, Idle, Stopped, Disabled, Linked, Pin drift, and Pin blocked facets and their counts, a "matched of total" count while a filter is active, and a Graph and Table view toggle. Facets combine with OR and the name filter with AND. On the graph the filter fades non-matching MCP servers, resources, and clients, their tool nodes, and their edges without moving the viewport, and Enter in the filter frames the matches. The table lists every MCP server, resource, and linked client with Name, Kind, Status, Transport, Tools, Replicas, and Pin columns, sorted with servers needing attention first; every header sorts. Clients read "Linked" and are not counted as running. Clicking a row, or Enter on it, opens the same inspector as clicking the node on the graph, and switching views keeps the selection and the inspector. The table is one tab stop, and Down Arrow in the filter moves into it: the arrow keys, `j`, `k`, Home, End, Page Up, and Page Down move between rows, and with the inspector open they also change the inspected item. "Inspect gateway" above the table opens the gateway. With more than 24 MCP servers at first load the workspace opens on the table until you pick a view; the choice is remembered across reloads. Returning from the table to the graph re-frames it and keeps dragged positions. Access Lens always uses the graph, and the Table option is disabled while it is on. The palette adds "Show Stack as table" and "Show Stack as graph".
- The web UI command palette now reaches every workspace and puts the best match first. Every workspace has a "Go to" command with its icon and Cmd/Ctrl+1-9 shortcut; "Open Traces", "Open Logs", "Open Metrics", and "Open Variables" are now "Go to Traces", "Go to Logs", "Go to Metrics", and "Go to Variables". New commands open Tools' usage audit and risk filter, Metrics' overview, clients, servers, and tools views, Traces or Runs, Connections' clients needing attention, and Variables that are not referenced, and each of those workspaces now has its own Current Context group. Refresh is available in every workspace, and Reload Config (the same flow as the header, including the change preview) and Toggle compact mode are new. Search ranks by match quality before usage: an exact name ("tools", "logs", "vault", or "variables") comes first, then prefix, word start, and keyword matches, and usage only orders commands within the same match quality. The set of results for a query is unchanged. Skills, variables, and traces are no longer capped at 20, 30, and 20 entries; a search lists the first 50 matches and says how many more there are. `l:` scopes to Library (`r:` still works), the hints name each prefix, Tab reaches the scope chip's clear button, and Backspace on a bare prefix clears it. A `v:` or `t:` search before variables or traces have loaded says which workspace to open, above any matching Variables or Traces commands. The current workspace's Cmd/Ctrl+1-9 shortcut or Go to command keeps its URL instead of resetting it. Choosing a trace from the palette while Traces is open now selects it.
- The web UI header keeps workspace labels readable and groups its secondary actions. The active workspace pill always shows its label and is scrolled into view when the workspace changes; the other pills show their labels whenever the header's left side has room (from about 1250 px with the Persistence label hidden). Every pill and header control is at least 32 by 32 px. Refresh, Reload Config, and Forget credential moved into a "More actions" menu at the right end of the header, and its trigger shows a spinning loader while a refresh or reload runs. Reload Config has its own icon, so it is no longer confused with Refresh. Create new is now the header's filled primary button and shows its label from 1360 px. A new Help menu opens Keyboard shortcuts and the documentation. The Persistence label is visually hidden below 1440 px. Forget credential, from the header or the command palette ("Forget gateway credential…"), now asks for confirmation before it removes the saved credential from the browser; the gateway and the credential itself are unaffected.
- The Create new wizard's Review step now states what its primary button changes before the click. It reads "Ready to deploy" or "Fix errors before deploying" for a server or resource and "Ready to save" or "Fix errors before saving" for a stack, instead of "generate". For a server or resource, a "What Deploy changes" section names the stack file Deploy writes, says whether the gateway is watching that file (and so reloads and tries to start the server or resource after the write) or is not watching it (and nothing starts until Reload Config or `gridctl reload`), and shows a diff of the file before and after with added and rewritten line counts. The append re-serializes the whole file, so the diff can show blank-line and indentation changes elsewhere; the section says so when it does. When the post-append stack would be invalid, the section lists the issues. For a stack, a "What Save & Load changes" section names the file written to the stack library and says whether it will be loaded. A new read-only endpoint, `POST /api/stack/append/preview`, returns the file before and after the append through the same code path as the write, and `POST /api/stack/append` gains additive `path` and `watching` response fields. When the append reports that the gateway is not watching the stack file, the wizard no longer polls for a server that cannot start: it shows a warning toast asking for Reload Config and closes.
- A hook held for pin approval now shows up where operators already look in the web UI. The Pins workspace has a Hooks tab (`/pins?kind=hook`, `&hook=<name>`) that lists pinned hooks with drifted ones first, honors the Attention filter, and opens a detail with the hook's origin, verification and first-pinned times, approval history, the pin review (status, findings, changed files, the `hook.json` line diff, reason, evidence references, and Approve), and the pinned file digests. The status bar pin badge counts drifted hooks (`Pins: 1 drifted (1 hook)`) and links to the first one, and a warning toast (`Pin drift detected on 1 hook`) with a View action appears when hook drift appears. A Library hook card whose pin is drifted shows a "Pin drift" chip that opens the review. The pack update toast that reports held hooks has a Review action, and the pack import wizard's done step has a "Review held hook" button, both opening the first held hook's review. The rail, badge, toast, and chip read the stored pin status from `GET /api/hook-pins`; the review verifies live and its status is authoritative. This also fixes the hook Pin tab in the Library, which kept showing the pre-approval diff after Approve until the hook was reselected: it now reloads and shows the pinned state, a rejected approve because the content changed reloads the diff for re-review, and a refused reason or evidence reference shows the server's message and keeps the typed input. No REST shape changed.
- The Stack canvas now wraps MCP servers into columns and puts servers that need attention first. When a stack has enough servers that one column would shrink the whole-graph fit, servers fill up to six columns top to bottom, then left to right, and resources move right of the last column. With a linked client, compact cards wrap from 10 servers (two columns at 20 and four at 60), and full cards from four, because full cards are much taller. Without a client, compact cards wrap from nine. Compact stacks below that keep their current positions. Servers are ordered unhealthy, restarting, or pin-blocked first, then needing authorization, then with schema drift, then the rest, each group in name order, so a broken server sits at the top of the column nearest the gateway and moves there on the next poll without refitting the view. A disabled server stays with the rest. Tab order follows the same order. Expanded tools fan out in one column right of every server column. Full cards are now spaced by their actual content, so cards with a health error, authorization notice, or drift row no longer overlap the card below, and a card that gains or loses a row moves only the cards below it in its own column. This also fixes drift and blocked rows disappearing from full cards after Reset layout or the compact/full card toggle until the next pins refresh.
- The web UI spec pane is now a resizable column beside the Stack graph instead of an overlay on top of it, so the graph refits to the narrower canvas and stays visible. It opens at 560 px and can be dragged between 360 and 960 px, never wider than 60% of the workspace (on a workspace narrower than 900 px the lower bound is 40% of its width). Closing and reopening the pane keeps the dragged width until you leave the Stack workspace or reload the page; the width is not saved. Only one right-side panel shows at a time: opening the spec closes the inspector and the Access Lens editor, and selecting a node or opening the access editor closes the spec. The pane lists validation issues above the YAML, errors first, each with its severity as a word ("Error", "Warning", or "Info"), the field path, the message, and its line when known; clicking a located issue scrolls to that line and highlights it. Lines are now found from the field path in the displayed YAML (for example `mcp-servers[3].network` points at that server's `network` key), which fixes issues being drawn on the line matching an array index. The dot on every valid line is gone, and lines with issues show a severity icon whose name gives the count and severities. When a background refresh fails after the spec has loaded, a banner says whether the spec or the validation could not be refreshed, keeps the last copy on screen, and offers Retry. Escape closes the pane unless a field has focus or the spec diff is open, and focus moves into the pane when it opens. The palette command "Open Spec Editor" is now "Open spec".

### Changed

- Optimize findings on the web UI Metrics Overview now explain themselves without hover, and telemetry persistence indicators agree when per-server overrides are set. Each actionable finding shows its full summary directly under the title, formats its weekly impact as an estimate (`~2.3M tok/wk`) with a unit line explaining what that figure measures (schema overhead versus projected output-format savings, distinct from recorded usage), adds a "Show fix" disclosure for its remediation snippet, and links schema-related findings to the Tools workspace in Audit Mode for that server. The overflow note when more than three findings exist is now a button that opens the gateway sidebar's Optimize section on Stack, and info-only summaries render in full without a tooltip. When stack-global persistence is off while an enabled server persists a signal through a per-server override, the header chip now reads "Persistence: Per-server" with active styling instead of "Persistence: Off" (overrides on disabled servers do not turn the chip on), each popover row notes how many enabled servers diverge from the stack-global setting, and the aggregated "persisted from" divider appends the count of persisting servers (for example `· 1 server only`).
- Collapsed web UI log rows now show up to four of the entry's attributes after the message, so a row reads without expanding it. A tool call row shows the tool name, a compact duration ("1.2ms", "30.00s"), the outcome as the word `ok` or `error`, and the client; a failed call shows its error text in the error color; a `server stderr` row shows the output line. Other rows show `name`, `reason`, `status`, `method`, or `path` when present. `server` (already the source column) and `replica_id` are never shown, and attributes appear in a fixed priority order so the duration and outcome are kept first. Hovering one shows the full `key=value`, a search match inside one is highlighted, and clicking one expands the row as before. With wrap off the message keeps priority and attributes that do not fit are clipped; with wrap on they wrap after the message. The expand panel, filtering, copy, and export are unchanged. This applies to the Logs workspace and the detached logs window.
- Every web UI workspace now shows its title as a readable heading instead of a small faded uppercase label or a screen-reader-only heading. Stack, Library, Variables, Tools, Metrics, Pins, Logs, Traces, and Connections share one header layout: the title, an optional view switcher, a one-line summary in 12 px text, and the workspace's actions aligned right, with actions wrapping to a second line when the row is too narrow. The Traces and Runs switch is a bordered segmented control with "Traces" and "Runs" labels, so Runs reads as a control, and the Library kind switch uses the same segment styling. Stack gains a slim header above the canvas and inspector with its MCP server and tool counts. Connections shows its header in the loading and failed states too, and every Library segment keeps the same header padding when compact mode is on.

### Bug Fixes

- The web UI Metrics token charts now show unambiguous time labels, 12 px axis ticks, and an accessible table view. X-axis ticks on the 24h range include the short weekday beside the time, and ticks on the 7d range include the month and day so multi-day buckets no longer repeat; Live, 1h, and 6h ticks stay hours and minutes. Chart tooltips now show the full local date, bucket start and end time, and interval (for example `Fri, Nov 20, 2026, 09:00 AM – 10:00 AM · 1h interval`), and the server inspector sparkline uses the same range-aware axis and tooltip formatting. Axis tick text on the main chart and sparkline now renders at 12 px instead of the browser default size. Both the Metrics workspace and the detached Metrics window add a "View as table" button on the token chart header that toggles to a scrollable table with exact token counts, full date and bucket start-to-end row headers, a sticky header, and the Fleet Total column when focused on a server; "View as chart" returns to the graph. Interactive legend items in the shared `AreaChart` component now render as `<button type="button">` elements with `aria-pressed`; Metrics legends stay static text.
- The web UI Metrics workspace and its popout now report what Clear and Refresh did. A failed clear shows an error toast that begins "Clear metrics failed" and leaves every number on screen unchanged, where it previously failed silently; a successful clear shows "Metrics cleared" and refreshes the chart, KPI cards, session line, server, client, and tool tables, and rail counts together instead of leaving the tables on the wiped numbers until the next background poll. Refresh spins its icon until the series, status, and tool usage have reloaded, and the screen reader announcement ("Metrics refreshed" or "Metrics refresh failed") now comes after it settles rather than on the click. Pause on Live now also freezes the session line, tables, and rail counts, Resume refetches the series at once instead of waiting up to five seconds, and Refresh while paused updates them once and stays paused. The "Focused" chip appears only when the chart actually shows the selected server, so it no longer appears for a selected client or a server with no samples in the window.
- The web UI Logs view now confirms "Copy filtered view" with a toast naming the entry count (for example "500 entries copied"), and the palette command warns "Nothing to copy" when no entries match instead of copying an empty view. The count line reads "Showing 500 of 1,000 buffered", or "12 of 500 match · 1,000 buffered" with a filter, and its tooltip names the window size and the buffer capacity. Timestamps always use a 00 to 23 hour, show the month and day when the loaded entries span more than one local day, and carry a tooltip with the full local date, time, and timezone beside the original value. Level badges show full names (`ERROR` and `DEBUG` instead of `ERRO` and `DEBU`). Filter matches are now highlighted in the component and source cells, a row kept only by a trace ID or attribute shows a "match in trace ID" or "match in attributes" hint, and expanding such a row highlights the match in the trace ID or opens Other attributes with the match highlighted. The filter input is named "Filter logs" for assistive technology, and the control bar wraps onto a second row in narrow windows.
- The web UI Tools workspace per-server list can now be curated from the keyboard and is announced in full by screen readers. Tab from the filter input lands on the highlighted row, Space toggles that tool's exposure without opening it, Enter still opens it in the detail rail, and arrow keys, Home, and End move focus with the highlight; hovering a row no longer moves focus. The row checkboxes are no longer tab stops, so the list is a single stop. Each row is announced with the tool name, whether it is enabled or disabled, its groups, its annotation chips in full words (for example "read-only" and "non-destructive"), its policy verdict (an approval-required tool is announced as requiring approval), and its Audit state and last use, with the description as its description; it was previously announced only as "`<tool>` details". In Audit Mode, "Disable N unused" now saves the saved allow-list minus the unused tools and nothing else: other unchecked or checked rows stay as unsaved changes and the Save button keeps their count, where previously they were saved too. A Disable unused save that does not persist (a refused group reference, a changed stack file, any other error, or a network failure) restores the rows as they were before the click. The "Sort tools" select is now disabled while the filter has text, with a "Relevance order" note beside it, because filtered results are always in relevance order and the select had no effect. Lists of more than 200 tools now render only the rows near the scroll position.
- The web UI Tools search now finds every tool that contains the query, including matches more than about 40 characters into a description and exact tool names behind a long server name; previously those were silently dropped. This is a bug fix to the shared search filter, so the per-server tool filter, the Library skill search, the registry sidebar, and the tool pickers in the Create new wizard and Stack sidebar gain the same fix. Fuzzy tolerance is now stricter: a query matches with about one typo instead of two, so "click" no longer matches "slack", and three-character queries match exactly. In Tools search, tools whose name contains the query are listed before description-only matches, exact matches are highlighted in the server name, tool name, and description, and a late description match is shown from just before it. Up and Down move through the results from the search input and Enter opens the active one; the input is a combobox and the results a listbox for screen readers, with the match count announced. Opening a result no longer discards the query: the URL keeps it as `?back=`, and a "Back to results" chip returns to the same results with the picked row active. Selecting a server in the rail, typing a new query, or dismissing the chip removes it.
- The web UI Runs tab now offers Wipe history and the retention fields whenever run records exist, including while recording is disabled; Disable recording and Omit caller labels still appear only while recording is enabled. With recording disabled, the retention row notes that the change applies when recording is enabled and that retained records are not pruned while recording is off, and `GET /api/runs/status` now reports the retention the stack file would apply (or the defaults) instead of zero or a disabled recorder's earlier values. On a daemon started with recording disabled, `POST /api/runs/wipe` previously returned `success: false` and deleted nothing, and `DELETE /api/telemetry` with `signal=runs` returned 500; the bare `DELETE /api/telemetry` deleted the other signals and then returned 500. All three now delete the run files. Response shapes are unchanged, and the offline `gridctl runs wipe` still refuses while another daemon owns the stack. A failed Load older now keeps the loaded rows under a banner with the server's error message and Retry, where it previously replaced them with a fixed "Run history is unreadable" panel; Retry repeats the append, or reloads the first page after a 409 from a wipe. A failed first load shows the server's message. Save retention now requires whole numbers (1 to 3650 days and 1 to 1048576 MiB), reports invalid input beside the fields without sending a request, always sends both values, and shows "Retention saved" on success. The run list is now one tab stop with the Traces keyboard model (arrows or `j` and `k`, Home, End, Enter, and Escape) instead of a tab stop per row with no arrow keys; Enter on a focused button in the Runs tab still activates that button. Retention errors clear as soon as the field is edited.
- The web UI trace waterfall now works from the keyboard and with a screen reader. Spans are a tree with one tab stop: ArrowUp and ArrowDown (or `k` and `j`), Home, and End move between spans, ArrowLeft moves to the parent span and ArrowRight to the first child, and Enter or Space opens span detail (pressing it again closes it). These keys no longer move the trace list highlight. Each span is announced with its name, duration, and status (and "slow" when it is above the p95), along with its level and position. Spans are now listed parent-first, so a child always sits under its own parent instead of in start-time order. The span-name column handle is a tab stop named "Resize span name column" that resizes with ArrowLeft and ArrowRight in 1 percent steps and jumps to its limits with Home and End. Closing span detail with Escape or its close button, including from the drawer's resize handle, returns focus to the span's row, and closing the waterfall with focus inside it returns focus to the trace list. The span detail content is focusable so it scrolls with the keyboard, and the waterfall header and span detail buttons are now at least 24 by 24 px. In a waterfall narrower than 480 px, View logs and View metrics show only their icons and keep their names for assistive technology.
- Literal secrets typed into the Create new wizard are no longer shown or kept in clear text. A literal bearer token, header value, OAuth client secret, gateway token, or credential-named environment or build argument value (one whose key contains TOKEN, PASSWORD, SECRET, API_KEY, AUTH, or the rest of the `gridctl export` list) is masked in its input with a Show value toggle, while `${var:KEY}` and other references stay readable. Each masked field warns that the literal will be written to the stack file in clear text and offers "Store as variable", which saves the value as a secret variable under a name you choose, replaces the field with `${var:KEY}`, and refuses a name that already exists instead of overwriting it. The YAML preview and the Review step's "Exact server YAML" mask literal secrets until "Show secrets" is pressed; the masked text keeps every other line and character as written, and validation, deploy, copy, and download still use the real values. The wizard's copy in browser session storage and saved drafts now omit literal secrets (the Save draft toast says how many were left out), and form-mode YAML is no longer stored in the session at all because it is regenerated from the form. Reloading the page no longer reopens the wizard, and this includes the automatic reload after a UI update; choosing "Create new" resumes the previous form without its literals and shows a notice when any were dropped. Draft files are now written with mode `0600` in a `0700` directory, and an existing drafts directory is tightened to `0700` on the next save. Drafts saved by an earlier release are not rewritten; delete any that held a literal secret.
- The web UI Traces workspace now always shows the waterfall of the selected trace. Previously, selecting one trace and then another quickly could leave the first trace's waterfall under the second trace's row, URL, and header, so View logs and Export acted on the wrong trace. When the list holds fewer traces than the buffer, the count line and a footer below the list say so ("newest 100 of 742 in buffer", or "newest 100 loaded" under a server, errors, or minimum duration filter), state that search and time range apply to the loaded traces only, and offer Load more, which raises the request limit by 100 up to the buffer capacity. A changed server-side filter starts again from 100. Pasting a full 32-character trace ID that no loaded row has offers "Open trace", which opens it when it is still in the buffer. Status is now the first column of the trace list, so it stays visible beside the waterfall; compact rows show a shaped glyph named for the status. In the waterfall, spans with no server attribute appear in the legend as `gateway`, a root span named `tools/call` shows its tool (`tools/call <tool>`), and error and slow spans carry an error icon and a `slow` tag beside the name instead of relying on color alone. The server filter, the search input, the trace error icon, and the span ID copy button now have accessible names. The REST API is unchanged.
- A connected gateway with no MCP servers and no resources now shows a call to action on the Stack canvas. Previously the canvas showed only the gateway node, because the "Create your first stack" card appeared only before the first status arrived, underneath the loading overlay. The card now sits to the right of the gateway node, follows it when the canvas is panned or the node is dragged, and keeps its size on zoom. Its "Create new" button opens the creation wizard the same way the header button does, matching the "Create new" label used by every other wizard entry point (it previously read "New Stack"). With no stack loaded the card reads "No stack loaded"; with an empty stack loaded it reads "This stack is empty" and also offers "MCP Server" and "Resource" quick-add links. The card is hidden while the Stack is loading or showing a connection error, and once the stack has an MCP server or a resource. Dragging, scrolling, or double-clicking on the card does not pan or zoom the canvas.
- Compact Stack canvas cards no longer rely on color alone to report state. Every non-running state (needs authorization, error, restarting, initializing, stopped, and idle) and a disabled server, schema drift, and a blocked schema pin now show an icon beside the server or resource name, each with a name for screen readers. A running server with no pin state looks as before. Hovering the error or restarting icon shows the health error or restart attempt, and the drift icon opens the server's drift view in Pins. A disabled server shows only the disabled icon, never a needs authorization or error icon, and its dot is now a hollow square. Status dots have a distinct shape per status (filled circle, hollow circle, half circle, square, diamond, hollow diamond, triangle, and hollow square), and a dot drawn with animation turned off no longer blinks. A new Legend control in the canvas control panel lists every dot shape, card icon, and header counter, and the compact tool counter (`3t`) now has a tooltip. Selecting a server, resource, or the gateway no longer dims the rest of the graph, so neighbors stay comparable; selecting a client and Access Lens still dim what is out of reach. The autoscale scale-up ring now pulses as intended without hiding the card's selection ring, and the autoscale counter shows an up or down arrow and says "scaling up" or "scaling down" in its tooltip.
- The Stack canvas now keeps the zoom and position you choose. After you scroll to zoom, drag the canvas, or use Zoom in or Zoom out, opening, closing, or resizing the inspector, selecting a node or client, expanding or collapsing a server's tools, and a server added by hot reload no longer reframe the whole graph. Expanding a server whose tools are off screen pans just enough to show them at the current zoom, zooming out only when they cannot fit, and collapsing moves nothing. Fit view (button, `f`, Cmd/Ctrl+0, or the palette's "Zoom to fit"), Reset layout, and the compact/full card toggle frame the whole graph and turn automatic fitting back on. A plain click on blank canvas still counts as not moving it, so automatic fitting stays on until you actually pan or zoom. A blank-canvas click now only clears the selection and closes the inspector: expanded tool fan-outs stay expanded and the view does not move. Selecting a node from outside the canvas (the palette's "Inspect", the needs-auth toast's "Authorize", the auth-pending badge, the gateway sidebar, the Variables workspace, Tools' "Keep editing", and Access Lens restoring a client) now moves the selection ring to that node and pans it into view when it is off screen. With `prefers-reduced-motion: reduce`, canvas and tool detail viewport moves are instant.
- Per-server telemetry persistence in the Stack inspector is now set directly. Each of Logs, Metrics, and Traces shows Inherit, On, and Off as separate buttons, and choosing one writes exactly that state to the stack file in one request. Previously each row cycled through Inherit, On, and Off, so turning a signal Off from Inherit first wrote On, which started persisting that signal for the server before the second click turned it off, and the data written in between stayed on disk. The row also kept showing the old state for up to 15 seconds after a change, so a second click repeated the first request. The chosen state now shows right away, including after "Reset to global", and each row still shows whether the global default is on or off. While a change is saving, the Inherit, On, and Off buttons and Reset to global are disabled.
- The Stack inspector's Authorization section is now open whenever a server that needs authorization is selected, including after inspecting another OAuth server and when the selected server changes from authorized to needing authorization; completing an authorization no longer collapses it. The status bar "Auth: N pending" chip and the "Authorize" action on the needs-auth toast now open Stack from any workspace with the server selected and the inspector open, asking first when Tools has unsaved changes, and a stale "Authorize" for a server that left the stack selects nothing and shows "`<name>` is no longer in the stack". When the selected node leaves the graph, the inspector shows a closable "not in the stack" message instead of an empty rail and the canvas is no longer dimmed. If the node does not return within 20 seconds, the selection is cleared, the inspector closes, and a warning toast names it. The delay exists because a restarting server is briefly absent from status, so a server that comes back from a restart keeps its selection.
- The Stack inspector offers Restart only for an enabled MCP server. Resources and disabled servers no longer show a Restart button that always failed, and View Logs moves up in its place. Restart is now a secondary button: when the gateway has active sessions it first asks for confirmation, naming the server, the gateway session count, and that in-flight calls to that server will fail. A successful restart shows a toast and refreshes the canvas right away, and a restart that fails after another node was selected reports the failure in a toast. Restart, Enable, and Disable no longer carry an error, hint, or busy state from one selected node to the next, and a server whose restart is still running shows Restarting... when selected again instead of offering a second restart. A disabled server now reads disabled on the canvas header dot (a hollow square), in a single disabled badge in full cards, and in the inspector State row, instead of initializing.
- The Create new wizard now works with a keyboard and a screen reader. Section headers, stack item headers, the findings toggle, and the draft Save and Drafts buttons report whether they are expanded. Form and YAML, and Templates and Catalog, are labeled groups whose buttons report which one is selected, and the selected segment has a border and heavier text so it stands out without color. Server type, template, resource type, and catalog cards report their selection, and the selected server type card shows a check mark. With no active stack, the MCP Server and Resource cards stay focusable, are announced as unavailable, and show "Requires an active stack. Create a Stack first." as readable text instead of a hover tooltip. The header reads "Step 2 of 4: Template" instead of an unlabeled row of dots, and focus moves to it on each step change. The dialog is titled "Create new". Skill and pack imports no longer show the header stepper over their own; the skill import's indicator is an ordered list that marks the current step, and focus follows each import step and lands on the result heading. A finished skill import now offers "View in Library" and "Import another" (or "Back to selection" and "Close" when nothing was imported), and after a finished skill or pack import the next "Create new" opens on the type step however the wizard was closed. A failed catalog load shows an alert with Retry instead of "0 servers", and an empty catalog reads "No servers in the catalog" instead of quoting an empty search. Escape or a click outside closes an open draft popover without closing the wizard and returns focus to its button. A failed tool probe toasts the actual error instead of "Probe failed", and a probe that needs authorization no longer toasts.
- Six correctness fixes in the Create new wizard. Changing an MCP server's type now replaces a transport the new type does not offer with that type's default, so switching a stdio container to External URL stores and emits `transport: http` instead of showing HTTP while the YAML kept `transport: stdio`; a stored transport the type does not offer (from a draft or YAML mode) is now shown in the select as "not valid for this type" rather than hidden behind another value. Labels, datalists, and error descriptions in server and stack forms now use per-form ids, so with several servers in a stack a label such as "Skip TLS Verification" acts on its own server instead of the first one. After a successful deploy or stack save, the next "Create new" starts on the type step with an empty form instead of reopening Review for the resource just deployed; closing without deploying still keeps work in progress. The form and YAML preview panels are sized in percent again, so the form panel can no longer shrink to 40 pixels. The skill import source check now accepts any `scheme://` URL (nested GitLab groups, ports, `ssh://`, Azure DevOps, and trailing slashes), `git@host:path`, and absolute local paths, and a trailing slash no longer produces an empty source name in the preview request. The stack's gateway tracing control is now a "Tracing enabled" select with Default (on), On, and Off, so tracing can be turned off (`enabled: false`).
- Deploying an MCP server from the Create new wizard's Review step no longer keeps running after leaving Review. Previously the registration check kept requesting status for up to five minutes after Back, Close, Escape, the backdrop, or Start over, and when it finished it showed a toast and could close a wizard opened later. Leaving Review now stops the check. The wizard also remembers the server it added: returning to Review with unchanged YAML, including after closing and reopening the wizard or reloading the tab, offers "Check deployment" instead of sending the same server again (which the gateway refused as a duplicate name). With changed YAML, Review names the server already added and notes that another deploy needs a different name. Container, source, external, local, SSH, and OpenAPI servers now all show a status line while deploying ("Starting container and connecting server...", "Building image, starting container, and connecting server...", or "Connecting server..."), "Deployment is still in progress" when the check gives up, and a persistent alert with a link to the server's filtered logs on failure; previously only Python source servers did. A failed resource append shows the same alert. While the stack update request is pending, Deploy reads "Adding to stack..." and is disabled together with Back and Start over, including in a Review step reopened during the request; Close, Escape, and the backdrop still work.
- The Create new wizard's Configure step now flags empty required fields and duplicate names before Review. For an MCP server, resource, or stack, Next stays in place while a required field is empty or a name collides, and the reason beside it names what is missing (`Name is required`, `3 fields need attention: Name, Image, Port`, or `MCP server 2: Image is required`). A field shows its inline error once it loses focus, and clicking the blocked Next shows every error, expands the section or stack item that holds one, and focuses the first invalid field. A new MCP server or resource named like an MCP server or resource in the running stack's status is flagged on the Name field; a name that is declared in the stack file but absent from status is not caught here and still fails at Deploy. Inside a stack, two MCP servers or two resources sharing a name, or a resource named like an MCP server, are flagged on the later item. The server's validation is unchanged and remains authoritative; YAML mode is not gated by these checks.
- Hook review and pack detail in the web UI now handle hooks the way they handle skills and agents. Unsync in a hook's Projection tab previously removed the hook from every client in one click and reported "Hook sync finished"; it now asks for confirmation, names the clients whose entries and script copies it removes, names detected clients that read the Claude Code projection (such as Cursor) as stopping too, says that a drifted projection is kept until the drift is reviewed, and reports the result as an unsync (`Unsynced block-force-push from 2 clients`, or the refused client and its reason). Unsync is disabled with a tooltip when the hook is not projected or every projection has drifted. The `hook.json` diff in the Pin tab is taller (up to 384 px) and scrolls inside. When scripts were added or modified, the Pin tab now says that script contents are not shown and names the hook's registry directory to read them in; script contents are still not shown in hook pin review. In pack detail, hook rows now appear under Hooks after Rules instead of last under a raw `hook` label, and a hook's name links to it in the Library (a hook missing from the registry stays plain text). The pack card counts hooks (`1 hook`), and the pack remove dialog's preview now lists hooks, which it previously left out. No REST shape changed.
- Every approval in the web UI now follows one confirmation rule. Approving server, skill, or context pin drift, or a proposal, asks for confirmation when the change carries a warn or critical scan finding, when it is large (five or more changes, or 50 or more changed lines), or when the diff shown is truncated; the confirmation names each reason, and for a truncated diff the CLI command that prints the full diff. Previously only server drift confirmed, and only for five or more changes or a finding; skill, context, and proposal approvals were one click. A small change with no warn or critical finding still approves in one click, and info-only findings still only require a reason. The SKILL.md diff past 2,000 lines now shows lines from both the pinned and the current document with a count of hidden lines, where it previously could show only removed lines. The proposal approve button now reads "Approve and apply" on the apply path and "Record approval" on the record path. A Pins link to a server, skill, or context fragment that is no longer pinned (`/pins?server=<name>`, `?kind=skill&skill=<name>`, or `?kind=context&fragment=<name>`) now shows a "`<name>` is no longer pinned" warning and moves the URL to the item actually shown, instead of silently opening the first item under the stale name; resetting a pin from the workspace does not raise this warning.
- Trust actions in the web UI now show what they act on before they can be taken. The pack update dialog and the pack import wizard list each security finding under its resource (`kind/name`, its source, and whether it blocks the import) with its description, location, and severity color, in a scrolling block above the trust checkbox, instead of one sentence naming the flagged resources; this includes the findings returned when an update is refused. The pack import scanner's regex is labeled as a matched rule and is never shown as if it were text found in the pack. In the Global Context dialog's drift review, Adopt into canon, Overwrite client, Adopt, Capture, and Force overwrite stay disabled with a tooltip until the diff loads, and Cancel stays enabled. The diff renders with added and removed lines colored and hidden characters escaped, and an empty diff says there are no line-level differences. A failed diff load previously appeared as the diff text with every action enabled; it now shows an alert with the error, Retry, and the `gridctl ctx` commands for that case. Selecting another drifted fragment disables the actions until its diff arrives. In the Pins workspace, each new tool in server drift now shows its description and input and output schemas from the tool catalog, labeled as catalog content, or says the definition is not available. The agent and hook projection drift reviews now say that no diff is available in the UI and name the file to compare. No REST shape changed.
- The Pins workspace's Context tab now reviews fragment drift the way the Skills tab does. A drifted fragment shows one line diff of the pinned and current content with `+` and `-` lines and a changed-line count, instead of two separately scrolling panes. When the documents are too large to align, the heading says so and a note explains that the pinned text is shown as removed followed by the current text as added; in that case, or when the diff is cut off at its render limit, the full current document is available below the diff. The SKILL.md diff in the Skills tab shows the same heading and note for documents too large to align. While the diff loads the tab shows "Loading diff…" with Approve disabled, where it previously showed the pinned text under the "Current document" heading; a failed load shows an alert with the error and a Retry button. An approve rejected because the content changed since the reviewed diff now reloads the diff for re-review, and the previous diff is not shown during the reload. The detail now shows when the pin was last verified and first pinned, its approval history, and an evidence references field that is sent with the approval. Reset moved from the header to a Danger section below the content, and its confirm says that the next context sync re-pins whatever content is on disk without a diff and that this cannot be undone; a failed reset shows an error toast. `/pins?kind=context&fragment=<name>&view=drift` and `&view=findings` now scroll to the drift section (or, for a fragment without drift, to its findings and pinned document), and Ctrl+Enter or Cmd+Enter and the palette commands "Pins: Approve Current Drift" and "Pins: Reset Selected Pin" (renamed from "Pins: Reset Pins for Selected Server") act on the selected fragment. The pin review opened from the Global Context dialog shows the same detail and updates its status, history, and title after an approve made inside it.
- Pin drift and findings toasts in the web UI no longer reappear on every page load, never expire, or cover dialogs. Previously each load raised a toast for drift and findings that already existed (up to six), the toasts stayed until dismissed even though they were meant to last six seconds, and they painted over open dialogs such as the Create new wizard's footer buttons. A second item drifting or flagged while another was already affected raised nothing, and View opened the alphabetically first affected item instead of the new one. Now only items that become drifted or flagged during the session raise a toast: one drift toast and one findings toast per poll at most, naming the item ("Pin drift detected on server github") or counting several ("New findings on 1 server and 2 skills"), with View closing the toast and opening the first newly affected item. An item that recovers and drifts again toasts again. These toasts close after 10 seconds unless hovered or focused, and while a dialog or the command palette is open they are hidden with their timer paused: one raised during the dialog shows for its full 10 seconds after the last dialog closes, and one already showing when a dialog opens returns for its remaining time; the status bar badges still show the standing counts. Any toast whose `duration` is set now honors it even with an action button, so the Library sync failure toast now closes after six seconds; toasts with an action and no `duration`, and error toasts (with or without a `duration`), still stay until dismissed. A toast with longer error detail now has a Details button that shows it, instead of only a hover tooltip.
- The Create new wizard no longer drops YAML edits or discards work without asking. Previously, switching from YAML mode back to Form silently dropped every edit the form does not read (for example `openapi.baseUrl`, server `auth`, `ssh`, or a stack's `gateway` block) and put back blocks deleted in YAML. Now the switch is refused when it would drop, change, or restore anything, and an alert names up to five affected keys ("Switching to Form would drop, change, or restore: openapi.baseUrl. Stay in YAML mode and continue to Review, or undo these edits."); Review still works from YAML mode, and the alert clears when the YAML is edited. Loading a draft, choosing a different resource type, or opening the wizard on a different preset type no longer keeps the previous resource's YAML, which the editor showed and Review deployed. A draft saved in YAML mode now keeps its YAML text and reopens in YAML mode; drafts saved earlier load in Form mode as before. `GET` and `POST /api/wizard/drafts` gain an additive optional `yaml` field, omitted when empty. Start over and loading a draft now ask for confirmation when the form or YAML has unsaved content or an import has progress. Closing the wizard (backdrop, Escape, or Close) and Start over now ask before discarding a skill or pack import past its source step or with a preview or install in flight, and reloading the tab then shows the browser's leave warning. A finished import and the pack done step, including while Apply now runs, close without asking. See [Wizard Drafts](docs/api-reference.md#wizard-drafts).
- Keyboard shortcuts in the web UI now work from text fields and on Windows and Linux. The command palette (Ctrl+K or Cmd+K), workspace (Ctrl or Cmd plus 1 to 9), Logs (Ctrl+J or Cmd+J), compact mode (`Ctrl+\` or `Cmd+\`), and refresh (Ctrl+Shift+R or Cmd+Shift+R) shortcuts now fire while an input, select, or editor outside a dialog has focus; previously they did nothing there, and in Firefox Ctrl+K went to the browser. Fit, zoom, `f`, `?`, and Escape still do nothing in a field, and a field inside a dialog keeps every key. On macOS only Cmd triggers these shortcuts from a field, so Ctrl+K keeps deleting to the end of the line. Ctrl+Shift+R on Windows and Linux now refreshes the UI's data instead of hard-reloading the page (Ctrl+F5 still hard-reloads), and Caps Lock no longer disables the palette and Logs shortcuts. Ctrl+Shift+K and Ctrl+Shift+J are left to the browser, and Ctrl+Alt combinations (AltGr on Windows) no longer trigger any shortcut. The keyboard shortcuts panel, palette, header tooltips, and Preferences now show Ctrl and Shift off macOS and ⌘ and ⇧ on macOS, and the panel lists fit view and zoom under a new Stack group instead of Global.
- The Stack canvas and inspector in the web UI now work from the keyboard. A focused node shows a ring in the primary color, including disabled and dimmed nodes, and edges are no longer tab stops, so Tab moves from node to node. Backspace and Delete no longer remove the selected node from the canvas until the next refresh (nothing was ever sent to the gateway), and dragging from a node's handle no longer draws a connection line. Enter or Space on a node, or an "Inspect" command in the command palette, now moves focus to the inspector heading; closing the inspector with Escape or its close button returns focus to the node. The palette now has "Inspect" commands for linked clients and resources as well as servers. Collapsed inspector sections no longer leave their buttons and switches reachable with Tab or visible to screen readers, their toggles report whether they are expanded, and a section's content now loads the first time it is opened. An open section is no longer cut off at 1,000 px.
- Muted text, the danger button and danger confirms, error text on its tinted background, and violet text in the dark theme now meet WCAG AA 4.5:1 contrast in both web UI themes. Muted text is lighter in the dark theme and darker in the light theme, error red is darker in the light theme and slightly lighter in the dark theme, and dark violet is lighter; charts, status dots, and badges that share these colors shift with them. Filled danger buttons now use a flat, darker rose fill instead of a gradient. Text with reduced opacity and focus indicators are not changed by this entry.
- Dimmed labels, hints, column headers, section headings, placeholders, separators, and hover states in the web UI now render at the full muted, secondary, or primary accent color instead of a translucent version that blended into the surface. This affects every workspace in both themes, including the Library kind switch, Variables column headers, status bar counts, inspector headings, empty-state hints, diff gutters, YAML comments, and nearly every input placeholder. Primary text links now brighten to the lighter accent on hover, and primary buttons with a tinted background keep their text color on hover while the background changes. Frontend lint now rejects an opacity modifier on a text token except on a decorative icon marked `aria-hidden`.
- Workspaces in the web UI now keep data on screen when a refresh fails. Previously one failed poll after a successful load (for example while the gateway restarts) hid what the UI already held. Logs replaced its rows with "Logs unavailable"; Stack covered the graph with an opaque panel; Metrics and the detached Metrics window replaced the dashboard with a failed state; Runs replaced the table with a fixed message after a failed Load older and emptied it after a failed Refresh; and Traces alternated between a skeleton and the empty state on every poll when the list was empty. Now a failed refresh keeps the last-known data under an alert row with the error and Retry; Logs, Stack, Metrics, and Traces also show the time of the last successful load. Log rows stay scrollable, expandable, and exportable. Stack desaturates the graph but leaves it interactive, and the spec and Access Lens editors stay at full color. The skeleton shows only before the first load completes, and the full failed state only when nothing is loaded; Stack's failed state picks its reconnect hint from the connection status instead of the error text, and Runs' failed state now includes the real error. The Runs and Traces banners stay up while a retry runs instead of briefly clearing. Metrics switching to another range shows the loading state instead of the previous range's error, and Runs and Traces never show rows loaded under other filters (Traces: server, errors only, or minimum duration) as last-known data.
- Library segments, pickers, and the Global Context dialog now show a failed load instead of loading forever or claiming there is nothing there. Previously a failed fetch (for example while the gateway restarts or a credential needs re-entry) left the Agents segment blank; left the Hooks, Packs, and Proposals segments and the pack detail on "Loading …"; showed the proposal Pin as "loading" with Approve disabled and no reason; left the wizard variable picker on "Loading..." (including when the variable store is locked); and showed "No supporting files" in the skill file tree, "No fragments yet" and "Add a fragment to start." in the Global Context fragments view, and "No pin record" in the context pin review. Each of these now shows the error and a Retry when nothing is loaded, and keeps data already on screen under an alert row with Retry when a refresh fails. The variable picker says "Variables locked. Unlock them in the Variables workspace, then retry." when the store is locked. A failed proposal detail reads Pin "unavailable" and explains that Approve needs the current base and pin status, and Approve stays disabled until that detail loads. The Global Context dialog's first-load failure is now a failed state with Retry that never shows together with the loading line, its refresh-failure strips gained Retry, and its messages now name the next step (a credential failure reads "Load global context paused. Verify your gateway credential to resume." instead of "Authentication required"). Fragment Add is disabled until the fragment list loads, and a failed reload keeps the loaded list and the open editor. These load failures no longer raise a toast, and cancelled requests show nothing. The Agents segment also shows "Loading agents…" while its first fetch runs.
- Failures in the Create new wizard now keep their detail and no longer dead-end. In the Review step, an unknown or mistyped key in a server or resource YAML is listed with its field and line and disables Deploy, instead of the single warning "Validation unavailable"; when validation truly cannot run, the warning now names the reason and Deploy stays enabled. A rejected deploy of any server or resource now shows a persistent alert in the Review step with the gateway's message and every returned issue (for example a duplicate name), instead of a toast reading "stack validation failed after append" or "Deploy failed: 400"; a name already reported by the running stack is flagged before Deploy, which stays disabled until the name changes. A rejected skill import now stays on the Browse or Configure step with the URL, credentials, and selections intact and an inline error, so the Install button retries it, instead of moving to an empty "Review & Install" step. The form-step YAML preview now validates server and resource fragments the same way the Review step does, so its badge agrees with Review, and a late validation response no longer replaces the result for newer YAML in the preview or in Review.
- The web UI shell now stays exactly as wide as the window. When the status bar's content was wider than the window (about 1,330 px with most status items present, so 1280 px laptops and side-by-side windows were affected), the whole shell was laid out at that width and clipped on the right with no scrollbar: header actions such as Create new, Refresh, Reload Config, and Preferences were off-screen, the status bar's right side was cut, and Stack's fit-to-view framed nodes partly outside the canvas. The status bar now puts its alert badges (spec health, pin drift, findings, pending authorization, and rate limits) directly after the connection state and before the counters. When the bar runs out of room, "Updated" and then the counters (sessions, tokens, and the rest) are hidden whole, starting from the right, and only after every counter is hidden do alert badge labels shorten with an ellipsis; each badge keeps its icon and status dot, and its tooltip or accessible name carries the full label.
- Keyboard list shortcuts in the web UI no longer take keys away from other controls. Enter now activates the focused button, link, or menu item in Logs, Traces, Variables, and Pins; previously the list cancelled it, so toolbar buttons, the Logs export menu, and links did nothing, Variables opened the selected variable for editing (fetching its value), and Pins moved focus to Approve and approved on a second Enter. Arrow keys, Home, and End move the list only when focus is on the page or inside the list, so they no longer change the selection or block scrolling from other controls and panes in those workspaces, Metrics, or Connections; `j` and `k` still move the list from a toolbar button. In Variables, Pins, Connections, and Metrics, keyboard focus now follows the highlighted row after a row was clicked, so Enter acts on the highlighted row, and Enter on a focused variable row selects and edits that row. Pins drift approval moved from Enter to Ctrl+Enter or Cmd+Enter, which clicks the Approve button (keeping its confirmation and reason checks), works from the reason and evidence fields, approves once when held, and does nothing inside a dialog or on a link; the chord is shown next to both Approve buttons, on the palette command, and in the keyboard shortcuts panel. With nothing selected, ArrowUp now goes to the last row instead of the second to last. Enter and Space on the connect switch in a Connections row now toggle the switch without selecting the row.
- Reopening the skill editor on the same skill now loads the stored body before Save is enabled. Previously, after a save closed the editor, opening that skill again showed the body from the earlier open as unchanged, and saving wrote it over the saved edit (or over a change made elsewhere in the meantime); typing on top of that text lost the earlier edit the same way. While the body loads, the field is read-only and Save stays disabled. The Library inspector's Instructions tab and Overview package check now reload the body after an editor save, a take upstream, or a sync, without reselecting the skill and without a loading flash. A collapsed and re-expanded row in the Stack sidebar's registry list now loads the stored body for its preview.
- Saving or forking a skill from the web editor no longer deletes or rewrites its acceptance criteria. Previously any criterion not written exactly as `GIVEN ... WHEN ... THEN ...` was lost on save without a warning: free-form text (including `PASS:` and `FAIL:` criteria) was deleted, lowercase or prefixed forms such as `Given X, when Y, then Z` and `Scenario 1: GIVEN ...` were rewritten, and multi-line criteria lost their newlines. A criterion now opens as GIVEN, WHEN, and THEN fields only when it is already in that exact form; any other criterion opens as one multi-line text field and is saved verbatim unless edited, and the editor no longer opens dirty. A GIVEN/WHEN/THEN row with only some fields filled is marked as incomplete, counts as an unsaved change when closing, and makes Save and Fork ask for confirmation before dropping it. The editor's validation indicator now opens a list of every error and warning message, including a local warning for incomplete criteria, instead of showing only counts. Validation no longer reports false errors for descriptions containing `: `, a newline, or a leading `#`, `[`, `{`, or `"`: the editor now sends the save payload itself to `POST /api/registry/skills/validate` through a new optional `skill` request field, which validates an object in the `PUT /api/registry/skills/{name}` body shape exactly as a save would. The field is additive; `content` requests behave as before, and sending both returns `400`.
- The Library skill import wizard now honors "Activate after import" and no longer trusts flagged skills implicitly. Previously every skill imported `active` regardless of the checkbox, and the wizard sent `trust: true` whenever any selected skill or agent had scan findings. A skill left unticked on the Configure step now imports as `draft`. The Configure step shows each skill's activation checkbox in its row, lists every flagged selected skill with its findings in one block, and keeps Install disabled until its acknowledgment is ticked; the request's `trust` is now set only from a ticked skill or agent acknowledgment, and changing the skill or agent selection clears the matching acknowledgment. Both acknowledgments state that trust applies to every item in the import. On the Browse step, a skill whose scan found nothing reads "no findings" in a neutral color instead of a green "safe", each row's name is a button that changes the preview from the keyboard, and the footer button reads "Next" when it leads to the Configure step. `POST /api/skills/sources` gains an additive optional `noActivateSkills` array naming skills to import as `draft`; when `selected` is non-empty, a listed name outside it returns 400, and a listed name that matches no skill in the import adds a warning. Requests without the field behave as before. See [`POST /api/skills/sources`](docs/api-reference.md#post-apiskillssources).
- Adding a variable in the web UI no longer silently overwrites one with the same key. Previously "Add one" in the Variables workspace and "Create & Insert" in the stack wizard's variable picker replaced the stored variable, reset its type, visibility, set, description, docs, example, and deprecation note, and still reported `Variable "KEY" created`, with no way to recover the old value. Both forms now show `KEY already exists.` as the key is typed and disable the add button, including for Enter. Replacing takes a `Replace…` (`Replace value…` in the picker) click and then a confirmation that states the current value cannot be recovered and lists the type, visibility, or set change; a replace is a partial update, so the description and other metadata are kept, and the picker keeps the set. The picker also offers `Insert existing`, which inserts `${var:KEY}` without writing. A plain add now sends `create_only: true`, so a key created elsewhere since the list loaded is refused, the list is refreshed, and the form shows the conflict instead of overwriting it. In the import modal, rows whose key already exists now start skipped (a UI behavior change: existing keys in an import are not written until a row is un-skipped or "Skip all conflicts" is cleared), skipped rows show an `exists` badge, a line explains the existing keys, and the button names new rows and overwrites separately (`Import 3`, `Import 3, overwrite 2`, or `Overwrite 2`). `POST /api/var` gains an additive optional `create_only` field: when `true`, an existing key returns `409 Conflict` with an error that names the key and no value. Requests without the field behave as before. See [`POST /api/var`](docs/api-reference.md#post-apivar).
- Reload Config and "Compare to running" in the web UI now compare the stack file with the configuration the running gateway loaded, instead of a copy of the stack file the browser tab kept since it first loaded. Previously, an edit made before the page loaded (or before a hard refresh) was applied by Reload Config with no preview and reported by "Compare to running" as "No drift" while Spec mode showed the change, and after a `--watch` hot reload, a `gridctl reload`, or a reload from another tab, the tab compared against a stale copy and showed changes that were already applied. A failed preview request no longer falls through to a reload, and a second click while a reload is in progress no longer starts another. Both views now ask `GET /api/stack/plan` when they open and show a structured list of added, changed, and removed MCP servers, resources, gateway settings, and networks, with each change's details and items from an override file marked; this list replaces the line-by-line text diff. When the preview request fails, the dialog shows the error with Cancel and "Reload without preview". With no listed changes, Reload Config still reloads, since a reload also applies `clients`, `limits`, `policy`, `groups`, `skills`, `model_preferences`, `runs`, and `experimental` changes and rotated `${secret:}` values that the comparison does not cover, and both views state those limits and that a reload can still be refused or require a restart. A reload failure without an error code, such as a network configuration change that needs a full restart, now shows the gateway's message instead of `Reload failed: 400`. No REST, CLI, or stack file contract changed.
- The web UI status bar now reports how fresh its data is and no longer opens on "Disconnected". Previously the "Updated" age only advanced when something else re-rendered the page, so it stayed at "Updated just now" while a status request hung, and during an outage it read like healthy data in the same muted tone; every page load also showed a red "Disconnected" until the first status response arrived, including behind the credential prompt. The age now ticks every second on its own, and while the gateway is unreachable it reads "Last updated 4m ago" in the warning tone. Before the first response, and while the credential prompt is open, the bar shows a neutral "Connecting"; the red state appears only after an attempt has failed. For assistive technology, the bar is now a footer landmark named "Status bar", and the connection state is a status message that does not include the ticking age.
- The detached inspector, Logs, Traces, and Metrics windows now show notifications. Previously a toast raised in one of those windows was never shown or announced, so in the inspector popout a failed tool allow-list save, a save that needs `gridctl reload`, and a successful save all looked the same. The detached Library window now shows "Library unavailable" with the error and Retry when the registry cannot be loaded, instead of "No skills registered", and a failed refresh keeps the loaded skills under an alert row with Retry. The detached inspector now shows "Gateway unavailable" with the error and Retry when status cannot be loaded, and names a node that is not in the running stack as not found, instead of "Select a node to view details" in both cases; its Refresh button now updates the node list it shows.
- Detached windows now behave more like the main window. A popout's title (for example "Gridctl - Logs", or "Gridctl - Details: db" with the selected node) now survives a reload and appears when the window's URL is opened directly; previously only a window opened from the main window got a title. The inspector, Library, Logs, Metrics, and Traces popouts show a banner under the header when the gateway cannot be reached, with Retry, and keep their data on screen; the banner does not repeat a failure the window already shows. Fullscreen in the Logs, Metrics, and Traces popouts now shows a warning when the browser refuses it instead of failing silently. The inspector's node menu and the Logs source menu now close on Escape (returning focus to the menu button) and on an outside click, and announce themselves as menus. The Library and inspector popouts no longer change the Logs text size: the inspector popout has its own Inspector row in Preferences, and the Library popout now follows the Library text size from Preferences (13 px by default). A size set earlier from those popouts stays as the Logs size. Text-size changes made in Preferences or in another window now apply to open windows without a reload. The Traces popout's waterfall gains View logs and View metrics, which open those workspaces in a new tab. Skills imported from a git source now show their provenance strip in the Library popout's editor and in the detached editor. Both menus label MCP servers "MCP server", and the Metrics popout header reads "Metrics" instead of "Token Metrics".
- Escape in the web UI now closes only the topmost dialog or panel, and global shortcuts do nothing while a modal dialog is open. Previously Escape with focus on a dialog button also cleared the Stack selection and closed the inspector; Escape in a dialog opened from another dialog (such as Import in Global Context) closed both; Escape in the Compare or Reload Config preview opened from the Spec pane closed the preview, the pane, and the selection; and with the Access Lens on, Escape turned the lens off instead of closing Preferences. Modal dialogs now take precedence over the Access Lens and Spec side panels, and a nested dialog closes before the dialog that opened it. While a modal dialog is open (including the command palette and the credential prompt), workspace navigation (Cmd/Ctrl+1-9 and Cmd/Ctrl+J), the command palette (Cmd/Ctrl+K), refresh (Cmd/Ctrl+Shift+R), compact mode (Cmd/Ctrl+\), fit and zoom on the Stack canvas, the rail toggles `[` and `]`, the shortcuts panel `?`, list navigation keys, and the Library `/` and `n` keys do nothing and are left to the browser, even when focus has fallen to the page. The Access Lens and Spec side panels are not modal, so these shortcuts keep working beside them.
- Keyboard focus in the web UI is now visible on every control in both themes. A focused control shows a solid 2px outline in the primary color, drawn inside the control where a scrolling or rounded container would cut it off. Previously icon buttons, the theme picker, workspace pills, tabs, switches, confirm dialog buttons, the credential prompt, list rows, and nearly every text field, select, and textarea turned that outline off and showed a faint ring, border, or fill instead (1.05:1 to 2.99:1 against the surface, below the 3:1 WCAG minimum), and in Windows High Contrast they showed no indicator at all. The skill instructions textarea and the JSON variable editor showed nothing at all. The keyboard-current row in the Tools list, the sidebar Tools editor, the tools and operations pickers, the command palette, and the Logs and Traces grids now also carries the inset outline while its list or grid has focus, and none when focus is elsewhere, so the row selected on first render is not outlined. Text fields show the outline on a mouse click as well, as browsers match focus-visible there.
- The command palette in the web UI now shows each group heading ("Recent", "Current Context", and "Navigate To") once. Previously every group showed a plain, unstyled copy of its heading above the styled row with its icon. Each group keeps its heading as its accessible name. Arrow keys in the Light, Dark, and System theme control (in the header Appearance popover and in Preferences) now move focus with the selection. Previously focus stayed on the radio that had been checked, so pressing ArrowRight twice from Light selected Dark and left Light focused instead of moving to System.
- The Stack inspector's Tools editor in the web UI can no longer save one server's tool selection to another server. Previously, with unsaved tool changes, selecting a different MCP server showed a discard prompt but left Save, the tool rows, Select all, and Clear active, so Save wrote the first server's selection to the second; when the two servers had the same number of tools, a curated selection could also be sent as an empty whitelist, exposing every tool on the second server. Unsaved changes now belong to the server they were made on: while another server is selected, the editor is locked, Save is disabled, and the rows and bulk buttons do nothing until you return to that server or discard. The prompt is now a confirm dialog that stays up until you choose, also appears when the Tools section is collapsed, and its Keep editing button returns to the edited server in the popout window too. Selecting another node, clicking the canvas background, pressing Escape, closing or popping out the inspector, the palette's Inspect commands, the pending authorization badge and toast, and the View Logs, Variables, and Open in Tools workspace buttons now ask before discarding unsaved tool changes instead of dropping them silently, and reloading or closing the tab with unsaved tool changes now shows the browser's leave warning. A selection is saved as an empty whitelist only when it includes every tool of the server being saved. In the Tools workspace, if the active server changes under unsaved changes (for example the server leaves the stack), a confirm now offers Discard, plus Keep editing when the server still exists.
- The Global Context dialog's fragments mode in the web UI no longer moves unsaved text from one fragment to another. Previously, adding a fragment while the open one had unsaved edits selected the new fragment and kept the old text in the editor, so Save wrote it into the new fragment and reported success; a refresh after the open fragment was removed outside the dialog did the same to the first remaining fragment. Unsaved edits now belong to the fragment they were typed in. Create in the rail is refused with a warning while there are unsaved edits, and the typed name stays in the input; a fragment created while you start typing elsewhere is added without switching to it, and text typed while the list reloads after a create or a save keeps its fragment selected; and edits to a fragment removed outside the dialog are discarded with a warning instead of being applied to another fragment. After a save, the editor shows the saved text as clean immediately instead of the old text until the list reloads. A new Revert button beside Save asks for confirmation and then restores the fragment's saved content, and the toast shown when switching fragments with unsaved edits now reads "Save or revert your changes before switching fragments" instead of pointing at a discard control that did not exist.
- Leaving an unsaved editor in the web UI now asks first, from every shell-level navigation. Previously, with unsaved edits in the Tools workspace, the Access Lens, the skill editor, the agent editor, or the Global Context dialog, Cmd/Ctrl+J, Cmd/Ctrl+1-9 (except from the Tools workspace), every command palette navigation, the status bar's experimental-flags and tokens buttons and its spec health, pin drift, findings, and rate limit badges, the Create Resource wizard's Variable and Client Link tiles, the Stack inspector's and graph's links to other workspaces (including Manage Skills and the security evidence actions), and the "View" action on pin drift and findings toasts left without a prompt and discarded the edits (the Access Lens draft stayed in memory with no reload warning). Each of these now shows one "Discard unsaved changes" confirm that names what is unsaved: Cancel keeps the page and the edits, and "Discard & leave" discards them, closes an open editor, and goes to the requested page. The Tools workspace and the Access Lens ask only when leaving their workspace, so "Open Spec Editor" with Access Lens edits, for example, does not ask; the three editors ask on any of these navigations that they leave reachable. While one of the editors is open, the shell shortcuts, workspace pills, palette, and status bar are inactive, so a toast's View action is the navigation that asks. A tool-only Access Lens edit now also asks from the workspace pills and the Skills row. "Reset gridctl…" from the palette opens its dialog only after the navigation goes ahead. With nothing unsaved, every control navigates as before. The agent editor now warns on reload or tab close while it has unsaved edits, and Cmd+S or Ctrl+S saves it. Browser Back and Forward are not covered.
- The Variables import dialog, the Variables inline edit, and staged Connections link changes in the web UI now ask before discarding unsaved input. Previously Escape, a backdrop click, the close button, or Cancel closed the import dialog and lost pasted text and per-row type, set, secret, and skip choices; selecting another variable, moving the list selection with the keyboard, or closing the inspector reset a changed edit; and leaving Connections or reloading lost staged link and unlink changes. Each now raises a discard confirm when there is something to lose, and cancelling keeps the input as it was. A changed import or inline edit also blocks shell navigation (including a consumer chip in the inspector, which leaves the Stack selection untouched when cancelled), and staged connection changes block navigation that leaves Connections, including "Edit in Tools" and agent links. All three warn on reload or tab close while unsaved. An untouched import dialog (including one seeded by a dropped file or the missing-variables Create action), an unchanged edit form, the edit form's own Cancel and Escape, the staged bar's Discard, and selecting another client in Connections behave as before.
- The Create new wizard's Environment Variables, Build Arguments, and Custom headers editors no longer merge, reorder, or drop rows while typing. Previously, typing a key that passed through another row's key (for example `DB_HOST` next to `DB=postgres`) merged the two rows, cleared a value, and lost focus; clicking Add twice gave one blank row; an all-digit key such as `8080` jumped to the top and the next keystroke renamed a different row; and a `__proto__` key deleted its row. Each row now stays where it was typed, Add always adds a blank row, and two rows sharing a key are both marked with an inline `Duplicate key "DB". Only the first row is used.` message, with the first row's value used in the generated YAML until the collision is resolved. A pending duplicate survives collapsing and reopening its section. Generated YAML is unchanged for input without duplicate keys.
- Template cards in the Create new wizard now apply to every resource type and replace the form instead of merging into it. Previously the PostgreSQL, Redis, MySQL, and MongoDB resource cards and the Basic Stack card did nothing, so the resource form repeated the same four choices in a Preset section; an MCP server card set only the server type, transport, and source, so after using a catalog entry, Blank Server kept the catalog's name, image, command, environment, URL, and auth in the form, YAML preview, and deploy. Now a resource card opens the form filled with that database's name, image, ports, and environment, and the Preset section is gone (the Add Resource presets inside a stack are unchanged). Basic Stack opens with one container server, expanded so it is visible, and Blank Stack with none. Clicking any template card, including the one already selected, or "Use this server" replaces that type's form data, name included, and leaves other types' data alone; Back followed by Next still keeps the form. A picked card opens without field errors left over from the previous form. A pick made in YAML mode returns to the form view with the template's data. The Basic Stack card now reads "Stack with one container server", and the Blank Stack preview no longer shows a `network:` line.
- The Stack inspector now shows a server's health first. Previously the Status section listed provenance and a full security evidence card above Health, the health error, and crash details, so on a narrow rail an unhealthy server's error was below the fold. Status now starts with State, Health, Last Check, the health error, and per-replica crash details (which now also show before the first health check reports), followed by Transport and the identifier rows. Security evidence and Execution evidence moved out of Status into their own collapsed sections after Availability, each with a one-line summary in its header such as "1 failing, 3 unknown, 2 passing" or "0 of 1 replicas eligible"; the summary counts checks and never calls a server secure. Both sections start collapsed on every selection, and the detached inspector also shows health under State and the two evidence sections between Status and Tools. Endpoint, Container, SSH Host, Spec, Config, Image, Source, and Resolved values now wrap to the rail's width instead of truncating behind a hover title, a new Container row shows the full container ID, and Endpoint, Container, Config, Image, Source, and Resolved have copy buttons that show a check for 1.5 seconds or "Copy failed" when the browser refuses. The tool popover's Copy name button now shows "Copied" or "Copy failed" instead of nothing, Copy filtered view in Logs now shows a toast for either result, and the toast from other copy actions now reads "Copy failed" when the browser refused the legacy copy command instead of reporting success. The inspector's accent line is drawn on the inspector's own left edge.

## [1.0.0-rc.9] - 2026-11-22

### Features

- Rendered agent projections can carry a model preference. `model_preferences.agents` accepts an optional `clients` map keyed by rendered agent target (`opencode`, `copilot`, or `gemini`), each with an optional `default` and `overrides` (exact agent name to value) written in that client's own model vocabulary: `provider/model` with an optional `#variant` for OpenCode, a model name for Copilot, and a model ID or `inherit` for Gemini CLI. With `rewrite: true`, the value resolved for an agent (its override, then the client default) is written as the `model:` key of the rendered file, after `mode` on OpenCode and after `description` on Copilot and Gemini CLI. Gridctl never translates a Claude alias, and the author's declared `model:` still never reaches a rendered file. The lock entry records the value as `model_value`, and the sync row detail reads `model preference "<value>" applied (policy)`, or `(policy; author declared "<value>" is not carried)`, followed by `; ` and the lossy-render text when other keys were dropped; `model` no longer appears among the dropped keys there. The lifecycle matches the identity rewrite: a sync without stack context preserves the file, a hand edit is `drifted` and is overwritten only with `--force` after a backup, removing the value (or `rewrite: false`) reconciles the file back to the pass-through render and clears `model_value`, and the daemon applies values on every reconcile and hot reload. Without `clients`, rendered output and the identity copy are byte-identical to before, and no lockfile revision bump is needed. Under a loaded stack (the daemon or `--stack`), `skill project status` now judges rendered agent projections against the bytes a sync would write, so a `model_value` change reads `stale` with `model preference policy changed` and any other difference, such as a gridctl upgrade that changes renderer output, reads `stale` with `projection output changed`; previously those rows read `in-sync`. Lint changes: `model-preference-unhonored` for agents now counts per rendered target, leaves out targets where every resolving agent has a client value, and names `set model_preferences.agents.clients.<slug>` as the remedy; the new advisory warning `model-preference-client-value-shape` flags an OpenCode value not shaped like `provider/model[#variant]`; client values are never checked against the Claude alias list. Structural errors inside `clients` are the one non-advisory model preference finding: a key that is not a rendered agent target (including `claude-code`), a `clients` block under `skills`, an empty override value, a whitespace-only `default`, and an entry with no value fail validation (`gridctl validate` exits 1), `gridctl apply` and hot reload refuse the stack, and `skill project sync --stack` and `skill project status --stack` exit 2 naming the field. Every other finding keeps its severity. `GET /api/registry/agents` and `GET /api/registry/agents/{name}` gain an additive `modelPreference.clients` map (`{"<slug>": {"value", "resolution"}}`, present only for targets that resolve a value), and the Library agent inspector annotates those targets' honor rows with `stack value <value>`. The `skill project status` text table now labels agent rows with a `model_value` (identity and rendered) as `copy (model policy)`, which fixes a mismatch with the CLI reference and skills guide: agent rows previously printed the raw `copy` channel even for identity rewrites. The JSON `channel` stays `copy`. Manual acceptance remains for a live OpenCode V2 session loading a projected agent with `model: provider/model#variant` and the child session reporting that model and variant, and for Copilot CLI or VS Code and Gemini CLI reading the `model` key from a projected file. See [Model Preferences](docs/config-schema.md#model-preferences).

### Changed

- A re-imported hook whose bytes differ from its pin is now held instead of re-pinned. This is a behavior change in the release-candidate series: the hooks guide previously said an accepted `pack add` or `skill update` re-pins. Now only the first import (trust on first use) and `gridctl skill pins approve` write pinned bytes. An accepted `pack add`, `skill update`, `POST /api/packs`, or REST skill source update still installs the new bytes in the registry and records the new commit in `.origin.json`, so `gridctl skill pins diff <name> --kind hook` reviews them, but the pin keeps the approved bytes, findings, and reason with status `drift`, and no approvals entry is written. `pack apply` and `skill project sync --kind hook` then report `skipped-pin-drift` with the `--expect` remediation on every target, leave the projected script copy and client entry unchanged, and exit 1 until `gridctl skill pins approve <name> --kind hook --expect <composite_hash>` (with `--reason` when findings remain). `pack add` keeps its exit code and prints `Warning: <name>: content changed since it was pinned; projection waits for 'gridctl skill pins approve <name> --kind hook --expect <composite_hash>'`; the `pack add --format json` document and the `POST /api/packs` `doc` gain an additive `held_hooks` array of `{name, composite_hash}`, and the held hook stays in `hooks` and the pack record. `skill update` prints `Held hook <name>: content changed since it was pinned; approve with gridctl skill pins approve <name> --kind hook --expect <composite_hash>` instead of `Updated hook`, with an unchanged exit code. `POST /api/skills/sources/update` and `POST /api/skills/sources/{name}/update` report the hold as a line in that skill's `warnings`. The Library pack update toast and the import wizard's done step name held hooks. `--trust` still gates only the import scan and never approves a pin. Unchanged bytes keep the pin `pinned`, and a first import still pins with no approval. The pin file format is unchanged. Migration: after a pack update that changes a hook, review `gridctl skill pins diff <name> --kind hook`, approve with the printed `--expect` hash, then run `gridctl pack apply` again; scripts that run `pack add` and then `pack apply` unattended need that approval step. To re-trust a hook on first use instead, run `gridctl skill pins reset <name> --kind hook` before `pack add`. See [Hooks](docs/hooks.md#re-import-holds-changed-bytes).

### Maintenance

- The Gatekeeper frontend audit now runs `scripts/npm-audit-gate.py` over `npm audit --json`. The bar is unchanged: any high or critical advisory fails. A reviewed entry in `web/audit-exceptions.json` can accept one named advisory that has no patched release, for at most 30 days, and the gate fails on an expired entry or on one whose advisory the report no longer carries. The file ships empty: GHSA-fc5q-39xc-vjpf (picomatch), the advisory that prompted it, was withdrawn on 2026-11-18.

### Bug Fixes

- The gateway node's Skills row on the Stack canvas now opens the Library workspace in the same tab, like the gateway inspector's "Manage Skills" link. It previously opened a separate tab at `/library-window` without the workspace navigation, header actions, or status bar, closed the Stack inspector, and marked the Library as detached. The row honors the Access Lens unsaved-draft confirm before leaving the Stack workspace, and its accessible name is now "Open skills in the Library". The Library header's popout button is unchanged.
- The web UI now recovers when it changes under an open tab after an upgrade or a rebuild. A request for a missing file under `/assets/` returns 404 with `Cache-Control: no-store` instead of `index.html` with 200, `index.html` is served with `Cache-Control: no-cache`, and hashed files under `/assets/` are served with `Cache-Control: public, max-age=31536000, immutable`. Every other path, including client routes such as `/library/<name>`, still falls back to `index.html`. When a code-split chunk fails to load, the tab reloads itself once (at most once every 30 seconds per tab, and never when session storage is unavailable). If the reload does not happen, for example because an unsaved editor's leave prompt cancels it, the error boundary shows "Reload needed" with a Reload button instead of "Something went wrong" and the module URL. A tab opened before upgrading to this release still needs one manual reload, because the recovery code ships in the new bundle. Automatic recovery applies to later upgrades.
- `gridctl skill pins list` now reports drift against the registry on disk instead of the status stored by the daemon's last sync, so a `pack add`, `skill update`, or hand edit shows as drift without `skill pins verify`, with or without a running daemon. Previously the listing stayed `pinned` until the stack's daemon synced, which never happened for a stopped stack. `list` still never writes the pin file or the approvals log; `last_verified_at` and the `LAST VERIFIED` column remain the time of the daemon's last sync, a pinned skill missing from the registry keeps its stored status, and content that cannot be hashed lists as `drift`. `list --format json` keeps `schema_version` 1 and the same fields, and exit codes are unchanged. This matches the documented pin behavior, so it ships as a bug fix under Article VIII.

## [1.0.0-rc.8] - 2026-11-16

### Features

- An encrypted variable store can be unlocked once for the life of a stack. `gridctl apply` (daemon mode and `--foreground`) now prompts on stderr with `Variable store is locked. Passphrase: ` when stdin is a terminal, neither `GRIDCTL_VAULT_PASSPHRASE` nor `GRIDCTL_VAULT_PASSPHRASE_FILE` unlocked the store, the process is not the daemon child, and the stack references the store. A wrong passphrase prints `wrong passphrase` and prompts again, up to three attempts; EOF or ctrl-C prints `passphrase entry cancelled`, restores the terminal, and exits 1. A wrong value in either variable still fails without a prompt, and nothing prompts without a terminal. `gridctl plan` apply and `gridctl pack apply` reach the same prompt and now cancel it on ctrl-C. In daemon mode the parent hands the passphrase to the daemon child as one line on a stdin pipe with the new `--passphrase-stdin` argument, never in argv, the environment, or a file, and the daemon keeps the store unlocked until it stops. `--passphrase-stdin` is also a public flag on `apply` and `serve` for wrappers: one line, at most 4 KiB, within 10 seconds (`no passphrase arrived on stdin within 10s`), refused on a terminal (`--passphrase-stdin requires piped input`); the environment variables still take precedence and the line is then discarded. Without a terminal, a locked-store `apply` failure adds the line `  Or run gridctl apply in a terminal to be prompted`. `GET /api/var/status` gains the additive `unlock_source` field (`environment`, `passphrase_file`, `stdin`, `api`, or `cli`; omitted when locked, plaintext, or unrecorded), and `POST /api/var/unlock` accepts an additive optional `source` of `api` (the default) or `cli`; any other value is 400. A request without `source` behaves as before. The daemon registers the passphrase for log redaction. The `var` subcommands now honor `GRIDCTL_VAULT_PASSPHRASE_FILE` after `GRIDCTL_VAULT_PASSPHRASE` and before the terminal prompt, and a rejected file names the variable, not its contents. `gridctl upgrade --restart` reads each plain or stackless daemon's store status before any `destroy`: for a daemon holding an encrypted store unlocked with neither passphrase variable set, it checks that the target binary accepts `--passphrase-stdin`, prompts once on a terminal, and hands the passphrase to each `apply` and `serve` step over stdin. Without a terminal, with a target that predates the flag (a rollback or pinned `--version`), or when the local store cannot be read, it prints `Not restarted: <name> (...)`, leaves that daemon running, runs the other plans, and exits 1. Service-owned daemons and a passphrase from the environment are unchanged. Manual acceptance remains for the terminal prompt on macOS and Linux with zsh for `apply`, `pack apply`, `plan` apply, and `upgrade --restart`, including a wrong first attempt and ctrl-C. See [Vault is locked](docs/troubleshooting.md#vault-is-locked).
- `gridctl var unlock` and `gridctl var lock` now start and end the unlocked session on every running gateway for this home. `var unlock` verifies the passphrase against the local store (from `GRIDCTL_VAULT_PASSPHRASE`, then `GRIDCTL_VAULT_PASSPHRASE_FILE`, then a terminal prompt), posts it with `"source": "cli"` to each running gateway's `POST /api/var/unlock`, and prints one line per gateway: `unlocked`, `already unlocked`, or `failed: <reason>` (`failed: unauthorized; set GRIDCTL_GATEWAY_TOKEN` for an OAuth-mode gateway). With no gateway running it prints `No running gateway. Passphrase verified; gridctl apply will prompt for it when a stack needs the store.` A wrong passphrase contacts no gateway. The new `--format json` (and `--json`) prints `{"schema_version": 1, "verified": true, "gateways": [{"stack", "port", "status", "error"}]}`; exit `0` when verified and no gateway failed, `1` on a wrong passphrase, no passphrase available, or a failed gateway (JSON reports `verified: false` when the passphrase was not verified), `2` when the state directory or local store cannot be read. This replaces the verify-only `var unlock` described under Bug Fixes. Behavior change: on a store that is already encrypted, `var lock` no longer prompts or re-encrypts the file with a new passphrase; it calls the new `POST /api/var/relock` on every running gateway and prints `Store is already encrypted. Locked in N running gateway(s): <name> (<port>)` (or `no running gateway to lock`). Use `gridctl var change-passphrase` to rotate the passphrase. On a plaintext store `var lock` encrypts as before. `POST /api/var/relock` (additive, canonical only, no body) drops the variables, sets, passphrase, and unlock source from the daemon's memory without touching the file and returns `{"status": "locked"}` or `{"status": "already_locked"}`; a plaintext store returns 409 `store is not encrypted`, and no store returns 503. `gridctl status` gains a VARS column per gateway (`plaintext`, `locked`, `unlocked (<source>)`, `unlocked`, or `-`) and `--json` gains an additive optional `variables` object (`encrypted`, `locked`, `unlock_source`). The `gridctl doctor` vault message now lists which running gateways hold an encrypted store unlocked and how, and which are locked; the check ID and JSON shape are unchanged. `gridctl var explain` on a locked store names a running gateway that holds it unlocked (`store is locked in this process; gateway <name> holds it unlocked`); the JSON shape is unchanged. Web UI: the Lock action on an encrypted store now calls `POST /api/var/relock` directly with no passphrase form. It previously re-encrypted the store through `POST /api/var/lock` with whatever passphrase was typed, unchecked against the current one, which could silently rotate it. The encrypt form now only encrypts a plaintext store, and the Variables header shows how an encrypted store was unlocked (`Unlocked by prompt`, `Unlocked by environment`, `Unlocked by passphrase file`, `Unlocked through the API`, or `Unlocked by gridctl var unlock`). Manual acceptance remains for `gridctl var unlock` at a terminal prompt, including ctrl-C. See [Vault is locked](docs/troubleshooting.md#vault-is-locked).
- The `var` subcommands now use a running gateway that holds the store unlocked. When the CLI's store is encrypted and locked, `gridctl var set`, `get`, `list`, `delete`, `import`, `export`, `sets list|create|delete`, `run`, and `scan` try `GRIDCTL_VAULT_PASSPHRASE`, then `GRIDCTL_VAULT_PASSPHRASE_FILE`, then the lowest-port running gateway for this home whose `GET /api/var/status` reports the store unlocked, then a terminal prompt, so after one `gridctl apply` they need no passphrase for the stack's life. Through a gateway they call the existing `/api/var` routes with the daemon's recorded credentials; `export`, `run`, and `scan` fetch values per key, `run` fetches only the selected keys, `export` never fetches reserved keys, and output and exit codes match the local path. `--log-level debug` names the gateway used. A gateway that is unreachable, answers 401 or 423, or reports the store locked is skipped; any other failure fails the command with `gateway <name>: <status> <message>` and does not prompt. Without a terminal and with no source available, the error is now `vault is locked. Set GRIDCTL_VAULT_PASSPHRASE or GRIDCTL_VAULT_PASSPHRASE_FILE, start the stack with gridctl apply, or run this command in a terminal to be prompted`. `var scan` without `--file` now resolves a locked store the same way (exit `2` when nothing is available); with `--file` it still skips a locked store without prompting. `var change-passphrase` and `var explain` stay local, and `gridctl skill`, `gridctl import`, and `gridctl add` keep their local-only unlock and message. The `var explain` locked problem text now ends `, so apply and var commands use the gateway` (naming the lowest-port gateway when several hold the store unlocked); the JSON shape is unchanged. See [Vault is locked](docs/troubleshooting.md#vault-is-locked).
- Hooks now project to Antigravity. `gridctl skill project sync --kind hook`, `gridctl pack apply`, and the REST hook projection routes write one top-level `gridctl-<name>` key per hook into the shared `~/.gemini/config/hooks.json` when `~/.gemini/config`, `~/.gemini/antigravity`, `~/.gemini/antigravity-cli`, or `~/.gemini/antigravity-ide` is a directory. The key holds `enabled: true` and `PreToolUse` and `PostToolUse` matcher groups; matchers are mapped through a tool table (`Bash` to `run_command`, `Edit` to `replace_file_content|multi_replace_file_content`, and so on) and joined with `|`, or `*` for every tool. Antigravity has no exec form, so each `command` is the absolute script copy and its `args`, every word single-quoted; a path or argument containing a single quote, newline, or NUL byte is an `error` row naming the value, and nothing is written. `timeout` defaults to 600 seconds. Writes are hujson patches at `/gridctl-<name>` after a backup beside the file, so sibling keys and comments keep their bytes; a missing file is created with only the gridctl key and `created_file` is recorded, and unsync removes the key and leaves `{}` when no keys or comments remain. An unrecorded `gridctl-<name>` key is `skipped-foreign` unless `--force`, a hand-edited key is `drifted`, and a file that does not parse is `skipped-unmanaged` even with `--force`. Unmapped events and matcher tokens, `if`, and cosmetic handler keys are reported as for OpenCode (`lossy antigravity render: dropped handler keys ...`). Scripts are copied to `~/.gridctl/hooks/<name>@antigravity/`. gridctl does not translate Antigravity's camelCase payload; the flat `decision: deny` in the portable deny recipe is the documented deny. `antigravity` leaves `unsupported_clients` and the stderr note. A release without this target reads the record as `target-missing` (`the recorded groups were removed from the settings file`), returns `error` on its unsync, and never edits the hooks file or deletes the script copy: run `gridctl skill project unsync --kind hook --all --clients antigravity` before rolling back. Manual acceptance remains for a live Antigravity session loading the key, denying `git push --force`, its exit-code and empty-stdout behavior, and matcher anchoring. See [Hooks](docs/hooks.md#antigravity).
- Hooks now project to GitHub Copilot CLI. `gridctl skill project sync --kind hook`, `gridctl pack apply`, and the REST hook projection routes write `~/.copilot/hooks/gridctl-<name>.json` when `~/.copilot` is a directory. The file is a generated `version: 1` hook file (mode `0644`, two-space indent) with PascalCase `PreToolUse` and `PostToolUse` keys, so Copilot sends a Claude-shaped payload and applies Claude matcher semantics. Each handler runs the pinned script copy through `exec` plus `args`, with no shell string; `matcher` is passed through as written and omitted when the group had none, and `timeoutSec` is the handler `timeout` rounded up and omitted when unset. Groups under any other event are reported as dropped, a hook with none left is `skipped-unexpressible`, and `if` and cosmetic handler keys are reported as for OpenCode (`lossy copilot render: dropped handler keys ...`). Scripts are copied to `~/.gridctl/hooks/<name>@copilot/`; ownership, drift, `--force` with backups under `~/.gridctl/project-backups/hook-file/copilot/<name>/`, unsync, `pack remove`, and `reset` follow the OpenCode rules. `copilot` leaves `unsupported_clients` and the stderr note, and its `skipped-unsupported` rows become real actions on the next sync or apply. `COPILOT_HOME` is not honored. Copilot denies on any non-zero `PreToolUse` exit, so the example `block-force-push` script now follows a portable deny recipe documented in [Hooks](docs/hooks.md#portable-deny-recipe): on a match it prints one JSON object with both the flat `decision`/`reason` pair and the nested `hookSpecificOutput` block, writes the reason to stderr, and exits 2; otherwise it exits 0 with no output. A release without this target reads the Copilot record as `target-missing` (`the recorded groups were removed from the settings file`), returns `error` on its unsync, and never deletes its file or script copy: run `gridctl skill project unsync --kind hook --all --clients copilot` before rolling back. Manual acceptance remains for a live Copilot CLI session loading the flat `exec` entry under the PascalCase key, denying `git push --force`, and running under the session sandbox. See [Hooks](docs/hooks.md#copilot-cli).

### Bug Fixes

- A locked variable store no longer reads as a missing variable. With an encrypted store and neither `GRIDCTL_VAULT_PASSPHRASE` nor `GRIDCTL_VAULT_PASSPHRASE_FILE` set, `gridctl apply`, `gridctl plan` against a running daemon, `gridctl reload`, `GET /api/stack/plan`, and `POST /api/reload` now fail with `variable store is locked; could not resolve: <KEYS>`, name both environment variables as the fix, and point at `gridctl var explain <KEY>`, instead of `missing variable(s)` with a `gridctl var set` remedy that would overwrite the stored key. Exit codes, HTTP statuses, and the reload `invalid_candidate` code and message are unchanged; the reload result's `errors` list (REST body, nested `error`, and `gridctl reload --format json`) now carries that one locked-store entry, which names keys and no values. Other non-validation failures still omit `errors`, and the `missing variable(s)` text for a genuinely absent key on an unlocked store is unchanged. While the store is locked, a `${var:KEY}` that resolves from the environment logs `variable resolved from the environment because the variable store is locked` with the key, and `secrets.sets` logs that it injected nothing; both warnings appear on quiet reload and plan loads too. `gridctl var unlock` now says what it always did: it verifies the passphrase in that process and persists nothing (help text and the success line both say so). The non-terminal `vault is locked` error from `var` subcommands no longer recommends `gridctl var unlock` and names `GRIDCTL_VAULT_PASSPHRASE` or a terminal prompt. The documentation previously described `var unlock` as decrypting the store for the session and recommended it as the fix for a locked store; the command was never durable. The troubleshooting guide, CLI and API references, configuration schema, and the secrets-vault example now describe the two environment variables, the passphrase file, and that a web UI unlock lasts only for the daemon's lifetime.

## [1.0.0-rc.7] - 2026-11-13

### Features

- Hooks now project to OpenCode. `gridctl skill project sync --kind hook`, `gridctl pack apply`, and the REST hook projection routes write `~/.config/opencode/plugins/gridctl-hook-<name>.ts` when `~/.config/opencode` is a directory. The file is a generated OpenCode V2 plugin (template version 1, mode `0644`) that spawns the pinned script in exec form with a Claude-shaped stdin payload, denies a `PreToolUse` call on exit 2 or a JSON deny, and fails open with a warning otherwise. Every handler has a finite deadline: its `timeout`, else 600 seconds. Only `PreToolUse` and `PostToolUse` groups with matchers in the OpenCode tool table map. A hook with no expressible group is the new `skipped-unexpressible` action (exit 1 on a direct sync, counted as applied by `pack apply`). A partly expressible hook is written and names `dropped groups: ...` in the sync and status detail; a handler `if` is reported as `dropped if "<rule>": the handler runs on every matched call`, and cosmetic keys as `lossy opencode render: dropped handler keys ...`. `requires.hooks` counts a partly expressible hook as enforced. Each client now owns its script copy: Claude Code keeps `~/.gridctl/hooks/<name>/`, OpenCode uses `~/.gridctl/hooks/<name>@opencode/`, and each unsync, status, and reset row acts on its own copy only. Ownership, drift, `--force` with backups under `~/.gridctl/project-backups/hook-file/opencode/<name>/`, unsync, `pack remove`, and `reset` follow the Claude Code rules. Hook lock entries record `channel: dedicated-file` for OpenCode; the channel is display only and is read from the target table, so no lockfile revision bump. A release without this target reads the OpenCode record as `drifted`, returns `error` on its unsync, and never deletes the plugin or its script copy: run `gridctl skill project unsync --kind hook --all --clients opencode` before rolling back, or re-upgrade. Status rows gain the channel `dedicated-file` and render `lossy`. Cursor and Windsurf (now named Windsurf (Devin Local), also detected by `~/.config/devin` and `~/.devin`) are reported as covered by the Claude Code projection: their `skipped-unsupported` detail and stderr note say so, and REST `unsupported_clients` rows gain `covered_by`. Gemini CLI is detected by `~/.gemini/settings.json` instead of `~/.gemini`, so an Antigravity-only home gets no Gemini CLI note or row. A default sync no longer reports an undetected target as `skipped-unavailable` when another target is detected. `gridctl pack remove` names the drifted client on a kept hook row. Manual acceptance remains for a live OpenCode V2 session resolving the plugin import and denying `git push --force`. See [Hooks](docs/hooks.md#opencode).

### Maintenance

- `task verify` now builds `./gridctl` and validates example stacks and their dependency references first, through the new `task examples:check`, the same task the Gatekeeper `test` job runs. A YAML file under `examples/` that fails `gridctl validate` now fails locally with the file and its validation errors. `task examples:validate` runs stack validation alone. The list of non-stack YAML excluded from both checks lives once, in `scripts/example-stacks.sh`.

### Bug Fixes

- Build with Go 1.26.11 so govulncheck no longer reports GO-2026-6859 in net/http (Go 1.26.10). The language version stays 1.26.0. Suppressed Docker client findings are unchanged.

## [1.0.0-rc.6] - 2026-11-10

### Features

- Proposals stage a change to a governed document for review. `gridctl propose <file|->` stores a JSON create document (a unified diff against a skill, a context fragment or `AGENTS.md`, or a hook, with its base composite hash, classification, prevalence, evidence, reason, and source) at `~/.gridctl/proposals/<id>.json` and scans the post-apply content for advisory findings. `gridctl proposals list|show|approve|reject|base` reviews them; `base` prints a target's current composite hash, including for a never-pinned skill. `kind: agent` is refused because agent pins do not exist yet. Approve is compare-and-swap on the base hash. A locally authored skill or local fragment with no pin drift takes the apply path: the diff is written, the target is re-pinned, and the approvals log entry carries the proposal's reason and evidence plus `proposal:<id>`. Any imported target (pack, git, or local import) and every hook take the record path: the verdict is recorded on the proposal only, with no target write, pin change, or log entry. Reject records a reason and keeps the proposal. A skill approve routes through the running daemon for the resolved stack. Exit codes are `0` success, `1` refusal (stale base, pin drift, reason required, not pending, apply failed, credential-shaped input), and `2` infrastructure. REST adds `POST /api/proposals`, `GET /api/proposals`, `GET /api/proposals/{id}`, `POST /api/proposals/{id}/approve`, and `POST /api/proposals/{id}/reject`. The security report adds `proposal.pending`, not a fail predicate, with reason codes `proposals_pending` and `no_pending_proposals` and the `pending_proposals` fact. `gridctl.security-report.v1` is unchanged; an older binary rejects a snapshot containing the new predicate or reason codes on import. `gridctl info --json` advertises `schemas.proposals` (`"1"`), the version of staged proposal files under `~/.gridctl/proposals/`, and the text `Schemas:` line gains `proposals=1`. Retained-binary entries in `gridctl upgrade --list --format json` gain the same `proposals` key; entries recorded by earlier releases read it as empty, which means unknown. `gridctl upgrade --rollback` reads the `version` of every proposal file once and refuses when one is newer than the target reads, naming the file (`Delete or move the proposal file before rolling back`). `--force` downgrades that refusal to a warning. A target that does not advertise `proposals` prints one warning and is not refused. An absent directory is silent. The Library workspace adds a Proposals segment (`/library?kind=proposal`) with a pending count on the kind switch, a detail panel that shows the stored diff with non-printable characters escaped, findings, evidence, and the verdict, and approve and reject controls; approve is disabled while the base is stale, while the target shows pin drift on the apply path, or while findings exist and the reason is empty. There is no UI create form. See [Proposals](docs/proposals.md).

## [1.0.0-rc.5] - 2026-11-07

### Features

- Packs can import hooks, and `gridctl skill project sync --kind hook` projects them to Claude Code only. REST registry, projection, and pin routes, and the Library Hooks segment, use the same engine. `POST /api/packs/{name}/apply` and `DELETE /api/packs/{name}` project and unsync hooks. `DELETE /api/registry/hooks/{name}` and `gridctl skill remove --kind hook` refuse when a projection is not retired (`hook <name> could not be unsynced on <client> (<action>); run 'gridctl skill project unsync --kind hook <name> --force' before removing`). A projection manager that cannot be built refuses that delete (REST 503 `Hook projection not available`, CLI exit 1 `hook projection not available`) and leaves the registry hook in place. `GET /api/packs/{name}` reports per-client hook rows the same way as `gridctl pack status`. The example pack `examples/portable-pack` ships `hooks/block-force-push` and requires gridctl `>=1.0.0-rc.5`. A hook is `hooks/<name>/hook.json` plus the scripts it names, selected by an opt-in `hooks:` list on `gridctl-pack.yaml` (pack repository only; empty means none). `gridctl pack add` scans the definition and every text file, skips that hook on any finding unless `--trust` (exit 1; other selected resources still import), copies an accepted directory to `~/.gridctl/registry/hooks/<name>/`, and pins the bytes at `~/.gridctl/pins/hooks/global.json`. `POST /api/packs` refuses the whole import with 409 before any write. A non-empty hook selection stamps `skills.lock.yaml` version 9. An older gridctl refuses that file. A rewrite of `project.lock.yaml` stamps revision 4, which records hook entries. A higher revision is kept. Sync copies scripts to `~/.gridctl/hooks/<name>/` and patches matcher groups into `~/.claude/settings.json` in exec form, leaving sibling bytes and comments unchanged. Gemini CLI, Cursor, Copilot, Windsurf, Antigravity, and OpenCode are reported as `skipped-unsupported` and are not written. A skill or agent may declare `metadata.requires.hooks`. Status and sync warn when that hook is not in-sync for the client. The warning does not change state or exit codes. `gridctl skill list|info|remove --kind hook` and `gridctl skill pins --kind hook` operate on that registry. Hook pins are not stack-scoped. `--stack` with `--kind hook` exits 2. `gridctl skill add` does not import hooks. See [Hooks](docs/hooks.md).

- Global context fragments, and the single `AGENTS.md` when fragments mode is off, are pinned trust-on-first-use. `gridctl ctx pins list|verify|diff|approve|reset` reviews that state. Pins live at `~/.gridctl/pins/context/global.json`, with approval history at `~/.gridctl/pins/approvals/context/global.json` (newest first, 10 kept). `approve` takes `--expect`, `--reason` on every approval, and repeatable `--evidence`. `gridctl upgrade --rollback` checks `~/.gridctl/pins/context/global.json` once against `context_pins` and warns instead of refusing. `ctx status` and `ctx sync` JSON gain a `pins` object (`drifted`, `missing`, `unpinned`); sync rows gain `pin_drift`. `GET /api/context`, `PUT /api/context`, and `POST /api/context/sync` gain the same object, and canonical and fragment saves gain `pin_status`. `gridctl info --json` advertises `schemas.context_pins`. Projection warns and continues on pin drift; existing `ctx` exit codes are unchanged. An unreadable or newer context pin file warns on `ctx status`, `ctx sync`, and the context document routes and does not change their exits or status codes; `ctx pins` and `/api/context/pins` still fail on that file. Approving a present fragment that has no pin record pins it. `gridctl doctor --security --source gateway:` now exits 1 while a fragment is in pin drift (`context.pin.continuity`). Releases before this one reject a security-report snapshot that contains the new checks. `gridctl.security-report.v1` is unchanged. `--source file:` reports the gateway-level context checks as unknown.

- Skill and tool pin approvals can record a reason and repeatable opaque evidence references. `gridctl skill pins approve` and `gridctl pins approve` accept `--reason` and `--evidence`. Each approval appends an entry (time, approver, bound hash, reason, and references, newest first, 10 kept) to `~/.gridctl/pins/approvals/skills/<stack>.json` or `~/.gridctl/pins/approvals/tools/<stack>.json`. Skill pin records gain `approvals` and `composite_hash`. Tool pin records gain `approvals`, and card-trust approvals record one bound entry inside the card transaction. `list --format json` and `diff` JSON include the history (`last_approval` on diff). `gridctl info --json` advertises `schemas.pin_approvals`. `gridctl upgrade --rollback` checks those logs against that key. The security report adds `skill.pin.approval` and `pin.schema.approval`, not fail predicates, with reason codes `approval_recorded` and `approval_unrecorded`. `gridctl.security-report.v1` is unchanged. An older binary rejects a snapshot whose checks use the new predicates or reason codes on import: `validateImportedReport` refuses unknown predicates and reason codes rather than ignoring them. Credential-shaped references, reasons, and approver names are refused at input and are not stored. An approver name longer than 256 bytes is refused the same way. Approve responses include `approved_by`, `approved_by_source`, and `evidence_count` for the entry that was stored.

### Bug Fixes

- Removing a skill resets its pin in each stack pin file the removal can update. `gridctl skill remove`, `gridctl pack remove`, `DELETE /api/registry/skills/{name}`, `DELETE /api/skills/sources/{name}`, and `DELETE /api/packs/{name}` all do this. A pack remove row says `registry entry removed, pin reset (<stacks>)` when a pin was reset, and `--dry-run` says `would reset pin (<stacks>)` without writing. A pin-directory listing error is logged and does not fail that preview. Source removal adds an optional `pins_reset` object, omitted when nothing was reset. The approvals log is left untouched. A pin-reset problem is logged and does not fail the removal. From the CLI, a running daemon is reset through its API. An API refusal, a state file that cannot be read, or a pin file that cannot be loaded leaves that stack in place. The log names `gridctl skill pins reset <name> --stack <stack>` when the API refuses. From REST, a stack whose daemon is running elsewhere is left in place and the log names the same command. A state file or pin file that cannot be read is left in place on that path too.

- A daemon-mode `gridctl apply` that fails during startup now says whether the gateway process exited or is still running. If the child exits, apply reports the PID, exit status, and a short tail of the daemon log (registered secrets masked) as soon as the process exits, instead of waiting out the full readiness budget. If the child is still running when the budget ends, the message names the PID, port, and last readiness response, and prints the `gridctl status`, `gridctl logs <stack> -f`, and `gridctl destroy <stack path>` commands. The process is left running. `gridctl serve`, and `gridctl apply` with no stack file, report the same two outcomes for the health wait, and name `gridctl stop` and `gridctl logs gridctl`. Exit codes and the 60-second and 30-second budgets are unchanged.

- `gridctl reset` and `POST /api/reset` now retire hook projections: one `hook` row per client, hand-edited groups kept unless `--force` (including under `--purge`), a hand-edited script copy kept on the default tier (`--purge` still deletes it with `~/.gridctl`), an unparsable settings file reported as `failed` and left untouched, the settings file and script copy in the pre-reset archive for a `would-remove` row, and unsync before `--purge` deletes `~/.gridctl`. A missing hook manager is a `skipped` row and exit 1. Reset rows gain an optional `script_dir` field. The text preview lists `skipped` rows with their detail and does not count them as recreatable. `--purge` help text says in-sync hook groups are retired and hand-edited groups are kept unless `--force`.
- `gridctl pack remove` and `DELETE /api/packs/{name}` keep a hook whose projection was not retired (`skipped-unmanaged`, `skipped-drift`, or `error`), list it in `kept`, and report the same refusal as `gridctl skill remove --kind hook`, instead of deleting the registry directory, pin, and pack record while the client still runs the script copy. `--dry-run` reports the keep instead of `would-remove`, so a preview of a blocked removal now exits 1 like the removal itself.
- `gridctl pack remove` and `DELETE /api/packs/{name}` now remove every fragment the pack record lists, even when it was never projected or its projections were already unsynced. A fragment whose store copy differs from what the pack installed is kept (`skipped-drift`, listed in `kept` as `rule/<name>`, exit 1) unless `--force`. A fragment that cannot be read is kept with an `error` row the same way, including under `--force`. `--dry-run` reports those keeps. A rule with no recorded provenance is kept until `pack add` records it or `--force` is passed. Compiled client documents are refreshed by the next `gridctl ctx sync`. If that removal leaves the fragment store empty, that sync reports that no fragments exist and leaves the compiled document in place. The web remove dialog's aftermath toast uses the first remediation on a kept row instead of always saying to adopt a skill or agent edit. Its first sentence still says the kept members were locally edited. The preview still lists only `skipped-drift` rows as kept, so an unreadable rule's `error` row is not shown there.
- The Library hook pin tab renders scan findings through the shared findings list, with the finding code, file and line, and matched pattern, and escapes non-printable characters in every finding field. The pack import review step escapes finding text the same way.
- A removed or empty hook script copy is restored on the next sync instead of being reported as a hand edit. `pack apply` counts `skipped-unsupported` hook rows in `Applied N/M` and does not exit 1 for those rows alone. Hook sync preserves a settings file's mode and does not write its backup wider than 0644. A missing `hooks` key is inserted as one pretty-printed value on its own line.
- The Library hook pin tab shows added, removed, and modified files and a line diff of `hook.json`. Approve stays disabled when the hook is not pinned.
- The example `block-force-push` script denies `git push --force` and `git push -f` when the flag follows the remote, and does not treat `--force-with-lease` or `--force-if-includes` as `--force`.

- Hook unsync removes the `hooks` key when gridctl's own removals leave nothing under it, so a `~/.claude/settings.json` that had no `hooks` key before `sync --kind hook` returns to its original bytes after `unsync --kind hook`, except a trailing comma on the previous last member, which sync consumed as a separator and unsync does not restore. `pack remove`, `skill remove --kind hook`, `reset`, and the REST unsync routes share the change. A `hooks` object that still holds a user group, a user event array, or any other member is left as it was. A settings file gridctl created is not deleted; it becomes an empty object.

- A registry hook whose `hook.json` no longer validates stays in `skill list --kind hook` and can be removed. `skill update` prints `Skipped: <name> (hook):` when a hook fails the scan or drift gate, and a named update exits 1. A re-import refreshes a script's executable bit. Approving a hook whose current bytes scan clean clears findings recorded earlier. Hook pins use `~/.gridctl/pins/hooks/global.json` so they do not share a file with tool pins for a stack named `hooks`.

### Security

- Hook import is gated by the danger-pattern scanner and a content pin. Any finding of any severity skips that hook on `gridctl pack add` unless `--trust` (exit 1; other selected resources still import). `POST /api/packs` refuses the whole import with 409 before any write. A finding accepted with `--trust` is stored on the pin. Approving later drift requires `--expect` and, when findings remain, `--reason`. Projection writes only approved pin bytes. An unapproved registry edit is skipped and does not change the settings file or the script copy. gridctl does not execute hook scripts. Hook pin routes, and the store's diff, approve, and composite-hash lookup, reject a name that is not lowercase letters, digits, and hyphens before reading the registry.

## [1.0.0-rc.4] - 2026-11-03

### Features

- The web UI names a blocked popout and keeps the source panel in place. The warning stays until dismissed. Open opens the same route in a new tab and does not clear the warning. A detached panel's button stays enabled and reads "Detached. Click to focus"; clicking it focuses that window, and a missing window clears the detached state instead of leaving a blank popup. Reloading the main window asks open popouts to report back. A reply restores the detached indicator without opening a new window, and a reply for a popout that is already detached does not close the Stack inspector. Preferences, in the header and the command palette, sets the theme, compact mode for the seven workspaces that use it, and text size for Library, Tools, Traces, and the log font. Text size still changes when the browser refuses the storage write; the new size stays in memory for the session and is not persisted. `?`, the command palette, and Preferences open a keyboard shortcuts panel grouped by the handler that actually fires each chord. Existing preference values move to `gridctl-pref-*` keys on load. Shared switch, badge, and button primitives replace the one-off controls, wizard tiles show a count only when one is known and positive, and the Fleet button explains why it will not open when the stack has no servers.

- The web UI uses Variables, Library, MCP server, Pin drift, and Create new in visible text. Error toasts name what failed, keep a short reason, and add a next step when the failure has one; hover shows the untrimmed text. Cancelled requests stay quiet. The authentication prompt names `gateway.auth` and `gridctl var get`, distinguishes a rejected credential, a refused Host header, and a gateway error, and confirms when the credential is saved. Forget credential in the header, and Forget gateway credential in the command palette, clear the saved entry for this origin. Detached windows do not show either control; they follow a forget from the main window.

- The web UI serves Outfit and IBM Plex Mono from the gateway instead of fonts.googleapis.com. Latin subset woff2 files (61,888 bytes) are preloaded from the same origin, and the SIL Open Font License texts are served at `/fonts/LICENSE-outfit.txt` and `/fonts/LICENSE-ibm-plex-mono.txt`. Micro labels use `text-3xs` (10 px at the default root) and `text-2xs` (11 px), so they follow the browser default font size. Nine-pixel and eight-pixel labels are now 10 px. The built UI grew by 70,293 bytes (3,303,008 to 3,373,301). The local binary grew by 73,950 bytes (81,087,522 to 81,161,472).

- The skill editor and both global-context editors update only the changed block of the markdown preview. Unchanged blocks keep their DOM nodes, so a text selection in the preview survives typing elsewhere in the document. A document that contains raw block HTML keeps the previous whole-document rendering.

- The skill editor and both global-context editors update the textarea before a long markdown preview catches up. Logs, Traces, Variables, and Tools keep each list row as-is across a poll when that row did not change. Relative timestamps still refresh on each poll. With relative time off, an unchanged Logs poll does not redraw rows.

- The web UI shell loads a smaller entry chunk. Popout pages, the creation wizard, the global context dialog, markdown rendering, and the YAML parser load on demand, and the React Flow and chart libraries are fetched only by the routes that draw them. While the tab is visible, the shell status cycle (status, tools, and clients) stays on a three second cadence, and inventory (catalog, pins, registry, telemetry, and spec health) runs every 15 seconds. A hidden tab runs that status cycle once every 30 seconds and pauses inventory and the other workspace polls. Inventory runs again on return, and again when status recovers from a disconnect, so Pins and Library clear the outage banner without waiting for the next 15 second tick. A spec fetch clears the last telemetry spec only on HTTP 503. A detached sidebar leaves its loading spinner once the first status attempt fails. Detached windows take status from the main window while it is visible and poll themselves when that window is hidden or closed. The detached sidebar and registry windows poll their own inventory every 15 seconds. The browser no longer requests `/sse`. Status polling is the disconnect signal.

### Fixes

- The web UI no longer swallows typing in the JSON variable editor, and Cmd/Ctrl+F reaches browser Find. Fit view and canvas zoom shortcuts apply only on the Stack workspace, and Cmd/Ctrl+A is no longer intercepted. The Refresh tooltip reads "Refresh (⌘⇧R)". Metrics and Connections show ⌘5 and ⌘9 chips in the command palette. The Variables list footer hints at j/k, Home/End, Enter, and e. Agent, skill, and Traces tabs move with the keyboard. The workspace switcher is a navigation landmark, not a tab list. In-shell pages expose one main landmark, a heading, and named regions. Status dots have accessible names and stay distinct under reduced motion. `/runs` and `/runs/:runID` redirect to the Runs tab at `/traces?view=runs`. The run id is not carried; the Runs view has no selection parameter.

### Bug Fixes

- Dragging a node on the Stack canvas no longer shifts the other nodes in its column into the vacated slot on the next status poll, in both compact and full card styles. (#1476)

- Light theme colors now follow `data-theme`. Wizard cards, borders, canvas checkboxes, chart series, trace spans, the autoscale sparkline, edge markers, and glows no longer keep dark-only white tints or hex values. The Connections Drifted badge uses `status-pending`; `status-warning` was never declared, so the badge had no background. New surface-overlay tokens cover the card tints, and new chart tokens cover the series hues. The raw-palette lint rule now rejects every default Tailwind color family, including chart files. In dark, the danger gradient's second stop is `status-error` at 80%, vault list tags use `secondary`, checked tool controls use the primary fill, shared canvas dots use the `border-subtle` token, and the command palette backdrop matches the modal. (#1368)

- Narrow windows no longer clip header actions or wrap the status bar onto itself. Workspace rails keep their declared pixel minimums instead of collapsing to a few dozen pixels, pack-sourced skill cards keep a readable name, and the Stack inspector, skill editor, global context split, and trace waterfall column resize with a pointer, including touch and pen. Row actions stay visible on devices without hover. (#1365)

- The create-server and create-stack wizards, vault prompts, and skill editor now associate each visible label with its control. Icon-only buttons on those forms and in the registry, zoom controls, trace waterfall, and global context dialog have accessible names. Field errors are tied to the control with `aria-invalid` and `aria-describedby`. The skill browse toggle is a checkbox. `IconButton` requires a name and renders `type="button"`. (#1363)

- Pins, the Library skills list, Tools, Variables, Connections, run history, and Logs now show a named failure and a Retry control when their request fails, instead of a spinner, skeleton, or teaching empty state. Pins, the Library skills list, Tools, and Variables keep loaded data and show a banner, so an unsaved Tools edit stays mounted. Run history and Logs replace the list with the failure. Connections shows "Connections unavailable" when no clients are loaded after a failed health fetch, and keeps that failure on screen while Retry runs. Before the health fetch settles, an empty client list shows "Loading connections…" instead of "No client registry available". When clients are listed and the wiring fetch failed before any rows loaded, the Ownership badge and Wiring section say "unavailable". An unconfigured registry or variable store, and pinning that is off, still show their empty states. A credential rejection still opens the auth prompt. Metrics already showed a failure and Retry; its title is now "Metrics unavailable". (#1361)

### Security

- Update the transitive frontend package decode-uri-component to 0.5.1. Versions through 0.5.0 match the high-severity denial-of-service advisory GHSA-qr28-fpjm-wq7f, which fails the frontend CI security audit. query-string already allows 0.5.1.

### Fixed

- Toasts announce through a polite live region. Errors and toasts that carry an action stay until dismissed. A timed toast pauses while the pointer or keyboard focus is on it, and every toast has a Dismiss control that sits above the status bar. Visible toasts are capped at five by dropping the oldest timed toast, then the oldest persistent one, so a new toast is always shown. A repeat of the same type and message keeps the one already showing. A paused toast stays paused with a fresh duration; otherwise its timer restarts. The creation wizard and the spec diff are labeled dialogs that restore focus on close. The variable set picker keeps Tab inside its portaled dropdown and closes on Escape instead of closing the wizard. Modal close, expand, and popout buttons have accessible names. The theme picker, telemetry pill, canvas overlays menu, log level filter, and log view options restore focus to their trigger on Escape and do not reopen when that trigger is clicked again. Enabling or disabling run recording, and clearing metrics, uses a confirm dialog. A failed run mutation keeps that dialog open and shows the error inside it, not also in the toolbar behind the dialog. The tools editor discard prompt moves focus to Keep editing and cancels on Escape. Keyboard focus is visible on the logs and traces grids, the wizard, and the command palette. Overlays use a named stacking scale so toasts paint above dialogs and portaled dropdowns paint above the wizard. (#1362)

- The web UI now asks before wiping run history, deleting a skill file, deleting a wizard draft, or removing a context fragment. Wipe history names the stack and the stored file count and size, and states that the wipe is stack-wide and ignores the current filters. Removing a context fragment saves a backup under `~/.gridctl/project-backups/context/`, outside the fragments directory. Deleting a skill file or a wizard draft does not save a backup. Skill and global context editors ask before discarding unsaved edits, including on reload or tab close. Closing the detached skill editor after save, discard, reset, detach, fork, or compare-take does not show a browser leave prompt. A skill that has just loaded its body is not treated as dirty. Escape on a confirm closes that confirm only. (#1360)

- `gridctl upgrade --list` sizes the TAG column from the longest retained tag (minimum 10) so pre-release tags such as `v1.0.0-rc.3` no longer run into ORIGIN. This matches the documented aligned-column behavior. (#1442)

- Downstream OAuth now uses `gateway.public_url` for the `/oauth/callback` redirect when it is set, so `redirect_uri` is `<public_url>/oauth/callback` (path prefix included) instead of `http://localhost:<port>/oauth/callback`. Dynamic client registration sends `application_type: web` for an https redirect and `native` otherwise. With `public_url` unset, the loopback redirect and `application_type: native` are unchanged. A non-loopback `http` `public_url` logs one warning and does not refuse startup. A pre-registered `client_id` must allow the new redirect URI on the provider. Changing `public_url` requires a daemon restart. The next login replaces a cached registration; run `gridctl auth reset` if the provider rejects re-registration. (#1439)

- The docs and the v1.0.0-rc.3 changelog entry previously said releases before `requires.gridctl` reject a stack that contains it. Those releases ignore the key. Strict unknown-key checking and the floor both start at v1.0.0-rc.3. The wording was corrected. (#1443)

- The Stack canvas opens a node inspector from the keyboard. Tab to a gateway, client, server, or resource node and press Enter or Space; Escape closes the inspector and clears the selection ring. Enter on the expand chevron or the Skills row acts on that control only. Each of those nodes announces its name, type, and status, the canvas controls have accessible names, and the gateway Skills row is a labeled button. The command palette lists `Inspect <name>` and selects that server. Identical status polls keep the same node and edge objects. A client link, unlink, or scope change still appears in the poll that returned it. Quiet edges no longer animate. Token-active servers animate at most 12 edges, and a selection animates only the highlighted path, drawn at 3px. Repeated node surfaces drop backdrop blur and `transition-all`, and the unused glass rules and permanent `will-change` rules are gone. In light theme, server, resource, and client nodes use the opaque surface they already declare. The gateway node, controls, and minimap are unchanged. (#1359)

## [1.0.0-rc.3] - 2026-10-27

### Deprecated

- Legacy span attribute keys (`server.name`, `mcp.server.name`, `mcp.server`, `mcp.replica.id`, `mcp.client.name`, `mcp.protocol.generation`, `mcp.tool.name`, `tool.name`, `output.format`, `mcp.autoscale.direction`, `mcp.autoscale.reason`, `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.usage.cache_read.input_tokens`, and `gen_ai.usage.cache_creation.input_tokens`) are still emitted beside the new keys and will be removed in 1.1.0. The UI fallback for the four `gen_ai.usage.*` keys goes away with that removal. `network.transport` keeps its key, but the values change immediately: `stdio` and `process` become `pipe`, and `http`, `sse`, `ssh`, OpenAPI, and A2A become `tcp`. `gridctl.transport` keeps the old label (`stdio`, `process`, `ssh`, `http`, `sse`, `openapi`, `a2a`). Dashboards that filter `network.transport` on the old values need to move now. The old values are documented until the same 1.1.0 removal.

### Security

- Update the frontend development dependencies Zod to 4.6.7 and Undici to 8.12.3 to address memory-exhaustion and denial-of-service advisories (GHSA-mrhx-c7xc-m3w5 and GHSA-xjj6-f33q-929c) that fail the CI security audit.

- The web UI markdown sanitizer is DOMPurify 3.4.17. Versions through 3.4.16 match the critical advisory range (GHSA-cg2g-xh64-v4q7 and GHSA-mrv2-3pqr-j5jr). Skill markdown still uses the same default sanitize call.

- Breaking: a non-loopback bind with no `gateway.allowed_origins`, or with `["*"]`, refuses to start. Set `gateway.allowed_origins` to the origins that load the UI, or pass `--insecure-allow-unauthenticated` / set `gateway.insecure_allow_unauthenticated: true`. The same override covers the existing unauthenticated-bind refusal. Under the override, startup continues with the wildcard and logs one warning for each skipped refusal. Loopback binds are unchanged: no refusal, `["*"]` default, no new warning. Enumerated public discovery GETs (`/mcp/server-card`, `/groups/{name}/mcp/server-card`, `/.well-known/ai-catalog.json`) are exempt from authentication and answer `Access-Control-Allow-Origin: *`. `gridctl doctor --security` and `GET /api/security-report` add `gateway.origins.startup` with the mode and discovery-route list, and no origin strings. This is not a fix for a stored-credential leak: CORS does not send credentials, and the web UI token stays in same-origin localStorage. The refused configuration and this migration ship in the next 1.0.0 release candidate.

### Features

- `gridctl info` advertises the schema versions this binary reads. `--json` adds `schemas` (`daemon_state`, `project_lock`, `import_lock`, `tool_pins`, `skill_pins`) on both the success path and the runtime-detection-error path. Text output adds a `Schemas:` line. Pin versions are strings. `gridctl upgrade` records that object on each retained binary: the replaced binary from its own constants, a newly downloaded binary from `<path> info --json` (10 second timeout, environment limited to `PATH`, `HOME`, and `GRIDCTL_HOME`). A store hit keeps the schemas already recorded and is not probed again. A missing `schemas` object, a non-zero exit, a timeout, or output over 64 KiB stores null. A present object that omits a pin field stores that field as empty, which rollback treats as unknown rather than version zero. `gridctl upgrade --rollback` refuses, before the confirmation prompt, when a daemon state file, tool pin file, skill pin file, project lock, import lock, stack `requires.gridctl` floor (including a parent-only floor), or pack floor recorded in the import lock is newer than the target advertises. Exit 2, one line per problem. `--force` prints those as warnings and continues (exit 0). A target with `schemas: null` prints `warning: compatibility unknown: <tag> was retained by a release that does not advertise schemas` and continues. A missing or unparsable stack file is a warning, not a refusal. An unparsable `requires.gridctl` string is a refusal. The floor check follows `extends` up to 10 files and does not expand variables; a URL parent is a warning. `gridctl-pack.lock.yaml` is not checked. Daemon state files gain optional `binary_version` (no schema bump). `gridctl status` adds a VERSION column, empty when the field is absent, and one stderr line per running daemon whose version differs (`warning: <stack> runs gridctl <version>; this CLI is <version>. Restart it: <command>`). An empty `binary_version` is not a mismatch. `--json` adds `binary_version` and does not print that warning. `gridctl doctor` adds `daemon.version` (`ok` when every running daemon matches or none are running; an empty `binary_version` matches; `warn` on a mismatch or when state files cannot be read). After every successful upgrade or rollback, running daemons are listed with a restart command, and the summary prints `Run again with --restart to do this now.` Pass `--restart` on that same command. It cannot be combined with `--list` or `--check`. A later `gridctl upgrade --restart`, once that binary is current, prints `gridctl is up to date.` and restarts running daemons from recorded state without downloading or replacing the binary. A later `gridctl upgrade --rollback --restart` starts another rollback when a previous binary is still retained. `--restart` performs the listed plan: service-owned daemons through the supervisor, plain and stackless daemons by invoking the newly installed binary. Flags that were not recorded are named and are not reproduced, including a non-default `--base-port`, which returns to the default. On macOS the service line is `gridctl upgrade --restart (launchd label dev.gridctl.<name>)`, because restart is `launchctl bootout`, `bootstrap`, and `enable`. A service record whose stack file disagrees with the daemon state, or that cannot be read, is treated as a plain daemon. An empty stack file is restarted with `serve`. A restart failure after a successful swap exits 1 and names the daemons that were and were not restarted.

- `gridctl upgrade` on a standalone install keeps the replaced binary and the installed binary under `~/.gridctl/versions`. The default is two entries (the active release and the previous one). `--keep` is the count for that run. Active and previous are never pruned, so `--keep 1` still leaves both. `--keep 0` replaces the binary and does not write or prune the store. A later `gridctl upgrade --version <tag>` uses a retained copy when its checksum matches and prints `Using retained <tag> (no download)`. A mismatch downloads again. Reinstalling the running tag from a verified store entry installs those retained bytes and does not overwrite the store entry or the recorded previous tag. `gridctl upgrade --rollback` restores the previous retained binary offline. `gridctl upgrade --rollback <tag>` restores that tag. Rollback prompts unless `--yes`. `--list` marks active with `*` and previous with `p`, lines the columns up under the header, prints sizes with one decimal place above bytes (`61.5 MiB`), and names directories it will not delete. The footer count is the number of entries, not `--keep`. `--list --format json` includes `schema_version`, `active`, `previous`, entries, and `orphans`, and exits 0 when the store is empty. Exit `0` is success, including a rollback that printed warnings. Exit `1` is a failed download, verification, swap, or restart. Exit `2` means nothing retained, the named tag is absent, a retained entry whose file is missing, or a pre-swap refusal. A missing store prints `nothing retained; run gridctl upgrade first`. An absent tag, or a manifest entry whose binary file is missing, prints `<tag> is not retained (gridctl upgrade --list)`. The missing-file row stays until the next write. A dev build is not retained. Homebrew installs still print `brew upgrade gridctl/tap/gridctl` and do not list or roll back unless `--force`. Windows refuses `--rollback` (exit 1) and skips retention with one stderr line. `gridctl reset --purge` removes the store. Each retained entry is one copy of the gridctl binary. A retained release that does not advertise schemas prints `warning: compatibility unknown: <tag> was retained by a release that does not advertise schemas` and still rolls back. Entries retained before schema advertisement have `schemas: null`. A newer manifest is refused and is not rewritten. A tag, active value, or previous value that is not a single directory name is refused and is not rewritten. `--rollback` and `--version` together are a usage error.

- Add optional `requires.gridctl` on `stack.yaml` and `gridctl-pack.yaml`. The value is a semver constraint the running gridctl must satisfy (`>=1.1.0`, for example). A string that does not parse is `must be a semver constraint (e.g. ">=1.1.0")`. `gridctl validate` enforces the constraint in the file it reads and does not resolve `extends`. Apply, serve, and reload enforce the merged parent and child floors. Two simple floors merge as the child constraint, then `, `, then the parent. When either side uses `||`, the merge is the OR of each child clause AND each parent clause, so a version that satisfies only one side is refused. The error is `requires.gridctl: stack requires gridctl <constraint>, running <version>`, and it names `gridctl upgrade --version v<floor>` when the constraint has no `||` and the highest `>=` clause satisfies the whole constraint. A leading `v` on the running version is not shown. `gridctl validate` exits 1, and `--format json` carries `field` `requires.gridctl` with `severity` `error`. A `dev` build checks the syntax and skips enforcement, logging one warning. Override files and booster fragments cannot carry the key. A composed pack stack keeps the base stack's floor. Releases before this one ignore it in both a stack file and a pack manifest (fail open); the floor is enforced only from this release on. `pack add` refuses an unsatisfied or invalid floor before import, including `--dry-run` (exit 1). The pack message is `pack <name> requires gridctl <constraint>, running <version>` and has no upgrade hint. A passing floor, including one skipped on a `dev` build, is recorded. The key does not stamp a new `skills.lock.yaml` version. An older reader drops it on the next write. `pack apply` checks the recorded value and projects nothing when it is unsatisfied or invalid (exit 1), including `--dry-run`. A pack imported before this field existed is not checked until `pack add` records it. Preview appends the refusal to `warnings` and returns the rest of the document. It starts nothing. A lock error after the floor check returns that error instead. `POST /api/packs` and `POST /api/packs/{name}/apply` return 422 for that refusal. Omitting the key changes nothing.

- Add optional `gateway.tool_discovery` (`mode: full|deferred`, default `full`, and `always_expose`). Deferred mode returns `search_tools`, `call_tool`, and the pinned set from `tools/list` on both protocol generations. `search_tools` pages full definitions. `call_tool` dispatches one tool through the ordinary path, so client scope, whitelist, groups, policy, rate limits, schema pins, and MRTR apply to the inner call and not to the outer frame. A policy-blocked tool is still listed. Direct calls by canonical name remain allowed. `gridctl link --discovery`, `link.discovery`, `gridctl project sync --discovery`, and `POST /api/clients/{slug}/link` write a `discovery` query parameter. Handshake sessions freeze it at initialize. Stateless requests and `subscriptions/listen` streams honor it per request or per stream. Both notification generations fingerprint each session or subscriber from its effective mode, including when code mode is also on. `tool_discovery` hot-reloads without container work. `GET /api/status` and `gridctl status --json` add optional `tool_discovery` (omitted when full). The human status table is unchanged. A stateless client continues an `input_required` round trip by retrying `call_tool` with the standard `requestState` and `inputResponses` fields. Handshake sessions keep the existing cross-era error. Omitting the block leaves `tools/list`, initialize, and listen acknowledgements unchanged. (#1392)

- OpenTelemetry span attributes now use the MCP semantic conventions where those conventions name a key (`mcp.method.name`, `gen_ai.tool.name`, `gen_ai.operation.name=execute_tool`, `error.type`, `network.transport`, `server.address`, `server.port`) and a `gridctl.*` key otherwise. The root tool-call span is kind SERVER and is named `tools/call <tool>` after routing. The downstream span is kind CLIENT. A `traceparent` in `params._meta` or in the HTTP headers becomes the parent of that span, and every downstream transport receives `traceparent`, `tracestate`, and `baggage`. The trace provider sets `service.name` from the stack name. Opt-in `gateway.metrics.otlp` exports operation-duration histograms and call, token, replica, and drop counters over OTLP HTTP/protobuf. Both blocks are read at startup; a change requires a restart. Span names are not a versioned contract. A real Grafana or Datadog ingest was not exercised.

- Serve `subscriptions/listen` to stateless clients on `/mcp` and `/groups/{name}/mcp`, and consume it from stateless HTTP downstream servers that declare `tools.listChanged`, `prompts.listChanged`, or `resources.listChanged`. The stream acknowledges the honored subset, sends a keepalive comment every 30 seconds, and delivers coalesced `notifications/tools/list_changed`, `notifications/prompts/list_changed`, and `notifications/resources/list_changed`, each tagged with the listen request id. Tool events use the existing client-profile and group fingerprint. Prompt and resource events follow server visibility, and registry changes are visible to every subscriber that opted in. `resourceSubscriptions` is not honored. At most 16 streams are open per access identity and 256 overall. The stateless `server/discover` result now advertises `listChanged` for tools, and for prompts and resources when those capabilities are advertised. Handshake `initialize` is unchanged. A missing or non-object `params.notifications` is `-32602` at HTTP 200. Over either cap, the response is `-32603` at HTTP 200 (`listen stream limit exceeded: 16 per client` or `listen stream limit exceeded: 256 overall`). Downstream listen streams reconnect with backoff (1 second, doubling, 30 second cap, reset to 1 second after an acknowledged stream) and refresh tools on every acknowledged connect; a later connect also reindexes resources when that kind was requested. `subscriptionStream` is `open` only while that stream is acknowledged and reading. Unregistering a prompt or resource server still notifies a group subscriber that could see it. `/api/mcp-servers` and `gridctl status --json` add optional `subscriptionStream` (`open`, `closed`, or `unsupported`). An SSE URL pinned to `protocol_generation: stateless` is included. Handshake, OpenAPI, A2A, stdio, and process servers omit it. A schema-pin block does not open a stream. Stateless stdio and process servers are not consumed. The Stack sidebar shows the listen stream state. The human status table does not add a column. No `stack.yaml` field changes. Open the listen stream before the first list; nothing is replayed.

- Add registry Skills over MCP (`io.modelcontextprotocol/skills`) with `skills/list`, `skills/get`, and `resources/directory/read`, plus file reads under `skill://registry/<name>/`. `resources/list` adds a `SKILL.md` entry beside each legacy entry. Handshake declaration and the three methods require client opt-in; stateless discovery advertises `directoryRead: true`. Entries carry served frontmatter and SHA-256 manifests; canonical `SKILL.md` rendering omits `state` and matches skill pins. Nested skills have independent roots and are excluded from parent manifests, deviating from the extension's nested prefix and inclusion rules. Symlinks are now skipped by pins and manifests; existing pins that included links can drift. Review them with `skill pins diff`, then `skill pins approve --expect`, before approving the new composite hash. Replace required linked content with regular files inside the skill directory. Dotfiles, `.tmp` files, and irregular files are not served. Manifests over 512 entries or 16,777,216 bytes become `dynamic` with a warning, while files remain readable. `skill list --json` adds `manifest.digest`, `manifest.files`, `manifest.bytes`, and `manifest.dynamic` when the manifest can be hashed, and `manifest_error` when it cannot. An unreadable skill is omitted from `skills/list` with one warning per registry generation; the other skills are still listed. `gridctl skill list` keeps that row and exits 0. `/api/status` and gateway entries in `status --json` add `extensions`. Global exposure policy is applied before hashing, and disk refresh applies to the new methods. Legacy prompts, `skills://registry/` reads, projections, and stack YAML remain unchanged.

- OAuth review corrections: metadata paths return 405 for unsupported methods even with the embedded UI, and malformed group metadata paths remain protected. Client rate limits match resolved OAuth profile keys, preserving exact subject aliases; unlisted principals need server- or tool-scoped default coverage. JWT media-type comparisons are case-insensitive. These fixes align the new mode with its documented contracts. (#1388)

- Add optional `gateway.auth.type: oauth` resource-server mode with issuer discovery, JWT verification, RFC 9728 metadata, required scopes, and claim-derived client identity. Configure an issuer and audience, map raw case-sensitive subjects through profile aliases, and use `GRIDCTL_GATEWAY_TOKEN` for CLI access. A non-empty variable overrides state-recorded credentials as Bearer in every auth mode; unset it to use saved shared credentials. OAuth sessions are bound to their creating principal; declared client labels no longer select authorization profiles in this mode. To migrate, remove `token` and `header`, configure issuer fields and subject aliases, and restart. OAuth settings require a restart. With the variable unset, bearer and API-key modes retain their existing behavior. (#1388)

- `pack add` reports an extends lock mismatch: a different base repo is unresolved, a different commit is a `lock:` note, and `--locked` refuses before any write. `pack lock` accepts an `ssh://` username with no password and still refuses https userinfo and ssh passwords. Preview returns `409` `lock_stale` for a stale lock under `locked: true`. A newer pack lock is `409` on add and preview. A booster dry-run reports `would-start`, `would-replace`, `would-reload`, or `unchanged`.
- Add pack composition and `gridctl-pack.lock.yaml`. A booster declares `stack: {extends: <base>, file: <fragment>}`. `pack add` validates the fragment (servers, resources, and variables only) and pins it under the booster checkout. `pack apply` of the base or any booster composes every attached fragment in pack-name order into `~/.gridctl/packs/<base>/composed/stack.yaml` and starts, replaces, or reloads the base daemon on its existing port. The first attach replaces the running base; later fragment edits reload. `pack remove` of the last booster replaces back onto the pinned stack. Removing a base with boosters attached exits 1 unless `--force`. `gridctl pack lock` writes the lock; `pack add` honors it, `--locked` fails when it is stale or missing, and `--ignore-lock` resolves refs. `skill update` refuses a lock-pinned source unless `--ignore-lock`. `skills.lock.yaml` stamps version 8 only when a pack record has `lock` or `fragment`, or a member has `locked_by`. A scalar `stack:` with no sources is unchanged. REST accepts `locked` and `ignoreLock` and returns `409` `lock_stale`; it does not start, stop, reload, or write the lock. `gridctl plan` prints composed layers. A committed lock does not pin the pack repository's own commit.

- Add `secrets.providers` and `${secret:<provider>/<path>}`. `exec` runs an argv with `{{path}}` replaced inside each element, never a shell. `onepassword` runs `op read --no-newline op://<path>` with an allowlisted environment and an optional `token_file` (absolute or `~/`, mode 0600 or stricter, symlinks rejected). References stay literal in validate, plan, export, and the API. Apply and reload resolve them, exclude a failed workload, and record a value-free registration failure that recovers on `gridctl reload` or the health-check cadence. A reference used only by a disabled server or resource is not resolved and is not started by recovery. A whitespace-only path is a validation error (`secret reference path is empty`). Provider subprocesses receive an explicit environment and do not inherit the daemon environment when that list is empty. A failed gateway token, bind, or origin fails the deploy. A rotated gateway token on reload is `restart_required`. The daemon state file still stores the resolved gateway token at mode 0600, as it does for `${var:}`, and nothing else resolved is persisted. `gridctl var explain secret:<provider>/<path>` reports consumers without the value. `gridctl var scan --file` adds resolved provider values as scan candidates. `GET /api/secrets` and the Variables workspace list references and status without values. A stack that contained `${secret:` as an ordinary literal now validates as a secret reference. There is no escape; rewrite that string if it was not meant to be a reference. `plan` does not detect rotation of an unchanged reference. Manual acceptance against real `op` (service account and Connect, including `token_file` on macOS and Linux) remains.
- Add `gridctl service install`, `status`, `logs`, and `uninstall`. Install writes a user-scoped systemd unit or launchd agent that runs the gateway in the foreground, and records it as lockfile kind `service` before the file is written. Lockfile revision stays 2, so an older gridctl keeps the entry and does not remove the unit. On Linux the unit comes back at the next user-manager start (login, or boot when linger is enabled). macOS launch agents start at login, not at boot. `GRIDCTL_VAULT_PASSPHRASE_FILE` names a user-owned passphrase file (mode 0600 or stricter, at most 4 KiB) so an encrypted store can unlock at start; gridctl never creates that file. `apply -f` and `serve -f` now record a daemon state file for their lifetime, so `gridctl status` lists them and `gridctl destroy <name>` stops them. `destroy` stops a gateway the supervisor owns through the supervisor instead of killing the process, and leaves the unit enabled. A daemon the supervisor does not own is still stopped. Re-running install on a service the supervisor is running restarts it through the supervisor. A unit file gridctl did not record is refused unless `--force`, which backs it up first. A missing `systemctl` or `launchctl` is exit 2. `gridctl reset` removes installed services before daemons. Not supported on Windows. `sd_notify` readiness and `LoadCredential=` are follow-ups.
- Add an optional top-level `policy:` block that blocks or allows a listed tool by canonical name on every `tools/call` path (`/mcp` on both protocol generations, group endpoints, code-mode inner calls, `gridctl call`, and `POST /api/tools/call`). Rules are last-match-wins, which differs from the `skills:` deny-then-allow order. `block_destructive` reads the downstream server's own `destructiveHint` as indexed at the last refresh, not group override hints, and a never-started server has no annotations yet so its first call is allowed unless an explicit rule matches. `require_approval` is parsed and reported as authored and enforced as block until approvals exist. On `/mcp`, group endpoints, and code-mode inner calls, a denial is an in-band `isError` result beginning `Error: tool blocked by policy`, not `-32602`. It does not reach the downstream server, consume a rate-limit token, or cold-start a scaled-to-zero server. `gridctl call` exits 1 with `outcome.reason` `policy_blocked` and `outcome.gate` `policy`. `POST /api/tools/call` returns HTTP 200 with `error.code` `policy_blocked` and the safe message `tool blocked by policy`. Run records use reason `policy_blocked` under stage `gate`. A policy-only edit hot-reloads without container work. `GET /api/policy` reports the block and per-tool verdicts, including whitelist-hidden tools with `exposed: false`. The Tools workspace shows a `BLOCKED` or `APPROVAL` chip, and the gateway inspector shows a read-only summary. `gridctl doctor --security` adds `policy.declared.gateway`, which is not a fail predicate. Listing stays the whitelist's job. The block is not inherited across `extends`. Releases before this one ignore the key, so a downgrade fails open. A release with strict decoding that does not know the key rejects the file. The change is additive under Articles VIII and IX. Omitting the block leaves dispatch unchanged.
- Add optional `enabled:` on MCP servers and resources (default true). A disabled server is validated but not built, started, registered, health-checked, or pinned. It is absent from tool, prompt, and resource lists, and `gridctl status` shows STATE `disabled`. `gridctl enable` and `gridctl disable` rewrite the stack and reload a running daemon. `PATCH /api/mcp-servers/{name}` does the same when the write target is the base file, and returns `409 override_required` without writing when the target is an override. Reload JSON adds optional `disabled` and `enabled`. A `${var:KEY}` consumed only by disabled workloads no longer blocks apply. Adding `enabled: true` is not a reload change. Disabling does not reset schema pins. Host ports from `--base-port` skip disabled container servers; the flag remains a starting port only.
- Add `stack.override.yaml` beside a stack file, and `~/.gridctl/overrides/<pack>.yaml` for a pack-carried stack. `--override` and `--no-override` on `apply`, `serve`, `plan`, and `validate` select or suppress it. The override may set `enabled`, `env`, `tools`, `replicas`, `restart`, `ping_timeout`, `ready_timeout`, `output_format`, and `headers` on servers that already exist. It cannot add servers or change gateway bind, auth, or origins. `enable` and `disable` on a pinned checkout write the override. `export` notices a sibling `stack.override.yaml` and emits the base unless `--with-override` is passed. A pack override under `~/.gridctl/overrides/` is not part of that discovery. Omitting `enabled` and the override file leaves existing stacks unchanged.
- Add optional `headers:` on external URL and OpenAPI servers. Each value is sent on every downstream HTTP or SSE request, and on OpenAPI operation requests and the readiness HEAD, next to `auth:` or the brokered `Authorization` header. The auth header wins when the same name is set in both. The OpenAPI spec fetch does not receive them. Values expand `${VAR}` and `${var:KEY}`. Reserved names and an `Authorization` entry that conflicts with top-level `auth:` are rejected. `openapi.auth` is not that conflict; it still wins `Authorization` at request time. Values are registered with the log redactor and omitted from traces, run records, and status. Probe errors scrub them. Export refuses an inline literal and does not print the value. A short literal is masked everywhere that string appears in logs, the same way an environment value is. `gridctl import` maps client header maps into `auth:` plus `headers:`. A lone non-credential header such as `X-Tenant` now lands in `headers:` instead of `auth.type: header`. The server form has a Custom headers list, and Discover tools sends the configured headers. A headers change reconnects that server. `gridctl plan` reports `headers changed` without values. Omitting `headers:` changes nothing.
- Publish SEP-2127 Server Cards at `GET /mcp/server-card` and `GET /groups/{name}/mcp/server-card`, and an AI Catalog at `GET /.well-known/ai-catalog.json`. Cards are public read-only metadata. They are advisory, never a trust or credential source, and they do not change pins. Optional `gateway.description` and `gateway.public_url` are additive. Unset, they change nothing except the card. The advertised origin is `public_url`, a loopback Host on a loopback arrival, or a Host in `gateway.allowed_hosts`. Otherwise `remotes` and catalog entries are omitted. `POST /api/servers/card` and `gridctl add <url>` consume a card into a stack draft with `${var:KEY}` placeholders and no credential values. `--no-card` probes and writes a plain `url` block. An off-origin remote is refused. The wizard's Detect from URL action prefills transport, URL, and auth the same way. SEP-2127 is Final. The extension discovery notes and the AI Catalog specification are drafts. The vendored schema commit in `pkg/servercard/testdata/NOTICE.md` is the contract.

### Fixed

- `GET /ready` returns 503 while a declared MCP server is still spawning or completing its handshake, and 200 only after every declared server has registered or had that attempt recorded. This fixes the documented contract (200 only when all MCP servers are connected and initialized) and adds no field or flag. `GET /api/mcp-servers` and `gridctl status` list that server as not initialized while it registers.

- A pathless `gateway.tracing.endpoint` such as `http://localhost:4318` now posts to `/v1/traces`, matching the documented collector URL. Any other path is still used as-is. This is a fix of the documented contract, not a new configuration field.
- `gridctl.gateway.replicas` reports the replica count for each server and state. `mcp.client.operation.duration` is recorded only when the downstream call was made. Export rejects an inline literal in `gateway.tracing.headers` and `gateway.metrics.otlp.headers`. An OTLP export failure that does not name its signal is counted as `unknown` rather than `traces`. A header-derived span link stays on the outer root and is not copied onto a code-mode inner call.
- Registry REST file GETs and Skills over MCP share a deterministic MIME table. Images (`.png`, `.jpg`, `.jpeg`, `.gif`, `.svg`, `.webp`), `.txt`, `.html`, `.css`, `.js`, `.toml`, `.xml`, and `.pdf` now receive explicit content types instead of `application/octet-stream`. Existing mapped types, including `.md` as `text/markdown`, are unchanged; unknown extensions keep the binary fallback.

### Documentation

- The process-wide exact-value redaction registry now covers `logging.RedactString` as well as slog records, so a `${var:}` value registered with the gateway is redacted in trace attributes too.
- Document the list-ordering guarantee for `tools/list` (including group endpoints and the code-mode `search` exception), `prompts/list`, `resources/list`, `resources/templates/list`, and `GET /api/tools/discover`. Behavior is unchanged.
- Sync the agent guidance, project status, threat model, and feature guides with the amended Constitution (#1376). Articles III, IV, VIII, and XII are summarized as amended: coverage is per exported behavior, real stand-in processes are allowed in integration tests when the external service cannot run in CI, "breaking" is limited to machine-readable contracts and failing stack files or invocations, and secure defaults are stated as the loopback-bind and widened-bind rules the gateway enforces. Notes that labeled v1.0.0-rc.2 log-wording and human status-text changes as breaking now say those are not versioned contracts. The threat model's Article XII section now records the widened-bind origin refusal. The previous open gap, a wildcard fallback on every bind, is closed.

### Maintenance

- Fix the scheduled NPM Audit Fix workflow to open a PR for compatible updates when advisories remain, report the remaining count, and still fail on npm execution or registry errors.
- Pin the MCP conformance suite to `@modelcontextprotocol/conformance@0.2.0-alpha.12` and score each protocol generation against its frozen requirement set. The CI job also runs a Docker-free fleet (two mock servers, two active registry skills, and `/groups/dev/mcp`) and is a verifier lane, so a skip fails the job. Baseline entries are per-check where one check fails, each with a reason. The three 2026-07-28 scenarios that stay unscored still run one at a time under a strict baseline.

### Bug Fixes

- Build with Go 1.26.10 so govulncheck no longer reports GO-2026-6809 in crypto/x509 (Go 1.26.9). The language version stays 1.26.0. Suppressed Docker client findings are unchanged.
- `gridctl enable` and `gridctl disable` write a daemon-reported override at its absolute path, so a relative `--override` cannot create a file in another working directory. A pack checkout always writes `~/.gridctl/overrides/<pack>.yaml`. A different `--override` is refused. `PATCH /api/mcp-servers/{name}` refuses a pinned checkout even when the daemon was started with `--no-override`. A concurrent edit exits 1 and says to re-run. Reload and enable text print Disabled and Enabled. A prepare failure while enabling a server is retried on the next reload. Duplicate keys in an override are rejected. A new override file is block style. Disabled status rows use `http`, `stdio`, `sse`, or an empty transport, not server kinds.

- Skill import now installs every file in a skill's own directory. Files previously excluded (a root helper such as `pcap_utils.py`, or any path outside `scripts/`, `references/`, and `assets/`) now install, scan, count, pin, and project. A skill whose `SKILL.md` sits at a git clone root keeps the three-directory allowlist and reports other root entries it left out in one warning (at most five names). Dot-prefixed entries are never installed in either layout, and a dot entry at the clone root is not named in that warning. Re-import of an own-directory skill replaces the whole directory except `SKILL.md`, its backups, dot-prefixed files, and any directory that contains its own `SKILL.md`. A nested registry skill is left in place and is not reported as removed. Other removals are reported. A repository-root re-import still prunes only the allowlist, and reports those removals the same way. `fileCount` now counts every installed supporting file. A skill directory whose previously ignored files exceed the existing per-file (5 MiB), per-skill (500 files), or total (50 MiB) caps now fails import with the cap named, where it previously imported without those files. This changes the documented three-directory behavior toward the Agent Skills specification. `docs/skills.md`, the threat model, and the API reference are updated in the same change. It ships in the next 1.0.0 release candidate under Article VIII's candidate window.

- Breaking: stack YAML decoding rejects unknown keys, and a group `tools:` entry or `overrides` key that names a tool outside its server's non-empty `tools:` whitelist is a validation error (`tool '<server__tool>' is hidden by mcp-servers[<server>].tools and can never be called`). An unknown key in an extends parent is reported as `extends[<file>].<path>`, so the line is read in that file. A comment-only file still decodes as empty. A tools whitelist save that fails candidate load for any reason other than a hidden-tool reference, including a missing vault variable or an unreadable extends parent, returns 400 and does not write. A profile `tools:` entry that names one is a warning (`the profile may be wider than the whitelist on purpose`). A group `exclude` entry that names one is a warning (`excluding a hidden tool has no effect`). Fix or remove the unknown key. Leftover removed fields (`gateway.default_model`, a per-server `model:`, top-level `client_models:`, and `limits.budgets`) are unknown keys and must be deleted; they used to be ignored. `gridctl validate --policy` does not apply this check. Drop the group reference or add the tool back to the whitelist. `description` is now an accepted server field and appears in `gridctl status --json` and `/api/mcp-servers` when set. `gridctl validate` JSON issues add optional `line`. `invalid_candidate` adds optional `errors`. The tools whitelist endpoints add optional `issues` on `400 hidden_tool_reference` and refuse the write. This ships in the next 1.0.0 release candidate.
- Breaking: the per-server `tools:` whitelist is now enforced on `tools/call` for every MCP path (`/mcp`, group endpoints, and direct calls with code mode on), not only `gridctl call` and `POST /api/tools/call`. Before this fix a client sending a hidden tool's prefixed name reached the downstream server. Hidden and unknown tools return the same unknown-tool error. On handshake sessions the text for an unknown server and for an unprefixed name changes from `Error: unknown server: ...` and `Error: invalid tool name format: ...` to `Error: unknown tool` (the stateless `-32602 Unknown tool: <name>` response is unchanged; the code-mode names `search` and `execute` are not part of that collapse, because they stay callable when code mode is on and still fail as an invalid tool name when it is off). Both a group `tools:` entry and a `clients.profiles.<id>.tools` entry that names a whitelist-hidden tool now fail at call time. Code-mode `search` and `execute` remain callable on the MCP path when code mode is on; `execute` of a hidden tool is rejected. Call admission follows the last tool-index refresh: a name can appear on `tools/list` and still be rejected until that refresh finishes. A never-started idle-to-zero server has no filtered surface yet, so a configured `tools:` list is what blocks a hidden name from cold-starting; a visible tool still cold-starts. `gridctl call` and `POST /api/tools/call` on a never-started idle-to-zero server now cold-start a visible tool instead of returning unknown tool. This brings the gateway in line with the documented whitelist contract; the changed handshake error text is a wire-visible result change and ships in the next 1.0.0 release candidate (#1372).
- `gridctl plan` and `GET /api/stack/plan` compare the stack file against the configuration the daemon loaded, so an in-place edit is reported instead of `No changes`. `GET /api/stack/health` derives its drift block from that comparison, and the spec-mode overlay follows it. The status-bar badge still shows validation only. Planning the daemon's own file now exits nonzero when the daemon is running but does not answer, instead of printing `No changes`. Store-backed `image`, `url`, and `network` changes are reported as `image changed`, `url changed`, and `network changed`, without values. This brings `plan` in line with its documented behavior; the nonzero exit on an unreachable daemon is an exit-code change and ships in the next 1.0.0 release candidate. Callers that treated an in-place edit as up to date, or an unreachable daemon as success, should read the diff and the nonzero exit (#1373).

## [1.0.0-rc.2] - 2026-10-12

### Removed

- **Breaking (Experimental):** `gridctl models` and its subcommands (`init`, `edit`, `validate`, `render`, `sync`, `status`, `ack-restart`, `adopt`, `unsync`) are removed, along with the Model routing dialog, the `/api/project/models/{status,validate,sync,adopt,ack-restart}` routes, the `models` lockfile kind, and the `litellm-contract` Gatekeeper job. `model_preferences:`, the Library model chips, and the gateway's MCP tool router are unchanged. Synced LiteLLM and OpenCode files keep working, because gridctl was never on the inference path. The surface was Experimental and shipped only in the `1.0.0` release candidate. The removal is breaking under Article VIII; the release vehicle is the maintainer's decision. Before upgrading, run `gridctl models unsync` on the previous release (`--force` for hand-edited targets). Afterwards, remove the `include:` line, delete `gridctl-models.yaml`, remove the `provider.<id>` subtree from `opencode.json`, delete `~/.gridctl/models/`, and optionally delete `kind: models` entries from `~/.gridctl/project.lock.yaml` (they are otherwise inert). `gridctl reset` no longer removes those targets. `--purge` deletes the policy and the lockfile, but not the LiteLLM or OpenCode files. See [Cleaning up a removed model routing policy](docs/troubleshooting.md#cleaning-up-a-removed-model-routing-policy).

### Features

- Let a pack select skills and agents from named external git repositories. `sources:` in `gridctl-pack.yaml` declares each repository once (`repo`, `ref`, `path`, and an `auth` block without `ssh_key_path`). A selection is still a string, or `{name, source}`. `gridctl pack add` clones each source in name order, pins the resolved commit, and records member lock sources keyed `<pack>/<source>`. A source that fails to clone, is itself a pack, or whose import fails stays unresolved (exit 1); the rest import and the pack record is still written. `--vault-key` and `--auth-token` apply to the pack repository only. `--ssh-key` also applies to an SSH source with no other auth. `--source-auth` and REST `sourceAuth` override one source and persist a vault reference or key path, never a token. `pack status` adds `source` rows. A lockfile that records external sources or a pack member is stamped version 7. An older gridctl refuses that file, including unrelated skill updates. Remove the pack with this build before downgrading. Packs without `sources:` are unchanged. `skill update` of a member skill refreshes the recorded names only. A `skill add --source-name` that points that member key at a different repository is refused. Pointing a declared source at a new repository requires `pack remove`, then `pack add`. An empty `skills:` list still skips a skill another source already owns. A failed source exits 1 even when no selection names it. Printed source URLs drop embedded userinfo. A repeated skill or agent name is rejected when `sources:` is declared. There is no index (#1339).
- Add per-server `configs` and stack-relative volume sources for container servers. `configs` copies inline `content` or a stack-relative `file` into each static, reloaded, and autoscaled replica at create time, with optional `mode`, `uid`, and `gid`. It is rejected when `execution.mode` is `hardened`, because Docker refuses an archive copy into a read-only root; seed an engine-local volume under `execution.mounts` separately. A resolved `${var:KEY}` in `content` is written into the container layer and is readable by anyone with engine access, the same as a resolved environment variable. Export keeps the authored reference, does not bundle `file` entries, and prints a notice when those entries exist. Both additions are optional. Omitting them changes nothing. Relative volume host paths (`./`, `../`, and `~`), which `gridctl validate` previously accepted, were rejected by Docker and resolved by Podman against the Podman service's working directory. They now resolve against the stack file directory (`~` against the home directory) when the stack is loaded. Absolute paths and named volumes are unchanged. Resource volumes gain the same expansion and resolution. Stacks that relied on the old Podman resolution should move those files next to the stack file or replace the source with an absolute path. `gridctl plan` and `gridctl validate` still show and check authored volume strings; they do not open `file` entries. A change to a referenced file's bytes is not detected until the stack file changes or the server is recreated. Under Articles VIII and IX, schedule the relative-volume resolution change for a maintainer-owned major release, not a patch or minor release (#1337).
- Add `gridctl import <client> --kind` so one invocation can import servers, skills, agents, and context from one client install. `--kind` accepts `servers`, `skills`, `agents`, `context`, and `all`, plus the singular aliases. Omitting `--kind`, or passing exactly `servers`, keeps the previous stdout, stderr, and exit codes. A multi-kind run prints one summary table, or one JSON document, and stops at the first nonzero kind code without rolling back earlier writes. A server row counts as imported only after the stack write; a failed or cancelled write is skipped. Context is never overwritten: an existing canonical file is skipped, a fragments-mode home is skipped, and `--context-fragment` is the explicit way to add a fragment (the first fragment still migrates the single-file store). A kind the client does not support is a skip row. Additive under Article VIII: no stack schema, lockfile, or on-disk format change, and no major-release scheduling is required for `--kind` (#1335).
- Discover project-scoped MCP configs with `gridctl import` for Claude Code, VS Code, Cursor, Roo, Gemini CLI, Zed, and OpenCode. Discovery starts at the working directory, or `--project-dir`, and stops at the nearest git root. `--scope all` (the default) reads Claude Code local scope, then project files, then `OPENCODE_CONFIG` when set, then user files. `--scope user` reads user files only. `--scope project` reads project files only and does not open a home file. The order is gridctl's choice: it matches the documented rules for Claude Code, Roo, Gemini, and OpenCode. Cursor does not document a replacement rule, and Zed gates project servers on worktree trust, which gridctl does not consult. `.mcp.json` is one source for Claude Code and VS Code. `${workspaceFolder}` and `${CLAUDE_PROJECT_DIR}` (including a `:-` default) are replaced with the project directory the relative path was resolved against. A local-scope row uses the matching `projects.<dir>` directory. Under `--scope all`, Claude Code's user file uses the discovery start directory. `--scope user`, other user files, `OPENCODE_CONFIG`, and `--source-config` leave those placeholders in place. Other client placeholders such as `${input:...}` are skipped. A non-OpenCode `cwd` at project or local scope is skipped; at user scope it imports with a warning. OpenCode `cwd` is still skipped at every scope. Text output prints a `Sources:` block and labels each client with its scope. JSON adds optional `origins`, `scopes`, and source `scope` and `clients` fields. Schema version stays 1. `--source-config` reads exactly one file for any named client. `--scope user` keeps the previous candidate set, skip reasons, and exit codes, but not the exact text: the sources block replaces the OpenCode source line, and a non-OpenCode `cwd` warns. A project file that points at this gateway is still filtered as the gateway's own entry until #1295. A project file whose path is the same as that client's user file is listed once, as the user row. A readable home file with no `projects` object produces no local row. An empty `--scope project` scan says no project files were found. A same-name collision warning names the kept definition after identical origins have been merged. Output and selection changes, including the `--scope user` text changes and the project-scope `cwd` skip, require a maintainer-owned major release and must not ship in a patch or minor release (#1333).
- Import skills and agents from a local directory with `gridctl skill add <dir>`, and from Claude Code, OpenCode, and agents home locations with `gridctl skill import <client>`. Local origins record the resolved path and a content hash, with no commit. `gridctl skill update` re-reads that path, including agents in a mixed source, and does not call git. A lockfile that contains a local source is stamped version 6; files without one keep their previous stamp. An older gridctl refuses that file, including unrelated skill and pack commands. Remove the local sources with this build before downgrading. `POST /api/skills/sources` accepts an absolute daemon-host directory and rejects relative paths and paths inside the gridctl home. `GET /api/skills/sources` adds `kind`. `gridctl export --output` omits local sources from `skills.yaml` and prints a notice, because those paths are not portable. OpenCode agent files are listed and skipped. Git-only import, pack, and lock behavior is unchanged (#1331).
- Advertise `tools.listChanged` to handshake clients and push `notifications/tools/list_changed` on each affected session's GET stream when that session's visible tool list changes. Changes are coalesced (250 ms quiet window, 2 second maximum latency) and scoped to the session's client profile and group. Code mode and the stateless generation are unchanged. Handshake downstream servers that emit the same notification are refreshed off the reader, pin-checked, and then propagated upstream. HTTP and SSE coverage is limited to notifications that arrive on a POST response stream. `/api/mcp-servers` and `gridctl status --json` add `capabilities.toolsListChanged` for the downstream declaration. The human status table does not add a column. The Stack sidebar badges that declaration as tools list changed. Group sessions receive the event on `GET /groups/{name}/mcp`. `GET /mcp` sends response headers before the first event or keepalive. Additive and non-breaking under Article VIII: no `stack.yaml` field changes, and the stateless wire surface is unchanged (#1329).
- Let a pack carry a stack. `stack:` in `gridctl-pack.yaml` names a stack file in the same repository. `pack add` pins a checkout under `~/.gridctl/packs/<name>/<commit>/` and refuses inline credentials and an `extends` chain that leaves the repository. `pack apply` starts that daemon before wiring clients, `pack status` reports whether it matches the pin, and `pack remove` stops a daemon whose state file is under the checkout. A resolved stack, or an unresolved stack that records a detail, stamps the whole `skills.lock.yaml` at version 5. An older gridctl refuses that file, including unrelated skill updates. Remove the pack with this build before downgrading. Packs with neither keep their previous stamp. REST and the web UI do not start or stop the daemon: apply returns `skipped-unavailable`, and deleting a running pack-owned daemon leaves the daemon, the checkout, and the pack record in place so `gridctl pack remove` can finish the job. Packs without `stack:` are otherwise unchanged (#1318).
- Aggregate downstream `prompts/list`, `prompts/get`, `resources/list`, `resources/read`, and `resources/templates/list` with registry skills on both protocol generations. Downstream prompts are named `<server>__<prompt>`. Resource URIs are not rewritten. Client profiles and groups hide out-of-scope servers as not found. Optional `protocol_extensions: ["io.modelcontextprotocol/ui"]` makes the gateway declare the MCP Apps client extension downstream. Handshake-era `resources/read` not-found now returns `-32002` with `data.uri` instead of `-32603`. Callers that treated every handshake read failure as `-32603` should accept `-32002` and read `data.uri`; stateless not-found remains `-32602`. `/api/mcp-servers` and `gridctl status --json` add prompt, MCP resource, template, capability, collision, and list-error fields. The human status table adds `PROMPTS` and `RESOURCES` only when a count is non-zero. `limits:` and run recording still cover `tools/call` only. A handshake `input_required` on `prompts/get` names the prompt. A read miss refreshes resource lists within the 10-second deadline. An upstream disconnect is not stored as `resourceListError` (#1317).

- Add experimental outbound A2A 1.0/0.3 JSON-RPC tools behind the off-by-default `experimental.a2a` flag. Mandatory card-byte trust, origin-bound credentials, secret context/task capabilities, and per-conversation Bedrock sessions guard send/get/cancel and advisory skill calls. Results retain atomic JSON and handles under configured output limits, with explicit semantic content truncation. Handles expire after 24 hours and are lost on replacement/restart; labels and run history cannot recover them. A2A-only stacks need no container runtime. Fixture coverage does not establish hosted compatibility or shared-agent isolation (#1185).
- Add gateway-owned capability primitives with shared bounded accounting, generation retirement, expiring context/task authority, and independent same-task send/cancel slots. The A2A adapter uses payload-free observations that retain locally estimated numeric token usage while skipping payload-bearing observers and external token counters. Diagnostic sanitation recognizes typed capability handles; code-mode logs exclude source excerpts and thrown values. The store has no user-facing tuning fields (#1185).
- Breaking diagnostic output: typed capability strings in recorded names, labels, and errors are masked, and code-mode failure logs use local categories. Nested structured log attributes containing recognized secrets may become sanitized JSON strings. Migrate diagnostic consumers to generated attempt/trace IDs and outcome categories, and accept that attribute shape; caller-facing code errors remain available. Schedule this diagnostic-output change for a maintainer-owned major release under Article VIII (#1185).
- Add `gridctl call` and `gridctl tools search` for invoking and discovering live tools on a selected running gateway, plus `POST /api/tools/call` and `GET /api/tools/discover`. Calls use authenticated daemon state, a default `cli` caller label, optional `--as`, strict canonical catalog admission, and typed outcomes with exits 0/1/2. Discovery is read-only lexical search. REST success is not proof of upstream MCP protocol negotiation; lost calls are not retried (#1154).
- Add a downstream Python 3.12 MCP runtime base recipe (`ghcr.io/gridctl/mcp-runtime-python`) with locked-project and hashed-requirements derivatives, snapshot-pinned tini init, UID/GID 10001, and a dedicated OCI workflow that scans and attests index and platform SBOMs before promoting aliases. The image is a foundation, not a ready server; generated Python 3.10-3.13 builds are unchanged. Supported tags wait on hosted amd64/arm64 acceptance, candidate publication, and anonymous evidence (#1230).
- Add opt-in metadata-only persisted run records for returning tool-dispatch attempts, including early denials. Recording uses a bounded queue and single writer, independent of tracing and metrics. Query through `gridctl runs`, `GET /api/runs`, and the Runs tab beside Traces using a shared versioned envelope. Default retention is seven days and 100 MiB per stack, including the active file. Wipe is stack-wide and is not secure erasure. Previous-round links require an authenticated generated attempt ID. Recording is best-effort; names and caller-declared labels may still be sensitive (#1155).
- Add opt-in `gridctl validate --policy` for offline evaluation of captured stack declarations against a versioned six-rule policy file. Flag absence preserves ordinary validation; an explicit empty, missing, or invalid policy fails without falling back. The evaluator reads only the selected policy, entry stack, and bounded local extends under the candidate root, without environment, secret, registry, runtime, or user-home resolution. YAML merge keys, same-file duplicate names, and symbolic-link path components are rejected. Dynamic image and URL values that can change execution class stay unknown under local and SSH prohibitions. Acceptance is not deployment admission. Empty selections and all-N/A results are errors. Unknowns stay distinct from violations. Source-built servers remain excluded from digest claims. Literal `*` tool names warn without becoming globs. No API, UI, or runtime enforcement is added (#1226).
- Add opt-in `gridctl validate --check-mutable-refs` for local, deterministic diagnostics of literal image tags and `npx`/`uvx` selectors. Default CLI output and exits, REST validation, health counts, apply, and stack schema are unchanged. Informational `reference-not-assessed` findings and a coverage note are shown even when there are no warnings, including unknown wrappers; exit zero is not complete coverage. npm partial versions and wildcards are mutable; additional `npx --package` and `uvx --with` selectors are assessed conservatively. Digests are content-addressed, not publisher-verified, and an exact package version is not a transitive lock (#1229).
- Add a passive security evidence report on `gridctl doctor --security`, `GET /api/security-report`, and existing gateway/server detail surfaces. Source selection is explicit (`file:`, `snapshot:`, or `gateway:`) with no fallback, probes, scans, or pin mutation. Gateway credentials attach only to loopback origins. Imported snapshots cannot authenticate verification claims. Exit zero means no established failures among documented fail predicates, not that the stack is secure (#1224).
- Add opt-in per-MCP-server execution declarations, container control admission and instance-bound evidence, local environment inheritance and executable lookup, lossless execution form/YAML preservation, and per-replica reporting. Omitted execution declarations retain compatibility behavior; selected profiles refuse unavailable required evidence. Supporting resources and remote execution remain outside the enforcement scope (#1221).
- Breaking output change: existing process-replica and runtime resource status gains execution metadata, and human replica rows distinguish execution state. Migrate STATE-text consumers to `status --json` and evaluate per-replica evidence separately from MCP health. Under Article VIII, schedule this output change for a maintainer-owned major release, not a patch or minor release (#1221).

### Documentation

- Refresh the README, CLI help, and release metadata so they lead with version-controlled agent setup and distinguish gateway linking from resource projection. Replace the placeholder quick start with a verified `mcp-server-fetch` 2026.8.18 tool call, give releases without live-tool commands a clear stopping point, add a concepts page for the stack, pack, skill, agent, rule, registry, projection, and link vocabulary, and correct pack, skill-authoring, projection, and registry-example guidance (#1356).
- Document downstream prompt and resource aggregation, `protocol_extensions`, server-level client and group visibility, status counts, the handshake `resources/read` not-found code, and the MCP Apps example. A server with no aggregated tools is never a group member. `resourceListError` covers resource lists only (#1317).
- Record the September 21, 2026 genuine generic A2A 1.0 and hosted AgentCore bearer rehearsal, including agent/runtime identities, card digests, workflow outcomes, session observations, and shared-memory limitations. Link the evidence from the README, A2A example, security guidance, and development conventions. Final exact-PR-head external acceptance remains required before merge or manual-merge handoff; rehearsal and CI fixtures do not close that gate (#1185).
- Document A2A fields, tools, capability bounds and secrecy, credential origins, bounded card freshness, immutable card/identity pins, and downstream shared-memory limits. Add example setup and private-file CLI guidance, link the feature from the README and indexes, and distinguish remote task states from completed MCP/REST invocations and CLI exits (#1185).
- Document capability bounds, expiry, uncertainty, teardown accounting, and sensitive-observer behavior, including local token estimates and diagnostic-recognition limits. Align usage, configuration, API, CLI, security, examples, and architecture guidance with diagnostic migration and payload-free counting boundaries (#1185).
- Document `gridctl call`, `gridctl tools search`, live-tool versus catalog search, REST call/discover envelopes and limits, typed exits, no-retry behavior, lexical matching, REST-versus-MCP verification limits, and the caller-label trust boundary (#1154).
- Document the Python MCP runtime base contract, writable-state mounts, evidence layers, tag policy, licensing notices, and maintenance ownership, and cross-link wizard help, Python source examples, and troubleshooting (#1230).
- Add a practical security guide covering verified installation, local and remote access, credentials, execution restrictions, content review, exports, diagnostic privacy, and declaration checks, with an explicit source baseline and release-availability distinctions (#1219).
- Document the adversarial scenario index and post-suite Go JSON execution verifier, including what a passing case proves and does not prove. This is test accounting, not comprehensive security testing (#1227).
- Document offline stack declaration policy: source-root limits, unknown and N/A handling, declaration-versus-runtime scope, warning exit handling, trusted checker/policy selection, descriptor identity for policy exclusion, and a CI workflow design that pins actions, verifies checker digests outside the candidate tree, and does not execute pull-request code as the checker (#1226).
- Pin runnable example stacks and public setup snippets to reviewed image digests and exact package versions, including the README Claude Desktop `mcp-remote@0.14.2` bridge. Document placeholder exceptions and quarterly pin review, and add `task examples:refs` plus Gatekeeper enforcement that fails unpinned or unassessed in-scope selectors without making unrelated example warnings fatal (#1229).
- Document the passive security evidence report, including no-fallback source selection, unknown/partial/N/A states, allowlisted identifiers, and exit-zero limits. No completeness or certification claims (#1224).
- Make the security-evidence example stack valid for `gridctl validate` by giving the container server a port and using a store reference for the gateway token instead of an unset environment interpolation (#1224).
- Document execution schema/defaults, CLI/API evidence, accepted reload failure recovery, runtime support limits, and local-process hygiene across references, examples, the threat model, and architecture guidance. Positive rootless Podman acceptance remains a required external gate (#1221).
- Align gateway-auth lifecycle guidance across the threat model, restart troubleshooting, API/CLI references, browser recovery, examples, and architecture map (#1228).
- Publish a source-verified security threat model covering trust boundaries, current controls and defaults, credential custody, release evidence, and residual risks (#1218).

### Bug Fixes

- Reuse an existing container when the engine expands a Docker Hub image name, such as `alpine:latest` to `docker.io/library/alpine:latest`. A different registry, tag, or digest still replaces the container. An execution revision still replaces it.
- Build with Go 1.26.9 and golang.org/x/net v0.60.0 so govulncheck no longer reports 13 symbol-reachable findings in Go 1.26.6 and x/net v0.59.0 (GO-2026-6599, GO-2026-6600, GO-2026-6603, GO-2026-6604, GO-2026-6605, GO-2026-6607, GO-2026-6608, GO-2026-6609, GO-2026-6610, GO-2026-6611, GO-2026-6612, GO-2026-6613, and GO-2026-6617). The language version stays 1.26.0. Suppressed Docker client findings are unchanged.
- Answer server requests on HTTP and SSE POST streams and return the response that matches the call, instead of treating the first event with an id as the result. A handshake-generation `ping` is answered with an empty result; every other handshake-generation server request gets `-32601 Method not found`. Notifications stay a DEBUG `server notification skipped` line and do not change the result. The call returns when its own response arrives, even if the server leaves the stream open. The reply POST is closed without reading its body, so an event-stream answer cannot stall the call or replace the session. A failed reply is logged and does not fail the call. A stateless server request is logged and not answered. Stdio, local process, OpenAPI, and A2A are unchanged. Unsolicited server requests on the GET listener stream, keepalive pings included, are still unhandled (#1327).
- Restart a managed stdio MCP container after it exits, instead of leaving the replica in `restarting`. The health monitor starts that container, re-attaches, and returns it to rotation with the existing exponential backoff. `restart:` defaults to `always`, including a container stopped with `docker stop` or `podman stop`, because gridctl cannot see the runtime's manual-stop flag. `on-failure` and `on-failure:N` (N from 1 through 4294967295) limit which exits restart and how many monitor-initiated attempts run; a larger N fails validation. `no` waits for a manual restart or re-apply. A container the runtime no longer has is reported as terminal rather than retried. Hardened replicas still pass admission before the existing 5-second stop timeout. Autoscaled servers are not restarted, and status omits `restartPolicy` for them. Status and the API add `restartPolicy`, `restartExhausted`, and `containerRestarts` on managed stdio that is not autoscaled. `state` stays `healthy`, `unhealthy`, or `restarting`, and human STATE text is unchanged (#1325).
- Record container stderr, exit code, OOM, and retry state on gridctl's own surfaces. Stderr lines are WARN `server stderr` entries in the log ring, so Logs, `GET /api/logs`, and `GET /api/mcp-servers/{name}/logs` show them after redaction. A container stderr line over 1 MiB is logged truncated and later lines are still captured. A failed container health check adds `replicas[].exit` (`code`, `oomKilled`, `finishedAt`, `status`, optional `error`). `status` is the runtime string, passed through unchanged. Docker and Podman compat inspection both reported `exited` for a process that exited 3. Reconnect does not attach unless the container is running, so a Podman compat attach cannot reset an exited container to `created` and clear the exit code. Unhealthy servers add `stderrTail` (at most 10 lines). `gridctl status --json` also carries `lastError`, `lastCheck`, and `lastHealthy` per replica. The Stack node shows amber `restarting` with the attempt count and next retry, and the inspector shows exit fields and recent stderr beside execution evidence. Healthy replicas with no exit record are omitted from that block. Rejected stdio requests (`server request rejected`) and dropped notifications (`server notification dropped`) are DEBUG lines without params, as are skipped HTTP and SSE no-id notifications (`server notification skipped`). Those lines are absent unless the gateway log level is debug. Verbose stderr can crowd the 1000-entry ring; `gridctl logs --server` still reads the runtime while the container exists. Tools workspace coloring is unchanged and still shows `error` for every unhealthy server.
- Breaking output change: `gridctl status --replicas` appends `; exited <code>` and ` (OOM)` when an exit is known to the STATE cell. The rollup table is unchanged. Migrate STATE-text consumers to `status --json`. Under Article VIII, schedule this output change for a maintainer-owned major release, not a patch or minor release.

- Persist the `--ssh-key` path (never key material or a passphrase) on skill and pack import, for both the CLI and REST, and reuse it on later updates when no auth is supplied. A later `pack add` with no auth flags also reuses a stored `--vault-key` reference. Literal and piped tokens still persist nothing. Lockfiles that record an ssh key path stamp schema version 4; files without those fields keep their previous stamp, including `version: 3` for pack variable declarations (#1316).
- Report an error when a skill freshness check cannot fetch or authenticate, instead of treating the recorded commit as current. An unreachable or unauthenticated remote now produces an error for that source instead of "already up to date". This supersedes the offline-degrade note from #1010 for `skill update`. A named update exits 1; a bulk update warns, continues with the other sources, and exits 0. `skill add` still serves cached content with a warning when a re-clone fetch fails. Under Article VIII, a previously successful offline `skill update <name>` now fails; schedule that behavior change for a maintainer-owned major release (#1316).
- Forward non-text `tools/call` content blocks and tool-definition fields that were previously dropped. Image, audio, resource link, and embedded resource blocks now carry their payload, and `tools/list` now carries `_meta`, `icons`, `execution`, unknown annotation keys, and downstream titles. Usage token counts remain text-only (#1315). Under Article VIII, schedule this output change for a maintainer-owned major release, not a patch or minor release.
- Import native OpenCode local MCP entries as exact command arrays, with `environment` mapped to stack env and whole-value `{env:NAME}` references converted to `${NAME}` for later stack expansion. Unsupported `{file}` expressions, embedded expressions, invalid env names, disabled servers, and working directories are skipped with value-free reasons. The disabled-server and working-directory skips apply to every OpenCode shape, including remote entries and string commands. Automatic import reads one file under the Gridctl home (`opencode.json`, or `opencode.jsonc` only when JSON is absent) and does not merge `config.json`, project files, or OpenCode override variables. Use `--source-config` to select another file, including a non-default XDG path (#1294).
- Breaking: `gridctl import` text output now includes per-entry skip reasons and the selected OpenCode source, and JSON output may include optional `source_path`, `source_paths`, and `sources` fields. Schema version stays 1. `source` and `found_in` remain client slugs. `servers` stays null when nothing is enumerated. OpenCode automatic import now reads `opencode.jsonc` when `opencode.json` is absent, so a previously empty import can import servers. OpenCode entries with `enabled: false`, or with a working directory, are now skipped for remote entries and string commands as well as command arrays; those shapes were previously imported and a working directory was dropped. It still does not match OpenCode's merged loader. Select another file with `--source-config`. Migrate text consumers that assumed silent skips, and JSON consumers that reject unknown fields. Under Article VIII this output and selection change requires a maintainer-owned major release and must not ship in a patch or minor release (#1294).
- Respond to keepalive pings from container and local-process stdio MCP servers so healthy sessions remain connected (#1304).
- Generate future Homebrew casks with a supported, macOS-only declarative post-install step instead of the deprecated `postflight` hook. The step removes quarantine from the staged binary, preserves command failures, and does not run xattr on Linux. These casks require Homebrew 6.0.13 or later; users on older versions should run `brew update`. The already-published 1.0.0-rc.1 cask is unchanged (#1290).
- Retry transient release archive and checksum download failures in the installer, with bounded retries and request timeouts, so temporary HTTP 504 responses do not immediately fail installation.
- Release unused A2A capability reservations after a plain HTTP 424 refusal, matching JSON-RPC refusals. Correct the experimental flag summary to describe the callable adapter (#1185).
- Classify A2A declarations as non-image exclusions for `explicit-image-digests`, matching OpenAPI. Preserve the error for evaluations with no applicable checks (#1185).
- Keep a successful verified-test run at exit 0 when Gatekeeper supplies `--summary` without `--capture`, so the EXIT trap cannot replace a passing verifier with a failed job (#1227).
- Count Go test JSON identities exactly, so sibling subtests whose names share a slash prefix (HTTP route cases such as `/api/traces` and `/api/traces/{traceId}`) do not fail the unit-lane scenario verifier (#1227).
- Match unqualified local image tags to Podman's `localhost/` RepoTags so a locally built image is not pulled from a remote registry (#1230).
- Match locally cached images by RepoDigests so digest and tag-plus-digest references hit the cache instead of comparing RepoTags only (#1229).
- Reconcile equivalent image references during hardened container reuse and verify Podman's bridge-mode network membership separately from its namespace mode. Keep exact network inventories, loopback publication, and fresh kernel evidence mandatory (#1221).
- Decode explicitly present null Podman capability lists as empty native sets while rejecting omitted or malformed fields. Full instance identity and all five kernel capability sets remain mandatory before routing (#1221).
- Create hardened Podman workloads through its native API to disable undeclared automatic writable tmpfs mounts. Preserve explicit scratch inventories, accept bounded copy-up scratch semantics, and report value-free mount mismatch subconditions (#1221).
- Verify native Podman capability sets when inspection expands an all-capabilities drop, compare equivalent tmpfs options semantically, and identify mismatched execution fields without exposing raw engine values. Instance-bound kernel evidence remains required before routing (#1221).
- Use native Podman cgroup-v2 capability evidence when its Docker-compatible resource flags are incomplete; retain instance-bound kernel checks before protected workloads can route tools (#1221).
- Keep failed execution evidence visible in server details when per-control observations are unavailable. Reject nested null mount/tmpfs fields in execution JSON instead of treating them as omitted defaults (#1221).
- Reap exited MCP child processes promptly, clear their reported PID, and allow shutdown and request cancellation to interrupt blocked stdin writes. Failed process reinitialization closes the replacement child (#1221).
- Interrupt blocked container stdin writes during cancellation and teardown. Keep successfully initialized gateway-owned processes alive beyond the initiating request, retire every client on unregistration, and reap late spawns from retired autoscalers (#1221).
- Report unavailable startup security preflight when manual reload is not ready, without incorrectly requiring `--watch` (#1228).
- Breaking: reject reload and stackless initialization when effective gateway authentication, bind, Host/Origin allowlists, or insecure override differs from startup. Mixed edits are rejected before application; saved YAML remains on disk. Restart the gateway with its original startup options to activate the change. This correction requires maintainer-owned major-release scheduling under Articles VIII/IX and must not ship in a patch or minor release (#1228).
- Support Bearer and API-key browser credentials, including supported custom headers. Verify drafts before persistence, retain usable credentials on non-auth failures, pause protected requests on gateway rejection, and refuse redirects. Versioned localStorage and explicit window-only notices cover detached windows; scripts on the gateway origin can read stored credentials (#1228).
- Discard Connections health results after navigation or credential replacement, pause session polling during credential re-entry, and stop interrupted connection changes without automatically replaying them (#1228).
- Limit the Homebrew cask's quarantine removal to macOS so Linuxbrew installations no longer fail by invoking the unavailable `/usr/bin/xattr`.

### Maintenance

- Add an explicit Linux-x86_64 regression that runs the pinned GoReleaser against the production-derived Homebrew cask configuration and checks generated archive hashes. Offline Python discovery now rejects legacy, duplicate, unguarded, wrong-token, and missing postflight-step output without downloading or invoking GoReleaser. This detects the current compatibility defect but does not change the generated cask (#1290).
- Add a declarative adversarial scenario index and a post-suite Go JSON execution verifier on Gatekeeper unit, integration, and Podman lanes. Designated invocations use `-json -count=1 -race`. The verifier checks that required identities ran and passed; it does not prove the security properties those tests assert. Hosted Podman execution remains required for Podman-assigned cases. Not a comprehensive security test suite (#1227).
- Add a manually triggered, read-only Homebrew authentication diagnostic that compares the release Python helper with GitHub CLI and reports sanitized results without exposing credentials.

## [1.0.0-rc.1] - 2026-09-09

### Documentation

- Link external release verification from installation and CLI guidance, clarify verifier diagnostics and immutable-mode prerequisites, and document separate release-policy tests (#1220).
- Correct stack export's README and stability claims, add migration and output-failure guidance, and demonstrate YAML, JSON, and directory exports with the local-process example (#1222).
- Correct the API reference's grouped-auth exemptions and remote gateway setup guidance, and clarify credential requirements in group and client-linking documentation (#1223).
- Python source examples now include a practical daily stack that builds the Fetch server from an exact PyPI release and the Time server from a commit-pinned project in the official MCP servers monorepo. The guide explains source pins, generated command selection, and build and runtime network behavior.

### Security

- Binary release tooling now requires exact-commit validation, draft-first assembly, authenticated archive and inventory evidence, and external verification before publication. Homebrew advancement follows public asset verification. Installer and updater origin verification remains external; no first covered production version is assigned before release acceptance. The vulnerability wrapper also rejects incomplete or malformed scanner output (#1220).
- Breaking: stack exports now preserve authored references without resolving environment or stored values and reject recognized inline credentials and sensitive default/replacement operands. Exports reread configuration rather than reconstructing runtime state. Replace inline credentials with authored references and supply recipient variables separately; review unclassified literals before sharing. CLI artifact preflight protects source files, and the Stack spec view provides a separate export action. Verbose diagnostics no longer dump resolved configuration. Under Article VIII, this correction must not ship in a patch or minor release; no release version is assigned here.
- Configured gateway authentication now covers grouped MCP and SSE routes, including initialization, discovery, calls, stream replay, and session deletion. Grouped clients that omitted credentials must send the same bearer token or API-key header required by `/mcp` on every request. Public UI files, probes, terminal CORS preflight, and the exact state-validated downstream OAuth callback remain public; no-auth loopback, valid credential formats, and all supported MCP protocol generations remain unchanged (#1223)
- Variable names beginning with `GRIDCTL_`, plus `OP_CONNECT_TOKEN` and `OP_SERVICE_ACCOUNT_TOKEN`, are now reserved for gridctl bootstrap and control-plane credentials. New store writes reject them, imports skip them with key-only warnings, exports and variable-set injection omit legacy entries, and local MCP processes no longer inherit them from the gridctl daemon. `${var:...}` and `${vault:...}` references to reserved keys remain literal and return a distinct resolution error without falling back to the ambient environment. Ordinary environment interpolation of non-credential `GRIDCTL_*` values remains supported. This is a compatibility-sensitive security boundary for the next major release; remove legacy entries with `gridctl var delete KEY --force` after moving any downstream credential to a non-reserved name (#1186)

### Bug Fixes

- Release source binding explicitly peels annotated tags to commits, and the backend release gate has a 15-minute timeout to accommodate policy tests (#1220).
- The Stack spec view preserves the export API's value-free field path and corrective action on failure instead of showing only a generic HTTP status. Failed exports still produce no download (#1222).
- The Library no longer claims that intentionally external `scripts/`, `references/`, or `assets/` paths are missing from a complete imported skill package. New imports record that all managed supporting-file trees were evaluated, while legacy and local skills retain the missing-file warning.
- Container stdio MCP servers no longer report a phantom allocated host port during apply, reload, or autoscaling. Stdio workloads publish no ports, while HTTP/SSE workloads continue to receive sequential published ports among themselves.
- Documentation now covers scoped variable delivery across the README, architecture map, REST wire shapes, stack schema inventory, and variable examples. The remote-gateway example no longer instructs users to store a reserved `GRIDCTL_*` control-plane key.
- A downstream MCP server that is unreachable when gridctl registers it (at startup, via hot-reload add or modify, or via a failed restart) is no longer dropped for the life of the process. Retryable failures enter a pending-registration loop driven by the health monitor: an immediate scan when the monitor starts, then attempts on each cycle gated by exponential backoff (capped at 30s), each preceded by a short reachability probe so a still-down endpoint costs about two seconds rather than a full ready window. Status output shows the failed server with a `retrying in Ns` hint instead of a terminal-looking error, and `POST /api/mcp-servers/{name}/restart` (the UI Restart button) on such a server forces an immediate attempt instead of returning 404. Removing the server (reload or unregister) drops the retry state, and a generation guard makes sure an attempt already in flight can never re-register a server that was removed or replaced meanwhile. Authorization failures (owned by the OAuth login flow), configuration errors (which fail identically every time), and container HTTP/SSE servers whose container was removed after a readiness timeout stay out of the loop. The practical effect: the strict "backends first, then gridctl, then AI clients" start order is gone - declared servers that come up late join the gateway automatically (#1180)

### Features

- The create-server wizard now supports generated Python containers for exact PyPI packages and packaged Git or local sources, including backend-resolved package versions, advanced Python inputs, server-level command and mount serialization, and lossless Python source round trips through Expert YAML mode. Eligible catalog PyPI entries retain host `uvx` by default and offer an explicit container toggle, with required mount details for filesystem inputs. Review shows the shared backend build plan and generated Dockerfile, waits for registration instead of reporting an append as deployment success, links to server-filtered logs, and both server sidebars show matching Python source and image provenance.
- The REST API now validates individual wizard resources, resolves bounded and cached public-PyPI version inventories, previews immutable Python build plans, and returns the exact generated Dockerfile from the shared builder implementation. MCP status payloads identify generated Python containers and expose their declared pin, actual versioned image tag, and image-recorded commit or artifact provenance.
- Python source containers now have complete CLI support: `gridctl plan` resolves first-class build actions with immutable source identity, image tag, and cache state, while `--show-dockerfile` exposes the exact generated file in text and JSON output. Resolution failures remain visible beside the config diff and block apply. Apply and reload emit structured per-server phases and INFO image-build diagnostics. `gridctl add --container` writes exact structured PyPI or credential-free commit-pinned GitHub sources without changing the default host `uvx` catalog path, and `gridctl doctor` warns when host `uvx` is unavailable without requiring it for container workflows. The schema reference, CLI reference, troubleshooting guidance, and copy-paste PyPI example cover the new workflow and its security limits.
- Stack sources can now opt into generated Python containers with `runtime: python`, including exact public PyPI releases and packaged git or local projects in validated subdirectories. Generated builds select a compatible Python 3.10-3.13 image, resolve console scripts without executing package code on the host, preserve explicit Dockerfile precedence, and carry Python options and pinned-image provenance through content-addressed builds.
- Source image builds now reuse an existing image only when its `io.gridctl.build-input-digest` label matches the resolved build plan, and built images carry non-secret build-input and source provenance labels. The builder also gains bounded official-PyPI release resolution, hash-verified wheel metadata inspection, static `pyproject.toml` and `setup.py` parsing, Python 3.10-3.13 compatibility selection, deterministic console-script resolution, and a digest-pinned, non-root uv Dockerfile generator.
- Source-based MCP servers now resolve and build one desired image per logical server before container reconciliation. Static replicas, hot reload replacements, and autoscaled spawns use that exact image instead of rebuilding per replica or reconstructing a mutable `latest` tag, and stale containers are replaced when their image differs. Container MCP servers also accept validated `volumes` mounts that reach static, reloaded, and autoscaled workloads.
- Source image builds now resolve Git refs to full commits in isolated builder worktrees, report mutable branch refs in resolved plans, and use content-addressed image tags instead of `latest`. Build identity includes source content, Dockerfile selection, build arguments, command, and target platform, while config plan and hot reload share complete effective server and source comparison.
- Scoped variable delivery is complete: `gridctl var explain` reports value-free store, environment, declaration, and consumer resolution; `gridctl var run` injects an explicit set/key selection into direct child processes with streaming exact-value redaction and signal forwarding; and `gridctl var scan` checks working-tree or staged Git content for exact stored secret values. Stored variables gain description, docs, example, and deprecation metadata with JSON and `.env` round trips. Stack and pack manifests can declare advisory, value-free variable prerequisites, with inheritance diagnostics, versioned pack persistence, and actionable unmet-variable output (#1188)
- `gridctl models` (Experimental) manages one model routing policy document (`~/.gridctl/models/policy.yaml`) and projects it into a LiteLLM auto-router config fragment plus an OpenCode provider stanza, giving a self-hosted mixed local-and-cloud model fleet a versioned source of truth with the same ownership, drift, and backup guarantees as skill and context projection. The rendered fragment carries only the router (LiteLLM's `include:` extends `model_list` across files, so backends stay declared once in the user's own config and are referenced by name; re-emitting them would silently load-balance duplicates), the `include:` line is a single-line text edit that leaves the rest of the parent config byte-identical, and the OpenCode write is an RFC 6902 patch that owns only the provider subtree and never touches the user's top-level `model` pick. Because LiteLLM reads config only at startup, sync latches a `restart-pending` state that only `gridctl models ack-restart` clears, and the sync output distinguishes "file written" from "policy live". The verb family (`init` with topology templates or `--from-litellm` scaffolding, `edit`, `validate`, `render`, `sync` with `--dry-run --diff` and `--check`, `status`, `adopt`, `unsync`, `ack-restart`) follows the projection house conventions, `gridctl reset` covers the new kind, and the rendered schema is contract-checked in CI by booting a digest-pinned LiteLLM release (v1.97.0, Auto Router v2) against a fragment included from a human-shaped config (#1178)
- The model routing policy gains a web surface (Experimental, inheriting the CLI feature's tier): a Model routing dialog in the Connections workspace (an always-visible header action, plus a summary section with a deep link in OpenCode's detail pane) showing per-target projection status, the policy's tier-to-backend routing summary, validation findings, and drift review with unified diffs, with the whole-policy verbs as gated actions: preview (dry-run diff), sync, accept-on-disk-as-owned (adopt), overwrite-with-policy (forced sync), and a confirmed "Mark restarted" that clears the restart-pending latch (rendered as an annotation chip, never an attention state, mirroring the engine's `NeedsAttention` contract). OpenCode drift joins the Connections client-health attention axis; the LiteLLM targets belong to no client and surface only in the dialog. Backed by new `/api/project/models/{status,validate,sync,adopt,ack-restart}` endpoints on the shared projection-handler conventions (`[]` not `null`, engine refusals as 409s with their message verbatim, validate-before-sync so an invalid policy returns findings instead of a 500), with the REST reset engine now sharing the same lazily built manager. The policy document itself stays CLI-edited by design; there is no browser editor (#1182)

## [0.1.0-rc.3] - 2026-08-22

Third release candidate. Adds LM Studio as the 16th `gridctl link` client, live-verified against a real 0.4.x install, along with a post-link notes channel that surfaces client-specific guidance in both the CLI and the Connections detail pane.

### Features

- `gridctl link lmstudio` connects LM Studio (an MCP host since 0.3.17) to the gateway: a url-only streamable-HTTP `gridctl` entry written into `~/.lmstudio/mcp.json` with wiring ownership, the stack.yaml `link:` block, `gridctl import`, and the Connections workspace all included. The entry carries no `type` key (LM Studio's in-app editor strips unknown keys), and session attribution maps the app's `clientInfo.name` to the `lmstudio` slug. After a successful link, client-specific notes cover the version floor, lazy server loading, per-tool approval, the distinction from the OpenAI-compatible API on port 1234, and a tip to prefer `--group` or code mode for local models; the same notes appear on the client's Connections detail pane, sourced from the backend so future clients with caveats need no frontend work. `gridctl ctx` reports LM Studio as unsupported (system prompt is per-chat in the app UI), and `gridctl link lms` explains that `lms` is the headless CLI rather than the MCP host (#1165)

## [0.1.0-rc.2] - 2026-08-19

Second release candidate. Adds `gridctl reset` and its web UI dialog, the `GRIDCTL_HOME` override, pack import credentials across CLI, REST, and both wizards, and an OpenAPI operations picker in the create-server wizard, plus fixes for OpenAPI TLS handling, the operations filter, and the pack wizard crash.

### Features

- The web UI gains the reset surface: a danger-zone strip at the foot of the Connections workspace (plus a command-palette entry) opens a Reset gridctl dialog backed by the same engine as `gridctl reset`. The dialog opens on the server's dry run and renders nothing actionable until it resolves, so the blast radius the user confirms is the one the engine computed: removable artifacts grouped per client with expand-to-detail, hand-edited items in a calm amber "your edits are safe" section rather than the removal list, and an explicit statement per client that everything else there is untouched. The two tiers are stacked radio cards whose labels name outcomes and paths, with the purge card red-bordered at rest, not only when selected, so the heavier option can never read as the milder one; switching tiers refetches the preview because the confirm token is tier-bound. Purge requires typing the resolved state-directory path the preview returned, the same string the server enforces, so the browser gate is real rather than decorative. Escape and backdrop clicks are inert while the reset runs, results are announced in-dialog through a live region (never a toast, which has none), partial failures list each failed row with the server's error verbatim and the idempotent-retry remediation, and both tiers finish by reloading the page: the serving daemon sits inside the blast radius either way, so the stores describe a world that no longer exists (#1142)

- `gridctl reset` removes everything gridctl placed on a machine, in one ownership-driven pass: projected skills, agents, and context rules in client directories, the gateway entries gridctl owns inside shared client MCP configs, and every stack's daemons, containers, and networks. The default tier preserves `~/.gridctl` (vault, oauth grants, pins, registry, cache, telemetry, logs); `--purge` deletes it too, behind a typed confirmation of the exact resolved path. Removal is driven by the projection lockfile and wiring ownership records, never a directory sweep: hand-edited files are kept unless `--force`, entries gridctl never created are never removed even with `--force`, and daemons stop before projections are touched so a live reconcile cannot re-project mid-removal. A tar.gz backup is written before anything is deleted (fail-closed: backup failure aborts the reset untouched; the purge archive lives outside the purged tree and excludes oauth tokens and daemon state on purpose). There is no restore command by design; the printed recovery path is forward (`gridctl apply`, `gridctl pack add`, `gridctl link`), with the archive as a manual fallback. `--dry-run`, `--yes`, `--verbose`, and `--format json` (versioned document) round out the surface, with exit codes 0/1/2 and idempotent re-runs. The same engine backs `POST /api/reset/preview` and `POST /api/reset`, gated to loopback peers and same-origin requests, guarded by a single-use preview-issued confirm token plus a server-enforced purge phrase, and sequenced so the result document is flushed before the serving daemon dismantles itself (#1142)

- `GRIDCTL_HOME` (and the `--home` global flag) replaces the home directory for every gridctl path: `~/.gridctl` and the client projection targets alike, so `GRIDCTL_HOME=/tmp/demo gridctl apply` runs a fully isolated instance that cannot touch real client configs, and a `reset --purge` under it is confined by construction. All path resolution funnels through one resolver with an AST-level guard test that fails the build on any stray `os.UserHomeDir` call, because a partially honored override silently writes to the real home (the allowlist keeps stack.yaml `~` expansion and `~/.docker` detection on the real home deliberately). The resolver errors instead of falling back to a relative `.gridctl`, closing a latent wrong-directory hazard on the destructive path. Daemons record their resolved home in a now-versioned state file and expose it via `/api/status`; `status` warns when the daemon on the default port belongs to a different home, state-mutating commands disclose a non-default home once on stderr, `status --json` gains a `home` field, and `doctor` reports the active home and its source (#1142)

- The pack import wizard can supply credentials, and the pack update dialog is no longer a dead end for a private pack. The backend gained pack auth separately; this is the web half. The credentials card that the skill import wizard has always had is now a shared component both wizards render rather than a copy in each, and it stays collapsed and optional: most packs are public, so leading with a credentials question would tax every import for the minority case. It opens itself, with an announced reason, when a preview fails in a way credentials could fix. The update dialog previously previewed on mount with no credentials and no input fields, so a private pack opened straight into an error with its Update button permanently disabled; a vault-backed pack now resolves there with no user input at all (the request omits the auth field, which is what makes the server re-resolve the reference it recorded at import), and a pack imported with a one-off token - which by design stores no reference - gets an inline credentials row and an explicit retry instead of a wall. Failures preserve everything the user typed and let the submit button double as retry, including an auth failure discovered at install time, which returns to the source step where the credentials live rather than restarting the wizard (#1135)

- An unreachable ssh-agent gets its own remedy in the wizard rather than a token field that cannot help. Because the daemon inherits `SSH_AUTH_SOCK` only from the shell that started it, an agent in the browser user's terminal is irrelevant, and an SSH URL cannot be authenticated with a pasted token at all - so the credentials card deliberately does **not** open for this case. Instead a distinct banner explains that the daemon has no reachable socket and offers "Try HTTPS instead", which swaps in the HTTPS URL the server derived for the same repository and opens the credentials card in vault mode: the only remedy solvable inside the browser. Restarting the daemon after starting an agent is offered as secondary text. The button appears only when the server could derive an equivalent, so a host with no HTTPS mapping does not get an action that would fail. Source-step failures now announce through the existing alert region only, not an alert plus a toast (#1135)

- Pack import accepts credentials on the CLI and the REST API, bringing packs to parity with skill import. Every pack clone previously ran with a zero-valued auth config, so a private pack repository worked only when the process happened to have inherited an ssh-agent socket or a `GITHUB_TOKEN`, with no way to supply a credential on any surface - a conspicuous gap on a path that otherwise has a full CLI verb family, a complete REST surface, and its own Library segment. `gridctl pack add` now takes `--vault-key`, `--auth-token-stdin`, `--auth-token`, and `--ssh-key`, and `POST /api/packs` and `POST /api/packs/preview` accept the same optional `auth` object the skill source endpoints take. The load-bearing half is persistence: the credential reference now reaches the importer and the pack's lockfile record, including the branch that builds that record from scratch for a wiring-only pack or a fully skipped selection, so a private pack imported with `--vault-key` records `${var:KEY}` and re-resolves on every later update instead of importing once and failing forever after. Only the reference is ever written; a literal or piped token stays transient by construction, which is why those two forms deliberately leave nothing behind. Omitting `auth` on a repository already imported with a reference resolves the stored one automatically, which is what lets an update preview a private pack with no user input, while an explicit empty object suppresses it. `pack add` also routes its failures through the same classify-hint-redact path as `skill add`; it previously printed the raw error, which meant no remediation hint and no redaction of a token embedded in the repo URL. Public pack import is unchanged in keystrokes and in what it records, and no `stack.yaml` change is involved (#1135)

- The create-server wizard's Operations Filter is now a picker instead of a blind textarea. An OpenAPI-backed server turns every operation in its spec into an MCP tool, and a large spec breaks clients outright - Cursor drops tools past roughly 40, VS Code and Copilot hard-cap at 128 - so curating that set matters, but the control took raw `operationId` values one per line and knew nothing about the spec. Operators had to already know the IDs, or deploy everything and prune afterwards. A "Load operations" button now parses the spec on demand and lists what it contains, searchable across operationId, path, summary, and tag, with method and tag filter chips. An explicit three-way mode maps 1:1 onto the config and states what happens to operations added to the spec later: All operations (no `operations` block, later additions exposed), Only selected (`include`, later additions not exposed), and All except selected (`exclude`, later additions exposed unless excluded). Rows show the raw operationId alongside the sanitized tool name, and the selection persists the raw value, which is what the filter actually matches - a list of sanitized names would silently match nothing for any spec whose IDs contain dots or spaces. Deselecting to nothing removes the filter rather than writing `include: []`, which means "expose everything" while reading as a whitelist. Selecting every operation offers, but does not force, a switch back to All, naming the trade: an enumerated list pins the server to today's spec. Operations that cannot become tools are reported with a reason rather than hidden, deprecated operations are badged, DELETE selections and specs above 50 operations draw a non-blocking advisory, and the Review step gains a one-line summary of the outcome. Loading is always explicit - editing the spec marks the list stale instead of refetching. Manual entry remains for specs that cannot be fetched while authoring, such as a path with an unexpanded variable or one that only resolves on the gateway host. No `stack.yaml` change (#1115)

- OpenAPI spec preview endpoint (backend): `POST /api/openapi/operations` parses an OpenAPI document on demand and returns every operation it contains, so the create-server wizard can show what a spec actually holds instead of asking operators to recall `operationId` values from memory. Each row carries the raw `operationId` (what `openapi.operations.include` matches) alongside the sanitized tool name (what the model sees), plus method, path, summary, tags, and a deprecated flag; the two identifiers differ whenever an ID contains characters outside `[a-zA-Z0-9_-]`, and reporting only one of them would produce a filter that silently matches nothing. Operations that cannot become tools — no `operationId`, or a sanitized name that comes out empty — are returned as explicit skipped rows with a reason rather than being dropped. Enumeration is shared with the deployed tool builder so preview and deploy cannot disagree about which operations are usable. Successful parses are cached for five minutes keyed on the spec and TLS material, with the same two-tier concurrency cap the server probe uses. Spec fetching is unauthenticated, matching deploy behavior, so the request accepts a spec reference and optional TLS files and no credentials. External `$ref` resolution is disabled on this path (it stays enabled for deploy): preview is reachable from the browser against an arbitrary operator-supplied URL, so following references inside a fetched document would let a hostile spec drive further daemon-side requests. The parser rejects such a document outright rather than omitting the referencing operations, so a multi-file spec deploys normally but does not preview, and the error names `$ref` and points at manual entry. No `stack.yaml` change; the wizard UI that consumes this arrives separately (#1115)

### Fixed
- Examples that failed or silently no-oped on first use are corrected: the code-mode example drops its dead top-level `agents:` block (removed with the Agent IDE; the stack schema silently ignored it) and now describes the real ACL model, where the sandbox enforces the connecting client's allowed-tool set; the registry examples stop referencing pre-made skills that do not exist on disk, so the documented activation curls hit real skills; the autoscale example invokes the `mock-stdio-server` binary the mock-server build actually produces, via the stack-relative path its siblings use; the scoped variable-sets example drops a `port:` key the resource schema does not have; the getting-started stacks drop untagged `description:` keys that were silently ignored; and the gateway example's client URL comment now points at the real default, `http://localhost:8180/mcp`. The README hero stack moves Atlassian and Zapier from the legacy `npx mcp-remote` bridge to the shipped native OAuth shape (`url:` plus `auth.type: oauth` with `gridctl auth login`), pointing Atlassian at its current streamable HTTP endpoint (`/v1/mcp/authv2`; the `/v1/sse` URL the example previously used was retired 30 June 2026), the examples index gains the missing `declarative-link/` category and a feature matrix covering every example on disk with correct transports, the platforms docs stop framing remote OAuth endpoints and host processes as containers, and both quick starts now agree on `mcp-basic.yaml` as the first file to apply. The reference docs get the same accuracy pass: the API reference documents `POST /api/reset/preview` and `POST /api/reset` with their loopback, same-origin, and single-use confirm-token contract, corrects `replicaId` to the integer it is on the wire, replaces `${vault:}` leftovers with the canonical `${var:KEY}` form, states the real auth exemption list, and adds the `home` and `gateway.tokenizer` status fields; the CLI reference corrects `stop --force` (orphan termination only), adds the `--bind` / `--bind-all` / `--insecure-allow-unauthenticated` listen flags to `apply` and `serve`, and fills in `unlink --group`, `skill update --trust`, `pack apply --clients` (wiring-only), the doctor home / lockfile / wiring checks, and the smaller flag omissions; troubleshooting's bare `gridctl destroy` recipes now pass the required stack argument; the Node floor is corrected to 22 in the install and contributing guides; `docs/project-status.md` is stamped at `v0.1.0-rc.1` and gains reset, `GRIDCTL_HOME`, and protocol dual-stack rows; and the config schema labels `secrets:` / `${vault:}` as compatibility aliases, adds the `auth` server field row, and fixes the `mcp_servers:` typo in the scaling guide (#1145)

- The pack import wizard no longer crashes on the success screen after a successful import. Go marshals a nil slice as `null` rather than `[]`, and a clean import produces no progress notes, so the response carried `"notes": null` while the client's type declared an array — the success step called `.map` on it and the error boundary replaced the whole page with "Cannot read properties of null (reading 'map')". The import had already completed at that point, so the pack was correctly in the registry and the user had no way to know it. The engine now emits `[]` for the add document's `skills` and `agents` and for `notes`, matching the types the API reference documents, and the wizard and update dialog guard every list they receive from the API. A pack whose manifest resolves to nothing was affected the same way, and would also have crashed the review step before importing anything. Populated responses serialize exactly as before (#1140)

- The ssh-agent remedy now reaches the skill import wizard and the pack update dialog, not just the pack import wizard. When a clone fails because the daemon has no reachable agent socket, credentials cannot help — a token cannot authenticate an SSH URL — so the useful response is switching to the HTTPS URL for the same repository, which the server already computes. Only one surface offered that. The skill wizard now shows the same banner with a "Try HTTPS instead" action that rewrites the URL field and opens the credentials card in vault mode. The pack update dialog, which previously opened straight into an error with its Update button permanently disabled and nothing to click, now names the cause and the HTTPS URL with a copy action; it deliberately does not switch protocol itself, because that dialog acts on the origin recorded at import and repointing it silently would change what the pack tracks. Neither surface opens the credentials card for this failure, and neither renders an action when the server could not derive an equivalent URL (#1138)

- Skill REST endpoints now send `httpsEquivalent` alongside `ssh_agent_unavailable`. `writeGitError` passed an empty repository into the structured-error writer, so the source add, check, and preview endpoints emitted the failure code without the rewritten URL that makes it actionable — leaving a client with a cause and no fix. The troubleshooting guide documented the field as arriving for skills as well as packs, so this was a contract the code did not honor (#1138)

- `gridctl pack add` accepts `--path`. `packops.AddOptions.Path` and `POST /api/packs` have both supported a subdirectory since the packs REST surface landed, but the CLI never passed it, so a pack in a subdirectory could be imported from the web UI and not from the command line. This mattered beyond a missing flag: `pack add` is the documented update verb, so re-adding a pack that had been imported with a path re-resolved against the entire repository and overwrote the pack record with a wider resource set, without warning. Note the semantics the flag pins: `--path` scopes resource discovery, it does not relocate the manifest, which is always read from the repository root (#1138)

- The variable picker is now usable by keyboard inside a modal dialog. Its dropdown renders through a portal to `document.body`, which puts it outside the enclosing dialog's focus-trap container and outside that dialog's Escape handler, so two defaults were actively wrong: Tab moved focus out of the picker and onto the dialog, making the variable list unreachable without a mouse, and Escape closed the whole dialog rather than just the picker. The popover now owns both keys itself rather than asking each consumer to opt in, which fixes it for every form that embeds it, not only the one where it was noticed. Escape closes the picker and returns focus to its trigger; Tab is left to the browser so the enclosing trap cannot treat the picker's own subtree as somewhere focus has escaped to (#1135)

- An unreachable ssh-agent is now reported as an actionable condition instead of a server fault. Cloning an SSH URL with no agent surfaced go-git's raw `SSH agent requested but SSH_AUTH_SOCK not-specified` string, which named a library the user has never heard of and no remedy, and over HTTP it fell through to a 500 - indistinguishable from the daemon being broken. The check now happens before dialing, wherever an ambient SSH clone is about to run, so both skills and packs get it: the error names the true cause (this gridctl process has no reachable socket, and a daemonized gridctl inherits one only from the shell that started it, and can outlive it), and it distinguishes an unset variable from a socket that has since gone away. Over REST it is a 422 carrying `"code": "ssh_agent_unavailable"` plus a server-computed `httpsEquivalent` for the same repository, so a client can offer the one remedy that needs no agent rather than string-matching an error message. The CLI prints the three fixes in order of reliability: retry over HTTPS with a credential (naming the rewritten URL when it can derive one), start an agent and restart the daemon so it inherits the socket, or name the key directly with `--ssh-key`. A stale socket path - the daemon outliving its agent, which is the case that actually bites - was previously not detected at all, since only the unset case was checked. Note that gridctl does not read `~/.ssh/config`, so a per-host `IdentityFile` that makes plain `git clone` work still does nothing here; that is now stated in the docs rather than left to be discovered (#1135)

- An OpenAPI server's `openapi.tls.caFile` and `openapi.tls.insecureSkipVerify` now take effect on their own. Both were applied only inside the branch that loads a client certificate, so a spec or API served with a self-signed or internally issued certificate still failed verification unless `certFile` happened to be set too - the two most common mTLS-adjacent settings were silent no-ops for the far more common case of an unauthenticated client talking to a privately signed server. A TLS config is now built whenever any of `certFile`, `caFile`, or `insecureSkipVerify` is present, with client-certificate loading still gated on `certFile`, and the transport's existing ALPN settings are preserved rather than replaced (previously configuring mTLS silently dropped HTTP/2 for that server). Anyone who set `insecureSkipVerify: true` without a client certificate was getting full verification despite asking for none; they now get what the field says (#1115)

- The OpenAPI spec preview no longer reports an unreachable host as a malformed document. A DNS failure, refused connection, TLS rejection, or timeout produces a transport error rather than the non-2xx response the classifier looked for, so all of them fell through to `parse_failed` with the hint "confirm the document is a valid OpenAPI 3.x spec" - pointing the operator at a document that was never served. These now classify as `fetch_failed` and say to check reachability from the gateway host, which is what fetches the spec. An unexpanded `${VAR}` in the spec path is also recognized ahead of the underlying failure, so a local path carrying one is named as such instead of reported as a missing file (#1115)

- The create-server wizard now writes the OpenAPI operations filter it collects. The Operations Filter control captured an include/exclude list of `operationId` values into form state, but the YAML serializer never emitted the `operations` key, so the deployed server generated a tool for every operation in the spec. Because an absent include list means "generate everything" to the backend, the loss was silent: no error, no warning, and the Review step does not display the generated YAML. The control has never worked since it was added, and shipped in every release from v0.1.0-beta.10. An empty selection still omits the key rather than emitting `include: []`, which would read as a whitelist while acting as a no-op. Note for anyone resuming a wizard draft saved before this release: drafts persist raw form state rather than generated YAML, so a stored operations list is intact and will now be applied on the next deploy, shrinking that server's tool set to what was originally selected. A shrinking tool set is deliberately not treated as schema-pin drift, so `gridctl pins verify --exit-code` and `pins diff` still exit 0; stale pin records for removed tools clear on `gridctl pins approve <server>` (#1112)

- Expert mode no longer discards a server's type on the way back to the form. Toggling the wizard's raw-YAML editor off ran a parser that returned `serverType: 'container'` for every input, which orphaned the whole `openapi`, `ssh`, or `source` block: the data survived in form state but the serializer never reached it, so deploying afterwards silently dropped the spec, auth, TLS, and operations filter. The type is now inferred from the YAML, and the parser no longer overwrites fields it cannot represent. The remaining limitation is unchanged and now stated in the editor: edits made inside expert mode to nested configuration do not carry back to the form view (#1112)

### Changed

- The skill auth flags gained a stdin form and a warning on the unsafe one, applied to `skill add`, `skill try`, and the new `pack add` together so the sibling commands cannot drift. `--auth-token <value>` puts a credential in shell history and in the process list, a documented anti-pattern (CWE-214) - GitHub's own CLI accepts tokens on stdin only. Rather than break the flag for the CI use it was added for, it stays and now prints a one-line stderr warning naming the exposure, alongside a new `--auth-token-stdin` (and the conventional `--auth-token -`) that reads the token from stdin instead, trimming the trailing newline that a token copied out of a provider UI carries. The auth hint text also lists the safer option first: it used to suggest `--auth-token` ahead of `--vault-key`, recommending the leaky form by position. The four CLI auth helpers now take an explicit writer and reader rather than reaching for `os.Stderr` and `os.Stdin`, which is what makes any of this testable; they had no unit tests before this change and now have table tests (#1135)

- The Node version used for frontend development is pinned via `.nvmrc` and an advisory `engines` field on `web/package.json`, matching the Node 22 that CI runs. The frontend test suite does not start on Node 20 (`jsdom` fails with `webidl.util.markAsUncloneable is not a function`), and on Node 23 and newer a built-in web-storage global collides with the jsdom one and fails a large share of component tests. Production builds are unaffected on newer versions; this pins the development and test environment (#1112)

## [0.1.0-rc.1] - 2026-08-10

First release candidate. Every shipped feature surface is now Stable (see [docs/project-status.md](docs/project-status.md)); the Experimental tier is retained only for features shipping dark behind the `experimental:` feature-flag registry, which currently holds none.

### Removed

- **Breaking:** the dollar-cost layer is removed from the backend, completing the staged removal begun with the web UI entry below. The gateway sits below the LLM client and cannot observe actual spend, so its dollar figures were always estimates of a fraction of a related quantity; token and usage metrics, which the gateway does measure, stay. Gone: `pkg/pricing` and the embedded LiteLLM snapshot (with the weekly refresh workflow and `task pricing:*`), the stack.yaml attribution fields (`gateway.default_model`, per-server `model:`, `client_models:`) and dollar budgets under `limits:` (rate limits stay; existing configs still load, with the removed keys ignored by the non-strict parser), the pricing and cost API routes (`GET /api/pricing/models`, the three model PUT routes, `GET`/`DELETE /api/metrics/cost`) and the `/api/status` cost and model-attribution fields, `costUsd` on `/api/tools/usage`, the `expensive_model_on_cheap_task` heuristic, the `gen_ai.cost.usd` and `gen_ai.request.model` span attributes (token and cache attributes stay), and the budget spend ledger (leftover ledger files under the state directory are orphaned and harmless). `gridctl optimize` findings now report `impact_tokens_per_week` (projected assuming ~500 prompts/week, named in the summary) instead of `impact_usd_per_week`, and `--min-impact` is token-denominated; `gridctl limits` reports rate limits only, and its budget-exceeded exit code 1 is gone. Token counting (`pkg/token`, `tokenizer:` config) is untouched (#1089)

- **Breaking (web UI):** the dollar-cost layer is removed from the web UI as the first stage of removing cost estimation entirely (the backend stage is the entry above; token and usage metrics stay). The Metrics workspace is now a token/usage surface: the Cost KPI card, cost charts, cost columns and cost-descending default sorts, the Models scope, and the priced/unpriced tool facet are gone, and a session-cumulative Format Savings card takes the fourth KPI slot. The pricing manager, effective-model tags, the command-palette pricing entry, the Stack inspector Pricing section, the wizard pricing fields (the wizard no longer emits `model:` or `default_model:`), the status-bar session-cost pill, the Traces cost pill, and dollar-budget bars are removed; the Limits panel and status-bar chip now surface rate limits only. `gridctl optimize` findings render in the UI without weekly-USD framing. Backend behavior is unchanged in this release: cost APIs still compute and serve, budgets still enforce, and stack.yaml fields still parse (#1089)

### Changed

- Every shipped feature surface graduates from Experimental to Stable ahead of the release candidate. The 27 Experimental rows in `docs/project-status.md` are now Stable, and the tier legend records that the Experimental tier is retained only for features shipping dark behind the `experimental:` feature-flag registry, which currently holds none. The flag machinery itself is untouched: the `experimental:` stack block, `GRIDCTL_EXPERIMENTAL_*` overrides, the `/api/status` `features` payload, and the status-bar chip all behave exactly as before. Three surfaces changed to make the claim honest rather than cosmetic. The pack manifest graduates to `apiVersion: gridctl.dev/v1`, with `gridctl.dev/v1alpha1` still accepted indefinitely so existing `gridctl-pack.yaml` files import unchanged (Article IX) and a wrong apiVersion now names both accepted spellings. The four agent projection targets drop their experimental tier flag, which removes the `(experimental)` suffix from `gridctl skill project status --kind agent`, the always-false `experimental` field from `GET /api/project/agents/status`, and the badge from the Library's agent projection rows. The Antigravity marker, which meant "this path rests on unofficial sourcing" rather than "this feature is unstable", is renamed accordingly: `Unofficial` in `pkg/skillsync` and `pkg/contexts`, `unofficial` on the wire in `GET /api/context` and `skill project status --format json`, and `(unofficial path)` in `ctx status`, `skill project status`, and the Global Context dialog. The `status`, `info`, and `doctor` `--json` schemas drop their "experimental until 1.0" caveat and adopt the 0.x backward-compatibility guarantee. Also removes `RuntimeInfo.IsExperimental`, dead since Podman graduated: it had no callers and always returned false

- `docs/cost-observability.md` is renamed to `docs/usage-observability.md`, matching the Usage Observability title it took when the dollar-cost layer was removed; every inbound link is updated. The feature table in `docs/project-status.md` gains a model preferences row, and the shipped `examples/registry/model-preferences.yaml` is registered in both example indexes (#1108)

### Features

- Model preference support in the Library (web UI): skills and agents now surface their model preference wherever they are listed and inspected, completing the backend entry below. A shared read-only chip renders on skill cards, Library table rows, agent cards, and both detail panels: an author declaration renders neutral, and a policy-resolved value (a stack `model_preferences` default or override) renders in the accent style with a `· policy` suffix so provenance is never ambiguous. The skill inspector gains a Model preference section and the agent Overview upgrades its raw `model` row to the typed view, both showing declared and applied values plus the per-target honor matrix (honored / ignored / unknown / dropped-on-render), so silence never reads as support. Surfacing is null-safe against older backends (no `modelPreference` object, no chip) and read-only by design: the policy stays YAML-only, matching the skills exposure-policy precedent (#1094)

- Model preference support for skills and agents (backend): the `model:` frontmatter that skill and agent authors already write is now a first-class, surfaced preference, and stacks can set defaults and overrides for it. A typed read-side view recognizes top-level `model:` and `metadata.preferred-model`/`metadata.model`, shown as a Model column in `gridctl skill list` (both kinds), in `skill project status` output as the `copy (model policy)` channel label and JSON `model_value` for rewritten projections, and as a `modelPreference` object on registry skill and agent REST responses, together with a per-target honor matrix (Claude Code honors skills inline and agents as a resolution input; the interop dir is consumer-dependent; Antigravity ignores it; rendered agent dialects drop it, as before). A new optional top-level `model_preferences:` stack block (per-scope `rewrite`/`default`/`overrides`, off by default, not inherited across `extends`, hot-reloadable) rewrites projected frontmatter on sync when opted in: the resolved preference (override beats author beats default, raising or lowering) lands in projected copies only (the registry canonical is never touched, so skill pins cannot drift from policy), and affected skill projections are forced to copy channel with the reason recorded and displayed (`copy (model policy)`). The skill lockfile gains a dual-hash model (installed vs canonical, migrating transparently) so rewritten projections report drift and staleness correctly; adopt restores policy-owned model keys to the author's declaration so they never flow back into the registry; a policy-less sync preserves rewritten projections rather than reverting the daemon's work, and `skill project sync --stack <path>` (and `status --stack`) applies a stack's policy from the CLI. `gridctl validate` gains three advisory findings (`model-preference-unknown-alias`, `model-preference-unhonored`, `model-preference-portability`); nothing enters the optimize/usage layer and no cost is claimed anywhere; this is a preference layer, unrelated to the removed cost-attribution fields (#1094)

- Pack provenance chips: pack ownership now shows wherever pack-managed resources appear, not only in the Packs segment. Agent projection status rows (`GET /api/project/agents/status`) and per-fragment context status rows (`GET /api/context`) gain an additive `pack` field carrying the applying pack's name, which the engines already recorded but never surfaced. In the web UI, a shared chip (unifying the two shipped hand copies) adds `pack: <name>` provenance to the skill inspector, skill cards, the Library table, and agent cards through the existing client-side join, and, straight from the wire, to the Connections client detail's agent projection rows (matching the Wiring section's badge) and the Global Context dialog's per-fragment status lines. Every chip is display plus a deep link to the pack detail; pack management verbs stay in the Packs segment (#1080)

- Packs REST surface: the full pack verb set is now available over HTTP, backed by a new `pkg/packops` engine extracted from the CLI (the CLI keeps its exact output and exit codes). `GET /api/packs` lists installed packs with identity, origin, per-kind counts, and attention; `GET /api/packs/{name}` adds per-resource state rows, refusing with a 409 naming both repos when two sources claim one pack name instead of picking nondeterministically. `POST /api/packs` imports from git behind the blocking security scan, and unlike the CLI it refuses the whole import with a 409 carrying the flagged resources before any write when findings lack trust; a POST against an already-imported origin is the documented update path. `POST /api/packs/preview` resolves a manifest read-only for import review; `POST /api/packs/{name}/apply` takes `clients`, `force`, and `dry_run` with full CLI flag parity; `DELETE /api/packs/{name}` supports a dry-run cascade preview and force. The pack record now persists the manifest's description and author so list views never re-clone, and `skills.lock.yaml` mutations are protected by a cross-process file lock so concurrent operations (CLI, API server, pack verbs) serialize instead of silently losing updates (#1078)

- Fragment-aware context adopt over REST: `POST /api/context/adopt/{slug}` accepts an optional body scoping the adopt in fragments mode. `{"fragment": "<name>"}` pulls one hand-edited projected file back into its source fragment on identity multi-file targets (the REST twin of `gridctl ctx adopt <client> [fragment]`), and `{"into": "<name>"}` captures a compiled target's edited body into a designated fragment, creating it when absent (the twin of `--into`). An empty body keeps the whole-client behavior. Refusals are now typed sentinels mapped to proper statuses (409 for the three adopt refusals with the engine's prose on the wire, 404 for an unknown fragment, 400 for a bad capture name) instead of a blanket 500. `GET /api/context` rows for multi-file clients gain a structured `fragments` array listing every out-of-sync fragment with its own state, so a drifted fragment can no longer hide a stale one behind the single summary string (#1076)

- Connections client health hub: the Connections workspace grows from a single-column link-toggle list into a per-client health hub. A resizable client rail (attention-first ordering with a drift filter, brand icons, link badges, the familiar inline connect toggle, and keyboard navigation) sits beside a selected-client detail pane answering "what did gridctl configure on this client, and is it still true?": collapsible sections that auto-expand on drift cover the gateway entry's full wiring ownership state with inline Re-link and Adopt (refusals render the engine's reason verbatim), global context sync with inline Sync/Unsync and a Review deep link into the Global Context dialog, agent projections with per-row render class linking into the Library (non-target clients say so explicitly instead of showing blank space), an access-scope summary deep-linking into the Tools per-client editor (`/tools?client=<slug>`), and live session activity attributed by the sessions API's new client identity. Ownership state and live connectivity render as two separately labeled axes, never one merged health light; sessions the UI cannot attribute appear in their own labeled bucket with synthesized identities, stateless-generation clients read "sessionless" rather than "0 sessions", and the status bar reads the same session array as the workspace so the two counts cannot diverge. Deep links: `?client=<slug>` selection, and the creation wizard's Client Link route now lands on a spotlighted detected-unlinked client via `?spotlight=unlinked`. The staged Review & Apply flow is unchanged and stays workspace-wide (#1074)

- Wiring ownership REST: `GET /api/project/wiring/status` exposes the full six-state ownership vocabulary (`in-sync`, `stale`, `drifted`, `target-missing`, `foreign`, `missing`) with per-row detail, remediation, and pack tag — the complete form of the fact `/api/clients` collapses into one `drifted` boolean — and `POST /api/project/wiring/adopt` wraps the take-ownership verb that was previously CLI-only, returning 409 with the engine's reason on refusal. The link endpoint's conflict message now offers the API adopt route alongside the CLI. Session entries from `GET /api/sessions` gain client identity (`clientName`, `clientVersion`, and a normalized `accessId` that matches provisioner client slugs), so sessions can be attributed to linked clients. Groundwork for the Connections client health hub (#1074)

### Fixed

- Handshake-era HTTP health checks now detect generation flips, closing the last dual-era health asymmetry. An external HTTP server that negotiated the handshake generation and redeployed as stateless-only used to read healthy forever (the health check was a bare reachability GET the flipped server still answers 200) while every tool call failed, and only a gateway restart recovered it. The handshake-era check now sends a protocol-level `ping` (stdio/process parity) and, on a method-not-found answer, confirms with a read-only `server/discover` probe before failing health, so lax legacy servers without `ping` and proxies that reject unauthenticated requests keep reading healthy exactly as before; only positive evidence of a modern peer (or transport unreachability) fails the check. The HTTP client is now reconnectable: the health monitor's existing backoff-gated reconnect path re-resolves the generation on the live client, clears the stale session, and refreshes tools before pins are re-verified, so a flipped server converges to healthy on the new generation without a restart. SSE clients, which are pinned to the handshake generation, skip flip detection entirely since re-negotiation can never move them (#1088)

- Closed the delivery gaps a post-ship audit found across the Library and Connections surfaces: `docs/project-status.md` no longer claims packs have no web UI (the Packs segment and pack REST shipped); agent projection rows in the Library now link each client row to that client in Connections, matching the reverse deep link Connections always had; the Connections Context section now lists a client's out-of-sync context fragments with their states and pack provenance chips instead of a summary line only; and the Wiring section's pack badge is now the same navigable chip the Agents rows use (#1082)

- The Global Context dialog's Review flow no longer dead-ends in fragments mode. On a multi-file client the drift dialog compared a summary string instead of file contents and its Adopt button called the whole-client endpoint, which the engine correctly refuses on multi-file targets, so every review ended in an error toast. The dialog now reviews drift per fragment with a real diff, adopts losslessly on identity renders, explains lossy renders with working alternatives instead of a dead button, and offers fragment capture on compiled targets (#1076)

- The status bar's session count and `GET /api/sessions` no longer diverge. The two surfaces read different stores with different lifecycles: the gateway's session manager is swept every 5 minutes at a 30-minute idle cutoff, while the streamable transport kept its own map that only shrank when a client sent a graceful `DELETE /mcp` — which crashed CLIs and closed tabs never do, so that count only grew (a 7 vs 21 divergence was observed on one load). The transport now defers to the gateway's manager as the single source of truth: expired sessions are excluded from listings and lazily torn down, both counts agree by construction, and a client posting on an expired session receives the spec's 404 and re-initializes cleanly instead of continuing unattributed (#1074)

- Agents in the Library workspace: the web UI's Library gains a `Skills | Agents` segmented control (URL-synced as `?kind=agent`; the retired `/agent` route now lands there), making CLI-imported agents first-class in the UI. The Agents segment shows a source-grouped catalog with per-client projection chips, an always-visible KPI strip (Total, Projected, Drifted, Experimental; deliberately no lifecycle or usage KPIs, since agents have neither), a "Sync N stale agents" pill, and an inspector with Overview (frontmatter, with `tools` and `model` called out as per-client-translated), Body, and Projection tabs. Projection rows speak the CLI's exact state vocabulary through a shared StatePill (extracted from the Global Context dialog so context and agent rows stay one color language), carry the render class and dropped-keys detail, and offer sync, unsync, and drift review inline; adopting a hand-edit is offered on the identity target with the backup named up front, while lossy render targets get the refusal spelled out with its two real alternatives as working buttons, never a disabled control with a tooltip. The single-file editor saves the whole AGENT.md byte-verbatim behind the same blocking security scan imports run, rendering findings inline on a refusal. The import wizard now discovers agents alongside skills, lists them by name in the browse and review steps (names are cheap at agent scale), and passes an explicit agent selection to the importer — which the source-add API now accepts (`selectedAgents`), since a skill selection alone deliberately skips agents (#1071)

- Agents REST surface: imported agent definitions are now manageable over HTTP, closing the "agents have no REST endpoints" limitation ahead of the Library UI. `GET /api/registry/agents` lists installed agents (bodies omitted by default, `?full=1` for complete shapes; passthrough frontmatter rides in `extra` as an ordered `{key, value}` array, never an object, because key order is part of the verbatim-projection contract), `GET /api/registry/agents/{name}` returns one agent with `body` and `raw`, `PUT /api/registry/agents/{name}` edits the file as a whole (re-parse, rename refusal, then the same blocking security scan imports run, whose findings return 409 with no trust override over REST, then a byte-verbatim atomic write, so identity projections never read an edit as manufactured drift), and `DELETE` removes the agent through the importer so origin and lock entries leave with it. A new `/api/project/agents/*` namespace exposes the projection engine: `status` (per-client rows with the shared state vocabulary plus `render: identity|lossy` and `experimental`), `sync`, `unsync` (refuses an empty request), and `adopt`, whose lossy-render refusals surface the engine's full reason as a 409 rather than a generic error. Source add and update responses already carried imported-agent detail; sync results now count refreshed agents (`importedAgents`), and the source preview response gains `agents` and `malformedAgents` so a mixed repo previews both kinds (#1071)

- Rules fragment library in the web UI: the Global Context dialog now recognizes fragments mode and swaps the single-file editor for a fragment rail in composition (filename-lexicographic) order, with add and delete affordances, a globs badge for path-scoped fragments, and the same markdown/preview split, marker validation, and sync-all flow feeding whichever fragment is selected; a dirty draft blocks fragment switching so typing is never silently discarded. The per-client strip gains a mode chip (`multi-file` / `compiled`) beside each state, and the single-file editor gains a deliberate "Fragments" affordance that names the first fragment and states the AGENTS.md-to-`fragments/00-default.md` migration before writing anything, mirroring `ctx add` (#1046)

- Rules fragment library: `gridctl ctx` can opt into a multi-file fragment store under `~/.gridctl/context/fragments/*.md` (optional `description` / `paths:` frontmatter). `ctx add <name>` activates the mode on first use by migrating AGENTS.md to `fragments/00-default.md` with a backup; read-only commands never migrate. Composition is filename-lexicographic. Multi-file passthrough targets are Claude Code, VS Code Copilot (`applyTo` from `paths:`), Cline, and Roo, each fragment file individually lockfile-owned so shared rules directories never claim user files; every other single-file target receives a compiled document with `<!-- Source: -->` attribution and the existing size-cap hard errors. Status gains a `mode` column (`single-file` / `multi-file` / `compiled`); adopt is lossless per-fragment on multi-file identity renders and refuses on compiled targets unless `--into <fragment>`. Packs activate the `rules:` manifest slot (`rules/*.md` or `fragments/*.md` in the pack repo; empty means none): rule installs pass the same blocking security scan and `--trust` gate as skills and agents, never overwrite a differing local fragment, surface a fragments-mode migration explicitly, and only pack-shipped fragments carry the pack tag, so cascade remove can never retract a user fragment. New CLI: `ctx list|add|rm`, fragment-aware `edit`/`diff`/`adopt`; REST: `/api/context/fragments` (#1046)

- Skill governance in the web UI: the Pins workspace gains a Servers | Skills toggle in its rail, extending the existing review grammar to skill-document pins: skills list drift-first under the same attention filter, and a drifted skill's detail pane shows a semantic summary line, a line-level prose diff of the canonical SKILL.md (non-printables escaped per line), the per-file added/removed/modified list, and advisory findings, with Approve bound to the reviewed diff's composite hash (a stale-content 409 reloads the diff for re-review) and a reason input required when unresolved findings exist; Reset mirrors the server flow behind the same confirm-dialog pattern. Drifted skills imported from one git source group under a source header in the rail. The Library shows one compact governance indicator per row/card only when attention is needed (pin drift, warn-or-critical findings, or a policy denial; quiet skills show nothing), the skill detail panel gains a Governance section (factual origin `Local` / `Imported: repo@ref`, pin state with a Review-in-Pins deep link, finding counts, policy verdict) plus a "Blocked by policy" chip that names the matching rule while the lifecycle badge stays untouched, and the status-bar pin badges now count skill drift and findings (server-only stacks read exactly as before), deep-linking to `/pins?kind=skill`. Skill pins ride the existing 3-second poll in their own progressive-disclosure block with rising-edge toasts (#1045)

- Skill governance pins: registry skill documents now get the same trust-on-first-use treatment tool definitions get from schema pins. The daemon silently pins every skill on first sight, recording per-file SHA-256 digests over the whole document set — the canonical `SKILL.md` (hashed on its parse-rendered form, so frontmatter normalization from imports, editor saves, and projection never manufactures false drift) plus every supporting file (dotfiles, `.origin.json`, and temp files excluded) — along with factual provenance (`local`, or `git` with repo/ref/commit from the origin sidecar). Any later content change flips the skill to "pin drift" (a distinct fact from the Library's sync drift), which persists until a human approves or resets: the daemon never auto-clears it, and the pin file lives at `~/.gridctl/pins/skills/<stack>.json`, outside the watched registry tree, so pin writes can never feed back into the disk watcher. The pins poisoning heuristics (P001–P005) now scan skill names, descriptions, and bodies via a new `ScanSkill` entry point, with findings persisted on the record as strictly advisory data — the deterministic hash is the gate, and approving a skill with unresolved findings requires a `--reason`, persisted for later reviewers. A new `gridctl skill pins list|verify|diff|approve|reset` family mirrors the `gridctl pins` contract (`--format json` with `schema_version`, exit `0`/`1`/`2`, opt-in `--fail-on-findings`, `approve --expect` bound to the reviewed diff's composite hash; distinct from `skill pin`, which pins a git source ref), backed by `/api/skill-pins` endpoints with the same 409-on-stale-hash concurrency, and registry API responses gain a `governance` object (source, origin, pin status, finding counts, policy verdict) for the upcoming Pins-workspace UI. Separately, an optional top-level `skills:` block in stack.yaml adds a global exposure policy: allow/deny name globs (deny wins, then allow, then `default:`) filtering which skills the gateway serves via prompts/resources and which projection sync places — denied skills keep their registry state and stay visible in the API flagged with the matching rule, recorded projections are skipped with a pointer rather than removed, `gridctl apply` warns for every active skill the policy hides, `gridctl validate` flags a default-deny block with no allow list, and edits hot-reload. Omitting the block preserves today's behavior exactly, and per-client skill scoping remains deferred (#1045)

- Packs: a git repo carrying a `gridctl-pack.yaml` manifest (apiVersion gridctl.dev/v1, kind: Pack) now imports and applies as one unit. `gridctl pack add <repo-url>` clones through the same origin pipeline as `skill add` (security scan, `--trust` gate) and imports exactly the manifest's selection of skills and agents — agent-level selection is new plumbing this release adds to the importer, with the legacy contract preserved: a plain skill selection still skips agents. `gridctl pack apply` projects the selection through the existing engines and, when the manifest declares `wiring: true`, ensures the gateway entry through the wiring ownership manager (skipping with a hint when no gateway is running); every projection is tagged with the pack name in the unified project lockfile, a resource tagged by a different pack is refused, and apply is additive, never transactional (`Applied N/M` summary, exit 1 on any skip). `gridctl pack status` reports the shared state vocabulary per resource plus `unresolved` rows for manifest selections the repo does not ship. `gridctl pack remove` cascades in dependency order — projections unsynced, wiring records removed through the ownership manager, then registry entries, then the pack record — keeping drifted projections with a remediation hint unless `--force` and trimming the pack record on partial removal. The import lockfile stamps schema version 2 only when a pack record exists, so setups without packs keep downgrade freedom. Manifest field names align with the Claude Code plugin.json family; the `rules:` field selects context rule fragments (see Rules fragment library). No enable/disable state, no inter-pack dependencies, no marketplace, no web UI (#1034)

- Multi-client agent renders: `gridctl skill project sync --kind agent` now projects imported agents to three rendered targets alongside the identity Claude Code copy, where those clients are detected: OpenCode (`~/.config/opencode/agents/<name>.md`, emitted as a `mode: subagent` definition), GitHub Copilot (`~/.copilot/agents/<name>.agent.md` — Copilot's global agents directory; contrary to earlier wording, Copilot does not read `~/.claude/agents`), and Gemini CLI (`~/.gemini/agents/<name>.md`). Renders are deterministic and explicitly lossy: frontmatter keys a dialect cannot express (Claude `tools` on OpenCode, and `model`, `hooks`, `mcpServers`, vendor keys everywhere) are dropped and named in sync output, `skill project status` gains a RENDER column (`identity` or `lossy`), and adopt is refused on rendered targets with a pointer at the identity projection, so client-dialect content can never flow back into the canonical store. Cursor needs no target: it reads `~/.claude/agents` natively. Hand-edited rendered files still report drifted and are never overwritten without `--force`, including by the daemon reconcile (#1034)

- Wiring ownership: `gridctl link` and `unlink` now record what they write instead of guessing what they wrote. Every link stores the client, config path, entry name, and an RFC 8785 canonical hash of the written value in the unified project lockfile (`~/.gridctl/project.lock.yaml`, hash only, never the value, since entries can carry secrets in env blocks; a short hash history means a gridctl upgrade changing its own written shape never reads as user drift). Unlink deletes an entry only when its current value is one gridctl recorded (or `--force`, which unlink now accepts for hand-edited entries); entries gridctl never wrote are never deleted, with or without force, closing the long-standing hazard where a user's own localhost entry could be removed by name match. A pre-existing entry identical to what gridctl would write is adopted silently; anything else foreign is refused with an adopt hint, and the old localhost/npx shape heuristic now serves only that one-time migration hint. The new `gridctl project` verb family (`sync|status|unsync|adopt --kind wiring`, with `--format json` and exit codes `0`/`1`/`2`) exposes the state matrix: `in-sync`, `stale` (gateway port or entry shape changed), `drifted`, `target-missing` (distinguishing a removed key from a wiped config file, the failure mode first-party clients have shipped), `foreign`, and `missing`. Every link surface records ownership identically (CLI, `apply --flash`, the stack `link:` reconcile during apply, and the web UI, whose Connections workspace gains a Drifted badge), `gridctl doctor` gains advisory per-client wiring rows ("detected but not linked", drift), and the daemon never auto-reconciles wiring. Existing links predate the lockfile and show as `foreign` until relinked or adopted once (#1031)

- Agent resource kind: `gridctl skill add <repo-url>` now also discovers Claude Code subagent definitions matching the `agents/*.md` convention (at the repo root or any subdirectory root, the plugin-shaped layout), so a repo shipping `skills/` plus `agents/` imports as a unit. Each agent lands verbatim at `~/.gridctl/registry/agents/<name>/AGENT.md` with the same `.origin.json` sidecar, security scan (`--trust` gate covers bodies and frontmatter values), lockfile tracking, and drift-safe `skill update` semantics skills get; agent names are validated (lowercase letters, digits, hyphens; no colons, which Claude Code refuses) and duplicate names within a batch fail those items with an error naming both sources. `gridctl skill project sync --kind agent` projects every imported agent to `~/.claude/agents/<name>.md` as a single copied file with dedicated-file ownership: a pre-existing hand-authored file is refused without `--force` and backed up under `~/.gridctl/project-backups/agent/` with it, a hand-edited projection reports drifted, and `adopt --kind agent` pulls the edit back into the canonical store instead. `skill project status` gains agent rows (marked experimental), every row in `skill project` JSON output now carries a `kind` field, `skill list`/`remove`/`info` take `--kind agent`, and the daemon reconciles agent projections alongside skills. The import lockfile (`~/.gridctl/skills.lock.yaml`) gains a schema `version` field with a newer-version guard; version-less files migrate on read. Claude Code is the only render target in this slice (identity render; Cursor also reads `~/.claude/agents` natively), and there is no web UI surface yet (agents are CLI-only for now) (#1020)

- MCP protocol generation in the web UI: mixed-generation fleets show a Generation row in the Stack canvas sidebar next to the existing Protocol row (suppressed when every server speaks the same generation, since a uniform label carries no signal), and the Connections workspace gains a read-only Live sessions card listing each active session with its negotiated protocol version and generation. The card is deliberately separate from the client rows above it: those are declared links (config-file state), while sessions are transport state, and stateless-generation clients (2026-07-28) are sessionless by design and never appear there (#1018)

- MCP transport dual-stack: the gateway now speaks both protocol generations concurrently, per peer, in both directions: the handshake era (2025-11-25 and earlier: `initialize`, `Mcp-Session-Id` sessions) and the new stateless era (2026-07-28: per-request `_meta`, `server/discover`, MRTR). Downstream, each server's generation is auto-negotiated by probing `server/discover` and falling back to the legacy handshake on anything not positively modern (auth challenges and probe 5xx reject outright rather than misclassifying); the verdict is re-derived on reconnect and restart, and an optional per-server `protocol_generation: auto | handshake | stateless` knob pins mis-probing peers. Upstream, `POST /mcp` (and group endpoints) accepts stateless requests with no session, implements `server/discover`, validates the mirrored `Mcp-Method`/`Mcp-Name` headers (base64 sentinel decoding included) with the spec's `-32020`/`-32021`/`-32022` error codes, and derives per-request identity from `_meta` clientInfo so access scoping, budgets, telemetry, and tracing behave identically on both paths. Results are bridged between generations: `resultType` is synthesized for legacy servers, `ttlMs`/`cacheScope` aggregate as min-across-fleet (zero when any server is legacy) and private-unless-all-public, and MRTR `requestState` relays byte-exact through a routing envelope that survives gateway tool renaming, with retries never cached. Cross-generation MRTR is deliberately out of scope and reports a clear error. The tasks extension proxies opaquely when exactly one stateless server declares it. `listChanged` is no longer advertised on any generation (gridctl never emitted the notification; advertising it was a conformance violation). Generation is visible in `gridctl status --json`, `/api/mcp-servers` (`protocolGeneration`), `/api/sessions` (`entries` with per-session tags), a `gridctl doctor` per-server check, a negotiation-time log line, and an `mcp.protocol.generation` trace attribute. Schema-pin fingerprints are untouched by design (the new result fields live outside tool definitions), so a fleet changing generations shows zero pin drift. The official `@modelcontextprotocol/conformance` suite is wired into `tests/integration/` with a checked-in expected-failures baseline and a `task test:conformance` task (#1018)

- Experimental feature flags: a new optional top-level `experimental:` block in stack.yaml enables registered experimental flags by name, backed by a typed registry (`pkg/flags`) with lifecycle stages (experimental, graduated, removed) and a graduation-deadline test so flags cannot rot silently. Everything defaults to off, so an unchanged stack.yaml behaves identically. Unknown flag names warn at `gridctl apply` and `gridctl validate` listing the valid names, and graduated or removed names warn with a specific migration message — never an error, so a stack written against a newer gridctl still deploys. Each flag can be overridden per process with `GRIDCTL_EXPERIMENTAL_<NAME>` (strconv.ParseBool vocabulary; unset defers to YAML, unparseable warns). Enabled flags surface in `GET /api/status` (`features` plus `feature_details`), `gridctl status --json`, and the web UI as a read-only "N experimental" status-bar chip with per-flag rows in the spec pane; flags are configured in stack.yaml only and cannot be toggled from the UI. Edits to the block hot-reload without container restarts. The first registered flag, `transport_dual_stack`, was reserved for the MCP transport work. The flag lifecycle is documented in CONTRIBUTING.md, and `GRIDCTL_NO_SKILL_UPDATE_CHECK` now accepts the same boolean vocabulary (previously only `"1"`) (#1021)

- `gridctl skill project adopt <skill> --client <slug>`: a hand-edited copy projection (Antigravity's forced copies, or `--copy` elsewhere) can now be pulled back into the registry skill instead of being clobbered by `--force`. Adopt backs up the registry's `SKILL.md` as `SKILL.md.pre-<sha>` (the same convention forced updates use), writes the changed files back, and re-syncs that one (skill, client) pair to in-sync; other clients projecting the skill go stale until the next sync. The adopted content counts as local edits, so `gridctl skill update` refuses to overwrite it without `--force`. Symlinked projections are refused (the registry copy is the source of truth), as are empty or invalid projected files. Exit codes follow the family convention: `0` adopted, `1` nothing to adopt, `2` infrastructure (#1019)

### Changed

- On the stateless generation, the gateway-owned skill surfaces (`prompts/list`, `resources/list`, `resources/templates/list`, and `resources/read`) now carry their own cache metadata (`ttlMs` 60000, private) instead of the downstream tool fleet's aggregate. Skills are served from the local registry, so their cache lifetime is unrelated to the tool fleet: previously one legacy tool server in the stack pinned every skill list and read to uncacheable. `tools/list` and `server/discover` keep the fleet aggregate, which is the surface it actually describes (post-dual-stack audit follow-up)

- The `transport_dual_stack` experimental flag has graduated: the MCP transport dual-stack shipped unconditionally, the flag never gated anything, and its registry entry now says so. A stack.yaml still setting `experimental: transport_dual_stack` deploys unchanged and warns with a migration message pointing at the `protocol_generation` per-server knob. The integration conformance harness now pins `@modelcontextprotocol/conformance` 0.2.0-alpha.10 and exercises the 2026-07-28 generation for real: the default active suite runs under `--spec-version 2026-07-28` (the previous pin's stateless scenarios lived under an empty `draft` set, so the modern surface had no third-party validation), and the 20 stateless-only scenarios the suite has not yet promoted (SEP-2575 `_meta` validation, SEP-2549 caching, SEP-2243 headers, MRTR) run individually. Expected-failures baselines are now per generation (the suite runs strict with a baseline, and a few multi-version scenarios fail on exactly one generation) (#1084)

- `gridctl pack status` rule rows now report per-client projection state once a pack is applied (in-sync, stale, drifted, or target-missing per client), instead of only store-level presence. Coverage is per fragment-file projection: multi-file clients get one row per client, while a compiled client's whole-document state stays in `gridctl ctx status`. A rule that was imported but never projected keeps its store-presence row, and a missing rule still reports `missing` with the re-run hint. Multi-pack output is now sorted by pack name, and a pack name claimed by two imported sources now refuses `pack status <name>`, `pack apply`, and `pack remove` with both repos named (exit 2) instead of acting on a nondeterministic pick (#1078)

- Projection state now lives in one unified lockfile, `~/.gridctl/project.lock.yaml`, shared by `gridctl ctx` and `gridctl skill project` and owned by the new `pkg/project` engine. The lockfile is keyed by destination path, so one destination can never have two owners, and versioned in two tiers: a breaking `version` readers refuse when newer, and an additive `revision` whose unknown fields survive rewrites and re-syncs by older binaries of the same version. Legacy `skillsync.lock.yaml` and `context.lock.yaml` files migrate automatically on the first sync after upgrading: both are backed up to `~/.gridctl/project-migration-backup/<timestamp>/`, then replaced with version-2 tombstones so a downgraded gridctl fails loudly ("written by a newer gridctl version") instead of silently diverging; restore the backup if you downgrade deliberately. Context operations now hold the same cross-process lock skill projections always had, closing a CLI-vs-daemon race. `gridctl doctor` gains a `project.lockfile` check reporting which generation is in use, and CLI behavior is otherwise byte-identical, enforced by characterization tests (#1019)

- Code mode is now a stable feature: the `gateway.code_mode` and `gateway.code_mode_timeout` settings and the `--code-mode` flag are no longer labeled experimental. Behavior is unchanged (#1021)

- The project task runner is now Task (https://taskfile.dev): `task build`, `task test`, `task lint`, and friends replace the Makefile targets, with namespaced names for grouped work (`build:web`, `test:integration`, `pricing:update`, `mock:servers`). Run `task --list` for the catalog. A transitional Makefile shim keeps every old `make <target>` name working while muscle memory catches up. `task test` and `task test:integration` now run with the race detector to match CI, `task lint` covers both golangci-lint and the frontend lint, a failed frontend build now fails `task build:web` instead of silently staging stale assets, and the `gridctl validate` hint for unknown pricing models points at `task pricing:update` (#1014)

### Bug Fixes

- A downstream server that changes protocol generation between connections now re-negotiates cleanly and surfaces through the health channel. Two defects fixed (#1086): the era re-probe no longer carries the previously negotiated `MCP-Protocol-Version` header (which contradicted the probe's `_meta` and made strict stateless servers reject the probe with `-32020` instead of answering, wedging registration after a generation flip), and the HTTP transport's health check on the stateless generation now exercises `server/discover` like the stdio and process transports do, instead of a bare reachability GET, so a mid-session generation flip fails into the health/degraded channel rather than surfacing as per-call tool errors. All three transports additionally validate the discover answer itself (resultType complete, a mutually supported stateless version), so a lax peer answering junk to unknown methods can no longer read healthy while every real call fails

- Tool calls on stdio and process transports with tracing active no longer risk corrupting large integer arguments. The trace-context `_meta` injection decoded params through `map[string]any`, converting every number to float64 on the way through, so integers beyond 53 bits (IDs, timestamps in nanoseconds) could be silently rounded on both protocol generations whenever a span was sampled. The merge now passes every byte outside the injected trace keys through untouched

- Running the official 2026-07-28 conformance scenarios against the stateless edge surfaced five wire deviations, all fixed. A request whose `MCP-Protocol-Version` header declares the stateless generation but whose `_meta` is missing or incomplete is now rejected with `-32602` at HTTP 400 naming the missing piece (SEP-2575); previously a `_meta`-less modern request fell through to session lookup and answered "Mcp-Session-Id header required", and `server/discover` answered incomplete probes leniently (a compliant modern prober always stamps full `_meta`, so the leniency only masked non-compliant callers; a missing `clientCapabilities` also returned `-32600` instead of `-32602`). A header/`_meta` protocol-version disagreement now returns `-32020` HeaderMismatch even when one side is also unsupported (`-32022` is reserved for the agreeing-but-unsupported case). `resources/templates/list` is implemented on both generations (an empty list with caching hints, since the gateway exposes no templated resources; it previously answered `-32601` despite the advertised resources capability, which SEP-2549 flags). An empty `tools/list` now marshals `"tools": []` instead of `null`, which conformance clients read as a failed call. On the stateless path, calling a tool no server provides returns the spec's `-32602` "Unknown tool" protocol error instead of an executed-with-error result, and `resources/read` failures always use `-32602` with the requested URI in the error data (SEP-2164); handshake-era responses for both are unchanged (#1084)

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
