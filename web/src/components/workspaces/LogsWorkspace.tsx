import { useMemo } from 'react';
import { useNavigate } from 'react-router';
import { Activity, Layers, Radio, Server } from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useUIStore } from '../../stores/useUIStore';
import { useStackStore } from '../../stores/useStackStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { PopoutButton } from '../ui/PopoutButton';
import { PersistedFromMarker } from '../telemetry/PersistedFromMarker';
import { GATEWAY_LOG_SOURCE, LogsView, logSourceOf, useLogsView } from '../log';
import { useLogsCommands } from '../log/useLogsCommands';

// LogsWorkspace is the first-class log surface: the aggregate multi-server
// stream from GET /api/logs with no selection prerequisite. The left rail
// picks a source (all / gateway / per server) as a client-side filter over the
// same stream — never a second fetch path. Filter state and semantics live in
// the shared LogsView core (URL-synced ?source= with a permanent ?agent=
// alias, ?level=, ?q=, ?trace=, ?range=, ?n=), so reload, deep-links, the
// node-to-logs and trace-to-logs pivots, and the detached window all land on
// the exact view they name. Every stream action lives in the shared control
// bar; this host contributes only identity chrome and the popout.
export function LogsWorkspace() {
  const navigate = useNavigate();
  const compact = useUIStore((s) => s.compactMode.logs);
  const logsDetached = useUIStore((s) => s.logsDetached);
  const { openDetachedWindow } = useWindowManager();
  const mcpServers = useStackStore((s) => s.mcpServers);

  const view = useLogsView();
  const { source, sourceCounts } = view;

  const openDetached = () => openDetachedWindow('logs', view.filterQuery || undefined);
  useLogsCommands({ view, onOpenDetached: openDetached });

  // Rail entries stay stable across filters: deployed servers plus any source
  // present in the raw stream (even one no longer deployed), so entries stay
  // reachable while counts reflect the active filters.
  const serverNames = useMemo(() => {
    const names = new Set(mcpServers.map((s) => s.name));
    for (const log of view.logs) {
      const name = logSourceOf(log);
      if (name !== GATEWAY_LOG_SOURCE) names.add(name);
    }
    return [...names].sort();
  }, [mcpServers, view.logs]);

  const leftRail = (
    <SourceRail
      compact={compact}
      source={source}
      onSelectSource={view.setSource}
      serverNames={serverNames}
      totalCount={view.facetTotal}
      counts={sourceCounts}
      onViewMetrics={(server) =>
        navigate(`/metrics?scope=servers&selected=${encodeURIComponent(server)}`)
      }
    />
  );

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell workspace="logs" defaultLeftPct={16} left={leftRail} minLeftPx={180}>
        <main className="flex flex-col h-full overflow-hidden">
          <header
            className={cn(
              'flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle flex items-center gap-3 px-6',
              compact ? 'py-2' : 'py-3',
            )}
          >
            <div className="font-sans text-text-muted/60 text-[10px] uppercase tracking-[0.4em]">logs</div>
            <div className="font-mono text-[10px] text-text-muted truncate">
              {source ?? 'all sources'}
            </div>
          </header>

          <LogsView
            view={view}
            onTraceClick={(traceId) => navigate(`/traces?trace=${encodeURIComponent(traceId)}`)}
            header={
              source != null && source !== GATEWAY_LOG_SOURCE ? (
                <PersistedFromMarker serverName={source} signal="logs" />
              ) : undefined
            }
            emptyText="No logs yet"
            toolbarExtra={
              <PopoutButton
                onClick={openDetached}
                tooltip="Open in separate window"
                disabled={logsDetached}
              />
            }
          />
        </main>
      </WorkspaceShell>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Left rail — source navigator
// ---------------------------------------------------------------------------

interface SourceRailProps {
  compact: boolean;
  source: string | null;
  onSelectSource: (source: string | null) => void;
  serverNames: string[];
  totalCount: number;
  counts: Map<string, number>;
  /** Pivot to the Metrics workspace scoped to a server. */
  onViewMetrics?: (server: string) => void;
}

function SourceRail({ compact, source, onSelectSource, serverNames, totalCount, counts, onViewMetrics }: SourceRailProps) {
  return (
    <aside className="h-full flex flex-col bg-surface border-r border-border-subtle">
      <div className={cn('flex-shrink-0 px-3 border-b border-border-subtle/60', compact ? 'py-2' : 'py-3')}>
        <div className="text-[10px] font-medium text-text-muted/60 uppercase tracking-[0.3em]">sources</div>
      </div>
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-2 py-2 space-y-0.5">
        <SourcePill
          label="All sources"
          icon={Layers}
          count={totalCount}
          active={source == null}
          onClick={() => onSelectSource(null)}
        />
        <SourcePill
          label="Gateway"
          icon={Radio}
          count={counts.get(GATEWAY_LOG_SOURCE) ?? 0}
          active={source === GATEWAY_LOG_SOURCE}
          onClick={() => onSelectSource(GATEWAY_LOG_SOURCE)}
        />
        {serverNames.map((name) => (
          <SourcePill
            key={name}
            label={name}
            icon={Server}
            count={counts.get(name) ?? 0}
            active={source === name}
            onClick={() => onSelectSource(name)}
            onViewMetrics={onViewMetrics ? () => onViewMetrics(name) : undefined}
          />
        ))}
      </div>
    </aside>
  );
}

function SourcePill({
  label,
  icon: Icon,
  count,
  active,
  onClick,
  onViewMetrics,
}: {
  label: string;
  icon: LucideIcon;
  count: number;
  active: boolean;
  onClick: () => void;
  onViewMetrics?: () => void;
}) {
  return (
    <div className="group/pill relative">
      <button
        onClick={onClick}
        aria-current={active}
        className={cn(
          'w-full flex items-center gap-2 px-3 py-1.5 rounded-md text-left transition-colors',
          active ? 'bg-primary/10 text-primary' : 'text-text-secondary hover:bg-surface-highlight/50 hover:text-text-primary',
        )}
      >
        <Icon size={13} className={active ? 'text-primary' : 'text-text-muted'} aria-hidden="true" />
        <span className={cn('flex-1 min-w-0 text-xs font-medium truncate', active && 'text-primary')}>{label}</span>
        <span
          className={cn(
            'flex-shrink-0 text-[10px] font-mono px-1.5 py-0.5 rounded tabular-nums',
            active ? 'bg-primary/15 text-primary' : 'bg-surface-elevated text-text-muted',
            onViewMetrics && 'group-hover/pill:opacity-0 group-focus-within/pill:opacity-0 transition-opacity',
          )}
        >
          {count}
        </span>
      </button>
      {onViewMetrics && (
        <button
          onClick={onViewMetrics}
          title={`View metrics for ${label}`}
          aria-label={`View metrics for ${label}`}
          className={cn(
            'absolute right-2 top-1/2 -translate-y-1/2 p-1 rounded',
            'opacity-0 group-hover/pill:opacity-100 focus-visible:opacity-100',
            'text-text-muted hover:text-primary hover:bg-surface-highlight transition-all',
          )}
        >
          <Activity size={11} />
        </button>
      )}
    </div>
  );
}

export default LogsWorkspace;
