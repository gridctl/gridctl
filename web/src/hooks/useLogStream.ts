import { useCallback, useEffect, useRef, useState } from 'react';
import { DEFAULT_LOG_WINDOW, parseLogEntry, type ParsedLog } from '../components/log/logTypes';
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
  /** Entries currently in the server ring (not just the fetched window). */
  bufferTotal: number;
  /** Maximum entries the server ring can hold; 0 when unknown. */
  bufferCapacity: number;
  /** Epoch ms of the last completed load; anchors client-side time windows. */
  lastLoadedAt: number;
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
export function useLogStream({ active, paused = false, lines = DEFAULT_LOG_WINDOW }: UseLogStreamOptions): UseLogStreamResult {
  const [logs, setLogs] = useState<ParsedLog[]>([]);
  const [isLoading, setIsLoading] = useState(active);
  const [error, setError] = useState<string | null>(null);
  const [bufferTotal, setBufferTotal] = useState(0);
  const [bufferCapacity, setBufferCapacity] = useState(0);
  const [lastLoadedAt, setLastLoadedAt] = useState(0);
  const clearedBeforeRef = useRef<number | null>(null);

  const fetchLogs = useCallback(async () => {
    try {
      const envelope = await fetchGatewayLogs(lines);
      let parsed = (envelope.logs ?? []).map(parseLogEntry);
      const clearedBefore = clearedBeforeRef.current;
      if (clearedBefore != null) {
        parsed = parsed.filter((log) => {
          const ts = Date.parse(log.timestamp);
          return Number.isNaN(ts) || ts > clearedBefore;
        });
      }
      setLogs(parsed);
      setBufferTotal(envelope.total ?? 0);
      setBufferCapacity(envelope.bufferCapacity ?? 0);
      setLastLoadedAt(Date.now());
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

  return { logs, isLoading, error, bufferTotal, bufferCapacity, lastLoadedAt, refresh: fetchLogs, clear };
}
