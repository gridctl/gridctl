# Gridctl Web Design System: "Obsidian Observatory"

This document defines the visual language, behavior, and code standards for the Gridctl web interface.
**All UI changes must strictly adhere to these guidelines.**

## 1. Design Philosophy
**Aesthetic:** Dark, scientific, mission control, "observatory".
**Core values:**
*   **Precision:** Thin borders, mono fonts for data, distinct status indicators.
*   **Depth:** Heavy use of layering via "glass morphism" (blur + translucency) and Z-axis separation.
*   **Energy:** Dark backgrounds contrasted with glowing "active" elements (Amber/Teal).
*   **Atmosphere:** Subtle grain, glows, and organic pulses to make the interface feel "alive" rather than static.

## 2. Color Palette (Tailwind Tokens)

### Backgrounds & Surfaces
*   `bg-background` (`#08080a`): Global app background.
*   `bg-surface` (`#111113`): Base card/panel background.
*   `bg-surface-elevated` (`#18181b`): Modals, dropdowns, floating panels.
*   `bg-surface-highlight` (`#1f1f23`): Hover states, active list items.

### Brand Colors
*   **Primary (Amber):** `text-primary` / `bg-primary` (`#f59e0b`). Used for: Gateway, actions, active states, energy flow.
*   **Secondary (Teal):** `text-secondary` / `bg-secondary` (`#0d9488`). Used for: Resources, static data, technical elements.
*   **Tertiary (Purple/Violet):** `text-tertiary` / `bg-tertiary` (`#8b5cf6`). Used for: Agents, AI elements, autonomous components, MCP servers.

### Status Indicators
*   **Running:** `bg-status-running` (`#10b981`) + Glow.
*   **Stopped:** `bg-status-stopped` (`#52525b`).
*   **Error:** `bg-status-error` (`#f43f5e`) + Blink animation.
*   **Pending:** `bg-status-pending` (`#eab308`) + Pulse.

### Transport Badges
*   **All transports (HTTP/SSE/Stdio):** Violet (`bg-violet-500/10 text-violet-400`)

### Text Hierarchy
*   `text-text-primary` (`#fafaf9`): Headings, main content.
*   `text-text-secondary` (`#a8a29e`): UI labels, descriptions.
*   `text-text-muted` (`#78716c`): Placeholder text, subtle metadata.

## 3. Typography
**Font Family:**
*   **Sans:** `font-sans` ("Outfit"). Usage: UI elements, headings, buttons.
*   **Mono:** `font-mono` ("IBM Plex Mono"). Usage: IDs, logs, code, port numbers, technical values.

**Scaling:**
*   Use `text-sm` for standard UI controls.
*   Use `text-xs` for metadata/labels.
*   Use `text-lg`/`text-xl` sparingly for section headers.

## 4. Component Patterns

### Glass Panels (The "Obsidian" Look)
Use the glass utility classes for containers. No flat solid backgrounds.
```tsx
<div className="glass-panel p-4">Content</div>           // Standard Panel
<div className="glass-panel-elevated p-2">Content</div>  // Elevated (Tooltips, Popovers)
```

### Buttons
*   **Primary:** Gradient Amber. `btn-primary` class.
*   **Secondary:** Surface color with border. `btn-secondary` class.
*   **Ghost:** Transparent, hover highlight. `btn-ghost` class.
*   **Icon Buttons:** Square, usually `p-2`, often `text-text-muted hover:text-primary`.

### Borders & Separation
*   Use `border-border` (`#27272a`) for structural edges.
*   Use `border-border-subtle` (`rgba(255,255,255,0.06)`) for internal dividers.
*   **Rule:** Prefer 1px borders over background color changes to define separation.

### Shadows & Glows
*   Use `shadow-node` for floating elements.
*   Use `shadow-glow-primary` for active/focused elements to create a "light emitting" effect.

## 5. Animation & Icons

**Transitions:** Always add `transition-all duration-200` to interactive elements. Avoid `translate-y` on graph nodes (React Flow clipping).

**Icons:** Lucide React (`lucide-react`), stroke 1.5-2px, size `w-4 h-4` or `w-5 h-5`.

## 6. Graph Layout System

### Butterfly Layout (Hub-and-Spoke)

| Zone | Position | Contents |
|------|----------|----------|
| 0 | Left | Local Agents (consumers) |
| 1 | Center | Gateway (hub) |
| 2 | Right | MCP Servers, Remote A2A Agents |
| 3 | Far Right | Resources |

**Edge Direction:** Left-to-right (Agent → Gateway → Server/Resource) representing request flow.

**Path Highlighting:** Clicking an Agent highlights its path through Gateway to used servers; other nodes dim to 0.25 opacity.

Implementation in `src/lib/graph/` (butterfly.ts, edges.ts, nodes.ts, transform.ts).

## 7. Graph Node Types

### Gateway Node
*   **Shape:** Rounded rectangle (`rounded-2xl`)
*   **Color:** Primary (Amber) accents
*   **Content:** Name, version, server/agent/resource counts, status

### MCP Server Node
*   **Shape:** Rounded rectangle (`rounded-xl`)
*   **Color:** Violet accents for all MCP server types (unified theme)
*   **Content:** Name, transport type, endpoint/container ID, tool count, status
*   **Format Badge:** Teal badge next to transport badge, shown only when `outputFormat` is non-default (not `json`). Displays format name (TOON, CSV, text).
*   **Scale Badge:** Inline mono badge in the header row. When `data.autoscale` is present the badge renders `×<current>/<target>` (e.g. `×2/3`); otherwise, when `replicaCount > 1`, it falls back to `×N`; otherwise nothing. Autoscale takes precedence over static replicas.
*   **Decision Ring:** Subtle inset ring driven by `data.autoscale.lastDecision`. `"up"` → amber `animate-pulse-glow`; `"down"` → `ring-secondary/40`; `"noop"` → no extra ring. The ring is inset (`ring-inset`) so it layers below the selection ring without fighting it.
*   **Token Heat Overlay:** Amber glow proportional to relative token usage when heat map is enabled via Flame button in canvas controls.
*   **Type Indicators:** Gray bordered badge next to status badge indicating server type:
    *   **Container:** Terminal icon + "Container" (for container-based servers)
    *   **External:** Globe icon + "External" (for external URL servers)
    *   **Local:** Cpu icon + "Local" (for local process servers)
    *   **SSH:** KeyRound icon + "SSH" (for SSH servers)
    *   **OpenAPI:** FileJson icon + "OpenAPI" (for OpenAPI-backed servers)

### Resource Node
*   **Shape:** Rounded rectangle (`rounded-xl`)
*   **Color:** Secondary (Teal) accents
*   **Content:** Name, image, network, status

### Agent Node (Unified)
*   **Shape:** Rounded square (`rounded-lg`, 144x144px)
*   **Color:** Variant-based styling ("Cartridge" design pattern)
    *   Local agents without A2A: Tertiary (Purple) base
    *   Local agents with A2A: Purple base + Teal accents
    *   Remote agents: Secondary (Teal) accents
*   **Icon:** Bot icon from Lucide
*   **Content:**
    *   Name and status indicator
    *   Variant badge (local/remote) in top-left corner
    *   A2A badge (when enabled) in top-right corner
    *   Skill count (when A2A enabled)
    *   Container ID hint (local agents only)
*   **Edge Style (Agent → Gateway):**
    *   Without A2A: Purple dashed line (`strokeDasharray: '5,5'`)
    *   With A2A: Teal dashed line (`strokeDasharray: '8,4'`, strokeWidth: 2)

## 8. Sidebar Sections

The detail sidebar displays contextual information when a node is selected.

### Token Usage Section (MCP Servers Only)
Shows per-server token counts, sparkline trend chart, and conditional format savings display.

*   **Location:** Between Status and Actions sections (MCP server nodes only)
*   **Icon:** Activity (Lucide)
*   **Components:**
    *   `TokenUsageSection` - Token counts (input/output/total), SparkChart with 5s auto-refresh, format savings bar with ARIA accessibility
    *   `SparkChart` - Minimal Recharts-based sparkline (no axes, legends, or tooltips)
*   **Visibility:** Only renders when token data exists for the selected server
*   **Format Savings:** Conditional bar showing original vs formatted tokens and savings percentage

### Scaling Section (Autoscaled MCP Servers Only)
Live view of reactive-autoscale state. Renders whenever the selected server's `MCPServerStatus.autoscale` is populated.

*   **Location:** Below Token Usage, above Actions (MCP server nodes only)
*   **Icon:** Activity (Lucide), `defaultOpen`
*   **Component:** `AutoscalePanel` (`web/src/components/status/AutoscalePanel.tsx`)
*   **Layout (top-down):**
    *   Headline: `Current <c> / Target <t> · Range <min>–<max>` (mono, violet emphasis)
    *   Dwell phrase keyed off `lastDecision` (e.g. `Scaling up · median in-flight 14, target 10`; `Stable · …`; `Idle · scaled to zero`)
    *   Inline-SVG sparkline - `current` solid violet, `target` dashed violet at 50%, min/max as faint horizontal guide lines at 15%. No chart library; path data memoized.
    *   Collapsible **Recent Decisions** feed (closed by default) - last ≤10 client-derived entries with `HH:MM:SS · kind chip · from→to`
*   **Derivation:** The decision feed is **not** streamed by the backend; it's reconstructed in the store by diffing `lastScaleUpAt` / `lastScaleDownAt` between polls. Two scale events within one 3s polling window can coalesce - accepted trade-off.

### Access Section (Agents Only)
Shows the MCP server dependencies for an agent with tool-level access visualization.

*   **Location:** Displayed for agent nodes with `uses` configured
*   **Icon:** Network (Lucide)
*   **Structure:**
    *   Each server dependency rendered as an `AccessItem` card
    *   Server header with violet theme (`bg-violet-500/10`)
    *   Access badge: "Full Access" (violet) or "Restricted" (amber)
    *   Tool list when restricted (individual tool names with Wrench icons)

```tsx
// AccessItem styling
<div className="rounded-lg bg-surface-elevated border border-border/40">
  {/* Server Header - Violet theme */}
  <div className="bg-violet-500/10">
    <Server className="text-violet-400" />
    <span className="text-violet-100">{serverName}</span>
    {/* Badge */}
    {isRestricted ? (
      <span className="border-amber-500/30 text-amber-400">Restricted</span>
    ) : (
      <span className="border-violet-500/30 text-violet-400">Full Access</span>
    )}
  </div>
  {/* Tool List */}
  {isRestricted && tools.map(tool => (
    <div className="bg-background/50">
      <Wrench className="text-primary" />
      <span className="font-mono">{tool}</span>
    </div>
  ))}
</div>
```

## 9. Layout Architecture

### Unified App Shell

The app is wrapped by a single `<AppShell>` (`src/components/shell/AppShell.tsx`) that owns the four-row chrome (Header / main / BottomPanel / StatusBar), the `ReactFlowProvider`, the command palette, the global runs SSE stream, and keyboard shortcut wiring. Every workspace renders inside its `<Outlet />`.

```tsx
<ReactFlowProvider>
  <div style={{
    display: 'grid',
    gridTemplateRows: `${HEADER_HEIGHT}px 1fr ${bottomRowHeight}px ${STATUSBAR_HEIGHT}px`,
  }}>
    <Header />              {/* Row 1: 56px */}
    <main><Outlet /></main> {/* Row 2: 1fr - active workspace renders here */}
    <BottomPanel />         {/* Row 3: 40px collapsed; 100–800px expanded */}
    <StatusBar />           {/* Row 4: 32px */}
  </div>
</ReactFlowProvider>
```

`AppShell` runs side-effect hooks once for the whole app: `useGlobalRunsStream` (live tail of every run via SSE), `useRunsCommands` (palette commands for runs), `useGlobalCommands`, `useKeyboardShortcuts`, `useSSEShutdown`. Workspaces never re-mount these; they just consume the resulting state.

### Workspaces

Three top-level workspaces share the shell, routed and code-split:

| Workspace | Route | Component | Purpose |
|---|---|---|---|
| Topology | `/topology` | `TopologyWorkspace` | Stack canvas (butterfly layout), node inspector |
| Skills | `/skills` | `SkillsWorkspace` | Agent IDE - typed-skill sidebar, list/canvas toggle, trace overlay, run launcher |
| Runs | `/runs` | `RunsWorkspace` (+ `/runs/:runID` → `RunDetailWorkspace`) | Live runs grid + inspector + waterfall |

Workspace metadata lives in `src/types/workspace.ts` (`WORKSPACE_CONFIG` - id, label, icon, shortcut key). Adding a workspace = appending one entry; the switcher, shortcuts, and labels follow automatically. `RootRedirect` picks the landing workspace from `localStorage` (`gridctl-last-workspace` global or `gridctl-last-workspace:<stackId>` per-stack); the legacy `/agent` path redirects to `/skills` preserving query/hash.

### WorkspaceShell (rails primitive)

`src/components/layout/WorkspaceShell.tsx` is the shared resizable-panel primitive (built on `react-resizable-panels` v4) that workspaces wrap their canvas in. Skills and Runs both use it; Topology uses a leaner CSS-grid layout because the right rail is the legacy `<Sidebar>` overlay.

```tsx
<WorkspaceShell
  workspace="skills"            // persistence key
  defaultLeftPct={18}
  defaultRightPct={24}
  minLeftPx={220}
  minRightPx={300}
  left={<SkillSidebar />}
  right={<NodeDetail />}
>
  <Canvas />
</WorkspaceShell>
```

Behaviors: per-workspace width persistence via `useWorkspaceLayout`, double-click separator to reset to defaults, `[` and `]` collapse/expand the left/right rail (suppressed inside text inputs). Rails are optional - omit `left` or `right` to render a single-rail layout.

### Resizable Panels

| Panel | Where | Min | Default | Max |
|-------|-------|-----|---------|-----|
| Topology sidebar | `<Sidebar />` overlay in TopologyWorkspace | 280px | 320px | 600px |
| WorkspaceShell rails | Skills (left + right), Runs (right) | `minLeftPx`/`minRightPx` props | percent of 1440px | n/a |
| Bottom panel | `<BottomPanel />` in AppShell | 100px | 250px | 800px |

**ResizeHandle Component (`src/components/ui/ResizeHandle.tsx`):** the legacy mouse-driven handle still used by Topology and BottomPanel; WorkspaceShell uses RRP's `<Separator>` for keyboard + pointer parity.

### Panel State Management
- `useUIStore` manages `sidebarOpen`, `bottomPanelOpen`, `activeWorkspace`, `commandPaletteOpen`, and the per-workspace `compactMode` map.
- Topology's `<Sidebar>` uses CSS transform (`translate-x-full`) for show/hide animation.
- BottomPanel height is controlled via the shell's grid row size, not the sidebar's width.

## 10. Defensive Coding Patterns

### Null Safety for Arrays
Store state and API responses may contain `null` instead of empty arrays. Always use nullish coalescing when accessing array methods:

```tsx
// BAD - will crash if array is null
const count = items.length;
const filtered = items.filter(x => x.active);
items.map(item => <Item key={item.id} />)

// GOOD - safe with null fallback
const count = (items ?? []).length;
const filtered = (items ?? []).filter(x => x.active);
(items ?? []).map(item => <Item key={item.id} />)
```

### Common Patterns

**Length checks:**
```tsx
{(items?.length ?? 0) > 0 && <List items={items} />}
```

**Conditional rendering with maps:**
```tsx
{items && (items ?? []).map(item => (
  <Item key={item.id} item={item} />
))}
```

**Store selectors:**
```tsx
const mcpServers = useStackStore((s) => s.mcpServers);
const count = (mcpServers ?? []).filter(s => s.initialized).length;
```

### Components Requiring Null Safety
These components access arrays that may be null during state transitions:

| Component | Arrays | Pattern |
|-----------|--------|---------|
| Header.tsx | `mcpServers` | `(mcpServers ?? []).filter()` |
| StatusBar.tsx | `mcpServers`, `resources` | `(arr ?? []).length` |
| Sidebar.tsx | `agentData.uses`, `selector.tools` | `(uses ?? []).map()` |
| BottomPanel.tsx | `logs` | `(logs ?? []).map()` |
| Canvas.tsx | `nodes`, `edges` | `(nodes ?? []).map()` |
| ToolList.tsx | `tools`, `serverTools` | `(tools ?? []).filter()` |

## 11. Detachable Windows (Pop-out)

The UI supports detaching panels into separate browser windows for multi-monitor workflows.

### Routes (all are pop-out routes rendered outside `<AppShell>` on purpose - frameless)
- `/logs` - Detached logs viewer with node selector
- `/metrics` - Detached metrics dashboard with KPI cards, chart, and per-server table
- `/sidebar` - Detached sidebar with node selector
- `/editor` - Detached registry editor (skill)
- `/registry` - Detached registry sidebar (skill list + actions)
- `/vault` - Detached vault panel (`DetachedVaultPage`)
- `/traces` - Detached traces viewer (`DetachedTracesPage`)

### Components
- **PopoutButton** (`src/components/ui/PopoutButton.tsx`): Pop-out icon button with hover glow effect
- **useWindowManager** (`src/hooks/useWindowManager.ts`): Manages detached windows lifecycle
- **useBroadcastChannel** (`src/hooks/useBroadcastChannel.ts`): Cross-window state sync

### State Management
- `logsDetached`, `sidebarDetached`, and `editorDetached` in `useUIStore` track detached state
- BroadcastChannel API enables real-time sync between windows
- Windows notify main app on open/close for UI state updates

### Window Configuration
| Window | Size | Features |
|--------|------|----------|
| Logs | 900x500 | Node selector, pause/resume, fullscreen |
| Sidebar | 420x700 | Node selector, collapsible sections |
| Editor | 720x750 | Skill editing, auto-close on save |

### Usage Pattern
```tsx
const { openDetachedWindow } = useWindowManager();

// Open logs for specific agent
openDetachedWindow('logs', `agent=${encodeURIComponent(agentName)}`);

// Open sidebar for specific node
openDetachedWindow('sidebar', `node=${encodeURIComponent(nodeName)}`);

// Open editor for existing skill
openDetachedWindow('editor', `type=skill&name=${encodeURIComponent(skillName)}`);

// Open editor for new skill
openDetachedWindow('editor', 'type=skill');
```

## 12. Bottom Panel (Tabbed Container)

The BottomPanel is a tabbed container with six tabs - **Logs**, **Metrics**, **Spec**, **Traces**, **Runs**, **Pins**. All panels are rendered simultaneously and toggled via CSS `invisible` so re-tabbing is instant. `setBottomPanelTab` always opens the panel as a side-effect. The Runs tab shows an in-flight badge fed by `useGlobalRunsStream`.

### Tab roster

| Tab | Component | Data source |
|---|---|---|
| Logs | `LogsTab` (shared with `DetachedLogsPage`) | `GET /api/logs` + `GET /api/agents/{name}/logs` |
| Metrics | `MetricsTab` | `GET /api/metrics/tokens?range=` (poll) |
| Spec | `SpecTab` | `GET /api/stack/spec`, `GET /api/stack/plan`, drift overlay |
| Traces | `TracesTab` | `GET /api/traces`, `GET /api/traces/{id}` |
| Runs | `RunsBottomTab` | `useGlobalRunsStream` → `GET /api/agent/runs/events/stream` |
| Pins | `PinsTab` | `GET /api/pins[/{server}]` + approve / reset actions |

### Metrics Tab

*   **Components:**
    *   `MetricsTab` - Session KPI cards, Recharts area chart, time range selector, and sortable per-server table
    *   Vendored Tremor Raw chart components adapted for Obsidian Observatory theme
    *   Recharts with vendor chunk code splitting
*   **Controls:** Time range selector (30m, 1h, 6h, 24h, 7d), pause/resume, clear, fullscreen
*   **Pop-out:** Full detached metrics window with KPI cards, area chart, per-server table, and all controls
*   **Data source:** `GET /api/metrics/tokens?range=` with polling

### Structured Log Viewer

The LogsTab and DetachedLogsPage components provide a structured log viewer with filtering capabilities.

### Log Entry Format

Logs are fetched from two sources:
- **Gateway logs**: `GET /api/logs` returns structured JSON entries
- **Container logs**: `GET /api/agents/{name}/logs` returns string arrays (parsed as JSON when possible)

```typescript
interface LogEntry {
  level: string;     // "DEBUG", "INFO", "WARN", "ERROR"
  ts: string;        // RFC3339Nano timestamp
  msg: string;       // Log message
  component?: string; // Component name (e.g., "gateway", "router")
  trace_id?: string;  // Trace ID for correlation
  attrs?: Record<string, unknown>; // Additional attributes
}
```

### Log Level Styling

| Level | Text Color | Badge Background | Border | Dot |
|-------|------------|------------------|--------|-----|
| ERROR | `text-status-error` | `bg-status-error/10` | `border-status-error/30` | `bg-status-error` |
| WARN | `text-status-pending` | `bg-status-pending/10` | `border-status-pending/30` | `bg-status-pending` |
| INFO | `text-primary` | `bg-primary/10` | `border-primary/30` | `bg-primary` |
| DEBUG | `text-text-muted` | `bg-surface-highlight` | `border-border/30` | `bg-text-muted` |

### Log Line Layout

Each log line uses a CSS grid layout with em-based column widths that scale with the `--log-font-size` custom property:

```tsx
// LogLine grid structure
<div className="grid gap-2 px-3 py-1 log-text grid-cols-[8.5em_5.5em_7.5em_1fr_2em]">
  <span>Timestamp</span>   {/* 8.5em - HH:MM:SS.mmm format */}
  <span>Level Badge</span> {/* 5.5em - Colored badge with dot */}
  <span>Component</span>   {/* 7.5em - Truncated component name */}
  <span>Message</span>     {/* 1fr  - Flexible message area, truncated */}
  <span>Expand Icon</span> {/* 2em  - ChevronRight indicator */}
</div>
```

### Features

| Feature | Description |
|---------|-------------|
| **Level Filter** | Dropdown with checkboxes to toggle ERROR, WARN, INFO, DEBUG visibility |
| **Search** | Text input filters by message, component, or trace_id |
| **Expandable Entries** | Click any log line to expand full wrapped message, attrs, and trace_id |
| **Auto-scroll** | Automatically scrolls to bottom; pauses when user scrolls up |
| **Gateway Badge** | "Structured" badge shown when viewing gateway logs |
| **Text Zoom** | +/- controls and Ctrl+Scroll to scale log text size (8-22px, persisted) |

### Components (Shared Module: `components/log/`)

All log components are extracted into `src/components/log/` and shared between BottomPanel and DetachedLogsPage:

- **LevelFilter**: Dropdown component with level toggle checkboxes
- **LogLine**: Individual log entry with expand/collapse; expanded view shows full message with `whitespace-pre-wrap break-words`, plus attrs and trace_id when present
- **ZoomControls**: Compact +/- buttons with font size display and reset
- **parseLogEntry()**: Parses string or LogEntry into normalized ParsedLog format. Handles JSON, Docker timestamp prefixes (`2026-...Z `), Go slog text format (`time=... level=... msg=...` with key=value attrs), and plain text with level keyword detection
- **formatTimestamp()**: Formats RFC3339 timestamps to HH:MM:SS.mmm
- **logTypes.ts**: Shared types (LogLevel, ParsedLog), level styling constants, and parsing regexes

### Text Zoom

Log text size is controlled via the `--log-font-size` CSS custom property set on the log container.

| Element | CSS Class | Behavior |
|---------|-----------|----------|
| Log text | `.log-text` | Uses `var(--log-font-size, 11px)` with line-height 1.5 |
| Detail text | `.log-text-detail` | Uses `calc(var(--log-font-size) - 1px)` with line-height 1.4 |

**Hook: `useLogFontSize(containerRef)`**
- Default: 11px, Range: 8-22px, Step: 1px
- Persisted to `localStorage` key `gridctl-log-font-size`
- Ctrl+Scroll zoom within the log container (non-passive wheel handler)
- Returns: `{ fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault }`

### Shared Implementation

Both `BottomPanel.tsx` and `DetachedLogsPage.tsx` import from `components/log/` to ensure consistent behavior in attached and detached modes.

## 13. Skills Workspace and Agent IDE

The Skills workspace (`/skills`) is the developer view inside the unified shell. It hosts the agent IDE - the live canvas that mirrors the AST of typed skills on disk - plus a CRUD interface for SKILL.md frontmatter, file editing, validation, and a per-skill **Run Launcher** that fires runs through `POST /api/agent/runs` and overlays the live trace on the canvas.

Code is canon: the IDE never writes back to source. The watcher (`useDevSocket`) subscribes to `/api/agent/dev/events` and re-renders the graph on save in <300ms (TS) or after `gridctl agent build` (Go).

### Workspace shape

```tsx
<WorkspaceShell workspace="skills" left={<SkillSidebar />} right={<NodeDetail />}>
  {viewMode === 'list' ? <NodeList /> : <Canvas />}
</WorkspaceShell>
```

URL-driven state for deep links: `?skill=foo&run=bar&view=canvas|list`.

### Components (`src/components/agent/ide/`)

- **SkillSidebar**: Typed-skill list with handler-language badges, state pills, and a Run button per skill. Backs the WorkspaceShell left rail.
- **NodeList** / **Canvas**: list and React Flow canvas modes for the parsed handler graph.
- **NodeDetail**: WorkspaceShell right-rail inspector - node attributes, source location with click-to-`$EDITOR` (`vscode://file/...:line`), live trace status.
- **RunLauncherModal**: Per-skill run launcher driven by the skill's input JSON Schema.
- **SchemaForm**: JSON-Schema-driven form used by the run launcher.
- **SkillRunButton**: Inline run-trigger anchor used in `SkillSidebar` and skill cards.
- **TracePill** / **useRunTrace**: Trace overlay state - fetches the live ledger for `?run=<id>` and decorates canvas nodes with the same status colors and latency the gateway records.
- **RunOutputView** / **RunsList** / **useRunsForSkill**: Per-skill run history feed with deep-link to `/runs/<id>`.
- **ResumeButton**: Time-travel resume affordance for suspended runs.

### Registry UI (still under `src/components/registry/`)

The legacy registry CRUD is now embedded inside the Skills workspace as a popout - `DetachedEditorPage` opens at `/editor?type=skill&name=...` for a multi-monitor edit experience.

- **RegistrySidebar**: Skills list with create/edit/delete/activate/disable actions.
- **SkillEditor**: Split-pane markdown editor with frontmatter helpers, live preview, and validation.
- **SkillFileTree**: File browser for `scripts/`, `references/`, `assets/` within a skill directory.
- **SkillCard** / **SkillCardSkeleton** / **SkillActions** / **StateBadge**: presentational primitives shared by the skills surfaces.

### Editor Features

| Feature | Description |
|---------|-------------|
| Split pane | Markdown editor (left) + live preview (right) |
| Frontmatter helpers | Form fields synced with YAML frontmatter |
| File tree | Browse and manage supporting files |
| Validation | Real-time spec validation with errors/warnings |
| State toggle | Draft/Active/Disabled state management |
| Popout | Detachable to separate window via `/editor` |

### Stores

- `useRegistryStore` - skills + loading states from `/api/registry/skills`.
- `useUIStore` - `compactMode.skills` toggles a denser rail/canvas layout (Cmd+Shift+. while focused on the workspace).

### Graph Integration

- **RegistryNode**: shows active/total skill count on the topology canvas.
- Appears connected to the gateway node when the registry has content (progressive disclosure).

## 14. Runs Workspace and Live Stream

The Runs workspace (`/runs`) renders the JSONL ledger at `~/.gridctl/runs/<run_id>.jsonl` as a live grid + inspector. `/runs/:runID` opens a detail view with the full waterfall.

### Components (`src/components/runs/` and `src/components/workspaces/`)

- **RunsWorkspace** / **RunDetailWorkspace**: workspace bodies under `<AppShell>`.
- **RunsFilterBar**: status / skill / since / parent filters; the URL is the single source of truth.
- **RunsGrid**: virtualised list of run summaries with status pills and skill chips.
- **RunInspector**: right-rail event timeline plus per-node attributes; opens the detail route on demand.
- **RunWaterfall**: span-style timeline rendered in `RunDetailWorkspace`.
- **StatusPill** / `status.ts` / `format.ts` / `tree.ts`: shared status, time, and parent/child layout helpers.
- **RunsBottomTab**: live span waterfall surfaced in the AppShell BottomPanel under the **Runs** tab - driven by the same stream as the workspace.

### Global SSE bus

`useGlobalRunsStream` (mounted **once** in `<AppShell>`) opens an `EventSource` against `GET /api/agent/runs/events/stream` so every workspace receives live events without subscribing per-run. The BottomPanel's in-flight badge, the Runs grid, and the Skills workspace's trace overlay all consume the same stream via `useRunsStore`. Per-run SSE (`/api/agent/runs/{run_id}/events`) is still the source of truth for one run's full timeline.

### Pause toggle

The shell BottomPanel exposes a pause control for the runs SSE stream so an operator inspecting a frozen state can stop the grid from scrolling out from under them. Resume re-attaches and back-fills from the ledger; no events are dropped.

## 15. Authentication

Gateway authentication support in the web UI.

### Components

- **AuthPrompt** (`src/components/auth/AuthPrompt.tsx`): Modal that prompts for a bearer token when the gateway returns 401

### Store: `useAuthStore`

Manages `authRequired` (boolean) and `token` (string) state. The token is included as a Bearer header in all API requests when set.

### Integration

- `usePolling` detects 401 responses and sets `authRequired = true`
- `AuthPrompt` renders over the main UI until a valid token is provided
- Token persists in the store for the session duration

## 16. Keyboard Shortcuts

Hook: `useKeyboardShortcuts` (`src/hooks/useKeyboardShortcuts.ts`). Wired once by `<AppShell>`.

### Global

| Shortcut | Action |
|----------|--------|
| `Cmd/Ctrl+1` | Switch to Topology workspace |
| `Cmd/Ctrl+2` | Switch to Skills workspace |
| `Cmd/Ctrl+3` | Switch to Runs workspace |
| `Cmd/Ctrl+K` | Open command palette |
| `Cmd/Ctrl+R` | Refresh polled state |
| `Cmd/Ctrl+B` | Toggle BottomPanel |
| `Cmd/Ctrl+,` | Switch BottomPanel to Traces tab |
| `Cmd/Ctrl+Shift+.` | Toggle Compact Mode for the active workspace |
| `Cmd/Ctrl+F` | Fit canvas |
| `Cmd/Ctrl+=` / `Cmd/Ctrl+-` | Zoom canvas in/out |
| `Escape` | Clear node selection, close inspector |

### WorkspaceShell rails

| Shortcut | Action |
|----------|--------|
| `[` | Collapse/expand the left rail (skipped inside text inputs) |
| `]` | Collapse/expand the right rail (skipped inside text inputs) |

### Command Palette

`CommandPalette` (`src/components/palette/CommandPalette.tsx`) hosts a Cmd+K palette wired through `useGlobalCommands` (shell-level) and `useRunsCommands` / `useSkillsCommands` (workspace-level). Workspaces register commands as they mount; the palette deduplicates by id so a stale registration from a previous mount does not pollute the list.

## 17. Compact Mode

Each workspace can toggle a denser layout independently. State lives in `useUIStore.compactMode` keyed by `Workspace`.

| Workspace | Compact effect |
|---|---|
| Topology | Compact card style on graph nodes (`compactCards` flag) |
| Skills | Narrower default rail percentages and tighter node-list rows |
| Runs | Tighter filter bar + grid spacing |

Toggle via `Cmd/Ctrl+Shift+.` while the workspace is active, or via the command palette.

## 18. Vault Panel

### Components (`src/components/vault/`)

- **VaultPanel**: Right sidebar (w-[380px]) for managing secrets and variable sets. Features:
  - Quick-add form with key validation, password input, set selector
  - Secret list grouped by unassigned + variable sets (collapsible)
  - Per-secret actions: reveal (10s auto-hide timer), inline edit, delete with confirmation
  - Lock/unlock controls with passphrase input (confirmation match on lock)
  - Encrypted badge in header when vault is locked
  - Uses `useVaultStore` for state (`secrets`, `sets`, `locked`, `encrypted`)

- **VaultLockPrompt**: Centered modal overlay shown when vault is locked. Features:
  - Lock icon with primary color glow
  - Passphrase input with show/hide toggle
  - Auto-focus on mount, Enter key submits
  - Error message display for wrong passphrase
  - Fade-in scale animation on glass panel background

### Store (`src/stores/useVaultStore.ts`)

Zustand store with fields: `secrets`, `sets`, `loading`, `error`, `locked`, `encrypted`.
- `setLocked(true)` clears `secrets` and `sets` to null
- Null-safe coercion: `setSets(null)` and `setSecrets(null)` coerce to `[]`

### API Functions (`src/lib/api.ts`)

Vault-specific: `fetchVaultSecrets`, `createVaultSecret`, `getVaultSecret`, `updateVaultSecret`, `deleteVaultSecret`, `fetchVaultSets`, `createVaultSet`, `deleteVaultSet`, `assignSecretToSet`, `fetchVaultStatus`, `unlockVault`, `lockVault`.

### Styling Notes

- Glass panel sidebar with backdrop blur
- Amber primary for lock/unlock actions, Teal secondary for set icons
- Monospace font (`font-mono`) for secret keys and values
- Error states use `status-error` color token

## 19. Creation Wizard

The creation wizard is a multi-step modal for adding MCP servers, resources, stacks, and skills to the running configuration.

### Components (`src/components/wizard/`)

- **CreationWizard**: Root wizard modal - routes to the appropriate step flow based on resource type
- **RecipePicker**: First step - resource type selection cards (Stack, MCP Server, Resource, Agent, Skill)
- **BrowseStep**: Registry browser for importing skills
- **AddSourceStep**: Transport/source configuration for MCP servers
- **MCPServerForm**: MCP server detail form. Advanced section includes a segmented **Scaling** control (Static replicas | Autoscale). The two branches share no state - toggling clears the opposite field group so the emitted YAML carries at most one of `replicas:` or `autoscale:`. Hidden on `external` and `openapi` server types (backend rejects both).
- **ResourceForm**: Resource (non-MCP container) form
- **StackForm**: Stack spec builder
- **ReviewStep**: Final review and deploy step (all resource types); for stacks, shows **Save & Load** instead of Deploy
- **SkillImportWizard**: Dedicated wizard flow for importing skills from git
- **DraftManager**: Draft persistence for wizard in-progress state
- **ExpertModeToggle**: Toggle between guided and expert (raw YAML) modes
- **SecretsPopover**: Inline vault secret picker for form fields
- **TransportAdvisor**: Guided transport selector with recommendation logic
- **TemplateGrid**: Template browser component
- **YAMLPreview**: Live YAML preview panel

### Stackless Mode Gating

When no stack is active (stackless mode), the wizard gates stack-dependent resource types:

| Resource Type | Stackless Behavior |
|---------------|-------------------|
| **Stack** | Always enabled - the mechanism for loading a stack |
| **MCP Server** | `opacity-40 cursor-not-allowed`; clicking is a no-op with tooltip "Requires an active stack" |
| **Resource** | `opacity-40 cursor-not-allowed`; clicking is a no-op with tooltip "Requires an active stack" |
| **Agent** | Always enabled |
| **Skill** | Always enabled |

### Stack Save & Load Flow

When `resourceType === 'stack'`, the ReviewStep shows a **Save & Load** button instead of **Deploy**:

1. Calls `POST /api/stacks` to persist the stack spec to `~/.gridctl/stacks/{name}.yaml`
2. Calls `POST /api/stack/initialize` to cold-load it into the running daemon
3. If a stack is already active (409 response), shows a "Stack saved to library" toast instead

### Header Stack Indicator

When a stack is active and the daemon is connected, the Header shows an active stack name pill:
- **Icon:** `Layers` (Lucide)
- **Styling:** `bg-primary/10 border border-primary/20 text-primary` pill with stack name
- **Hidden:** When no stack is loaded (stackless mode)

### Canvas Stackless Empty State

The Canvas empty state conditionally renders quick-add links based on stack status:
- **Always visible:** "Create your first stack" CTA (navigates to stack wizard)
- **Hidden until stack active:** Quick-add "Add MCP Server" and "Add Resource" links

## 20. Checklist for New Components

1. Use Tailwind color tokens (no hardcoded hex values)
2. Use `font-mono` for technical data, `font-sans` for UI
3. Use glass panels for floating containers
4. Add hover states with glow/border changes
5. **Use `?? []` fallback for all array `.length`, `.map()`, `.filter()` calls**
6. **Use optional chaining (`?.`) when accessing nested properties**
7. For resizable containers, use `min-h-0` to allow flex shrinking
