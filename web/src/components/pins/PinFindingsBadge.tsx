import { useNavigate } from 'react-router';
import { ShieldAlert } from 'lucide-react';
import { cn } from '../../lib/cn';
import { skillPinsUrl } from '../../lib/skillGovernance';
import {
  usePinsStore,
  useFindingServerCount,
  useFirstFindingsServer,
  useFindingSkillCount,
  useFirstFindingsSkill,
} from '../../stores/usePinsStore';

// PinFindingsBadge is the status-bar chip for poisoning-scan findings,
// sibling to PinDriftBadge and AuthPendingBadge. It renders only when at
// least one server or skill has warn-or-critical findings; a quiet stack
// shows no chip at all (the drift badge already covers "Pins: OK").
export function PinFindingsBadge() {
  const pins = usePinsStore((s) => s.pins);
  const findingCount = useFindingServerCount();
  const firstFindings = useFirstFindingsServer();
  const skillFindingCount = useFindingSkillCount();
  const firstFindingsSkill = useFirstFindingsSkill();
  const navigate = useNavigate();

  if (pins === null && skillFindingCount === 0) return null;
  if (findingCount === 0 && skillFindingCount === 0) return null;

  // The server-only label is unchanged; skills join the tally only when
  // they actually carry findings.
  const parts: string[] = [];
  if (findingCount > 0) parts.push(`${findingCount} server${findingCount > 1 ? 's' : ''}`);
  if (skillFindingCount > 0)
    parts.push(`${skillFindingCount} skill${skillFindingCount > 1 ? 's' : ''}`);
  const label = `Findings: ${parts.join(' · ')}`;

  return (
    <button
      onClick={() =>
        navigate(
          firstFindings
            ? `/pins?server=${encodeURIComponent(firstFindings)}&view=findings`
            : firstFindingsSkill
              ? skillPinsUrl(firstFindingsSkill, 'findings')
              : '/pins',
        )
      }
      className={cn('flex items-center gap-2 transition-colors hover:opacity-80 text-status-pending')}
      title="Poisoning-scan findings on pinned tools or skills; review in the Pins workspace"
    >
      <ShieldAlert size={11} />
      <span className="relative flex items-center gap-1.5">
        <span className="w-1.5 h-1.5 rounded-full bg-status-pending shadow-[0_0_6px_var(--color-status-pending-glow)]" />
        <span className="font-medium">{label}</span>
      </span>
    </button>
  );
}
