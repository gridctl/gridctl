import { useMemo, useRef, useState } from 'react';
import { Group, Panel, Separator } from 'react-resizable-panels';
import { X, AlertCircle, Copy, Link2, Download } from 'lucide-react';
import { cn } from '../../lib/cn';
import { SpanDetail } from './SpanDetail';
import { SeparatorBody } from '../ui/SeparatorBody';
import { copyWithToast, showToast } from '../ui/Toast';
import { fetchTraceOTLP } from '../../lib/api';
import { formatDuration, formatTotalDuration } from '../../lib/duration';
import { computeSelfTimes, computeCriticalPath } from '../../lib/traceMath';
import { useWorkspaceLayout } from '../../hooks/useWorkspaceLayout';
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
  '#0d9488', // teal
  '#8b5cf6', // violet
  '#3b82f6', // blue
  '#ec4899', // pink
  '#10b981', // emerald
  '#f97316', // orange
  '#06b6d4', // cyan
  '#a855f7', // purple
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
    span.attributes['server.name'] ??
    span.attributes['mcp.server.name'] ??
    span.attributes['peer.service'] ??
    span.name.split('.')[0]
  );
}

function buildDepthMap(spans: Span[]): Map<string, number> {
  const depthMap = new Map<string, number>();
  const byId = new Map(spans.map((s) => [s.spanId, s]));

  function depth(spanId: string, visited = new Set<string>()): number {
    if (depthMap.has(spanId)) return depthMap.get(spanId)!;
    if (visited.has(spanId)) return 0;
    const span = byId.get(spanId);
    if (!span?.parentSpanId) {
      depthMap.set(spanId, 0);
      return 0;
    }
    visited.add(spanId);
    const d = depth(span.parentSpanId, visited) + 1;
    depthMap.set(spanId, d);
    return d;
  }

  spans.forEach((s) => depth(s.spanId));
  return depthMap;
}

function sortSpansByStartTime(spans: Span[]): Span[] {
  return [...spans].sort(
    (a, b) => new Date(a.startTime).getTime() - new Date(b.startTime).getTime()
  );
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

  const onNameColDrag = (e: React.MouseEvent) => {
    e.preventDefault();
    const pane = paneRef.current;
    if (!pane) return;
    const rect = pane.getBoundingClientRect();
    const contentWidth = rect.width - PANE_PADDING_X * 2;
    if (contentWidth <= 0) return;
    setNameColDragging(true);
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
    let latest = storedNameColPct;
    const onMove = (ev: MouseEvent) => {
      const pct = ((ev.clientX - rect.left - PANE_PADDING_X) / contentWidth) * 100;
      latest = Math.round(
        Math.min(TRACES_NAME_COL_MAX_PCT, Math.max(TRACES_NAME_COL_MIN_PCT, pct)) * 10,
      ) / 10;
      setLiveNameColPct(latest);
    };
    const onUp = () => {
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      setNameColDragging(false);
      setTracesPrefs({ nameColPct: latest });
      setLiveNameColPct(null);
    };
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
  };

  const { sorted, depthMap, traceStart, totalDuration, p95, selfTimes, criticalPath, childCounts } = useMemo(() => {
    const sorted = sortSpansByStartTime(trace.spans);
    const depthMap = buildDepthMap(trace.spans);
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
    for (const s of trace.spans) {
      if (s.parentSpanId) {
        childCounts.set(s.parentSpanId, (childCounts.get(s.parentSpanId) ?? 0) + 1);
      }
    }

    return { sorted, depthMap, traceStart, totalDuration, p95, selfTimes, criticalPath, childCounts };
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
    const set = new Set(sorted.map(getSpanServer));
    return Array.from(set);
  }, [sorted]);

  const hasError = trace.spans.some((s) => s.status === 'error');

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
      <div className="h-9 flex items-center justify-between px-3 flex-shrink-0 border-b border-border/30 bg-surface-elevated/20">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-[10px] font-medium text-text-muted uppercase tracking-wider flex-shrink-0">Trace</span>
          <span className="text-xs font-mono text-text-primary truncate" title={trace.traceId}>
            {trace.traceId.slice(0, 16)}
          </span>
          <span className="text-[10px] text-text-muted flex-shrink-0">
            {trace.spans.length} spans · {formatTotalDuration(totalDuration)}
          </span>
          {hasError && (
            <AlertCircle size={11} className="text-status-error flex-shrink-0" />
          )}
        </div>
        <div className="flex items-center gap-1 flex-shrink-0 ml-2">
          <button
            onClick={() => copyWithToast(trace.traceId, 'Trace ID')}
            title="Copy trace ID"
            className="p-1 rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors"
          >
            <Copy size={11} />
          </button>
          <button
            onClick={() =>
              copyWithToast(`${window.location.origin}/traces?trace=${encodeURIComponent(trace.traceId)}`, 'Trace link')
            }
            title="Copy deep link"
            className="p-1 rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors"
          >
            <Link2 size={11} />
          </button>
          <button
            onClick={downloadOTLP}
            title="Export trace as OTLP JSON"
            className="p-1 rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors"
          >
            <Download size={11} />
          </button>
          {actions && <div className="w-px h-4 bg-border/50 mx-0.5" />}
          {actions}
          {onClose && (
            <button
              onClick={onClose}
              className="p-1 rounded-md hover:bg-surface-highlight transition-colors flex-shrink-0"
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
      >
        {/* Waterfall */}
        <Panel id="waterfall-canvas" defaultSize={selectedSpan ? '65' : '100'} minSize={160}>
          <div ref={paneRef} className="flex flex-col h-full min-h-0 overflow-hidden">
            {/* Column header row with timeline ruler */}
            <div className="flex items-center h-7 px-3 flex-shrink-0 border-b border-border/20 bg-surface-elevated/10">
              <div style={{ width: nameColWidth }} className="text-[9px] text-text-muted uppercase tracking-wider">Span</div>
              <div
                onMouseDown={onNameColDrag}
                title="Drag to resize the span column"
                className={cn(
                  'w-1 -ml-1 self-stretch cursor-col-resize flex-shrink-0 rounded transition-colors',
                  isNameColDragging ? 'bg-primary/50' : 'hover:bg-primary/30',
                )}
              />
              <div className="flex-1 relative h-full" aria-hidden="true">
                <span className="absolute left-0 top-1/2 -translate-y-1/2 text-[8px] text-text-muted/70 font-mono">0</span>
                <span className="absolute left-1/2 -translate-x-1/2 top-1/2 -translate-y-1/2 text-[8px] text-text-muted/70 font-mono">
                  {formatDuration(totalDuration / 2)}
                </span>
                <span className="absolute right-0 top-1/2 -translate-y-1/2 text-[8px] text-text-muted/70 font-mono">
                  {formatDuration(totalDuration)}
                </span>
              </div>
              <div className="w-24 text-right text-[9px] text-text-muted uppercase tracking-wider">Duration</div>
            </div>

            {/* Span rows */}
            <div className="flex-1 overflow-y-auto scrollbar-dark min-h-0">
              {sorted.map((span) => {
                const depth = depthMap.get(span.spanId) ?? 0;
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

                const barColor = isError ? '#f43f5e' : isSlow ? '#eab308' : stableColor(server);

                return (
                  <div
                    key={span.spanId}
                    onClick={() => onSelectSpan(isSelected ? null : span.spanId)}
                    className={cn(
                      'flex items-center h-7 px-3 cursor-pointer transition-colors border-b border-border/10',
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
                          'text-[10px] truncate font-mono',
                          isError ? 'text-status-error' : 'text-text-secondary'
                        )}
                        title={span.name}
                      >
                        {span.name}
                      </span>
                    </div>

                    {/* Timeline bar column */}
                    <div className="flex-1 relative h-4">
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
                      <span className="text-[10px] text-text-muted font-mono">{formatDuration(span.duration)}</span>
                      {hasChildren && selfMs != null && (
                        <span className="text-[8px] text-text-muted/60 font-mono">self {formatDuration(selfMs)}</span>
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
                  <div key={srv} className="flex items-center gap-1 flex-shrink-0">
                    <div
                      className="w-2 h-2 rounded-sm flex-shrink-0"
                      style={{ backgroundColor: stableColor(srv) }}
                    />
                    <span className="text-[9px] text-text-muted font-mono">{srv}</span>
                  </div>
                ))}
                {sorted.some((s) => s.status === 'error') && (
                  <div className="flex items-center gap-1 flex-shrink-0">
                    <div className="w-2 h-2 rounded-sm bg-status-error" />
                    <span className="text-[9px] text-text-muted">error</span>
                  </div>
                )}
                {sorted.some((s) => s.duration > p95 && s.status !== 'error') && (
                  <div className="flex items-center gap-1 flex-shrink-0">
                    <div className="w-2 h-2 rounded-sm bg-status-pending" />
                    <span className="text-[9px] text-text-muted">slow (&gt;p95)</span>
                  </div>
                )}
                {criticalPath.size > 0 && (
                  <div className="flex items-center gap-1 flex-shrink-0">
                    <div className="w-2 h-2 rounded-sm bg-surface-elevated ring-1 ring-text-primary/70" />
                    <span className="text-[9px] text-text-muted">critical path</span>
                  </div>
                )}
              </div>
            )}
          </div>
        </Panel>

        {/* Span detail bottom drawer */}
        {selectedSpan && (
          <>
            <Separator id="sep-span-drawer" className="group/separator relative h-1.5 select-none">
              <SeparatorBody orientation="horizontal" />
            </Separator>
            <Panel id="span-drawer" defaultSize="35" minSize={160} maxSize="60">
              <SpanDetail
                span={selectedSpan}
                selfTimeMs={
                  (childCounts.get(selectedSpan.spanId) ?? 0) > 0
                    ? selfTimes.get(selectedSpan.spanId)
                    : undefined
                }
                onClose={() => onSelectSpan(null)}
              />
            </Panel>
          </>
        )}
      </Group>
    </div>
  );
}
