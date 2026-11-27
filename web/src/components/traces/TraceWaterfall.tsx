import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Group, Panel, Separator } from 'react-resizable-panels';
import { X, AlertCircle, Copy, Link2, Download } from 'lucide-react';
import { cn } from '../../lib/cn';
import { SpanDetail } from './SpanDetail';
import { SeparatorBody } from '../ui/SeparatorBody';
import { copyWithToast, showToast } from '../ui/Toast';
import { fetchTraceOTLP } from '../../lib/api';
import { formatDuration, formatTotalDuration } from '../../lib/duration';
import { computeSelfTimes, computeCriticalPath, preorderSpanRows } from '../../lib/traceMath';
import { useWorkspaceLayout } from '../../hooks/useWorkspaceLayout';
import { releasePointerCapture } from '../../lib/pointerCapture';
import {
  useUIStore,
  TRACES_NAME_COL_MIN_PCT,
  TRACES_NAME_COL_MAX_PCT,
} from '../../stores/useUIStore';
import type { SpanInterval } from '../../lib/traceMath';
import type { TraceDetail, Span } from '../../lib/api';

// Horizontal padding (px-3) on the header and row boxes; the name-column
// percentage resolves against the padded content box, so the drag math must
// subtract it or the handle leads the cursor.
const PANE_PADDING_X = 12;

interface TraceWaterfallProps {
  trace: TraceDetail;
  /** Controlled span selection, owned by TracesView for the Escape ladder. */
  selectedSpanId: string | null;
  onSelectSpan: (spanId: string | null) => void;
  onClose?: () => void;
  /** Extra header actions (e.g. the trace-to-logs pivot), left of close. */
  actions?: React.ReactNode;
}

// Distinct colors for server-keyed spans (in order of preference)
const SPAN_COLORS = [
  'var(--color-secondary)',
  'var(--color-tertiary)',
  'var(--color-chart-blue)',
  'var(--color-chart-pink)',
  'var(--color-status-running)',
  'var(--color-chart-orange)',
  'var(--color-chart-cyan)',
  'var(--color-chart-purple)',
];

// Critical-path highlight only earns its ink once the tree is non-trivial.
const CRITICAL_PATH_MIN_SPANS = 4;

function stableColor(key: string): string {
  let hash = 0;
  for (let i = 0; i < key.length; i++) {
    hash = ((hash << 5) - hash) + key.charCodeAt(i);
    hash |= 0;
  }
  return SPAN_COLORS[Math.abs(hash) % SPAN_COLORS.length];
}

function getSpanServer(span: Span): string {
  return (
    span.attributes['gridctl.server.name'] ||
    span.attributes['server.name'] ||
    span.attributes['mcp.server.name'] ||
    span.attributes['mcp.server'] ||
    span.attributes['peer.service'] ||
    ''
  );
}

function getSpanTool(span: Span): string {
  return (
    span.attributes['gen_ai.tool.name'] ||
    span.attributes['mcp.tool.name'] ||
    span.attributes['tool.name'] ||
    ''
  );
}

// A root span that returned before routing keeps the bare name; the tool is
// still in its attributes.
function displaySpanName(span: Span): string {
  if (span.name !== 'tools/call') return span.name;
  const tool = getSpanTool(span);
  return tool ? `tools/call ${tool}` : span.name;
}

function computeP95(spans: Span[]): number {
  if (spans.length === 0) return Infinity;
  const durations = spans.map((s) => s.duration).sort((a, b) => a - b);
  const idx = Math.min(Math.floor(durations.length * 0.95), durations.length - 1);
  return durations[idx];
}

/** End of a span in epoch ms; falls back to startTime + duration when endTime
 *  is absent or unparseable. NaN only when startTime itself is unparseable. */
function spanEndMs(span: Span): number {
  if (span.endTime) {
    const end = new Date(span.endTime).getTime();
    if (Number.isFinite(end)) return end;
  }
  const start = new Date(span.startTime).getTime();
  return Number.isFinite(start) ? start + span.duration : NaN;
}

export function TraceWaterfall({ trace, selectedSpanId, onSelectSpan, onClose, actions }: TraceWaterfallProps) {
  const setTracesPrefs = useUIStore((s) => s.setTracesPrefs);
  const storedNameColPct = useUIStore((s) => s.tracesPrefs.nameColPct);

  // Span-name vs timeline column split. Local state carries the live value
  // during a drag; release commits to the persisted prefs and falls back to
  // the store, so a second window picks the change up without remounting.
  const paneRef = useRef<HTMLDivElement>(null);
  const [liveNameColPct, setLiveNameColPct] = useState<number | null>(null);
  const [isNameColDragging, setNameColDragging] = useState(false);
  const nameColWidth = `${liveNameColPct ?? storedNameColPct}%`;
  const endDragRef = useRef<(() => void) | null>(null);

  useEffect(() => () => endDragRef.current?.(), []);

  const onNameColDrag = (e: React.PointerEvent) => {
    if (e.button !== 0) return;
    e.preventDefault();
    const pane = paneRef.current;
    if (!pane) return;
    const rect = pane.getBoundingClientRect();
    const contentWidth = rect.width - PANE_PADDING_X * 2;
    if (contentWidth <= 0) return;
    const target = e.currentTarget;
    const pointerId = e.pointerId;
    if (typeof target.setPointerCapture === 'function') {
      target.setPointerCapture(pointerId);
    }
    setNameColDragging(true);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    let latest = storedNameColPct;
    const onMove = (ev: Event) => {
      const pct = (((ev as PointerEvent).clientX - rect.left - PANE_PADDING_X) / contentWidth) * 100;
      latest = Math.round(
        Math.min(TRACES_NAME_COL_MAX_PCT, Math.max(TRACES_NAME_COL_MIN_PCT, pct)) * 10,
      ) / 10;
      setLiveNameColPct(latest);
    };
    let settled = false;
    const cleanup = () => {
      if (settled) return;
      settled = true;
      endDragRef.current = null;
      target.removeEventListener('pointermove', onMove);
      target.removeEventListener('pointerup', cleanup);
      target.removeEventListener('pointercancel', cleanup);
      window.removeEventListener('blur', cleanup);
      releasePointerCapture(target, pointerId);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      setNameColDragging(false);
      setTracesPrefs({ nameColPct: latest });
      setLiveNameColPct(null);
    };
    endDragRef.current = cleanup;
    target.addEventListener('pointermove', onMove);
    target.addEventListener('pointerup', cleanup);
    target.addEventListener('pointercancel', cleanup);
    window.addEventListener('blur', cleanup);
  };

  const { rows, rowIndex, traceStart, totalDuration, p95, selfTimes, criticalPath, childCounts } = useMemo(() => {
    // Pre-order rows are the single source for order, depth, sibling
    // position, and parent and child lookup.
    const rows = preorderSpanRows(trace.spans);
    const rowIndex = new Map(rows.map((r, i) => [r.span.spanId, i]));
    const p95 = computeP95(trace.spans);

    // Only finite timestamps participate; a single bad span must not poison
    // the whole timeline with NaN.
    const starts = trace.spans.map((s) => new Date(s.startTime).getTime()).filter(Number.isFinite);
    const ends = trace.spans.map(spanEndMs).filter(Number.isFinite);
    const traceStart = starts.length > 0 ? Math.min(...starts) : 0;
    const traceEnd = ends.length > 0 ? Math.max(...ends) : traceStart;
    const totalDuration = Math.max(traceEnd - traceStart, 1);

    // Interval shape for self-time and critical path; spans with unparseable
    // timestamps are excluded rather than poisoning the math.
    const intervals: SpanInterval[] = trace.spans
      .map((s) => ({
        spanId: s.spanId,
        parentSpanId: s.parentSpanId,
        start: new Date(s.startTime).getTime(),
        end: spanEndMs(s),
      }))
      .filter((iv) => Number.isFinite(iv.start) && Number.isFinite(iv.end));
    const selfTimes = computeSelfTimes(intervals);
    const criticalPath =
      trace.spans.length >= CRITICAL_PATH_MIN_SPANS ? computeCriticalPath(intervals) : new Set<string>();

    const childCounts = new Map<string, number>();
    for (const r of rows) {
      if (r.parentId) childCounts.set(r.parentId, (childCounts.get(r.parentId) ?? 0) + 1);
    }

    return { rows, rowIndex, traceStart, totalDuration, p95, selfTimes, criticalPath, childCounts };
  }, [trace]);

  const selectedSpan = trace.spans.find((s) => s.spanId === selectedSpanId) ?? null;

  // Height split between the waterfall and the span-detail bottom drawer.
  // Persisted per panel combination, so closing the drawer restores the
  // full-height waterfall and reopening restores the drawer height.
  const { defaultLayout, onLayoutChanged } = useWorkspaceLayout({
    workspace: 'traces',
    key: 'detail',
    panelIds: selectedSpan ? ['waterfall-canvas', 'span-drawer'] : ['waterfall-canvas'],
  });

  // Build unique server list for legend
  const servers = useMemo(() => {
    const set = new Set(rows.map((r) => getSpanServer(r.span)));
    return Array.from(set);
  }, [rows]);

  const hasError = trace.spans.some((s) => s.status === 'error');

  // Roving tab stop: the last focused row while it is still in the trace,
  // else the selected span, else the first row.
  const [focusedSpanId, setFocusedSpanId] = useState<string | null>(null);
  const rowRefs = useRef(new Map<string, HTMLDivElement>());
  const tabStopId =
    focusedSpanId && rowIndex.has(focusedSpanId)
      ? focusedSpanId
      : selectedSpanId && rowIndex.has(selectedSpanId)
        ? selectedSpanId
        : (rows[0]?.span.spanId ?? null);

  const focusRow = useCallback((spanId: string | null | undefined) => {
    if (spanId) rowRefs.current.get(spanId)?.focus();
  }, []);

  // Handled keys are stopped so the document-level trace list navigation
  // never sees them. Escape is left alone for the TracesView ladder.
  const onTreeKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    const target = e.target as HTMLElement;
    if (target.getAttribute('role') !== 'treeitem') return;
    const id = target.dataset.spanId;
    const index = id ? rowIndex.get(id) : undefined;
    if (id == null || index == null) return;
    let next: string | null | undefined;
    switch (e.key) {
      case 'ArrowDown':
      case 'j':
        next = rows[index + 1]?.span.spanId;
        break;
      case 'ArrowUp':
      case 'k':
        next = rows[index - 1]?.span.spanId;
        break;
      case 'Home':
        next = rows[0]?.span.spanId;
        break;
      case 'End':
        next = rows[rows.length - 1]?.span.spanId;
        break;
      case 'ArrowLeft':
        next = rows[index].parentId;
        break;
      case 'ArrowRight': {
        const child = rows[index + 1];
        next = child?.parentId === id ? child.span.spanId : null;
        break;
      }
      case 'Enter':
      case ' ':
        e.preventDefault();
        e.stopPropagation();
        onSelectSpan(selectedSpanId === id ? null : id);
        return;
      default:
        return;
    }
    e.preventDefault();
    e.stopPropagation();
    focusRow(next);
  };

  const currentNameColPct = liveNameColPct ?? storedNameColPct;
  const onNameColKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    let pct: number;
    switch (e.key) {
      case 'ArrowLeft':
        pct = currentNameColPct - 1;
        break;
      case 'ArrowRight':
        pct = currentNameColPct + 1;
        break;
      case 'Home':
        pct = TRACES_NAME_COL_MIN_PCT;
        break;
      case 'End':
        pct = TRACES_NAME_COL_MAX_PCT;
        break;
      default:
        return;
    }
    e.preventDefault();
    e.stopPropagation();
    setTracesPrefs({
      nameColPct: Math.min(TRACES_NAME_COL_MAX_PCT, Math.max(TRACES_NAME_COL_MIN_PCT, pct)),
    });
  };

  // Closing the drawer from inside it would drop focus to <body> when the
  // drawer unmounts, so the selected row takes focus first.
  const closeDrawer = () => {
    focusRow(selectedSpanId);
    onSelectSpan(null);
  };
  const onDrawerKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'Escape' || e.metaKey || e.ctrlKey || e.altKey) return;
    // One press closes one layer: stop the ladder from also closing the
    // waterfall.
    e.preventDefault();
    e.stopPropagation();
    closeDrawer();
  };

  const downloadOTLP = async () => {
    try {
      const doc = await fetchTraceOTLP(trace.traceId);
      const blob = new Blob([JSON.stringify(doc, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `trace-${trace.traceId}.json`;
      a.click();
      URL.revokeObjectURL(url);
      showToast('success', 'Trace exported as OTLP JSON');
    } catch {
      showToast('error', 'Export failed');
    }
  };

  return (
    <div className="flex flex-col h-full min-h-0">
      {/* Header */}
      <div className="@container h-9 flex items-center justify-between px-3 flex-shrink-0 border-b border-border/30 bg-surface-elevated/20">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-3xs font-medium text-text-muted uppercase tracking-wider flex-shrink-0">Trace</span>
          <span className="text-xs font-mono text-text-primary truncate" title={trace.traceId}>
            {trace.traceId.slice(0, 16)}
          </span>
          <span className="text-3xs text-text-muted flex-shrink-0">
            {trace.spans.length} spans · {formatTotalDuration(totalDuration)}
          </span>
          {hasError && (
            <AlertCircle
              size={11}
              role="img"
              aria-label="Trace has errors"
              className="text-status-error flex-shrink-0"
            >
              <title>Trace has errors</title>
            </AlertCircle>
          )}
        </div>
        <div className="flex items-center gap-1 flex-shrink-0 ml-2">
          <button
            type="button"
            onClick={() => copyWithToast(trace.traceId, 'Trace ID')}
            title="Copy trace ID"
            aria-label="Copy trace ID"
            className="size-6 inline-flex items-center justify-center rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors"
          >
            <Copy size={11} />
          </button>
          <button
            type="button"
            onClick={() =>
              copyWithToast(`${window.location.origin}/traces?trace=${encodeURIComponent(trace.traceId)}`, 'Trace link')
            }
            title="Copy deep link"
            aria-label="Copy deep link"
            className="size-6 inline-flex items-center justify-center rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors"
          >
            <Link2 size={11} />
          </button>
          <button
            type="button"
            onClick={downloadOTLP}
            title="Export trace as OTLP JSON"
            aria-label="Export trace as OTLP JSON"
            className="size-6 inline-flex items-center justify-center rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors"
          >
            <Download size={11} />
          </button>
          {actions && <div className="w-px h-4 bg-border/50 mx-0.5" />}
          {actions}
          {onClose && (
            <button
              type="button"
              onClick={onClose}
              className="size-6 inline-flex items-center justify-center rounded-md hover:bg-surface-highlight transition-colors flex-shrink-0"
              aria-label="Close waterfall"
            >
              <X size={12} className="text-text-muted" />
            </button>
          )}
        </div>
      </div>

      {/* Body: waterfall over an optional height-resizable span-detail drawer */}
      <Group
        orientation="vertical"
        className="flex-1 min-h-0"
        defaultLayout={defaultLayout}
        onLayoutChanged={onLayoutChanged}
        resizeTargetMinimumSize={{ coarse: 24, fine: 16 }}
      >
        {/* Waterfall */}
        <Panel id="waterfall-canvas" defaultSize={selectedSpan ? '65' : '100'} minSize={160}>
          <div ref={paneRef} className="flex flex-col h-full min-h-0 overflow-hidden">
            {/* Column header row with timeline ruler. The ruler clips instead
                of scrolling: a private scrollbar would slide Duration off the
                span rows' duration column. */}
            <div className="flex items-center h-7 px-3 flex-shrink-0 border-b border-border/20 bg-surface-elevated/10">
              <div style={{ width: nameColWidth }} className="min-w-0 overflow-hidden text-3xs text-text-muted uppercase tracking-wider whitespace-nowrap">Span</div>
              <div
                role="separator"
                aria-orientation="vertical"
                aria-label="Resize span name column"
                aria-valuemin={TRACES_NAME_COL_MIN_PCT}
                aria-valuemax={TRACES_NAME_COL_MAX_PCT}
                aria-valuenow={Math.round(currentNameColPct)}
                tabIndex={0}
                onPointerDown={onNameColDrag}
                onKeyDown={onNameColKeyDown}
                title="Drag to resize the span column"
                className={cn(
                  'w-1 -ml-1 self-stretch cursor-col-resize flex-shrink-0 rounded transition-colors touch-none',
                  'focus-visible:-outline-offset-2',
                  isNameColDragging ? 'bg-primary/50' : 'hover:bg-primary/30',
                )}
              />
              <div className="flex-1 grid grid-cols-3 items-center h-full min-w-0 overflow-hidden" aria-hidden="true">
                <span className="min-w-0 overflow-hidden whitespace-nowrap text-left text-3xs text-text-muted font-mono">0</span>
                <span className="min-w-0 overflow-hidden whitespace-nowrap text-center text-3xs text-text-muted font-mono">
                  {formatDuration(totalDuration / 2)}
                </span>
                <span className="min-w-0 overflow-hidden whitespace-nowrap text-right text-3xs text-text-muted font-mono">
                  {formatDuration(totalDuration)}
                </span>
              </div>
              <div className="w-24 flex-shrink-0 text-right text-3xs text-text-muted uppercase tracking-wider whitespace-nowrap">Duration</div>
            </div>

            {/* Span rows */}
            <div
              role="tree"
              aria-label="Spans"
              onKeyDown={onTreeKeyDown}
              className="flex-1 overflow-y-auto scrollbar-dark min-h-0"
            >
              {rows.map(({ span, depth, posInSet, setSize }) => {
                const spanStart = new Date(span.startTime).getTime();
                const leftPct = Number.isFinite(spanStart)
                  ? ((spanStart - traceStart) / totalDuration) * 100
                  : 0;
                const widthPct = Math.max((span.duration / totalDuration) * 100, 0.5);
                const server = getSpanServer(span);
                const isError = span.status === 'error';
                const isSlow = !isError && span.duration > p95;
                const isSelected = selectedSpanId === span.spanId;
                const onCriticalPath = criticalPath.has(span.spanId);
                const hasChildren = (childCounts.get(span.spanId) ?? 0) > 0;
                const selfMs = selfTimes.get(span.spanId);
                const name = displaySpanName(span);

                const barColor = isError ? 'var(--color-status-error)' : isSlow ? 'var(--color-status-pending)' : stableColor(server);

                return (
                  <div
                    key={span.spanId}
                    ref={(el) => {
                      if (el) rowRefs.current.set(span.spanId, el);
                      else rowRefs.current.delete(span.spanId);
                    }}
                    role="treeitem"
                    data-span-id={span.spanId}
                    aria-level={depth + 1}
                    aria-posinset={posInSet}
                    aria-setsize={setSize}
                    aria-selected={isSelected}
                    aria-label={`${name}, ${formatDuration(span.duration)}, ${span.status}${isSlow ? ', slow' : ''}`}
                    tabIndex={span.spanId === tabStopId ? 0 : -1}
                    onFocus={() => setFocusedSpanId(span.spanId)}
                    onClick={() => onSelectSpan(isSelected ? null : span.spanId)}
                    className={cn(
                      'flex items-center h-7 px-3 cursor-pointer transition-colors border-b border-border/10 focus-visible:-outline-offset-2',
                      isSelected
                        ? 'bg-surface-highlight/50'
                        : 'hover:bg-surface-highlight/20'
                    )}
                  >
                    {/* Name column */}
                    <div
                      className="flex items-center gap-1 min-w-0 pr-2"
                      style={{ width: nameColWidth, paddingLeft: `${depth * 12}px` }}
                    >
                      {depth > 0 && (
                        <span className="w-2 h-px bg-border/50 flex-shrink-0" />
                      )}
                      <span
                        className={cn(
                          'text-3xs truncate font-mono',
                          isError ? 'text-status-error' : 'text-text-secondary'
                        )}
                        title={name}
                      >
                        {name}
                      </span>
                      {isError && (
                        <AlertCircle size={10} role="img" aria-label="error" className="text-status-error flex-shrink-0" />
                      )}
                      {isSlow && (
                        <span className="flex-shrink-0 text-3xs text-status-pending font-mono">slow</span>
                      )}
                    </div>

                    {/* Timeline bar column. min-w-0 matches the ruler so a narrow
                        pane shrinks both instead of scrolling the rows alone. */}
                    <div className="flex-1 relative h-4 min-w-0">
                      <div
                        className={cn(
                          'absolute top-0 h-full rounded-sm opacity-90 transition-opacity hover:opacity-100',
                          onCriticalPath && 'ring-1 ring-text-primary/70'
                        )}
                        style={{
                          left: `${leftPct}%`,
                          width: `${widthPct}%`,
                          minWidth: '3px',
                          backgroundColor: barColor,
                        }}
                      />
                    </div>

                    {/* Duration: total, with self time when children exist */}
                    <div className="w-24 flex flex-col items-end justify-center leading-tight flex-shrink-0">
                      <span className="text-3xs text-text-muted font-mono whitespace-nowrap">{formatDuration(span.duration)}</span>
                      {hasChildren && selfMs != null && (
                        <span className="text-3xs text-text-muted font-mono whitespace-nowrap">self {formatDuration(selfMs)}</span>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>

            {/* Legend */}
            {servers.length > 0 && (
              <div className="h-7 flex items-center gap-3 px-3 flex-shrink-0 border-t border-border/20 bg-surface-elevated/10 overflow-x-auto">
                {servers.map((srv) => (
                  <div key={srv ? `server:${srv}` : 'gateway'} className="flex items-center gap-1 flex-shrink-0">
                    <div
                      className="w-2 h-2 rounded-sm flex-shrink-0"
                      style={{ backgroundColor: stableColor(srv) }}
                    />
                    <span className="text-3xs text-text-muted font-mono">{srv || 'gateway'}</span>
                  </div>
                ))}
                {trace.spans.some((s) => s.status === 'error') && (
                  <div className="flex items-center gap-1 flex-shrink-0">
                    <div className="w-2 h-2 rounded-sm bg-status-error" />
                    <span className="text-3xs text-text-muted">error</span>
                  </div>
                )}
                {trace.spans.some((s) => s.duration > p95 && s.status !== 'error') && (
                  <div className="flex items-center gap-1 flex-shrink-0">
                    <div className="w-2 h-2 rounded-sm bg-status-pending" />
                    <span className="text-3xs text-text-muted">slow (&gt;p95)</span>
                  </div>
                )}
                {criticalPath.size > 0 && (
                  <div className="flex items-center gap-1 flex-shrink-0">
                    <div className="w-2 h-2 rounded-sm bg-surface-elevated ring-1 ring-text-primary/70" />
                    <span className="text-3xs text-text-muted">critical path</span>
                  </div>
                )}
              </div>
            )}
          </div>
        </Panel>

        {/* Span detail bottom drawer */}
        {selectedSpan && (
          <>
            <Separator id="sep-span-drawer" className="group/separator relative h-1.5 select-none focus-visible:-outline-offset-2">
              <SeparatorBody orientation="horizontal" />
            </Separator>
            <Panel id="span-drawer" defaultSize="35" minSize={160} maxSize="60">
              <div className="h-full" onKeyDown={onDrawerKeyDown}>
                <SpanDetail
                  span={selectedSpan}
                  selfTimeMs={
                    (childCounts.get(selectedSpan.spanId) ?? 0) > 0
                      ? selfTimes.get(selectedSpan.spanId)
                      : undefined
                  }
                  onClose={closeDrawer}
                />
              </div>
            </Panel>
          </>
        )}
      </Group>
    </div>
  );
}
