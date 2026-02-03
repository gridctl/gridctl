import { useEffect, useRef, useState, useCallback } from 'react';
import { ChevronDown, ChevronUp, Copy, Trash2, Pause, Play, Terminal } from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { PopoutButton } from '../ui/PopoutButton';
import { useUIStore } from '../../stores/useUIStore';
import { useSelectedNodeData } from '../../stores/useStackStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { fetchAgentLogs } from '../../lib/api';
import { POLLING } from '../../lib/constants';
import type { NodeData } from '../../types';

export function BottomPanel() {
  const bottomPanelOpen = useUIStore((s) => s.bottomPanelOpen);
  const toggleBottomPanel = useUIStore((s) => s.toggleBottomPanel);
  const logsDetached = useUIStore((s) => s.logsDetached);
  const { openDetachedWindow } = useWindowManager();

  const selectedData = useSelectedNodeData() as NodeData | undefined;

  const [logs, setLogs] = useState<string[]>([]);
  const [isPaused, setIsPaused] = useState(false);
  const [autoScroll, setAutoScroll] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const containerRef = useRef<HTMLDivElement>(null);
  const intervalRef = useRef<number | null>(null);

  // Get agent name from selected node
  const agentName: string | null = selectedData && selectedData.type !== 'gateway' ? selectedData.name : null;

  const fetchLogs = useCallback(async () => {
    if (!agentName) return;

    try {
      const newLogs = await fetchAgentLogs(agentName, 500);
      setLogs(newLogs);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch logs');
    } finally {
      setIsLoading(false);
    }
  }, [agentName]);

  // Reset logs when agent changes
  useEffect(() => {
    setLogs([]);
    setError(null);
    setIsLoading(true);
    if (agentName) {
      fetchLogs();
    } else {
      setIsLoading(false);
    }
  }, [agentName, fetchLogs]);

  // Polling for logs
  useEffect(() => {
    if (!agentName || isPaused || !bottomPanelOpen) {
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
  }, [agentName, isPaused, bottomPanelOpen, fetchLogs]);

  // Auto-scroll to bottom
  useEffect(() => {
    if (autoScroll && containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [logs, autoScroll]);

  // Detect manual scroll
  const handleScroll = () => {
    if (!containerRef.current) return;
    const { scrollTop, scrollHeight, clientHeight } = containerRef.current;
    setAutoScroll(scrollHeight - scrollTop - clientHeight < 50);
  };

  const handleClearLogs = () => {
    setLogs([]);
  };

  const handleCopyLogs = async () => {
    try {
      await navigator.clipboard.writeText(logs.join('\n'));
    } catch {
      const textArea = document.createElement('textarea');
      textArea.value = logs.join('\n');
      document.body.appendChild(textArea);
      textArea.select();
      document.execCommand('copy');
      document.body.removeChild(textArea);
    }
  };

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
            <div className="p-1 rounded-md bg-primary/10">
              <Terminal size={12} className="text-primary" />
            </div>
            <span className="text-xs font-medium text-text-primary">
              {agentName ? `Logs: ${agentName}` : 'Logs'}
            </span>
          </div>
          {isPaused && (
            <span className="text-[10px] px-2 py-0.5 bg-status-pending/15 text-status-pending rounded-full font-medium border border-status-pending/20">
              Paused
            </span>
          )}
        </div>

        <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
          {agentName !== null && (
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
        <div
          ref={containerRef}
          onScroll={handleScroll}
          className="flex-1 overflow-auto font-mono text-xs p-4 bg-background/80 scrollbar-dark min-h-0"
        >
          {!agentName && (
            <div className="h-full flex flex-col items-center justify-center text-text-muted gap-2">
              <Terminal size={24} className="text-text-muted/50" />
              <span>Select a node to view logs</span>
            </div>
          )}

          {agentName !== null && isLoading && (
            <div className="flex items-center gap-2 text-text-muted">
              <div className="w-3 h-3 border-2 border-primary border-t-transparent rounded-full animate-spin" />
              Loading logs...
            </div>
          )}

          {agentName !== null && error && (
            <div className="flex items-center gap-2 text-status-error">
              <span className="w-2 h-2 rounded-full bg-status-error" />
              Error: {error}
            </div>
          )}

          {agentName !== null && !isLoading && !error && (logs?.length ?? 0) === 0 && (
            <div className="text-text-muted">No logs available</div>
          )}

          {agentName !== null &&
            (logs ?? []).map((line, i) => (
              <div
                key={i}
                className={cn(
                  'py-0.5 whitespace-pre-wrap break-all leading-relaxed',
                  line.includes('ERROR') && 'text-status-error',
                  line.includes('WARN') && 'text-status-pending',
                  line.includes('INFO') && 'text-primary',
                  !line.includes('ERROR') && !line.includes('WARN') && !line.includes('INFO') && 'text-text-muted'
                )}
              >
                {line}
              </div>
            ))}
        </div>
      )}
    </div>
  );
}
