import { create } from 'zustand';
import { subscribeWithSelector } from 'zustand/middleware';
import type { InventoryRecord, ResolvedTelemetry, TelemetrySignal } from '../types';
import { parseStackTelemetry } from '../lib/telemetry-config';

// Aggregate snapshot for the header pill / wipe modal. servers is the
// distinct count of servers with at least one inventory record; oldest /
// newest are the global span across all (server, signal) pairs. Computed
// once per store update so consumers don't re-derive on every render.
export interface TelemetrySummary {
  totalBytes: number;
  servers: number;
  signals: TelemetrySignal[];
  oldest?: Date;
  newest?: Date;
}

interface TelemetryState {
  inventory: InventoryRecord[];
  // Resolved view derived from the stack YAML content. Updated whenever the
  // raw spec changes; consumers read `config` rather than re-parsing.
  config: ResolvedTelemetry;
  rawSpec: string | null;
  // Last successful fetch — informational, used for spinner suppression
  // and stale-data badges if the network drops.
  lastFetchedAt: Date | null;
  error: string | null;

  setInventory: (records: InventoryRecord[]) => void;
  setRawSpec: (content: string | null) => void;
  setError: (error: string | null) => void;
}

const EMPTY_CONFIG: ResolvedTelemetry = { global: {}, servers: {} };

export const useTelemetryStore = create<TelemetryState>()(
  subscribeWithSelector((set) => ({
    inventory: [],
    config: EMPTY_CONFIG,
    rawSpec: null,
    lastFetchedAt: null,
    error: null,

    setInventory: (records) =>
      set({ inventory: records ?? [], lastFetchedAt: new Date(), error: null }),

    // The raw spec arrives as a YAML string; we parse it once on write so
    // every read is a cheap object lookup. A nil/empty spec collapses to
    // the empty resolved config — consistent with stackless mode.
    setRawSpec: (content) => {
      if (content === null) {
        set({ rawSpec: null, config: EMPTY_CONFIG });
        return;
      }
      set({ rawSpec: content, config: parseStackTelemetry(content) });
    },

    setError: (error) => set({ error }),
  })),
);

// === Selectors ===
//
// These are plain functions (not hooks) so callers can use them in both
// component bodies (via `useTelemetryStore` selectors) and in event
// handlers (via `useTelemetryStore.getState()`).

export function inventoryByServer(
  inventory: InventoryRecord[],
  serverName: string,
): InventoryRecord[] {
  return inventory.filter((r) => r.server === serverName);
}

export function totalSizeBytes(inventory: InventoryRecord[]): number {
  return inventory.reduce((sum, r) => sum + r.sizeBytes, 0);
}

export function summarize(inventory: InventoryRecord[]): TelemetrySummary {
  if (inventory.length === 0) {
    return { totalBytes: 0, servers: 0, signals: [] };
  }
  const seenServers = new Set<string>();
  const seenSignals = new Set<TelemetrySignal>();
  let totalBytes = 0;
  let oldest: Date | undefined;
  let newest: Date | undefined;
  for (const r of inventory) {
    seenServers.add(r.server);
    seenSignals.add(r.signal);
    totalBytes += r.sizeBytes;
    const o = new Date(r.oldestTime);
    const n = new Date(r.newestTime);
    if (!oldest || o < oldest) oldest = o;
    if (!newest || n > newest) newest = n;
  }
  // Preserve the canonical signal order (logs, metrics, traces) rather than
  // insertion order — keeps the wipe-modal enumeration stable across polls.
  const order: TelemetrySignal[] = ['logs', 'metrics', 'traces'];
  const signals = order.filter((s) => seenSignals.has(s));
  return {
    totalBytes,
    servers: seenServers.size,
    signals,
    oldest,
    newest,
  };
}

// Hook variants for component subscriptions. They subscribe to inventory
// only — the derived data is recomputed on each render but the inputs are
// referentially stable so React skips work via memoization at the consumer
// level if needed.
export const useInventory = () => useTelemetryStore((s) => s.inventory);
export const useTelemetryConfig = () => useTelemetryStore((s) => s.config);
