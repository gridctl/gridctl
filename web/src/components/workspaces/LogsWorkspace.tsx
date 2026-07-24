import { useCallback, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router';
import { Copy, Layers, Pause, Play, Radio, RefreshCw, Server, Trash2, X } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useUIStore } from '../../stores/useUIStore';
import { useStackStore } from '../../stores/useStackStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { useLogFontSize } from '../../hooks/useLogFontSize';
import { useLogStream } from '../../hooks/useLogStream';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { IconButton } from '../ui/IconButton';
import { PopoutButton } from '../ui/PopoutButton';
import { PersistedFromMarker } from '../telemetry/PersistedFromMarker';
import { copyTextToClipboard } from '../../lib/clipboard';
import {
  GATEWAY_LOG_SOURCE,
  LOG_LEVELS,
  LogFilterBar,
  LogStream,
  filterParsedLogs,
  logSourceOf,
  normalizeLogSourceParam,
  type LogLevel,
} from '../log';

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

// LogsWorkspace is the first-class log surface: the aggregate multi-server
// stream from GET /api/logs with no selection prerequisite. The left rail
// picks a source (all / gateway / per server) as a client-side filter over the
// same stream — never a second fetch path. Source, level, search, and trace
// filters are URL-synced (?agent=, ?level=, ?q=, ?trace=) so reload,
// deep-links, and the node-to-logs and trace-to-logs pivots all land on the
// exact view they name.
export function LogsWorkspace() {
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const compact = useUIStore((s) => s.compactMode.logs);
  const logsDetached = useUIStore((s) => s.logsDetached);
  const { openDetachedWindow } = useWindowManager();
  const mcpServers = useStackStore((s) => s.mcpServers);

  const [isPaused, setIsPaused] = useState(false);
  const { logs, isLoading, error, refresh, clear } = useLogStream({ active: true, paused: isPaused });

  const containerRef = useRef<HTMLDivElement>(null);
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } =
    useLogFontSize(containerRef);

  // ---- URL state ----------------------------------------------------------
  const source = normalizeLogSourceParam(searchParams.get('agent'));
  const searchQuery = searchParams.get('q') ?? '';
  const traceFilter = searchParams.get('trace');
  const enabledLevels = useMemo(() => levelsFromParam(searchParams.get('level')), [searchParams]);

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

  const clearFilters = useCallback(() => {
    updateParams((p) => {
      p.delete('q');
      p.delete('level');
      p.delete('trace');
    });
  }, [updateParams]);

  // ---- Derived data -------------------------------------------------------
  const sourceCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const log of logs) {
      const s = logSourceOf(log);
      counts.set(s, (counts.get(s) ?? 0) + 1);
    }
    return counts;
  }, [logs]);

  const serverNames = useMemo(() => {
    const names = new Set(mcpServers.map((s) => s.name));
    // Sources present in the stream but not (or no longer) deployed still get
    // a rail entry so their entries stay reachable.
    for (const name of sourceCounts.keys()) {
      if (name !== GATEWAY_LOG_SOURCE) names.add(name);
    }
    return [...names].sort();
  }, [mcpServers, sourceCounts]);

  const filteredLogs = useMemo(
    () =>
      filterParsedLogs(logs, {
        source,
        levels: enabledLevels,
        query: searchQuery,
        traceId: traceFilter,
      }),
    [logs, source, enabledLevels, searchQuery, traceFilter],
  );

  const hasActiveFilter =
    searchQuery !== '' || traceFilter != null || enabledLevels.size !== ALL_LEVELS.size;

  const handleCopy = () => copyTextToClipboard(filteredLogs.map((log) => log.raw).join('\n'));

  const leftRail = (
    <SourceRail
      compact={compact}
      source={source}
      onSelectSource={setSource}
      serverNames={serverNames}
      totalCount={logs.length}
      counts={sourceCounts}
    />
  );

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell workspace="logs" defaultLeftPct={16} left={leftRail} minLeftPx={180}>
        <main className="flex flex-col h-full overflow-hidden">
          <header
            className={cn(
              'flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle flex items-center justify-between gap-3 px-6',
              compact ? 'py-2' : 'py-3',
            )}
          >
            <div className="flex items-center gap-3 min-w-0">
              <div className="font-sans text-text-muted/60 text-[10px] uppercase tracking-[0.4em]">logs</div>
              <div className="font-mono text-[10px] text-text-muted truncate">
                {source ?? 'all sources'}
              </div>
              {isPaused && (
                <span className="text-[9px] px-1.5 py-0.5 bg-status-pending/15 text-status-pending rounded-full font-medium border border-status-pending/20">
                  Paused
                </span>
              )}
            </div>
            <div className="flex items-center gap-1">
              <IconButton
                icon={isPaused ? Play : Pause}
                onClick={() => setIsPaused((p) => !p)}
                tooltip={isPaused ? 'Resume' : 'Pause'}
                size="sm"
                variant="ghost"
                className={isPaused ? 'text-status-running hover:text-status-running' : ''}
              />
              <IconButton icon={RefreshCw} onClick={refresh} tooltip="Refresh" size="sm" variant="ghost" />
              <IconButton icon={Copy} onClick={handleCopy} tooltip="Copy Logs" size="sm" variant="ghost" />
              <IconButton
                icon={Trash2}
                onClick={clear}
                tooltip="Clear Logs"
                size="sm"
                variant="ghost"
                className="hover:text-status-error"
              />
              <div className="w-px h-4 bg-border/50 mx-0.5" />
              <PopoutButton
                onClick={() =>
                  openDetachedWindow('logs', source ? `agent=${encodeURIComponent(source)}` : undefined)
                }
                tooltip="Open in separate window"
                disabled={logsDetached}
              />
            </div>
          </header>

          <LogFilterBar
            searchQuery={searchQuery}
            onSearchChange={setSearchQuery}
            enabledLevels={enabledLevels}
            onToggleLevel={toggleLevel}
            fontSize={fontSize}
            onZoomIn={zoomIn}
            onZoomOut={zoomOut}
            onResetZoom={resetZoom}
            isMin={isMin}
            isMax={isMax}
            isDefault={isDefault}
            filteredCount={filteredLogs.length}
            totalCount={logs.length}
          >
            {traceFilter && (
              <button
                onClick={clearTraceFilter}
                title="Clear trace filter"
                className="flex items-center gap-1 h-6 px-2 text-[10px] font-mono rounded border bg-primary/10 text-primary border-primary/30 hover:bg-primary/15 transition-colors"
              >
                trace: {traceFilter.slice(0, 8)}
                <X size={9} />
              </button>
            )}
          </LogFilterBar>

          <LogStream
            logs={filteredLogs}
            isLoading={isLoading}
            error={error}
            hasActiveFilter={hasActiveFilter}
            onClearFilter={clearFilters}
            showSource={source == null}
            onTraceClick={(traceId) => navigate(`/traces?trace=${encodeURIComponent(traceId)}`)}
            fontSize={fontSize}
            containerRef={containerRef}
            header={
              source != null && source !== GATEWAY_LOG_SOURCE ? (
                <PersistedFromMarker serverName={source} signal="logs" />
              ) : undefined
            }
            emptyText="No logs yet"
          />
        </main>
      </WorkspaceShell>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Left rail — source navigator
// ---------------------------------------------------------------------------

interface SourceRailProps {
  compact: boolean;
  source: string | null;
  onSelectSource: (source: string | null) => void;
  serverNames: string[];
  totalCount: number;
  counts: Map<string, number>;
}

function SourceRail({ compact, source, onSelectSource, serverNames, totalCount, counts }: SourceRailProps) {
  return (
    <aside className="h-full flex flex-col bg-surface border-r border-border-subtle">
      <div className={cn('flex-shrink-0 px-3 border-b border-border-subtle/60', compact ? 'py-2' : 'py-3')}>
        <div className="text-[10px] font-medium text-text-muted/60 uppercase tracking-[0.3em]">sources</div>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-2 py-2 space-y-0.5">
        <SourcePill
          label="All sources"
          icon={Layers}
          count={totalCount}
          active={source == null}
          onClick={() => onSelectSource(null)}
        />
        <SourcePill
          label="Gateway"
          icon={Radio}
          count={counts.get(GATEWAY_LOG_SOURCE) ?? 0}
          active={source === GATEWAY_LOG_SOURCE}
          onClick={() => onSelectSource(GATEWAY_LOG_SOURCE)}
        />
        {serverNames.map((name) => (
          <SourcePill
            key={name}
            label={name}
            icon={Server}
            count={counts.get(name) ?? 0}
            active={source === name}
            onClick={() => onSelectSource(name)}
          />
        ))}
      </div>
    </aside>
  );
}

function SourcePill({
  label,
  icon: Icon,
  count,
  active,
  onClick,
}: {
  label: string;
  icon: LucideIcon;
  count: number;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      aria-current={active}
      className={cn(
        'group w-full flex items-center gap-2 px-3 py-1.5 rounded-md text-left transition-colors',
        active ? 'bg-primary/10 text-primary' : 'text-text-secondary hover:bg-surface-highlight/50 hover:text-text-primary',
      )}
    >
      <Icon size={13} className={active ? 'text-primary' : 'text-text-muted'} aria-hidden="true" />
      <span className={cn('flex-1 min-w-0 text-xs font-medium truncate', active && 'text-primary')}>{label}</span>
      <span
        className={cn(
          'flex-shrink-0 text-[10px] font-mono px-1.5 py-0.5 rounded tabular-nums',
          active ? 'bg-primary/15 text-primary' : 'bg-surface-elevated text-text-muted',
        )}
      >
        {count}
      </span>
    </button>
  );
}

export default LogsWorkspace;
