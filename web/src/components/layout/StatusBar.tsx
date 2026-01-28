import { Wifi, WifiOff, Clock, Server, Box } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';

function formatRelativeTime(date: Date): string {
  const seconds = Math.floor((Date.now() - date.getTime()) / 1000);
  if (seconds < 10) return 'just now';
  if (seconds < 60) return `${seconds}s ago`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ago`;
}

export function StatusBar() {
  const mcpServers = useStackStore((s) => s.mcpServers);
  const resources = useStackStore((s) => s.resources);
  const connectionStatus = useStackStore((s) => s.connectionStatus);
  const lastUpdated = useStackStore((s) => s.lastUpdated);
  const error = useStackStore((s) => s.error);

  const runningServers = mcpServers.filter((s) => s.initialized).length;
  const runningResources = resources.filter((r) => r.status === 'running').length;
  const isConnected = connectionStatus === 'connected';

  return (
    <div className="h-8 bg-surface/80 backdrop-blur-xl border-t border-border/50 flex items-center justify-between px-4 text-xs relative">
      {/* Top accent */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-border/50 to-transparent" />

      <div className="flex items-center gap-5">
        {/* Connection status */}
        <div className={cn(
          'flex items-center gap-2 px-2 py-0.5 rounded-full',
          isConnected ? 'text-status-running' : 'text-status-error'
        )}>
          <span className="relative">
            {isConnected ? <Wifi size={12} /> : <WifiOff size={12} />}
            {isConnected && (
              <span className="absolute -top-0.5 -right-0.5 w-1.5 h-1.5 bg-status-running rounded-full animate-pulse" />
            )}
          </span>
          <span className="font-medium">{isConnected ? 'Connected' : error || 'Disconnected'}</span>
        </div>

        {/* Divider */}
        <div className="w-px h-3 bg-border/50" />

        {/* Server count */}
        {mcpServers.length > 0 && (
          <div className="flex items-center gap-2 text-text-muted">
            <Server size={11} className="text-primary" />
            <span>
              <span className="text-status-running font-semibold">{runningServers}</span>
              <span className="text-text-muted/60">/{mcpServers.length}</span>
              <span className="ml-1">MCP</span>
            </span>
          </div>
        )}

        {/* Resource count */}
        {resources.length > 0 && (
          <div className="flex items-center gap-2 text-text-muted">
            <Box size={11} className="text-secondary" />
            <span>
              <span className="text-secondary font-semibold">{runningResources}</span>
              <span className="text-text-muted/60">/{resources.length}</span>
              <span className="ml-1">Resources</span>
            </span>
          </div>
        )}
      </div>

      {/* Last update */}
      {lastUpdated && (
        <div className="flex items-center gap-2 text-text-muted">
          <Clock size={11} className="text-text-muted/60" />
          <span>Updated {formatRelativeTime(lastUpdated)}</span>
        </div>
      )}
    </div>
  );
}
