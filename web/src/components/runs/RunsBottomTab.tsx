import { useMemo } from 'react';
import { Activity, ExternalLink, AlertCircle } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { cn } from '../../lib/cn';
import { useRunsStore } from '../../stores/useRunsStore';
import { RunWaterfall } from './RunWaterfall';
import { useRunTrace } from '../agent/ide/useRunTrace';
import { shortRunID } from './format';
import { statusTone } from './status';

/**
 * RunsBottomTab is the BottomPanel "Runs" surface — a live span
 * waterfall across the most recent in-flight runs. The tab badge
 * (count of in-flight runs) is rendered by BottomPanel; this body
 * focuses on the waterfall + a row of run chips for quick pivoting.
 *
 * The OTel-style "Traces" tab next to this one still serves the
 * MCP-server HTTP traces — they're a different domain so the two
 * tabs stay separate.
 */
export function RunsBottomTab() {
  const inFlight = useRunsStore((s) => s.inFlightRuns);
  const runs = useRunsStore((s) => s.runs);
  const streamStatus = useRunsStore((s) => s.streamStatus);

  const liveRuns = useMemo(() => {
    return runs.filter((r) => inFlight.has(r.run_id)).slice(0, 8);
  }, [runs, inFlight]);

  // Pick a focal run for the waterfall: the most recent in-flight, or
  // the most recent run overall if nothing is live. Useful even when
  // the daemon is idle so the panel isn't a blank rectangle.
  const focal = liveRuns[0] ?? runs[0];
  const focalTrace = useRunTrace(focal?.run_id ?? null);

  return (
    <div className="flex flex-col h-full">
      <header className="flex items-center justify-between px-3 h-9 flex-shrink-0 border-b border-border/30 bg-surface-elevated/20">
        <div className="flex items-center gap-2 text-[11px] text-text-muted">
          <Activity size={11} aria-hidden />
          <span className="uppercase tracking-[0.14em]">Live runs</span>
          <span className="text-text-muted/60">
            ({inFlight.size} in flight)
          </span>
        </div>
        <StreamStatusChip status={streamStatus} />
      </header>

      {liveRuns.length === 0 && !focal && (
        <div className="flex flex-col items-center justify-center flex-1 gap-2 text-text-muted">
          <Activity size={24} className="text-text-muted/30" />
          <span className="text-xs">No runs yet</span>
          <span className="text-[10px] text-text-muted/60">
            Launch a skill from the Skills workspace.
          </span>
        </div>
      )}

      {focal && (
        <div className="flex flex-1 min-h-0">
          {liveRuns.length > 0 && (
            <div className="w-44 flex-shrink-0 border-r border-border/30 overflow-y-auto scrollbar-dark py-1">
              {liveRuns.map((r) => (
                <LiveRunChip key={r.run_id} runID={r.run_id} skill={r.skill} status={r.status} active={r.run_id === focal.run_id} />
              ))}
            </div>
          )}
          <div className="flex-1 min-h-0 overflow-auto scrollbar-dark py-2">
            <RunWaterfall events={focalTrace.events} />
          </div>
        </div>
      )}
    </div>
  );
}

function LiveRunChip({
  runID,
  skill,
  status,
  active,
}: {
  runID: string;
  skill?: string;
  status: string;
  active: boolean;
}) {
  const navigate = useNavigate();
  const tone = statusTone(status);
  return (
    <button
      type="button"
      onClick={() => navigate(`/runs/${runID}`)}
      className={cn(
        'w-full text-left px-3 py-1.5 flex items-center gap-2',
        'border-l-2 transition-colors',
        active
          ? 'bg-primary/5 border-primary'
          : 'border-transparent hover:bg-surface-highlight/30',
      )}
      title={runID}
    >
      <span
        aria-hidden
        className={cn(
          'inline-block w-1.5 h-1.5 rounded-full flex-shrink-0',
          tone.dot,
          tone.pulse && 'animate-pulse',
        )}
      />
      <div className="min-w-0 flex-1">
        <div className="font-mono text-[11px] text-text-primary truncate">
          {shortRunID(runID)}
        </div>
        <div className="font-mono text-[10px] text-text-muted truncate">
          {skill ?? '—'}
        </div>
      </div>
      <ExternalLink size={9} className="text-text-muted/60" aria-hidden />
    </button>
  );
}

// Note: LiveRunChip intentionally uses a custom layout (no StatusPill)
// because the chip is dot-only — the small list cell can't fit the
// status label. The full StatusPill is used everywhere status text is
// visible.

function StreamStatusChip({ status }: { status: ReturnType<typeof useRunsStore.getState>['streamStatus'] }) {
  if (status === 'open') {
    return (
      <span className="inline-flex items-center gap-1 text-[10px] text-status-running font-medium">
        <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse" />
        Live
      </span>
    );
  }
  if (status === 'restarted') {
    return (
      <span className="inline-flex items-center gap-1 text-[10px] text-status-pending">
        <AlertCircle size={10} />
        Resynced
      </span>
    );
  }
  if (status === 'error') {
    return (
      <span className="inline-flex items-center gap-1 text-[10px] text-status-error">
        <AlertCircle size={10} />
        Offline
      </span>
    );
  }
  return (
    <span className="inline-flex items-center gap-1 text-[10px] text-text-muted">
      <span className="w-1.5 h-1.5 rounded-full bg-text-muted/40" />
      {status === 'connecting' ? 'Connecting' : 'Idle'}
    </span>
  );
}
