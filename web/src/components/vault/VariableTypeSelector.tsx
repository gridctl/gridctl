import { cn } from '../../lib/cn';
import type { VariableType } from '../../lib/api';

const TYPES: VariableType[] = ['string', 'json', 'list', 'number', 'bool'];

// VariableTypeSelector is a compact segmented control for choosing a
// variable's type in add/edit forms. PR 1 records the type as metadata only;
// PR 2 will wire type-aware expansion.
export function VariableTypeSelector({
  value,
  onChange,
  className,
}: {
  value: VariableType;
  onChange: (next: VariableType) => void;
  className?: string;
}) {
  return (
    <div
      role="group"
      aria-label="Variable type"
      className={cn(
        'inline-flex items-center rounded-md border border-border/40 bg-background/60 p-px',
        className,
      )}
    >
      {TYPES.map((t) => {
        const active = t === value;
        return (
          <button
            key={t}
            type="button"
            onClick={() => onChange(t)}
            className={cn(
              'rounded px-2 py-1 text-[10px] font-mono transition-colors',
              active
                ? 'bg-primary/20 text-primary'
                : 'text-text-muted hover:text-text-primary hover:bg-white/[0.04]',
            )}
          >
            {t}
          </button>
        );
      })}
    </div>
  );
}
