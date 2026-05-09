import { cn } from '../../../lib/cn';
import type { NodeTrace, NodeStatus } from './useRunTrace';

interface TracePillProps {
  trace: NodeTrace | undefined;
  compact?: boolean;
}

/**
 * TracePill is the per-node trace decoration shared between the
 * Slice 1 NodeList and the Slice 3 Canvas. Same component, two
 * surfaces — the Canvas passes compact=true so the pill fits inside
 * a node card.
 */
export function TracePill({ trace, compact }: TracePillProps) {
  if (!trace) {
    return compact ? null : <span className="w-20" aria-hidden />;
  }
  const status = trace.status;
  return (
    <span
      className={cn(
        'inline-flex items-center gap-2 font-mono whitespace-nowrap',
        compact ? 'text-[10px]' : 'text-xs',
      )}
    >
      <StatusDot status={status} />
      <span className={cn(statusToText(status), 'tabular-nums')}>
        {statusLabel(status)}
      </span>
      {trace.durationMicros != null && (
        <span className="text-text-muted/70 tabular-nums">
          {formatDuration(trace.durationMicros)}
        </span>
      )}
      {trace.costUSD != null && trace.costUSD > 0 && !compact && (
        <span className="text-text-muted/70 tabular-nums">
          ${trace.costUSD.toFixed(4)}
        </span>
      )}
    </span>
  );
}

function StatusDot({ status }: { status: NodeStatus }) {
  const base = 'inline-block w-2 h-2 rounded-full';
  switch (status) {
    case 'running':
      return (
        <span
          className={cn(base, 'bg-status-running')}
          style={{ boxShadow: '0 0 8px var(--color-status-running)' }}
        />
      );
    case 'ok':
      return <span className={cn(base, 'bg-status-running/70')} />;
    case 'error':
      return (
        <span
          className={cn(base, 'bg-status-error')}
          style={{ boxShadow: '0 0 8px var(--color-status-error)' }}
        />
      );
    case 'suspended':
      return (
        <span
          className={cn(base, 'bg-status-pending')}
          style={{ boxShadow: '0 0 8px var(--color-status-pending)' }}
        />
      );
    default:
      return <span className={cn(base, 'bg-text-muted/40')} />;
  }
}

function statusToText(status: NodeStatus): string {
  switch (status) {
    case 'running':
      return 'text-status-running';
    case 'ok':
      return 'text-status-running/80';
    case 'error':
      return 'text-status-error';
    case 'suspended':
      return 'text-status-pending';
    default:
      return 'text-text-muted';
  }
}

function statusLabel(status: NodeStatus): string {
  switch (status) {
    case 'queued':
      return 'queued';
    case 'running':
      return 'running…';
    case 'ok':
      return 'ok';
    case 'error':
      return 'error';
    case 'suspended':
      return 'awaiting approval';
    default:
      return status;
  }
}

function formatDuration(micros: number): string {
  if (micros < 1000) return `${micros}µs`;
  if (micros < 1_000_000) return `${(micros / 1000).toFixed(1)}ms`;
  return `${(micros / 1_000_000).toFixed(2)}s`;
}
