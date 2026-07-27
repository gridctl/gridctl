import { useState } from 'react';
import { ArrowUpRight, ChevronDown, ChevronRight } from 'lucide-react';
import {
  consumerLabel,
  describeConsumer,
  groupConsumers,
  isNavigable,
} from './consumerHelpers';
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
  // Overrides the default chrome (the inspector's drill-down panel styling).
  className?: string;
}

// ConsumerList renders a variable's reference sites. Sites that map to a
// canvas node are clickable; the rest render as plain rows. Long lists
// collapse behind a "see all" toggle unless the host disables it.
export function ConsumerList({
  consumers,
  showAll = false,
  onToggleShowAll,
  onConsumerClick,
  previewLimit = CONSUMER_PREVIEW_LIMIT,
  className,
}: ConsumerListProps) {
  const entries = groupConsumers(consumers);
  const visible =
    previewLimit === null || showAll ? entries : entries.slice(0, previewLimit);
  const hiddenCount = entries.length - visible.length;
  const collapsible = previewLimit !== null && entries.length > previewLimit;

  return (
    <div
      role="group"
      aria-label="Variables consuming this value"
      className={
        className ??
        'px-3 pb-2 pt-1 border-t border-border-subtle/60 space-y-0.5'
      }
    >
      {visible.map((entry, i) =>
        entry.kind === 'one' ? (
          <ConsumerRow
            key={`one-${entry.consumer.kind}-${entry.consumer.name}-${entry.consumer.field}-${entry.consumer.target ?? ''}-${i}`}
            consumer={entry.consumer}
            onConsumerClick={onConsumerClick}
          />
        ) : (
          <SetGroupRow
            key={`group-${entry.setName}-${i}`}
            setName={entry.setName}
            consumers={entry.consumers}
            onConsumerClick={onConsumerClick}
          />
        ),
      )}
      {collapsible && (hiddenCount > 0 || showAll) && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onToggleShowAll?.();
          }}
          className="px-2 py-0.5 text-[10px] text-primary hover:text-primary/80 transition-colors"
        >
          {showAll ? 'Show less' : `See all ${entries.length}`}
        </button>
      )}
    </div>
  );
}

function ConsumerRow({
  consumer,
  onConsumerClick,
  indented,
}: {
  consumer: Consumer;
  onConsumerClick?: (consumer: Consumer) => void;
  indented?: boolean;
}) {
  const label = consumerLabel(consumer);
  const tooltip = describeConsumer(consumer);

  if (isNavigable(consumer) && onConsumerClick) {
    return (
      <button
        onClick={(e) => {
          e.stopPropagation();
          onConsumerClick(consumer);
        }}
        aria-label={`Go to ${consumer.target ?? consumer.name} (${consumer.field})`}
        title={tooltip}
        className={`w-full flex items-center gap-1.5 px-2 py-1 rounded text-[10px] font-mono text-text-secondary hover:text-primary hover:bg-surface-highlight/50 transition-colors text-left ${indented ? 'pl-5' : ''}`}
      >
        <ArrowUpRight size={10} className="flex-shrink-0 text-text-muted" />
        <span className="truncate">{label}</span>
      </button>
    );
  }
  return (
    <div
      title={tooltip}
      className={`flex items-center gap-1.5 px-2 py-1 text-[10px] font-mono text-text-muted ${indented ? 'pl-5' : ''}`}
    >
      <span className="w-2.5 flex-shrink-0 text-center">·</span>
      <span className="truncate">{label}</span>
    </div>
  );
}

// SetGroupRow summarizes one scoped set ("set: dev · 4 servers") and expands
// to the individual, navigable per-workload rows.
function SetGroupRow({
  setName,
  consumers,
  onConsumerClick,
}: {
  setName: string;
  consumers: Consumer[];
  onConsumerClick?: (consumer: Consumer) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const servers = consumers.filter((c) => c.targetKind !== 'resource').length;
  const resources = consumers.length - servers;
  const parts: string[] = [];
  if (servers > 0) parts.push(`${servers} ${servers === 1 ? 'server' : 'servers'}`);
  if (resources > 0) {
    parts.push(`${resources} ${resources === 1 ? 'resource' : 'resources'}`);
  }
  const summary = `set: ${setName} · ${parts.join(', ')}`;

  return (
    <div className="space-y-0.5">
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          setExpanded((v) => !v);
        }}
        aria-expanded={expanded}
        title={`The "${setName}" set is scoped to named workloads and injects into ${parts.join(' and ')} (secrets.sets in stack.yaml)`}
        className="w-full flex items-center gap-1.5 px-2 py-1 rounded text-[10px] font-mono text-text-secondary hover:text-text-primary hover:bg-surface-highlight/50 transition-colors text-left"
      >
        {expanded ? (
          <ChevronDown size={10} className="flex-shrink-0 text-text-muted" />
        ) : (
          <ChevronRight size={10} className="flex-shrink-0 text-text-muted" />
        )}
        <span className="truncate">{summary}</span>
      </button>
      {expanded &&
        consumers.map((c, i) => (
          <ConsumerRow
            key={`${c.target}-${i}`}
            consumer={c}
            onConsumerClick={onConsumerClick}
            indented
          />
        ))}
    </div>
  );
}
