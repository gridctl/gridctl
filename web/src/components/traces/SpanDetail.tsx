import { X, Clock, Tag, Activity } from 'lucide-react';
import { cn } from '../../lib/cn';
import type { Span } from '../../lib/api';

interface SpanDetailProps {
  span: Span;
  onClose: () => void;
}

function formatTimestamp(iso: string | undefined): string {
  const t = iso ? new Date(iso).getTime() : NaN;
  if (!Number.isFinite(t)) return '–';
  return new Date(t).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    fractionalSecondDigits: 3,
  });
}

/** ISO end of a span; derives startTime + duration when endTime is absent. */
function spanEndIso(span: Span): string | undefined {
  if (span.endTime) return span.endTime;
  const start = new Date(span.startTime).getTime();
  return Number.isFinite(start) ? new Date(start + span.duration).toISOString() : undefined;
}

function formatDuration(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)}µs`;
  if (ms < 1000) return `${ms.toFixed(1)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

export function SpanDetail({ span, onClose }: SpanDetailProps) {
  // Empty-string values (e.g. unset gen_ai token counters) are noise, not data.
  const attrEntries = Object.entries(span.attributes).filter(([, value]) => value !== '');

  return (
    <div className="flex flex-col h-full bg-surface border-l border-border/50 min-w-0">
      {/* Header */}
      <div className="h-9 flex items-center justify-between px-3 flex-shrink-0 border-b border-border/30 bg-surface-elevated/20">
        <span className="text-xs font-medium text-text-primary truncate font-mono">{span.name}</span>
        <button
          onClick={onClose}
          className="p-1 rounded-md hover:bg-surface-highlight transition-colors flex-shrink-0 ml-2"
          aria-label="Close span detail"
        >
          <X size={12} className="text-text-muted" />
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto scrollbar-dark min-h-0 p-3 space-y-4">

        {/* Status badge */}
        <div className="flex items-center gap-2">
          <span
            className={cn(
              'px-2 py-0.5 text-[10px] font-medium rounded-full border',
              span.status === 'error'
                ? 'bg-status-error/10 text-status-error border-status-error/20'
                : 'bg-status-running/10 text-status-running border-status-running/20'
            )}
          >
            {span.status}
          </span>
          <span className="text-[10px] text-text-muted font-mono">{span.spanId.slice(0, 16)}</span>
        </div>

        {/* Timing */}
        <section>
          <div className="flex items-center gap-1.5 mb-2">
            <Clock size={10} className="text-text-muted" />
            <span className="text-[10px] font-medium text-text-muted uppercase tracking-wider">Timing</span>
          </div>
          <div className="rounded-lg bg-surface-elevated/60 border border-border/30 overflow-hidden">
            <table className="w-full text-xs">
              <tbody>
                <TimingRow label="Start" value={formatTimestamp(span.startTime)} />
                <TimingRow label="End" value={formatTimestamp(spanEndIso(span))} />
                <TimingRow label="Duration" value={formatDuration(span.duration)} highlight />
              </tbody>
            </table>
          </div>
        </section>

        {/* Attributes */}
        {attrEntries.length > 0 && (
          <section>
            <div className="flex items-center gap-1.5 mb-2">
              <Tag size={10} className="text-text-muted" />
              <span className="text-[10px] font-medium text-text-muted uppercase tracking-wider">
                Attributes ({attrEntries.length})
              </span>
            </div>
            <div className="rounded-lg bg-surface-elevated/60 border border-border/30 overflow-hidden">
              <table className="w-full text-xs">
                <tbody>
                  {attrEntries.map(([key, value]) => (
                    <tr key={key} className="border-b border-border/20 last:border-0 hover:bg-surface-highlight/20 transition-colors">
                      <td className="px-3 py-1.5 text-text-muted font-mono align-top w-[45%] break-all">{key}</td>
                      <td className="px-3 py-1.5 text-text-primary font-mono align-top break-all">{value}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        )}

        {/* Events */}
        {span.events.length > 0 && (
          <section>
            <div className="flex items-center gap-1.5 mb-2">
              <Activity size={10} className="text-text-muted" />
              <span className="text-[10px] font-medium text-text-muted uppercase tracking-wider">
                Events ({span.events.length})
              </span>
            </div>
            <div className="space-y-1.5">
              {span.events.map((event, idx) => (
                <div key={idx} className="rounded-lg bg-surface-elevated/60 border border-border/30 p-2.5">
                  <div className="flex items-center justify-between mb-1.5">
                    <span className="text-xs font-medium text-text-primary">{event.name}</span>
                    <span className="text-[10px] text-text-muted font-mono">{formatTimestamp(event.timestamp)}</span>
                  </div>
                  {Object.keys(event.attributes).length > 0 && (
                    <div className="space-y-0.5">
                      {Object.entries(event.attributes).map(([k, v]) => (
                        <div key={k} className="flex gap-2 text-[10px]">
                          <span className="text-text-muted font-mono">{k}:</span>
                          <span className="text-text-secondary font-mono break-all">{v}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </section>
        )}
      </div>
    </div>
  );
}

function TimingRow({ label, value, highlight }: { label: string; value: string; highlight?: boolean }) {
  return (
    <tr className="border-b border-border/20 last:border-0">
      <td className="px-3 py-1.5 text-text-muted w-24">{label}</td>
      <td className={cn('px-3 py-1.5 font-mono', highlight ? 'text-primary font-medium' : 'text-text-primary')}>{value}</td>
    </tr>
  );
}
