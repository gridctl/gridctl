import { useEffect, useRef, useState, useCallback } from 'react';
import { X, Pause, Play, ArrowDown } from 'lucide-react';
import { cn } from '../../lib/cn';
import { fetchAgentLogs } from '../../lib/api';
import { POLLING } from '../../lib/constants';
import { IconButton } from './IconButton';

interface LogViewerProps {
  agentName: string;
  onClose: () => void;
}

export function LogViewer({ agentName, onClose }: LogViewerProps) {
  const [logs, setLogs] = useState<string[]>([]);
  const [isPaused, setIsPaused] = useState(false);
  const [autoScroll, setAutoScroll] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const containerRef = useRef<HTMLDivElement>(null);
  const intervalRef = useRef<number | null>(null);

  const fetchLogs = useCallback(async () => {
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

  // Initial fetch and polling
  useEffect(() => {
    fetchLogs();

    if (!isPaused) {
      intervalRef.current = window.setInterval(fetchLogs, POLLING.LOGS);
    }

    return () => {
      if (intervalRef.current) {
        clearInterval(intervalRef.current);
      }
    };
  }, [fetchLogs, isPaused]);

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

  const scrollToBottom = () => {
    setAutoScroll(true);
    containerRef.current?.scrollTo({
      top: containerRef.current.scrollHeight,
      behavior: 'smooth',
    });
  };

  return (
    <div className="absolute inset-0 bg-background flex flex-col z-10">
      {/* Header */}
      <div className="flex items-center justify-between p-3 border-b border-border bg-surface">
        <span className="font-medium text-sm text-text-primary">
          Logs: {agentName}
        </span>
        <div className="flex items-center gap-2">
          <IconButton
            icon={isPaused ? Play : Pause}
            onClick={() => setIsPaused(!isPaused)}
            tooltip={isPaused ? 'Resume' : 'Pause'}
            size="sm"
            variant="ghost"
          />
          <IconButton
            icon={X}
            onClick={onClose}
            tooltip="Close"
            size="sm"
            variant="ghost"
          />
        </div>
      </div>

      {/* Log Content */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="flex-1 overflow-auto font-mono text-xs p-3 bg-black scrollbar-dark"
      >
        {isLoading && (
          <div className="text-text-muted">Loading logs...</div>
        )}

        {error && (
          <div className="text-status-error">Error: {error}</div>
        )}

        {!isLoading && !error && (logs?.length ?? 0) === 0 && (
          <div className="text-text-muted">No logs available</div>
        )}

        {(logs ?? []).map((line, i) => (
          <div
            key={i}
            className={cn(
              'py-0.5 whitespace-pre-wrap break-all',
              line.includes('ERROR') && 'text-status-error',
              line.includes('WARN') && 'text-status-pending',
              line.includes('INFO') && 'text-text-secondary',
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

      {/* Auto-scroll indicator */}
      {!autoScroll && (
        <button
          onClick={scrollToBottom}
          className={cn(
            'absolute bottom-4 right-4 flex items-center gap-1.5',
            'px-3 py-1.5 bg-primary rounded-full text-xs font-medium',
            'shadow-lg hover:bg-primaryLight transition-colors'
          )}
        >
          <ArrowDown size={12} />
          Jump to bottom
        </button>
      )}

      {/* Pause indicator */}
      {isPaused && (
        <div className="absolute top-12 left-1/2 -translate-x-1/2 px-3 py-1 bg-status-pending/20 text-status-pending text-xs rounded-full">
          Paused
        </div>
      )}
    </div>
  );
}
