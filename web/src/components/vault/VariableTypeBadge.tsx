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
    json: 'bg-violet-500/10 text-violet-300',
    list: 'bg-sky-500/10 text-sky-300',
    number: 'bg-emerald-500/10 text-emerald-300',
    bool: 'bg-amber-500/10 text-amber-300',
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
