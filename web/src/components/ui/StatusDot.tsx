import { cn } from '../../lib/cn';
import type { NodeStatus } from '../../types';

interface StatusDotProps {
  status: NodeStatus;
  size?: 'sm' | 'md';
}

const dotStyles: Record<NodeStatus, string> = {
  running: 'bg-status-running shadow-[0_0_8px_rgba(16,185,129,0.4)]',
  stopped: 'bg-status-stopped',
  error: 'bg-status-error shadow-[0_0_8px_rgba(244,63,94,0.4)]',
  initializing: 'bg-status-pending shadow-[0_0_8px_rgba(234,179,8,0.4)]',
};

export function StatusDot({ status, size = 'sm' }: StatusDotProps) {
  const sizeClasses = {
    sm: 'w-2 h-2',
    md: 'w-2.5 h-2.5',
  };

  return (
    <span className="relative inline-flex">
      <span
        className={cn(
          'rounded-full',
          sizeClasses[size],
          dotStyles[status]
        )}
      />
      {/* Pulse ring for running status */}
      {status === 'running' && (
        <span
          className={cn(
            'absolute inset-0 rounded-full bg-status-running animate-ping opacity-40',
          )}
          style={{ animationDuration: '2s' }}
        />
      )}
      {/* Blink for error status */}
      {status === 'error' && (
        <span
          className={cn(
            'absolute inset-0 rounded-full bg-status-error animate-pulse opacity-60',
          )}
          style={{ animationDuration: '1s' }}
        />
      )}
    </span>
  );
}
