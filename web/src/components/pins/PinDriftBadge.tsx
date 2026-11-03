import { useNavigate } from 'react-router';
import { Lock, LockOpen } from 'lucide-react';
import { cn } from '../../lib/cn';
import { skillPinsUrl } from '../../lib/skillGovernance';
import { contextPinsUrl } from '../../lib/contextGovernance';
import {
  usePinsStore,
  useDriftedServers,
  useFirstDriftedServer,
  useDriftedSkillCount,
  useFirstDriftedSkill,
  useDriftedContextCount,
  useFirstDriftedContext,
} from '../../stores/usePinsStore';

export function PinDriftBadge() {
  const pins = usePinsStore((s) => s.pins);
  const skillPins = usePinsStore((s) => s.skillPins);
  const driftedServers = useDriftedServers();
  const firstDrifted = useFirstDriftedServer();
  const skillDriftCount = useDriftedSkillCount();
  const firstDriftedSkill = useFirstDriftedSkill();
  const contextPins = usePinsStore((s) => s.contextPins);
  const contextDriftCount = useDriftedContextCount();
  const firstDriftedContext = useFirstDriftedContext();
  const navigate = useNavigate();

  // Mirror PinFindingsBadge: skill state renders even while the server poll
  // has not landed (or transiently failed) — both maps ride the same poll,
  // but /api/pins alone failing must not hide skill drift.
  if (pins === null && skillDriftCount === 0 && contextDriftCount === 0) return null;
  const havePinnedServers = pins !== null && Object.keys(pins).length > 0;
  const havePinnedSkills = skillPins !== null && Object.keys(skillPins).length > 0;
  const havePinnedContexts = contextPins !== null && Object.keys(contextPins).length > 0;
  if (!havePinnedServers && !havePinnedSkills && !havePinnedContexts) return null;

  const serverDriftCount = driftedServers.length;
  const driftCount = serverDriftCount + skillDriftCount + contextDriftCount;
  const isDrifted = driftCount > 0;

  // Skill and fragment drift are called out only when present, so a
  // server-only stack reads exactly as before.
  const callouts: string[] = [];
  if (skillDriftCount > 0) callouts.push(`${skillDriftCount} skill${skillDriftCount > 1 ? 's' : ''}`);
  if (contextDriftCount > 0) callouts.push(`${contextDriftCount} fragment${contextDriftCount > 1 ? 's' : ''}`);
  const label = isDrifted
    ? callouts.length > 0
      ? `Pins: ${driftCount} drifted (${callouts.join(', ')})`
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
    } else if (firstDriftedContext) {
      navigate(contextPinsUrl(firstDriftedContext, 'drift'));
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
