import { Lock, LockOpen } from 'lucide-react';
import { cn } from '../../lib/cn';
import { usePinsStore, useDriftedServers } from '../../stores/usePinsStore';
import { useUIStore } from '../../stores/useUIStore';

export function PinDriftBadge() {
  const pins = usePinsStore((s) => s.pins);
  const driftedServers = useDriftedServers();

  if (pins === null) return null;
  if (Object.keys(pins).length === 0) return null;

  const driftCount = driftedServers.length;
  const isDrifted = driftCount > 0;

  const label = isDrifted
    ? `Pins: ${driftCount} drifted`
    : 'Pins: OK';

  const colorClass = isDrifted ? 'text-status-pending' : 'text-status-running';
  const dotClass = isDrifted
    ? 'bg-status-pending shadow-[0_0_6px_var(--color-status-pending-glow)]'
    : 'bg-status-running shadow-[0_0_6px_var(--color-status-running-glow)]';

  const handleClick = () => {
    useUIStore.getState().setBottomPanelTab('pins');
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
