import { useState } from 'react';
import { Activity, ChevronDown, ChevronRight, Copy } from 'lucide-react';
import { cn } from '../../lib/cn';
import { copyWithToast } from '../ui/Toast';
import { KVTable } from '../ui/KVTable';
import { highlightMatches } from '../../lib/highlight';
import { formatRelativeTimeFine } from '../../lib/time';
import {
  GATEWAY_LOG_SOURCE,
  LEVEL_STYLES,
  PROMOTED_LOG_FIELDS,
  PROMOTED_LOG_KEYS,
  formatTimestamp,
  stringifyAttrValue,
  type ParsedLog,
} from './logTypes';

export function LogLine({
  log,
  isExpanded,
  onToggle,
  source,
  onTraceClick,
  onSourceClick,
  searchQuery = '',
  wrap = false,
  relativeTime = false,
  timeAnchor,
}: {
  log: ParsedLog;
  isExpanded: boolean;
  onToggle: () => void;
  /** When set, renders a source column (aggregate all-sources view). */
  source?: string;
  /** When set, trace IDs render as pivots into the trace view. */
  onTraceClick?: (traceId: string) => void;
  /** When set, the source badge becomes a click-to-filter affordance. */
  onSourceClick?: (source: string) => void;
  /** Active search query — matching spans in the message are highlighted. */
  searchQuery?: string;
  /** Soft-wrap the message instead of truncating to one line. */
  wrap?: boolean;
  /** Render a relative timestamp with the absolute value on hover. */
  relativeTime?: boolean;
  /** Epoch ms anchor for relative timestamps (the last completed load). */
  timeAnchor?: number;
}) {
  const styles = LEVEL_STYLES[log.level] || LEVEL_STYLES.DEBUG;
  const showSource = source !== undefined;

  const absoluteTs = formatTimestamp(log.timestamp);
  const parsedTs = Date.parse(log.timestamp);
  const timeLabel =
    relativeTime && Number.isFinite(parsedTs)
      ? formatRelativeTimeFine(new Date(parsedTs), timeAnchor)
      : absoluteTs;

  return (
    <div
      className={cn(
        'group border-l-2 transition-all duration-200',
        isExpanded ? 'bg-surface-highlight/30' : 'hover:bg-surface-highlight/20',
        styles.border.replace('border-', 'border-l-')
      )}
    >
      {/* Main log line */}
      <div
        role="gridcell"
        className={cn(
          'grid gap-2 px-3 py-1 log-text cursor-pointer',
          showSource
            ? 'grid-cols-[8.5em_5.5em_7em_7.5em_1fr_2em]'
            : 'grid-cols-[8.5em_5.5em_7.5em_1fr_2em]',
        )}
        onClick={onToggle}
      >
        {/* Timestamp */}
        <span
          className="text-text-muted font-mono log-text tabular-nums"
          title={relativeTime ? absoluteTs : log.timestamp}
        >
          {timeLabel}
        </span>

        {/* Level badge */}
        <span
          className={cn(
            'inline-flex items-center justify-center gap-1 px-1.5 py-0.5 rounded log-text font-semibold uppercase tracking-wide self-start',
            styles.bg,
            styles.text
          )}
        >
          <span className={cn('w-1 h-1 rounded-full', styles.dot)} />
          {log.level.slice(0, 4)}
        </span>

        {/* Source (aggregate view only) — click filters to this source */}
        {showSource &&
          (onSourceClick ? (
            <button
              onClick={(e) => {
                e.stopPropagation();
                onSourceClick(source);
              }}
              title={`Filter to ${source}`}
              aria-label={`Filter to ${source}`}
              className={cn(
                'inline-flex items-center gap-1 min-w-0 font-mono log-text text-left rounded self-start',
                'hover:underline focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary/50',
                source === GATEWAY_LOG_SOURCE ? 'text-primary/80' : 'text-violet-400/90'
              )}
            >
              <span
                className={cn(
                  'w-1 h-1 rounded-full flex-shrink-0',
                  source === GATEWAY_LOG_SOURCE ? 'bg-primary' : 'bg-violet-400'
                )}
              />
              <span className="truncate">{source}</span>
            </button>
          ) : (
            <span
              className={cn(
                'inline-flex items-center gap-1 min-w-0 font-mono log-text',
                source === GATEWAY_LOG_SOURCE ? 'text-primary/80' : 'text-violet-400/90'
              )}
              title={source}
            >
              <span
                className={cn(
                  'w-1 h-1 rounded-full flex-shrink-0',
                  source === GATEWAY_LOG_SOURCE ? 'bg-primary' : 'bg-violet-400'
                )}
              />
              <span className="truncate">{source}</span>
            </span>
          ))}

        {/* Component */}
        <span className="text-secondary font-mono log-text truncate" title={log.component}>
          {log.component || '\u2014'}
        </span>

        {/* Message */}
        <span
          className={cn(
            'font-mono log-text',
            wrap ? 'whitespace-pre-wrap break-words' : 'truncate',
            log.level === 'ERROR' ? 'text-status-error' : 'text-text-primary'
          )}
          title={wrap ? undefined : log.message}
        >
          {highlightMatches(log.message, searchQuery)}
        </span>

        {/* Trace pivot + expand indicator */}
        <span className="flex items-start justify-center gap-0.5 pt-0.5">
          {onTraceClick && log.traceId && (
            <button
              onClick={(e) => {
                e.stopPropagation();
                onTraceClick(log.traceId!);
              }}
              title={`View trace ${log.traceId}`}
              aria-label={`View trace ${log.traceId}`}
              className="p-0.5 rounded text-text-muted opacity-60 group-hover:opacity-100 focus-visible:opacity-100 hover:text-primary transition-all"
            >
              <Activity size={11} />
            </button>
          )}
          <ChevronRight
            size={12}
            className={cn(
              'text-text-muted transition-transform duration-200',
              isExpanded && 'rotate-90'
            )}
          />
        </span>
      </div>

      {/* Expanded details */}
      {isExpanded && (
        <LogLineDetail log={log} searchQuery={searchQuery} onTraceClick={onTraceClick} />
      )}
    </div>
  );
}

// Expand panel: full message, promoted MCP fields, copy actions, and the raw
// attrs collapsed under "Other attributes" — mirrors the trace SpanDetail
// vocabulary with slog-flat keys instead of OTel dotted ones.
function LogLineDetail({
  log,
  searchQuery,
  onTraceClick,
}: {
  log: ParsedLog;
  searchQuery: string;
  onTraceClick?: (traceId: string) => void;
}) {
  const [showOther, setShowOther] = useState(false);

  const promoted = PROMOTED_LOG_FIELDS.map(({ label, key }) => {
    const value = log.attrs?.[key];
    if (value == null || value === '') return null;
    return [label, highlightMatches(stringifyAttrValue(value), searchQuery)] as [string, React.ReactNode];
  }).filter((row): row is [string, React.ReactNode] => row !== null);

  const otherEntries = Object.entries(log.attrs ?? {}).filter(
    ([key, value]) => !PROMOTED_LOG_KEYS.has(key) && value !== '' && value != null,
  );

  return (
    <div className="px-3 pb-2 log-text" style={{ marginLeft: '8.5em' }}>
      <div className="p-2 rounded-md bg-background/60 border border-border/30 font-mono log-text-detail">
        {/* Full message with wrapping */}
        <pre
          className={cn(
            'whitespace-pre-wrap break-words overflow-x-auto',
            log.level === 'ERROR' ? 'text-status-error' : 'text-text-primary'
          )}
        >
          {highlightMatches(log.message, searchQuery)}
        </pre>

        {/* Copy actions */}
        <div className="flex items-center gap-1.5 mt-1.5">
          <CopyAction label="Copy message" onCopy={() => copyWithToast(log.message, 'Message')} />
          <CopyAction label="Copy raw" onCopy={() => copyWithToast(log.raw, 'Raw entry')} />
          {log.traceId && (
            <CopyAction label="Copy trace ID" onCopy={() => copyWithToast(log.traceId!, 'Trace ID')} />
          )}
        </div>

        {/* Trace chip — full id, pivots to the waterfall when wired */}
        {log.traceId && (
          <div className="flex items-center gap-2 mt-1.5 pt-1.5 border-t border-border/20">
            <span className="text-text-muted">trace</span>
            {onTraceClick ? (
              <button
                onClick={() => onTraceClick(log.traceId!)}
                title="View trace waterfall"
                className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded border bg-primary/10 text-primary border-primary/30 hover:bg-primary/15 transition-colors break-all text-left"
              >
                <Activity size={9} className="flex-shrink-0" />
                {log.traceId}
              </button>
            ) : (
              <span className="text-secondary break-all">{log.traceId}</span>
            )}
          </div>
        )}

        {/* Promoted MCP fields */}
        {promoted.length > 0 && (
          <div className="mt-1.5 pt-1.5 border-t border-border/20">
            <KVTable rows={promoted} />
          </div>
        )}

        {/* Other attributes, collapsed by default; promoted keys excluded */}
        {otherEntries.length > 0 && (
          <div className="mt-1.5 pt-1.5 border-t border-border/20">
            <button
              onClick={() => setShowOther((v) => !v)}
              aria-expanded={showOther}
              className="flex items-center gap-1.5 mb-1 text-text-muted hover:text-text-secondary transition-colors"
            >
              {showOther ? <ChevronDown size={10} /> : <ChevronRight size={10} />}
              <span className="uppercase tracking-wider">
                Other attributes ({otherEntries.length})
              </span>
            </button>
            {showOther && (
              <KVTable
                rows={otherEntries.map(([key, value]) => [key, stringifyAttrValue(value)])}
                monoKeys
              />
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function CopyAction({ label, onCopy }: { label: string; onCopy: () => void }) {
  return (
    <button
      onClick={onCopy}
      className="inline-flex items-center gap-1 px-1.5 py-0.5 rounded text-text-muted hover:text-primary hover:bg-surface-highlight transition-colors"
    >
      <Copy size={9} />
      {label}
    </button>
  );
}
