import { useNavigate } from 'react-router';
import { ShieldAlert } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useLimits } from '../../hooks/useLimits';
import { deriveLimitsSummary } from './limitsData';

// LimitsBadge is the status-bar chip for budget and rate limit pressure.
// Hidden entirely while every limit is ok (and always when no limits: block
// is configured) — the bar only speaks up when the operator should look.
// Follows the PinDriftBadge conventions: severity color + glow dot, click
// opens the owning workspace.
export function LimitsBadge() {
  const navigate = useNavigate();
  const { report } = useLimits(true);
  const summary = deriveLimitsSummary(report);

  if (!summary.configured) return null;
  if (summary.worst === 'ok') return null;

  const exceeded = summary.worst === 'exceeded';
  const count = exceeded ? summary.exceededCount : summary.warnCount;
  const label = exceeded
    ? `${count} budget${count === 1 ? '' : 's'} exceeded`
    : `${count} limit${count === 1 ? '' : 's'} near cap`;

  const colorClass = exceeded ? 'text-status-error' : 'text-status-pending';
  const dotClass = exceeded
    ? 'bg-status-error shadow-[0_0_6px_var(--color-status-error-glow)]'
    : 'bg-status-pending shadow-[0_0_6px_var(--color-status-pending-glow)]';

  return (
    <button
      onClick={() => navigate('/metrics')}
      aria-label={`${label}. Open Metrics workspace`}
      className={cn('flex items-center gap-2 transition-colors hover:opacity-80', colorClass)}
    >
      <ShieldAlert size={11} />
      <span className="relative flex items-center gap-1.5">
        <span className={cn('w-1.5 h-1.5 rounded-full', dotClass)} />
        <span className="font-medium">{label}</span>
      </span>
    </button>
  );
}
