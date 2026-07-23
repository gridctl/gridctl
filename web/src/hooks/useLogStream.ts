import { useCallback, useEffect, useRef, useState } from 'react';
import { parseLogEntry, type ParsedLog } from '../components/log/logTypes';
import { fetchGatewayLogs } from '../lib/api';
import { POLLING } from '../lib/constants';

interface UseLogStreamOptions {
  /** Fetch + poll only while true (workspace mounted, tab visible, ...). */
  active: boolean;
  /** Suspends polling without dropping the buffered entries. */
  paused?: boolean;
  lines?: number;
}

interface UseLogStreamResult {
  logs: ParsedLog[];
  isLoading: boolean;
  error: string | null;
  refresh: () => void;
  clear: () => void;
}

/**
 * Shared aggregate log stream. Always reads GET /api/logs — the whole
 * multi-server ring buffer — and leaves source/level/search filtering to the
 * caller via `filterParsedLogs`. Per-server views are client-side filters over
 * this stream, never a second fetch path (the per-server endpoint returns
 * unstructured strings and would fork the rendering).
 *
 * `clear()` records a watermark instead of just emptying local state: the ring
 * buffer is server-side, so a bare reset would repopulate on the next poll.
 * Entries at or before the newest cleared timestamp stay hidden.
 */
export function useLogStream({ active, paused = false, lines = 500 }: UseLogStreamOptions): UseLogStreamResult {
  const [logs, setLogs] = useState<ParsedLog[]>([]);
  const [isLoading, setIsLoading] = useState(active);
  const [error, setError] = useState<string | null>(null);
  const clearedBeforeRef = useRef<number | null>(null);

  const fetchLogs = useCallback(async () => {
    try {
      const entries = await fetchGatewayLogs(lines);
      let parsed = (entries ?? []).map(parseLogEntry);
      const clearedBefore = clearedBeforeRef.current;
      if (clearedBefore != null) {
        parsed = parsed.filter((log) => {
          const ts = Date.parse(log.timestamp);
          return Number.isNaN(ts) || ts > clearedBefore;
        });
      }
      setLogs(parsed);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch logs');
    } finally {
      setIsLoading(false);
    }
  }, [lines]);

  // Fetch on activation, then poll while active and not paused.
  useEffect(() => {
    if (!active) return;
    fetchLogs();
    if (paused) return;
    const interval = window.setInterval(fetchLogs, POLLING.LOGS);
    return () => clearInterval(interval);
  }, [active, paused, fetchLogs]);

  const clear = useCallback(() => {
    setLogs((prev) => {
      const newest = prev.reduce((max, log) => {
        const ts = Date.parse(log.timestamp);
        return Number.isNaN(ts) ? max : Math.max(max, ts);
      }, clearedBeforeRef.current ?? 0);
      clearedBeforeRef.current = newest > 0 ? newest : Date.now();
      return [];
    });
  }, []);

  return { logs, isLoading, error, refresh: fetchLogs, clear };
}
