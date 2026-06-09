import { useState, type ReactNode } from 'react';
import { Pause, Play, RefreshCw, Trash2, DollarSign } from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { METRICS_TIME_RANGES, type MetricsTimeRange } from '../../hooks/useMetricsSeries';

// MetricsControls is the shared control cluster for every metrics surface:
// the time-range segmented control, a live/pause toggle, refresh, clear (with
// an inline confirm), the pricing-manager opener, and a host-supplied `right`
// slot (popout in-shell, fullscreen detached). Pulling it out of the three
// hosts keeps their toolbars identical and the clear-confirm logic in one place.
export function MetricsControls({
  timeRange,
  onTimeRange,
  isPaused,
  onTogglePause,
  onRefresh,
  onClear,
  onOpenPricing,
  right,
  className,
}: {
  timeRange: MetricsTimeRange;
  onTimeRange: (range: MetricsTimeRange) => void;
  isPaused: boolean;
  onTogglePause: () => void;
  onRefresh: () => void;
  onClear: () => void;
  onOpenPricing: () => void;
  right?: ReactNode;
  className?: string;
}) {
  const [showClearConfirm, setShowClearConfirm] = useState(false);

  return (
    <div className={cn('flex items-center justify-between gap-2', className)}>
      <div className="flex items-center gap-2">
        <div className="flex items-center bg-background/60 rounded-md border border-border/40 overflow-hidden">
          {METRICS_TIME_RANGES.map((range) => (
            <button
              key={range.value}
              onClick={() => onTimeRange(range.value)}
              aria-pressed={timeRange === range.value}
              className={cn(
                'px-2.5 py-1 text-[10px] font-medium transition-colors',
                timeRange === range.value
                  ? 'bg-primary/15 text-primary'
                  : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40',
              )}
            >
              {range.label}
            </button>
          ))}
        </div>

        {timeRange === 'live' && !isPaused && (
          <span className="flex items-center gap-1 text-[9px] text-status-running font-medium">
            <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse motion-reduce:animate-none" />
            Live
          </span>
        )}
        {timeRange === 'live' && isPaused && (
          <span className="text-[10px] px-2 py-0.5 bg-status-pending/15 text-status-pending rounded-full font-medium border border-status-pending/20">
            Paused
          </span>
        )}
      </div>

      <div className="flex items-center gap-1">
        {timeRange === 'live' && (
          <IconButton
            icon={isPaused ? Play : Pause}
            onClick={onTogglePause}
            tooltip={isPaused ? 'Resume live updates' : 'Pause live updates'}
            size="sm"
            variant="ghost"
            className={isPaused ? 'text-status-running hover:text-status-running' : ''}
          />
        )}
        <IconButton icon={RefreshCw} onClick={onRefresh} tooltip="Refresh" size="sm" variant="ghost" />

        <div className="relative">
          <IconButton
            icon={Trash2}
            onClick={() => setShowClearConfirm(true)}
            tooltip="Clear Metrics"
            size="sm"
            variant="ghost"
            className="hover:text-status-error"
          />
          {showClearConfirm && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setShowClearConfirm(false)} />
              <div className="absolute right-0 top-full mt-1 z-50 p-3 rounded-lg bg-surface-elevated border border-border/50 shadow-xl min-w-[220px]">
                <p className="text-xs text-text-primary font-medium mb-2">Clear all token metrics?</p>
                <p className="text-[10px] text-text-muted mb-3">This cannot be undone.</p>
                <div className="flex items-center gap-2 justify-end">
                  <button
                    onClick={() => setShowClearConfirm(false)}
                    className="px-2.5 py-1 text-[10px] font-medium rounded-md bg-surface-highlight text-text-secondary hover:text-text-primary transition-colors"
                  >
                    Cancel
                  </button>
                  <button
                    onClick={() => {
                      setShowClearConfirm(false);
                      onClear();
                    }}
                    className="px-2.5 py-1 text-[10px] font-medium rounded-md bg-status-error/15 text-status-error hover:bg-status-error/25 transition-colors"
                  >
                    Clear
                  </button>
                </div>
              </div>
            </>
          )}
        </div>

        <IconButton icon={DollarSign} onClick={onOpenPricing} tooltip="Edit pricing models" size="sm" variant="ghost" />

        {right && (
          <>
            <div className="w-px h-4 bg-border/50 mx-0.5" />
            {right}
          </>
        )}
      </div>
    </div>
  );
}
