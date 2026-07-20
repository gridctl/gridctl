import { Wifi, WifiOff, Clock, Server, Box, Radio, Code, Gauge, ArrowDown, DollarSign } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';
import { formatRelativeTime } from '../../lib/time';
import { formatCompactNumber, formatUSD } from '../../lib/format';
import { SpecHealthBadge } from '../spec/SpecHealthBadge';
import { PinDriftBadge } from '../pins/PinDriftBadge';
import { PinFindingsBadge } from '../pins/PinFindingsBadge';
import { AuthPendingBadge } from '../sidebar/AuthPendingBadge';

export function StatusBar() {
  const navigate = useNavigate();
  const mcpServers = useStackStore((s) => s.mcpServers);
  const resources = useStackStore((s) => s.resources);
  const sessions = useStackStore((s) => s.sessions);
  const codeMode = useStackStore((s) => s.codeMode);
  const tokenUsage = useStackStore((s) => s.tokenUsage);
  const costUsage = useStackStore((s) => s.costUsage);
  const tokenizerName = useStackStore((s) => s.gatewayInfo?.tokenizer);
  const connectionStatus = useStackStore((s) => s.connectionStatus);
  const lastUpdated = useStackStore((s) => s.lastUpdated);
  const error = useStackStore((s) => s.error);

  const sessionCostUSD = costUsage?.session.total_usd;

  const runningServers = (mcpServers ?? []).filter((s) => s.initialized).length;
  const unhealthyServers = (mcpServers ?? []).filter((s) => s.healthy === false).length;
  const runningResources = (resources ?? []).filter((r) => r.status === 'running').length;
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
        {(mcpServers ?? []).length > 0 && (
          <div className="flex items-center gap-2 text-text-muted">
            <Server size={11} className="text-primary" />
            <span>
              <span className="text-status-running font-semibold">{runningServers}</span>
              <span className="text-text-muted/60">/{(mcpServers ?? []).length}</span>
              <span className="ml-1">MCP</span>
            </span>
            {unhealthyServers > 0 && (
              <>
                <span className="text-text-muted/60 mx-0.5">&middot;</span>
                <span className="text-status-error font-semibold">{unhealthyServers}</span>
                <span className="ml-0.5">err</span>
              </>
            )}
          </div>
        )}

        {/* Resource count */}
        {(resources ?? []).length > 0 && (
          <div className="flex items-center gap-2 text-text-muted">
            <Box size={11} className="text-secondary" />
            <span>
              <span className="text-secondary font-semibold">{runningResources}</span>
              <span className="text-text-muted/60">/{(resources ?? []).length}</span>
              <span className="ml-1">Resources</span>
            </span>
          </div>
        )}

        {/* Session count */}
        {sessions > 0 && (
          <div className="flex items-center gap-2 text-text-muted">
            <Radio size={11} className="text-primary" />
            <span>
              <span className="text-primary font-semibold">{sessions}</span>
              <span className="ml-1">sessions</span>
            </span>
          </div>
        )}

        {/* Code Mode indicator — read-only status, not an action (code mode is
            configured in stack.yaml and cannot be toggled at runtime). */}
        {codeMode && codeMode !== 'off' && (
          <div role="status" className="flex items-center gap-2 text-text-muted">
            <Code size={11} className="text-primary" />
            <span className="text-primary font-semibold">Code Mode</span>
          </div>
        )}

        {/* Token counter — opens the Metrics workspace */}
        {tokenUsage && tokenUsage.session.total_tokens > 0 && (
          <button
            type="button"
            onClick={() => navigate('/metrics')}
            aria-label="Open Metrics workspace"
            className="flex items-center gap-2 text-text-muted rounded px-1 -mx-1 hover:bg-surface-highlight/50 hover:text-text-secondary transition-colors"
          >
            <Gauge size={11} className="text-primary" />
            <span>
              <span className="text-primary font-semibold">{formatCompactNumber(tokenUsage.session.total_tokens)}</span>
              <span className="ml-1">tokens</span>
            </span>
          </button>
        )}

        {/* Session cost — emerald + "$" so it reads as money without relying on
            color alone. Opens the Metrics workspace. */}
        {sessionCostUSD !== undefined && (
          <button
            type="button"
            onClick={() => navigate('/metrics')}
            aria-label={`Estimated session cost ${formatUSD(sessionCostUSD)}. Open Metrics workspace`}
            title="Estimated cost · open Metrics"
            className="flex items-center gap-1.5 text-text-muted rounded px-1 -mx-1 hover:bg-surface-highlight/50 transition-colors"
          >
            <DollarSign size={11} className="text-emerald-400" />
            <span className="text-emerald-400 font-semibold tabular-nums">{formatUSD(sessionCostUSD)}</span>
            <span className="text-text-muted/60">est.</span>
          </button>
        )}

        {/* Tokenizer mode indicator */}
        {tokenizerName && (
          <div className="flex items-center gap-2 text-text-muted">
            <span className="text-text-muted/60 font-medium">{tokenizerName}</span>
          </div>
        )}

        {/* Format savings indicator */}
        {tokenUsage && tokenUsage.format_savings.savings_percent > 0 && (
          <div className="flex items-center gap-2 text-text-muted">
            <ArrowDown size={11} className="text-status-running" />
            <span className="text-status-running font-semibold">
              {Math.round(tokenUsage.format_savings.savings_percent)}% saved
            </span>
          </div>
        )}

        {/* Divider before spec health */}
        <div className="w-px h-3 bg-border/50" />

        {/* Spec health badge */}
        <SpecHealthBadge />

        {/* Divider before pin drift */}
        <div className="w-px h-3 bg-border/50" />
        <PinDriftBadge />
        <PinFindingsBadge />

        {/* Pending downstream authorizations (hidden when none) */}
        <AuthPendingBadge />
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
