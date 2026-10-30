import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchPolicy, type PolicyStatus } from '../lib/api';
import { useAuthStore } from '../stores/useAuthStore';
import { useInterval } from './useInterval';

const POLICY_POLL_MS = 15000;

export interface PolicyState {
  report: PolicyStatus | null;
  error: string | null;
}

// usePolicy fetches GET /api/policy and refreshes it while enabled. A stack
// without a policy: block returns configured: false. Failures stay local;
// the chip and sidebar summary must not crash the workspace.
export function usePolicy(enabled: boolean): PolicyState {
  const generation = useAuthStore((s) => s.generation);
  const authRequired = useAuthStore((s) => s.authRequired);
  const [report, setReport] = useState<PolicyStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const alive = useRef(true);
  useEffect(() => {
    alive.current = true;
    return () => { alive.current = false; };
  }, []);

  const load = useCallback(async () => {
    try {
      const data = await fetchPolicy();
      if (!alive.current || useAuthStore.getState().generation !== generation || useAuthStore.getState().authRequired) return;
      setReport(data);
      setError(null);
    } catch (err) {
      if (!alive.current || useAuthStore.getState().generation !== generation || useAuthStore.getState().authRequired) return;
      setError(err instanceof Error ? err.message : 'Failed to load policy');
    }
  }, [generation]);

  useInterval(load, POLICY_POLL_MS, { enabled: enabled && !authRequired });

  return { report, error };
}
