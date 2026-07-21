import { useEffect, useState } from 'react';
import { fetchGroups, type GroupsReport } from '../lib/api';

// Poll cadence for group definitions. Groups change at stack-edit speed;
// 15s matches the tool-usage and limits polls.
const GROUPS_POLL_MS = 15000;

export interface GroupsState {
  report: GroupsReport | null;
  error: string | null;
}

// useGroups fetches GET /api/groups and refreshes it on an interval while
// `enabled` is true. A stack without a groups: block returns
// configured: false, which every consumer treats as "render nothing"; the
// hook stays mounted so a hot reload that adds groups shows up on the next
// poll. Failures surface as an error string; groups are overlay data and
// must never crash the Tools workspace.
export function useGroups(enabled: boolean): GroupsState {
  const [report, setReport] = useState<GroupsReport | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!enabled) return;
    let active = true;

    // State writes happen only inside this async loader (after an await
    // tick), never synchronously in the effect body.
    const load = async () => {
      try {
        const data = await fetchGroups();
        if (!active) return;
        setReport(data);
        setError(null);
      } catch (err) {
        if (!active) return;
        setError(err instanceof Error ? err.message : 'Failed to load groups');
      }
    };

    void load();
    const id = setInterval(load, GROUPS_POLL_MS);
    return () => {
      active = false;
      clearInterval(id);
    };
  }, [enabled]);

  return { report, error };
}
