import { cn } from '../../lib/cn';
import type { NodeStatus } from '../../types';

interface StatusDotProps {
  status: NodeStatus;
  size?: 'sm' | 'md';
  pulse?: boolean;
  /** Accessible name. Defaults to a human form of the status. */
  label?: string;
}

const STATUS_LABELS: Record<NodeStatus, string> = {
  running: 'running',
  stopped: 'stopped',
  error: 'error',
  initializing: 'initializing',
  idle: 'idle',
  'needs-auth': 'needs auth',
  restarting: 'restarting',
};

const dotStyles: Record<NodeStatus, string> = {
  running: 'bg-status-running shadow-[0_0_8px_--alpha(var(--color-status-running)/40%)]',
  stopped: 'bg-status-stopped',
  error: 'bg-status-error shadow-[0_0_8px_--alpha(var(--color-status-error)/40%)]',
  initializing: 'bg-status-pending shadow-[0_0_8px_--alpha(var(--color-status-pending)/40%)]',
  idle: 'bg-status-idle',
  'needs-auth': 'bg-status-pending shadow-[0_0_8px_var(--color-status-pending-glow)]',
  restarting: 'bg-status-pending shadow-[0_0_8px_--alpha(var(--color-status-pending)/40%)]',
};

export function StatusDot({ status, size = 'sm', pulse = true, label }: StatusDotProps) {
  const name = label ?? STATUS_LABELS[status];
  const sizeClasses = {
    sm: 'w-2 h-2',
    md: 'w-2.5 h-2.5',
  };

  return (
    <span role="img" aria-label={name} title={name} className="relative inline-flex">
      <span
        className={cn(
          'rounded-full',
          sizeClasses[size],
          dotStyles[status],
          // Reduced motion drops the ping/pulse overlays. Shape keeps running
          // (solid + 1px ring) distinct from error (hollow + 2px ring).
          status === 'running' && 'motion-reduce:ring-1 motion-reduce:ring-status-running',
          status === 'error' &&
            'motion-reduce:bg-transparent motion-reduce:shadow-none motion-reduce:ring-2 motion-reduce:ring-status-error',
        )}
      />
      {/* Pulse ring for running status. Hidden under reduced motion so the ring treatment reads. */}
      {pulse && status === 'running' && (
        <span
          className="absolute inset-0 rounded-full bg-status-running animate-ping opacity-40 motion-reduce:hidden"
          style={{ animationDuration: '2s' }}
        />
      )}
      {/* Blink for error status. Hidden under reduced motion so the hollow dot reads. */}
      {status === 'error' && (
        <span
          className="absolute inset-0 rounded-full bg-status-error animate-pulse opacity-60 motion-reduce:hidden"
          style={{ animationDuration: '1s' }}
        />
      )}
    </span>
  );
}
