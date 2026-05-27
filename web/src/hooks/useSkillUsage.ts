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
}

// useSkillUsage fetches GET /api/skills/usage and refreshes it on an interval.
// Usage is a best-effort overlay joined to skills by name; a fetch failure is
// swallowed and the last good snapshot is retained (progressive disclosure,
// like the sources fetch in the Library). State writes happen only inside the
// async loader after an await tick, never synchronously in the effect body, so
// a refetch cannot cascade an extra synchronous render.
export function useSkillUsage(): SkillUsageState {
  const [usage, setUsage] = useState<SkillUsageResponse | null>(null);

  useEffect(() => {
    let active = true;

    const load = async () => {
      try {
        const data = await fetchSkillUsage();
        if (!active) return;
        setUsage(data);
      } catch {
        // Usage unavailable: keep the last snapshot (or null on first load)
        // so the Library shows no usage UI rather than an error.
      }
    };

    void load();
    const id = setInterval(load, USAGE_POLL_MS);
    return () => {
      active = false;
      clearInterval(id);
    };
  }, []);

  return { usage };
}
