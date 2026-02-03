import { ChevronRight } from 'lucide-react';
import { cn } from '../../lib/cn';
import { LEVEL_STYLES, formatTimestamp, type ParsedLog } from './logTypes';

export function LogLine({
  log,
  isExpanded,
  onToggle,
}: {
  log: ParsedLog;
  isExpanded: boolean;
  onToggle: () => void;
}) {
  const styles = LEVEL_STYLES[log.level] || LEVEL_STYLES.DEBUG;
  const hasDetails = log.attrs || log.traceId;

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
        className={cn(
          'grid gap-2 px-3 py-1',
          'grid-cols-[90px_50px_80px_1fr_20px]',
          hasDetails && 'cursor-pointer'
        )}
        onClick={hasDetails ? onToggle : undefined}
      >
        {/* Timestamp */}
        <span className="text-text-muted font-mono log-text tabular-nums">
          {formatTimestamp(log.timestamp)}
        </span>

        {/* Level badge */}
        <span
          className={cn(
            'inline-flex items-center justify-center gap-1 px-1.5 py-0.5 rounded log-text font-semibold uppercase tracking-wide',
            styles.bg,
            styles.text
          )}
        >
          <span className={cn('w-1 h-1 rounded-full', styles.dot)} />
          {log.level.slice(0, 4)}
        </span>

        {/* Component */}
        <span className="text-secondary font-mono log-text truncate" title={log.component}>
          {log.component || '\u2014'}
        </span>

        {/* Message */}
        <span
          className={cn(
            'font-mono log-text truncate',
            log.level === 'ERROR' ? 'text-status-error' : 'text-text-primary'
          )}
          title={log.message}
        >
          {log.message}
        </span>

        {/* Expand indicator */}
        <span className="flex items-center justify-center">
          {hasDetails && (
            <ChevronRight
              size={12}
              className={cn(
                'text-text-muted transition-transform duration-200',
                isExpanded && 'rotate-90'
              )}
            />
          )}
        </span>
      </div>

      {/* Expanded details */}
      {isExpanded && hasDetails && (
        <div className="px-3 pb-2 ml-[90px]">
          <div className="p-2 rounded-md bg-background/60 border border-border/30 font-mono log-text-detail">
            {log.traceId && (
              <div className="flex gap-2 mb-1">
                <span className="text-text-muted">trace_id:</span>
                <span className="text-secondary">{log.traceId}</span>
              </div>
            )}
            {log.attrs && (
              <pre className="text-text-secondary whitespace-pre-wrap break-all overflow-x-auto">
                {JSON.stringify(log.attrs, null, 2)}
              </pre>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
