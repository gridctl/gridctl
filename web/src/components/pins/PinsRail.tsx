import { CheckCircle2, Lock, LockOpen, ShieldAlert } from 'lucide-react';
import { cn } from '../../lib/cn';
import type { ServerPins } from '../../lib/api';
import { countServerAlertFindings } from '../../stores/usePinsStore';
import { formatRelativeTime } from '../../lib/time';
import { pinStatusMeta } from './pinStatus';

// ---------------------------------------------------------------------------
// Left rail: attention toggle + server rows
// ---------------------------------------------------------------------------

interface PinsRailProps {
  compact: boolean;
  /** Rows to render (already attention-filtered by the workspace). */
  entries: Array<[string, ServerPins]>;
  /** Total pinned servers before filtering, for the all-clear state. */
  totalCount: number;
  activeServerName: string;
  onSelect: (name: string) => void;
  attentionOnly: boolean;
  onToggleAttention: () => void;
  /** Marks the row kept visible only because it is the active ?server=. */
  isOutsideFilter: (name: string) => boolean;
}

export function PinsRail({
  compact,
  entries,
  totalCount,
  activeServerName,
  onSelect,
  attentionOnly,
  onToggleAttention,
  isOutsideFilter,
}: PinsRailProps) {
  return (
    <aside className="h-full flex flex-col bg-surface border-r border-border-subtle">
      <div
        className={cn(
          'flex-shrink-0 px-3 border-b border-border-subtle/60 flex items-center justify-between gap-2',
          compact ? 'py-2' : 'py-3',
        )}
      >
        <div className="text-[10px] font-medium text-text-muted/60 uppercase tracking-[0.3em]">
          servers
        </div>
        <button
          onClick={onToggleAttention}
          aria-pressed={attentionOnly}
          title={
            attentionOnly
              ? 'Show all pinned servers'
              : 'Show only servers with drift or warn-or-critical findings'
          }
          className={cn(
            'h-6 px-2 text-[10px] font-medium rounded border transition-colors flex items-center gap-1 flex-shrink-0',
            attentionOnly
              ? 'bg-status-pending/15 text-status-pending border-status-pending/30'
              : 'bg-background/60 text-text-muted border-border/40 hover:text-text-secondary hover:border-border/60',
          )}
        >
          <ShieldAlert size={9} />
          Attention
        </button>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-2 py-2 space-y-0.5">
        {entries.map(([name, sp]) => {
          const active = name === activeServerName;
          const { label, colorClass } = pinStatusMeta(sp.status);
          const findingCount = countServerAlertFindings(sp);
          const outside = isOutsideFilter(name);
          return (
            <button
              key={name}
              onClick={() => onSelect(name)}
              aria-current={active}
              title={outside ? 'Not in the attention filter' : undefined}
              className={cn(
                'w-full flex flex-col gap-0.5 px-3 py-1.5 rounded-md text-left transition-colors',
                active
                  ? 'bg-primary/10 text-primary'
                  : 'text-text-secondary hover:bg-surface-highlight/50 hover:text-text-primary',
                outside && 'opacity-60',
              )}
            >
              <span className="flex items-center gap-2 w-full">
                <span className={cn('flex-shrink-0', colorClass)}>
                  {sp.status === 'drift' ? <LockOpen size={11} /> : <Lock size={11} />}
                </span>
                <span
                  className={cn(
                    'flex-1 min-w-0 text-xs font-mono truncate',
                    active && 'text-primary',
                  )}
                >
                  {name}
                </span>
                {findingCount > 0 && (
                  <span
                    className="flex-shrink-0 inline-flex items-center gap-0.5 text-[10px] text-status-pending"
                    title={`${findingCount} warn-or-critical finding${findingCount > 1 ? 's' : ''}`}
                    aria-label={`${findingCount} finding${findingCount > 1 ? 's' : ''} on ${name}`}
                  >
                    <ShieldAlert size={9} />
                    {findingCount}
                  </span>
                )}
                <span
                  className={cn(
                    'flex-shrink-0 text-[10px] px-1.5 py-0.5 rounded',
                    sp.status === 'drift'
                      ? 'bg-status-pending/15 text-status-pending'
                      : 'bg-surface-elevated text-text-muted',
                  )}
                >
                  {label}
                </span>
              </span>
              <span className="pl-[19px] text-[10px] text-text-muted/70 truncate">
                {sp.tool_count} {sp.tool_count === 1 ? 'tool' : 'tools'}
                {sp.last_verified_at
                  ? ` · verified ${formatRelativeTime(new Date(sp.last_verified_at))}`
                  : ''}
              </span>
            </button>
          );
        })}

        {attentionOnly && entries.length === 0 && (
          <div className="px-3 py-6 text-center space-y-2">
            <CheckCircle2 size={16} className="mx-auto text-status-running/70" />
            <p className="text-[11px] text-text-muted">All clear: no drift or findings.</p>
            <button
              onClick={onToggleAttention}
              className="text-[11px] text-primary hover:underline"
            >
              Show all {totalCount} {totalCount === 1 ? 'server' : 'servers'}
            </button>
          </div>
        )}
      </div>
    </aside>
  );
}
