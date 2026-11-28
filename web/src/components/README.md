# Web Components

gridctl's SPA is structured around a single application shell that hosts the
URL-routable workspaces. This file documents the shell architecture, the
Zustand store layout, and the do-and-don't conventions every new component
should follow.

## Terminology

The encrypted variable workspace is Variables. The route `/vault`, `useVaultStore`, the `vault/` folder, and the palette section id `vault` are unchanged.

The encrypted store itself is the variable store. Lock state reads "Variables locked", "Unlock variables", "Variables unlocked", and "Lock variables". "Vault" or "vault" does not appear in visible text, form labels, tooltips, or `aria-label` values.

Secret-flagged variable values are secrets or secret values. That wording is allowed only when naming values with the secret flag, never as a navigation or section title. A stored variable chosen for auth is "a stored variable", not "a vault secret".

The skills, agents, hooks, packs, context, and proposals workspace is Library. "Registry" is reserved for the external MCP Registry in `wizard/CatalogPicker.tsx` (including its lowercase `registry` tier badge) and for the on-disk `~/.gridctl/registry` directory named in `system/ResetDialog.tsx`. Every Library segment's header is titled "Library", with the kind switch beside the title; list segments include a summary that counts their loaded items ("12 skills", "3 packs") and omit it while loading.

A server in the stack is an MCP server. Title-case labels use "MCP Server". Not "Server", and not bare "MCP".

The declared stack is Stack. That label is already consistent.

Pin drift toasts use "Pin drift". One toast covers every server, skill, and fragment newly drifted in a poll: "Pin drift detected on server github" for a single item, or "Pin drift detected on 1 server and 2 skills" for several. Findings toasts read "New findings on …" the same way. Per-pane section headers are unchanged.

Creation wizard entry points use "Create new". The header's primary button (its accessible name and, from 1360 px, its visible label) and the palette command use the same words. "Reload Config" keeps its existing casing everywhere.

## Shell architecture

```
<AppShell>             web/src/components/shell/AppShell.tsx
├── <Header>           web/src/components/layout/Header.tsx
│   └── <WorkspaceSwitcher>   <nav aria-label="Workspaces"> of NavLinks
├── <Outlet />         renders the active workspace body
│   ├── <StackWorkspace>      /stack  (toolbar over a graph or table body; hosts the ?spec=1 spec column; ?node=<id> selects a node)
│   ├── <LibraryWorkspace>    /library  (also /library/:skillName)
│   ├── <VaultWorkspace>      /vault
│   ├── <ToolsWorkspace>      /tools
│   ├── <MetricsWorkspace>    /metrics
│   ├── <PinsWorkspace>       /pins
│   ├── <LogsWorkspace>       /logs
│   ├── <TracesWorkspace>     /traces (Runs tab at ?view=runs)
│   └── <ConnectionsWorkspace> /connections
├── <StatusBar>        connection · alerts · servers · sessions · tokens
├── <CommandPalette>   workspace-scoped via the command registry
├── <PreferencesDialog> theme, compact mode, and text size
├── <ForgetCredentialDialog> confirms Forget credential
├── <ShortcutsPanel>   keyboard shortcuts (`?`)
└── <ToastContainer>
```

The shell is constant across workspaces; only the `<Outlet />` body and the
right rail change. Workspace switching is done via React Router `NavLink`s
in the header, the `⌘1`-`⌘9` shortcuts, or the command palette. `⌘J` jumps
to the Logs workspace. Press `?` outside a field or dialog to open the
keyboard shortcuts panel. The modifier shortcuts (palette, workspaces, Logs,
compact mode, and refresh) also work from a text field or editor outside a
dialog; on macOS only `⌘` counts there, so `Ctrl+K` keeps its text-editing
meaning. Shortcut labels follow the platform: `⌘` and `⇧` on macOS, `Ctrl`
and `Shift` elsewhere (`lib/platform.ts`). The switcher is
`<nav aria-label="Workspaces">` with `aria-current="page"` on the active link.
It is not a tab list. Fit view (`f`, `⌘0`) and canvas zoom apply only on
Stack, never from a field, and are listed under Stack in the shortcuts panel.
`⌘F` is left to the browser.

`layout/WorkspaceShell.tsx` lays out every workspace except Stack and Traces
as an optional left rail, a center pane, and an optional right rail. It
measures its own root with `useContainerWidth`. Below 900 px of shell width
(`collapseRightBelowPx`) the right rail is not docked, and below 640 px
(`collapseLeftBelowPx`) neither is the left rail. An unmeasured width (0)
keeps both rails docked. An undocked rail renders no panel or separator, so
each set of docked panels keeps its own saved layout and a narrow session
never changes the wide one. Its content moves through a portal into a
non-modal overlay drawer on the same side, without remounting, so inspector
state and focus survive a resize. Each rail has a toggle on the center
pane's inner edge at every width ("Collapse left rail", "Expand right rail",
and so on, with `aria-expanded`, and `aria-controls` in drawer mode) that
does what `[` or `]` does: collapse or expand a docked panel, or open or
close a drawer. At most one drawer is open. The `rightKey` prop opens the
right drawer when a selection appears and closes it when the selection
clears; the `leftKey` prop closes the left drawer when the navigation
selection changes. Escape closes an open drawer unless a modal dialog is
open or a rail child handled the key, and a press outside the left drawer
closes it. The drawer has no transform once its entrance animation ends,
so fixed dialogs inside a rail still cover the viewport.

The Stack canvas is read-only: `graph/Canvas.tsx` passes
`nodesConnectable={false}`, `edgesFocusable={false}`, and
`deleteKeyCode={null}`, and its change handlers drop `remove` changes, so
Tab moves between nodes and no key hides an element until the next poll.
Enter or Space on a node, and a palette Inspect command, call
`requestInspectorFocus(nodeId)` on `useUIStore`; the Stack `Sidebar` focuses
the inspector heading once that node is shown and clears the request either
way. A mouse click clears it. Before the inspector closes (its close button
or Escape in AppShell while `sidebarOpen` is set), `restoreNodeFocus` in `lib/inspectorFocus.ts`
returns focus to the node, but only when focus is inside the inspector or
on `<body>`.

A connected gateway with no MCP servers and no resources shows
`graph/StackEmptyState.tsx`, a card in a `NodeToolbar` bound to the gateway
node, so it adds no node or edge and the fit key is unchanged. `Canvas`
decides visibility from `useStackStore` (connected, `gatewayInfo` set, not
loading, no `error`, and empty `mcpServers` and `resources`), never from the
node count, because the gateway node always exists once a status arrives.
The card's "Create new" calls `open()` with no argument, like the header.
The "MCP Server" and "Resource" quick-add links render only when a stack is
loaded, since the wizard gates those types on `stackName`. The card root
carries `nopan nowheel nodrag`: the toolbar portals into the pan and zoom
surface, so without them a gesture on the card moves the canvas.

`StackWorkspace` renders `stack/StackToolbar.tsx` above a body that holds
either the graph (`Canvas` and `AccessLens`) or `stack/StackTable.tsx`; the
other view is unmounted, not hidden. The workspace owns the transient filter
(name query and status facets) and passes it to both. `lib/stackRows.ts`
builds the rows from `useStackStore` nodes, so a row id is the node id and a
row click opens the same inspector as a node click. On the graph the filter
adds `filtered-out` to non-matching nodes and their edges and never removes
any. The view choice is `stackPrefs.view` on `useUIStore` (persisted, `null`
until the user picks one); with no choice, more than
`STACK_TABLE_AUTO_THRESHOLD` MCP servers at the first status opens the table.
Access Lens always uses the graph. `/` focuses the filter outside fields and
dialogs.

AppShell owns the only in-shell `<main>`. A workspace root that used to
render `<main>` is a `<section aria-labelledby>` with one `<h1>`, rendered
visibly by `WorkspaceHeader` in every state (loading and failed included).
Give every `<aside>` a distinct `aria-label`.

The header's action cluster is, left to right: the Persistence pill (while
connected), Appearance, Preferences, the palette, Help, Create new, and More
actions. Create new is the filled primary button, a 34 px square that gains
its "Create new" label from 1360 px. Help (`components/shell/HeaderMenu.tsx`)
is a menu with Keyboard shortcuts and Documentation. More actions holds
Refresh, Reload Config, and, while a credential is held, Forget credential…
below a separator. While a refresh or reload runs, the More actions trigger
shows a spinning loader, is `aria-busy`, and the running item is disabled.
Both menus follow the WAI-ARIA menu button pattern: opening focuses the first
enabled item (or the item container when none is enabled), arrow keys wrap,
Home and End jump, choosing an item closes the menu before running it, and
Escape returns focus to the trigger.

The workspace `nav` scrolls and the action cluster does not shrink, so every
cluster control stays mounted when the window is narrow. The left group is
`min-w-0 flex-1`; without that the nav has no bounded width and the pills stay
clipped. The active pill is always labeled and is scrolled into view when the
workspace changes. The other pills are labeled only when the left group is at
least 880 px wide. That rule is a container query (`@container/header` on the
left group, `@[880px]/header:` on the pills), not a viewport breakpoint,
because the cluster's width varies with the Persistence label; all labels fit
from about 1250 px. Every pill and cluster control is at least 32 by 32 px.
The Persistence label is visually hidden below 1440 px and stays in the
accessibility tree. Below `sm` the logo is shorter and the decorative
dividers drop.

AppShell's single column is `minmax(0, 1fr)`, not `1fr`: a bare `1fr` cannot
shrink below the widest row's content, so a full status bar would lay the
whole shell out wider than the window and clip the header actions. The
status bar is one 32 px row with `min-w-0` and three parts. The connection
pill does not shrink. The alert badges (spec, pin drift, findings, pending
authorization, and rate limits) come next; when space runs out their labels
truncate with an ellipsis while the icon and status dot stay, and each
badge's `title` or accessible name keeps the full text. The counters come
last, ending with "Updated" at the right edge. The counter group has a zero
flex basis, so it only gets the width left over and empties before any alert
label truncates. It wraps and clips (`flex-wrap`, `h-full`, `overflow-clip`)
so a counter that does not fit is hidden whole, "Updated" first. Use
`overflow-clip`, not `overflow-hidden`: focusing a hidden counter button must
not scroll the group. The alert group is `empty:hidden` and draws its
trailing divider as `::after`, so an idle bar shows one divider. Tokenizer,
format savings, and sessions also hide below 800 px. A disconnected error
truncates, and its `title` carries the full string.
`web/shell-layout.browser.mjs` checks this layout in Chromium.

The bar is a `<footer aria-label="Status bar">` landmark, a sibling of
`<main>`. The connection item is a `role="status"` region that holds only
the state label: a neutral "Connecting" before the first status response
(and while a credential prompt is open), then "Connected", or the red error
once an attempt has failed. The "Updated" age sits outside every live region
and ticks on its own one-second `useNow` clock, so it keeps advancing when
no poll lands. While disconnected it reads "Last updated" in the warning tone.

The minimum supported window width is 480 px (`MIN_SUPPORTED_WIDTH` in
`lib/viewport.ts`, read through `useViewportWidth`). Below it AppShell covers
`<main>` with `MinWidthState`: "Window too narrow", the reason, and a
"Continue anyway" button. A hard block would fail WCAG (F102), so the state
is dismissible; the dismissal lasts until the page reloads and is not saved.
Widening to 480 px or more removes the state without a click. The header,
status bar, toasts, palette, and global dialogs stay live. The state is only
raised (on a width change or navigation) when no `[aria-modal="true"]`
element is open, because most dialogs render in place inside the workspace
subtree and would otherwise sit inert under it. While it shows, a
`MutationObserver` on the workspace wrapper watches for a dialog opening
there (the palette can open the skill editor); the state then gives way
without counting as a dismissal and focus moves into the dialog. The
observer is scoped to the wrapper because the palette is itself
`aria-modal`. The wrapper (`<div className="contents" inert={blocked}>`
around `ErrorBoundary` and `Outlet`) is rendered at every width: adding and
removing it with the state would change the element type at that position
and remount the workspace, discarding drafts.

Use a container query (`@container` with `@md:` and similar variants) or
`useContainerWidth` for anything inside a `WorkspaceShell` pane or a
resizable panel, because the pane is not the viewport's width. Reserve
viewport breakpoints (`sm:`, `md:`) for viewport-sized surfaces such as the
wizard modal, the toast stack, and the minimum-width state. The Library
header is the measured case: `LibraryHeaderActions` moves Import, Sync
sources, Refresh, and the popout into a More actions `HeaderMenu` when its
row is narrower than
`LIBRARY_ACTIONS_COLLAPSE_BELOW` in `lib/libraryHeader.ts` (990 px with a
pending update pill, 850 px without). A width of 0 means unknown and keeps
the full cluster.

Detached windows (`/sidebar`, `/editor`, `/logs-window`, `/metrics-window`,
`/traces-window`, `/registry` → `/library-window`) render *outside*
AppShell. They are popout-friendly single-purpose pages and load on demand.

`AuthBoundary` wraps the router in `routes.tsx`, covering both the shell and
all six detached routes. During credential re-entry, existing content stays
mounted and inert behind `AuthPrompt`; verified window-only storage notices
remain visible after the prompt closes. Forget credential… in the main
window header's More actions menu, and Forget gateway credential… in the
command palette, open the same confirmation (`ForgetCredentialDialog`, mounted
in AppShell because the header's `backdrop-blur` would trap it). Confirming
clears both storage keys and opens the prompt; Cancel, Escape, or the backdrop
leave the credential in place. Detached windows do not mount either
control. A forget in the main window reaches them through the existing
storage event.

## Store layout (Zustand slices pattern)

Cross-workspace shell state lives on `useUIStore` via composed slices:

```
useUIStore
├── WorkspaceSlice       activeWorkspace, setActiveWorkspace
├── CompactModeSlice     compactMode (per workspace), set/toggle helpers
└── (UIState extras)     sidebarOpen, command palette, detached flags, …
```

Each workspace owns its own data store and never imports another workspace's
store:

- Stack     → `useStackStore`
- Library   → `useRegistryStore`

Several supporting stores (`useSpecStore`, `useAuthStore`, `useContextStore`, `usePinsStore`,
`useTelemetryStore`, `useTracesStore`, `useVaultStore`, `useWizardStore`,
`useCanvasActionsStore`, `useShellActionsStore`, `useShellNavStore`) sit alongside the workspace stores. `useCanvasActionsStore`
holds fit and zoom until `graph/Canvas.tsx` registers them, so the shell does
not import React Flow. `useShellActionsStore` holds the header's Reload Config
flow, which `Header` registers on mount, so the palette command runs the same
path (including the spec diff modal) without the header using the registry. `useShellNavStore` holds `goToStack`, the
guarded navigation to Stack that `AppShell` registers on mount, so store
actions such as `openServerInspector` (the needs-auth toast and status bar
chip) can open the inspector from any workspace; it is a no-op until
registered. `useAuthStore` owns the credential, verification
attempt, and credential generation shared by protected requests and their
consumers. Data stores and polling hooks consult that generation to discard
superseded results.

Use the endpoint parsers in `lib/api.ts`; their shared transport is
`lib/gatewayRequest.ts`. It enforces same-origin requests, manual redirect
refusal, credential header validation, and gateway-rejection provenance.
Component continuations must also guard mount lifetime and credential
generation before updating state or starting another operation; Connections
is a regression-tested example. Re-verification resumes eligible reads,
never a save or other mutation.

The browser does not consume `/sse`. That route is a legacy negotiation hint
for clients that still probe the retired transport, and it closes after the
hint, so it carries no shutdown signal. Status polling is the disconnect signal.

## Shared primitives

These primitives live under `web/src/components/` and are consumed across
workspaces. Reach for them before duplicating UI:

| Primitive | Location | Used by |
|---|---|---|
| `CanvasBase` | `components/canvas/` | Stack `graph/Canvas.tsx` |
| `InspectorHeader` | `components/inspector/` | Inspectors that need the standard icon + title + close/popout strip. `headingRef` makes the title a focus target (`tabIndex={-1}`); only the Stack inspector passes it |
| `InspectorSection` | `components/inspector/` | Stack `Sidebar.tsx`, `GatewaySidebar.tsx`, `DetachedSidebarPage.tsx` (collapsible section pattern; contents mount on first open; an optional `summary` is a second header line that reads while collapsed) |
| `InspectorValueRow` | `components/inspector/` | Identifier rows in the Stack inspector, the detached inspector, and `SourceProvenance`. The value wraps (`break-all`), is focusable, and has no hover `title`; `copyLabel` adds a `CopyButton` |
| `CopyButton` | `components/ui/` | Copies through `copyTextToClipboard`, stops click propagation, and shows the result in place for 1.5 seconds ("Copied" or "Copy failed") with a visually hidden status announcement. `label` is the accessible name and names the field. Use it where a toast is unavailable or too loud; `copyWithToast` remains for toolbar copies |
| `WorkspaceHeader` | `components/ui/` | Every workspace and Library segment. The one visible `<h1>` (`title`, `headingId`), then an optional `switcher`, a one-line truncating `summary` at `text-xs`, and right-aligned `actions`; `children` add rows below. `compact` selects `py-2`. `rowRef` reaches the title row for callers that measure it (the Library header). Empty slots render no wrapper |
| `Tabs` / `Tab` / `TabPanel` | `components/ui/` | Keyboard-operable tab lists. Arrow keys move and select. `InspectorTabList` / `InspectorTabButton` are wrappers that keep inspector chrome. `variant="segmented"` renders an inline segment control (the Traces and Runs switch); `SEGMENT_GROUP_CLASS` and `segmentClass` (`ui/segment.ts`) give a non-tab group, such as `LibraryKindSwitch`, the same chrome |
| `EmptyState` | `components/ui/` | Anywhere a "no items / no selection" affordance is needed |
| `FailedState` | `components/ui/` | Failed fetch with nothing loaded. Title is `<feature> unavailable`, with Retry when `onRetry` is set. Not a spinner or an empty collection |
| `FailureBanner` | `components/ui/` | Failed refresh when data is already on screen: one alert row, with Retry when `onRetry` is set. `lastUpdated` adds a static "Last updated HH:MM:SS"; `className` overrides the default margins |
| `refreshPhase` | `lib/refreshPhase.ts` | The single definition of the rule above. Maps `{ error, hasLoaded }` to `loading` (skeleton), `failed` (`FailedState`), `stale` (data plus `FailureBanner`), or `ready`. `hasLoaded` means a load completed for what is selected now |
| `HeaderMenu` | `components/shell/` | WAI-ARIA menu button on `Popover`: the header's Help and More actions menus and the Library header's collapsed More actions menu. `size` and `variant` pass through to the `IconButton` trigger |
| `Popover` | `components/ui/` | Theme picker, telemetry pill, the header's Help and More actions menus (`HeaderMenu`), overlays menu, log level filter, log view options, the skill editor's validation messages, and `SelectMenu` (the detached inspector's node menu and the detached Logs source menu). Escape and a programmatic close return focus to the trigger; an outside click does not. The wizard's variables popover and variable-set selector do not use it: they portal their own dropdowns to `document.body` to escape the wizard's focus trap and own Escape and Tab |
| `DetachedWindowFrame` | `components/detached/` | Every detached page renders through it. Owns the grain, header, footer, `document.title`, the window's single `ToastContainer`, the fullscreen toggle (warns when the browser refuses), and the disconnected banner. It fetches nothing: the page passes `connection` from its one `useSharedStatus` call and holds `lost` false while its own failure notice is showing. The editor popout omits the header, footer, and banner because the flush editor covers them |
| `Field` | `components/ui/` | Label, control, error, then help. Owns the required marker and wires `htmlFor`, `aria-invalid`, and `aria-describedby`. Pass plain label text. A different sibling order keeps its wrapper and uses `useId` |
| `IconButton` | `components/ui/` | Requires `tooltip` or `ariaLabel`, and renders `type="button"`. `title` alone is not an accessible name. `size="lg"` is a 44 px square, border included; the default size stays `md`. `spinning` spins the glyph (never the button) and sets `aria-busy`. `hasPopup` and `expanded` render `aria-haspopup` and `aria-expanded`, and are absent when omitted |
| `Switch` | `components/ui/` | The only `role="switch"` control. `BoolToggle` and the Connections link toggle render it. `label` is the accessible name |
| `Badge` | `components/ui/` | Status badges keep the dot. `tone` (`neutral`, `primary`, `success`, `warning`) is a dot-free pill. Connections client badges use `tone` |
| `PopoutButton` | `components/ui/` | Opens a detached window. `detached` switches the icon and reads "Detached. Click to focus". Call sites leave it enabled and focus that window |
| `PreferencesDialog` | `components/shell/` | Header and palette. Theme, compact mode for the seven workspaces that read it, and text size |
| `ShortcutsPanel` | `components/shell/` | Opens on `?`, from the palette, and from Preferences. One static binding table, grouped by scope |

Every detached page (`pages/Detached*Page.tsx`) renders outside `AppShell`, so `DetachedWindowFrame` mounts its `ToastContainer`. No page mounts one itself. Do not add one to a page, `AuthBoundary`, or `routes.tsx`: a second container in the same document shows every toast twice.

## Layering

Overlay stacking uses named steps from `web/src/index.css`. Tailwind maps `--z-index-<name>` to the `z-<name>` utility.

| Step | Value | Use |
|---|---|---|
| `raised` | 10 | Local lift inside a stacking context (scrims, handles, badges) |
| `sticky` | 20 | Sticky table headers and in-flow overlays that must stay above raised content |
| `header` | 30 | App header, slide-over, and the Access Lens dirty bar |
| `overlay` | 40 | Loading scrims and the vault drag dropzone |
| `modal` | 50 | Dialogs, the creation wizard, the spec diff, and confirms |
| `popover` | 60 | Menus and portaled dropdowns. Above `modal` so a dropdown opened from a dialog paints over it |
| `toast` | 70 | Toasts and window-only credential notices |

`popover` outranks `modal` because portaled dropdowns opened from inside a modal must paint over it. A popover inside the header (`relative z-header`, its own stacking context) only ranks against header children, so it cannot reach the toast layer. Reload results use that toast layer. A fixed dialog inside a `backdrop-blur` header is trapped the same way. The metrics clear confirm is portaled to `document.body` so it covers the viewport. Run recording confirms stay in the workspace and are not portaled.

Toasts raised with `deferWhileModal` (the pin drift and findings toasts) are held, with their timers paused, while a dialog is open, and show for their remaining time once the last dialog closes. "A dialog is open" comes from `web/src/lib/modalPresence.ts`: `useFocusTrap` registers with it while active, and a dialog that traps focus another way (`CommandPalette`) registers itself with `enterModal`.

`CanvasBase` is intentionally small (~120 LOC) - it owns the React Flow
boilerplate (wrapper element, Background layers, proOptions) and exposes
workspace-specific props.

## What to do / what not to do

**Do**

- Use the slices pattern in `useUIStore` for cross-workspace state.
- Compose new primitives in `components/inspector/`, `components/canvas/`,
  or `components/ui/` when you find yourself duplicating UI shells across
  workspaces.
- Register workspace-specific command-palette entries via
  `useCommandRegistry().registerCommands(scope, commands)` on mount; clean
  up on unmount. See `useLibraryCommands` for the pattern.
  Give each command the `PaletteSection` its workspace's Current Context
  group reads (`WORKSPACE_PALETTE_SECTION` in
  `lib/paletteWorkspaceCommands.ts`; Tools and Connections use the `tools`
  and `connections` sections). `aliases` lists strings that name the command
  outright: a query equal to one ranks it first, and each alias must also
  appear in the label or a keyword. Search ranks by match quality (exact,
  prefix, word start, keyword prefix, substring, subsequence, in
  `lib/paletteScore.ts`) and orders by usage only within a tier.
- The shell registers a "Go to" command per `WORKSPACE_CONFIG` entry and
  the URL-param view commands for Tools, Metrics, Traces, Connections, and
  Variables (`lib/paletteWorkspaceCommands.ts`). Palette and `⌘1`-`⌘9`
  navigation goes through `navigateToWorkspace` (`lib/workspaceNav.ts`),
  which keeps the current URL when the target is the current workspace and
  otherwise asks the shared leave guard first.
- Spread `listNavProps` from `hooks/useListNav.ts` on the element that
  holds a `useListNav` list's rows, and `listNavRowProps` on each focusable
  row. Arrows, Home, End, `e`, `d`, and Enter act only when focus is on the
  page or inside the marked element, and not while a modal dialog is open.
  Enter on a focused control (a
  button, a link, or a row with a control role) is left to that control. A
  row that is itself a control handles its own Enter, as `VariableRow`
  does. When a list key moves the selection, focus follows it only from a
  marked row that exposes `aria-selected="true"` or `aria-current="true"`
  when selected; an index that shifts because rows changed leaves focus
  where it is. Don't put
  toolbars or filters inside the marked element.
- Enroll an overlay that closes on Escape through `hooks/useEscapeLayer.ts`
  (`modal: true` for a dialog, `false` for a slide-over) instead of adding
  its own `keydown` listener, so one Escape closes only the topmost layer.
  Window or document shortcut handlers return early when
  `isModalDialogOpen()` from `lib/dom.ts` is true.
- Use Tailwind **semantic tokens** (`bg-primary`, `text-secondary`,
  `border-status-error`, `bg-surface-overlay-2`, `stroke-chart-blue`) -
  never raw palette classes such as `bg-amber-500`. Destructive fills use
  `bg-danger`, and `border-border-input` is the 3:1 boundary for form
  controls. Card tints use `surface-overlay-1` through
  `surface-overlay-3`. Chart series use
  `chart-blue`, `chart-cyan`, `chart-pink`, `chart-orange`, and
  `chart-purple`. The set lives in `web/src/index.css` and re-keys with
  `data-theme`.
- Use the type scale for UI text: `text-3xs` (10 px at the default
  root), `text-2xs` (11 px), `text-xs` (12 px), and `text-sm` (14 px).
  Bracketed pixel sizes are not used in new code. `.log-text`,
  `.text-zoom`, and `.skill-md` keep their user-adjustable pixel sizes.
- Code-split each workspace, detached page, and the header dialogs with
  `React.lazy()` + `<Suspense>`. `routes.tsx` and `Header.tsx` already
  wire this up.
- Toast a caught failure with `describeError` from `web/src/lib/describeError.ts`.
  Pass `described.text` to `showToast`, and `described.fullDetail` as
  `detail` when it is set. A `detail` that differs from the message is the
  message's title and sits behind a Details toggle on the toast. Skip the
  toast when `isCancellation` is true.

**Don't**

- Don't build a `useWorkspaceStore` "store of stores" that imports the
  other Zustand stores. Use a slice or pass an action through props/context
  instead.
- Don't mix workspace bundles. A workspace component should not import from
  another workspace's folder. Shared utilities go in `components/canvas/`,
  `components/ui/`, or `components/inspector/`.
- Don't add a network poll with `setInterval`. Use `useInterval` so a hidden
  document pauses. The shell status cycle (status, tools, and clients)
  heartbeats every 30 seconds while hidden. Inventory and other workspace
  polls pause. A relative-time label that must advance between polls takes
  its clock from `useNow` in its own leaf component, as the status bar's
  "Updated" age does.
- Don't put behavior-changing logic in `CanvasBase`. Background layers,
  control bars, overlays, and node types all belong in the workspace canvas.
- Don't hand-roll a workspace header or render an `sr-only` page title.
  Use `WorkspaceHeader`, which owns the visible `<h1>`, and pass view
  switchers through `switcher` rather than overriding `Tab` classes.
- Don't render `<main>` inside a workspace, or `role="tab"` on a route link.
  AppShell owns the in-shell landmark. Give every `<aside>` a distinct
  `aria-label`.
- Don't pass a percent to `react-resizable-panels` as a number. v4 reads
  numbers as pixels. Pixel minimums stay numbers; percent defaults and
  the center floor are strings (the center floor is `"20%"`). Widen a
  separator hit target with `resizeTargetMinimumSize`
  (`{ coarse: 24, fine: 16 }`), not a pseudo-element.
- Don't reveal a row action with only `group-hover:`. Tailwind's `hover:`
  variant does not fire when the device has no hover. Add
  `hover-none:opacity-100` and `group-focus-within:opacity-100`. A named
  group keeps its suffix.
- Don't style focus per component. The global `:focus-visible` rule in
  `web/src/index.css` draws a 2px primary outline on every control. Don't
  add `outline-none`, `outline-hidden`, `outline-0`, focus rings
  (`focus:ring-*`, `focus-visible:ring-*`, `focus-within:ring-*`), or
  focus border or background classes (`focus:border-*`, `focus:bg-*`, and
  their `focus-visible:` forms), and don't give an outline a translucent
  color.
  When an ancestor clips the outline, move it inside the control with
  `focus-visible:-outline-offset-2`. Mark the current row of an
  active-descendant list with `outline-2 -outline-offset-2 outline-primary`
  gated on focus inside its list: a named group on the list root and
  `group-focus-within/<name>:` on the row (cmdk rows also gate on
  `data-[selected=true]:`, since cmdk selects the first row on render).
  `__tests__/focusIndicators.test.ts` enforces this for `.ts` and `.tsx`
  files.
- Don't start a custom drag with `mousedown` alone. Pointer events,
  capture, and `touch-none` cover mouse, touch, and pen. `pointerup`,
  `pointercancel`, and window `blur` share one cleanup
  (`web/src/lib/pointerCapture.ts`).
- Don't hide a pack chip name with a min-width container query. The name
  uses `@max-[339px]:hidden`, so it stays visible unless a query container
  ancestor is narrower than 339 px. A min-width query is false with no
  container and would blank the name on every other surface.
- Don't use white or black color utilities for surfaces that must follow
  the theme. Overlay tokens are already translucent, so do not add a slash
  modifier. Filled danger surfaces use `bg-danger` (`hover:bg-danger-hover`)
  with white text; `status-error` is for text, borders, tints, and dots, not
  a fill under white text. The raw-palette lint rule rejects every default
  Tailwind color family outside tests, including chart files.
- Don't put a slash opacity modifier on a text token (`text-text-muted/60`,
  `text-text-secondary/80`, `text-primary/70`), including placeholder and
  hover states such as `placeholder:text-text-muted/40` and
  `hover:text-primary/80`. The translucent color blends into the surface
  and drops below 4.5:1. Use `text-text-muted`, `text-text-secondary`, or
  `text-primary` at full opacity, and `hover:text-primary-light` for hover
  feedback on a `text-primary` link. The one exemption is a decorative icon
  with `aria-hidden` on the same element; lint enforces this.
- Don't size UI text with a bracketed pixel class. Use `text-3xs`,
  `text-2xs`, `text-xs`, or `text-sm`.
- Don't pass a raw error to `showToast`, and don't use `String(err)`.
  `messageOf` and `asError` cover a string or an `Error`.

## File map

```
web/src/components/
├── auth/             AuthBoundary, AuthPrompt
├── shell/            AppShell, WorkspaceSwitcher, RootRedirect
├── workspaces/       StackWorkspace, LibraryWorkspace, … (one per top-level workspace)
├── layout/           Header, StatusBar, Sidebar (Stack inspector), WorkspaceShell
├── inspector/        InspectorHeader, InspectorSection, InspectorValueRow, InspectorTabList (wraps ui/Tabs)
├── canvas/           CanvasBase                                              ← shared React Flow scaffolding
├── graph/            Stack Canvas + custom node types
├── stack/            Stack toolbar, table view, and Access Lens
├── registry/         Library workspace (LibraryGrid, SkillEditor, …)
├── context/          Library Context segment (ContextWorkspace, GlobalContextEditor, ContextInspector)
├── palette/          CommandPalette (workspace-scoped via useCommandRegistry)
└── ui/               Badge, Toast, IconButton, Field, EmptyState, Tabs, …
```
