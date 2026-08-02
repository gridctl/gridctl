import { useNavigate } from 'react-router';
import { Lock, LockOpen } from 'lucide-react';
import { cn } from '../../lib/cn';
import { skillPinsUrl } from '../../lib/skillGovernance';
import {
  usePinsStore,
  useDriftedServers,
  useFirstDriftedServer,
  useDriftedSkillCount,
  useFirstDriftedSkill,
} from '../../stores/usePinsStore';

export function PinDriftBadge() {
  const pins = usePinsStore((s) => s.pins);
  const skillPins = usePinsStore((s) => s.skillPins);
  const driftedServers = useDriftedServers();
  const firstDrifted = useFirstDriftedServer();
  const skillDriftCount = useDriftedSkillCount();
  const firstDriftedSkill = useFirstDriftedSkill();
  const navigate = useNavigate();

  // Mirror PinFindingsBadge: skill state renders even while the server poll
  // has not landed (or transiently failed) — both maps ride the same poll,
  // but /api/pins alone failing must not hide skill drift.
  if (pins === null && skillDriftCount === 0) return null;
  const havePinnedServers = pins !== null && Object.keys(pins).length > 0;
  const havePinnedSkills = skillPins !== null && Object.keys(skillPins).length > 0;
  if (!havePinnedServers && !havePinnedSkills) return null;

  const serverDriftCount = driftedServers.length;
  const driftCount = serverDriftCount + skillDriftCount;
  const isDrifted = driftCount > 0;

  // Skill drift is called out only when present, so a server-only stack
  // reads exactly as before.
  const label = isDrifted
    ? skillDriftCount > 0
      ? `Pins: ${driftCount} drifted (${skillDriftCount} skill${skillDriftCount > 1 ? 's' : ''})`
      : `Pins: ${driftCount} drifted`
    : 'Pins: OK';

  const colorClass = isDrifted ? 'text-status-pending' : 'text-status-running';
  const dotClass = isDrifted
    ? 'bg-status-pending shadow-[0_0_6px_var(--color-status-pending-glow)]'
    : 'bg-status-running shadow-[0_0_6px_var(--color-status-running-glow)]';

  // Land directly on a drifted item's diff section; server drift wins the
  // deep link when both kinds drift (its rail is the default landing).
  // PinsWorkspace validates the params and falls back to the first rail
  // entry when stale; ?view=drift scrolls the drift panel into view.
  const handleClick = () => {
    if (firstDrifted) {
      navigate(`/pins?server=${encodeURIComponent(firstDrifted)}&view=drift`);
    } else if (firstDriftedSkill) {
      navigate(skillPinsUrl(firstDriftedSkill, 'drift'));
    } else {
      navigate('/pins');
    }
  };

  return (
    <button
      onClick={handleClick}
      className={cn(
        'flex items-center gap-2 transition-colors hover:opacity-80',
        colorClass
      )}
    >
      {isDrifted ? <LockOpen size={11} /> : <Lock size={11} />}
      <span className="relative flex items-center gap-1.5">
        <span className={cn('w-1.5 h-1.5 rounded-full', dotClass)} />
        <span className="font-medium">{label}</span>
      </span>
    </button>
  );
}
