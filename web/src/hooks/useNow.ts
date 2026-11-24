import { useState } from 'react';
import { useInterval } from './useInterval';

const noop = () => {};

// A UI clock: the returned timestamp refreshes every intervalMs, so a caller
// that formats a relative time re-renders on its own. It reuses useInterval's
// lastRunAt state (one write per tick) and pauses while the tab is hidden.
export function useNow(intervalMs: number): number {
  const [mountedAt] = useState(() => Date.now());
  const { lastRunAt } = useInterval(noop, intervalMs, { immediate: false });
  return lastRunAt ?? mountedAt;
}
