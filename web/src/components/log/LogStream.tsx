import { useEffect, useMemo, useRef, useState, type RefObject } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { ScrollText, Terminal } from 'lucide-react';
import { EmptyState } from '../ui/EmptyState';
import { useListNav } from '../../hooks/useListNav';
import { LogLine } from './LogLine';
import { GATEWAY_LOG_SOURCE, logEntryKeys, logSourceOf, type ParsedLog } from './logTypes';

// Windowed rendering kicks in above this row count; small lists render plain
// so the DOM (and tests) stay simple where virtualization buys nothing.
const VIRTUALIZE_AT = 200;
const ESTIMATED_ROW_HEIGHT = 26;

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
  /** Click-to-filter on the per-line source badge. */
  onSourceClick?: (source: string) => void;
  fontSize: number;
  containerRef: RefObject<HTMLDivElement | null>;
  /** Slot above the first entry (e.g. PersistedFromMarker). */
  header?: React.ReactNode;
  emptyText?: string;
  /** Expanded entry key — lifted so keyboard nav and hosts share it. */
  expandedKey: string | null;
  onToggleExpand: (key: string | null) => void;
  /** Active search query for match highlighting. */
  searchQuery?: string;
  wrap?: boolean;
  relativeTime?: boolean;
  timeAnchor?: number;
  /** Empty-state context: active source and trace filters. */
  source?: string | null;
  traceFilter?: string | null;
  onShowAllSources?: () => void;
  onClearTrace?: () => void;
  windowSize?: number;
  /** Focus the search input (bound to '/'). */
  onFocusSearch?: () => void;
}

// Scrollable log stream shared by the Logs workspace and the detached logs
// window: owns follow/auto-scroll (pausing scroll position when the user
// scrolls up), keyboard navigation, virtualization for large windows, and
// the loading/error/empty states.
export function LogStream({
  logs,
  isLoading,
  error,
  hasActiveFilter,
  onClearFilter,
  showSource,
  onTraceClick,
  onSourceClick,
  fontSize,
  containerRef,
  header,
  emptyText = 'No logs yet',
  expandedKey,
  onToggleExpand,
  searchQuery = '',
  wrap = false,
  relativeTime = false,
  timeAnchor,
  source,
  traceFilter,
  onShowAllSources,
  onClearTrace,
  windowSize,
  onFocusSearch,
}: LogStreamProps) {
  const [autoScroll, setAutoScroll] = useState(true);
  // Keyboard cursor tracks entry identity, not array position: the poll
  // replaces the array every tick, so an index would drift to another line.
  const [navKey, setNavKey] = useState<string | null>(null);
  const keys = useMemo(() => logEntryKeys(logs), [logs]);
  const navIndex = useMemo(() => (navKey ? keys.indexOf(navKey) : -1), [keys, navKey]);
  const activeNavKey = navIndex >= 0 ? navKey : null;

  const virtualize = logs.length > VIRTUALIZE_AT;
  const virtualizer = useVirtualizer({
    count: virtualize ? logs.length : 0,
    getScrollElement: () => containerRef.current,
    estimateSize: () => ESTIMATED_ROW_HEIGHT,
    overscan: 12,
    getItemKey: (index) => keys[index] ?? index,
  });
  const virtualizerRef = useRef(virtualizer);
  virtualizerRef.current = virtualizer;

  // Follow: keep pinned to the newest entry until the user scrolls up.
  useEffect(() => {
    if (!autoScroll || !containerRef.current) return;
    if (virtualize) {
      virtualizerRef.current.scrollToIndex(logs.length - 1, { align: 'end' });
    } else {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [logs, autoScroll, virtualize, containerRef]);

  const handleScroll = () => {
    if (!containerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current;
    setAutoScroll(scrollHeight - scrollTop - clientHeight < 50);
  };

  // Keyboard navigation: j/k or arrows move the cursor, Enter expands, '/'
  // focuses search, Escape collapses (then clears the cursor). Entering nav
  // suspends follow-tail — scrollIntoView and the bottom pin would fight.
  // Deliberately no 'e' errors binding: useListNav reserves 'e' for onEdit in
  // the CRUD workspaces, and a Logs-only meaning would be inconsistent.
  useListNav({
    itemCount: logs.length,
    selectedIndex: navIndex < 0 ? 0 : navIndex,
    setSelectedIndex: (i) => {
      const key = keys[i];
      if (key == null) return;
      setNavKey(key);
      setAutoScroll(false);
      if (virtualize) {
        virtualizerRef.current.scrollToIndex(i);
      } else {
        document.getElementById(`log-row-${i}`)?.scrollIntoView({ block: 'nearest' });
      }
    },
    onEnter: () => {
      if (activeNavKey) onToggleExpand(expandedKey === activeNavKey ? null : activeNavKey);
    },
    onSlash: onFocusSearch,
    onEscape: () => {
      if (expandedKey) onToggleExpand(null);
      else if (navKey) setNavKey(null);
    },
  });

  const renderRow = (log: ParsedLog, i: number) => (
    <LogLine
      log={log}
      isExpanded={expandedKey === keys[i]}
      onToggle={() => onToggleExpand(expandedKey === keys[i] ? null : keys[i])}
      source={showSource ? logSourceOf(log) : undefined}
      onTraceClick={onTraceClick}
      onSourceClick={showSource ? onSourceClick : undefined}
      searchQuery={searchQuery}
      wrap={wrap}
      relativeTime={relativeTime}
      timeAnchor={timeAnchor}
    />
  );

  const rowClass = (i: number) =>
    activeNavKey === keys[i] ? 'bg-surface-highlight/40' : undefined;

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

      {/* Empty states, most specific filter first */}
      {!isLoading && !error && logs.length === 0 && (
        traceFilter ? (
          <EmptyState
            icon={ScrollText}
            title="No logs for this trace"
            description="The entries expired from the buffer or were never correlated."
            action={
              onClearTrace && (
                <button onClick={onClearTrace} className="text-[10px] text-primary hover:underline">
                  Clear trace filter
                </button>
              )
            }
          />
        ) : source != null ? (
          <EmptyState
            icon={Terminal}
            title={`No log lines for ${source === GATEWAY_LOG_SOURCE ? 'the gateway' : source}`}
            description={windowSize ? `Nothing in the last ${windowSize} entries under the current filters.` : 'Nothing in the current window under the current filters.'}
            action={
              <span className="flex items-center gap-3">
                {onShowAllSources && (
                  <button onClick={onShowAllSources} className="text-[10px] text-primary hover:underline">
                    Show all sources
                  </button>
                )}
                {onClearFilter && (
                  <button onClick={onClearFilter} className="text-[10px] text-primary hover:underline">
                    Clear filters
                  </button>
                )}
              </span>
            }
          />
        ) : hasActiveFilter ? (
          <EmptyState
            icon={Terminal}
            title="No entries match your filters"
            action={
              onClearFilter && (
                <button onClick={onClearFilter} className="text-[10px] text-primary hover:underline">
                  Clear filters
                </button>
              )
            }
          />
        ) : (
          <EmptyState
            icon={Terminal}
            title={emptyText}
            description="Activity appears here when the gateway and servers log."
          />
        )
      )}

      {/* Log entries */}
      {!error && logs.length > 0 && (
        virtualize ? (
          <>
            {header}
            <div
              role="grid"
              aria-label="Log entries"
              tabIndex={0}
              aria-activedescendant={navIndex >= 0 ? `log-row-${navIndex}` : undefined}
              className="relative w-full focus:outline-none"
              style={{ height: virtualizer.getTotalSize() }}
            >
              {virtualizer.getVirtualItems().map((item) => (
                <div
                  key={item.key}
                  role="row"
                  id={`log-row-${item.index}`}
                  aria-selected={activeNavKey === keys[item.index]}
                  data-index={item.index}
                  ref={virtualizer.measureElement}
                  className={`absolute top-0 left-0 w-full border-b border-border/20 ${rowClass(item.index) ?? ''}`}
                  style={{ transform: `translateY(${item.start}px)` }}
                >
                  {renderRow(logs[item.index], item.index)}
                </div>
              ))}
            </div>
          </>
        ) : (
          <>
          {header}
          <div
            role="grid"
            aria-label="Log entries"
            tabIndex={0}
            aria-activedescendant={navIndex >= 0 ? `log-row-${navIndex}` : undefined}
            className="divide-y divide-border/20 focus:outline-none"
          >
            {logs.map((log, i) => (
              <div
                key={keys[i]}
                role="row"
                id={`log-row-${i}`}
                aria-selected={activeNavKey === keys[i]}
                className={rowClass(i)}
              >
                {renderRow(log, i)}
              </div>
            ))}
          </div>
          </>
        )
      )}
    </div>
  );
}
