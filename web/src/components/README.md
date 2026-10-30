# Web Components

gridctl's SPA is structured around a single application shell that hosts the
URL-routable workspaces. This file documents the shell architecture, the
Zustand store layout, and the do-and-don't conventions every new component
should follow.

## Shell architecture

```
<AppShell>             web/src/components/shell/AppShell.tsx
├── <Header>           web/src/components/layout/Header.tsx
│   └── <WorkspaceSwitcher>   <nav aria-label="Workspaces"> of NavLinks
├── <Outlet />         renders the active workspace body
│   ├── <StackWorkspace>      /stack  (hosts the ?spec=1 slide-over)
│   ├── <LibraryWorkspace>    /library  (also /library/:skillName)
│   ├── <VaultWorkspace>      /vault
│   ├── <ToolsWorkspace>      /tools
│   ├── <MetricsWorkspace>    /metrics
│   ├── <PinsWorkspace>       /pins
│   ├── <LogsWorkspace>       /logs
│   ├── <TracesWorkspace>     /traces (Runs tab at ?view=runs)
│   └── <ConnectionsWorkspace> /connections
├── <StatusBar>        connection · servers · sessions · tokens · spec
├── <CommandPalette>   workspace-scoped via the command registry
└── <ToastContainer>
```

The shell is constant across workspaces; only the `<Outlet />` body and the
right rail change. Workspace switching is done via React Router `NavLink`s
in the header, the `⌘1`-`⌘9` shortcuts, or the command palette. `⌘J` jumps
to the Logs workspace. The switcher is `<nav aria-label="Workspaces">` with
`aria-current="page"` on the active link. It is not a tab list. Fit view
(`f`, `⌘0`) and canvas zoom apply only on Stack. `⌘F` is left to the browser.

AppShell owns the only in-shell `<main>`. A workspace root that used to
render `<main>` is a `<section aria-labelledby>` with one `<h1>`. Give every
`<aside>` a distinct `aria-label`.

The workspace `nav` scrolls and the action cluster does not shrink, so
Create, Refresh, Reload Config, theme, and the palette stay mounted when
the window is narrow. The left group is `min-w-0 flex-1`; without that
the nav has no bounded width and the pills stay clipped. Below `sm` the
logo is shorter, the persistence label is visually hidden, and the
decorative dividers drop. The status bar stays one row; tokenizer, format
savings, and sessions hide at 800 px and below. A disconnected error
truncates, and its `title` carries the full string.

Detached windows (`/sidebar`, `/editor`, `/logs-window`, `/metrics-window`,
`/traces-window`, `/registry` → `/library-window`) render *outside*
AppShell - they're popout-friendly single-purpose pages.

`AuthBoundary` wraps the router in `routes.tsx`, covering both the shell and
all six detached routes. During credential re-entry, existing content stays
mounted and inert behind `AuthPrompt`; verified window-only storage notices
remain visible after the prompt closes.

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

Several supporting stores (`useSpecStore`, `useAuthStore`, `usePinsStore`,
`useTelemetryStore`, `useTracesStore`, `useVaultStore`, `useWizardStore`) sit
alongside the workspace stores. `useAuthStore` owns the credential, verification
attempt, and credential generation shared by protected requests and their
consumers. Data stores and polling hooks consult that generation to discard
superseded results.

Use the endpoint parsers in `lib/api.ts`; their shared transport is
`lib/gatewayRequest.ts`. Streaming consumers use that transport directly. It
enforces same-origin requests, manual redirect refusal, credential header
validation, and gateway-rejection provenance. Component continuations must
also guard mount lifetime and credential generation before updating state or
starting another operation; Connections is a regression-tested example.
Re-verification resumes eligible reads, never a save or other mutation.

`useSSEShutdown` consumes `/sse` negotiation through cancellable fetch. The
endpoint closes normally after the hint, so EOF does not signal shutdown;
status polling remains the disconnect fallback.

## Shared primitives

These primitives live under `web/src/components/` and are consumed across
workspaces. Reach for them before duplicating UI:

| Primitive | Location | Used by |
|---|---|---|
| `CanvasBase` | `components/canvas/` | Stack `graph/Canvas.tsx` |
| `InspectorHeader` | `components/inspector/` | Inspectors that need the standard icon + title + close/popout strip |
| `InspectorSection` | `components/inspector/` | Stack `Sidebar.tsx`, `DetachedSidebarPage.tsx` (collapsible section pattern) |
| `Tabs` / `Tab` / `TabPanel` | `components/ui/` | Keyboard-operable tab lists. Arrow keys move and select. `InspectorTabList` / `InspectorTabButton` are wrappers that keep inspector chrome |
| `EmptyState` | `components/ui/` | Anywhere a "no items / no selection" affordance is needed |
| `FailedState` | `components/ui/` | Failed fetch with nothing loaded. Title is `<feature> unavailable`, with Retry when `onRetry` is set. Not a spinner or an empty collection |
| `FailureBanner` | `components/ui/` | Failed refresh when data is already on screen: one alert row and Retry |
| `Popover` | `components/ui/` | Theme picker, telemetry pill, overlays menu, log level filter, log view options. Escape and a programmatic close return focus to the trigger; an outside click does not |
| `Field` | `components/ui/` | Label, control, error, then help. Owns the required marker and wires `htmlFor`, `aria-invalid`, and `aria-describedby`. Pass plain label text. A different sibling order keeps its wrapper and uses `useId` |
| `IconButton` | `components/ui/` | Requires `tooltip` or `ariaLabel`, and renders `type="button"`. `title` alone is not an accessible name. `size="lg"` is a 44 px square, border included; the default size stays `md` |

## Layering

Overlay stacking uses named steps from `web/src/index.css`. Tailwind maps `--z-index-<name>` to the `z-<name>` utility.

| Step | Value | Use |
|---|---|---|
| `raised` | 10 | Local lift inside a stacking context (scrims, handles, badges) |
| `sticky` | 20 | Sticky table headers and in-flow overlays that must stay above raised content |
| `header` | 30 | App header, slide-over, and the Access Lens dirty bar |
| `overlay` | 40 | Loading scrims, the vault drag dropzone, the shutdown pill, and the header reload banner |
| `modal` | 50 | Dialogs, the creation wizard, the spec diff, and confirms |
| `popover` | 60 | Menus and portaled dropdowns. Above `modal` so a dropdown opened from a dialog paints over it |
| `toast` | 70 | Toasts and window-only credential notices |

`popover` outranks `modal` because portaled dropdowns opened from inside a modal must paint over it. A popover inside the header (`relative z-header`, its own stacking context) only ranks against header children, so it still paints over the reload banner and cannot reach the toast layer. A fixed dialog inside a `backdrop-blur` header is trapped the same way. The metrics clear confirm is portaled to `document.body` so it covers the viewport. Run recording confirms stay in the workspace and are not portaled.

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
- Use Tailwind **semantic tokens** (`bg-primary`, `text-secondary`,
  `border-status-error`) - never raw color literals like `bg-amber-500` in
  new code.
- Code-split each workspace with `React.lazy()` + `<Suspense>`; the router
  already wires this up in `routes.tsx`.

**Don't**

- Don't build a `useWorkspaceStore` "store of stores" that imports the
  other Zustand stores. Use a slice or pass an action through props/context
  instead.
- Don't mix workspace bundles. A workspace component should not import from
  another workspace's folder. Shared utilities go in `components/canvas/`,
  `components/ui/`, or `components/inspector/`.
- Don't put behavior-changing logic in `CanvasBase`. Background layers,
  control bars, overlays, and node types all belong in the workspace canvas.
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
- Don't start a custom drag with `mousedown` alone. Pointer events,
  capture, and `touch-none` cover mouse, touch, and pen. `pointerup`,
  `pointercancel`, and window `blur` share one cleanup
  (`web/src/lib/pointerCapture.ts`).
- Don't hide a pack chip name with a min-width container query. The name
  uses `@max-[339px]:hidden`, so it stays visible unless a query container
  ancestor is narrower than 340 px. A min-width query is false with no
  container and would blank the name on every other surface.

## File map

```
web/src/components/
├── auth/             AuthBoundary, AuthPrompt
├── shell/            AppShell, WorkspaceSwitcher, RootRedirect
├── workspaces/       StackWorkspace, LibraryWorkspace, … (one per top-level workspace)
├── layout/           Header, StatusBar, Sidebar (Stack inspector), WorkspaceShell
├── inspector/        InspectorHeader, InspectorSection, InspectorTabList (wraps ui/Tabs)
├── canvas/           CanvasBase                                              ← shared React Flow scaffolding
├── graph/            Stack Canvas + custom node types
├── registry/         Library workspace (LibraryGrid, SkillEditor, …)
├── palette/          CommandPalette (workspace-scoped via useCommandRegistry)
└── ui/               Badge, Toast, IconButton, Field, EmptyState, Tabs, …
```
