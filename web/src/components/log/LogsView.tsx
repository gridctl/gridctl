import { useCallback, useRef, useState } from 'react';
import { Copy, Download, Pause, Play, RefreshCw, Trash2, X } from 'lucide-react';
import { useLogFontSize } from '../../hooks/useLogFontSize';
import { useDismiss } from '../../hooks/useDismiss';
import { IconButton } from '../ui/IconButton';
import { showToast } from '../ui/Toast';
import { downloadTextFile, logExportFilename } from '../../lib/download';
import { LogFilterBar } from './LogFilterBar';
import { LogStream } from './LogStream';
import { GATEWAY_LOG_SOURCE, serializeLogsJSONL, serializeLogsText } from './logTypes';
import type { LogsViewState } from './useLogsView';

interface LogsViewProps {
  view: LogsViewState;
  /** When set, trace IDs render as pivots into the trace view. */
  onTraceClick?: (traceId: string) => void;
  /** Slot above the first entry (e.g. PersistedFromMarker). */
  header?: React.ReactNode;
  emptyText?: string;
  /** Host-specific toolbar actions (popout, fullscreen), rendered rightmost. */
  toolbarExtra?: React.ReactNode;
}

// Shared log surface: one control bar (search, levels, Errors, filter chips,
// view options, zoom, counts, live state, and every stream action) plus the
// stream and the keyboard hint strip. Consumed by the Logs workspace and the
// detached window so the two never diverge — host chrome contributes only
// identity (title, source picker) and a `toolbarExtra` slot.
export function LogsView({
  view,
  onTraceClick,
  header,
  emptyText,
  toolbarExtra,
}: LogsViewProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } =
    useLogFontSize(containerRef);

  const [exportOpen, setExportOpen] = useState(false);
  const exportRef = useDismiss<HTMLDivElement>(exportOpen, () => setExportOpen(false));

  const exportFiltered = useCallback(
    (format: 'jsonl' | 'txt') => {
      setExportOpen(false);
      const logs = view.filteredLogs;
      if (logs.length === 0) return;
      const content = format === 'jsonl' ? serializeLogsJSONL(logs) : serializeLogsText(logs);
      downloadTextFile(content, logExportFilename(format), format === 'jsonl' ? 'application/jsonl' : 'text/plain');
      showToast('success', `Exported ${logs.length} entries as ${format.toUpperCase()}`);
    },
    [view.filteredLogs],
  );

  const trailing = (
    <div className="flex items-center gap-1 flex-shrink-0">
      {/* Live / paused state */}
      {view.isPaused ? (
        <span className="text-[10px] px-2 py-0.5 bg-status-pending/15 text-status-pending rounded-full font-medium border border-status-pending/20">
          Paused
        </span>
      ) : (
        <span className="flex items-center gap-1 text-[9px] text-status-running font-medium">
          <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse motion-reduce:animate-none" />
          Live
        </span>
      )}
      <IconButton
        icon={view.isPaused ? Play : Pause}
        onClick={view.togglePause}
        tooltip={view.isPaused ? 'Resume live updates' : 'Pause live updates'}
        pressed={view.isPaused}
        size="sm"
        variant="ghost"
        className={view.isPaused ? 'text-status-running hover:text-status-running' : ''}
      />
      <IconButton icon={RefreshCw} onClick={view.refresh} tooltip="Refresh" size="sm" variant="ghost" />
      <IconButton
        icon={Copy}
        onClick={view.copyFiltered}
        tooltip="Copy filtered view"
        size="sm"
        variant="ghost"
        disabled={view.filteredLogs.length === 0}
      />
      <div ref={exportRef} className="relative">
        <IconButton
          icon={Download}
          onClick={() => setExportOpen((v) => !v)}
          tooltip="Export filtered view"
          size="sm"
          variant="ghost"
          disabled={view.filteredLogs.length === 0}
        />
        {exportOpen && (
          <div className="absolute right-0 top-full mt-1 z-50 min-w-[130px] py-1 rounded-lg border border-border bg-surface-elevated/95 backdrop-blur-xl shadow-bevel animate-fade-in-scale">
            <button
              onClick={() => exportFiltered('jsonl')}
              className="w-full px-3 py-1.5 text-left text-[10px] text-text-secondary hover:bg-surface-highlight hover:text-text-primary transition-colors"
            >
              Export as JSONL
            </button>
            <button
              onClick={() => exportFiltered('txt')}
              className="w-full px-3 py-1.5 text-left text-[10px] text-text-secondary hover:bg-surface-highlight hover:text-text-primary transition-colors"
            >
              Export as TXT
            </button>
          </div>
        )}
      </div>
      <IconButton
        icon={Trash2}
        onClick={view.clear}
        tooltip="Clear this view (server buffer unchanged)"
        size="sm"
        variant="ghost"
        className="hover:text-status-error"
      />
      {toolbarExtra && (
        <>
          <div className="w-px h-4 bg-border/50 mx-0.5" />
          {toolbarExtra}
        </>
      )}
    </div>
  );

  return (
    <>
      <LogFilterBar
        searchQuery={view.searchQuery}
        onSearchChange={view.setSearchQuery}
        searchInputRef={searchInputRef}
        enabledLevels={view.enabledLevels}
        onToggleLevel={view.toggleLevel}
        errorsOnly={view.errorsOnly}
        onToggleErrorsOnly={view.toggleErrorsOnly}
        fontSize={fontSize}
        onZoomIn={zoomIn}
        onZoomOut={zoomOut}
        onResetZoom={resetZoom}
        isMin={isMin}
        isMax={isMax}
        isDefault={isDefault}
        filteredCount={view.filteredLogs.length}
        totalCount={view.logs.length}
        windowSize={view.windowSize}
        onWindowSizeChange={view.setWindowSize}
        timeRange={view.timeRange}
        onTimeRangeChange={view.setTimeRange}
        wrap={view.wrap}
        onToggleWrap={view.toggleWrap}
        relativeTime={view.relativeTime}
        onToggleRelativeTime={view.toggleRelativeTime}
        bufferTotal={view.bufferTotal}
        bufferCapacity={view.bufferCapacity}
        trailing={trailing}
      >
        {view.source != null && (
          <FilterChip
            label={`source: ${view.source === GATEWAY_LOG_SOURCE ? 'gateway' : view.source}`}
            title="Clear source filter"
            ariaLabel="Clear source filter"
            onClear={() => view.setSource(null)}
          />
        )}
        {view.traceFilter && (
          <FilterChip
            label={`trace: ${view.traceFilter.slice(0, 8)}`}
            title="Clear trace filter"
            onClear={view.clearTraceFilter}
          />
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
        onSourceClick={(s) => view.setSource(s)}
        fontSize={fontSize}
        containerRef={containerRef}
        header={header}
        emptyText={emptyText}
        expandedKey={view.expandedKey}
        onToggleExpand={view.setExpandedKey}
        searchQuery={view.searchQuery}
        wrap={view.wrap}
        relativeTime={view.relativeTime}
        timeAnchor={view.lastLoadedAt > 0 ? view.lastLoadedAt : undefined}
        source={view.source}
        traceFilter={view.traceFilter}
        onShowAllSources={() => view.setSource(null)}
        onClearTrace={view.clearTraceFilter}
        windowSize={view.windowSize}
        onFocusSearch={() => searchInputRef.current?.focus()}
      />

      {/* Keyboard hint strip */}
      <div className="flex items-center gap-2 px-3 h-5 flex-shrink-0 border-t border-border/20 text-[9px] text-text-muted/60 select-none">
        <span><kbd className="font-mono">j</kbd>/<kbd className="font-mono">k</kbd> navigate</span>
        <span><kbd className="font-mono">↵</kbd> expand</span>
        <span><kbd className="font-mono">/</kbd> search</span>
        <span><kbd className="font-mono">esc</kbd> collapse</span>
      </div>
    </>
  );
}

// Removable active-filter chip (source, trace) in the shared chip row.
function FilterChip({
  label,
  title,
  ariaLabel,
  onClear,
}: {
  label: string;
  title: string;
  ariaLabel?: string;
  onClear: () => void;
}) {
  return (
    <button
      onClick={onClear}
      title={title}
      aria-label={ariaLabel}
      className="flex items-center gap-1 h-6 px-2 text-[10px] font-mono rounded border bg-primary/10 text-primary border-primary/30 hover:bg-primary/15 transition-colors flex-shrink-0"
    >
      {label}
      <X size={9} />
    </button>
  );
}
