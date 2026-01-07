import { Wifi, WifiOff, Clock } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useTopologyStore } from '../../stores/useTopologyStore';

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
  const mcpServers = useTopologyStore((s) => s.mcpServers);
  const resources = useTopologyStore((s) => s.resources);
  const connectionStatus = useTopologyStore((s) => s.connectionStatus);
  const lastUpdated = useTopologyStore((s) => s.lastUpdated);
  const error = useTopologyStore((s) => s.error);

  const runningServers = mcpServers.filter((s) => s.initialized).length;
  const runningResources = resources.filter((r) => r.status === 'running').length;
  const isConnected = connectionStatus === 'connected';

  return (
    <div className="h-8 bg-surface border-t border-border flex items-center justify-between px-4 text-xs">
      <div className="flex items-center gap-4">
        {/* Connection status */}
        <div className={cn(
          'flex items-center gap-1.5',
          isConnected ? 'text-status-running' : 'text-status-error'
        )}>
          {isConnected ? <Wifi size={12} /> : <WifiOff size={12} />}
          <span>{isConnected ? 'Connected' : error || 'Disconnected'}</span>
        </div>

        {/* Server count */}
        {mcpServers.length > 0 && (
          <span className="text-text-muted">
            <span className="text-status-running font-medium">{runningServers}</span>
            /{mcpServers.length} MCP servers
          </span>
        )}

        {/* Resource count */}
        {resources.length > 0 && (
          <span className="text-text-muted">
            <span className="text-secondary font-medium">{runningResources}</span>
            /{resources.length} resources
          </span>
        )}
      </div>

      {/* Last update */}
      {lastUpdated && (
        <div className="flex items-center gap-1.5 text-text-muted">
          <Clock size={12} />
          <span>Updated {formatRelativeTime(lastUpdated)}</span>
        </div>
      )}
    </div>
  );
}
