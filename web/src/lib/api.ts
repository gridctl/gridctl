import type { GatewayStatus, MCPServerStatus, ToolsListResult } from '../types';

// Base URL for API calls - empty for same origin
const API_BASE = '';

// === Auth Token Management ===

const AUTH_STORAGE_KEY = 'gridctl-auth-token';

export class AuthError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'AuthError';
  }
}

export function getStoredToken(): string | null {
  try {
    return localStorage.getItem(AUTH_STORAGE_KEY);
  } catch {
    return null;
  }
}

export function storeToken(token: string): void {
  try {
    localStorage.setItem(AUTH_STORAGE_KEY, token);
  } catch {
    // localStorage may be unavailable
  }
}

export function clearToken(): void {
  try {
    localStorage.removeItem(AUTH_STORAGE_KEY);
  } catch {
    // localStorage may be unavailable
  }
}

function buildHeaders(extra?: Record<string, string>): Record<string, string> {
  const headers: Record<string, string> = { ...extra };
  const token = getStoredToken();
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }
  return headers;
}

// === Generic Fetch Wrapper ===

async function fetchJSON<T>(endpoint: string): Promise<T> {
  const response = await fetch(`${API_BASE}${endpoint}`, {
    headers: buildHeaders(),
  });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

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
  const response = await fetch(
    `${API_BASE}/api/agents/${encodeURIComponent(name)}/logs?lines=${lines}`,
    { headers: buildHeaders() },
  );

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

  if (!response.ok) {
    let errorMessage = `Logs fetch failed: ${response.status} ${response.statusText}`;
    try {
      const errorData = await response.json();
      if (errorData.error) {
        errorMessage = errorData.error;
      }
    } catch {
      // JSON parsing failed, use default message
    }
    throw new Error(errorMessage);
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
    headers: buildHeaders(),
  });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

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
    headers: buildHeaders(),
  });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

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
  const response = await fetch(url, { headers: buildHeaders() });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

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
  const response = await fetch(`${API_BASE}/api/reload`, {
    method: 'POST',
    headers: buildHeaders(),
  });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

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
    headers: buildHeaders({ 'Content-Type': 'application/json' }),
    body: JSON.stringify(request),
  });

  if (response.status === 401) {
    throw new AuthError('Authentication required');
  }

  const result = await response.json() as JSONRPCResponse<T>;

  if (result.error) {
    throw new Error(`MCP error ${result.error.code}: ${result.error.message}`);
  }

  return result.result as T;
}
