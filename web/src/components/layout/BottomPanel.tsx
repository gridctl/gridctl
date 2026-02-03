import { useEffect, useRef, useState, useCallback, useMemo } from 'react';
import {
  ChevronDown,
  ChevronUp,
  Copy,
  Trash2,
  Pause,
  Play,
  Terminal,
  Search,
  Radio,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { PopoutButton } from '../ui/PopoutButton';
import { LogLine, LevelFilter, ZoomControls, parseLogEntry, type LogLevel, type ParsedLog } from '../log';
import { useUIStore } from '../../stores/useUIStore';
import { useSelectedNodeData } from '../../stores/useStackStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { useLogFontSize } from '../../hooks/useLogFontSize';
import { fetchAgentLogs, fetchGatewayLogs } from '../../lib/api';
import { POLLING } from '../../lib/constants';
import type { NodeData } from '../../types';

export function BottomPanel() {
  const bottomPanelOpen = useUIStore((s) => s.bottomPanelOpen);
  const toggleBottomPanel = useUIStore((s) => s.toggleBottomPanel);
  const logsDetached = useUIStore((s) => s.logsDetached);
  const { openDetachedWindow } = useWindowManager();

  const selectedData = useSelectedNodeData() as NodeData | undefined;

  const [logs, setLogs] = useState<ParsedLog[]>([]);
  const [isPaused, setIsPaused] = useState(false);
  const [autoScroll, setAutoScroll] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Filter state
  const [searchQuery, setSearchQuery] = useState('');
  const [enabledLevels, setEnabledLevels] = useState<Set<LogLevel>>(
    new Set(['ERROR', 'WARN', 'INFO', 'DEBUG'])
  );
  const [expandedIndex, setExpandedIndex] = useState<number | null>(null);

  const containerRef = useRef<HTMLDivElement>(null);
  const intervalRef = useRef<number | null>(null);

  // Log font size with Ctrl+Scroll zoom
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } =
    useLogFontSize(containerRef);

  // Determine log source: gateway or agent
  const isGateway = selectedData?.type === 'gateway';
  const agentName: string | null =
    selectedData && selectedData.type !== 'gateway' ? selectedData.name : null;
  const hasSource = isGateway || agentName !== null;

  const fetchLogs = useCallback(async () => {
    try {
      if (isGateway) {
        const entries = await fetchGatewayLogs(500);
        setLogs((entries ?? []).map(parseLogEntry));
      } else if (agentName) {
        const lines = await fetchAgentLogs(agentName, 500);
        setLogs((lines ?? []).map(parseLogEntry));
      }
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch logs');
    } finally {
      setIsLoading(false);
    }
  }, [isGateway, agentName]);

  // Reset logs when source changes
  useEffect(() => {
    setLogs([]);
    setError(null);
    setExpandedIndex(null);
    setSearchQuery('');
    if (hasSource) {
      setIsLoading(true);
      fetchLogs();
    } else {
      setIsLoading(false);
    }
  }, [hasSource, isGateway, agentName, fetchLogs]);

  // Polling for logs
  useEffect(() => {
    if (!hasSource || isPaused || !bottomPanelOpen) {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
      return;
    }

    intervalRef.current = window.setInterval(fetchLogs, POLLING.LOGS);

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
        intervalRef.current = null;
      }
    };
  }, [hasSource, isPaused, bottomPanelOpen, fetchLogs]);

  // Filter logs
  const filteredLogs = useMemo(() => {
    return (logs ?? []).filter((log) => {
      if (!enabledLevels.has(log.level)) return false;

      if (searchQuery) {
        const query = searchQuery.toLowerCase();
        return (
          log.message.toLowerCase().includes(query) ||
          log.component?.toLowerCase().includes(query) ||
          log.traceId?.toLowerCase().includes(query)
        );
      }

      return true;
    });
  }, [logs, enabledLevels, searchQuery]);

  // Auto-scroll to bottom
  useEffect(() => {
    if (autoScroll && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [filteredLogs, autoScroll]);

  // Detect manual scroll
  const handleScroll = () => {
    if (!containerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current;
    setAutoScroll(scrollHeight - scrollTop - clientHeight < 50);
  };

  const handleClearLogs = () => {
    setLogs([]);
    setExpandedIndex(null);
  };

  const handleCopyLogs = async () => {
    const text = filteredLogs.map((log) => log.raw).join('\n');
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      const textArea = document.createElement('textarea');
      textArea.value = text;
      document.body.appendChild(textArea);
      textArea.select();
      document.execCommand('copy');
      document.body.removeChild(textArea);
    }
  };

  const toggleLevel = (level: LogLevel) => {
    setEnabledLevels((prev) => {
      const next = new Set(prev);
      if (next.has(level)) {
        next.delete(level);
      } else {
        next.add(level);
      }
      return next;
    });
  };

  const sourceName = isGateway ? 'Gateway' : agentName;

  return (
    <div
      className={cn(
        'h-full w-full',
        'bg-surface/90 backdrop-blur-xl border-t border-border/50',
        'flex flex-col relative',
        'transition-all duration-300 ease-out'
      )}
    >
      {/* Top accent line */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/20 to-transparent" />

      {/* Header - Always visible */}
      <div
        className="h-10 flex-shrink-0 flex items-center justify-between px-4 cursor-pointer hover:bg-surface-highlight/30 transition-colors"
        onClick={toggleBottomPanel}
      >
        <div className="flex items-center gap-3">
          <button
            onClick={(e) => {
              e.stopPropagation();
              toggleBottomPanel();
            }}
            className="p-1 rounded-md hover:bg-surface-highlight transition-colors"
          >
            {bottomPanelOpen ? (
              <ChevronDown size={14} className="text-text-muted" />
            ) : (
              <ChevronUp size={14} className="text-text-muted" />
            )}
          </button>
          <div className="flex items-center gap-2">
            <div
              className={cn(
                'p-1 rounded-md',
                isGateway ? 'bg-primary/10' : 'bg-tertiary/10'
              )}
            >
              {isGateway ? (
                <Radio size={12} className="text-primary" />
              ) : (
                <Terminal size={12} className="text-tertiary" />
              )}
            </div>
            <span className="text-xs font-medium text-text-primary">
              {sourceName ? `Logs: ${sourceName}` : 'Logs'}
            </span>
            {isGateway && (
              <span className="text-[10px] px-1.5 py-0.5 bg-primary/10 text-primary rounded font-medium border border-primary/20">
                Structured
              </span>
            )}
          </div>
          {isPaused && (
            <span className="text-[10px] px-2 py-0.5 bg-status-pending/15 text-status-pending rounded-full font-medium border border-status-pending/20">
              Paused
            </span>
          )}
          {!autoScroll && !isPaused && (
            <span className="text-[10px] px-2 py-0.5 bg-surface-highlight text-text-muted rounded-full font-medium border border-border/30">
              Scrolled
            </span>
          )}
        </div>

        <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
          {hasSource && (
            <>
              <IconButton
                icon={isPaused ? Play : Pause}
                onClick={() => setIsPaused(!isPaused)}
                tooltip={isPaused ? 'Resume' : 'Pause'}
                size="sm"
                variant="ghost"
                className={isPaused ? 'text-status-running hover:text-status-running' : ''}
              />
              <IconButton icon={Copy} onClick={handleCopyLogs} tooltip="Copy Logs" size="sm" variant="ghost" />
              <IconButton
                icon={Trash2}
                onClick={handleClearLogs}
                tooltip="Clear Logs"
                size="sm"
                variant="ghost"
                className="hover:text-status-error"
              />
              <div className="w-px h-4 bg-border/50 mx-0.5" />
            </>
          )}
          <PopoutButton
            onClick={() => openDetachedWindow('logs', agentName ? `agent=${encodeURIComponent(agentName)}` : undefined)}
            tooltip="Open in new window"
            disabled={logsDetached}
          />
        </div>
      </div>

      {/* Content - Only visible when panel is open */}
      {bottomPanelOpen && (
        <>
          {/* Filter bar */}
          {hasSource && (
            <div className="flex items-center gap-2 px-4 py-2 border-b border-border/30 bg-surface-elevated/30">
              {/* Search input */}
              <div className="relative flex-1 max-w-xs">
                <Search
                  size={12}
                  className="absolute left-2.5 top-1/2 -translate-y-1/2 text-text-muted"
                />
                <input
                  type="text"
                  placeholder="Filter logs..."
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  className={cn(
                    'w-full pl-7 pr-3 py-1.5 text-xs font-mono',
                    'bg-background/60 border border-border/50 rounded-md',
                    'text-text-primary placeholder:text-text-muted',
                    'focus:outline-none focus:border-primary/50 focus:ring-1 focus:ring-primary/20',
                    'transition-all duration-200'
                  )}
                />
              </div>

              {/* Level filter */}
              <LevelFilter enabledLevels={enabledLevels} onToggle={toggleLevel} />

              {/* Zoom controls */}
              <ZoomControls
                fontSize={fontSize}
                onZoomIn={zoomIn}
                onZoomOut={zoomOut}
                onReset={resetZoom}
                isMin={isMin}
                isMax={isMax}
                isDefault={isDefault}
              />

              {/* Log count */}
              <span className="text-[10px] text-text-muted font-mono ml-auto">
                {filteredLogs.length} / {(logs ?? []).length} entries
              </span>
            </div>
          )}

          {/* Log content */}
          <div
            ref={containerRef}
            onScroll={handleScroll}
            className="flex-1 overflow-auto bg-background/80 scrollbar-dark min-h-0"
            style={{ '--log-font-size': `${fontSize}px` } as React.CSSProperties}
          >
            {/* Empty state */}
            {!hasSource && (
              <div className="h-full flex flex-col items-center justify-center text-text-muted gap-2">
                <Terminal size={24} className="text-text-muted/50" />
                <span className="text-xs">Select a node to view logs</span>
              </div>
            )}

            {/* Loading state */}
            {hasSource && isLoading && (
              <div className="flex items-center gap-2 p-4 text-text-muted text-xs">
                <div className="w-3 h-3 border-2 border-primary border-t-transparent rounded-full animate-spin" />
                Loading logs...
              </div>
            )}

            {/* Error state */}
            {hasSource && error && (
              <div className="flex items-center gap-2 p-4 text-status-error text-xs">
                <span className="w-2 h-2 rounded-full bg-status-error" />
                Error: {error}
              </div>
            )}

            {/* No logs state */}
            {hasSource && !isLoading && !error && (filteredLogs?.length ?? 0) === 0 && (
              <div className="flex flex-col items-center justify-center h-full text-text-muted text-xs gap-2">
                <Terminal size={20} className="text-text-muted/30" />
                <span>No logs available</span>
                {searchQuery && (
                  <button
                    onClick={() => setSearchQuery('')}
                    className="text-primary hover:underline"
                  >
                    Clear filter
                  </button>
                )}
              </div>
            )}

            {/* Log entries */}
            {hasSource && !isLoading && !error && (filteredLogs?.length ?? 0) > 0 && (
              <div className="divide-y divide-border/20">
                {(filteredLogs ?? []).map((log, i) => (
                  <LogLine
                    key={i}
                    log={log}
                    isExpanded={expandedIndex === i}
                    onToggle={() => setExpandedIndex(expandedIndex === i ? null : i)}
                  />
                ))}
              </div>
            )}
          </div>
        </>
      )}
    </div>
  );
}
