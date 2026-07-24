import { Search } from 'lucide-react';
import { cn } from '../../lib/cn';
import { LevelFilter } from './LevelFilter';
import { ZoomControls } from '../ui/ZoomControls';
import type { LogLevel } from './logTypes';

interface LogFilterBarProps {
  searchQuery: string;
  onSearchChange: (query: string) => void;
  enabledLevels: Set<LogLevel>;
  onToggleLevel: (level: LogLevel) => void;
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
  /** Extra chips (e.g. an active trace filter) rendered after the level filter. */
  children?: React.ReactNode;
}

// Search + level + zoom row shared by the Logs workspace and the detached
// logs window. Purely presentational; all state lives with the caller.
export function LogFilterBar({
  searchQuery,
  onSearchChange,
  enabledLevels,
  onToggleLevel,
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
  children,
}: LogFilterBarProps) {
  return (
    <div className="flex items-center gap-2 px-4 py-2 border-b border-border/30 bg-surface-elevated/30 flex-shrink-0">
      {/* Search input */}
      <div className="relative flex-1 max-w-xs">
        <Search
          size={12}
          className="absolute left-2.5 top-1/2 -translate-y-1/2 text-text-muted"
        />
        <input
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

      {children}

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

      {/* Log count */}
      <span
        className="text-[10px] text-text-muted font-mono ml-auto"
        title={windowSize ? `Showing the most recent ${windowSize} buffer entries` : undefined}
      >
        {filteredCount} / {totalCount} entries
        {windowSize ? ` · last ${windowSize}` : ''}
      </span>
    </div>
  );
}
