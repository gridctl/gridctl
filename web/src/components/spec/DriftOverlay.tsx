import { useMemo } from 'react';
import { AlertTriangle, CloudOff } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useSpecStore } from '../../stores/useSpecStore';

interface DriftOverlayProps {
  className?: string;
}

/** Canvas overlay that shows spec-vs-running drift indicators. */
export function DriftOverlay({ className }: DriftOverlayProps) {
  const health = useSpecStore((s) => s.health);
  const drift = health?.drift;

  const { ghostItems, warningItems, summary } = useMemo(() => {
    if (!drift || drift.status === 'in-sync') {
      return { ghostItems: [] as string[], warningItems: [] as string[], summary: '' };
    }

    const added = drift.added ?? [];
    const removed = drift.removed ?? [];
    const changed = drift.changed ?? [];

    // "added" = in spec but not running (ghost nodes)
    // "removed" = running but not in spec (warning badges)
    const parts: string[] = [];
    if (added.length > 0) parts.push(`${added.length} not deployed`);
    if (removed.length > 0) parts.push(`${removed.length} not in spec`);
    if (changed.length > 0) parts.push(`${changed.length} changed`);

    return {
      ghostItems: added,
      warningItems: removed,
      summary: parts.join(', '),
    };
  }, [drift]);

  if (!drift || drift.status === 'in-sync') {
    return null;
  }

  return (
    <div className={cn('pointer-events-none', className)}>
      {/* Drift summary banner */}
      <div className="pointer-events-auto absolute top-3 left-1/2 -translate-x-1/2 z-20">
        <div className="glass-panel rounded-lg px-3 py-1.5 flex items-center gap-2 border border-primary/30 bg-primary/5">
          <AlertTriangle size={12} className="text-primary" />
          <span className="text-[10px] font-medium text-primary">{summary}</span>
          {drift.status === 'unknown' && (
            <span className="text-[9px] text-text-muted">(drift unknown)</span>
          )}
        </div>
      </div>

      {/* Ghost nodes — declared in spec but not running */}
      {ghostItems.length > 0 && (
        <div className="pointer-events-auto absolute bottom-14 left-3 z-20 space-y-1">
          {ghostItems.map((name) => (
            <div
              key={name}
              className="glass-panel rounded-lg px-2.5 py-1.5 flex items-center gap-2 border border-dashed border-primary/30 opacity-60"
            >
              <CloudOff size={10} className="text-primary/60" />
              <span className="text-[10px] text-text-muted font-mono">{name}</span>
              <span className="text-[8px] text-primary/60 uppercase tracking-wider">Not deployed</span>
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
              title={`"${name}" is running but not declared in the spec`}
            >
              <AlertTriangle size={10} className="text-status-pending" />
              <span className="text-[10px] text-text-muted font-mono">{name}</span>
              <span className="text-[8px] text-status-pending uppercase tracking-wider">Not in spec</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
