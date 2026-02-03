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
          'grid gap-2 px-3 py-1 log-text cursor-pointer',
          'grid-cols-[8.5em_5.5em_7.5em_1fr_2em]',
        )}
        onClick={onToggle}
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
        <div className="px-3 pb-2 log-text" style={{ marginLeft: '8.5em' }}>
          <div className="p-2 rounded-md bg-background/60 border border-border/30 font-mono log-text-detail">
            {/* Full message with wrapping */}
            <pre className={cn(
              'whitespace-pre-wrap break-words overflow-x-auto',
              log.level === 'ERROR' ? 'text-status-error' : 'text-text-primary'
            )}>
              {log.message}
            </pre>
            {log.traceId && (
              <div className="flex gap-2 mt-1 pt-1 border-t border-border/20">
                <span className="text-text-muted">trace_id:</span>
                <span className="text-secondary">{log.traceId}</span>
              </div>
            )}
            {log.attrs && (
              <pre className="text-text-secondary whitespace-pre-wrap break-all overflow-x-auto mt-1 pt-1 border-t border-border/20">
                {JSON.stringify(log.attrs, null, 2)}
              </pre>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
