import { useEffect, useState } from 'react';
import { CheckCircle2, ShieldAlert, X } from 'lucide-react';
import { fetchAgentRuns, approveAgentRun, type AgentRunSummary } from '../../lib/agent-runs';
import { cn } from '../../lib/cn';

const POLL_INTERVAL_MS = 5000;

/**
 * ApprovalBanner surfaces every run currently suspended on an approval
 * gate. It polls /api/agent/runs every 5s and renders one row per
 * run with status="awaiting_approval", offering Approve / Reject
 * actions inline. Successful actions clear the row optimistically;
 * the next poll reconciles against server truth.
 *
 * The banner sits below the Header in the App grid (z-30) so it does
 * not collide with the shutdown overlay or auth prompt.
 */
export function ApprovalBanner() {
  const [pending, setPending] = useState<AgentRunSummary[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    async function poll() {
      try {
        const { runs } = await fetchAgentRuns(100);
        if (cancelled) return;
        setPending(runs.filter((r) => r.status === 'awaiting_approval' && r.pending_approval));
        setError(null);
      } catch (err) {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : String(err));
      }
    }
    void poll();
    const handle = window.setInterval(poll, POLL_INTERVAL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(handle);
    };
  }, []);

  if (error || pending.length === 0) {
    return null;
  }

  async function handleDecision(run: AgentRunSummary, approved: boolean) {
    setBusy(run.run_id);
    try {
      await approveAgentRun({
        run_id: run.run_id,
        approval_id: run.pending_approval,
        approved,
        source: 'web',
      });
      setPending((prev) => prev.filter((r) => r.run_id !== run.run_id));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(null);
    }
  }

  return (
    <div className="absolute top-16 left-1/2 -translate-x-1/2 z-30 flex flex-col gap-2 max-w-2xl w-full px-4">
      {pending.map((run) => (
        <div
          key={run.run_id}
          className={cn(
            'flex items-center gap-3 px-4 py-2 rounded-lg backdrop-blur-xl border shadow-lg',
            'bg-status-warning/10 border-status-warning/30 text-status-warning',
            'animate-fade-in-scale',
          )}
        >
          <ShieldAlert size={18} className="shrink-0" />
          <div className="flex-1 min-w-0">
            <div className="text-sm font-medium truncate">
              Approval pending — {run.skill ?? 'agent run'}
            </div>
            <div className="text-xs text-text-muted truncate">
              {run.run_id}
            </div>
          </div>
          <button
            type="button"
            disabled={busy === run.run_id}
            onClick={() => void handleDecision(run, true)}
            className={cn(
              'inline-flex items-center gap-1 px-3 py-1 rounded-md text-xs font-medium',
              'bg-status-running/20 text-status-running hover:bg-status-running/30',
              'transition-colors disabled:opacity-50',
            )}
          >
            <CheckCircle2 size={14} /> Approve
          </button>
          <button
            type="button"
            disabled={busy === run.run_id}
            onClick={() => void handleDecision(run, false)}
            className={cn(
              'inline-flex items-center gap-1 px-3 py-1 rounded-md text-xs font-medium',
              'bg-status-error/20 text-status-error hover:bg-status-error/30',
              'transition-colors disabled:opacity-50',
            )}
          >
            <X size={14} /> Reject
          </button>
        </div>
      ))}
    </div>
  );
}
