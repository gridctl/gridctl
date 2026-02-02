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
*   **Type Indicators:** Gray bordered badge next to status badge indicating server type:
    *   **Container:** Terminal icon + "Container" (for container-based servers)
    *   **External:** Globe icon + "External" (for external URL servers)
    *   **Local:** Cpu icon + "Local" (for local process servers)
    *   **SSH:** KeyRound icon + "SSH" (for SSH servers)

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

### CSS Grid Main Layout
The app uses CSS Grid for the main layout with four rows:

```tsx
// App.tsx grid structure
<div style={{
  display: 'grid',
  gridTemplateRows: `${HEADER_HEIGHT}px 1fr ${bottomRowHeight}px ${STATUSBAR_HEIGHT}px`,
  gridTemplateColumns: '1fr',
}}>
  <Header />           {/* Row 1: Fixed 56px */}
  <main>               {/* Row 2: Flexible (1fr) */}
    <Canvas />         {/* Fills main area */}
    <Sidebar />        {/* Absolute overlay, right side */}
  </main>
  <BottomPanel />      {/* Row 3: 40px collapsed, 100-800px expanded */}
  <StatusBar />        {/* Row 4: Fixed 32px */}
</div>
```

### Resizable Panels
Both sidebar and bottom panel support drag-to-resize:

| Panel | Direction | Min | Default | Max |
|-------|-----------|-----|---------|-----|
| Sidebar | Horizontal (width) | 280px | 320px | 600px |
| Bottom Panel | Vertical (height) | 100px | 250px | 800px |

**ResizeHandle Component:** Thin handle with amber glow on hover/drag, positioned at panel edge.

### Panel State Management
- `useUIStore` manages `sidebarOpen` and `bottomPanelOpen` states
- Sidebar uses CSS transform (`translate-x-full`) for show/hide animation
- Bottom panel height controlled via grid row size

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

### Routes
- `/logs` - Detached logs viewer with node selector
- `/sidebar` - Detached sidebar with node selector

### Components
- **PopoutButton** (`src/components/ui/PopoutButton.tsx`): Pop-out icon button with hover glow effect
- **useWindowManager** (`src/hooks/useWindowManager.ts`): Manages detached windows lifecycle
- **useBroadcastChannel** (`src/hooks/useBroadcastChannel.ts`): Cross-window state sync

### State Management
- `logsDetached` and `sidebarDetached` in `useUIStore` track detached state
- BroadcastChannel API enables real-time sync between windows
- Windows notify main app on open/close for UI state updates

### Window Configuration
| Window | Size | Features |
|--------|------|----------|
| Logs | 900x500 | Node selector, pause/resume, fullscreen |
| Sidebar | 420x700 | Node selector, collapsible sections |

### Usage Pattern
```tsx
const { openDetachedWindow } = useWindowManager();

// Open logs for specific agent
openDetachedWindow('logs', `agent=${encodeURIComponent(agentName)}`);

// Open sidebar for specific node
openDetachedWindow('sidebar', `node=${encodeURIComponent(nodeName)}`);
```

## 12. Checklist for New Components

1. Use Tailwind color tokens (no hardcoded hex values)
2. Use `font-mono` for technical data, `font-sans` for UI
3. Use glass panels for floating containers
4. Add hover states with glow/border changes
5. **Use `?? []` fallback for all array `.length`, `.map()`, `.filter()` calls**
6. **Use optional chaining (`?.`) when accessing nested properties**
7. For resizable containers, use `min-h-0` to allow flex shrinking
