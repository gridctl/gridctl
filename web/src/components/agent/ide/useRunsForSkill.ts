import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchAgentRuns, type AgentRunSummary } from '../../../lib/agent-runs';

export interface UseRunsForSkillOptions {
  /**
   * When set, the returned `runs` are filtered to only include runs
   * for this skill. When omitted, every run within the fetch window
   * is returned — used by the sidebar Runs tab.
   */
  skillName?: string;
  /**
   * Maximum number of rows to keep after filtering. The fetch always
   * pulls `fetchLimit` rows from the API; this trims post-filter so
   * the modal's "Run like…" picker can render the most recent N.
   */
  limit?: number;
  /**
   * Number of rows requested from the API. Bigger windows give the
   * skill filter room to find matches when many other skills have
   * recently run.
   */
  fetchLimit?: number;
  /**
   * Bumping this value re-runs the fetch. Used by AgentIDE to
   * refresh the sidebar list when a new run is launched.
   */
  refreshKey?: number | string;
}

export interface UseRunsForSkillResult {
  runs: AgentRunSummary[];
  loading: boolean;
  error: string | null;
  refresh: () => void;
}

/**
 * useRunsForSkill is the shared fetch+filter hook behind both the
 * RunLauncherModal's "Run like…" picker and the SkillSidebar's Runs
 * tab. The single hook keeps the two surfaces honest about what a
 * "run" looks like and lets either surface refresh the other (via
 * the AgentIDE-managed refreshKey) when a new run is launched.
 *
 * The hook returns the unsorted server order — the API already
 * returns `started_at` desc. Consumers that need other orderings can
 * sort the returned slice.
 */
export function useRunsForSkill(options: UseRunsForSkillOptions = {}): UseRunsForSkillResult {
  const { skillName, limit, fetchLimit = 100, refreshKey } = options;
  const [runs, setRuns] = useState<AgentRunSummary[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [localKey, setLocalKey] = useState(0);

  // Track the in-flight request id so a stale fetch can't clobber a
  // newer one. The ref pattern keeps the cancellation check out of
  // the effect body (which would trip set-state-in-effect lint).
  const requestRef = useRef(0);

  const refresh = useCallback(() => setLocalKey((k) => k + 1), []);

  useEffect(() => {
    requestRef.current += 1;
    const myRequest = requestRef.current;
    fetchAgentRuns(fetchLimit)
      .then(({ runs: all }) => {
        if (requestRef.current !== myRequest) return;
        const filtered = skillName ? all.filter((r) => r.skill === skillName) : all;
        const trimmed = limit != null ? filtered.slice(0, limit) : filtered;
        setRuns(trimmed);
        setError(null);
        setLoading(false);
      })
      .catch((err: unknown) => {
        if (requestRef.current !== myRequest) return;
        setError(err instanceof Error ? err.message : String(err));
        setRuns([]);
        setLoading(false);
      });
    return () => {
      // Bump so the in-flight promise's settle handler bails out.
      requestRef.current += 1;
    };
  }, [skillName, limit, fetchLimit, refreshKey, localKey]);

  return { runs, loading, error, refresh };
}
