import { useEffect, useMemo, useRef } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { cn } from '../../lib/cn';
import { useUIStore } from '../../stores/useUIStore';
import { useStackStore } from '../../stores/useStackStore';
import { useTracesStore } from '../../stores/useTracesStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { PopoutButton } from '../ui/PopoutButton';
import { TracesView } from '../traces/TracesView';

// TracesWorkspace is the first-class trace surface: the global trace list,
// waterfall, and span detail from TracesView, with the selection and filters
// URL-synced (?trace=, ?server=, ?errors=, ?q=) so deep links — including the
// log-line trace pivot — restore the exact view. The trace detail pivots back
// to /logs?trace=<id> for the reverse correlation.
export function TracesWorkspace() {
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const compact = useUIStore((s) => s.compactMode.traces);
  const tracesDetached = useUIStore((s) => s.tracesDetached);
  const { openDetachedWindow } = useWindowManager();

  const mcpServers = useStackStore((s) => s.mcpServers);
  const servers = useMemo(() => mcpServers.map((s) => s.name).sort(), [mcpServers]);

  const selectedTraceId = useTracesStore((s) => s.selectedTraceId);
  const filters = useTracesStore((s) => s.filters);
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
    if (server != null || errors != null || q != null) {
      setFilters({
        ...(server != null ? { server } : {}),
        ...(errors != null ? { errorsOnly: errors === '1' } : {}),
        ...(q != null ? { search: q } : {}),
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
        return params;
      },
      { replace: true },
    );
  }, [selectedTraceId, filters, setSearchParams]);

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <header
        className={cn(
          'flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle flex items-center gap-3 px-6',
          compact ? 'py-2' : 'py-3',
        )}
      >
        <div className="font-sans text-text-muted/60 text-[10px] uppercase tracking-[0.4em]">traces</div>
        <div className="font-mono text-[10px] text-text-muted truncate">
          {selectedTraceId ? selectedTraceId.slice(0, 16) : 'all tool calls'}
        </div>
      </header>
      <div className="flex-1 min-h-0">
        <TracesView
          active
          servers={servers}
          onViewLogs={(traceId) => navigate(`/logs?trace=${encodeURIComponent(traceId)}`)}
          toolbarExtra={
            <PopoutButton
              onClick={() => openDetachedWindow('traces')}
              tooltip="Open in separate window"
              disabled={tracesDetached}
            />
          }
        />
      </div>
    </div>
  );
}

export default TracesWorkspace;
