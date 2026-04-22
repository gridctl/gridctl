import { useEffect, useRef, useState, useCallback, Component, type ReactNode } from 'react';
import {
  Activity,
  AlertCircle,
  Maximize2,
  Minimize2,
  RefreshCw,
  X,
  Filter,
} from 'lucide-react';
import { cn } from '../lib/cn';
import { IconButton } from '../components/ui/IconButton';
import { ZoomControls } from '../components/log/ZoomControls';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { useTextZoom } from '../hooks/useTextZoom';
import { fetchTraces, fetchTraceDetail, fetchMCPServers } from '../lib/api';
import type { TraceSummary, TraceDetail } from '../lib/api';
import { TraceWaterfall } from '../components/traces/TraceWaterfall';
import { POLLING } from '../lib/constants';

interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

class DetachedErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="h-screen w-screen bg-background flex items-center justify-center">
          <div className="text-center p-8 max-w-md">
            <div className="p-4 rounded-xl bg-status-error/10 border border-status-error/20 inline-block mb-4">
              <AlertCircle size={32} className="text-status-error" />
            </div>
            <h1 className="text-lg text-status-error mb-2">Something went wrong</h1>
            <pre className="text-xs text-text-muted bg-surface p-4 rounded-lg overflow-auto max-h-32 mb-4">
              {this.state.error?.message}
            </pre>
            <button
              onClick={() => window.location.reload()}
              className="px-4 py-2 bg-primary text-background rounded-lg font-medium hover:bg-primary-light transition-colors"
            >
              Reload Window
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
  } catch {
    return iso;
  }
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function DetachedTracesPageContent() {
  const [traces, setTraces] = useState<TraceSummary[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [selectedTraceId, setSelectedTraceId] = useState<string | null>(null);
  const [traceDetail, setTraceDetail] = useState<TraceDetail | null>(null);
  const [isLoadingDetail, setIsLoadingDetail] = useState(false);
  const [serverFilter, setServerFilter] = useState('');
  const [servers, setServers] = useState<string[]>([]);
  const [errorsOnly, setErrorsOnly] = useState(false);
  const [search, setSearch] = useState('');
  const [isFullscreen, setIsFullscreen] = useState(false);
  const tableRef = useRef<HTMLDivElement>(null);

  useDetachedWindowSync('traces');

  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } = useTextZoom({
    storageKey: 'gridctl-traces-font-size',
    defaultSize: 11,
    minSize: 8,
    maxSize: 20,
    containerRef: tableRef,
  });

  const load = useCallback(async () => {
    try {
      const result = await fetchTraces({
        server: serverFilter || undefined,
        errors: errorsOnly || undefined,
        limit: 100,
      });
      setTraces(result.traces);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch traces');
    } finally {
      setIsLoading(false);
    }
  }, [serverFilter, errorsOnly]);

  // Initial load
  useEffect(() => {
    setIsLoading(true);
    load();
  }, [load]);

  // Auto-refresh
  useEffect(() => {
    const interval = window.setInterval(load, POLLING.STATUS);
    return () => clearInterval(interval);
  }, [load]);

  // Load deployed server names for the filter dropdown
  useEffect(() => {
    fetchMCPServers()
      .then((list) => setServers(list.map((s) => s.name).sort()))
      .catch(() => {});
  }, []);

  const selectTrace = useCallback(async (traceId: string | null) => {
    setSelectedTraceId(traceId);
    setTraceDetail(null);
    if (!traceId) return;
    setIsLoadingDetail(true);
    try {
      const detail = await fetchTraceDetail(traceId);
      setTraceDetail(detail);
    } catch {
      // ignore
    } finally {
      setIsLoadingDetail(false);
    }
  }, []);

  const toggleFullscreen = async () => {
    if (!document.fullscreenElement) {
      await document.documentElement.requestFullscreen();
      setIsFullscreen(true);
    } else {
      await document.exitFullscreen();
      setIsFullscreen(false);
    }
  };

  useEffect(() => {
    const handler = () => setIsFullscreen(!!document.fullscreenElement);
    document.addEventListener('fullscreenchange', handler);
    return () => document.removeEventListener('fullscreenchange', handler);
  }, []);

  const filteredTraces = search
    ? traces.filter(
        (t) =>
          t.traceId.toLowerCase().includes(search.toLowerCase()) ||
          t.operation.toLowerCase().includes(search.toLowerCase()) ||
          t.server.toLowerCase().includes(search.toLowerCase())
      )
    : traces;

  const hasFilters = serverFilter || errorsOnly || search;

  return (
    <div className="h-screen w-screen bg-background flex flex-col overflow-hidden">
      {/* Background grain */}
      <div
        className="fixed inset-0 pointer-events-none z-0 opacity-[0.015]"
        style={{
          backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noise'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noise)'/%3E%3C/svg%3E")`,
        }}
      />

      {/* Header */}
      <header className="h-12 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-b border-border/50 flex items-center justify-between px-4 z-10 relative">
        <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/30 to-transparent" />

        <div className="flex items-center gap-3">
          <div className="p-1.5 rounded-lg border bg-primary/10 border-primary/20">
            <Activity size={14} className="text-primary" />
          </div>
          <span className="text-sm font-semibold text-text-primary">Traces</span>
          <span className="flex items-center gap-1 text-[9px] text-status-running font-medium">
            <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse" />
            Live
          </span>
        </div>

        <div className="flex items-center gap-2">
          <ZoomControls
            fontSize={fontSize}
            onZoomIn={zoomIn}
            onZoomOut={zoomOut}
            onReset={resetZoom}
            isMin={isMin}
            isMax={isMax}
            isDefault={isDefault}
          />
          <div className="w-px h-4 bg-border/50" />
          <IconButton
            icon={RefreshCw}
            onClick={() => { setIsLoading(true); load(); }}
            tooltip="Refresh"
            size="sm"
            variant="ghost"
          />
          <div className="w-px h-4 bg-border/50" />
          <IconButton
            icon={isFullscreen ? Minimize2 : Maximize2}
            onClick={toggleFullscreen}
            tooltip={isFullscreen ? 'Exit Fullscreen' : 'Fullscreen'}
            size="sm"
            variant="ghost"
          />
        </div>
      </header>

      {/* Filters bar */}
      <div className="h-10 flex-shrink-0 bg-surface-elevated/20 border-b border-border/30 flex items-center gap-2 px-4">
        <input
          type="text"
          placeholder="Search traces…"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="h-6 px-2 text-[10px] bg-background/60 border border-border/40 rounded text-text-secondary placeholder:text-text-muted focus:outline-none focus:border-primary/50 w-48"
        />
        <select
          value={serverFilter}
          onChange={(e) => setServerFilter(e.target.value)}
          className="h-6 px-1.5 text-[10px] bg-background/60 border border-border/40 rounded text-text-secondary focus:outline-none focus:border-primary/50"
        >
          <option value="">All servers</option>
          {servers.map((s) => (
            <option key={s} value={s}>{s}</option>
          ))}
        </select>
        <button
          onClick={() => setErrorsOnly(!errorsOnly)}
          className={cn(
            'h-6 px-2 text-[10px] font-medium rounded border transition-colors flex items-center gap-1',
            errorsOnly
              ? 'bg-status-error/15 text-status-error border-status-error/30'
              : 'bg-background/60 text-text-muted border-border/40 hover:text-text-secondary'
          )}
        >
          <AlertCircle size={9} />
          Errors only
        </button>
        {hasFilters && (
          <button
            onClick={() => { setServerFilter(''); setErrorsOnly(false); setSearch(''); }}
            className="h-6 px-1.5 text-[10px] text-text-muted hover:text-text-secondary flex items-center gap-1 rounded hover:bg-surface-highlight/30 transition-colors"
          >
            <X size={9} />
            Clear
          </button>
        )}
      </div>

      {/* Content */}
      <main className="flex flex-1 min-h-0">
        {/* Trace list */}
        <div className={cn('flex flex-col min-h-0 border-r border-border/30', traceDetail ? 'w-[35%]' : 'w-full')}>
          {isLoading && filteredTraces.length === 0 && (
            <div className="p-4 space-y-2 animate-pulse">
              {[1, 2, 3, 4, 5].map((i) => (
                <div key={i} className="h-8 rounded bg-surface-elevated/60 border border-border/20" />
              ))}
            </div>
          )}

          {error && !isLoading && (
            <div className="flex flex-col items-center justify-center flex-1 gap-3">
              <AlertCircle size={24} className="text-status-error" />
              <span className="text-xs text-status-error">{error}</span>
              <button onClick={load} className="text-xs text-primary hover:underline">Retry</button>
            </div>
          )}

          {!isLoading && !error && filteredTraces.length === 0 && (
            <div className="flex flex-col items-center justify-center flex-1 gap-2 text-text-muted">
              <Activity size={32} className="text-text-muted/30" />
              <span className="text-sm">No traces yet</span>
              <span className="text-xs text-text-muted/60">
                {hasFilters ? 'No traces match your filters' : 'Traces appear after tool calls'}
              </span>
              {hasFilters && (
                <button
                  onClick={() => { setServerFilter(''); setErrorsOnly(false); setSearch(''); }}
                  className="text-xs text-primary hover:underline flex items-center gap-1"
                >
                  <Filter size={10} /> Clear filters
                </button>
              )}
            </div>
          )}

          {filteredTraces.length > 0 && (
            <div
              ref={tableRef}
              className="flex-1 overflow-y-auto scrollbar-dark min-h-0"
              style={{ '--text-zoom-size': `${fontSize}px` } as React.CSSProperties}
            >
              <table className="w-full">
                <thead className="sticky top-0 bg-surface/95 backdrop-blur-sm">
                  <tr className="border-b border-border/30">
                    <th className="px-4 py-2 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Time</th>
                    <th className="px-4 py-2 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Trace ID</th>
                    <th className="px-4 py-2 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Operation</th>
                    <th className="px-4 py-2 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Server</th>
                    <th className="px-4 py-2 text-right text-[9px] font-medium text-text-muted uppercase tracking-wider">Duration</th>
                    <th className="px-4 py-2 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {filteredTraces.map((trace) => {
                    const isSelected = selectedTraceId === trace.traceId;
                    return (
                      <tr
                        key={trace.traceId}
                        onClick={() => selectTrace(isSelected ? null : trace.traceId)}
                        className={cn(
                          'border-b border-border/15 cursor-pointer transition-colors',
                          isSelected ? 'bg-primary/5 border-l-2 border-l-primary' : 'hover:bg-surface-highlight/20'
                        )}
                      >
                        <td className="px-4 py-2 text-text-muted font-mono whitespace-nowrap text-zoom">{formatTime(trace.startTime)}</td>
                        <td className="px-4 py-2 font-mono text-text-secondary text-zoom">{trace.traceId.slice(0, 8)}</td>
                        <td className="px-4 py-2 text-text-primary truncate max-w-[200px] text-zoom">{trace.operation}</td>
                        <td className="px-4 py-2 text-text-secondary font-mono text-zoom">{trace.server}</td>
                        <td className="px-4 py-2 text-right text-text-secondary tabular-nums font-mono text-zoom">{formatDuration(trace.duration)}</td>
                        <td className="px-4 py-2">
                          <span
                            className={cn(
                              'px-1.5 py-0.5 text-[9px] font-medium rounded-full border',
                              trace.status === 'error'
                                ? 'bg-status-error/10 text-status-error border-status-error/20'
                                : 'bg-status-running/10 text-status-running border-status-running/20'
                            )}
                          >
                            {trace.status}
                          </span>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>

        {/* Waterfall panel */}
        {selectedTraceId && (
          <div className="flex-1 min-h-0 min-w-0">
            {isLoadingDetail && !traceDetail && (
              <div className="flex items-center justify-center h-full">
                <div className="w-8 h-8 rounded-full border-2 border-primary/30 border-t-primary animate-spin" />
              </div>
            )}
            {traceDetail && (
              <TraceWaterfall
                trace={traceDetail}
                onClose={() => selectTrace(null)}
              />
            )}
          </div>
        )}
      </main>

      {/* Footer */}
      <footer className="h-6 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-t border-border/50 flex items-center justify-between px-4 text-[10px] text-text-muted">
        <span>{traces.length > 0 ? `${traces.length} trace${traces.length !== 1 ? 's' : ''}` : 'No data'}</span>
        <span className="flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-text-muted animate-pulse" />
          Detached Window
        </span>
      </footer>
    </div>
  );
}

export function DetachedTracesPage() {
  return (
    <DetachedErrorBoundary>
      <DetachedTracesPageContent />
    </DetachedErrorBoundary>
  );
}
