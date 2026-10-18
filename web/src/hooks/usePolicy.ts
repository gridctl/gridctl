import { useEffect, useState } from 'react';
import { fetchPolicy, type PolicyStatus } from '../lib/api';
import { useAuthStore } from '../stores/useAuthStore';

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

  useEffect(() => {
    if (!enabled || authRequired) return;
    let active = true;

    const load = async () => {
      try {
        const data = await fetchPolicy();
        if (!active || useAuthStore.getState().generation !== generation) return;
        setReport(data);
        setError(null);
      } catch (err) {
        if (!active || useAuthStore.getState().generation !== generation) return;
        setError(err instanceof Error ? err.message : 'Failed to load policy');
      }
    };

    void load();
    const id = setInterval(load, POLICY_POLL_MS);
    return () => {
      active = false;
      clearInterval(id);
    };
  }, [enabled, generation, authRequired]);

  return { report, error };
}
