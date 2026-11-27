import { cn } from '../../lib/cn';

const SUCCESS = 'bg-status-running/10 text-status-running border-status-running/20';
const ERROR = 'bg-status-error/10 text-status-error border-status-error/20';
const REFUSED = 'bg-status-pending/10 text-status-pending border-status-pending/20';
const NEUTRAL = 'bg-surface-elevated text-text-muted border-border/40';

const TONES: Record<string, string> = {
  completed: SUCCESS,
  tool_error: ERROR,
  routing_failed: ERROR,
  transport_error: ERROR,
  timeout: ERROR,
  denied: REFUSED,
  retry_rejected: REFUSED,
};

/** Tone classes for a run disposition; unknown values read as neutral. */
// eslint-disable-next-line react-refresh/only-export-components -- pure lookup kept beside the badge it styles
export function dispositionTone(value: string): string {
  return Object.prototype.hasOwnProperty.call(TONES, value) ? TONES[value] : NEUTRAL;
}

// The disposition is shown verbatim: the same strings are filter values, CLI
// output, and documented vocabulary. The tone carries the meaning.
export function DispositionBadge({ value, className }: { value: string; className?: string }) {
  return (
    <span
      className={cn(
        'px-1.5 py-0.5 text-3xs font-medium rounded-full border whitespace-nowrap',
        dispositionTone(value),
        className,
      )}
    >
      {value}
    </span>
  );
}
