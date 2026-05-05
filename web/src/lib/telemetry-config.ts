// Parses raw stack YAML into a resolved view of telemetry persistence
// configuration. The backend is the source of truth for the on-disk YAML;
// this helper exists so the frontend can render the header pill, sidebar
// tri-state, and graph dot without an extra round-trip after every PATCH.
//
// We accept "best effort" parsing: a malformed stack file should not crash
// the UI — it returns an empty config so the rest of the app continues to
// render and the operator can fix the YAML in the spec tab.

import { parse as parseYAML } from 'yaml';
import type {
  ResolvedTelemetry,
  ServerPersistOverride,
  TelemetryPersistDefaults,
  TelemetryRetention,
  TelemetrySignal,
} from '../types';

const SIGNALS: TelemetrySignal[] = ['logs', 'metrics', 'traces'];

interface RawStack {
  telemetry?: {
    persist?: Record<string, unknown>;
    retention?: Record<string, unknown>;
  };
  'mcp-servers'?: Array<{
    name?: string;
    telemetry?: {
      persist?: Record<string, unknown>;
    } | null;
  }>;
}

const EMPTY: ResolvedTelemetry = { global: {}, servers: {} };

/**
 * Parse a stack YAML string into a resolved telemetry view. Errors and
 * unexpected shapes degrade to the empty config rather than throwing, so
 * an unrelated parse failure never blocks the rest of the UI from rendering.
 */
export function parseStackTelemetry(raw: string | null | undefined): ResolvedTelemetry {
  if (!raw) return EMPTY;
  let doc: unknown;
  try {
    doc = parseYAML(raw);
  } catch {
    return EMPTY;
  }
  if (!doc || typeof doc !== 'object') return EMPTY;
  const stack = doc as RawStack;

  const global: TelemetryPersistDefaults = {};
  for (const sig of SIGNALS) {
    const v = stack.telemetry?.persist?.[sig];
    if (typeof v === 'boolean') global[sig] = v;
  }

  let retention: TelemetryRetention | undefined;
  const r = stack.telemetry?.retention;
  if (r && typeof r === 'object') {
    const out: TelemetryRetention = {};
    if (typeof r.max_size_mb === 'number') out.max_size_mb = r.max_size_mb;
    if (typeof r.max_backups === 'number') out.max_backups = r.max_backups;
    if (typeof r.max_age_days === 'number') out.max_age_days = r.max_age_days;
    if (Object.keys(out).length > 0) retention = out;
  }

  const servers: Record<string, ServerPersistOverride> = {};
  const list = Array.isArray(stack['mcp-servers']) ? stack['mcp-servers'] : [];
  for (const s of list) {
    if (!s || typeof s.name !== 'string' || s.name === '') continue;
    const persist = s.telemetry?.persist;
    if (!persist || typeof persist !== 'object') continue;
    const override: ServerPersistOverride = {};
    for (const sig of SIGNALS) {
      const v = persist[sig];
      if (typeof v === 'boolean') override[sig] = v;
    }
    if (Object.keys(override).length > 0) {
      servers[s.name] = override;
    }
  }

  return { global, servers, ...(retention ? { retention } : {}) };
}

/**
 * Resolve the effective on/off state for a single (server, signal) pair
 * using the same inheritance rules as the Go backend's PersistLogs/Metrics/
 * Traces helpers: explicit per-server override wins; otherwise the
 * stack-global default; otherwise false.
 */
export function effectiveSignal(
  cfg: ResolvedTelemetry,
  serverName: string,
  signal: TelemetrySignal,
): boolean {
  const override = cfg.servers[serverName]?.[signal];
  if (typeof override === 'boolean') return override;
  return cfg.global[signal] ?? false;
}

/**
 * Convenience: are any signals effectively persisted for this server?
 * Drives the graph node dot indicator and the sidebar "active" pill.
 */
export function anySignalEnabled(cfg: ResolvedTelemetry, serverName: string): boolean {
  return SIGNALS.some((s) => effectiveSignal(cfg, serverName, s));
}

/**
 * Tri-state for the sidebar control. inherit means the per-server override
 * is absent; on/off mean explicit true/false. The state machine in the UI
 * cycles inherit → on → off → inherit, mapping each transition to the
 * PATCH body shape described in the API helpers.
 */
export type TriState = 'inherit' | 'on' | 'off';

export function readTriState(
  cfg: ResolvedTelemetry,
  serverName: string,
  signal: TelemetrySignal,
): TriState {
  const override = cfg.servers[serverName]?.[signal];
  if (override === true) return 'on';
  if (override === false) return 'off';
  return 'inherit';
}

/** Cycle to the next tri-state: inherit → on → off → inherit. */
export function nextTriState(state: TriState): TriState {
  switch (state) {
    case 'inherit':
      return 'on';
    case 'on':
      return 'off';
    case 'off':
      return 'inherit';
  }
}

/**
 * Map a tri-state transition to the PATCH body value for a single signal:
 * on → true, off → false, inherit → null (clears the override).
 */
export function triStateToPatch(state: TriState): boolean | null {
  switch (state) {
    case 'on':
      return true;
    case 'off':
      return false;
    case 'inherit':
      return null;
  }
}
