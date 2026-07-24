import { useState } from 'react';
import { X, Clock, Tag, Activity, Copy, ChevronRight, ChevronDown } from 'lucide-react';
import { cn } from '../../lib/cn';
import { copyWithToast } from '../ui/Toast';
import { formatUSD, formatCompactNumber } from '../../lib/format';
import { formatDuration } from '../../lib/duration';
import type { Span } from '../../lib/api';

interface SpanDetailProps {
  span: Span;
  /** Duration minus child coverage; present only when the span has children. */
  selfTimeMs?: number;
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

// MCP-first promotion: these attributes render as named fields at the top;
// anything else collapses under "Other attributes".
const PROMOTED_FIELDS: { label: string; keys: string[]; format?: (v: string) => string }[] = [
  { label: 'Tool', keys: ['mcp.tool.name', 'gen_ai.tool.name', 'tool.name'] },
  { label: 'Server', keys: ['server.name', 'mcp.server.name'] },
  { label: 'Client', keys: ['mcp.client.name'] },
  { label: 'Transport', keys: ['network.transport'] },
  { label: 'Replica', keys: ['mcp.replica.id'] },
  { label: 'Model', keys: ['gen_ai.request.model'] },
  {
    label: 'Input tokens',
    keys: ['gen_ai.usage.input_tokens'],
    format: (v) => formatCompactNumber(Number(v)),
  },
  {
    label: 'Output tokens',
    keys: ['gen_ai.usage.output_tokens'],
    format: (v) => formatCompactNumber(Number(v)),
  },
  {
    label: 'Cost',
    keys: ['gen_ai.cost.usd'],
    format: (v) => formatUSD(Number(v)),
  },
];

const PROMOTED_KEYS = new Set(PROMOTED_FIELDS.flatMap((f) => f.keys));

/** First message from an OTel exception/error event, for error spans. */
function errorMessage(span: Span): string | null {
  for (const event of span.events) {
    const msg = event.attributes['exception.message'] ?? event.attributes['error.message'];
    if (msg) return msg;
    if (event.name === 'exception') return event.attributes['exception.type'] ?? event.name;
  }
  return null;
}

export function SpanDetail({ span, selfTimeMs, onClose }: SpanDetailProps) {
  const [showOther, setShowOther] = useState(false);

  // Empty-string values (e.g. unset gen_ai token counters) are noise, not data.
  const attrEntries = Object.entries(span.attributes).filter(([, value]) => value !== '');
  const promoted = PROMOTED_FIELDS.map((field) => {
    const key = field.keys.find((k) => span.attributes[k]);
    const raw = key ? span.attributes[key] : undefined;
    if (!raw) return null;
    return { label: field.label, value: field.format ? field.format(raw) : raw };
  }).filter((f): f is { label: string; value: string } => f !== null);
  const otherEntries = attrEntries.filter(([key]) => !PROMOTED_KEYS.has(key));

  const cost = span.attributes['gen_ai.cost.usd'];
  const errMsg = span.status === 'error' ? errorMessage(span) : null;

  return (
    <div className="flex flex-col h-full bg-surface border-l border-border/50 min-w-0">
      {/* Header */}
      <div className="h-9 flex items-center justify-between px-3 flex-shrink-0 border-b border-border/30 bg-surface-elevated/20">
        <span className="text-xs font-medium text-text-primary truncate font-mono" title={span.name}>{span.name}</span>
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

        {/* Status badge + cost pill + span ID */}
        <div className="flex items-center gap-2 min-w-0">
          <span
            className={cn(
              'px-2 py-0.5 text-[10px] font-medium rounded-full border flex-shrink-0',
              span.status === 'error'
                ? 'bg-status-error/10 text-status-error border-status-error/20'
                : 'bg-status-running/10 text-status-running border-status-running/20'
            )}
          >
            {span.status}
          </span>
          {cost && (
            <span className="px-2 py-0.5 text-[10px] font-medium rounded-full border bg-primary/10 text-primary border-primary/20 font-mono flex-shrink-0">
              {formatUSD(Number(cost))}
            </span>
          )}
          <span className="text-[10px] text-text-muted font-mono truncate" title={span.spanId}>
            {span.spanId.slice(0, 16)}
          </span>
          <button
            onClick={() => copyWithToast(span.spanId, 'Span ID')}
            title="Copy span ID"
            className="p-1 rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors flex-shrink-0"
          >
            <Copy size={10} />
          </button>
        </div>

        {/* Error message: the badge alone doesn't explain what failed */}
        {errMsg && (
          <div className="rounded-lg bg-status-error/5 border border-status-error/20 px-3 py-2 text-[11px] text-status-error break-words">
            {errMsg}
          </div>
        )}

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
                {selfTimeMs != null && (
                  <TimingRow label="Self time" value={formatDuration(selfTimeMs)} />
                )}
              </tbody>
            </table>
          </div>
        </section>

        {/* Promoted MCP fields */}
        {promoted.length > 0 && (
          <section>
            <div className="flex items-center gap-1.5 mb-2">
              <Tag size={10} className="text-text-muted" />
              <span className="text-[10px] font-medium text-text-muted uppercase tracking-wider">MCP</span>
            </div>
            <KVTable rows={promoted.map(({ label, value }) => [label, value] as [string, string])} />
          </section>
        )}

        {/* Other attributes, collapsed by default */}
        {otherEntries.length > 0 && (
          <section>
            <button
              onClick={() => setShowOther((v) => !v)}
              aria-expanded={showOther}
              className="flex items-center gap-1.5 mb-2 text-text-muted hover:text-text-secondary transition-colors"
            >
              {showOther ? <ChevronDown size={10} /> : <ChevronRight size={10} />}
              <span className="text-[10px] font-medium uppercase tracking-wider">
                Other attributes ({otherEntries.length})
              </span>
            </button>
            {showOther && <KVTable rows={otherEntries} monoKeys />}
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

function KVTable({ rows, monoKeys }: { rows: [string, string][]; monoKeys?: boolean }) {
  return (
    <div className="rounded-lg bg-surface-elevated/60 border border-border/30 overflow-hidden">
      <table className="w-full text-xs">
        <tbody>
          {rows.map(([key, value]) => (
            <tr key={key} className="border-b border-border/20 last:border-0 hover:bg-surface-highlight/20 transition-colors">
              <td className={cn('px-3 py-1.5 text-text-muted align-top w-[45%]', monoKeys && 'font-mono break-all')}>
                {key}
              </td>
              <td className="px-3 py-1.5 text-text-primary font-mono align-top break-all">{value}</td>
            </tr>
          ))}
        </tbody>
      </table>
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
