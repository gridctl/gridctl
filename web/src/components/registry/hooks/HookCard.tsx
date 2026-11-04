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
}

export const HookCard = memo(({ hook, statuses, onSelect, isActive = false }: HookCardProps) => {
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
            <span className="text-3xs text-text-muted/70">Not projected</span>
          )}
          {statuses.map((row) => (
            <span key={row.client} className="inline-flex items-center gap-1" title={row.detail}>
              <StatePill state={row.state} />
              <span className="text-3xs text-text-secondary">{hookClientName(row.client)}</span>
            </span>
          ))}
        </div>
      </button>
    </div>
  );
});
HookCard.displayName = 'HookCard';
