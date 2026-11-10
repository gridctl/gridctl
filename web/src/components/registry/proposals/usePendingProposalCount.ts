import { useEffect, useState } from 'react';
import { fetchProposals } from '../../../lib/api';

// usePendingProposalCount loads the pending count once per mount for the
// kind switch badge. A failure leaves the badge hidden: the count is a
// discovery hint, and the Proposals segment reports load errors itself.
export function usePendingProposalCount(enabled: boolean): number | null {
  const [count, setCount] = useState<number | null>(null);
  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    const load = async () => {
      try {
        const doc = await fetchProposals({ status: 'pending' });
        if (!cancelled) setCount(doc.proposals?.length ?? 0);
      } catch {
        // Badge stays hidden.
      }
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [enabled]);
  return count;
}
