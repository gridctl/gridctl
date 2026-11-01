import { useEffect, useId, useMemo, useRef } from 'react';
import { useNavigate, useSearchParams } from 'react-router';
import { cn } from '../../lib/cn';
import { WORKSPACE_LABELS } from '../../types/workspace';
import { useUIStore } from '../../stores/useUIStore';
import { useStackStore } from '../../stores/useStackStore';
import { useTracesStore } from '../../stores/useTracesStore';
import { useRunsStore } from '../../stores/useRunsStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { PopoutButton } from '../ui/PopoutButton';
import { Tab, TabPanel, Tabs } from '../ui/Tabs';
import { TracesView } from '../traces/TracesView';
import { RunsView } from '../runs/RunsView';

// TracesWorkspace is the first-class trace surface: the global trace list,
// waterfall, and span detail from TracesView, with the selection and filters
// URL-synced (?trace=, ?server=, ?errors=, ?q=, ?seg=) so deep links —
// including the log-line trace pivot — restore the exact view. The trace
// detail pivots back to /logs?trace=<id> for the reverse correlation and to
// /metrics?scope=servers&selected=<server> for the same server's metrics.
export function TracesWorkspace() {
  const headingId = useId();
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const compact = useUIStore((s) => s.compactMode.traces);
  const tracesDetached = useUIStore((s) => s.tracesDetached);
  const { openDetachedWindow } = useWindowManager();

  const mcpServers = useStackStore((s) => s.mcpServers);
  const servers = useMemo(() => mcpServers.map((s) => s.name).sort(), [mcpServers]);

  const view = searchParams.get('view') === 'runs' ? 'runs' : 'traces';
  const selectedTraceId = useTracesStore((s) => s.selectedTraceId);
  const filters = useTracesStore((s) => s.filters);
  const runCount = useRunsStore((s) => s.records.length);
  const runPartial = useRunsStore((s) => s.partial);
  const selectTrace = useTracesStore((s) => s.selectTrace);
  const setFilters = useTracesStore((s) => s.setFilters);

  // URL → store, once on mount: deep links win over whatever the store still
  // holds from a previous visit.
  const initialParams = useRef(searchParams);
  useEffect(() => {
    const p = initialParams.current;
    const trace = p.get('trace');
    const server = p.get('server');
    const errors = p.get('errors');
    const q = p.get('q');
    const seg = p.get('seg');
    if (server != null || errors != null || q != null || seg != null) {
      setFilters({
        ...(server != null ? { server } : {}),
        ...(errors != null ? { errorsOnly: errors === '1' } : {}),
        ...(q != null ? { search: q } : {}),
        ...(seg != null ? { segment: seg === 'all' ? 'all' : 'tool-calls' } : {}),
      });
    }
    if (trace && trace !== useTracesStore.getState().selectedTraceId) {
      selectTrace(trace);
    }
  }, [setFilters, selectTrace]);

  // Store → URL mirror. Reads live store state so the first pass after the
  // mount sync above never rewrites the URL from stale render-time values.
  useEffect(() => {
    const s = useTracesStore.getState();
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev);
        if (s.selectedTraceId) params.set('trace', s.selectedTraceId);
        else params.delete('trace');
        if (s.filters.server) params.set('server', s.filters.server);
        else params.delete('server');
        if (s.filters.errorsOnly) params.set('errors', '1');
        else params.delete('errors');
        if (s.filters.search) params.set('q', s.filters.search);
        else params.delete('q');
        if (s.filters.segment === 'all') params.set('seg', 'all');
        else params.delete('seg');
        return params;
      },
      { replace: true },
    );
  }, [selectedTraceId, filters, setSearchParams]);

  const selectTraces = () => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.delete('view');
      return next;
    }, { replace: true });
  };
  const selectRuns = () => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      next.set('view', 'runs');
      return next;
    }, { replace: true });
  };

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <h1 id={headingId} className="sr-only">{WORKSPACE_LABELS.traces}</h1>
      <header
        className={cn(
          'flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle flex items-center gap-3 px-6',
          compact ? 'py-2' : 'py-3',
        )}
      >
        <Tabs
          ariaLabel="Observability view"
          className="px-0 pt-0 border-0 border-b-0 gap-1"
        >
          <Tab
            id="traces-tab-traces"
            selected={view === 'traces'}
            controls="traces-tabpanel"
            onSelect={selectTraces}
            className={cn(
              'font-sans text-3xs uppercase tracking-[0.4em] px-2 py-1 rounded border-b-0 border-transparent mb-0',
              view === 'traces' ? 'text-primary' : 'text-text-muted/60',
            )}
          >
            traces
          </Tab>
          <Tab
            id="traces-tab-runs"
            selected={view === 'runs'}
            controls="traces-tabpanel"
            onSelect={selectRuns}
            className={cn(
              'font-sans text-3xs uppercase tracking-[0.4em] px-2 py-1 rounded border-b-0 border-transparent mb-0',
              view === 'runs' ? 'text-primary' : 'text-text-muted/60',
            )}
          >
            runs
          </Tab>
        </Tabs>
        <div className="font-mono text-3xs text-text-muted truncate">
          {view === 'runs'
            ? `${runCount} dispatch record${runCount === 1 ? '' : 's'}${runPartial ? ' (partial)' : ''}`
            : selectedTraceId
              ? selectedTraceId.slice(0, 16)
              : filters.segment === 'all'
                ? 'all traces'
                : 'tool calls'}
        </div>
      </header>
      <TabPanel
        id="traces-tabpanel"
        labelledBy={view === 'runs' ? 'traces-tab-runs' : 'traces-tab-traces'}
        active
        className="flex-1 min-h-0"
      >
        {view === 'runs' ? (
          <RunsView servers={servers} />
        ) : (
          <TracesView
            active
            servers={servers}
            onViewLogs={(traceId) => navigate(`/logs?trace=${encodeURIComponent(traceId)}`)}
            onViewMetrics={(server) => navigate(`/metrics?scope=servers&selected=${encodeURIComponent(server)}`)}
            toolbarExtra={
              <PopoutButton
                onClick={() => openDetachedWindow('traces')}
                tooltip="Open in separate window"
                disabled={tracesDetached}
              />
            }
          />
        )}
      </TabPanel>
    </div>
  );
}

export default TracesWorkspace;
