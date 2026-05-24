import { Minus, Plus } from 'lucide-react';
import { cn } from '../../lib/cn';

// NumberValueInput is a numeric field for `number` variables. It deliberately
// uses type="text" + inputMode="numeric" rather than type="number": the native
// spinner mutates values on scroll, silently drops non-"e" characters, and is
// poorly exposed to assistive tech. The +/- steppers are explicit, labelled
// buttons kept out of the tab order so keyboard users tab straight to the
// field. Validation is owned by the parent (VariableValueInput).
export function NumberValueInput({
  value,
  onChange,
  onBlur,
  onSubmit,
  onCancel,
  invalid,
  describedBy,
  compact,
  enableZoom,
  autoFocus,
  placeholder = '42',
}: {
  value: string;
  onChange: (next: string) => void;
  onBlur?: () => void;
  onSubmit?: () => void;
  onCancel?: () => void;
  invalid?: boolean;
  describedBy?: string;
  compact?: boolean;
  enableZoom?: boolean;
  autoFocus?: boolean;
  placeholder?: string;
}) {
  const step = (delta: number) => {
    const n = Number(value);
    const base = Number.isFinite(n) && value.trim() !== '' ? n : 0;
    // Trim floating-point noise (e.g. 0.1 + 1) without forcing integers.
    onChange(String(Number((base + delta).toFixed(10))));
  };

  return (
    <div className="relative">
      <input
        type="text"
        inputMode="numeric"
        value={value}
        autoFocus={autoFocus}
        aria-invalid={invalid || undefined}
        aria-describedby={describedBy}
        placeholder={placeholder}
        onChange={(e) => onChange(e.target.value)}
        onBlur={onBlur}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault();
            onSubmit?.();
          }
          if (e.key === 'Escape') onCancel?.();
        }}
        className={cn(
          'w-full bg-surface border rounded-lg px-3 py-2 pr-14 text-xs font-mono text-text-primary',
          'placeholder:text-text-muted focus:ring-1 focus:ring-primary/30 outline-none transition-colors',
          invalid ? 'border-status-error/50' : 'border-border focus:border-primary/50',
          compact && 'py-1.5',
          enableZoom && 'log-text',
        )}
      />
      <div className="absolute right-1.5 top-1/2 -translate-y-1/2 flex items-center gap-0.5">
        <button
          type="button"
          tabIndex={-1}
          aria-label="Decrement"
          onClick={() => step(-1)}
          className="p-1 rounded text-text-muted hover:text-text-primary hover:bg-surface-highlight transition-colors"
        >
          <Minus size={12} />
        </button>
        <button
          type="button"
          tabIndex={-1}
          aria-label="Increment"
          onClick={() => step(1)}
          className="p-1 rounded text-text-muted hover:text-text-primary hover:bg-surface-highlight transition-colors"
        >
          <Plus size={12} />
        </button>
      </div>
    </div>
  );
}
