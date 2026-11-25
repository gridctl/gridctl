import { memo } from 'react';
import { Webhook } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { StatePill } from '../../ui/StatePill';
import { hookClientName } from './hookModel';
import type { HookProjectionStatus, RegistryHook } from '../../../types';

export interface HookCardProps {
  hook: RegistryHook;
  statuses: HookProjectionStatus[];
  onSelect: (hook: RegistryHook) => void;
  isActive?: boolean;
  // Stored pin status is drift: an import is held for pin approval. This is
  // separate from the projection pills' "drifted" (a hand-edited client file).
  pinDrift?: boolean;
  onOpenPinDrift?: (name: string) => void;
}

export const HookCard = memo(({ hook, statuses, onSelect, isActive = false, pinDrift = false, onOpenPinDrift }: HookCardProps) => {
  return (
    <div
      aria-current={isActive ? 'true' : undefined}
      className={cn(
        'group relative rounded-xl overflow-hidden flex flex-col',
        'border transition-colors bg-surface/80 border-border-subtle hover:border-primary/40',
        isActive && 'border-primary/50 bg-primary/[0.06]',
      )}
    >
      <button
        type="button"
        className="p-3 flex flex-col gap-2 text-left"
        aria-label={`Inspect ${hook.name}`}
        onClick={() => onSelect(hook)}
      >
        <div className="flex items-start gap-2">
          <Webhook size={14} className="text-text-muted mt-0.5 flex-shrink-0" aria-hidden="true" />
          <span className="font-semibold text-sm text-text-primary truncate flex-1">{hook.name}</span>
        </div>
        <p className="text-2xs text-text-muted line-clamp-2">{hook.description}</p>
        <div className="flex items-center gap-1 flex-wrap" aria-label={`Projection chips for ${hook.name}`}>
          {statuses.length === 0 && (
            <span className="text-3xs text-text-muted">Not projected</span>
          )}
          {statuses.map((row) => (
            <span key={row.client} className="inline-flex items-center gap-1" title={row.detail}>
              <StatePill state={row.state} />
              <span className="text-3xs text-text-secondary">{hookClientName(row.client)}</span>
            </span>
          ))}
        </div>
      </button>
      {pinDrift && (
        <div className="px-3 pb-3 -mt-1">
          <button
            type="button"
            aria-label={`Review pin drift for ${hook.name}`}
            onClick={() => onOpenPinDrift?.(hook.name)}
            className="inline-flex items-center text-3xs font-medium px-1.5 py-0.5 rounded bg-status-pending/15 text-status-pending border border-status-pending/30 hover:bg-status-pending/25 transition-colors"
          >
            Pin drift
          </button>
        </div>
      )}
    </div>
  );
});
HookCard.displayName = 'HookCard';
