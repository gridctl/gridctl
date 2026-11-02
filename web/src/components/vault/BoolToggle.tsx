import { cn } from '../../lib/cn';
import { Switch } from '../ui/Switch';

// BoolToggle is an accessible on/off switch for `bool` variables. The visible
// state text reflects the value (true/false); the control's accessible label
// is stable ("Value") so assistive tech doesn't hear the label change on flip.
export function BoolToggle({
  checked,
  onChange,
  compact,
  className,
}: {
  checked: boolean;
  onChange: (next: boolean) => void;
  compact?: boolean;
  className?: string;
}) {
  return (
    <div className={cn('flex items-center gap-2', className)}>
      <Switch
        checked={checked}
        onChange={onChange}
        label="Value"
        size={compact ? 'sm' : 'md'}
      />
      <span className="text-xs font-mono text-text-secondary select-none">
        {checked ? 'true' : 'false'}
      </span>
    </div>
  );
}
