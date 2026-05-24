import { useEffect, useState } from 'react';
import { fetchToolUsage } from '../lib/api';
import type { ToolUsageResponse } from '../types';

// Poll cadence for Audit Mode usage. Slower than status polling — usage shifts
// over hours/days, so a tight loop buys nothing and the Fuse/classification
// memos are keyed on this data, not rebuilt per render.
const USAGE_POLL_MS = 15000;

export interface ToolUsageState {
  usage: ToolUsageResponse | null;
  error: string | null;
  // Epoch ms when `usage` was fetched. Audit classification uses this as
  // "now" so the clock read stays out of render (React purity) and matches
  // the snapshot it compares against. null until the first successful load.
  fetchedAt: number | null;
}

// useToolUsage fetches GET /api/tools/usage and refreshes it on an interval
// while `enabled` is true (Audit Mode on). When disabled it stops polling and
// retains the last snapshot so toggling back is instant; the next enable
// triggers an immediate refetch. The 401 path surfaces as an error string
// rather than throwing — the workspace renders usage as best-effort overlay
// data and must not crash the editor if it's unavailable.
export function useToolUsage(enabled: boolean): ToolUsageState {
  const [usage, setUsage] = useState<ToolUsageResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [fetchedAt, setFetchedAt] = useState<number | null>(null);

  useEffect(() => {
    if (!enabled) return;
    let active = true;

    // State writes happen only inside this async loader (after an await
    // tick), never synchronously in the effect body, so a refetch can't
    // cascade an extra synchronous render. Date.now() is read here (not in
    // render) so audit classification stays pure.
    const load = async () => {
      try {
        const data = await fetchToolUsage();
        if (!active) return;
        setUsage(data);
        setFetchedAt(Date.now());
        setError(null);
      } catch (err) {
        if (!active) return;
        setError(err instanceof Error ? err.message : 'Failed to load tool usage');
      }
    };

    void load();
    const id = setInterval(load, USAGE_POLL_MS);
    return () => {
      active = false;
      clearInterval(id);
    };
  }, [enabled]);

  return { usage, error, fetchedAt };
}
