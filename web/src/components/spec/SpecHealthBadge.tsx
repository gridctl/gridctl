import { useEffect, useRef } from 'react';
import { FileCheck } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useSpecStore } from '../../stores/useSpecStore';
import { useUIStore } from '../../stores/useUIStore';
import { fetchStackHealth } from '../../lib/api';

export function SpecHealthBadge() {
  const health = useSpecStore((s) => s.health);
  const setHealth = useSpecStore((s) => s.setHealth);
  const pollRef = useRef<ReturnType<typeof setInterval>>(null);

  useEffect(() => {
    const load = () => {
      fetchStackHealth().then(setHealth).catch(() => {});
    };
    load();
    pollRef.current = setInterval(load, 10000);
    return () => {
      if (pollRef.current) clearInterval(pollRef.current);
    };
  }, [setHealth]);

  const handleClick = () => {
    useUIStore.getState().setBottomPanelTab('spec');
  };

  if (!health) return null;

  const status = health.validation.status;
  const errorCount = health.validation.errorCount;
  const warningCount = health.validation.warningCount;

  let label: string;
  let colorClass: string;
  let dotClass: string;

  switch (status) {
    case 'valid':
      label = 'Spec: Valid';
      colorClass = 'text-status-running';
      dotClass = 'bg-status-running shadow-[0_0_6px_var(--color-status-running-glow)]';
      break;
    case 'warnings':
      label = `Spec: ${warningCount} warning${warningCount !== 1 ? 's' : ''}`;
      colorClass = 'text-status-pending';
      dotClass = 'bg-status-pending shadow-[0_0_6px_var(--color-status-pending-glow)]';
      break;
    case 'errors':
      label = `Spec: ${errorCount} error${errorCount !== 1 ? 's' : ''}`;
      colorClass = 'text-status-error';
      dotClass = 'bg-status-error shadow-[0_0_6px_var(--color-status-error-glow)]';
      break;
    default:
      label = 'Spec: Unknown';
      colorClass = 'text-text-muted';
      dotClass = 'bg-text-muted';
  }

  return (
    <button
      onClick={handleClick}
      className={cn(
        'flex items-center gap-2 transition-colors hover:opacity-80',
        colorClass
      )}
    >
      <FileCheck size={11} />
      <span className="relative flex items-center gap-1.5">
        <span className={cn('w-1.5 h-1.5 rounded-full', dotClass)} />
        <span className="font-medium">{label}</span>
      </span>
    </button>
  );
}
