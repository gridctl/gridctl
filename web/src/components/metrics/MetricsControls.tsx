import { useRef, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { Pause, Play, RefreshCw, Trash2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { showToast } from '../ui/Toast';
import { describeError, isCancellation } from '../../lib/describeError';
import { METRICS_TIME_RANGES, type MetricsTimeRange } from '../../hooks/useMetricsSeries';

// MetricsControls is the shared control cluster for every metrics surface:
// the time-range segmented control, a live/pause toggle, refresh, clear (with
// a confirm dialog), and a host-supplied `right` slot (popout in-shell,
// fullscreen detached). Pulling it out of the hosts keeps their toolbars
// identical and the clear-confirm logic in one place. The cluster also owns
// the outcome reporting for both actions: a spinner and a polite status
// message for refresh, and a toast for clear.
export function MetricsControls({
  timeRange,
  onTimeRange,
  isPaused,
  onTogglePause,
  onRefresh,
  onClear,
  right,
  className,
}: {
  timeRange: MetricsTimeRange;
  onTimeRange: (range: MetricsTimeRange) => void;
  isPaused: boolean;
  onTogglePause: () => void;
  onRefresh: () => Promise<boolean>;
  onClear: () => Promise<void>;
  right?: ReactNode;
  className?: string;
}) {
  const [showClearConfirm, setShowClearConfirm] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [refreshMsg, setRefreshMsg] = useState('');
  // Refs, not state: a second click in the same frame must see the flag.
  const refreshPending = useRef(false);
  const clearPending = useRef(false);

  const handleRefresh = async () => {
    if (refreshPending.current) return;
    refreshPending.current = true;
    setRefreshing(true);
    // Emptied first so a repeat result is announced again.
    setRefreshMsg('');
    let ok = false;
    try {
      ok = await onRefresh();
    } catch {
      ok = false;
    } finally {
      refreshPending.current = false;
      setRefreshing(false);
    }
    setRefreshMsg(ok ? 'Metrics refreshed' : 'Metrics refresh failed');
  };

  const handleClear = async () => {
    if (clearPending.current) return;
    clearPending.current = true;
    try {
      await onClear();
      showToast('success', 'Metrics cleared');
    } catch (err) {
      if (!isCancellation(err)) {
        const d = describeError(err, 'Clear metrics');
        showToast('error', d.text, { detail: d.fullDetail });
      }
    } finally {
      clearPending.current = false;
    }
  };

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
                'px-2.5 py-1 text-3xs font-medium transition-colors focus-visible:-outline-offset-2',
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
          <span className="flex items-center gap-1 text-3xs text-status-running font-medium">
            <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse motion-reduce:animate-none" />
            Live
          </span>
        )}
        {timeRange === 'live' && isPaused && (
          <span className="text-3xs px-2 py-0.5 bg-status-pending/15 text-status-pending rounded-full font-medium border border-status-pending/20">
            Paused
          </span>
        )}
      </div>

      <div className="flex items-center gap-1">
        <span className="sr-only" role="status" aria-live="polite">{refreshMsg}</span>
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
        <IconButton
          icon={RefreshCw}
          onClick={() => void handleRefresh()}
          tooltip="Refresh"
          size="sm"
          variant="ghost"
          iconClassName={refreshing ? 'animate-spin motion-reduce:animate-none' : undefined}
        />

        <IconButton
          icon={Trash2}
          onClick={() => setShowClearConfirm(true)}
          tooltip="Clear Metrics"
          size="sm"
          variant="ghost"
          className="hover:text-status-error"
        />
        {/* Portaled: both hosts render this cluster inside a backdrop-blur header,
            which would trap a fixed dialog. */}
        {createPortal(
          <ConfirmDialog
            isOpen={showClearConfirm}
            onClose={() => setShowClearConfirm(false)}
            onConfirm={() => {
              setShowClearConfirm(false);
              void handleClear();
            }}
            title="Clear all metrics?"
            message="This wipes all recorded token and tool usage history. It cannot be undone."
            confirmLabel="Clear"
            variant="danger"
          />,
          document.body,
        )}

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
