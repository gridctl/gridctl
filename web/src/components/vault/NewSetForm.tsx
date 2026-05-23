import { Check, XCircle } from 'lucide-react';
import { cn } from '../../lib/cn';

export interface NewSetFormProps {
  show: boolean;
  value: string;
  onChange: (val: string) => void;
  onSave: () => void;
  onCancel: () => void;
  className?: string;
  // Apply `.log-text` so the input scales with the parent's zoom controls
  // (detached page); VaultPanel leaves this off.
  enableZoom?: boolean;
}

// Inline form to create a new Variable Set. Renders nothing when `show` is
// false so callers can place it inline with their list/grid without taking
// vertical space.
export function NewSetForm({
  show,
  value,
  onChange,
  onSave,
  onCancel,
  className,
  enableZoom,
}: NewSetFormProps) {
  if (!show) return null;
  return (
    <div className={cn('flex gap-2', className)}>
      <input
        type="text"
        value={value}
        onChange={(e) =>
          onChange(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))
        }
        placeholder="set-name"
        autoFocus
        className={cn(
          'flex-1 bg-surface border border-border rounded-lg px-2 py-1 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 outline-none transition-colors',
          enableZoom && 'log-text',
        )}
        onKeyDown={(e) => {
          if (e.key === 'Enter') onSave();
          if (e.key === 'Escape') onCancel();
        }}
      />
      <button
        onClick={onSave}
        className="p-1 rounded hover:bg-surface-highlight transition-colors"
        disabled={!value.trim()}
      >
        <Check size={12} className="text-status-running" />
      </button>
      <button
        onClick={onCancel}
        className="p-1 rounded hover:bg-surface-highlight transition-colors"
      >
        <XCircle size={12} className="text-text-muted" />
      </button>
    </div>
  );
}
