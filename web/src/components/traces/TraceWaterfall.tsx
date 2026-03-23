import { useMemo, useState } from 'react';
import { X, AlertCircle } from 'lucide-react';
import { cn } from '../../lib/cn';
import { SpanDetail } from './SpanDetail';
import type { TraceDetail, Span } from '../../lib/api';

interface TraceWaterfallProps {
  trace: TraceDetail;
  onClose?: () => void;
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

function formatDuration(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)}µs`;
  if (ms < 1000) return `${ms.toFixed(1)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function formatTotalDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

export function TraceWaterfall({ trace, onClose }: TraceWaterfallProps) {
  const [selectedSpanId, setSelectedSpanId] = useState<string | null>(null);

  const { sorted, depthMap, traceStart, totalDuration, p95 } = useMemo(() => {
    const sorted = sortSpansByStartTime(trace.spans);
    const depthMap = buildDepthMap(trace.spans);
    const p95 = computeP95(trace.spans);

    const starts = trace.spans.map((s) => new Date(s.startTime).getTime());
    const ends = trace.spans.map((s) => new Date(s.endTime).getTime());
    const traceStart = Math.min(...starts);
    const traceEnd = Math.max(...ends);
    const totalDuration = Math.max(traceEnd - traceStart, 1);

    return { sorted, depthMap, traceStart, totalDuration, p95 };
  }, [trace]);

  const selectedSpan = trace.spans.find((s) => s.spanId === selectedSpanId) ?? null;

  // Build unique server list for legend
  const servers = useMemo(() => {
    const set = new Set(sorted.map(getSpanServer));
    return Array.from(set);
  }, [sorted]);

  const hasError = trace.spans.some((s) => s.status === 'error');

  return (
    <div className="flex flex-col h-full min-h-0">
      {/* Header */}
      <div className="h-9 flex items-center justify-between px-3 flex-shrink-0 border-b border-border/30 bg-surface-elevated/20">
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-[10px] font-medium text-text-muted uppercase tracking-wider flex-shrink-0">Trace</span>
          <span className="text-xs font-mono text-text-primary truncate">{trace.traceId.slice(0, 16)}</span>
          <span className="text-[10px] text-text-muted flex-shrink-0">
            {trace.spans.length} spans · {formatTotalDuration(totalDuration)}
          </span>
          {hasError && (
            <AlertCircle size={11} className="text-status-error flex-shrink-0" />
          )}
        </div>
        {onClose && (
          <button
            onClick={onClose}
            className="p-1 rounded-md hover:bg-surface-highlight transition-colors flex-shrink-0 ml-2"
            aria-label="Close waterfall"
          >
            <X size={12} className="text-text-muted" />
          </button>
        )}
      </div>

      {/* Body: waterfall + optional span detail */}
      <div className="flex flex-1 min-h-0">
        {/* Waterfall */}
        <div className={cn('flex flex-col min-h-0 overflow-hidden', selectedSpan ? 'w-[60%]' : 'w-full')}>
          {/* Column header row */}
          <div className="flex items-center h-7 px-3 flex-shrink-0 border-b border-border/20 bg-surface-elevated/10">
            <div className="w-[40%] text-[9px] text-text-muted uppercase tracking-wider">Span</div>
            <div className="flex-1 text-[9px] text-text-muted uppercase tracking-wider">Timeline</div>
            <div className="w-16 text-right text-[9px] text-text-muted uppercase tracking-wider">Duration</div>
          </div>

          {/* Span rows */}
          <div className="flex-1 overflow-y-auto scrollbar-dark min-h-0">
            {sorted.map((span) => {
              const depth = depthMap.get(span.spanId) ?? 0;
              const spanStart = new Date(span.startTime).getTime();
              const leftPct = ((spanStart - traceStart) / totalDuration) * 100;
              const widthPct = Math.max((span.duration / totalDuration) * 100, 0.5);
              const server = getSpanServer(span);
              const isError = span.status === 'error';
              const isSlow = !isError && span.duration > p95;
              const isSelected = selectedSpanId === span.spanId;

              const barColor = isError ? '#f43f5e' : isSlow ? '#eab308' : stableColor(server);

              return (
                <div
                  key={span.spanId}
                  onClick={() => setSelectedSpanId(isSelected ? null : span.spanId)}
                  className={cn(
                    'flex items-center h-7 px-3 cursor-pointer transition-colors border-b border-border/10',
                    isSelected
                      ? 'bg-surface-highlight/50'
                      : 'hover:bg-surface-highlight/20'
                  )}
                >
                  {/* Name column */}
                  <div
                    className="w-[40%] flex items-center gap-1 min-w-0 pr-2"
                    style={{ paddingLeft: `${depth * 12}px` }}
                  >
                    {depth > 0 && (
                      <span className="w-2 h-px bg-border/50 flex-shrink-0" />
                    )}
                    <span
                      className={cn(
                        'text-[10px] truncate font-mono',
                        isError ? 'text-status-error' : 'text-text-secondary'
                      )}
                    >
                      {span.name}
                    </span>
                  </div>

                  {/* Timeline bar column */}
                  <div className="flex-1 relative h-4">
                    <div
                      className="absolute top-0 h-full rounded-sm opacity-90 transition-opacity hover:opacity-100"
                      style={{
                        left: `${leftPct}%`,
                        width: `${widthPct}%`,
                        minWidth: '3px',
                        backgroundColor: barColor,
                      }}
                    />
                  </div>

                  {/* Duration */}
                  <div className="w-16 text-right text-[10px] text-text-muted font-mono flex-shrink-0">
                    {formatDuration(span.duration)}
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
            </div>
          )}
        </div>

        {/* Span detail panel */}
        {selectedSpan && (
          <div className="w-[40%] min-h-0 border-l border-border/30">
            <SpanDetail
              span={selectedSpan}
              onClose={() => setSelectedSpanId(null)}
            />
          </div>
        )}
      </div>
    </div>
  );
}
