import { useEffect, useRef, useCallback, useMemo } from 'react';
import { Activity, AlertCircle, RefreshCw, X, Filter } from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { PopoutButton } from '../ui/PopoutButton';
import { ZoomControls } from '../log/ZoomControls';
import { useUIStore } from '../../stores/useUIStore';
import { useTracesStore } from '../../stores/useTracesStore';
import { useStackStore } from '../../stores/useStackStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { useTextZoom } from '../../hooks/useTextZoom';
import { TraceWaterfall } from './TraceWaterfall';
import { POLLING } from '../../lib/constants';

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

export function TracesTab() {
  const bottomPanelOpen = useUIStore((s) => s.bottomPanelOpen);
  const bottomPanelTab = useUIStore((s) => s.bottomPanelTab);
  const tracesDetached = useUIStore((s) => s.tracesDetached);
  const { openDetachedWindow } = useWindowManager();

  const traces = useTracesStore((s) => s.traces);
  const isLoading = useTracesStore((s) => s.isLoading);
  const error = useTracesStore((s) => s.error);
  const filters = useTracesStore((s) => s.filters);
  const setFilters = useTracesStore((s) => s.setFilters);
  const selectedTraceId = useTracesStore((s) => s.selectedTraceId);
  const traceDetail = useTracesStore((s) => s.traceDetail);
  const isLoadingDetail = useTracesStore((s) => s.isLoadingDetail);
  const detailError = useTracesStore((s) => s.detailError);
  const selectTrace = useTracesStore((s) => s.selectTrace);
  const loadTraces = useTracesStore((s) => s.loadTraces);

  const containerRef = useRef<HTMLDivElement>(null);
  const intervalRef = useRef<number | null>(null);
  const isVisible = bottomPanelOpen && bottomPanelTab === 'traces';

  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } = useTextZoom({
    storageKey: 'gridctl-traces-font-size',
    defaultSize: 11,
    minSize: 8,
    maxSize: 20,
    containerRef,
  });

  const load = useCallback(() => {
    loadTraces();
  }, [loadTraces]);

  // Initial load + reload when visible
  useEffect(() => {
    if (!isVisible) return;
    load();
  }, [isVisible, load]);

  // Auto-refresh every 3 seconds
  useEffect(() => {
    if (!isVisible) {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      return;
    }
    intervalRef.current = window.setInterval(load, POLLING.STATUS);
    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [isVisible, load]);

  // Reload when filters change
  useEffect(() => {
    if (!isVisible) return;
    load();
  }, [filters, load, isVisible]);

  // Server list from deployed MCP servers (always populated when gateway is connected)
  const mcpServers = useStackStore((s) => s.mcpServers);
  const servers = useMemo(() => mcpServers.map((s) => s.name).sort(), [mcpServers]);

  // Client-side search filter
  const filteredTraces = useMemo(() => {
    if (!filters.search) return traces;
    const q = filters.search.toLowerCase();
    return traces.filter(
      (t) =>
        t.traceId.toLowerCase().includes(q) ||
        t.operation.toLowerCase().includes(q) ||
        t.server.toLowerCase().includes(q)
    );
  }, [traces, filters.search]);

  const hasFilters = filters.server || filters.errorsOnly || filters.minDuration != null || filters.search;

  return (
    <div className="flex flex-col h-full">
      {/* Control bar */}
      <div className="flex items-center justify-between px-3 h-9 flex-shrink-0 border-b border-border/30 bg-surface-elevated/20 gap-2">
        <div className="flex items-center gap-2 min-w-0 flex-1">
          {/* Search */}
          <input
            type="text"
            placeholder="Search traces…"
            value={filters.search}
            onChange={(e) => setFilters({ search: e.target.value })}
            className="h-6 px-2 text-[10px] bg-background/60 border border-border/40 rounded text-text-secondary placeholder:text-text-muted focus:outline-none focus:border-primary/50 w-36"
          />

          {/* Server filter */}
          <select
            value={filters.server}
            onChange={(e) => setFilters({ server: e.target.value })}
            className="h-6 px-1.5 text-[10px] bg-background/60 border border-border/40 rounded text-text-secondary focus:outline-none focus:border-primary/50 max-w-[120px]"
          >
            <option value="">All servers</option>
            {servers.map((s) => (
              <option key={s} value={s}>{s}</option>
            ))}
          </select>

          {/* Errors toggle */}
          <button
            onClick={() => setFilters({ errorsOnly: !filters.errorsOnly })}
            className={cn(
              'h-6 px-2 text-[10px] font-medium rounded border transition-colors flex items-center gap-1',
              filters.errorsOnly
                ? 'bg-status-error/15 text-status-error border-status-error/30'
                : 'bg-background/60 text-text-muted border-border/40 hover:text-text-secondary hover:border-border/60'
            )}
          >
            <AlertCircle size={9} />
            Errors
          </button>

          {/* Min duration */}
          <div className="flex items-center gap-1">
            <input
              type="number"
              placeholder="Min ms"
              value={filters.minDuration ?? ''}
              onChange={(e) =>
                setFilters({ minDuration: e.target.value ? Number(e.target.value) : null })
              }
              className="h-6 px-2 text-[10px] bg-background/60 border border-border/40 rounded text-text-secondary placeholder:text-text-muted focus:outline-none focus:border-primary/50 w-16"
              min={0}
            />
          </div>

          {/* Clear filters */}
          {hasFilters && (
            <button
              onClick={() => setFilters({ server: '', errorsOnly: false, minDuration: null, search: '' })}
              className="h-6 px-1.5 text-[10px] text-text-muted hover:text-text-secondary transition-colors flex items-center gap-1 rounded hover:bg-surface-highlight/30"
            >
              <X size={9} />
              Clear
            </button>
          )}

          {/* Live indicator */}
          <span className="flex items-center gap-1 text-[9px] text-status-running font-medium flex-shrink-0">
            <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse" />
            Live
          </span>
        </div>

        <div className="flex items-center gap-1 flex-shrink-0">
          <ZoomControls
            fontSize={fontSize}
            onZoomIn={zoomIn}
            onZoomOut={zoomOut}
            onReset={resetZoom}
            isMin={isMin}
            isMax={isMax}
            isDefault={isDefault}
          />
          <div className="w-px h-4 bg-border/50 mx-0.5" />
          <IconButton
            icon={RefreshCw}
            onClick={load}
            tooltip="Refresh"
            size="sm"
            variant="ghost"
          />
          <div className="w-px h-4 bg-border/50 mx-0.5" />
          <PopoutButton
            onClick={() => openDetachedWindow('traces')}
            disabled={tracesDetached}
          />
        </div>
      </div>

      {/* Body */}
      <div className="flex flex-1 min-h-0">
        {/* Trace list */}
        <div className={cn('flex flex-col min-h-0', selectedTraceId && traceDetail ? 'w-[40%] border-r border-border/30' : 'w-full')}>
          {/* Loading skeleton */}
          {isLoading && filteredTraces.length === 0 && (
            <div className="p-3 space-y-2 animate-pulse">
              {[1, 2, 3].map((i) => (
                <div key={i} className="h-7 rounded bg-surface-elevated/60 border border-border/20" />
              ))}
            </div>
          )}

          {/* Error */}
          {error && !isLoading && (
            <div className="flex flex-col items-center justify-center flex-1 gap-2 text-xs">
              <AlertCircle size={20} className="text-status-error" />
              <span className="text-status-error">{error}</span>
              <button onClick={load} className="text-primary hover:underline text-xs">Retry</button>
            </div>
          )}

          {/* Empty state */}
          {!isLoading && !error && filteredTraces.length === 0 && (
            <div className="flex flex-col items-center justify-center flex-1 gap-2 text-text-muted">
              <Activity size={24} className="text-text-muted/30" />
              <span className="text-xs">No traces yet</span>
              <span className="text-[10px] text-text-muted/60">
                {hasFilters ? 'No traces match your filters' : 'Traces appear after tool calls'}
              </span>
              {hasFilters && (
                <button
                  onClick={() => setFilters({ server: '', errorsOnly: false, minDuration: null, search: '' })}
                  className="text-[10px] text-primary hover:underline flex items-center gap-1"
                >
                  <Filter size={9} /> Clear filters
                </button>
              )}
            </div>
          )}

          {/* Table */}
          {filteredTraces.length > 0 && (
            <div
              ref={containerRef}
              className="flex-1 overflow-y-auto scrollbar-dark min-h-0"
              style={{ '--text-zoom-size': `${fontSize}px` } as React.CSSProperties}
            >
              <table className="w-full">
                <thead className="sticky top-0 z-10 bg-surface-elevated/95 backdrop-blur-sm">
                  <tr className="border-b border-border/30">
                    <th className="px-3 py-1.5 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Time</th>
                    <th className="px-3 py-1.5 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Trace ID</th>
                    <th className="px-3 py-1.5 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Operation</th>
                    <th className="px-3 py-1.5 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Server</th>
                    <th className="px-3 py-1.5 text-right text-[9px] font-medium text-text-muted uppercase tracking-wider">Duration</th>
                    <th className="px-3 py-1.5 text-right text-[9px] font-medium text-text-muted uppercase tracking-wider">Spans</th>
                    <th className="px-3 py-1.5 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Status</th>
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
                          isSelected
                            ? 'bg-primary/5 border-l-2 border-l-primary'
                            : 'hover:bg-surface-highlight/20'
                        )}
                      >
                        <td className="px-3 py-1.5 text-text-muted font-mono whitespace-nowrap text-zoom">{formatTime(trace.startTime)}</td>
                        <td className="px-3 py-1.5 font-mono text-text-secondary text-zoom">{trace.traceId.slice(0, 8)}</td>
                        <td className="px-3 py-1.5 text-text-primary truncate max-w-[160px] text-zoom">{trace.operation}</td>
                        <td className="px-3 py-1.5 text-text-secondary font-mono text-zoom">{trace.server}</td>
                        <td className="px-3 py-1.5 text-right text-text-secondary tabular-nums font-mono text-zoom">{formatDuration(trace.duration)}</td>
                        <td className="px-3 py-1.5 text-right text-text-muted tabular-nums text-zoom">{trace.spanCount}</td>
                        <td className="px-3 py-1.5">
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
                <div className="w-6 h-6 rounded-full border-2 border-primary/30 border-t-primary animate-spin" />
              </div>
            )}
            {detailError && (
              <div className="flex flex-col items-center justify-center h-full gap-2 text-xs">
                <AlertCircle size={16} className="text-status-error" />
                <span className="text-status-error">{detailError}</span>
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
      </div>
    </div>
  );
}
