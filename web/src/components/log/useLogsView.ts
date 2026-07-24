import { useCallback, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router';
import { useLogStream } from '../../hooks/useLogStream';
import { copyTextToClipboard } from '../../lib/clipboard';
import {
  LOG_LEVELS,
  filterParsedLogs,
  logSourceOf,
  normalizeLogSourceParam,
  type LogLevel,
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

export interface LogsViewState {
  logs: ParsedLog[];
  filteredLogs: ParsedLog[];
  isLoading: boolean;
  error: string | null;
  source: string | null;
  searchQuery: string;
  traceFilter: string | null;
  enabledLevels: Set<LogLevel>;
  /** True when any filter (source included) can be hiding entries. */
  hasActiveFilter: boolean;
  /** Per-source counts under the active level/search/trace filters. */
  sourceCounts: Map<string, number>;
  /** Entry count under the active level/search/trace filters, all sources. */
  facetTotal: number;
  isPaused: boolean;
  setSource: (next: string | null) => void;
  setSearchQuery: (q: string) => void;
  toggleLevel: (level: LogLevel) => void;
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
// ?agent=, ?q=, ?level= (incl. the `none` sentinel), and ?trace= behave
// identically in either window and every view is a shareable deep link.
export function useLogsView(): LogsViewState {
  const [searchParams, setSearchParams] = useSearchParams();
  const [isPaused, setIsPaused] = useState(false);
  const { logs, isLoading, error, refresh, clear } = useLogStream({ active: true, paused: isPaused });

  const source = normalizeLogSourceParam(searchParams.get('agent'));
  const searchQuery = searchParams.get('q') ?? '';
  const traceFilter = searchParams.get('trace');
  const levelParam = searchParams.get('level');
  const enabledLevels = useMemo(() => levelsFromParam(levelParam), [levelParam]);

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

  const setSource = useCallback(
    (next: string | null) => {
      updateParams((p) => {
        if (next) p.set('agent', next);
        else p.delete('agent');
      });
    },
    [updateParams],
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

  const toggleLevel = useCallback(
    (level: LogLevel) => {
      const next = new Set(enabledLevels);
      if (next.has(level)) next.delete(level);
      else next.add(level);
      updateParams((p) => {
        if (next.size === ALL_LEVELS.size) p.delete('level');
        else if (next.size === 0) p.set('level', NO_LEVELS_PARAM);
        else p.set('level', [...next].map((l) => l.toLowerCase()).join(','));
      });
    },
    [enabledLevels, updateParams],
  );

  const clearTraceFilter = useCallback(() => {
    updateParams((p) => p.delete('trace'));
  }, [updateParams]);

  // Source counts as a filter: the empty-state CTA must recover from a
  // source-only view too, so `agent` clears with the rest.
  const clearFilters = useCallback(() => {
    updateParams((p) => {
      p.delete('agent');
      p.delete('q');
      p.delete('level');
      p.delete('trace');
    });
  }, [updateParams]);

  // Facet pass: every filter except source. Rail and dropdown counts show
  // what selecting each source would yield under the current filters, not
  // raw buffer totals.
  const facetLogs = useMemo(
    () => filterParsedLogs(logs, { levels: enabledLevels, query: searchQuery, traceId: traceFilter }),
    [logs, enabledLevels, searchQuery, traceFilter],
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
    enabledLevels.size !== ALL_LEVELS.size;

  const copyFiltered = useCallback(
    () => copyTextToClipboard(filteredLogs.map((log) => log.raw).join('\n')),
    [filteredLogs],
  );

  const togglePause = useCallback(() => setIsPaused((p) => !p), []);

  const filterQuery = useMemo(() => {
    const p = new URLSearchParams();
    if (source) p.set('agent', source);
    if (searchQuery) p.set('q', searchQuery);
    if (levelParam) p.set('level', levelParam);
    if (traceFilter) p.set('trace', traceFilter);
    return p.toString();
  }, [source, searchQuery, levelParam, traceFilter]);

  return {
    logs,
    filteredLogs,
    isLoading,
    error,
    source,
    searchQuery,
    traceFilter,
    enabledLevels,
    hasActiveFilter,
    sourceCounts,
    facetTotal: facetLogs.length,
    isPaused,
    setSource,
    setSearchQuery,
    toggleLevel,
    clearTraceFilter,
    clearFilters,
    togglePause,
    refresh,
    clear,
    copyFiltered,
    filterQuery,
  };
}
