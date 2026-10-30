import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchGroups, type GroupsReport } from '../lib/api';
import { useInterval } from './useInterval';

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
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; };
  }, []);

  const load = useCallback(async () => {
    try {
      const data = await fetchGroups();
      if (!alive.current) return;
      setReport(data);
      setError(null);
    } catch (err) {
      if (!alive.current) return;
      setError(err instanceof Error ? err.message : 'Failed to load groups');
    }
  }, []);

  useInterval(load, GROUPS_POLL_MS, { enabled });

  return { report, error };
}
