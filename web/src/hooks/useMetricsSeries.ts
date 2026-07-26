import { useCallback, useEffect, useRef, useState } from 'react';
import { fetchTokenMetrics, fetchCostMetrics, clearTokenMetrics } from '../lib/api';
import { POLLING } from '../lib/constants';
import type { TokenMetricsResponse, CostMetricsResponse } from '../types';

export type MetricsTimeRange = 'live' | '1h' | '6h' | '24h' | '7d';

export const METRICS_TIME_RANGES: { value: MetricsTimeRange; label: string }[] = [
  { value: 'live', label: 'Live' },
  { value: '1h', label: '1h' },
  { value: '6h', label: '6h' },
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
];

// Map a UI range to the backend `range` param. "live" reads the last 30m and
// pairs with the auto-refresh below.
export function apiRangeFor(range: MetricsTimeRange): string {
  return range === 'live' ? '30m' : range;
}

// Parse a ?range= URL param; anything unknown (or absent) falls back to live,
// so deep links without a range keep today's meaning. Mirrors
// normalizeLogTimeRangeParam in the Logs workspace.
export function normalizeMetricsTimeRangeParam(param: string | null): MetricsTimeRange {
  return param === '1h' || param === '6h' || param === '24h' || param === '7d' ? param : 'live';
}

// Human label for the active window, shown beside window-scoped numbers.
// Live reads the last 30 minutes (see apiRangeFor), so it is labeled by that
// window rather than as a total.
export function windowLabelFor(range: MetricsTimeRange): string {
  return range === 'live' ? 'Last 30m' : `Last ${range}`;
}

interface UseMetricsSeriesArgs {
  timeRange: MetricsTimeRange;
  // Gates fetching entirely (e.g. the bottom tab only loads while visible).
  enabled?: boolean;
  // Suspends the live auto-refresh without unmounting.
  paused?: boolean;
  // Request per-client cost buckets too (drives the workspace inspector's
  // per-client cost sparkline). Off by default — the glance surfaces don't
  // need it.
  perClient?: boolean;
}

interface UseMetricsSeriesResult {
  metricsData: TokenMetricsResponse | null;
  costData: CostMetricsResponse | null;
  isLoading: boolean;
  error: string | null;
  reload: () => void;
  clear: () => Promise<void>;
}

// useMetricsSeries owns the token + cost time-series polling shared by the
// bottom Metrics tab, the Metrics workspace, and the detached window. It does
// NOT own the real-time status snapshot (tokenUsage / costUsage / models) —
// those come from the app store in-shell, or a local status poll in the
// detached window. Both series are fetched on one cycle (Promise.allSettled)
// so a transient failure of one endpoint never drops the other's data.
export function useMetricsSeries({
  timeRange,
  enabled = true,
  paused = false,
  perClient = false,
}: UseMetricsSeriesArgs): UseMetricsSeriesResult {
  const [metricsData, setMetricsData] = useState<TokenMetricsResponse | null>(null);
  const [costData, setCostData] = useState<CostMetricsResponse | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const intervalRef = useRef<number | null>(null);

  const apiRange = apiRangeFor(timeRange);

  // No synchronous setState here: the first statement awaits, so callers
  // (including the mount/range effect) never trigger a cascading render. The
  // skeleton is gated on `!metricsData`, so once data lands it never reappears
  // on a background refresh anyway; explicit reloads flip the flag themselves.
  const loadMetrics = useCallback(async () => {
    try {
      const [tokenResult, costResult] = await Promise.allSettled([
        fetchTokenMetrics(apiRange),
        fetchCostMetrics(apiRange, perClient),
      ]);
      if (tokenResult.status === 'fulfilled') setMetricsData(tokenResult.value);
      if (costResult.status === 'fulfilled') setCostData(costResult.value);
      const firstFailure =
        (tokenResult.status === 'rejected' && tokenResult.reason) ||
        (costResult.status === 'rejected' && costResult.reason);
      if (firstFailure) {
        setError(firstFailure instanceof Error ? firstFailure.message : 'Failed to fetch metrics');
      } else {
        setError(null);
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch metrics');
    } finally {
      setIsLoading(false);
    }
  }, [apiRange, perClient]);

  // Fetch on mount/enable and whenever the range changes. loadMetrics flips the
  // loading flag itself, so the effect body stays free of synchronous setState.
  useEffect(() => {
    if (!enabled) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async callback; state is set only after await, not synchronously
    void loadMetrics();
  }, [enabled, loadMetrics]);

  // Auto-refresh while live and not paused.
  useEffect(() => {
    if (!enabled || paused || timeRange !== 'live') {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      return;
    }
    intervalRef.current = window.setInterval(() => void loadMetrics(), POLLING.METRICS);
    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [enabled, paused, timeRange, loadMetrics]);

  const reload = useCallback(() => {
    void loadMetrics();
  }, [loadMetrics]);

  const clear = useCallback(async () => {
    await clearTokenMetrics();
    setMetricsData(null);
    setCostData(null);
    setIsLoading(true);
    void loadMetrics();
  }, [loadMetrics]);

  // The API echoes the requested range, so a response held over from a
  // previous range is never handed to consumers under the new label — the
  // hook reports "loading" until the in-flight fetch overwrites it. A failed
  // fetch keeps `error` set instead, so the mismatch cannot wedge the
  // loading state.
  const currentMetrics = metricsData && metricsData.range === apiRange ? metricsData : null;
  const currentCost = costData && costData.range === apiRange ? costData : null;
  const rangeSwitching = !error && metricsData !== null && currentMetrics === null;

  return {
    metricsData: currentMetrics,
    costData: currentCost,
    isLoading: isLoading || rangeSwitching,
    error,
    reload,
    clear,
  };
}
