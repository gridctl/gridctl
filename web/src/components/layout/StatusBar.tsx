import { Wifi, WifiOff, Clock, Server, Box, Radio, Code, FlaskConical, Gauge, ArrowDown, Loader2 } from 'lucide-react';
import { useGuardedNavigate } from '../../hooks/useGuardedNavigate';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';
import { formatRelativeTime } from '../../lib/time';
import { useNow } from '../../hooks/useNow';
import { formatCompactNumber } from '../../lib/format';
import { SpecHealthBadge } from '../spec/SpecHealthBadge';
import { PinDriftBadge } from '../pins/PinDriftBadge';
import { PinFindingsBadge } from '../pins/PinFindingsBadge';
import { AuthPendingBadge } from '../sidebar/AuthPendingBadge';
import { LimitsBadge } from '../metrics/LimitsBadge';

export function StatusBar() {
  const navigate = useGuardedNavigate();
  const mcpServers = useStackStore((s) => s.mcpServers);
  const resources = useStackStore((s) => s.resources);
  const statusSessions = useStackStore((s) => s.sessions);
  const sessionEntries = useStackStore((s) => s.sessionEntries);
  // Prefer the entries array when the Connections workspace has loaded it:
  // both surfaces then read one list, so the two numbers cannot diverge
  // even transiently between polling cadences. The status-poll count is
  // the fallback while entries are unfetched (backend keeps them equal).
  const sessions = sessionEntries !== null ? sessionEntries.length : statusSessions;
  const codeMode = useStackStore((s) => s.codeMode);
  const toolDiscovery = useStackStore((s) => s.toolDiscovery);
  const featureDetails = useStackStore((s) => s.featureDetails);
  const tokenUsage = useStackStore((s) => s.tokenUsage);
  const tokenizerName = useStackStore((s) => s.gatewayInfo?.tokenizer);
  const connectionStatus = useStackStore((s) => s.connectionStatus);
  const error = useStackStore((s) => s.error);

  const runningServers = (mcpServers ?? []).filter((s) => s.initialized).length;
  const unhealthyServers = (mcpServers ?? []).filter((s) => s.healthy === false).length;
  const runningResources = (resources ?? []).filter((r) => r.status === 'running').length;
  const isConnected = connectionStatus === 'connected';
  const isConnecting = connectionStatus === 'connecting';
  const label = isConnected ? 'Connected' : isConnecting ? 'Connecting' : error || 'Disconnected';

  return (
    <footer
      aria-label="Status bar"
      className="h-8 min-w-0 bg-surface/80 backdrop-blur-xl border-t border-border/50 flex items-center gap-5 px-4 text-xs relative whitespace-nowrap"
    >
      {/* Top accent */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-border/50 to-transparent" />

      {/* Connection status. The live region holds only the state label;
          the ticking "Updated" age stays outside it so it is not announced
          on every tick. */}
      <div role="status" className={cn(
        'flex items-center gap-2 px-2 py-0.5 rounded-full shrink-0',
        isConnected ? 'text-status-running' : isConnecting ? 'text-text-muted' : 'text-status-error'
      )}>
        <span className="relative">
          {isConnected ? (
            <Wifi size={12} aria-hidden="true" />
          ) : isConnecting ? (
            <Loader2 size={12} className="animate-spin" aria-hidden="true" />
          ) : (
            <WifiOff size={12} aria-hidden="true" />
          )}
          {isConnected && (
            <span aria-hidden="true" className="absolute -top-0.5 -right-0.5 w-1.5 h-1.5 bg-status-running rounded-full animate-pulse" />
          )}
        </span>
        <span
          className="font-medium truncate min-w-0 max-w-[40vw]"
          title={label}
        >
          {label}
        </span>
      </div>

      {/* Divider */}
      <div className="w-px h-3 bg-border/50 shrink-0" />

      {/* Alert badges. Each hides itself when idle; :empty then drops the
          group and its trailing divider (the ::after). Labels truncate only
          once the counter group beside it has shrunk to nothing. */}
      <div className="flex items-center gap-5 min-w-0 empty:hidden after:content-[''] after:w-px after:h-3 after:bg-border/50 after:shrink-0">
        <SpecHealthBadge />
        <PinDriftBadge />
        <PinFindingsBadge />
        {/* Pending downstream authorizations */}
        <AuthPendingBadge />
        {/* Rate limit pressure */}
        <LimitsBadge />
      </div>

      {/* Counters wrap instead of shrinking: an item that does not fit moves
          to a second line below the clip edge, so it is hidden whole rather
          than cut mid-text. overflow-clip (not hidden) keeps a focused
          hidden button from scrolling that line into view. */}
      <div className="min-w-0 flex-1 flex flex-wrap content-start items-center gap-x-5 h-full overflow-clip">
        {/* Zero-width first item, its margin cancelling the gap, so the first
            counter can wrap too: a flex line always keeps its first item. */}
        <div aria-hidden="true" className="w-0 h-full -mr-5 shrink-0" />

        {/* Server count */}
        {(mcpServers ?? []).length > 0 && (
          <div className="flex items-center gap-2 h-full text-text-muted">
            <Server size={11} className="text-primary" />
            <span>
              <span className="text-status-running font-semibold">{runningServers}</span>
              <span className="text-text-muted">/{(mcpServers ?? []).length}</span>
              <span className="ml-1">MCP servers</span>
            </span>
            {unhealthyServers > 0 && (
              <>
                <span className="text-text-muted mx-0.5">&middot;</span>
                <span className="text-status-error font-semibold">{unhealthyServers}</span>
                <span className="ml-0.5">err</span>
              </>
            )}
          </div>
        )}

        {/* Resource count */}
        {(resources ?? []).length > 0 && (
          <div className="flex items-center gap-2 h-full text-text-muted">
            <Box size={11} className="text-secondary" />
            <span>
              <span className="text-secondary font-semibold">{runningResources}</span>
              <span className="text-text-muted">/{(resources ?? []).length}</span>
              <span className="ml-1">Resources</span>
            </span>
          </div>
        )}

        {/* Code Mode indicator — read-only status, not an action (code mode is
            configured in stack.yaml and cannot be toggled at runtime). */}
        {codeMode && codeMode !== 'off' && (
          <div role="status" className="flex items-center gap-2 h-full text-text-muted">
            <Code size={11} className="text-primary" />
            <span className="text-primary font-semibold">Code Mode</span>
          </div>
        )}

        {/* Deferred discovery indicator. Read-only status, not an action
            (tool_discovery is configured in stack.yaml or on the link URL). */}
        {toolDiscovery && toolDiscovery !== 'off' && (
          <div role="status" className="flex items-center gap-2 h-full text-text-muted">
            <span className="text-primary font-semibold">Deferred discovery</span>
          </div>
        )}

        {/* Session count */}
        {/* Hidden at and below the 768px wrap width. md would still show them there. */}
        {sessions > 0 && (
          <div className="flex items-center gap-2 h-full text-text-muted max-[800px]:hidden">
            <Radio size={11} className="text-primary" />
            <span>
              <span className="text-primary font-semibold">{sessions}</span>
              <span className="ml-1">sessions</span>
            </span>
          </div>
        )}

        {/* Experimental flags — an aggregate count, deliberately neutral (an
            enabled experiment is not "good" or "bad"). Read-only: flags are
            configured in stack.yaml and cannot be toggled from the UI; the
            click deep-links to the spec pane where each flag is listed. */}
        {featureDetails.length > 0 && (
          <button
            type="button"
            onClick={() => navigate('/stack?spec=1')}
            aria-label={`${featureDetails.length} experimental ${featureDetails.length === 1 ? 'flag' : 'flags'} enabled. Open spec pane`}
            title={featureDetails.map((f) => f.name).join(', ')}
            className="flex items-center gap-2 h-full text-text-muted rounded px-1 -mx-1 hover:bg-surface-highlight/50 hover:text-text-secondary transition-colors"
          >
            <FlaskConical size={11} />
            <span>
              <span className="font-semibold">{featureDetails.length}</span>
              <span className="ml-1">experimental</span>
            </span>
          </button>
        )}

        {/* Token counter — opens the Metrics workspace */}
        {tokenUsage && tokenUsage.session.total_tokens > 0 && (
          <button
            type="button"
            onClick={() => navigate('/metrics')}
            aria-label="Open Metrics workspace"
            className="flex items-center gap-2 h-full text-text-muted rounded px-1 -mx-1 hover:bg-surface-highlight/50 hover:text-text-secondary transition-colors"
          >
            <Gauge size={11} className="text-primary" />
            <span>
              <span className="text-primary font-semibold">{formatCompactNumber(tokenUsage.session.total_tokens)}</span>
              <span className="ml-1">tokens</span>
            </span>
          </button>
        )}

        {/* Tokenizer mode indicator */}
        {tokenizerName && (
          <div className="flex items-center gap-2 h-full text-text-muted max-[800px]:hidden">
            <span className="text-text-muted font-medium">{tokenizerName}</span>
          </div>
        )}

        {/* Format savings indicator */}
        {tokenUsage && tokenUsage.format_savings.savings_percent > 0 && (
          <div className="flex items-center gap-2 h-full text-text-muted max-[800px]:hidden">
            <ArrowDown size={11} className="text-status-running" />
            <span className="text-status-running font-semibold">
              {Math.round(tokenUsage.format_savings.savings_percent)}% saved
            </span>
          </div>
        )}

        {/* Last update: right-aligned when it fits, and the first to drop */}
        <StatusFreshness />
      </div>
    </footer>
  );
}

// The age ticks on its own one-second clock so it keeps advancing when no
// poll lands (a refused or hung request). Only this leaf re-renders per tick.
function StatusFreshness() {
  const lastUpdated = useStackStore((s) => s.lastUpdated);
  const connectionStatus = useStackStore((s) => s.connectionStatus);
  useNow(1000);

  if (!lastUpdated) return null;
  const stale = connectionStatus === 'disconnected' || connectionStatus === 'error';

  return (
    <div className="flex items-center gap-2 h-full ml-auto text-text-muted">
      <Clock size={11} aria-hidden="true" className={stale ? 'text-status-pending' : 'text-text-muted'} />
      <span className={cn(stale && 'text-status-pending')}>
        {stale ? 'Last updated' : 'Updated'} {formatRelativeTime(lastUpdated)}
      </span>
    </div>
  );
}
