import { RefreshCw, Settings, Zap } from 'lucide-react';
import { cn } from '../../lib/cn';
import { StatusDot } from '../ui/StatusDot';
import { IconButton } from '../ui/IconButton';
import { useStackStore } from '../../stores/useStackStore';
import logoSvg from '../../assets/brand/logo.svg';

interface HeaderProps {
  onRefresh?: () => void;
  isRefreshing?: boolean;
}

export function Header({ onRefresh, isRefreshing }: HeaderProps) {
  const gatewayInfo = useStackStore((s) => s.gatewayInfo);
  const mcpServers = useStackStore((s) => s.mcpServers);
  const connectionStatus = useStackStore((s) => s.connectionStatus);

  const runningCount = (mcpServers ?? []).filter((s) => s.initialized).length;
  const totalCount = (mcpServers ?? []).length;
  const unhealthyCount = (mcpServers ?? []).filter((s) => s.healthy === false).length;
  const isConnected = connectionStatus === 'connected';

  return (
    <header className="h-14 bg-surface/80 backdrop-blur-xl border-b border-border/50 flex items-center justify-between px-5 relative z-30">
      {/* Subtle gradient line at top */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/30 to-transparent" />

      {/* Left: Logo & Version */}
      <div className="flex items-center gap-4">
        <div className="flex items-center gap-3">
          {/* Brand Logo */}
          <img
            src={logoSvg}
            alt="Gridctl"
            className="h-10 w-auto block"
          />
          {/* Version */}
          {gatewayInfo?.version && (
            <span className="text-xs font-mono text-text-muted tracking-wide">
              {gatewayInfo.version}
            </span>
          )}
        </div>
      </div>

      {/* Center: Status Pills */}
      <div className="flex items-center gap-3">
        {/* Connection Status */}
        <div className={cn(
          'flex items-center gap-2 px-3 py-1.5 rounded-full',
          'bg-surface-elevated/60 backdrop-blur-sm border',
          isConnected ? 'border-status-running/20' : 'border-status-error/20'
        )}>
          <StatusDot status={isConnected ? 'running' : 'error'} />
          <span className="text-xs font-medium text-text-secondary">
            {isConnected ? 'Connected' : 'Disconnected'}
          </span>
        </div>

        {/* Server Count */}
        {totalCount > 0 && (
          <div className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-surface-elevated/60 backdrop-blur-sm border border-border/50">
            <Zap size={12} className="text-primary" />
            <span className="text-xs font-medium">
              <span className="text-status-running">{runningCount}</span>
              <span className="text-text-muted mx-0.5">/</span>
              <span className="text-text-secondary">{totalCount}</span>
              <span className="text-text-muted ml-1.5">active</span>
              {unhealthyCount > 0 && (
                <span className="text-status-error ml-1.5">
                  ({unhealthyCount} unhealthy)
                </span>
              )}
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
          className={cn(
            isRefreshing && 'animate-spin',
            'hover:text-primary hover:border-primary/30'
          )}
          tooltip="Refresh (⌘R)"
        />
        <IconButton
          icon={Settings}
          onClick={() => {/* Open settings */}}
          tooltip="Settings"
          className="hover:text-primary hover:border-primary/30"
        />
      </div>
    </header>
  );
}
