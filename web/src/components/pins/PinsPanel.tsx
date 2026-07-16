import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { Lock, LockOpen, CheckCircle2, ChevronRight, Clock, RefreshCw } from 'lucide-react';
import { cn } from '../../lib/cn';
import { usePinsStore } from '../../stores/usePinsStore';
import { approveServerPins, fetchServerPins } from '../../lib/api';
import { pinStatusMeta } from './pinStatus';
import { showToast } from '../ui/Toast';
import { formatRelativeTime } from '../../lib/time';

export function PinsPanel() {
  const pins = usePinsStore((s) => s.pins);
  const navigate = useNavigate();
  const [approvingServers, setApprovingServers] = useState<Set<string>>(new Set());

  const handleApprove = async (serverName: string) => {
    setApprovingServers((prev) => new Set(prev).add(serverName));
    try {
      await approveServerPins(serverName);
      const updated = await fetchServerPins();
      usePinsStore.getState().setPins(updated);
      showToast('success', `Pins approved for ${serverName}`);
    } catch (err) {
      showToast('error', `Failed to approve: ${err instanceof Error ? err.message : 'Unknown error'}`);
    } finally {
      setApprovingServers((prev) => {
        const next = new Set(prev);
        next.delete(serverName);
        return next;
      });
    }
  };

  if (pins === null) {
    return (
      <div className="flex items-center justify-center h-full text-text-muted text-xs gap-2">
        <Lock size={12} className="text-text-muted/60" />
        Pin monitoring not available
      </div>
    );
  }

  const entries = Object.entries(pins);

  if (entries.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-text-muted text-xs gap-2">
        <CheckCircle2 size={12} className="text-status-running" />
        No servers pinned yet
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full overflow-auto scrollbar-dark">
      <table className="w-full text-xs border-collapse">
        <thead>
          <tr className="border-b border-border/30 sticky top-0 bg-surface/90 backdrop-blur-sm">
            <th className="text-left px-4 py-2 text-text-muted font-medium">Server</th>
            <th className="text-left px-4 py-2 text-text-muted font-medium">Status</th>
            <th className="text-left px-4 py-2 text-text-muted font-medium">Tools</th>
            <th className="text-left px-4 py-2 text-text-muted font-medium">Last Verified</th>
            <th className="px-4 py-2" />
          </tr>
        </thead>
        <tbody>
          {entries.map(([name, sp]) => {
            const { label, colorClass } = pinStatusMeta(sp.status);
            const isApproving = approvingServers.has(name);
            const lastVerified = sp.last_verified_at
              ? formatRelativeTime(new Date(sp.last_verified_at))
              : '—';

            const openWorkspace = () => navigate(`/pins?server=${encodeURIComponent(name)}`);
            return (
              <tr
                key={name}
                onClick={() => {
                  // A mouseup that ends a text-selection drag must not
                  // navigate away from the text just selected.
                  if (window.getSelection()?.toString()) return;
                  openWorkspace();
                }}
                className="border-b border-border/20 hover:bg-surface-highlight/20 transition-colors cursor-pointer"
              >
                <td className="px-4 py-2.5 font-mono text-text-primary">{name}</td>
                <td className="px-4 py-2.5">
                  <span className={cn('flex items-center gap-1.5', colorClass)}>
                    {sp.status === 'drift' ? (
                      <LockOpen size={10} />
                    ) : (
                      <Lock size={10} />
                    )}
                    {label}
                  </span>
                </td>
                <td className="px-4 py-2.5 text-text-muted font-mono">{sp.tool_count}</td>
                <td className="px-4 py-2.5 text-text-muted">
                  <span className="flex items-center gap-1">
                    <Clock size={10} className="text-text-muted/60" />
                    {lastVerified}
                  </span>
                </td>
                <td className="px-4 py-2.5 text-right">
                  <div className="flex items-center justify-end gap-2">
                  {sp.status === 'drift' && (
                    <button
                      onClick={(e) => {
                        // Approving must not also trigger the row's navigation.
                        e.stopPropagation();
                        handleApprove(name);
                      }}
                      disabled={isApproving}
                      className={cn(
                        'flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium transition-all duration-200 ml-auto',
                        isApproving
                          ? 'text-text-muted bg-surface-highlight/30 cursor-not-allowed'
                          : 'text-status-running bg-status-running/10 border border-status-running/20 hover:bg-status-running/20'
                      )}
                    >
                      {isApproving ? (
                        <>
                          <RefreshCw size={10} className="animate-spin" />
                          Approving…
                        </>
                      ) : (
                        <>
                          <CheckCircle2 size={10} />
                          Approve
                        </>
                      )}
                    </button>
                  )}
                  {/* The row's onClick is a mouse convenience; this button is
                      the accessible affordance (keyboard focus + SR name). */}
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      openWorkspace();
                    }}
                    aria-label={`Open ${name} in Pins workspace`}
                    className="p-0.5 rounded text-text-muted/40 hover:text-text-primary hover:bg-surface-highlight transition-colors flex-shrink-0"
                  >
                    <ChevronRight size={13} aria-hidden="true" />
                  </button>
                  </div>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
