import { useState, type RefObject } from 'react';
import { AlertCircle, Search, SlidersHorizontal } from 'lucide-react';
import { cn } from '../../lib/cn';
import { LevelFilter } from './LevelFilter';
import { ZoomControls } from '../ui/ZoomControls';
import { IconButton } from '../ui/IconButton';
import { useDismiss } from '../../hooks/useDismiss';
import { LOG_TIME_RANGES, LOG_WINDOW_SIZES, type LogLevel, type LogTimeRange } from './logTypes';

interface LogFilterBarProps {
  searchQuery: string;
  onSearchChange: (query: string) => void;
  /** Focusable from the '/' keyboard binding. */
  searchInputRef?: RefObject<HTMLInputElement | null>;
  enabledLevels: Set<LogLevel>;
  onToggleLevel: (level: LogLevel) => void;
  /** One-click Errors state: level set is exactly {ERROR}. */
  errorsOnly: boolean;
  onToggleErrorsOnly: () => void;
  fontSize: number;
  onZoomIn: () => void;
  onZoomOut: () => void;
  onResetZoom: () => void;
  isMin: boolean;
  isMax: boolean;
  isDefault: boolean;
  filteredCount: number;
  totalCount: number;
  /** Poll window size; when set, the count labels the view as "last N". */
  windowSize?: number;
  onWindowSizeChange?: (n: number) => void;
  timeRange?: LogTimeRange;
  onTimeRangeChange?: (range: LogTimeRange) => void;
  wrap?: boolean;
  onToggleWrap?: () => void;
  relativeTime?: boolean;
  onToggleRelativeTime?: () => void;
  /** Server ring occupancy/capacity for honest window labeling. */
  bufferTotal?: number;
  bufferCapacity?: number;
  /** Extra chips (e.g. active source/trace filters) rendered after the level filter. */
  children?: React.ReactNode;
  /** Action cluster (Live pill, pause, copy, export, ...) rendered rightmost. */
  trailing?: React.ReactNode;
}

// The shared logs control bar: search, level and Errors filters, active
// filter chips, the view-options popover, zoom, the window-honest count, and
// a trailing action-cluster slot. One bar serves the Logs workspace and the
// detached window. Purely presentational; all state lives with the caller.
export function LogFilterBar({
  searchQuery,
  onSearchChange,
  searchInputRef,
  enabledLevels,
  onToggleLevel,
  errorsOnly,
  onToggleErrorsOnly,
  fontSize,
  onZoomIn,
  onZoomOut,
  onResetZoom,
  isMin,
  isMax,
  isDefault,
  filteredCount,
  totalCount,
  windowSize,
  onWindowSizeChange,
  timeRange = 'all',
  onTimeRangeChange,
  wrap = false,
  onToggleWrap,
  relativeTime = false,
  onToggleRelativeTime,
  bufferTotal = 0,
  bufferCapacity = 0,
  children,
  trailing,
}: LogFilterBarProps) {
  const [viewOptionsOpen, setViewOptionsOpen] = useState(false);
  const viewOptionsRef = useDismiss<HTMLDivElement>(viewOptionsOpen, () => setViewOptionsOpen(false));
  const viewOptionsActive = timeRange !== 'all' || wrap || relativeTime;

  return (
    <div className="flex items-center gap-2 px-4 py-2 border-b border-border/30 bg-surface-elevated/30 flex-shrink-0">
      {/* Search input */}
      <div className="relative flex-1 max-w-xs">
        <Search
          size={12}
          className="absolute left-2.5 top-1/2 -translate-y-1/2 text-text-muted"
        />
        <input
          ref={searchInputRef}
          type="text"
          placeholder="Filter logs..."
          value={searchQuery}
          onChange={(e) => onSearchChange(e.target.value)}
          className={cn(
            'w-full pl-7 pr-3 py-1.5 text-xs font-mono',
            'bg-background/60 border border-border/50 rounded-md',
            'text-text-primary placeholder:text-text-muted',
            'focus:outline-none focus:border-primary/50 focus:ring-1 focus:ring-primary/20',
            'transition-all duration-200'
          )}
        />
      </div>

      {/* Level filter */}
      <LevelFilter enabledLevels={enabledLevels} onToggle={onToggleLevel} />

      {/* One-click Errors — drives the same level state as the multi-toggle */}
      <button
        onClick={onToggleErrorsOnly}
        aria-pressed={errorsOnly}
        title={errorsOnly ? 'Restore previous level selection' : 'Show only ERROR entries'}
        className={cn(
          'h-6 px-2 text-[10px] font-medium rounded border transition-colors flex items-center gap-1 flex-shrink-0',
          errorsOnly
            ? 'bg-status-error/15 text-status-error border-status-error/30'
            : 'bg-background/60 text-text-muted border-border/40 hover:text-text-secondary hover:border-border/60'
        )}
      >
        <AlertCircle size={9} />
        Errors
      </button>

      {children}

      {/* Filters popover: window size, time range, wrap, relative time */}
      <div ref={viewOptionsRef} className="relative flex-shrink-0">
        <IconButton
          icon={SlidersHorizontal}
          onClick={() => setViewOptionsOpen((v) => !v)}
          tooltip="View options"
          size="sm"
          variant="ghost"
          className={cn(viewOptionsActive && 'ring-1 ring-primary/30 rounded')}
        />
        {viewOptionsOpen && (
          <div
            className={cn(
              'absolute right-0 top-full mt-1 z-50 w-56 p-2',
              'rounded-lg border border-border bg-surface-elevated/95',
              'backdrop-blur-xl shadow-bevel animate-fade-in-scale'
            )}
          >
            <div className="px-1 py-1 text-[9px] uppercase tracking-[0.18em] text-text-muted/70">
              View options
            </div>
            {onWindowSizeChange && (
              <div className="flex items-center justify-between gap-2 px-1 py-1.5">
                <span className="text-[10px] text-text-secondary">Window</span>
                <div className="flex items-center bg-background/60 rounded-md border border-border/40 overflow-hidden">
                  {LOG_WINDOW_SIZES.map((n) => (
                    <button
                      key={n}
                      onClick={() => onWindowSizeChange(n)}
                      aria-pressed={windowSize === n}
                      className={cn(
                        'px-1.5 py-0.5 text-[9px] font-medium transition-colors',
                        windowSize === n
                          ? 'bg-primary/15 text-primary'
                          : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40'
                      )}
                    >
                      {n}
                    </button>
                  ))}
                </div>
              </div>
            )}
            {onTimeRangeChange && (
              <div className="flex items-center justify-between gap-2 px-1 py-1.5">
                <span className="text-[10px] text-text-secondary">Time range</span>
                <div className="flex items-center bg-background/60 rounded-md border border-border/40 overflow-hidden">
                  {LOG_TIME_RANGES.map((range) => (
                    <button
                      key={range.value}
                      onClick={() => onTimeRangeChange(range.value)}
                      aria-pressed={timeRange === range.value}
                      className={cn(
                        'px-1.5 py-0.5 text-[9px] font-medium transition-colors',
                        timeRange === range.value
                          ? 'bg-primary/15 text-primary'
                          : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40'
                      )}
                    >
                      {range.label}
                    </button>
                  ))}
                </div>
              </div>
            )}
            {onToggleWrap && (
              <PopoverToggle label="Wrap lines" pressed={wrap} onToggle={onToggleWrap} />
            )}
            {onToggleRelativeTime && (
              <PopoverToggle
                label="Relative timestamps"
                pressed={relativeTime}
                onToggle={onToggleRelativeTime}
              />
            )}
          </div>
        )}
      </div>

      {/* Zoom controls */}
      <ZoomControls
        fontSize={fontSize}
        onZoomIn={onZoomIn}
        onZoomOut={onZoomOut}
        onReset={onResetZoom}
        isMin={isMin}
        isMax={isMax}
        isDefault={isDefault}
      />

      {/* Log count, honest against the poll window and the server ring */}
      <span
        className="text-[10px] text-text-muted font-mono ml-auto whitespace-nowrap"
        title={
          windowSize
            ? `Showing the most recent ${windowSize} buffer entries${bufferCapacity > 0 ? ` · ring holds ${bufferTotal} of ${bufferCapacity}` : ''}`
            : undefined
        }
      >
        {filteredCount} / {totalCount} entries
        {windowSize ? ` · last ${windowSize}` : ''}
        {bufferCapacity > 0 ? ` · buffer ${bufferTotal}/${bufferCapacity}` : ''}
      </span>

      {trailing}
    </div>
  );
}

function PopoverToggle({
  label,
  pressed,
  onToggle,
}: {
  label: string;
  pressed: boolean;
  onToggle: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-2 px-1 py-1.5">
      <span className="text-[10px] text-text-secondary">{label}</span>
      <button
        onClick={onToggle}
        aria-pressed={pressed}
        className={cn(
          'px-1.5 py-0.5 text-[9px] font-medium rounded border transition-colors',
          pressed
            ? 'bg-primary/15 text-primary border-primary/30'
            : 'bg-background/60 text-text-muted border-border/40 hover:text-text-secondary'
        )}
      >
        {pressed ? 'On' : 'Off'}
      </button>
    </div>
  );
}
