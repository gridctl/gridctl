import { CheckCircle, XCircle, Minus } from 'lucide-react';
import { cn } from '../../lib/cn';
import type { SkillTestResult } from '../../types';

type Density = 'card' | 'compact';

interface TestStatusBadgeProps {
  testResult?: SkillTestResult | null;
  density?: Density;
  onClick?: () => void;
  className?: string;
}

/**
 * Shared test-status pill for SkillCard and SkillItem. When `onClick` is
 * supplied, renders as a button; otherwise renders as a passive span.
 * `density="compact"` drops counts for tight rows; `density="card"` shows
 * `N passing` / `N failing` for informational footers.
 */
export function TestStatusBadge({
  testResult,
  density = 'card',
  onClick,
  className,
}: TestStatusBadgeProps) {
  const untested = !testResult || testResult.status === 'untested';
  const failed = !untested && (testResult?.failed ?? 0) > 0;

  let icon, label, color, title;
  if (untested) {
    icon = <Minus size={11} />;
    label = 'untested';
    color = 'text-amber-400/80 bg-amber-400/8 border-amber-400/20';
    title = 'No test run yet';
  } else if (failed) {
    icon = <XCircle size={11} />;
    label =
      density === 'compact' ? 'failing' : `${testResult?.failed ?? 0} failing`;
    color = 'text-rose-400 bg-rose-400/8 border-rose-400/20';
    title = `${testResult?.failed ?? 0} failed`;
  } else {
    icon = <CheckCircle size={11} />;
    label =
      density === 'compact' ? 'passing' : `${testResult?.passed ?? 0} passing`;
    color = 'text-emerald-400 bg-emerald-400/8 border-emerald-400/20';
    title = `${testResult?.passed ?? 0} passed`;
  }

  const baseClasses = cn(
    'inline-flex items-center gap-1 text-[10px] font-medium px-1.5 py-0.5 rounded border',
    color,
    className,
  );

  if (onClick) {
    return (
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onClick();
        }}
        title={title}
        className={cn(baseClasses, 'hover:brightness-110 transition-all')}
      >
        {icon}
        <span>{label}</span>
      </button>
    );
  }

  return (
    <span title={title} className={baseClasses}>
      {icon}
      <span>{label}</span>
    </span>
  );
}
