import { useEffect, useRef, useState } from 'react';
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
  Radio,
  Layers,
} from 'lucide-react';
import { cn } from '../lib/cn';
import { IconButton } from '../components/ui/IconButton';
import { GATEWAY_LOG_SOURCE, LogsView, useLogsView } from '../components/log';
import { fetchStatus } from '../lib/api';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { POLLING } from '../lib/constants';
import type { GatewayStatus } from '../types';
import { ErrorBoundary } from '../components/ui/ErrorBoundary';

interface NodeOption {
  name: string;
  type: 'mcp-server' | 'resource';
}

// Frameless logs popout. The log surface itself is the shared LogsView (same
// URL-synced ?agent=/?q=/?level=/?trace= semantics as the workspace, in this
// window's own URL); this page adds the window chrome: source dropdown,
// pause/copy/clear actions, fullscreen, and the footer. Trace pivots open the
// Traces workspace in a full app tab since the popout has no other workspaces.
function DetachedLogsPageContent() {
  const view = useLogsView();
  const { source } = view;

  const [nodes, setNodes] = useState<NodeOption[]>([]);
  const [isFullscreen, setIsFullscreen] = useState(false);
  const [dropdownOpen, setDropdownOpen] = useState(false);

  const dropdownRef = useRef<HTMLDivElement>(null);

  // Register with main window
  useDetachedWindowSync('logs');

  // Fetch available nodes for the source picker
  useEffect(() => {
    const fetchNodes = async () => {
      try {
        const status: GatewayStatus = await fetchStatus();
        const nodeList: NodeOption[] = [
          ...(status['mcp-servers'] ?? []).map((s) => ({ name: s.name, type: 'mcp-server' as const })),
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

  const isGateway = source === GATEWAY_LOG_SOURCE;
  const sourceLabel = source == null ? 'All sources' : isGateway ? 'Gateway' : source;

  const handleSelectSource = (next: string | null) => {
    view.setSource(next);
    setDropdownOpen(false);
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
            source == null
              ? 'bg-surface-elevated/60 border-border/50'
              : isGateway
                ? 'bg-primary/10 border-primary/20'
                : 'bg-tertiary/10 border-tertiary/20'
          )}>
            {source == null ? (
              <Layers size={14} className="text-text-secondary" />
            ) : isGateway ? (
              <Radio size={14} className="text-primary" />
            ) : (
              <Terminal size={14} className="text-tertiary" />
            )}
          </div>

          {/* Source selector dropdown */}
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
              <span className="text-text-primary">{sourceLabel}</span>
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
                <SourceOption
                  label="All sources"
                  dotClass="bg-text-muted"
                  tag="all"
                  selected={source == null}
                  onSelect={() => handleSelectSource(null)}
                />
                <SourceOption
                  label="Gateway"
                  dotClass="bg-primary"
                  tag="gateway"
                  selected={isGateway}
                  onSelect={() => handleSelectSource(GATEWAY_LOG_SOURCE)}
                />
                {(nodes ?? []).map((node) => (
                  <SourceOption
                    key={node.name}
                    label={node.name}
                    dotClass={node.type === 'mcp-server' ? 'bg-violet-400' : 'bg-secondary'}
                    tag={node.type === 'mcp-server' ? 'server' : node.type}
                    selected={source === node.name}
                    onSelect={() => handleSelectSource(node.name)}
                  />
                ))}
              </div>
            )}
          </div>

          {isGateway && (
            <span className="text-[10px] px-1.5 py-0.5 bg-primary/10 text-primary rounded font-medium border border-primary/20">
              Structured
            </span>
          )}
          {view.isPaused && (
            <span className="text-[10px] px-2 py-0.5 bg-status-pending/15 text-status-pending rounded-full font-medium border border-status-pending/20">
              Paused
            </span>
          )}
        </div>

        <div className="flex items-center gap-1">
          <IconButton
            icon={RefreshCw}
            onClick={view.refresh}
            tooltip="Refresh"
            size="sm"
            variant="ghost"
          />
          <IconButton
            icon={view.isPaused ? Play : Pause}
            onClick={view.togglePause}
            tooltip={view.isPaused ? 'Resume' : 'Pause'}
            size="sm"
            variant="ghost"
            className={view.isPaused ? 'text-status-running hover:text-status-running' : ''}
          />
          <IconButton
            icon={Copy}
            onClick={view.copyFiltered}
            tooltip="Copy Logs"
            size="sm"
            variant="ghost"
            disabled={view.filteredLogs.length === 0}
          />
          <IconButton
            icon={Trash2}
            onClick={view.clear}
            tooltip="Clear Logs"
            size="sm"
            variant="ghost"
            className="hover:text-status-error"
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

      {/* Log surface — shared with the workspace */}
      <LogsView
        view={view}
        onTraceClick={(traceId) =>
          window.open(`/traces?trace=${encodeURIComponent(traceId)}`, '_blank', 'noopener')
        }
        emptyText="No logs available"
      />

      {/* Footer status bar */}
      <footer className="h-6 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-t border-border/50 flex items-center justify-between px-4 text-[10px] text-text-muted">
        <span>
          {view.filteredLogs.length} / {view.logs.length} entries {view.isPaused ? '(paused)' : ''}
        </span>
        <span className="flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-text-muted animate-pulse" />
          Detached Window
        </span>
      </footer>
    </div>
  );
}

function SourceOption({
  label,
  dotClass,
  tag,
  selected,
  onSelect,
}: {
  label: string;
  dotClass: string;
  tag: string;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      onClick={onSelect}
      className={cn(
        'w-full flex items-center gap-2 px-3 py-2 text-left text-sm transition-colors',
        'hover:bg-surface-highlight',
        selected && 'bg-primary/10 text-primary'
      )}
    >
      <span className={cn('w-1.5 h-1.5 rounded-full', dotClass)} />
      <span className="truncate">{label}</span>
      <span className="ml-auto text-[10px] text-text-muted uppercase">{tag}</span>
    </button>
  );
}

// Export with error boundary wrapper
export function DetachedLogsPage() {
  return (
    <ErrorBoundary variant="window">
      <DetachedLogsPageContent />
    </ErrorBoundary>
  );
}
