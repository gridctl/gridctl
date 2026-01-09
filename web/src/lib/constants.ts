// Layout parameters for node positioning
// Uses Left-to-Right (LR) dagre layout - industry standard for flow diagrams
export const LAYOUT = {
  // Node dimensions
  GATEWAY_WIDTH: 240,
  GATEWAY_HEIGHT: 160,
  NODE_WIDTH: 256,
  NODE_HEIGHT: 140,
  AGENT_SIZE: 144,     // Unified agent node size (rounded rectangle)

  // Dagre layout spacing
  NODE_SPACING: 60,    // Vertical spacing between nodes in same rank
  RANK_SPACING: 120,   // Horizontal spacing between tiers (gateway -> servers -> etc)
  MARGIN_X: 40,        // Left/right margin
  MARGIN_Y: 40,        // Top/bottom margin

  // Legacy radial layout (kept for reference/fallback)
  CENTER_X: 150,
  CENTER_Y: 300,
  MCP_RADIUS: 280,
  RESOURCE_RADIUS: 420,
  AGENT_RADIUS: 350,
} as const;

// ============================================
// OBSIDIAN OBSERVATORY - Color Palette
// ============================================
export const COLORS = {
  // Core Obsidian Palette
  background: '#08080a',
  surface: '#111113',
  surfaceElevated: '#18181b',
  surfaceHighlight: '#1f1f23',
  border: '#27272a',
  borderSubtle: 'rgba(255, 255, 255, 0.06)',

  // Primary - Warm Amber (Energy, Activity)
  primary: '#f59e0b',
  primaryLight: '#fbbf24',
  primaryDark: '#d97706',
  primaryGlow: 'rgba(245, 158, 11, 0.15)',

  // Secondary - Deep Teal (Technical, Data)
  secondary: '#0d9488',
  secondaryLight: '#14b8a6',
  secondaryDark: '#0f766e',
  secondaryGlow: 'rgba(13, 148, 136, 0.15)',

  // Tertiary - Purple/Violet (Agents, AI)
  tertiary: '#8b5cf6',
  tertiaryLight: '#a78bfa',
  tertiaryDark: '#7c3aed',
  tertiaryGlow: 'rgba(139, 92, 246, 0.15)',

  // Status colors
  statusRunning: '#10b981',
  statusStopped: '#52525b',
  statusError: '#f43f5e',
  statusPending: '#eab308',

  // Transport indicators (matching primary/secondary)
  transportHttp: '#0d9488',  // Teal for network
  transportStdio: '#f59e0b', // Amber for local

  // Text hierarchy - Warm whites
  textPrimary: '#fafaf9',
  textSecondary: '#a8a29e',
  textMuted: '#78716c',

  // Edge colors
  edgeDefault: '#27272a',
  edgeAnimated: '#f59e0b',
} as const;

// Node type identifiers
export const NODE_TYPES = {
  GATEWAY: 'gateway',
  MCP_SERVER: 'mcpServer',
  RESOURCE: 'resource',
  AGENT: 'agent',
} as const;

// Edge type identifiers
export const EDGE_TYPES = {
  DATA_FLOW: 'smoothstep',
} as const;

// Animation durations (ms)
export const ANIMATION = {
  NODE_TRANSITION: 250,
  EDGE_FLOW: 1200,
  STATUS_PULSE: 2000,
  STAGGER_DELAY: 50,
} as const;

// Polling intervals (ms)
export const POLLING = {
  STATUS: 3000,      // Poll status every 3 seconds
  TOOLS: 30000,      // Poll tools every 30 seconds
  LOGS: 2000,        // Poll logs every 2 seconds
} as const;

// Tool naming
// Delimiter between agent name and tool name in prefixed tool names.
// Format: "agentname::toolname"
// This matches the Go backend constant in pkg/mcp/router.go
export const TOOL_NAME_DELIMITER = '::';
