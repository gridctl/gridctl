import { useMemo } from 'react';
import { Eye, EyeOff } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useSpecStore } from '../../stores/useSpecStore';

interface SpecModeOverlayProps {
  className?: string;
}

/**
 * Canvas overlay for spec mode — shows ghost nodes for items declared
 * in the spec but not deployed, and warning badges for running items
 * not in the spec.
 */
export function SpecModeOverlay({ className }: SpecModeOverlayProps) {
  const health = useSpecStore((s) => s.health);
  const drift = health?.drift;

  const { ghostItems, warningItems } = useMemo(() => {
    const added = drift?.added ?? [];
    const removed = drift?.removed ?? [];

    return {
      ghostItems: added,
      warningItems: removed,
    };
  }, [drift]);

  if (!drift || drift.status === 'in-sync') {
    return (
      <div className={cn('pointer-events-none', className)}>
        <div className="pointer-events-auto absolute top-3 left-1/2 -translate-x-1/2 z-20">
          <div className="glass-panel rounded-lg px-3 py-1.5 flex items-center gap-2 border border-secondary/30 bg-secondary/5">
            <Eye size={12} className="text-secondary" />
            <span className="text-[10px] font-medium text-secondary">Spec Mode — all in sync</span>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={cn('pointer-events-none', className)}>
      {/* Spec mode banner */}
      <div className="pointer-events-auto absolute top-3 left-1/2 -translate-x-1/2 z-20">
        <div className="glass-panel rounded-lg px-3 py-1.5 flex items-center gap-2 border border-secondary/30 bg-secondary/5">
          <Eye size={12} className="text-secondary" />
          <span className="text-[10px] font-medium text-secondary">
            Spec Mode
            {ghostItems.length > 0 && ` — ${ghostItems.length} not deployed`}
            {warningItems.length > 0 && ` — ${warningItems.length} not in spec`}
          </span>
        </div>
      </div>

      {/* Ghost nodes — declared in spec but not running */}
      {ghostItems.length > 0 && (
        <div className="pointer-events-auto absolute bottom-14 left-3 z-20 space-y-1">
          {ghostItems.map((name) => (
            <div
              key={name}
              className="glass-panel rounded-lg px-2.5 py-1.5 flex items-center gap-2 border border-dashed border-secondary/30 opacity-50"
            >
              <EyeOff size={10} className="text-secondary/60" />
              <span className="text-[10px] text-text-muted font-mono">{name}</span>
              <span className="text-[8px] text-secondary/60 uppercase tracking-wider">Declared</span>
            </div>
          ))}
        </div>
      )}

      {/* Warning badges — running but not in spec */}
      {warningItems.length > 0 && (
        <div className="pointer-events-auto absolute bottom-14 right-3 z-20 space-y-1">
          {warningItems.map((name) => (
            <div
              key={name}
              className="glass-panel rounded-lg px-2.5 py-1.5 flex items-center gap-2 border border-status-pending/30"
              title={`"${name}" is running but not in the spec`}
            >
              <EyeOff size={10} className="text-status-pending" />
              <span className="text-[10px] text-text-muted font-mono">{name}</span>
              <span className="text-[8px] text-status-pending uppercase tracking-wider">Untracked</span>
            </div>
          ))}
        </div>
      )}

    </div>
  );
}
