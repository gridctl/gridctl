import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';
import type { NodeStatus } from '../../types';

interface BadgeProps {
  status: NodeStatus;
  children: ReactNode;
  className?: string;
}

const statusStyles: Record<NodeStatus, string> = {
  running: 'bg-status-running/10 text-status-running border-status-running/20 shadow-glow-success',
  stopped: 'bg-status-stopped/15 text-text-muted border-status-stopped/30',
  error: 'bg-status-error/10 text-status-error border-status-error/20 shadow-glow-error',
  initializing: 'bg-status-pending/10 text-status-pending border-status-pending/20',
};

export function Badge({ status, children, className }: BadgeProps) {
  return (
    <div
      className={cn(
        'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg border',
        'text-[10px] uppercase font-semibold tracking-widest',
        'transition-all duration-200',
        statusStyles[status],
        className
      )}
    >
      {/* Status indicator dot */}
      <span className={cn(
        'w-1.5 h-1.5 rounded-full',
        status === 'running' && 'bg-status-running animate-pulse',
        status === 'stopped' && 'bg-status-stopped',
        status === 'error' && 'bg-status-error animate-pulse',
        status === 'initializing' && 'bg-status-pending animate-pulse'
      )} style={{ animationDuration: '2s' }} />
      {children}
    </div>
  );
}
