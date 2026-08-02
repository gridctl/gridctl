import { LockOpen, ShieldAlert, ShieldOff } from 'lucide-react';
import { cn } from '../../lib/cn';
import type { SkillGovernance } from '../../types';
import { governanceNeedsAttention, governanceSummary } from '../../lib/skillGovernance';

// SkillGovernanceBadge is the Library's single compact governance indicator:
// one icon slot, shown only when attention is needed (pin drift, warn-or-
// critical advisory findings, or a policy denial), never stacked with other
// chips. Deliberately non-interactive — Library rows and cards are already
// clickable, and a nested button would break their semantics; the detail
// panel carries the interactive deep link into the Pins workspace.
export function SkillGovernanceBadge({
  governance,
  className,
}: {
  governance?: SkillGovernance;
  className?: string;
}) {
  if (!governance || !governanceNeedsAttention(governance)) return null;
  const g = governance;
  const summary = governanceSummary(g);

  // One icon, worst signal first: a policy denial changes what clients see
  // right now, pin drift needs a human decision, findings are advisory.
  const Icon = g.policyDenied ? ShieldOff : g.pinStatus === 'drift' ? LockOpen : ShieldAlert;
  const tone =
    g.policyDenied
      ? 'text-status-error'
      : g.pinStatus === 'drift'
        ? 'text-status-pending'
        : g.maxFindingSeverity === 'critical'
          ? 'text-status-error'
          : 'text-status-pending';

  return (
    <span
      className={cn('flex-shrink-0 inline-flex', tone, className)}
      title={summary}
      aria-label={summary}
      role="img"
    >
      <Icon size={11} />
    </span>
  );
}
