import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router';
import { useLogStream } from '../../hooks/useLogStream';
import { copyTextToClipboard } from '../../lib/clipboard';
import { useUIStore } from '../../stores/useUIStore';
import {
  DEFAULT_LOG_WINDOW,
  LOG_LEVELS,
  LOG_TIME_RANGE_MS,
  filterParsedLogs,
  logSourceOf,
  normalizeLogSourceParam,
  normalizeLogTimeRangeParam,
  normalizeLogWindowParam,
  type LogLevel,
  type LogTimeRange,
  type ParsedLog,
} from './logTypes';

const ALL_LEVELS: ReadonlySet<LogLevel> = new Set(LOG_LEVELS);

// `none` is the every-level-disabled sentinel: an empty CSV would read back as
// "param absent" and silently re-enable all levels on the round-trip.
const NO_LEVELS_PARAM = 'none';

function levelsFromParam(param: string | null): Set<LogLevel> {
  if (!param) return new Set(ALL_LEVELS);
  if (param === NO_LEVELS_PARAM) return new Set<LogLevel>();
  const parsed = param
    .split(',')
    .map((l) => l.trim().toUpperCase())
    .filter((l): l is LogLevel => (ALL_LEVELS as Set<string>).has(l));
  return parsed.length > 0 ? new Set(parsed) : new Set(ALL_LEVELS);
}

function levelsToParam(levels: Set<LogLevel>): string | null {
  if (levels.size === ALL_LEVELS.size) return null;
  if (levels.size === 0) return NO_LEVELS_PARAM;
  return [...levels].map((l) => l.toLowerCase()).join(',');
}

export interface LogsViewState {
  logs: ParsedLog[];
  filteredLogs: ParsedLog[];
  isLoading: boolean;
  error: string | null;
  source: string | null;
  searchQuery: string;
  traceFilter: string | null;
  enabledLevels: Set<LogLevel>;
  /** True when the level set is exactly {ERROR} (the one-click Errors state). */
  errorsOnly: boolean;
  /** True when any filter (source included) can be hiding entries. */
  hasActiveFilter: boolean;
  /** Per-source counts under the active level/search/trace/time filters. */
  sourceCounts: Map<string, number>;
  /** Entry count under the active level/search/trace/time filters, all sources. */
  facetTotal: number;
  isPaused: boolean;
  /** Poll window size (one of LOG_WINDOW_SIZES). */
  windowSize: number;
  /** Client-side time window over buffered timestamps. */
  timeRange: LogTimeRange;
  /** Entries currently in the server ring. */
  bufferTotal: number;
  /** Server ring capacity; 0 when unknown. */
  bufferCapacity: number;
  /** Soft-wrap long messages in collapsed rows (persisted preference). */
  wrap: boolean;
  /** Relative timestamps instead of absolute (persisted preference). */
  relativeTime: boolean;
  /** Anchor of the last completed load; freezes time windows while paused. */
  lastLoadedAt: number;
  /** Expanded entry key (entry identity, not index), shared across surfaces. */
  expandedKey: string | null;
  setExpandedKey: (key: string | null) => void;
  setSource: (next: string | null) => void;
  setSearchQuery: (q: string) => void;
  toggleLevel: (level: LogLevel) => void;
  toggleErrorsOnly: () => void;
  setWindowSize: (n: number) => void;
  setTimeRange: (range: LogTimeRange) => void;
  toggleWrap: () => void;
  toggleRelativeTime: () => void;
  clearTraceFilter: () => void;
  clearFilters: () => void;
  togglePause: () => void;
  refresh: () => void;
  clear: () => void;
  copyFiltered: () => void;
  /** Current filter state as a query string (popout hand-off). */
  filterQuery: string;
}

// URL-synced filter state plus the shared aggregate stream. Both the Logs
// workspace and the detached window drive their surface from this hook, so
// ?source= (legacy ?agent=), ?q=, ?level= (incl. the `none` sentinel),
// ?trace=, ?range=, and ?n= behave identically in either window and every
// view is a shareable deep link. Persisted preferences (source, level set,
// window size) seed a virgin /logs on mount; URL params always win.
export function useLogsView(): LogsViewState {
  const [searchParams, setSearchParams] = useSearchParams();
  const [isPaused, setIsPaused] = useState(false);
  const [expandedKey, setExpandedKey] = useState<string | null>(null);
  const logsPrefs = useUIStore((s) => s.logsPrefs);
  const setLogsPrefs = useUIStore((s) => s.setLogsPrefs);

  const updateParams = useCallback(
    (mutate: (p: URLSearchParams) => void) => {
      setSearchParams(
        (prev) => {
          const params = new URLSearchParams(prev);
          mutate(params);
          return params;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // Seed persisted source/level prefs once on a virgin /logs (no filter
  // params at all): a deep link keeps its exact meaning, while a bare visit
  // reopens the way the user left it. Window size seeds independently — it
  // is a view preference, not a filter. Written into the URL (not derived
  // continuously) so the address bar stays the single shareable truth.
  useEffect(() => {
    const prefs = useUIStore.getState().logsPrefs;
    updateParams((p) => {
      const hasFilterParams = ['source', 'agent', 'level', 'q', 'trace', 'range'].some((k) => p.has(k));
      if (!hasFilterParams) {
        if (prefs.source) p.set('source', prefs.source);
        if (prefs.levelParam) p.set('level', prefs.levelParam);
      }
      if (!p.has('n') && prefs.windowSize !== DEFAULT_LOG_WINDOW) p.set('n', String(prefs.windowSize));
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Legacy ?agent= reads permanently as an alias for ?source= — bookmarks and
  // already-open popout windows carry it and are never rewritten.
  const source = normalizeLogSourceParam(searchParams.get('source') ?? searchParams.get('agent'));
  const searchQuery = searchParams.get('q') ?? '';
  const traceFilter = searchParams.get('trace');
  const levelParam = searchParams.get('level');
  const enabledLevels = useMemo(() => levelsFromParam(levelParam), [levelParam]);
  const windowSize = normalizeLogWindowParam(searchParams.get('n'), DEFAULT_LOG_WINDOW);
  const timeRange = normalizeLogTimeRangeParam(searchParams.get('range'));

  const { logs, isLoading, error, bufferTotal, bufferCapacity, lastLoadedAt, refresh, clear } =
    useLogStream({ active: true, paused: isPaused, lines: windowSize });

  const setSource = useCallback(
    (next: string | null) => {
      updateParams((p) => {
        if (next) p.set('source', next);
        else p.delete('source');
        p.delete('agent');
      });
      setLogsPrefs({ source: next ?? '' });
    },
    [updateParams, setLogsPrefs],
  );

  const setSearchQuery = useCallback(
    (q: string) => {
      updateParams((p) => {
        if (q) p.set('q', q);
        else p.delete('q');
      });
    },
    [updateParams],
  );

  const writeLevels = useCallback(
    (next: Set<LogLevel>) => {
      const param = levelsToParam(next);
      updateParams((p) => {
        if (param === null) p.delete('level');
        else p.set('level', param);
      });
      setLogsPrefs({ levelParam: param ?? '' });
    },
    [updateParams, setLogsPrefs],
  );

  const toggleLevel = useCallback(
    (level: LogLevel) => {
      const next = new Set(enabledLevels);
      if (next.has(level)) next.delete(level);
      else next.add(level);
      writeLevels(next);
    },
    [enabledLevels, writeLevels],
  );

  const errorsOnly = enabledLevels.size === 1 && enabledLevels.has('ERROR');

  // One-click Errors is a toggle, not a one-way trip: switching it on
  // remembers the previous level selection and switching it off restores it.
  const levelsBeforeErrorsRef = useRef<Set<LogLevel> | null>(null);
  const toggleErrorsOnly = useCallback(() => {
    if (errorsOnly) {
      writeLevels(levelsBeforeErrorsRef.current ?? new Set(ALL_LEVELS));
      levelsBeforeErrorsRef.current = null;
    } else {
      levelsBeforeErrorsRef.current = new Set(enabledLevels);
      writeLevels(new Set<LogLevel>(['ERROR']));
    }
  }, [errorsOnly, enabledLevels, writeLevels]);

  const setWindowSize = useCallback(
    (n: number) => {
      updateParams((p) => {
        if (n === DEFAULT_LOG_WINDOW) p.delete('n');
        else p.set('n', String(n));
      });
      setLogsPrefs({ windowSize: n });
    },
    [updateParams, setLogsPrefs],
  );

  const setTimeRange = useCallback(
    (range: LogTimeRange) => {
      updateParams((p) => {
        if (range === 'all') p.delete('range');
        else p.set('range', range);
      });
    },
    [updateParams],
  );

  const toggleWrap = useCallback(
    () => setLogsPrefs({ wrap: !useUIStore.getState().logsPrefs.wrap }),
    [setLogsPrefs],
  );
  const toggleRelativeTime = useCallback(
    () => setLogsPrefs({ relativeTime: !useUIStore.getState().logsPrefs.relativeTime }),
    [setLogsPrefs],
  );

  const clearTraceFilter = useCallback(() => {
    updateParams((p) => p.delete('trace'));
  }, [updateParams]);

  // Source counts as a filter: the empty-state CTA must recover from a
  // source-only view too, so source clears with the rest — including the
  // persisted source/level prefs, or the next bare visit would re-apply
  // exactly what the user just cleared.
  const clearFilters = useCallback(() => {
    updateParams((p) => {
      p.delete('source');
      p.delete('agent');
      p.delete('q');
      p.delete('level');
      p.delete('trace');
      p.delete('range');
    });
    setLogsPrefs({ source: '', levelParam: '' });
  }, [updateParams, setLogsPrefs]);

  // Time cutoff anchored to the last completed load, not the render clock:
  // keeps the memo pure and freezes the window while paused.
  const since = useMemo(() => {
    const rangeMs = LOG_TIME_RANGE_MS[timeRange];
    if (rangeMs == null || lastLoadedAt <= 0) return null;
    return lastLoadedAt - rangeMs;
  }, [timeRange, lastLoadedAt]);

  // Facet pass: every filter except source. Rail and dropdown counts show
  // what selecting each source would yield under the current filters, not
  // raw buffer totals.
  const facetLogs = useMemo(
    () =>
      filterParsedLogs(logs, {
        levels: enabledLevels,
        query: searchQuery,
        traceId: traceFilter,
        since,
      }),
    [logs, enabledLevels, searchQuery, traceFilter, since],
  );

  const sourceCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const log of facetLogs) {
      const s = logSourceOf(log);
      counts.set(s, (counts.get(s) ?? 0) + 1);
    }
    return counts;
  }, [facetLogs]);

  const filteredLogs = useMemo(() => filterParsedLogs(facetLogs, { source }), [facetLogs, source]);

  const hasActiveFilter =
    source != null ||
    searchQuery !== '' ||
    traceFilter != null ||
    timeRange !== 'all' ||
    enabledLevels.size !== ALL_LEVELS.size;

  const copyFiltered = useCallback(
    () => copyTextToClipboard(filteredLogs.map((log) => log.raw).join('\n')),
    [filteredLogs],
  );

  const togglePause = useCallback(() => setIsPaused((p) => !p), []);

  const filterQuery = useMemo(() => {
    const p = new URLSearchParams();
    if (source) p.set('source', source);
    if (searchQuery) p.set('q', searchQuery);
    if (levelParam) p.set('level', levelParam);
    if (traceFilter) p.set('trace', traceFilter);
    if (timeRange !== 'all') p.set('range', timeRange);
    if (windowSize !== DEFAULT_LOG_WINDOW) p.set('n', String(windowSize));
    return p.toString();
  }, [source, searchQuery, levelParam, traceFilter, timeRange, windowSize]);

  return {
    logs,
    filteredLogs,
    isLoading,
    error,
    source,
    searchQuery,
    traceFilter,
    enabledLevels,
    errorsOnly,
    hasActiveFilter,
    sourceCounts,
    facetTotal: facetLogs.length,
    isPaused,
    windowSize,
    timeRange,
    bufferTotal,
    bufferCapacity,
    wrap: logsPrefs.wrap,
    relativeTime: logsPrefs.relativeTime,
    lastLoadedAt,
    expandedKey,
    setExpandedKey,
    setSource,
    setSearchQuery,
    toggleLevel,
    toggleErrorsOnly,
    setWindowSize,
    setTimeRange,
    toggleWrap,
    toggleRelativeTime,
    clearTraceFilter,
    clearFilters,
    togglePause,
    refresh,
    clear,
    copyFiltered,
    filterQuery,
  };
}
