// Layout parameters for node positioning
export const LAYOUT = {
  CENTER_X: 150,
  CENTER_Y: 300,
  MCP_RADIUS: 280,     // Distance from gateway to MCP servers
  RESOURCE_RADIUS: 420, // Distance from gateway to resources
  NODE_WIDTH: 256,
  NODE_HEIGHT: 140,
} as const;

// Color palette matching Tailwind config - Quantum Neon
export const COLORS = {
  // Background
  background: '#0D0D0D',
  surface: '#1a1a1a',
  surfaceHighlight: '#2a2a2a',
  border: '#3a3a3a',

  // Primary/Secondary (Neon)
  primary: '#00CAFF',
  primaryLight: '#66DFFF',
  secondary: '#B915CC',
  secondaryLight: '#D45DE8',

  // Status colors (Neon)
  statusRunning: '#2CFF05',
  statusStopped: '#4a4a4a',
  statusError: '#FF3366',
  statusPending: '#FFCC00',

  // Transport indicators
  transportHttp: '#B915CC',
  transportStdio: '#00CAFF',

  // Text
  textPrimary: '#f8fafc',
  textSecondary: '#a0a0a0',
  textMuted: '#707070',

  // Edges
  edgeDefault: '#64748B',  // Cool Gray
  edgeAnimated: '#00CAFF',
} as const;

// Node type identifiers
export const NODE_TYPES = {
  GATEWAY: 'gateway',
  MCP_SERVER: 'mcpServer',
  RESOURCE: 'resource',
} as const;

// Edge type identifiers
export const EDGE_TYPES = {
  DATA_FLOW: 'smoothstep',
} as const;

// Animation durations (ms)
export const ANIMATION = {
  NODE_TRANSITION: 200,
  EDGE_FLOW: 1500,
  STATUS_PULSE: 2000,
} as const;

// Polling intervals (ms)
export const POLLING = {
  STATUS: 3000,      // Poll status every 3 seconds
  TOOLS: 30000,      // Poll tools every 30 seconds
  LOGS: 2000,        // Poll logs every 2 seconds
} as const;
