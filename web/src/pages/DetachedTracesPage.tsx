import { useCallback, useEffect, useState } from 'react';
import { Activity } from 'lucide-react';
import { fetchMCPServers } from '../lib/api';
import { TracesView } from '../components/traces/TracesView';
import { DetachedWindowFrame } from '../components/detached/DetachedWindowFrame';
import { ErrorBoundary } from '../components/ui/ErrorBoundary';
import { useSharedStatus } from '../hooks/useSharedStatus';
import { useAuthStore } from '../stores/useAuthStore';
import { useTracesStore } from '../stores/useTracesStore';

// Frameless traces popout. The trace surface itself is the shared TracesView;
// this page only supplies the frame's header and connection state. The
// detached window runs its own store instance, so nothing here bleeds into
// the main shell's traces state. Pivots open a full app tab since the popout
// has no other workspaces.
function DetachedTracesPageContent() {
  const generation = useAuthStore(s => s.generation);
  const authRequired = useAuthStore(s => s.authRequired);
  const [servers, setServers] = useState<string[]>([]);
  const { status, settled, error, refresh } = useSharedStatus({ enabled: !authRequired });
  const listError = useTracesStore(s => s.error);

  // Load deployed server names for the filter dropdown. The detached window
  // has no stack-store poller, so fetch directly.
  useEffect(() => {
    if (authRequired) return;
    let cancelled = false;
    fetchMCPServers()
      .then((list) => { if (!cancelled && useAuthStore.getState().generation === generation) setServers(list.map((s) => s.name).sort()); })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [generation, authRequired]);

  const onViewLogs = useCallback((traceId: string) => {
    window.open(`/logs?trace=${encodeURIComponent(traceId)}`, '_blank', 'noopener');
  }, []);
  const onViewMetrics = useCallback((server: string) => {
    window.open(`/metrics?scope=servers&selected=${encodeURIComponent(server)}`, '_blank', 'noopener');
  }, []);

  const lost = (error != null || (settled && status == null)) && listError == null;

  return (
    <DetachedWindowFrame
      windowType="traces"
      fullscreen
      header={{
        icon: (
          <div className="p-1.5 rounded-lg border bg-primary/10 border-primary/20">
            <Activity size={14} className="text-primary" />
          </div>
        ),
        start: <span className="text-sm font-semibold text-text-primary">Traces</span>,
      }}
      connection={{ lost, message: error ?? undefined, onRetry: () => void refresh() }}
      footerStart="Traces"
    >
      <main className="flex-1 min-h-0">
        <TracesView
          active={!authRequired}
          servers={servers}
          onViewLogs={onViewLogs}
          onViewMetrics={onViewMetrics}
        />
      </main>
    </DetachedWindowFrame>
  );
}

export function DetachedTracesPage() {
  return (
    <ErrorBoundary variant="window">
      <DetachedTracesPageContent />
    </ErrorBoundary>
  );
}
