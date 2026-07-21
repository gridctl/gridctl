import { useEffect, useState } from 'react';
import { fetchLimits, type LimitsReport } from '../lib/api';

// Poll cadence for limit consumption. Budgets move at tool-call speed, not
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
  const [report, setReport] = useState<LimitsReport | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled) return;
    let active = true;

    // State writes happen only inside this async loader (after an await
    // tick), never synchronously in the effect body.
    const load = async () => {
      try {
        const data = await fetchLimits();
        if (!active) return;
        setReport(data);
        setError(null);
      } catch (err) {
        if (!active) return;
        setError(err instanceof Error ? err.message : 'Failed to load limits');
      }
    };

    void load();
    const id = setInterval(load, LIMITS_POLL_MS);
    return () => {
      active = false;
      clearInterval(id);
    };
  }, [enabled]);

  return { report, error };
}
