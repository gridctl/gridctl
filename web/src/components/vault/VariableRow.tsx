import { useEffect, useRef } from 'react';
import { Link2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { VariableTypeBadge } from './VariableTypeBadge';
import { VariableVisibilityIcon } from './VariableVisibilityIcon';
import type { Variable } from '../../lib/api';

// Shared column template so the sticky header row and every variable row
// align: Key | Type | Set | Value preview | Used by.
export const VARIABLE_ROW_GRID =
  'grid grid-cols-[minmax(0,1.6fr)_4rem_minmax(0,0.8fr)_minmax(0,1.2fr)_3.25rem] items-center gap-2';

export interface VariableRowProps {
  variable: Variable;
  selected: boolean;
  onSelect: () => void;
  // Pre-computed preview text: the actual value for loaded plaintext, a
  // fixed-length mask for secrets — never the raw secret.
  preview: string;
  consumerCount: number;
  compact?: boolean;
}

// VariableRow is the workspace's table-like list row. Unlike SecretItem (the
// sidebar's expandable card) it carries no inline expand/edit — clicking
// selects the variable and all depth lives in the right-rail inspector.
export function VariableRow({
  variable,
  selected,
  onSelect,
  preview,
  consumerCount,
  compact,
}: VariableRowProps) {
  const ref = useRef<HTMLButtonElement>(null);

  // Keep the selected row visible when selection moves via keyboard.
  useEffect(() => {
    if (selected) ref.current?.scrollIntoView?.({ block: 'nearest' });
  }, [selected]);

  return (
    <button
      ref={ref}
      type="button"
      role="option"
      aria-selected={selected}
      onClick={onSelect}
      className={cn(
        'w-full text-left px-6 border-l-2 border-b border-border-subtle/40 transition-colors',
        'focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary/40 focus-visible:ring-inset',
        VARIABLE_ROW_GRID,
        compact ? 'py-1.5' : 'py-2.5',
        selected
          ? 'border-l-primary bg-primary/[0.06]'
          : 'border-l-transparent hover:bg-surface-highlight/40',
      )}
    >
      <span className="flex items-center gap-2 min-w-0">
        <VariableVisibilityIcon isSecret={variable.is_secret} />
        <span className="text-xs font-mono font-medium text-text-primary truncate">
          {variable.key}
        </span>
      </span>
      <span>
        {variable.type === 'string' ? (
          <span className="text-[10px] font-mono text-text-muted/50">
            string
          </span>
        ) : (
          <VariableTypeBadge type={variable.type} />
        )}
      </span>
      <span className="min-w-0">
        {variable.set ? (
          <span className="inline-flex max-w-full items-center text-[10px] font-mono px-1.5 py-0.5 rounded bg-surface-elevated text-text-secondary truncate">
            {variable.set}
          </span>
        ) : (
          <span className="text-[10px] text-text-muted/40">—</span>
        )}
      </span>
      <span className="text-[10px] font-mono text-text-muted truncate">
        {preview}
      </span>
      <span className="flex justify-end">
        {consumerCount > 0 && (
          <span
            title={`Used by ${consumerCount} ${consumerCount === 1 ? 'consumer' : 'consumers'}`}
            className="inline-flex items-center gap-1 rounded-md px-1.5 py-px text-[10px] font-mono bg-surface-elevated text-text-muted"
          >
            <Link2 size={9} />
            {consumerCount}
          </span>
        )}
      </span>
    </button>
  );
}
