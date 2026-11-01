import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchToolUsage } from '../lib/api';
import { messageOf } from '../lib/describeError';
import type { ToolUsageResponse } from '../types';
import { useAuthStore } from '../stores/useAuthStore';
import { useInterval } from './useInterval';

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
  const generation = useAuthStore(s => s.generation);
  const authRequired = useAuthStore(s => s.authRequired);
  const [usage, setUsage] = useState<ToolUsageResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [fetchedAt, setFetchedAt] = useState<number | null>(null);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; };
  }, []);

  const load = useCallback(async () => {
    try {
      const data = await fetchToolUsage();
      if (!alive.current || useAuthStore.getState().generation !== generation || useAuthStore.getState().authRequired) return;
      setUsage(data);
      setFetchedAt(Date.now());
      setError(null);
    } catch (err) {
      if (!alive.current || useAuthStore.getState().generation !== generation || useAuthStore.getState().authRequired) return;
      setError(messageOf(err) || 'Failed to load tool usage');
    }
  }, [generation]);

  useInterval(load, USAGE_POLL_MS, { enabled: enabled && !authRequired });

  return { usage, error, fetchedAt };
}
