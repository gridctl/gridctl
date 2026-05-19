import { Lock, Eye } from 'lucide-react';
import { cn } from '../../lib/cn';

// VariableVisibilityIcon renders a tiny lock-or-eye icon distinguishing a
// secret variable from a plaintext one. Lives next to the variable key so
// the user can tell at a glance which rows are redacted in logs.
export function VariableVisibilityIcon({
  isSecret,
  size = 11,
  className,
}: {
  isSecret: boolean;
  size?: number;
  className?: string;
}) {
  if (isSecret) {
    return (
      <Lock
        size={size}
        className={cn('text-amber-400/80', className)}
        aria-label="secret"
      />
    );
  }
  return (
    <Eye
      size={size}
      className={cn('text-text-muted', className)}
      aria-label="plaintext"
    />
  );
}
