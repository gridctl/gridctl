import { Lock, Eye } from 'lucide-react';
import { cn } from '../../lib/cn';

// VariableSecretToggle is a two-position segmented control for choosing
// whether a variable is stored as a Secret (default; redacted in logs) or
// Plaintext (visible in logs). Default is Secret per Article XII.
export function VariableSecretToggle({
  isSecret,
  onChange,
  className,
}: {
  isSecret: boolean;
  onChange: (next: boolean) => void;
  className?: string;
}) {
  return (
    <div
      role="group"
      aria-label="Variable visibility"
      className={cn(
        'inline-flex items-center rounded-md border border-border/40 bg-background/60 p-px',
        className,
      )}
    >
      <button
        type="button"
        onClick={() => onChange(true)}
        className={cn(
          'inline-flex items-center gap-1 rounded px-2 py-1 text-[10px] font-medium transition-colors',
          isSecret
            ? 'bg-amber-500/20 text-amber-300'
            : 'text-text-muted hover:text-text-primary hover:bg-white/[0.04]',
        )}
        title="Secret — redacted in logs"
      >
        <Lock size={10} /> Secret
      </button>
      <button
        type="button"
        onClick={() => onChange(false)}
        className={cn(
          'inline-flex items-center gap-1 rounded px-2 py-1 text-[10px] font-medium transition-colors',
          !isSecret
            ? 'bg-sky-500/20 text-sky-300'
            : 'text-text-muted hover:text-text-primary hover:bg-white/[0.04]',
        )}
        title="Plaintext — visible in logs"
      >
        <Eye size={10} /> Plaintext
      </button>
    </div>
  );
}
