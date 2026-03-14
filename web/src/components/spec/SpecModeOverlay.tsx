import { useMemo } from 'react';
import { Eye, EyeOff, Link2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useSpecStore } from '../../stores/useSpecStore';
import { useStackStore } from '../../stores/useStackStore';

interface SpecModeOverlayProps {
  className?: string;
}

/**
 * Canvas overlay for spec mode — shows ghost nodes for items declared
 * in the spec but not deployed, and warning badges for running items
 * not in the spec. Also shows declared uses[] connections.
 */
export function SpecModeOverlay({ className }: SpecModeOverlayProps) {
  const health = useSpecStore((s) => s.health);
  const plan = useSpecStore((s) => s.plan);
  const drift = health?.drift;
  const agents = useStackStore((s) => s.agents);

  const { ghostItems, warningItems, usesConnections } = useMemo(() => {
    const added = drift?.added ?? [];
    const removed = drift?.removed ?? [];

    // Build uses[] connections from all agents (including non-running ones from spec)
    const connections: Array<{ agent: string; server: string }> = [];
    for (const agent of agents) {
      if (agent.uses) {
        for (const use of agent.uses) {
          connections.push({ agent: agent.name, server: use.server });
        }
      }
    }

    // Also add connections from plan diff items (added agents with uses)
    if (plan?.items) {
      for (const item of plan.items) {
        if (item.kind === 'agent' && item.action === 'add' && item.details) {
          for (const detail of item.details) {
            if (detail.startsWith('uses:')) {
              const servers = detail.slice(5).trim().split(',');
              for (const srv of servers) {
                connections.push({ agent: item.name, server: srv.trim() });
              }
            }
          }
        }
      }
    }

    return {
      ghostItems: added,
      warningItems: removed,
      usesConnections: connections,
    };
  }, [drift, agents, plan]);

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

      {/* Uses connections summary */}
      {usesConnections.length > 0 && (
        <div className="pointer-events-auto absolute top-10 right-3 z-20">
          <div className="glass-panel rounded-lg px-2.5 py-1.5 border border-secondary/20">
            <div className="text-[9px] text-secondary/60 uppercase tracking-wider mb-1">Declared connections</div>
            {usesConnections.slice(0, 8).map((c, i) => (
              <div key={i} className="flex items-center gap-1.5 text-[10px] text-text-muted">
                <Link2 size={8} className="text-secondary/40" />
                <span className="font-mono">{c.agent}</span>
                <span className="text-secondary/30">→</span>
                <span className="font-mono">{c.server}</span>
              </div>
            ))}
            {usesConnections.length > 8 && (
              <div className="text-[9px] text-text-muted mt-0.5">
                +{usesConnections.length - 8} more
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
