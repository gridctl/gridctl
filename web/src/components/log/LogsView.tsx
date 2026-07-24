import { useRef } from 'react';
import { X } from 'lucide-react';
import { useLogFontSize } from '../../hooks/useLogFontSize';
import { LOG_STREAM_WINDOW } from '../../hooks/useLogStream';
import { LogFilterBar } from './LogFilterBar';
import { LogStream } from './LogStream';
import type { LogsViewState } from './useLogsView';

interface LogsViewProps {
  view: LogsViewState;
  /** When set, trace IDs render as pivots into the trace view. */
  onTraceClick?: (traceId: string) => void;
  /** Slot above the first entry (e.g. PersistedFromMarker). */
  header?: React.ReactNode;
  emptyText?: string;
}

// Shared log surface: filter bar (search, levels, trace chip, zoom, counts)
// plus the stream. Consumed by the Logs workspace and the detached window so
// the two never diverge on filter semantics or rendering.
export function LogsView({ view, onTraceClick, header, emptyText }: LogsViewProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } =
    useLogFontSize(containerRef);

  return (
    <>
      <LogFilterBar
        searchQuery={view.searchQuery}
        onSearchChange={view.setSearchQuery}
        enabledLevels={view.enabledLevels}
        onToggleLevel={view.toggleLevel}
        fontSize={fontSize}
        onZoomIn={zoomIn}
        onZoomOut={zoomOut}
        onResetZoom={resetZoom}
        isMin={isMin}
        isMax={isMax}
        isDefault={isDefault}
        filteredCount={view.filteredLogs.length}
        totalCount={view.logs.length}
        windowSize={LOG_STREAM_WINDOW}
      >
        {view.traceFilter && (
          <button
            onClick={view.clearTraceFilter}
            title="Clear trace filter"
            className="flex items-center gap-1 h-6 px-2 text-[10px] font-mono rounded border bg-primary/10 text-primary border-primary/30 hover:bg-primary/15 transition-colors"
          >
            trace: {view.traceFilter.slice(0, 8)}
            <X size={9} />
          </button>
        )}
      </LogFilterBar>

      <LogStream
        logs={view.filteredLogs}
        isLoading={view.isLoading}
        error={view.error}
        hasActiveFilter={view.hasActiveFilter}
        onClearFilter={view.clearFilters}
        showSource={view.source == null}
        onTraceClick={onTraceClick}
        fontSize={fontSize}
        containerRef={containerRef}
        header={header}
        emptyText={emptyText}
      />
    </>
  );
}
