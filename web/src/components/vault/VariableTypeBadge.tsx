import { cn } from '../../lib/cn';
import type { VariableType } from '../../lib/api';

// VariableTypeBadge renders a small inline badge for a variable's type.
// `string` is the common case and renders nothing to reduce noise on rows
// where the type is unsurprising.
export function VariableTypeBadge({
  type,
  className,
}: {
  type: VariableType;
  className?: string;
}) {
  if (type === 'string') return null;

  const palette: Record<VariableType, string> = {
    string: 'bg-surface text-text-muted',
    json: 'bg-tertiary/10 text-tertiary-light',
    list: 'bg-secondary/10 text-secondary-light',
    number: 'bg-status-running/10 text-status-running',
    bool: 'bg-status-pending/10 text-status-pending',
  };

  return (
    <span
      className={cn(
        'inline-flex items-center rounded-md px-1.5 py-px text-[10px] font-mono font-medium',
        palette[type],
        className,
      )}
      title={`type: ${type}`}
    >
      {type}
    </span>
  );
}
