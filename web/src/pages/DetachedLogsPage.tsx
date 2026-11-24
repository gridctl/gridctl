import { useCallback, useMemo } from 'react';
import { Terminal, Radio, Layers } from 'lucide-react';
import { cn } from '../lib/cn';
import { GATEWAY_LOG_SOURCE, LogsView, useLogsView } from '../components/log';
import { DetachedWindowFrame } from '../components/detached/DetachedWindowFrame';
import { SelectMenu, type SelectMenuItem } from '../components/detached/SelectMenu';
import { useSharedStatus } from '../hooks/useSharedStatus';
import { ErrorBoundary } from '../components/ui/ErrorBoundary';
import { useAuthStore } from '../stores/useAuthStore';

interface NodeOption {
  name: string;
  type: 'mcp-server' | 'resource';
}

// Frameless logs popout. The log surface itself is the shared LogsView (same
// URL-synced ?source=/?q=/?level=/?trace= semantics as the workspace, in this
// window's own URL) including the full control bar, so every stream action
// stays in lockstep with the workspace; this page adds only the source menu
// to the frame header. Trace pivots open the Traces workspace in a full app
// tab since the popout has no other workspaces.
function DetachedLogsPageContent() {
  const generation = useAuthStore(s => s.generation);
  const authRequired = useAuthStore(s => s.authRequired);
  const view = useLogsView();
  const { source } = view;
  const onTraceClick = useCallback((traceId: string) => {
    window.open(`/traces?trace=${encodeURIComponent(traceId)}`, '_blank', 'noopener');
  }, []);

  const { status, settled, error, refresh } = useSharedStatus({ enabled: !authRequired });
  const nodes = useMemo<NodeOption[]>(() => {
    if (!status || useAuthStore.getState().generation !== generation) return [];
    return [
      ...(status['mcp-servers'] ?? []).map((s) => ({ name: s.name, type: 'mcp-server' as const })),
      ...(status.resources ?? []).map((r) => ({ name: r.name, type: 'resource' as const })),
    ];
  }, [status, generation]);

  const isGateway = source === GATEWAY_LOG_SOURCE;
  const sourceLabel = source == null ? 'All sources' : isGateway ? 'Gateway' : source;

  const items: SelectMenuItem[] = [
    {
      key: '__all__',
      label: 'All sources',
      dotClass: 'bg-text-muted',
      tag: 'all',
      selected: source == null,
      onSelect: () => view.setSource(null),
    },
    {
      key: '__gateway__',
      label: 'Gateway',
      dotClass: 'bg-primary',
      tag: 'gateway',
      selected: isGateway,
      onSelect: () => view.setSource(GATEWAY_LOG_SOURCE),
    },
    ...nodes.map((node) => ({
      key: `${node.type}:${node.name}`,
      label: node.name,
      dotClass: node.type === 'mcp-server' ? 'bg-tertiary-light' : 'bg-secondary',
      tag: node.type === 'mcp-server' ? 'MCP server' : 'resource',
      selected: source === node.name,
      onSelect: () => view.setSource(node.name),
    })),
  ];

  // The stream's own failed state already reports an outage.
  const lost = (error != null || (settled && status == null)) && view.error == null;

  return (
    <DetachedWindowFrame
      windowType="logs"
      fullscreen
      header={{
        icon: (
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
        ),
        start: (
          <>
            <SelectMenu
              label="Log source"
              value={<span className="text-text-primary">{sourceLabel}</span>}
              items={items}
            />
            {isGateway && (
              <span className="text-3xs px-1.5 py-0.5 bg-primary/10 text-primary rounded font-medium border border-primary/20">
                Structured
              </span>
            )}
          </>
        ),
      }}
      connection={{ lost, message: error ?? undefined, onRetry: () => void refresh() }}
    >
      {/* Log surface: shared with the workspace, including the full control bar */}
      <main className="flex-1 min-h-0 flex flex-col">
        <LogsView view={view} onTraceClick={onTraceClick} emptyText="No logs available" />
      </main>
    </DetachedWindowFrame>
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
