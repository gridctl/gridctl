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
Do not use flat solid backgrounds for containers. Use the glass utility classes.
```tsx
// Standard Panel
<div className="glass-panel p-4">Content</div>

// Elevated (e.g., Tooltips, Popovers)
<div className="glass-panel-elevated p-2">Content</div>
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

## 5. Animation & Interaction
*   **Transitions:** Always add `transition-all duration-200` (or similar) to interactive elements.
*   **Micro-interactions:** Use shadow/glow changes and border highlights on hover. Avoid `translate-y` on graph nodes (causes clipping in React Flow).
*   **Loading:** Use `animate-pulse-glow` for loading skeletons, not standard grey pulses.

## 6. Iconography
*   **Library:** Lucide React (`lucide-react`).
*   **Style:** Stroke width `1.5px` or `2px`.
*   **Size:** Standard size is `w-4 h-4` (16px) or `w-5 h-5` (20px).

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
*   **Edge Style:**
    *   Without A2A: Purple dashed line (`strokeDasharray: '5,5'`)
    *   With A2A: Teal dashed line (`strokeDasharray: '8,4'`, strokeWidth: 2)

## 8. Implementation Checklist
When creating new UI components:
1.  [ ] Are you using `tailwind.config.js` colors instead of hardcoded hex values?
2.  [ ] Is the font family correct? (Mono for data, Sans for UI).
3.  [ ] Does it have a "glass" feel if it's a floating panel?
4.  [ ] Are borders subtle (`border-border`)?
5.  [ ] Do interactive elements glow or change color on hover?
