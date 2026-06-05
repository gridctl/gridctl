import { ArrowUpRight } from 'lucide-react';
import { isNavigable } from './consumerHelpers';
import type { Consumer } from '../../lib/api';

// How many consumers to show before collapsing behind a "see all" toggle.
export const CONSUMER_PREVIEW_LIMIT = 3;

export interface ConsumerListProps {
  consumers: Consumer[];
  // Collapsing state is host-owned so it survives re-renders of this list.
  showAll?: boolean;
  onToggleShowAll?: () => void;
  onConsumerClick?: (consumer: Consumer) => void;
  // Rows beyond this count collapse behind the toggle. Pass null to disable
  // collapsing entirely (the inspector shows every site).
  previewLimit?: number | null;
  // Overrides the default chrome (SecretItem's drill-down panel styling).
  className?: string;
}

// ConsumerList renders a variable's reference sites. Server/resource sites
// are clickable (they navigate to a topology node); other kinds render as
// plain rows. Long lists collapse behind a "see all" toggle unless the host
// disables it. Shared by SecretItem (VaultPanel) and VariableInspector.
export function ConsumerList({
  consumers,
  showAll = false,
  onToggleShowAll,
  onConsumerClick,
  previewLimit = CONSUMER_PREVIEW_LIMIT,
  className,
}: ConsumerListProps) {
  const visible =
    previewLimit === null || showAll
      ? consumers
      : consumers.slice(0, previewLimit);
  const hiddenCount = consumers.length - visible.length;
  const collapsible =
    previewLimit !== null && consumers.length > previewLimit;

  return (
    <div
      role="group"
      aria-label="Variables consuming this value"
      className={
        className ??
        'px-3 pb-2 pt-1 border-t border-border-subtle/60 space-y-0.5'
      }
    >
      {visible.map((c, i) => {
        const label = `${c.name || c.kind} · ${c.field}`;
        if (isNavigable(c) && onConsumerClick) {
          return (
            <button
              key={`${c.kind}-${c.name}-${c.field}-${i}`}
              onClick={(e) => {
                e.stopPropagation();
                onConsumerClick(c);
              }}
              aria-label={`Go to ${c.name} (${c.field})`}
              className="w-full flex items-center gap-1.5 px-2 py-1 rounded text-[10px] font-mono text-text-secondary hover:text-primary hover:bg-surface-highlight/50 transition-colors text-left"
            >
              <ArrowUpRight
                size={10}
                className="flex-shrink-0 text-text-muted"
              />
              <span className="truncate">{label}</span>
            </button>
          );
        }
        return (
          <div
            key={`${c.kind}-${c.name}-${c.field}-${i}`}
            className="flex items-center gap-1.5 px-2 py-1 text-[10px] font-mono text-text-muted"
          >
            <span className="w-2.5 flex-shrink-0 text-center">·</span>
            <span className="truncate">{label}</span>
          </div>
        );
      })}
      {collapsible && (hiddenCount > 0 || showAll) && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onToggleShowAll?.();
          }}
          className="px-2 py-0.5 text-[10px] text-primary hover:text-primary/80 transition-colors"
        >
          {showAll ? 'Show less' : `See all ${consumers.length}`}
        </button>
      )}
    </div>
  );
}
