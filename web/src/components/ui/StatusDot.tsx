import { cn } from '../../lib/cn';
import { NODE_STATUS_LABELS } from '../../lib/nodeStatus';
import type { NodeStatus } from '../../types';

interface StatusDotProps {
  status: NodeStatus;
  size?: 'sm' | 'md';
  pulse?: boolean;
  /** Accessible name. Defaults to a human form of the status. */
  label?: string;
}

/** One shape per status so the dot reads without color. */
// eslint-disable-next-line react-refresh/only-export-components -- the legend and tests read the same map the dot draws from
export const STATUS_SHAPES: Record<NodeStatus, string> = {
  running: 'circle',
  idle: 'circle-hollow',
  initializing: 'circle-half',
  stopped: 'square',
  error: 'diamond',
  restarting: 'diamond-hollow',
  'needs-auth': 'triangle',
  disabled: 'square-hollow',
};

const STATUS_COLORS: Record<NodeStatus, string> = {
  running: 'text-status-running',
  idle: 'text-status-idle',
  initializing: 'text-status-pending',
  stopped: 'text-status-stopped',
  error: 'text-status-error',
  restarting: 'text-status-pending',
  'needs-auth': 'text-status-pending',
  disabled: 'text-status-stopped',
};

// Hollow variants use a 1.5 stroke in the 8-unit box so they stay visible at 8 px.
const hollow = { fill: 'none', stroke: 'currentColor', strokeWidth: 1.5, strokeLinejoin: 'round' as const };

function Shape({ shape }: { shape: string }) {
  switch (shape) {
    case 'circle-hollow':
      return <circle cx="4" cy="4" r="3.25" {...hollow} />;
    case 'circle-half':
      return (
        <>
          <circle cx="4" cy="4" r="3.25" {...hollow} />
          <path d="M4 0 A4 4 0 0 0 4 8 Z" fill="currentColor" />
        </>
      );
    case 'square':
      return <rect x="0.5" y="0.5" width="7" height="7" rx="0.5" fill="currentColor" />;
    case 'square-hollow':
      return <rect x="1.25" y="1.25" width="5.5" height="5.5" {...hollow} />;
    case 'diamond':
      return <polygon points="4,0 8,4 4,8 0,4" fill="currentColor" />;
    case 'diamond-hollow':
      return <polygon points="4,1.1 6.9,4 4,6.9 1.1,4" {...hollow} />;
    case 'triangle':
      return <polygon points="4,0.5 8,7.5 0,7.5" fill="currentColor" />;
    default:
      return <circle cx="4" cy="4" r="4" fill="currentColor" />;
  }
}

export function StatusDot({ status, size = 'sm', pulse = true, label }: StatusDotProps) {
  const name = label ?? NODE_STATUS_LABELS[status];
  const shape = STATUS_SHAPES[status];
  const sizeClasses = {
    sm: 'w-2 h-2',
    md: 'w-2.5 h-2.5',
  };

  return (
    <span role="img" aria-label={name} title={name} data-shape={shape} className="relative inline-flex">
      <svg
        viewBox="0 0 8 8"
        aria-hidden="true"
        focusable="false"
        className={cn(
          'block overflow-visible',
          sizeClasses[size],
          STATUS_COLORS[status],
          pulse && status === 'error' && 'animate-pulse motion-reduce:animate-none',
        )}
      >
        <Shape shape={shape} />
      </svg>
      {pulse && status === 'running' && (
        <span
          className="absolute inset-0 rounded-full bg-status-running animate-ping opacity-40 motion-reduce:hidden"
          style={{ animationDuration: '2s' }}
        />
      )}
    </span>
  );
}
