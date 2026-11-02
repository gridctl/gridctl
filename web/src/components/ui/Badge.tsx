import type { ReactNode } from 'react';
import { cn } from '../../lib/cn';
import type { NodeStatus } from '../../types';

export type BadgeTone = 'neutral' | 'primary' | 'success' | 'warning';

type BadgeProps = {
  children: ReactNode;
  className?: string;
} & (
  | { status: NodeStatus; tone?: never }
  | { tone: BadgeTone; status?: never }
);

const statusStyles: Record<NodeStatus, string> = {
  running: 'bg-status-running/10 text-status-running border-status-running/20 shadow-glow-success',
  stopped: 'bg-status-stopped/15 text-text-muted border-status-stopped/30',
  error: 'bg-status-error/10 text-status-error border-status-error/20 shadow-glow-error',
  initializing: 'bg-status-pending/10 text-status-pending border-status-pending/20',
  idle: 'bg-status-idle/10 text-status-idle border-status-idle/20',
  'needs-auth': 'bg-status-pending/10 text-status-pending border-status-pending/25',
  restarting: 'bg-status-pending/10 text-status-pending border-status-pending/20',
};

const toneStyles: Record<BadgeTone, string> = {
  neutral: 'bg-surface-elevated text-text-muted',
  primary: 'bg-primary/15 text-primary',
  success: 'bg-status-running/15 text-status-running',
  warning: 'bg-status-pending/15 text-status-pending',
};

export function Badge(props: BadgeProps) {
  if (props.tone) {
    return (
      <span
        className={cn(
          'inline-flex items-center text-3xs uppercase tracking-wide px-1.5 py-0.5 rounded-full',
          toneStyles[props.tone],
          props.className,
        )}
      >
        {props.children}
      </span>
    );
  }

  const { status, children, className } = props;
  return (
    <div
      className={cn(
        'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg border',
        'text-3xs uppercase font-semibold tracking-widest',
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
        status === 'initializing' && 'bg-status-pending animate-pulse',
        status === 'idle' && 'bg-status-idle',
        status === 'needs-auth' && 'bg-status-pending',
        status === 'restarting' && 'bg-status-pending animate-pulse'
      )} style={{ animationDuration: '2s' }} />
      {children}
    </div>
  );
}
