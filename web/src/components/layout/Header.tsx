import { Activity, RefreshCw, Settings } from 'lucide-react';
import { cn } from '../../lib/cn';
import { StatusDot } from '../ui/StatusDot';
import { IconButton } from '../ui/IconButton';
import { useTopologyStore } from '../../stores/useTopologyStore';

interface HeaderProps {
  onRefresh?: () => void;
  isRefreshing?: boolean;
}

export function Header({ onRefresh, isRefreshing }: HeaderProps) {
  const gatewayInfo = useTopologyStore((s) => s.gatewayInfo);
  const mcpServers = useTopologyStore((s) => s.mcpServers);
  const connectionStatus = useTopologyStore((s) => s.connectionStatus);

  const runningCount = mcpServers.filter((s) => s.initialized).length;
  const totalCount = mcpServers.length;
  const isConnected = connectionStatus === 'connected';

  return (
    <header className="h-14 bg-surface border-b border-border flex items-center justify-between px-4">
      {/* Left: Logo & Name */}
      <div className="flex items-center gap-3">
        <div className="flex items-center gap-2">
          {/* Logo icon */}
          <div className="w-8 h-8 bg-primary/20 rounded-lg flex items-center justify-center">
            <Activity size={18} className="text-primary" />
          </div>
          <div>
            <h1 className="text-base font-semibold text-text-primary">
              Agentlab
            </h1>
            {gatewayInfo && (
              <p className="text-xs text-text-muted -mt-0.5">
                {gatewayInfo.name}
              </p>
            )}
          </div>
        </div>
      </div>

      {/* Center: Status Summary */}
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-2 px-3 py-1.5 bg-background rounded-full">
          <StatusDot status={isConnected ? 'running' : 'stopped'} />
          <span className="text-xs text-text-secondary">
            Gateway {isConnected ? 'Connected' : 'Disconnected'}
          </span>
        </div>

        {totalCount > 0 && (
          <div className="flex items-center gap-2 px-3 py-1.5 bg-background rounded-full">
            <span className="text-xs text-text-secondary">
              <span className="text-status-running font-medium">{runningCount}</span>
              <span className="text-text-muted"> / {totalCount}</span>
              <span className="text-text-muted ml-1">servers</span>
            </span>
          </div>
        )}
      </div>

      {/* Right: Actions */}
      <div className="flex items-center gap-2">
        <IconButton
          icon={RefreshCw}
          onClick={onRefresh}
          disabled={isRefreshing}
          className={cn(isRefreshing && 'animate-spin')}
          tooltip="Refresh Status"
        />
        <IconButton
          icon={Settings}
          onClick={() => {/* Open settings */}}
          tooltip="Settings"
        />
      </div>
    </header>
  );
}
