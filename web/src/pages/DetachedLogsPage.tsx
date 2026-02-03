import { useEffect, useRef, useState, useCallback, Component, type ReactNode } from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  Terminal,
  Copy,
  Trash2,
  Pause,
  Play,
  ChevronDown,
  RefreshCw,
  Maximize2,
  Minimize2,
  AlertCircle,
} from 'lucide-react';
import { cn } from '../lib/cn';
import { IconButton } from '../components/ui/IconButton';
import { fetchAgentLogs, fetchStatus } from '../lib/api';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { POLLING } from '../lib/constants';
import type { GatewayStatus } from '../types';

// Error boundary for detached window
interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

class DetachedErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="h-screen w-screen bg-background flex items-center justify-center">
          <div className="text-center p-8 max-w-md">
            <div className="p-4 rounded-xl bg-status-error/10 border border-status-error/20 inline-block mb-4">
              <AlertCircle size={32} className="text-status-error" />
            </div>
            <h1 className="text-lg text-status-error mb-2">Something went wrong</h1>
            <pre className="text-xs text-text-muted bg-surface p-4 rounded-lg overflow-auto max-h-32 mb-4">
              {this.state.error?.message}
            </pre>
            <button
              onClick={() => window.location.reload()}
              className="px-4 py-2 bg-primary text-background rounded-lg font-medium hover:bg-primary-light transition-colors"
            >
              Reload Window
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

interface NodeOption {
  name: string;
  type: 'mcp-server' | 'agent' | 'resource';
}

function DetachedLogsPageContent() {
  const [searchParams, setSearchParams] = useSearchParams();
  const initialAgent = searchParams.get('agent');

  const [logs, setLogs] = useState<string[]>([]);
  const [isPaused, setIsPaused] = useState(false);
  const [autoScroll, setAutoScroll] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedAgent, setSelectedAgent] = useState<string | null>(initialAgent);
  const [nodes, setNodes] = useState<NodeOption[]>([]);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [dropdownOpen, setDropdownOpen] = useState(false);

  const containerRef = useRef<HTMLDivElement>(null);
  const intervalRef = useRef<number | null>(null);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Register with main window
  useDetachedWindowSync('logs');

  // Fetch available nodes
  useEffect(() => {
    const fetchNodes = async () => {
      try {
        const status: GatewayStatus = await fetchStatus();
        const nodeList: NodeOption[] = [
          ...(status['mcp-servers'] ?? []).map((s) => ({ name: s.name, type: 'mcp-server' as const })),
          ...(status.agents ?? []).map((a) => ({ name: a.name, type: 'agent' as const })),
          ...(status.resources ?? []).map((r) => ({ name: r.name, type: 'resource' as const })),
        ];
        setNodes(nodeList);
      } catch {
        // Ignore errors fetching nodes
      }
    };

    fetchNodes();
    const nodeInterval = window.setInterval(fetchNodes, POLLING.STATUS);

    return () => clearInterval(nodeInterval);
  }, []);

  // Close dropdown when clicking outside
  useEffect(() => {
    const handleClickOutside = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setDropdownOpen(false);
      }
    };

    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const fetchLogs = useCallback(async () => {
    if (!selectedAgent) return;

    try {
      const newLogs = await fetchAgentLogs(selectedAgent, 500);
      setLogs(newLogs);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch logs');
    } finally {
      setIsLoading(false);
    }
  }, [selectedAgent]);

  // Reset logs when agent changes
  useEffect(() => {
    setLogs([]);
    setError(null);
    setIsLoading(true);
    if (selectedAgent) {
      fetchLogs();
      // Update URL
      setSearchParams({ agent: selectedAgent });
    } else {
      setIsLoading(false);
      setSearchParams({});
    }
  }, [selectedAgent, fetchLogs, setSearchParams]);

  // Polling for logs
  useEffect(() => {
    if (!selectedAgent || isPaused) {
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
  }, [selectedAgent, isPaused, fetchLogs]);

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

  const handleClearLogs = () => setLogs([]);

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

  const toggleFullscreen = async () => {
    if (!document.fullscreenElement) {
      await document.documentElement.requestFullscreen();
      setIsFullscreen(true);
    } else {
      await document.exitFullscreen();
      setIsFullscreen(false);
    }
  };

  // Listen for fullscreen changes
  useEffect(() => {
    const handleFullscreenChange = () => {
      setIsFullscreen(!!document.fullscreenElement);
    };

    document.addEventListener('fullscreenchange', handleFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', handleFullscreenChange);
  }, []);

  const handleSelectAgent = (name: string) => {
    setSelectedAgent(name);
    setDropdownOpen(false);
  };

  return (
    <div className="h-screen w-screen bg-background flex flex-col overflow-hidden">
      {/* Background grain */}
      <div
        className="fixed inset-0 pointer-events-none z-0 opacity-[0.015]"
        style={{
          backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noise'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noise)'/%3E%3C/svg%3E")`,
        }}
      />

      {/* Header */}
      <header className="h-12 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-b border-border/50 flex items-center justify-between px-4 z-10 relative">
        {/* Top accent line */}
        <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/30 to-transparent" />

        <div className="flex items-center gap-3">
          <div className="p-1.5 rounded-lg bg-primary/10 border border-primary/20">
            <Terminal size={14} className="text-primary" />
          </div>

          {/* Agent selector dropdown */}
          <div className="relative" ref={dropdownRef}>
            <button
              onClick={() => setDropdownOpen(!dropdownOpen)}
              className={cn(
                'flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-all',
                'bg-surface-elevated/60 border border-border/50',
                'hover:bg-surface-highlight hover:border-text-muted/30',
                dropdownOpen && 'bg-surface-highlight border-text-muted/30'
              )}
            >
              <span className={cn(selectedAgent ? 'text-text-primary' : 'text-text-muted')}>
                {selectedAgent ?? 'Select node...'}
              </span>
              <ChevronDown
                size={14}
                className={cn(
                  'text-text-muted transition-transform duration-200',
                  dropdownOpen && 'rotate-180'
                )}
              />
            </button>

            {dropdownOpen && (
              <div className="absolute top-full left-0 mt-1 w-64 py-1 bg-surface-elevated/95 backdrop-blur-xl border border-border/50 rounded-lg shadow-lg z-50 animate-fade-in-scale">
                {(nodes ?? []).length === 0 ? (
                  <div className="px-3 py-2 text-xs text-text-muted">No nodes available</div>
                ) : (
                  (nodes ?? []).map((node) => (
                    <button
                      key={node.name}
                      onClick={() => handleSelectAgent(node.name)}
                      className={cn(
                        'w-full flex items-center gap-2 px-3 py-2 text-left text-sm transition-colors',
                        'hover:bg-surface-highlight',
                        selectedAgent === node.name && 'bg-primary/10 text-primary'
                      )}
                    >
                      <span
                        className={cn(
                          'w-1.5 h-1.5 rounded-full',
                          node.type === 'mcp-server' && 'bg-violet-400',
                          node.type === 'agent' && 'bg-tertiary',
                          node.type === 'resource' && 'bg-secondary'
                        )}
                      />
                      <span className="truncate">{node.name}</span>
                      <span className="ml-auto text-[10px] text-text-muted uppercase">
                        {node.type === 'mcp-server' ? 'server' : node.type}
                      </span>
                    </button>
                  ))
                )}
              </div>
            )}
          </div>

          {isPaused && (
            <span className="text-[10px] px-2 py-0.5 bg-status-pending/15 text-status-pending rounded-full font-medium border border-status-pending/20">
              Paused
            </span>
          )}
        </div>

        <div className="flex items-center gap-1">
          <IconButton
            icon={RefreshCw}
            onClick={fetchLogs}
            tooltip="Refresh"
            size="sm"
            variant="ghost"
            disabled={!selectedAgent}
          />
          <IconButton
            icon={isPaused ? Play : Pause}
            onClick={() => setIsPaused(!isPaused)}
            tooltip={isPaused ? 'Resume' : 'Pause'}
            size="sm"
            variant="ghost"
            className={isPaused ? 'text-status-running hover:text-status-running' : ''}
            disabled={!selectedAgent}
          />
          <IconButton
            icon={Copy}
            onClick={handleCopyLogs}
            tooltip="Copy Logs"
            size="sm"
            variant="ghost"
            disabled={!selectedAgent || (logs ?? []).length === 0}
          />
          <IconButton
            icon={Trash2}
            onClick={handleClearLogs}
            tooltip="Clear Logs"
            size="sm"
            variant="ghost"
            className="hover:text-status-error"
            disabled={!selectedAgent}
          />
          <div className="w-px h-4 bg-border/50 mx-1" />
          <IconButton
            icon={isFullscreen ? Minimize2 : Maximize2}
            onClick={toggleFullscreen}
            tooltip={isFullscreen ? 'Exit Fullscreen' : 'Fullscreen'}
            size="sm"
            variant="ghost"
          />
        </div>
      </header>

      {/* Log content */}
      <main
        ref={containerRef}
        onScroll={handleScroll}
        className="flex-1 overflow-auto font-mono text-xs p-4 bg-background scrollbar-dark"
      >
        {!selectedAgent && (
          <div className="h-full flex flex-col items-center justify-center text-text-muted gap-3 animate-fade-in-scale">
            <div className="p-4 rounded-xl bg-surface-elevated/50 border border-border/30">
              <Terminal size={32} className="text-text-muted/50" />
            </div>
            <span className="text-sm">Select a node to view logs</span>
          </div>
        )}

        {selectedAgent && isLoading && (
          <div className="flex items-center gap-2 text-text-muted animate-fade-in-up">
            <div className="w-3 h-3 border-2 border-primary border-t-transparent rounded-full animate-spin" />
            Loading logs...
          </div>
        )}

        {selectedAgent && error && (
          <div className="flex items-center gap-2 text-status-error animate-fade-in-up">
            <span className="w-2 h-2 rounded-full bg-status-error animate-pulse" />
            Error: {error}
          </div>
        )}

        {selectedAgent && !isLoading && !error && (logs?.length ?? 0) === 0 && (
          <div className="text-text-muted animate-fade-in-up">No logs available</div>
        )}

        {selectedAgent &&
          !isLoading &&
          !error &&
          (logs ?? []).map((line, i) => {
            // Ensure line is a string to prevent crashes
            const lineStr = typeof line === 'string' ? line : String(line ?? '');
            const hasError = lineStr.includes('ERROR');
            const hasWarn = lineStr.includes('WARN');
            const hasInfo = lineStr.includes('INFO');

            return (
              <div
                key={i}
                className={cn(
                  'py-0.5 whitespace-pre-wrap break-all leading-relaxed',
                  hasError && 'text-status-error',
                  hasWarn && !hasError && 'text-status-pending',
                  hasInfo && !hasError && !hasWarn && 'text-primary',
                  !hasError && !hasWarn && !hasInfo && 'text-text-muted'
                )}
              >
                {lineStr}
              </div>
            );
          })}
      </main>

      {/* Footer status bar */}
      <footer className="h-6 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-t border-border/50 flex items-center justify-between px-4 text-[10px] text-text-muted">
        <span>
          {(logs ?? []).length} lines {isPaused ? '(paused)' : ''}
        </span>
        <span className="flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse" />
          Detached Window
        </span>
      </footer>
    </div>
  );
}

// Export with error boundary wrapper
export function DetachedLogsPage() {
  return (
    <DetachedErrorBoundary>
      <DetachedLogsPageContent />
    </DetachedErrorBoundary>
  );
}
