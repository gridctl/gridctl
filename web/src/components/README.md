# Web Components

gridctl's SPA is structured around a single application shell that hosts three
URL-routable workspaces. This file documents the shell architecture, the
Zustand store layout, and the do-and-don't conventions every new component
should follow.

## Shell architecture

```
<AppShell>             web/src/components/shell/AppShell.tsx
├── <Header>           web/src/components/layout/Header.tsx
│   └── <WorkspaceSwitcher>   pills bound to React Router NavLinks
├── <Outlet />         renders the active workspace body
│   ├── <TopologyWorkspace>   /topology
│   ├── <SkillsWorkspace>     /skills
│   └── <RunsWorkspace>       /runs   (also /runs/:id → RunDetailWorkspace)
├── <BottomPanel>      Logs / Metrics / Spec / Traces / Runs / Pins
├── <StatusBar>        connection · servers · sessions · tokens · spec
├── <CommandPalette>   workspace-scoped via the command registry
└── <ToastContainer>
```

The shell is constant across workspaces; only the `<Outlet />` body and the
right rail change. Workspace switching is done via React Router `NavLink`s
in the header, the `⌘1` / `⌘2` / `⌘3` shortcuts, or the command palette.

Detached windows (`/sidebar`, `/logs`, `/editor`, `/metrics`, `/vault`,
`/traces`, `/registry`) render *outside* AppShell — they're popout-friendly
single-purpose pages that accept a `?workspace=` query param to render the
appropriate workspace content.

## Store layout (Zustand slices pattern)

Cross-workspace shell state lives on `useUIStore` via composed slices:

```
useUIStore
├── WorkspaceSlice       activeWorkspace, setActiveWorkspace
├── CompactModeSlice     compactMode (per workspace), set/toggle helpers
└── (UIState extras)     sidebarOpen, bottomPanelOpen, command palette, …
```

Each workspace owns its own data store and never imports another workspace's
store:

- Topology  → `useStackStore`
- Skills    → component-local state + agent-API hooks (`useDevSocket`, `useRunTrace`)
- Runs      → `useRunsStore`

Several supporting stores (`useSpecStore`, `useAuthStore`, `usePinsStore`,
`useRegistryStore`, `useTelemetryStore`, `useTracesStore`, `useVaultStore`,
`useWizardStore`, `usePlaygroundStore`) sit alongside the workspace stores;
they're feature-scoped and have no cross-store coupling.

## Shared primitives

These primitives live under `web/src/components/` and are consumed by every
workspace. Reach for them before duplicating UI:

| Primitive | Location | Used by |
|---|---|---|
| `CanvasBase` | `components/canvas/` | Topology `graph/Canvas.tsx`, Skills `agent/ide/Canvas.tsx` |
| `InspectorHeader` | `components/inspector/` | Inspectors that need the standard icon + title + close/popout strip |
| `InspectorSection` | `components/inspector/` | Topology `Sidebar.tsx`, `DetachedSidebarPage.tsx` (collapsible section pattern) |
| `InspectorTabList` / `InspectorTabButton` | `components/inspector/` | Skills `SkillSidebar.tsx` (tablist a11y wrapper) |
| `EmptyState` | `components/ui/` | Skills Canvas, anywhere a "no items / no selection" affordance is needed |

`CanvasBase` is intentionally small (~120 LOC) — it owns the React Flow
boilerplate (wrapper element, Background layers, proOptions) and exposes
workspace-specific props. The Topology canvas and Skills canvas remain
separate components that *compose* CanvasBase; they do **not** share a
polymorphic super-canvas.

## What to do / what not to do

**Do**

- Use the slices pattern in `useUIStore` for cross-workspace state.
- Compose new primitives in `components/inspector/`, `components/canvas/`,
  or `components/ui/` when you find yourself duplicating UI shells across
  workspaces.
- Register workspace-specific command-palette entries via
  `useCommandRegistry().registerCommands(scope, commands)` on mount; clean
  up on unmount. See `useRunsCommands` and `useSkillsCommands` for the
  pattern.
- Use Tailwind **semantic tokens** (`bg-primary`, `text-secondary`,
  `border-status-error`) — never raw color literals like `bg-amber-500` in
  new code.
- Code-split each workspace with `React.lazy()` + `<Suspense>`; the router
  already wires this up in `routes.tsx`.

**Don't**

- Don't build a `useWorkspaceStore` "store of stores" that imports the
  other Zustand stores. Use a slice or pass an action through props/context
  instead.
- Don't unify the Topology and Skills canvases into a single polymorphic
  canvas with overlay rendering modes. They share infrastructure, not
  rendering. Compose `CanvasBase` from two separate components.
- Don't build a polymorphic Inspector with a provider registry. The two
  Inspectors (`layout/Sidebar.tsx` for Topology, `agent/ide/NodeDetail.tsx`
  for Skills) serve different mental models. Share *primitives*, not the
  shell.
- Don't mix workspace bundles. A workspace component should not import from
  another workspace's folder (`components/graph/*` from a Skills file, for
  example). Shared utilities go in `components/canvas/`, `components/ui/`,
  or `components/inspector/`.
- Don't put behavior-changing logic in `CanvasBase`. Background layers,
  control bars, overlays, and node types all belong in the workspace canvas.

## File map

```
web/src/components/
├── shell/            AppShell, WorkspaceSwitcher, RootRedirect, AgentRedirect
├── workspaces/       TopologyWorkspace, SkillsWorkspace, RunsWorkspace, RunDetailWorkspace
├── layout/           Header, BottomPanel, StatusBar, Sidebar (Topology inspector)
├── inspector/        InspectorHeader, InspectorSection, InspectorTabList    ← shared primitives
├── canvas/           CanvasBase                                              ← shared React Flow scaffolding
├── graph/            Topology Canvas + custom node types
├── agent/ide/        Skills Canvas + NodeDetail + SkillSidebar + NodeList
├── skills/           SkillsWorkspace command registry, DetachedSkillsSidebar
├── runs/             RunsGrid, RunInspector, RunWaterfall, useGlobalRunsStream
├── palette/          CommandPalette (workspace-scoped via useCommandRegistry)
└── ui/               Badge, Toast, IconButton, EmptyState, …
```
