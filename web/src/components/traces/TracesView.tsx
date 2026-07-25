import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Group, Panel, Separator, usePanelRef } from 'react-resizable-panels';
import {
  Activity,
  AlertCircle,
  Copy,
  Pause,
  Play,
  RefreshCw,
  ScrollText,
  SlidersHorizontal,
  X,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { isTextInputTarget } from '../../lib/dom';
import { SeparatorBody } from '../ui/SeparatorBody';
import { IconButton } from '../ui/IconButton';
import { ZoomControls } from '../ui/ZoomControls';
import { EmptyState } from '../ui/EmptyState';
import { copyWithToast } from '../ui/Toast';
import { formatRelativeTime } from '../../lib/time';
import { formatTotalDuration } from '../../lib/duration';
import {
  useTracesStore,
  isToolCallTrace,
  TRACES_TIME_RANGES,
  TRACES_TIME_RANGE_MS,
} from '../../stores/useTracesStore';
import { useUIStore } from '../../stores/useUIStore';
import { useTextZoom } from '../../hooks/useTextZoom';
import { useListNav } from '../../hooks/useListNav';
import { useDismiss } from '../../hooks/useDismiss';
import { useContainerWidth } from '../../hooks/useContainerWidth';
import { useWorkspaceLayout } from '../../hooks/useWorkspaceLayout';
import { TraceWaterfall } from './TraceWaterfall';
import { PersistedFromMarker } from '../telemetry/PersistedFromMarker';
import { POLLING } from '../../lib/constants';

function formatAbsoluteTime(iso: string): string {
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

const SEGMENTS = [
  { value: 'tool-calls', label: 'Tool calls' },
  { value: 'all', label: 'All' },
] as const;

interface TracesViewProps {
  /** Load + poll only while true (workspace mounted, tab visible, ...). */
  active: boolean;
  /** Server names for the filter dropdown. */
  servers: string[];
  /** When set, the waterfall header gets a "View logs" pivot. */
  onViewLogs?: (traceId: string) => void;
  /** When set, the waterfall header gets a "View metrics" pivot. */
  onViewMetrics?: (server: string) => void;
  /** Extra toolbar actions (e.g. the popout button), rendered rightmost. */
  toolbarExtra?: React.ReactNode;
}

// Shared traces surface — control bar, trace list, and waterfall — backed by
// useTracesStore. Mounted by the Traces workspace and the detached window
// (each browser window gets its own store instance, so detached state never
// bleeds into the main shell).
export function TracesView({ active, servers, onViewLogs, onViewMetrics, toolbarExtra }: TracesViewProps) {
  const traces = useTracesStore((s) => s.traces);
  const tracingEnabled = useTracesStore((s) => s.tracingEnabled);
  const bufferSize = useTracesStore((s) => s.bufferSize);
  const bufferCapacity = useTracesStore((s) => s.bufferCapacity);
  const isLoading = useTracesStore((s) => s.isLoading);
  const isPaused = useTracesStore((s) => s.isPaused);
  const setPaused = useTracesStore((s) => s.setPaused);
  const lastLoadedAt = useTracesStore((s) => s.lastLoadedAt);
  const error = useTracesStore((s) => s.error);
  const filters = useTracesStore((s) => s.filters);
  const setFilters = useTracesStore((s) => s.setFilters);
  const selectedTraceId = useTracesStore((s) => s.selectedTraceId);
  const traceDetail = useTracesStore((s) => s.traceDetail);
  const isLoadingDetail = useTracesStore((s) => s.isLoadingDetail);
  const detailError = useTracesStore((s) => s.detailError);
  const selectTrace = useTracesStore((s) => s.selectTrace);
  const loadTraces = useTracesStore((s) => s.loadTraces);
  const setTracesPrefs = useUIStore((s) => s.setTracesPrefs);

  const containerRef = useRef<HTMLDivElement>(null);
  const intervalRef = useRef<number | null>(null);
  const [filtersOpen, setFiltersOpen] = useState(false);
  const filtersRef = useDismiss<HTMLDivElement>(filtersOpen, () => setFiltersOpen(false));
  // Keyboard highlight, tracked by trace ID so Live refreshes keep it sticky.
  const [navId, setNavId] = useState<string | null>(null);
  // Span selection lives here (not in TraceWaterfall) so Escape can close the
  // detail drawer before the waterfall. Stored with its owning trace and
  // derived against the rendered detail, so it is non-null exactly when the
  // drawer is visible: a stale selection can never eat an Escape press.
  const [spanSel, setSpanSel] = useState<{ traceId: string; spanId: string } | null>(null);
  const selectedSpanId =
    spanSel &&
    spanSel.traceId === selectedTraceId &&
    traceDetail?.traceId === spanSel.traceId &&
    traceDetail.spans.some((s) => s.spanId === spanSel.spanId)
      ? spanSel.spanId
      : null;

  // List | waterfall split. Persisted per panel combination, so the stage A
  // single-panel layout never pollutes the stage B split.
  const listPanelRef = usePanelRef();
  const { defaultLayout, onLayoutChanged } = useWorkspaceLayout({
    workspace: 'traces',
    key: 'split',
    panelIds: selectedTraceId ? ['trace-list', 'trace-waterfall'] : ['trace-list'],
  });

  // Column density derives from the list pane's real width, not from selection
  // state; a zero/unknown width (first paint, jsdom) keeps the full column set.
  const listPaneRef = useRef<HTMLDivElement>(null);
  const { width: listWidth } = useContainerWidth(listPaneRef);
  const compact = listWidth > 0 && listWidth < 480;

  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } = useTextZoom({
    storageKey: 'gridctl-traces-font-size',
    defaultSize: 11,
    minSize: 8,
    maxSize: 20,
    containerRef,
  });

  // Apply persisted segment/server prefs once on mount. Runs before the
  // workspace's URL-sync effect (child effects fire first), so URL params
  // still win when present. Write-back happens only at user gesture sites
  // (setSegment/setServer below), so a shared ?seg= link applies for the
  // visit without permanently replacing the user's own saved defaults.
  useEffect(() => {
    const prefs = useUIStore.getState().tracesPrefs;
    setFilters({ segment: prefs.segment, server: prefs.server });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const setSegment = useCallback(
    (segment: 'tool-calls' | 'all') => {
      setFilters({ segment });
      setTracesPrefs({ segment });
    },
    [setFilters, setTracesPrefs]
  );
  const setServer = useCallback(
    (server: string) => {
      setFilters({ server });
      setTracesPrefs({ server });
    },
    [setFilters, setTracesPrefs]
  );

  const load = useCallback(() => {
    loadTraces();
  }, [loadTraces]);

  // Initial load + reload when activated or a server-side filter changes.
  // filters.search is deliberately excluded: it only filters client-side, so
  // reloading per keystroke would hammer the API and flash the skeleton.
  useEffect(() => {
    if (!active) return;
    load();
  }, [active, filters.server, filters.errorsOnly, filters.minDuration, load]);

  // Auto-refresh while active and not paused. Pause freezes the display
  // only — the gateway keeps collecting; resuming just reloads.
  useEffect(() => {
    if (!active || isPaused) {
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
  }, [active, isPaused, load]);

  // Client-side filter pipeline: time range and search first, then the
  // tool-call segment (so the "infra hidden" count reflects the same window
  // the user is looking at).
  const { displayTraces, infraHidden } = useMemo(() => {
    let base = traces;
    const rangeMs = TRACES_TIME_RANGE_MS[filters.timeRange];
    if (rangeMs != null && lastLoadedAt > 0) {
      // Anchored to the last completed load, not the render clock: keeps the
      // memo pure and freezes the window while paused.
      const cutoff = lastLoadedAt - rangeMs;
      base = base.filter((t) => new Date(t.startTime).getTime() >= cutoff);
    }
    if (filters.search) {
      const q = filters.search.toLowerCase();
      base = base.filter(
        (t) =>
          t.traceId.toLowerCase().includes(q) ||
          t.operation.toLowerCase().includes(q) ||
          t.server.toLowerCase().includes(q) ||
          t.tool.toLowerCase().includes(q) ||
          t.client.toLowerCase().includes(q)
      );
    }
    if (filters.segment === 'all') {
      return { displayTraces: base, infraHidden: 0 };
    }
    const toolCalls = base.filter(isToolCallTrace);
    return { displayTraces: toolCalls, infraHidden: base.length - toolCalls.length };
  }, [traces, filters.timeRange, filters.search, filters.segment, lastLoadedAt]);

  const maxDuration = useMemo(
    () => displayTraces.reduce((max, t) => Math.max(max, t.duration), 0),
    [displayTraces]
  );

  const hasFilters =
    filters.server || filters.errorsOnly || filters.minDuration != null || filters.search || filters.timeRange !== 'all';
  const filtersPopoverActive = filters.minDuration != null || filters.timeRange !== 'all';
  const clearFilters = () => {
    setFilters({ server: '', errorsOnly: false, minDuration: null, search: '', timeRange: 'all' });
    setTracesPrefs({ server: '' });
  };

  // Keyboard navigation: j/k or arrows move the highlight, Enter opens the
  // waterfall, Escape closes it. Highlight index derives from navId so a
  // Live refresh reordering rows never teleports the cursor; a navId that a
  // filter change removed from view goes inert (no highlight, Enter no-op)
  // instead of opening an off-screen trace.
  const navIndex = useMemo(
    () => (navId ? displayTraces.findIndex((t) => t.traceId === navId) : -1),
    [displayTraces, navId]
  );
  const activeNavId = navIndex >= 0 ? navId : null;
  useListNav({
    itemCount: displayTraces.length,
    selectedIndex: navIndex < 0 ? 0 : navIndex,
    setSelectedIndex: (i) => {
      const trace = displayTraces[i];
      if (!trace) return;
      setNavId(trace.traceId);
      document.getElementById(`trace-row-${trace.traceId}`)?.scrollIntoView({ block: 'nearest' });
    },
    onEnter: () => {
      if (activeNavId) selectTrace(activeNavId);
    },
    onEscape: () => {
      // Ladder: span detail closes first, then the waterfall. An Escape with
      // nothing selected is left to the browser (e.g. exiting fullscreen in
      // the detached window).
      if (selectedSpanId) setSpanSel(null);
      else if (selectedTraceId) selectTrace(null);
    },
    enabled: active,
  });

  // Bind the span to the trace whose waterfall was actually clicked (the
  // rendered detail), not selectedTraceId: an out-of-order detail fetch can
  // briefly render trace A's waterfall while B is selected.
  const selectSpan = useCallback(
    (spanId: string | null) => {
      const traceId = useTracesStore.getState().traceDetail?.traceId;
      setSpanSel(spanId && traceId ? { traceId, spanId } : null);
    },
    []
  );

  // '[' toggles list collapse while a waterfall is open. Same contract
  // as WorkspaceShell: plain keypress only, suppressed while typing.
  useEffect(() => {
    if (!active || !selectedTraceId) return;
    function handler(e: KeyboardEvent) {
      if (e.key !== '[') return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (isTextInputTarget(e.target)) return;
      const panel = listPanelRef.current;
      if (!panel) return;
      e.preventDefault();
      if (panel.isCollapsed()) panel.expand();
      else panel.collapse();
    }
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [active, selectedTraceId, listPanelRef]);

  const bufferPressure = bufferCapacity > 0 ? bufferSize / bufferCapacity : 0;

  const selectedSummary = displayTraces.find((t) => t.traceId === selectedTraceId)
    ?? traces.find((t) => t.traceId === selectedTraceId);

  return (
    <div className="flex flex-col h-full">
      {/* Control bar */}
      <div className="flex items-center justify-between px-3 h-9 flex-shrink-0 border-b border-border/30 bg-surface-elevated/20 gap-2">
        <div className="flex items-center gap-2 min-w-0 flex-1">
          {/* Segment: tool calls vs everything in the buffer */}
          <div className="flex items-center bg-background/60 rounded-md border border-border/40 overflow-hidden flex-shrink-0">
            {SEGMENTS.map((seg) => (
              <button
                key={seg.value}
                onClick={() => setSegment(seg.value)}
                aria-pressed={filters.segment === seg.value}
                className={cn(
                  'px-2 py-1 text-[10px] font-medium transition-colors',
                  filters.segment === seg.value
                    ? 'bg-primary/15 text-primary'
                    : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40'
                )}
              >
                {seg.label}
              </button>
            ))}
          </div>

          {/* Server filter */}
          <select
            value={filters.server}
            onChange={(e) => setServer(e.target.value)}
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
            aria-pressed={filters.errorsOnly}
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

          {/* Search */}
          <input
            type="text"
            placeholder="Search traces…"
            value={filters.search}
            onChange={(e) => setFilters({ search: e.target.value })}
            className="h-6 px-2 text-[10px] bg-background/60 border border-border/40 rounded text-text-secondary placeholder:text-text-muted focus:outline-none focus:border-primary/50 w-36"
          />

          {/* Clear filters */}
          {hasFilters && (
            <button
              onClick={clearFilters}
              className="h-6 px-1.5 text-[10px] text-text-muted hover:text-text-secondary transition-colors flex items-center gap-1 rounded hover:bg-surface-highlight/30"
            >
              <X size={9} />
              Clear
            </button>
          )}

          {/* Counts, right-aligned within the left cluster */}
          <span className="ml-auto text-[10px] text-text-muted font-mono whitespace-nowrap flex-shrink-0 hidden sm:block">
            {displayTraces.length} matching
            {infraHidden > 0 && <span className="text-text-muted/60"> · {infraHidden} infra hidden</span>}
            {bufferCapacity > 0 && (
              <span
                className={cn(bufferPressure >= 0.9 ? 'text-status-pending' : 'text-text-muted/60')}
                title={`Ring buffer: ${bufferSize} of ${bufferCapacity} traces (gateway.tracing.max_traces)`}
              >
                {' '}· {bufferSize}/{bufferCapacity}
              </span>
            )}
          </span>
        </div>

        <div className="flex items-center gap-1 flex-shrink-0">
          {/* Live / paused state */}
          {isPaused ? (
            <span className="text-[10px] px-2 py-0.5 bg-status-pending/15 text-status-pending rounded-full font-medium border border-status-pending/20">
              Paused
            </span>
          ) : (
            <span className="flex items-center gap-1 text-[9px] text-status-running font-medium">
              <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse motion-reduce:animate-none" />
              Live
            </span>
          )}
          <IconButton
            icon={isPaused ? Play : Pause}
            onClick={() => setPaused(!isPaused)}
            tooltip={isPaused ? 'Resume live updates' : 'Pause live updates'}
            pressed={isPaused}
            size="sm"
            variant="ghost"
            className={isPaused ? 'text-status-running hover:text-status-running' : ''}
          />

          {/* Filters popover: min duration + time range presets */}
          <div ref={filtersRef} className="relative">
            <IconButton
              icon={SlidersHorizontal}
              onClick={() => setFiltersOpen((v) => !v)}
              tooltip="More filters"
              size="sm"
              variant="ghost"
              className={cn(filtersPopoverActive && 'ring-1 ring-primary/30 rounded')}
            />
            {filtersOpen && (
              <div
                className={cn(
                  'absolute right-0 top-full mt-1 z-50 w-52 p-2',
                  'rounded-lg border border-border bg-surface-elevated/95',
                  'backdrop-blur-xl shadow-bevel animate-fade-in-scale'
                )}
              >
                <div className="px-1 py-1 text-[9px] uppercase tracking-[0.18em] text-text-muted/70">
                  Filters
                </div>
                <label className="flex items-center justify-between gap-2 px-1 py-1.5 text-[10px] text-text-secondary">
                  <span>Min duration</span>
                  <span className="flex items-center gap-1">
                    <input
                      type="number"
                      placeholder="0"
                      value={filters.minDuration ?? ''}
                      onChange={(e) =>
                        setFilters({ minDuration: e.target.value ? Number(e.target.value) : null })
                      }
                      className="h-6 px-2 text-[10px] bg-background/60 border border-border/40 rounded text-text-secondary placeholder:text-text-muted focus:outline-none focus:border-primary/50 w-16 text-right"
                      min={0}
                    />
                    <span className="text-text-muted">ms</span>
                  </span>
                </label>
                <div className="flex items-center justify-between gap-2 px-1 py-1.5">
                  <span className="text-[10px] text-text-secondary">Time range</span>
                  <div className="flex items-center bg-background/60 rounded-md border border-border/40 overflow-hidden">
                    {TRACES_TIME_RANGES.map((range) => (
                      <button
                        key={range.value}
                        onClick={() => setFilters({ timeRange: range.value })}
                        aria-pressed={filters.timeRange === range.value}
                        className={cn(
                          'px-1.5 py-0.5 text-[9px] font-medium transition-colors',
                          filters.timeRange === range.value
                            ? 'bg-primary/15 text-primary'
                            : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40'
                        )}
                      >
                        {range.label}
                      </button>
                    ))}
                  </div>
                </div>
                {(filtersPopoverActive || hasFilters) && (
                  <button
                    onClick={clearFilters}
                    className="w-full mt-1 px-1 py-1 text-[10px] text-text-muted hover:text-text-secondary transition-colors flex items-center gap-1 rounded hover:bg-surface-highlight/30"
                  >
                    <X size={9} /> Clear all filters
                  </button>
                )}
              </div>
            )}
          </div>

          <div className="w-px h-4 bg-border/50 mx-0.5" />
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
          {toolbarExtra && (
            <>
              <div className="w-px h-4 bg-border/50 mx-0.5" />
              {toolbarExtra}
            </>
          )}
        </div>
      </div>

      {/* Body */}
      <Group
        orientation="horizontal"
        className="flex-1 min-h-0"
        defaultLayout={defaultLayout}
        onLayoutChanged={onLayoutChanged}
      >
        {/* Trace list */}
        <Panel
          id="trace-list"
          defaultSize={selectedTraceId ? '34' : '100'}
          minSize={240}
          collapsible
          collapsedSize={0}
          panelRef={listPanelRef}
        >
          <div ref={listPaneRef} className="flex flex-col h-full min-h-0 overflow-hidden">
            {/* Loading skeleton */}
            {isLoading && displayTraces.length === 0 && (
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

            {/* Empty states */}
            {!isLoading && !error && displayTraces.length === 0 && (
              !tracingEnabled ? (
                <EmptyState
                  icon={Activity}
                  title="Tracing is disabled"
                  description={
                    <>Enable <code className="font-mono text-[10px]">gateway.tracing</code> in stack.yaml to record tool-call traces.</>
                  }
                />
              ) : infraHidden > 0 ? (
                <EmptyState
                  icon={Activity}
                  title="No tool-call traces yet"
                  description={`${infraHidden} infrastructure ${infraHidden === 1 ? 'trace is' : 'traces are'} hidden.`}
                  action={
                    <button
                      onClick={() => setSegment('all')}
                      className="text-[10px] text-primary hover:underline"
                    >
                      Show all
                    </button>
                  }
                />
              ) : hasFilters ? (
                <EmptyState
                  icon={Activity}
                  title="No traces match your filters"
                  action={
                    <button onClick={clearFilters} className="text-[10px] text-primary hover:underline">
                      Clear filters
                    </button>
                  }
                />
              ) : (
                <EmptyState
                  icon={Activity}
                  title="No traces yet"
                  description="Traces appear after tool calls"
                />
              )
            )}

            {/* Table */}
            {displayTraces.length > 0 && (
              <>
                <div
                  ref={containerRef}
                  className="flex-1 overflow-y-auto scrollbar-dark min-h-0"
                  style={{ '--text-zoom-size': `${fontSize}px` } as React.CSSProperties}
                >
                  {/* Provenance boundary — top-of-list marker when any server
                      has traces persistence enabled with files on disk. */}
                  <PersistedFromMarker serverName={null} signal="traces" />
                  <table
                    role="grid"
                    aria-label="Traces"
                    tabIndex={0}
                    aria-activedescendant={activeNavId ? `trace-row-${activeNavId}` : undefined}
                    className="w-full focus:outline-none"
                  >
                    <thead className="sticky top-0 z-10 bg-surface-elevated/95 backdrop-blur-sm">
                      <tr className="border-b border-border/30">
                        <th className="px-3 py-1.5 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Time</th>
                        <th className="px-3 py-1.5 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Tool</th>
                        {!compact && (
                          <>
                            <th className="px-3 py-1.5 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Client</th>
                            <th className="px-3 py-1.5 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Server</th>
                          </>
                        )}
                        <th className="px-3 py-1.5 text-right text-[9px] font-medium text-text-muted uppercase tracking-wider">Duration</th>
                        {!compact && (
                          <th className="px-3 py-1.5 text-right text-[9px] font-medium text-text-muted uppercase tracking-wider">Spans</th>
                        )}
                        <th className="px-3 py-1.5 text-left text-[9px] font-medium text-text-muted uppercase tracking-wider">Status</th>
                        {!compact && <th className="w-8" aria-label="Row actions" />}
                      </tr>
                    </thead>
                    <tbody>
                      {displayTraces.map((trace) => {
                        const isSelected = selectedTraceId === trace.traceId;
                        const isHighlighted = activeNavId === trace.traceId;
                        const heat = maxDuration > 0 ? trace.duration / maxDuration : 0;
                        return (
                          <tr
                            key={trace.traceId}
                            id={`trace-row-${trace.traceId}`}
                            aria-selected={isSelected}
                            onClick={() => {
                              setNavId(trace.traceId);
                              selectTrace(isSelected ? null : trace.traceId);
                            }}
                            className={cn(
                              'group border-b border-border/15 cursor-pointer transition-colors',
                              isSelected
                                ? 'bg-primary/5 border-l-2 border-l-primary'
                                : isHighlighted
                                  ? 'bg-surface-highlight/30'
                                  : 'hover:bg-surface-highlight/20'
                            )}
                          >
                            <td
                              className="px-3 py-1.5 text-text-muted font-mono whitespace-nowrap text-zoom"
                              title={formatAbsoluteTime(trace.startTime)}
                            >
                              {formatRelativeTime(new Date(trace.startTime))}
                            </td>
                            <td
                              className="px-3 py-1.5 text-text-primary truncate max-w-[200px] text-zoom"
                              title={`${trace.operation} · ${trace.traceId}`}
                            >
                              {trace.tool || trace.operation}
                            </td>
                            {!compact && (
                              <>
                                <td className="px-3 py-1.5 text-text-secondary font-mono truncate max-w-[120px] text-zoom" title={trace.client || undefined}>
                                  {trace.client || '–'}
                                </td>
                                <td className="px-3 py-1.5 text-text-secondary font-mono text-zoom">{trace.server}</td>
                              </>
                            )}
                            <td className="px-3 py-1.5 text-right whitespace-nowrap text-zoom">
                              <span className="inline-flex items-center gap-1.5 justify-end">
                                <span
                                  aria-hidden="true"
                                  className={cn(
                                    'inline-block h-1.5 rounded-full',
                                    trace.status === 'error' ? 'bg-status-error/50' : 'bg-primary/40'
                                  )}
                                  style={{ width: `${Math.max(2, Math.round(heat * 40))}px` }}
                                />
                                <span className="text-text-secondary tabular-nums font-mono">
                                  {formatTotalDuration(trace.duration)}
                                </span>
                              </span>
                            </td>
                            {!compact && (
                              <td className="px-3 py-1.5 text-right text-text-muted tabular-nums text-zoom">{trace.spanCount}</td>
                            )}
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
                            {!compact && (
                              <td className="px-1 py-1.5">
                                <button
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    copyWithToast(trace.traceId, 'Trace ID');
                                  }}
                                  title={`Copy trace ID ${trace.traceId.slice(0, 8)}…`}
                                  className="opacity-0 group-hover:opacity-100 focus:opacity-100 p-1 rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-all"
                                >
                                  <Copy size={11} />
                                </button>
                              </td>
                            )}
                          </tr>
                        );
                      })}
                    </tbody>
                  </table>
                </div>
                {/* Keyboard hint strip */}
                <div className="flex items-center gap-2 px-3 h-5 flex-shrink-0 border-t border-border/20 text-[9px] text-text-muted/60 select-none">
                  <span><kbd className="font-mono">j</kbd>/<kbd className="font-mono">k</kbd> navigate</span>
                  <span><kbd className="font-mono">↵</kbd> open</span>
                  <span><kbd className="font-mono">esc</kbd> close</span>
                  {selectedTraceId && <span><kbd className="font-mono">[</kbd> list</span>}
                </div>
              </>
            )}
          </div>
        </Panel>

        {/* Waterfall panel */}
        {selectedTraceId && (
          <>
            <Separator id="sep-trace-list" className="group/separator relative w-1.5 select-none">
              <SeparatorBody orientation="vertical" />
            </Separator>
            <Panel id="trace-waterfall" defaultSize="66" minSize={280}>
              <div className="h-full min-h-0 min-w-0">
                {isLoadingDetail && !traceDetail && (
                  <div className="flex items-center justify-center h-full">
                    <div className="w-6 h-6 rounded-full border-2 border-primary/30 border-t-primary animate-spin" />
                  </div>
                )}
                {detailError && (
                  /not found/i.test(detailError) ? (
                    <EmptyState
                      icon={Activity}
                      title="Trace no longer in buffer"
                      description="The ring buffer evicted this trace."
                      action={
                        <button
                          onClick={() => selectTrace(null)}
                          className="text-[10px] text-primary hover:underline"
                        >
                          Clear selection
                        </button>
                      }
                    />
                  ) : (
                    <div className="flex flex-col items-center justify-center h-full gap-2 text-xs">
                      <AlertCircle size={16} className="text-status-error" />
                      <span className="text-status-error">{detailError}</span>
                    </div>
                  )
                )}
                {traceDetail && (
                  <TraceWaterfall
                    trace={traceDetail}
                    selectedSpanId={selectedSpanId}
                    onSelectSpan={selectSpan}
                    onClose={() => {
                      // The header X means "close everything": drop the span too so
                      // re-selecting this trace does not resurrect the drawer.
                      setSpanSel(null);
                      selectTrace(null);
                    }}
                    actions={
                      <>
                        {onViewLogs && (
                          <button
                            onClick={() => onViewLogs(traceDetail.traceId)}
                            title="View logs for this trace"
                            className="flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors"
                          >
                            <ScrollText size={11} />
                            View logs
                          </button>
                        )}
                        {onViewMetrics && selectedSummary?.server && (
                          <button
                            onClick={() => onViewMetrics(selectedSummary.server)}
                            title={`View metrics for ${selectedSummary.server}`}
                            className="flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-medium text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors"
                          >
                            <Activity size={11} />
                            View metrics
                          </button>
                        )}
                      </>
                    }
                  />
                )}
              </div>
            </Panel>
          </>
        )}
      </Group>
    </div>
  );
}
