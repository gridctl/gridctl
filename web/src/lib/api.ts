import type { GatewayStatus, MCPServerStatus, ToolsListResult } from '../types';

// Base URL for API calls - empty for same origin
const API_BASE = '';

// Generic fetch wrapper with error handling
async function fetchJSON<T>(endpoint: string): Promise<T> {
  const response = await fetch(`${API_BASE}${endpoint}`);

  if (!response.ok) {
    throw new Error(`API error: ${response.status} ${response.statusText}`);
  }

  return response.json();
}

// === API Functions ===

/**
 * Fetch gateway status including all MCP server statuses
 * GET /api/status
 */
export async function fetchStatus(): Promise<GatewayStatus> {
  return fetchJSON<GatewayStatus>('/api/status');
}

/**
 * Fetch list of registered MCP servers
 * GET /api/mcp-servers
 */
export async function fetchMCPServers(): Promise<MCPServerStatus[]> {
  return fetchJSON<MCPServerStatus[]>('/api/mcp-servers');
}

/**
 * Fetch all aggregated tools from all MCP servers
 * GET /api/tools
 */
export async function fetchTools(): Promise<ToolsListResult> {
  return fetchJSON<ToolsListResult>('/api/tools');
}

// === Agent Control Functions (require backend endpoints) ===

/**
 * Fetch logs for a specific agent
 * GET /api/agents/{name}/logs
 */
export async function fetchAgentLogs(name: string, lines = 100): Promise<string[]> {
  const response = await fetch(`${API_BASE}/api/agents/${encodeURIComponent(name)}/logs?lines=${lines}`);

  if (!response.ok) {
    // Try to parse JSON error message from backend
    try {
      const errorData = await response.json();
      if (errorData.error) {
        throw new Error(errorData.error);
      }
    } catch {
      // If JSON parsing fails, use generic message
    }
    throw new Error(`Logs fetch failed: ${response.status} ${response.statusText}`);
  }

  return response.json();
}

/**
 * Restart an agent's container
 * POST /api/agents/{name}/restart
 */
export async function restartAgent(name: string): Promise<void> {
  const response = await fetch(`${API_BASE}/api/agents/${encodeURIComponent(name)}/restart`, {
    method: 'POST',
  });

  if (!response.ok) {
    throw new Error(`Restart failed: ${response.status} ${response.statusText}`);
  }
}

/**
 * Stop an agent's container
 * POST /api/agents/{name}/stop
 */
export async function stopAgent(name: string): Promise<void> {
  const response = await fetch(`${API_BASE}/api/agents/${encodeURIComponent(name)}/stop`, {
    method: 'POST',
  });

  if (!response.ok) {
    throw new Error(`Stop failed: ${response.status} ${response.statusText}`);
  }
}

// === Structured Log Entry (from gateway) ===

export interface LogEntry {
  level: string;     // "DEBUG", "INFO", "WARN", "ERROR"
  ts: string;        // RFC3339Nano timestamp
  msg: string;       // Log message
  component?: string; // Component name (e.g., "gateway", "router")
  trace_id?: string;  // Trace ID for correlation
  attrs?: Record<string, unknown>; // Additional attributes
}

/**
 * Fetch structured gateway logs
 * GET /api/logs
 */
export async function fetchGatewayLogs(lines = 100, level?: string): Promise<LogEntry[]> {
  let url = `${API_BASE}/api/logs?lines=${lines}`;
  if (level) {
    url += `&level=${encodeURIComponent(level)}`;
  }
  const response = await fetch(url);

  if (!response.ok) {
    throw new Error(`Logs fetch failed: ${response.status} ${response.statusText}`);
  }

  return response.json();
}

// === Reload API ===

export interface ReloadResult {
  success: boolean;
  message: string;
  added?: string[];
  removed?: string[];
  modified?: string[];
  errors?: string[];
}

/**
 * Trigger a configuration reload
 * POST /api/reload
 */
export async function triggerReload(): Promise<ReloadResult> {
  const response = await fetch(`${API_BASE}/api/reload`, { method: 'POST' });
  const data = await response.json();

  if (!response.ok) {
    throw new Error(data.error || `Reload failed: ${response.status}`);
  }

  return data;
}

// === JSON-RPC Helper (for MCP protocol calls) ===

interface JSONRPCRequest {
  jsonrpc: '2.0';
  id: number | string;
  method: string;
  params?: unknown;
}

interface JSONRPCResponse<T = unknown> {
  jsonrpc: '2.0';
  id: number | string;
  result?: T;
  error?: {
    code: number;
    message: string;
    data?: unknown;
  };
}

let requestId = 0;

export async function mcpRequest<T>(
  method: string,
  params?: unknown
): Promise<T> {
  const request: JSONRPCRequest = {
    jsonrpc: '2.0',
    id: ++requestId,
    method,
    params,
  };

  const response = await fetch(`${API_BASE}/mcp`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(request),
  });

  const result = await response.json() as JSONRPCResponse<T>;

  if (result.error) {
    throw new Error(`MCP error ${result.error.code}: ${result.error.message}`);
  }

  return result.result as T;
}
