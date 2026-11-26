import { cn } from '../../lib/cn';
import { CopyButton } from '../ui/CopyButton';

interface InspectorValueRowProps {
  label: string;
  value: string;
  // Accessible name for an optional copy button, naming the field.
  copyLabel?: string;
  labelClassName?: string;
}

/**
 * Label and identifier row for inspector rails. The value wraps at any
 * character instead of truncating behind a hover title, so the full
 * identifier stays readable and focusable at the narrowest rail width.
 */
export function InspectorValueRow({
  label,
  value,
  copyLabel,
  labelClassName = 'text-sm',
}: InspectorValueRowProps) {
  return (
    <div className="flex justify-between items-start gap-4 min-w-0">
      <span className={cn(labelClassName, 'text-text-muted shrink-0')}>{label}</span>
      <div className="flex items-start justify-end gap-1 min-w-0">
        <span
          tabIndex={0}
          className="min-w-0 break-all text-xs text-text-secondary font-mono bg-background/50 px-2 py-1 rounded-md"
        >
          {value}
        </span>
        {copyLabel && <CopyButton value={value} label={copyLabel} />}
      </div>
    </div>
  );
}
