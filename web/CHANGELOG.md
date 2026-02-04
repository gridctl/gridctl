# Changelog

All notable changes to gridctl will be documented in this file.

## [Unreleased]


### Bug Fixes


- Remove duplicate v prefix from gateway node version display
- Prevent selection glow bleedthrough on agent badges
- Add null safety for nodes and edges arrays
- Add null safety for mcpServers array
- Add null safety for mcpServers and resources arrays
- Add null safety for logs array
- Add null safety for tools and whitelist arrays
- Add null safety for graph node creation
- Scale log grid columns with font size and add text wrapping

### Features


- Add brand logo asset
- Replace header icon with brand logo
- Add ToolSelector type to frontend
- Add whitelist filtering to ToolList component
- Add Access section to agent sidebar
- Add Butterfly layout engine for hub-and-spoke visualization
- Add path highlighting hook for agent selection
- Integrate path highlighting into Canvas component
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
- Add fetchGatewayLogs API function
- Add structured log viewer with filtering
- Add detached logs and sidebar pages
- Add shared log types and parsing utilities
- Add shared LogLine component
- Add shared LevelFilter component
- Add useLogFontSize hook with persistence
- Add ZoomControls component
- Add barrel export for log components
- Parse Docker timestamps and slog text format in log viewer

### Refactoring


- Update graph transform for ToolSelector
- Rename useTopologyStore to useStackStore
- Update App.tsx for useStackStore
- Update Canvas for useStackStore
- Update Header for useStackStore
- Update Sidebar for useStackStore
- Update StatusBar for useStackStore
- Update BottomPanel for useStackStore
- Update ToolList for useStackStore
- Update usePolling for useStackStore
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
- Simplify UI store for panel state management
- Simplify Sidebar to fill parent container
- Simplify BottomPanel to fill grid cell
- Use shared log components and add zoom controls
- Use shared log components and add zoom controls## [0.1.0-alpha.2] - 2026-01-23


### Bug Fixes


- Correct handle positions and remove translate-y hover
- Remove translate-y hover to prevent clipping
- Remove translate-y hover to prevent clipping
- Add overflow visible to prevent React Flow clipping
- Position agents on right side of gateway
- Use Record<string, unknown> for tool inputSchema
- Handle generic inputSchema in ToolList component
- Correct tool name delimiter to match backend

### Features


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
- Add AgentStatus and AgentNodeData types
- Add tertiary color palette for agent nodes
- Add agent nodes and edges to graph transform
- Add agents state to topology store
- Add circular AgentNode component
- Register AgentNode in React Flow node types
- Add agent count to gateway node display
- Add agent color to minimap node display
- Add agent-specific details to sidebar
- Add A2A agent types to web frontend
- Add A2A layout constants
- Add A2A agent node and edge transformation
- Add A2A agent state to topology store
- Add A2AAgentNode component with teal theme
- Register A2AAgentNode in node types
- Add A2A agent edge coloring
- Add A2A agent count to gateway node
- Add A2A agent details to sidebar
- Add dagre layout with LR hierarchy
- Unified agent node with variant styling
- Add tool name delimiter constant to frontend
- Add SSE transport and external field to frontend types
- Pass external field from API to node data
- Add transport icon and color utility functions
- Add violet styling and External badge for external servers
- Add external server styling to sidebar details
- Add localProcess field to frontend types
- Include localProcess in MCP server node data
- Add local process indicator to MCP server nodes
- Add SSH fields to MCP server status types
- Add workload type to container status response

### Refactoring


- Simplify CustomNode with clean design patterns
- Simplify GatewayNode with clean design patterns
- Integrate bottom panel and remove log viewer overlay
- Unify AgentStatus and AgentNodeData types
- Remove A2A_AGENT node type constant
- Unify agent nodes with arrowhead edges
- Remove separate a2aAgents state
- Remove A2AAgentNode from registry
- Delete deprecated A2AAgentNode component
- Update minimap colors for unified agents
- Unified agent details in sidebar
- Update parsePrefixedToolName for :: delimiter
- Remove unused LOCAL_PROCESS_STYLES constant
- Update web UI branding to Gridctl
