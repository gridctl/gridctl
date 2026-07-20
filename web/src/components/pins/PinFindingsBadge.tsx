import { useNavigate } from 'react-router-dom';
import { ShieldAlert } from 'lucide-react';
import { cn } from '../../lib/cn';
import { usePinsStore, useFindingServerCount } from '../../stores/usePinsStore';

// PinFindingsBadge is the status-bar chip for poisoning-scan findings,
// sibling to PinDriftBadge and AuthPendingBadge. It renders only when at
// least one server has warn-or-critical findings; a quiet stack shows no
// chip at all (the drift badge already covers "Pins: OK").
export function PinFindingsBadge() {
  const pins = usePinsStore((s) => s.pins);
  const findingCount = useFindingServerCount();
  const navigate = useNavigate();

  if (pins === null || findingCount === 0) return null;

  const label = `Findings: ${findingCount} server${findingCount > 1 ? 's' : ''}`;

  return (
    <button
      onClick={() => navigate('/pins')}
      className={cn('flex items-center gap-2 transition-colors hover:opacity-80 text-status-pending')}
      title="Poisoning-scan findings on pinned tools; review in the Pins workspace"
    >
      <ShieldAlert size={11} />
      <span className="relative flex items-center gap-1.5">
        <span className="w-1.5 h-1.5 rounded-full bg-status-pending shadow-[0_0_6px_var(--color-status-pending-glow)]" />
        <span className="font-medium">{label}</span>
      </span>
    </button>
  );
}
