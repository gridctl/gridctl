import { useCallback, useEffect, useMemo, useRef, useState, type RefObject } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { ArrowDown, ScrollText, Terminal } from 'lucide-react';
import { EmptyState, FailedState } from '../ui/EmptyState';
import { refreshPhase } from '../../lib/refreshPhase';
import { listNavProps, useListNav } from '../../hooks/useListNav';
import { LogLine } from './LogLine';
import {
  GATEWAY_LOG_SOURCE,
  countNewLogs,
  logEntryKeys,
  logSourceOf,
  type LogTailAnchor,
  type NewLogCount,
  type ParsedLog,
} from './logTypes';

// Windowed rendering kicks in above this row count; small lists render plain
// so the DOM (and tests) stay simple where virtualization buys nothing.
const VIRTUALIZE_AT = 200;
const ESTIMATED_ROW_HEIGHT = 26;
const FOLLOW_THRESHOLD = 50;
const NO_NEW_LOGS: NewLogCount = { count: 0, overflow: false };

interface LogStreamProps {
  /** Already-filtered entries to render. */
  logs: ParsedLog[];
  isLoading: boolean;
  error: string | null;
  /** A load has completed; defaults to `logs.length > 0` for direct callers. */
  hasLoaded?: boolean;
  onRetry?: () => void;
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
  /** Prefix absolute timestamps with the date (loaded window spans days). */
  showDate?: boolean;
  /** Empty-state context: active source and trace filters. */
  source?: string | null;
  traceFilter?: string | null;
  onShowAllSources?: () => void;
  onClearTrace?: () => void;
  windowSize?: number;
  /** Focus the search input (bound to '/'). */
  onFocusSearch?: () => void;
  /** Follow-tail: pinned to the newest entry. Owned by the host. */
  following: boolean;
  onFollowingChange: (next: boolean) => void;
}

// Scrollable log stream shared by the Logs workspace and the detached logs
// window: owns the follow-tail mechanics (pinning to the newest entry,
// suspending when the user scrolls up, the Jump to latest button), keyboard
// navigation, virtualization for large windows, and the loading/error/empty
// states. The follow flag itself is controlled by the host.
export function LogStream({
  logs,
  isLoading,
  error,
  hasLoaded,
  onRetry,
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
  showDate = false,
  source,
  traceFilter,
  onShowAllSources,
  onClearTrace,
  windowSize,
  onFocusSearch,
  following,
  onFollowingChange,
}: LogStreamProps) {
  const phase = refreshPhase({ error, hasLoaded: hasLoaded ?? logs.length > 0 });
  const showData = phase === 'ready' || phase === 'stale';
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

  // Newest entry seen while following; copied into anchor state only when
  // follow suspends, so polls while following cost no extra render.
  const tailRef = useRef<LogTailAnchor | null>(null);
  const [anchor, setAnchor] = useState<LogTailAnchor | null>(null);
  // Set while our own pin-to-bottom scrolls, so its scroll events (and the
  // virtualizer's follow-up corrections in the same frame) never suspend.
  const pinningRef = useRef(false);
  const lastScrollTopRef = useRef(0);

  const pinToBottom = useCallback(() => {
    const el = containerRef.current;
    if (!el || logs.length === 0) return () => {};
    pinningRef.current = true;
    if (virtualize) {
      virtualizerRef.current.scrollToIndex(logs.length - 1, { align: 'end' });
    } else {
      el.scrollTop = el.scrollHeight;
    }
    const frame = requestAnimationFrame(() => {
      pinningRef.current = false;
    });
    return () => cancelAnimationFrame(frame);
  }, [containerRef, logs.length, virtualize]);

  // Follow: keep pinned to the newest entry until the user scrolls up.
  useEffect(() => {
    if (!following) return;
    const last = logs.length - 1;
    if (last >= 0) tailRef.current = { key: keys[last], ts: Date.parse(logs[last].timestamp) };
    return pinToBottom();
  }, [logs, keys, following, pinToBottom]);

  const suspend = () => {
    if (!following) return;
    setAnchor(tailRef.current);
    onFollowingChange(false);
  };

  const fresh = useMemo(
    () => (following ? NO_NEW_LOGS : countNewLogs(keys, logs, anchor)),
    [following, keys, logs, anchor],
  );

  const jumpToLatest = () => {
    if (logs.length === 0) return;
    setNavKey(null);
    onFollowingChange(true);
    pinToBottom();
  };

  // One callback for every row. LogLine chooses expand vs collapse from
  // entryKey and isExpanded, so this identity does not follow the open row.
  const onToggle = useCallback((key: string | null) => {
    onToggleExpand(key);
  }, [onToggleExpand]);

  const handleScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    const { scrollTop, scrollHeight, clientHeight } = el;
    const previous = lastScrollTopRef.current;
    lastScrollTopRef.current = scrollTop;
    // The first event after our own pin is that pin (or a correction to it).
    const pinned = pinningRef.current;
    pinningRef.current = false;
    if (scrollHeight - scrollTop - clientHeight < FOLLOW_THRESHOLD) {
      if (!following) onFollowingChange(true);
      return;
    }
    // Only an upward scroll away from the bottom suspends. Virtualizer
    // measurement corrections and content growth move scrollTop without
    // the user asking to stop following.
    if (pinned) return;
    if (scrollTop < previous) suspend();
  };

  // Keyboard navigation: j/k or arrows move the cursor, Enter expands, '/'
  // focuses search, Escape collapses (then clears the cursor), End or G jumps
  // to the newest entry and resumes follow. Moving the cursor in a list that
  // overflows suspends follow; scrollIntoView and the bottom pin would fight.
  // Deliberately no 'e' errors binding: useListNav reserves 'e' for onEdit in
  // the CRUD workspaces, and a Logs-only meaning would be inconsistent.
  useListNav({
    itemCount: logs.length,
    selectedIndex: navIndex < 0 ? 0 : navIndex,
    setSelectedIndex: (i) => {
      const key = keys[i];
      if (key == null) return;
      setNavKey(key);
      const el = containerRef.current;
      if (el && el.scrollHeight > el.clientHeight) suspend();
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
    onEnd: jumpToLatest,
  });

  const showJump = !following && !error && showData && logs.length > 0;
  const jumpLabel =
    fresh.count === 0
      ? 'Jump to latest'
      : `${fresh.count}${fresh.overflow ? '+' : ''} new · Jump to latest`;
  const onJumpClick = () => {
    jumpToLatest();
    // The grid sits inside the scroller (as tall as the list when
    // virtualized), so a plain focus() would scroll back and undo the jump.
    containerRef.current
      ?.querySelector<HTMLElement>('[role="grid"]')
      ?.focus({ preventScroll: true });
  };

  const renderRow = (log: ParsedLog, i: number) => (
    <LogLine
      log={log}
      isExpanded={expandedKey === keys[i]}
      onToggle={onToggle}
      entryKey={keys[i]}
      source={showSource ? logSourceOf(log) : undefined}
      onTraceClick={onTraceClick}
      onSourceClick={showSource ? onSourceClick : undefined}
      searchQuery={searchQuery}
      wrap={wrap}
      relativeTime={relativeTime}
      timeAnchor={timeAnchor}
      showDate={showDate}
    />
  );

  const rowClass = (i: number) =>
    activeNavKey === keys[i]
      ? 'bg-surface-highlight/40 group-focus-within/grid:outline-2 group-focus-within/grid:-outline-offset-2 group-focus-within/grid:outline-primary'
      : undefined;

  return (
    <div className="relative flex-1 min-h-0 flex flex-col">
    <div
      ref={containerRef}
      onScroll={handleScroll}
      className="flex-1 overflow-auto bg-background/80 scrollbar-dark min-h-0"
      style={{ '--log-font-size': `${fontSize}px` } as React.CSSProperties}
    >
      {/* Loading state */}
      {phase === 'loading' && isLoading && logs.length === 0 && (
        <div className="flex items-center gap-2 p-4 text-text-muted text-xs">
          <div className="w-3 h-3 border-2 border-primary border-t-transparent rounded-full animate-spin" />
          Loading logs...
        </div>
      )}

      {/* Error state: nothing loaded. A failed refresh keeps the rows and
          the host renders a banner outside this scroll container. */}
      {phase === 'failed' && error && (
        <FailedState feature="Logs" message={error} onRetry={onRetry} inline />
      )}

      {/* Empty states, most specific filter first */}
      {(showData || (phase === 'loading' && !isLoading)) && logs.length === 0 && (
        traceFilter ? (
          <EmptyState
            icon={ScrollText}
            title="No logs for this trace"
            description="The entries expired from the buffer or were never correlated."
            action={
              onClearTrace && (
                <button onClick={onClearTrace} className="text-3xs text-primary hover:underline">
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
                  <button onClick={onShowAllSources} className="text-3xs text-primary hover:underline">
                    Show all sources
                  </button>
                )}
                {onClearFilter && (
                  <button onClick={onClearFilter} className="text-3xs text-primary hover:underline">
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
                <button onClick={onClearFilter} className="text-3xs text-primary hover:underline">
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
      {showData && logs.length > 0 && (
        virtualize ? (
          <>
            {header}
            <div
              role="grid"
              aria-label="Log entries"
              tabIndex={0}
              {...listNavProps}
              aria-activedescendant={navIndex >= 0 ? `log-row-${navIndex}` : undefined}
              className="group/grid relative w-full rounded focus-visible:-outline-offset-2"
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
            {...listNavProps}
            aria-activedescendant={navIndex >= 0 ? `log-row-${navIndex}` : undefined}
            className="group/grid divide-y divide-border/20 rounded focus-visible:-outline-offset-2"
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
    {/* Outside the scroller so it stays put; bottom-5 clears the
        horizontal scrollbar when wrap is off. */}
    {showJump && (
      <button
        type="button"
        onClick={onJumpClick}
        title="End or G"
        className="absolute bottom-5 left-1/2 -translate-x-1/2 z-raised flex items-center gap-1.5 h-7 px-3 rounded-full border border-border bg-surface-elevated text-text-primary text-3xs font-medium shadow-bevel hover:bg-surface-highlight transition-colors whitespace-nowrap animate-fade-in-up motion-reduce:animate-none"
      >
        <ArrowDown size={12} aria-hidden="true" />
        {jumpLabel}
      </button>
    )}
    </div>
  );
}
