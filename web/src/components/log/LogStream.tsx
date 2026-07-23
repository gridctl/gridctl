import { useEffect, useState, type RefObject } from 'react';
import { Terminal } from 'lucide-react';
import { LogLine } from './LogLine';
import { logSourceOf, type ParsedLog } from './logTypes';

interface LogStreamProps {
  /** Already-filtered entries to render. */
  logs: ParsedLog[];
  isLoading: boolean;
  error: string | null;
  /** True when a search/trace filter is hiding entries. */
  hasActiveFilter?: boolean;
  onClearFilter?: () => void;
  /** Render the per-line source badge (aggregate all-sources view). */
  showSource?: boolean;
  onTraceClick?: (traceId: string) => void;
  fontSize: number;
  containerRef: RefObject<HTMLDivElement | null>;
  /** Slot above the first entry (e.g. PersistedFromMarker). */
  header?: React.ReactNode;
  emptyText?: string;
}

// Scrollable log stream shared by the Logs workspace and the detached logs
// window: owns follow/auto-scroll (pausing scroll position when the user
// scrolls up), row expansion, and the loading/error/empty states.
export function LogStream({
  logs,
  isLoading,
  error,
  hasActiveFilter,
  onClearFilter,
  showSource,
  onTraceClick,
  fontSize,
  containerRef,
  header,
  emptyText = 'No logs yet',
}: LogStreamProps) {
  const [autoScroll, setAutoScroll] = useState(true);
  const [expandedIndex, setExpandedIndex] = useState<number | null>(null);

  // Follow: keep pinned to the newest entry until the user scrolls up.
  useEffect(() => {
    if (autoScroll && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [logs, autoScroll, containerRef]);

  const handleScroll = () => {
    if (!containerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current;
    setAutoScroll(scrollHeight - scrollTop - clientHeight < 50);
  };

  return (
    <div
      ref={containerRef}
      onScroll={handleScroll}
      className="flex-1 overflow-auto bg-background/80 scrollbar-dark min-h-0"
      style={{ '--log-font-size': `${fontSize}px` } as React.CSSProperties}
    >
      {/* Loading state */}
      {isLoading && logs.length === 0 && !error && (
        <div className="flex items-center gap-2 p-4 text-text-muted text-xs">
          <div className="w-3 h-3 border-2 border-primary border-t-transparent rounded-full animate-spin" />
          Loading logs...
        </div>
      )}

      {/* Error state */}
      {error && (
        <div className="flex items-center gap-2 p-4 text-status-error text-xs">
          <span className="w-2 h-2 rounded-full bg-status-error" />
          Error: {error}
        </div>
      )}

      {/* Empty state */}
      {!isLoading && !error && logs.length === 0 && (
        <div className="flex flex-col items-center justify-center h-full text-text-muted text-xs gap-2">
          <Terminal size={20} className="text-text-muted/30" />
          <span>{hasActiveFilter ? 'No entries match your filters' : emptyText}</span>
          {hasActiveFilter && onClearFilter && (
            <button onClick={onClearFilter} className="text-primary hover:underline">
              Clear filters
            </button>
          )}
        </div>
      )}

      {/* Log entries */}
      {!error && logs.length > 0 && (
        <div className="divide-y divide-border/20">
          {header}
          {logs.map((log, i) => (
            <LogLine
              key={i}
              log={log}
              isExpanded={expandedIndex === i}
              onToggle={() => setExpandedIndex(expandedIndex === i ? null : i)}
              source={showSource ? logSourceOf(log) : undefined}
              onTraceClick={onTraceClick}
            />
          ))}
        </div>
      )}
    </div>
  );
}
