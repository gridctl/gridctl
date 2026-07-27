import { useEffect, useState } from 'react';
import { fetchSkillUsage } from '../lib/api';
import type { SkillUsageResponse } from '../types';

// Poll cadence for skill usage. Usage shifts over hours/days, so a slow loop
// matches useToolUsage and keeps the join memo stable between renders.
const USAGE_POLL_MS = 15000;

export interface SkillUsageState {
  // null until the first successful load, and stays null when the endpoint is
  // unavailable (e.g. 503 when no metrics accumulator is wired) so the Library
  // degrades to no column / KPI / strip rather than erroring.
  usage: SkillUsageResponse | null;
  // Epoch ms of the last successful load, null before one lands. Together with
  // `error` this separates the four states the Library must tell apart:
  //   fetchedAt null, error null  → still loading
  //   fetchedAt null, error set   → unavailable (no accumulator wired)
  //   fetchedAt set,  error set   → stale (a refresh failed over a good snapshot)
  //   fetchedAt set,  error null  → live
  // Without it, "loading" and "nothing has ever been called" render identically.
  fetchedAt: number | null;
  // Message from the most recent failed fetch, cleared by the next success.
  error: string | null;
}

// useSkillUsage fetches GET /api/skills/usage and refreshes it on an interval.
// Usage is a best-effort overlay joined to skills by name; a fetch failure is
// recorded but the last good snapshot is retained (progressive disclosure,
// like the sources fetch in the Library). State writes happen only inside the
// async loader after an await tick, never synchronously in the effect body, so
// a refetch cannot cascade an extra synchronous render.
export function useSkillUsage(): SkillUsageState {
  const [state, setState] = useState<SkillUsageState>({
    usage: null,
    fetchedAt: null,
    error: null,
  });

  useEffect(() => {
    let active = true;

    const load = async () => {
      try {
        const data = await fetchSkillUsage();
        if (!active) return;
        setState({ usage: data, fetchedAt: Date.now(), error: null });
      } catch (err) {
        if (!active) return;
        // Usage unavailable: keep the last snapshot (or null on first load) so
        // the Library shows no usage UI rather than an error, but record why.
        setState((prev) => ({
          ...prev,
          error: err instanceof Error ? err.message : 'Usage unavailable',
        }));
      }
    };

    void load();
    const id = setInterval(load, USAGE_POLL_MS);
    return () => {
      active = false;
      clearInterval(id);
    };
  }, []);

  return state;
}
