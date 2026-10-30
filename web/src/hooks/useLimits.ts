import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchLimits, type LimitsReport } from '../lib/api';
import { useAuthStore } from '../stores/useAuthStore';
import { useInterval } from './useInterval';

// Poll cadence for limit state. Rate limits move at tool-call speed, not
// render speed; 15s matches the tool-usage poll and is fast enough for the
// warn/exceeded chip to feel live.
const LIMITS_POLL_MS = 15000;

export interface LimitsState {
  report: LimitsReport | null;
  error: string | null;
}

// useLimits fetches GET /api/limits and refreshes it on an interval while
// `enabled` is true. A stack without a limits: block returns
// configured: false, which every consumer treats as "render nothing" — the
// hook itself stays mounted so a hot reload that adds limits shows up on the
// next poll. Failures surface as an error string; limits are best-effort
// overlay data and must never crash a metrics surface.
export function useLimits(enabled: boolean): LimitsState {
  const generation = useAuthStore(s => s.generation);
  const authRequired = useAuthStore(s => s.authRequired);
  const [report, setReport] = useState<LimitsReport | null>(null);
  const [error, setError] = useState<string | null>(null);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; };
  }, []);

  const load = useCallback(async () => {
    try {
      const data = await fetchLimits();
      if (!alive.current || useAuthStore.getState().generation !== generation || useAuthStore.getState().authRequired) return;
      setReport(data);
      setError(null);
    } catch (err) {
      if (!alive.current || useAuthStore.getState().generation !== generation || useAuthStore.getState().authRequired) return;
      setError(err instanceof Error ? err.message : 'Failed to load limits');
    }
  }, [generation]);

  useInterval(load, LIMITS_POLL_MS, { enabled: enabled && !authRequired });

  return { report, error };
}
