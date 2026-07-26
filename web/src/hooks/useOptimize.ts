import { useEffect, useState } from 'react';
import { fetchOptimizeReport } from '../lib/api';
import type { OptimizeReport } from '../types';

// Poll cadence for optimize findings. Findings move at tool-call/analysis
// speed; 15s matches the tool-usage and limits polls (each mounted surface
// polls independently, same as those hooks — the sidebar previously polled
// at 5s inline).
const OPTIMIZE_POLL_MS = 15000;

export interface OptimizeState {
  report: OptimizeReport | null;
  error: string | null;
}

// useOptimize fetches GET /api/optimize and refreshes it on an interval while
// `enabled` is true. Failures surface as an error string; findings are
// best-effort advisory data and must never crash a metrics surface.
export function useOptimize(enabled: boolean): OptimizeState {
  const [report, setReport] = useState<OptimizeReport | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled) return;
    let active = true;

    // State writes happen only inside this async loader (after an await
    // tick), never synchronously in the effect body.
    const load = async () => {
      try {
        const data = await fetchOptimizeReport();
        if (!active) return;
        setReport(data);
        setError(null);
      } catch (err) {
        if (!active) return;
        setError(err instanceof Error ? err.message : 'Failed to load optimize report');
      }
    };

    void load();
    const id = setInterval(load, OPTIMIZE_POLL_MS);
    return () => {
      active = false;
      clearInterval(id);
    };
  }, [enabled]);

  return { report, error };
}
