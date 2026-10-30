import { memo } from 'react';
import { Copy } from 'lucide-react';
import { cn } from '../../lib/cn';
import { copyWithToast } from '../ui/Toast';
import { formatTotalDuration } from '../../lib/duration';
import type { TraceSummary } from '../../lib/api';

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

export interface TraceRowProps {
  trace: TraceSummary;
  relativeLabel: string;
  heat: number;
  isSelected: boolean;
  isHighlighted: boolean;
  compact: boolean;
  onActivate: (traceId: string) => void;
}

// Field-wise on the rendered trace fields. fetchTraces replaces every object
// on a poll, so a shallow memo would redraw the whole table.
// eslint-disable-next-line react-refresh/only-export-components -- comparator is the memo test seam
export function traceRowPropsEqual(prev: TraceRowProps, next: TraceRowProps): boolean {
  return (
    prev.trace.traceId === next.trace.traceId &&
    prev.trace.tool === next.trace.tool &&
    prev.trace.operation === next.trace.operation &&
    prev.trace.client === next.trace.client &&
    prev.trace.server === next.trace.server &&
    prev.trace.startTime === next.trace.startTime &&
    prev.trace.duration === next.trace.duration &&
    prev.trace.spanCount === next.trace.spanCount &&
    prev.trace.status === next.trace.status &&
    prev.relativeLabel === next.relativeLabel &&
    prev.isSelected === next.isSelected &&
    prev.isHighlighted === next.isHighlighted &&
    prev.heat === next.heat &&
    prev.compact === next.compact &&
    prev.onActivate === next.onActivate
  );
}

export const TraceRow = memo(function TraceRow({
  trace,
  relativeLabel,
  heat,
  isSelected,
  isHighlighted,
  compact,
  onActivate,
}: TraceRowProps) {
  return (
    <tr
      id={`trace-row-${trace.traceId}`}
      aria-selected={isSelected}
      onClick={() => onActivate(trace.traceId)}
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
        {relativeLabel}
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
            className="opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 hover-none:opacity-100 focus:opacity-100 p-1 rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-all"
          >
            <Copy size={11} />
          </button>
        </td>
      )}
    </tr>
  );
}, traceRowPropsEqual);

TraceRow.displayName = 'TraceRow';
