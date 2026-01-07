import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';
import type { NodeStatus } from '../../types';

interface BadgeProps {
  status: NodeStatus;
  children: ReactNode;
  className?: string;
}

const statusStyles: Record<NodeStatus, string> = {
  running: 'bg-status-running/15 text-status-running border-status-running/40',
  stopped: 'bg-status-stopped/30 text-text-muted border-status-stopped/50',
  error: 'bg-status-error/15 text-status-error border-status-error/40',
  initializing: 'bg-status-pending/15 text-status-pending border-status-pending/40',
};

// Neon text glow for active statuses
const glowStyles: Record<NodeStatus, string> = {
  running: 'drop-shadow-[0_0_4px_#2CFF05]',
  stopped: '',
  error: 'drop-shadow-[0_0_4px_#FF3366]',
  initializing: 'drop-shadow-[0_0_4px_#FFCC00]',
};

export function Badge({ status, children, className }: BadgeProps) {
  return (
    <div
      className={cn(
        'inline-flex items-center gap-1.5 px-2 py-0.5 rounded border',
        'text-[10px] uppercase font-bold tracking-wider',
        statusStyles[status],
        glowStyles[status],
        className
      )}
    >
      {children}
    </div>
  );
}
