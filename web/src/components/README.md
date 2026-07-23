# Web Components

gridctl's SPA is structured around a single application shell that hosts the
URL-routable workspaces. This file documents the shell architecture, the
Zustand store layout, and the do-and-don't conventions every new component
should follow.

## Shell architecture

```
<AppShell>             web/src/components/shell/AppShell.tsx
├── <Header>           web/src/components/layout/Header.tsx
│   └── <WorkspaceSwitcher>   pills bound to React Router NavLinks
├── <Outlet />         renders the active workspace body
│   ├── <StackWorkspace>      /stack  (hosts the ?spec=1 slide-over)
│   ├── <LibraryWorkspace>    /library  (also /library/:skillName)
│   ├── <VaultWorkspace>      /vault
│   ├── <ToolsWorkspace>      /tools
│   ├── <MetricsWorkspace>    /metrics
│   ├── <PinsWorkspace>       /pins
│   ├── <LogsWorkspace>       /logs
│   └── <TracesWorkspace>     /traces
├── <StatusBar>        connection · servers · sessions · tokens · spec
├── <CommandPalette>   workspace-scoped via the command registry
└── <ToastContainer>
```

The shell is constant across workspaces; only the `<Outlet />` body and the
right rail change. Workspace switching is done via React Router `NavLink`s
in the header, the `⌘1`-`⌘8` shortcuts, or the command palette. `⌘J` jumps
to the Logs workspace.

Detached windows (`/sidebar`, `/editor`, `/logs-window`, `/metrics-window`,
`/traces-window`, `/registry` → `/library-window`) render *outside*
AppShell - they're popout-friendly single-purpose pages.

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
alongside the workspace stores; they're feature-scoped and have no
cross-store coupling.

## Shared primitives

These primitives live under `web/src/components/` and are consumed by both
workspaces. Reach for them before duplicating UI:

| Primitive | Location | Used by |
|---|---|---|
| `CanvasBase` | `components/canvas/` | Stack `graph/Canvas.tsx` |
| `InspectorHeader` | `components/inspector/` | Inspectors that need the standard icon + title + close/popout strip |
| `InspectorSection` | `components/inspector/` | Stack `Sidebar.tsx`, `DetachedSidebarPage.tsx` (collapsible section pattern) |
| `InspectorTabList` / `InspectorTabButton` | `components/inspector/` | Library tab list a11y wrapper |
| `EmptyState` | `components/ui/` | Anywhere a "no items / no selection" affordance is needed |

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

## File map

```
web/src/components/
├── shell/            AppShell, WorkspaceSwitcher, RootRedirect
├── workspaces/       StackWorkspace, LibraryWorkspace, … (one per top-level workspace)
├── layout/           Header, StatusBar, Sidebar (Stack inspector), WorkspaceShell
├── inspector/        InspectorHeader, InspectorSection, InspectorTabList    ← shared primitives
├── canvas/           CanvasBase                                              ← shared React Flow scaffolding
├── graph/            Stack Canvas + custom node types
├── registry/         Library workspace (LibraryGrid, SkillEditor, …)
├── palette/          CommandPalette (workspace-scoped via useCommandRegistry)
└── ui/               Badge, Toast, IconButton, EmptyState, …
```
