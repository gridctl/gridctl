import { useEffect, useRef, useState, useCallback } from 'react';
import { ChevronDown, ChevronUp, Copy, Trash2, Pause, Play } from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { useUIStore } from '../../stores/useUIStore';
import { useSelectedNodeData } from '../../stores/useTopologyStore';
import { fetchAgentLogs } from '../../lib/api';
import { POLLING } from '../../lib/constants';
import type { NodeData } from '../../types';

export function BottomPanel() {
  const bottomPanelOpen = useUIStore((s) => s.bottomPanelOpen);
  const bottomPanelHeight = useUIStore((s) => s.bottomPanelHeight);
  const toggleBottomPanel = useUIStore((s) => s.toggleBottomPanel);

  const selectedData = useSelectedNodeData() as NodeData | undefined;

  const [logs, setLogs] = useState<string[]>([]);
  const [isPaused, setIsPaused] = useState(false);
  const [autoScroll, setAutoScroll] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const containerRef = useRef<HTMLDivElement>(null);
  const intervalRef = useRef<number | null>(null);

  // Get agent name from selected node (only for mcp-server and resource types)
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
      // Fallback for older browsers
      const textArea = document.createElement('textarea');
      textArea.value = logs.join('\n');
      document.body.appendChild(textArea);
      textArea.select();
      document.execCommand('copy');
      document.body.removeChild(textArea);
    }
  };

  // Collapsed header height
  const headerHeight = 36;

  return (
    <div
      className={cn(
        'bg-surface border-t border-border flex flex-col',
        'transition-all duration-200 ease-out'
      )}
      style={{ height: bottomPanelOpen ? bottomPanelHeight : headerHeight }}
    >
      {/* Header */}
      <div
        className="h-9 flex items-center justify-between px-3 border-b border-border cursor-pointer"
        onClick={toggleBottomPanel}
      >
        <div className="flex items-center gap-2">
          <button
            onClick={(e) => {
              e.stopPropagation();
              toggleBottomPanel();
            }}
            className="p-0.5 rounded hover:bg-surface-highlight transition-colors"
          >
            {bottomPanelOpen ? (
              <ChevronDown size={14} className="text-text-muted" />
            ) : (
              <ChevronUp size={14} className="text-text-muted" />
            )}
          </button>
          <span className="text-xs font-medium text-text-primary">
            {agentName ? `Logs: ${agentName}` : 'Logs'}
          </span>
          {isPaused && (
            <span className="text-[10px] px-1.5 py-0.5 bg-status-pending/20 text-status-pending rounded">
              Paused
            </span>
          )}
        </div>

        {bottomPanelOpen && agentName !== null && (
          <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
            <IconButton
              icon={isPaused ? Play : Pause}
              onClick={() => setIsPaused(!isPaused)}
              tooltip={isPaused ? 'Resume' : 'Pause'}
              size="sm"
              variant="ghost"
            />
            <IconButton
              icon={Copy}
              onClick={handleCopyLogs}
              tooltip="Copy Logs"
              size="sm"
              variant="ghost"
            />
            <IconButton
              icon={Trash2}
              onClick={handleClearLogs}
              tooltip="Clear Logs"
              size="sm"
              variant="ghost"
            />
          </div>
        )}
      </div>

      {/* Content */}
      {bottomPanelOpen && (
        <div
          ref={containerRef}
          onScroll={handleScroll}
          className="flex-1 overflow-auto font-mono text-xs p-3 bg-background scrollbar-dark"
        >
          {!agentName && (
            <div className="h-full flex items-center justify-center text-text-muted">
              Select a node to view logs
            </div>
          )}

          {agentName !== null && isLoading && (
            <div className="text-text-muted">Loading logs...</div>
          )}

          {agentName !== null && error && (
            <div className="text-status-error">Error: {error}</div>
          )}

          {agentName !== null && !isLoading && !error && logs.length === 0 && (
            <div className="text-text-muted">No logs available</div>
          )}

          {agentName !== null && logs.map((line, i) => (
            <div
              key={i}
              className={cn(
                'py-0.5 whitespace-pre-wrap break-all',
                line.includes('ERROR') && 'text-status-error',
                line.includes('WARN') && 'text-status-pending',
                line.includes('INFO') && 'text-primary',
                !line.includes('ERROR') &&
                  !line.includes('WARN') &&
                  !line.includes('INFO') &&
                  'text-text-muted'
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
