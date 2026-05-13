/**
 * Agent IDE API client. Wraps /api/agent/dev/* (skills list, AST
 * graphs, file-watcher SSE) and /api/agent/runs/* (trace events,
 * resume). Both ride the same auth token convention as the rest of
 * the gridctl frontend.
 */

const API_BASE = '';
const AUTH_STORAGE_KEY = 'gridctl-auth-token';

function buildHeaders(extra?: Record<string, string>): Record<string, string> {
  const headers: Record<string, string> = { ...extra };
  try {
    const token = localStorage.getItem(AUTH_STORAGE_KEY);
    if (token) headers['Authorization'] = `Bearer ${token}`;
  } catch {
    // localStorage unavailable
  }
  return headers;
}

// === Skills (parser-driven) ===

export type NodeKind = 'tool' | 'llm' | 'parallel' | 'handoff' | 'approval';
export type SkillLang = 'go' | 'ts' | '';

export interface SkillSummary {
  name: string;
  lang: SkillLang;
  dir: string;
  node_count: number;
  has_error?: boolean;
}

export interface AgentNode {
  id: string;
  kind: NodeKind;
  label: string;
  file: string;
  line: number;
  col: number;
  detail?: string;
}

export interface SkillGraph {
  skill: string;
  lang: SkillLang;
  file: string;
  nodes: AgentNode[];
  parse_error?: string;
}

export async function fetchSkills(): Promise<SkillSummary[]> {
  const res = await fetch(`${API_BASE}/api/agent/dev/skills`, {
    headers: buildHeaders(),
  });
  if (res.status === 503) return [];
  if (!res.ok) {
    throw new Error(`fetchSkills: ${res.status} ${res.statusText}`);
  }
  const body = (await res.json()) as { skills?: SkillSummary[] };
  return body.skills ?? [];
}

export async function fetchSkill(name: string): Promise<SkillGraph | null> {
  const res = await fetch(
    `${API_BASE}/api/agent/dev/skills/${encodeURIComponent(name)}`,
    { headers: buildHeaders() },
  );
  if (res.status === 404 || res.status === 503) return null;
  if (!res.ok) {
    throw new Error(`fetchSkill(${name}): ${res.status} ${res.statusText}`);
  }
  // Coerce a stray `null` nodes field to `[]` — the Go backend
  // guarantees `[]` on the wire, but the frontend null-derefs on
  // `graph.nodes.length` so we belt-and-suspenders here too.
  const g = (await res.json()) as SkillGraph;
  return { ...g, nodes: g.nodes ?? [] };
}

// === Watcher events (SSE) ===

export interface WatcherEvent {
  path: string;
  lang?: 'go' | 'ts' | '';
  op: string;
  time: string;
}

export interface WatcherSubscription {
  close(): void;
}

export function subscribeToWatcher(
  onEvent: (ev: WatcherEvent) => void,
  onError?: (err: Error) => void,
): WatcherSubscription {
  const url = `${API_BASE}/api/agent/dev/events`;
  const es = new EventSource(url, { withCredentials: false });
  es.onmessage = (msg) => {
    try {
      const ev = JSON.parse(msg.data) as WatcherEvent;
      onEvent(ev);
    } catch (err) {
      if (onError) onError(err as Error);
    }
  };
  es.addEventListener('error', () => {
    if (onError) onError(new Error('watcher stream interrupted'));
  });
  return {
    close() {
      es.close();
    },
  };
}

// === editor:// link ===

/**
 * editorURL builds the URL the IDE opens when a node is clicked.
 * Modern editors register a custom protocol — VS Code is `vscode://`,
 * IntelliJ is `idea://`, Zed is `zed://`. We default to `vscode://`
 * because it's the most-installed; a future setting will let the
 * operator override the prefix.
 */
export function editorURL(skillDir: string, file: string, line: number): string {
  const path = file.startsWith('/') ? file : `${skillDir}/${file}`;
  return `vscode://file/${encodeURI(path)}:${line}`;
}

// === Run trace events (SSE — uses existing /api/agent/runs surface) ===

export interface RunEventPayload {
  // Keys vary by event type; the IDE branches on `type`.
  [key: string]: unknown;
}

export interface RunEvent {
  run_id: string;
  seq: number;
  time: string;
  type: string;
  payload?: RunEventPayload;
}

export interface TraceSubscription {
  close(): void;
}

export function subscribeToRunEvents(
  runID: string,
  onEvent: (ev: RunEvent) => void,
  onError?: (err: Error) => void,
): TraceSubscription {
  const url = `${API_BASE}/api/agent/runs/${encodeURIComponent(runID)}/events`;
  const es = new EventSource(url, { withCredentials: false });
  es.onmessage = (msg) => {
    try {
      const ev = JSON.parse(msg.data) as RunEvent;
      onEvent(ev);
    } catch (err) {
      if (onError) onError(err as Error);
    }
  };
  es.addEventListener('error', () => {
    if (onError) onError(new Error('run event stream interrupted'));
  });
  return {
    close() {
      es.close();
    },
  };
}

export interface ResumeRequest {
  from_step?: string;
}

export async function resumeRun(
  runID: string,
  req: ResumeRequest = {},
): Promise<unknown> {
  const res = await fetch(
    `${API_BASE}/api/agent/runs/${encodeURIComponent(runID)}/resume`,
    {
      method: 'POST',
      headers: buildHeaders({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(req),
    },
  );
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    throw new Error(`resume failed: ${res.status} ${text}`);
  }
  return res.json();
}
