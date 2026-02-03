import { useEffect, useRef, useState, useCallback, useMemo, Component, type ReactNode } from 'react';
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
  Search,
  Filter,
  ChevronRight,
  Check,
  Radio,
} from 'lucide-react';
import { cn } from '../lib/cn';
import { IconButton } from '../components/ui/IconButton';
import { fetchAgentLogs, fetchGatewayLogs, fetchStatus, type LogEntry } from '../lib/api';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { POLLING } from '../lib/constants';
import type { GatewayStatus } from '../types';

// Log level configuration
type LogLevel = 'DEBUG' | 'INFO' | 'WARN' | 'ERROR';

const LOG_LEVELS: LogLevel[] = ['ERROR', 'WARN', 'INFO', 'DEBUG'];

const LEVEL_STYLES: Record<LogLevel, { text: string; bg: string; border: string; dot: string }> = {
  ERROR: {
    text: 'text-status-error',
    bg: 'bg-status-error/10',
    border: 'border-status-error/30',
    dot: 'bg-status-error',
  },
  WARN: {
    text: 'text-status-pending',
    bg: 'bg-status-pending/10',
    border: 'border-status-pending/30',
    dot: 'bg-status-pending',
  },
  INFO: {
    text: 'text-primary',
    bg: 'bg-primary/10',
    border: 'border-primary/30',
    dot: 'bg-primary',
  },
  DEBUG: {
    text: 'text-text-muted',
    bg: 'bg-surface-highlight',
    border: 'border-border/30',
    dot: 'bg-text-muted',
  },
};

// Parse log entry from string or JSON
interface ParsedLog {
  level: LogLevel;
  timestamp: string;
  message: string;
  component?: string;
  traceId?: string;
  attrs?: Record<string, unknown>;
  raw: string;
}

function parseLogEntry(input: string | LogEntry): ParsedLog {
  // If it's already a structured entry
  if (typeof input === 'object') {
    return {
      level: (input.level?.toUpperCase() as LogLevel) || 'INFO',
      timestamp: input.ts || '',
      message: input.msg || '',
      component: input.component,
      traceId: input.trace_id,
      attrs: input.attrs,
      raw: JSON.stringify(input, null, 2),
    };
  }

  // Try to parse as JSON
  try {
    const parsed = JSON.parse(input);
    return {
      level: (parsed.level?.toUpperCase() as LogLevel) || 'INFO',
      timestamp: parsed.ts || parsed.time || parsed.timestamp || '',
      message: parsed.msg || parsed.message || '',
      component: parsed.component || parsed.logger,
      traceId: parsed.trace_id || parsed.traceId,
      attrs: parsed,
      raw: input,
    };
  } catch {
    // Fall back to text parsing
    const level: LogLevel = input.includes('ERROR')
      ? 'ERROR'
      : input.includes('WARN')
        ? 'WARN'
        : input.includes('INFO')
          ? 'INFO'
          : 'DEBUG';

    return {
      level,
      timestamp: '',
      message: input,
      raw: input,
    };
  }
}

function formatTimestamp(ts: string): string {
  if (!ts) return '';
  try {
    const date = new Date(ts);
    return date.toLocaleTimeString('en-US', {
      hour12: false,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    }) + '.' + String(date.getMilliseconds()).padStart(3, '0');
  } catch {
    return ts.slice(11, 23); // Fallback: extract time portion
  }
}

// Level filter dropdown component
function LevelFilter({
  enabledLevels,
  onToggle,
}: {
  enabledLevels: Set<LogLevel>;
  onToggle: (level: LogLevel) => void;
}) {
  const [isOpen, setIsOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  // Close on outside click
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const activeCount = enabledLevels.size;

  return (
    <div className="relative" ref={dropdownRef}>
      <button
        onClick={() => setIsOpen(!isOpen)}
        className={cn(
          'flex items-center gap-1.5 px-2 py-1 rounded-md text-xs',
          'border transition-all duration-200',
          activeCount < 4
            ? 'bg-primary/10 border-primary/30 text-primary'
            : 'bg-surface-elevated/60 border-border/50 text-text-muted hover:text-text-primary hover:border-text-muted/30'
        )}
      >
        <Filter size={12} />
        <span>Level</span>
        {activeCount < 4 && (
          <span className="px-1.5 py-0.5 bg-primary/20 rounded text-[10px] font-medium">
            {activeCount}
          </span>
        )}
      </button>

      {isOpen && (
        <div className="absolute top-full left-0 mt-1 z-50 min-w-[140px] py-1 rounded-lg bg-surface-elevated border border-border/50 shadow-xl backdrop-blur-xl">
          {LOG_LEVELS.map((level) => {
            const enabled = enabledLevels.has(level);
            const styles = LEVEL_STYLES[level];
            return (
              <button
                key={level}
                onClick={() => onToggle(level)}
                className={cn(
                  'w-full flex items-center gap-2 px-3 py-1.5 text-xs transition-colors',
                  'hover:bg-surface-highlight',
                  enabled ? styles.text : 'text-text-muted'
                )}
              >
                <span
                  className={cn(
                    'w-4 h-4 rounded flex items-center justify-center border',
                    enabled ? `${styles.bg} ${styles.border}` : 'border-border/50'
                  )}
                >
                  {enabled && <Check size={10} />}
                </span>
                <span className={cn('w-2 h-2 rounded-full', styles.dot)} />
                <span className="font-medium">{level}</span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

// Expandable log line component
function LogLine({
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
          'grid gap-2 px-3 py-1 cursor-pointer',
          'grid-cols-[90px_50px_80px_1fr_20px]'
        )}
        onClick={hasDetails ? onToggle : undefined}
      >
        {/* Timestamp */}
        <span className="text-text-muted font-mono text-[11px] tabular-nums">
          {formatTimestamp(log.timestamp)}
        </span>

        {/* Level badge */}
        <span
          className={cn(
            'inline-flex items-center justify-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-semibold uppercase tracking-wide',
            styles.bg,
            styles.text
          )}
        >
          <span className={cn('w-1 h-1 rounded-full', styles.dot)} />
          {log.level.slice(0, 4)}
        </span>

        {/* Component */}
        <span className="text-secondary font-mono text-[11px] truncate" title={log.component}>
          {log.component || '—'}
        </span>

        {/* Message */}
        <span
          className={cn(
            'font-mono text-[11px] truncate',
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
          <div className="p-2 rounded-md bg-background/60 border border-border/30 font-mono text-[10px]">
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
  type: 'gateway' | 'mcp-server' | 'agent' | 'resource';
}

function DetachedLogsPageContent() {
  const [searchParams, setSearchParams] = useSearchParams();
  const initialAgent = searchParams.get('agent');

  const [logs, setLogs] = useState<ParsedLog[]>([]);
  const [isPaused, setIsPaused] = useState(false);
  const [autoScroll, setAutoScroll] = useState(true);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selectedAgent, setSelectedAgent] = useState<string | null>(initialAgent);
  const [nodes, setNodes] = useState<NodeOption[]>([]);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [dropdownOpen, setDropdownOpen] = useState(false);

  // Filter state
  const [searchQuery, setSearchQuery] = useState('');
  const [enabledLevels, setEnabledLevels] = useState<Set<LogLevel>>(
    new Set(['ERROR', 'WARN', 'INFO', 'DEBUG'])
  );
  const [expandedIndex, setExpandedIndex] = useState<number | null>(null);

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
          // Gateway option at the top
          { name: 'Gateway', type: 'gateway' as const },
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

  // Determine if selected is gateway
  const isGateway = selectedAgent === 'Gateway';

  const fetchLogs = useCallback(async () => {
    if (!selectedAgent) return;

    try {
      if (isGateway) {
        const entries = await fetchGatewayLogs(500);
        setLogs((entries ?? []).map(parseLogEntry));
      } else {
        const lines = await fetchAgentLogs(selectedAgent, 500);
        setLogs((lines ?? []).map(parseLogEntry));
      }
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to fetch logs');
    } finally {
      setIsLoading(false);
    }
  }, [selectedAgent, isGateway]);

  // Reset logs when agent changes
  useEffect(() => {
    setLogs([]);
    setError(null);
    setExpandedIndex(null);
    setSearchQuery('');
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

  // Filter logs
  const filteredLogs = useMemo(() => {
    return (logs ?? []).filter((log) => {
      // Level filter
      if (!enabledLevels.has(log.level)) return false;

      // Search filter
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
          <div className={cn(
            'p-1.5 rounded-lg border',
            isGateway ? 'bg-primary/10 border-primary/20' : 'bg-tertiary/10 border-tertiary/20'
          )}>
            {isGateway ? (
              <Radio size={14} className="text-primary" />
            ) : (
              <Terminal size={14} className="text-tertiary" />
            )}
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
                          node.type === 'gateway' && 'bg-primary',
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

          {isGateway && selectedAgent && (
            <span className="text-[10px] px-1.5 py-0.5 bg-primary/10 text-primary rounded font-medium border border-primary/20">
              Structured
            </span>
          )}
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

      {/* Filter bar */}
      {selectedAgent && (
        <div className="flex items-center gap-2 px-4 py-2 border-b border-border/30 bg-surface-elevated/30 flex-shrink-0">
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

          {/* Log count */}
          <span className="text-[10px] text-text-muted font-mono ml-auto">
            {filteredLogs.length} / {(logs ?? []).length} entries
          </span>
        </div>
      )}

      {/* Log content */}
      <main
        ref={containerRef}
        onScroll={handleScroll}
        className="flex-1 overflow-auto bg-background scrollbar-dark min-h-0"
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
          <div className="flex items-center gap-2 p-4 text-text-muted text-xs animate-fade-in-up">
            <div className="w-3 h-3 border-2 border-primary border-t-transparent rounded-full animate-spin" />
            Loading logs...
          </div>
        )}

        {selectedAgent && error && (
          <div className="flex items-center gap-2 p-4 text-status-error text-xs animate-fade-in-up">
            <span className="w-2 h-2 rounded-full bg-status-error animate-pulse" />
            Error: {error}
          </div>
        )}

        {selectedAgent && !isLoading && !error && (filteredLogs?.length ?? 0) === 0 && (
          <div className="flex flex-col items-center justify-center h-full text-text-muted text-xs gap-2 animate-fade-in-up">
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

        {selectedAgent && !isLoading && !error && (filteredLogs?.length ?? 0) > 0 && (
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
      </main>

      {/* Footer status bar */}
      <footer className="h-6 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-t border-border/50 flex items-center justify-between px-4 text-[10px] text-text-muted">
        <span>
          {filteredLogs.length} / {(logs ?? []).length} entries {isPaused ? '(paused)' : ''}
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
