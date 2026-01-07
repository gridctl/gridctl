import { cn } from '../../lib/cn';
import type { NodeStatus } from '../../types';

interface StatusDotProps {
  status: NodeStatus;
}

const dotColors: Record<NodeStatus, string> = {
  running: 'bg-status-running',
  stopped: 'bg-status-stopped',
  error: 'bg-status-error',
  initializing: 'bg-status-pending',
};

// Glow pulse animation class for each status
const glowClasses: Record<NodeStatus, string> = {
  running: 'status-glow-pulse',
  stopped: '',
  error: 'status-glow-error',
  initializing: 'status-glow-pending',
};

export function StatusDot({ status }: StatusDotProps) {
  return (
    <span
      className={cn(
        'rounded-full w-2 h-2', // Fixed 8px size
        dotColors[status],
        glowClasses[status]
      )}
    />
  );
}
