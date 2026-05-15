import { useEffect, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { ArrowLeft, GitBranch, Activity, AlertCircle } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useRunTrace } from '../agent/ide/useRunTrace';
import { RunOutputView } from '../agent/ide/RunOutputView';
import { RunWaterfall } from '../runs/RunWaterfall';
import { statusTone } from '../runs/status';
import { StatusPill } from '../runs/StatusPill';
import {
  shortRunID,
  formatAbsoluteTime,
  formatDurationBetween,
} from '../runs/format';
import {
  fetchRunDetail,
  useRunsStore,
} from '../../stores/useRunsStore';
import type { AgentRunDetail } from '../../lib/agent-runs';

/**
 * RunDetailWorkspace is the /runs/:id page — full waterfall, span
 * filter, run metadata, and the same RunOutputView the inspector
 * uses (composed at a larger size). Designed to read like a Honeycomb
 * trace view: the timeline runs across the top, the output below.
 */
export function RunDetailWorkspace() {
  const { runID } = useParams<{ runID: string }>();
  const navigate = useNavigate();
  const [detail, setDetail] = useState<AgentRunDetail | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [spanFilter, setSpanFilter] = useState('');
  const runTrace = useRunTrace(runID ?? null);

  // Hydrate via the REST endpoint so we render history even when the
  // run completed long before this page mounted (no SSE replay).
  useEffect(() => {
    if (!runID) return;
    let cancelled = false;
    void (async () => {
      try {
        const d = await fetchRunDetail(runID);
        if (cancelled) return;
        if (!d) {
          setError(`Run ${runID} not found`);
          setDetail(null);
        } else {
          setError(null);
          setDetail(d);
        }
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [runID]);

  // Prefer live events from the SSE trace once it has caught up to the
  // hydrated snapshot; this way new events stream in without us having
  // to re-fetch.
  const events =
    runTrace.events.length > (detail?.events?.length ?? 0)
      ? runTrace.events
      : (detail?.events ?? []);

  const summary = detail?.run;
  const tone = statusTone(summary?.status);

  // Hook the inspector store so closing this page leaves a clean
  // selection in the grid behind us.
  const setSelectedRun = useRunsStore((s) => s.setSelectedRun);

  if (!runID) {
    return <ErrorState message="No run ID in URL" onBack={() => navigate('/runs')} />;
  }
  if (error) {
    return <ErrorState message={error} onBack={() => navigate('/runs')} />;
  }

  return (
    <div className="absolute inset-0 flex flex-col bg-background">
      <header className="flex items-center justify-between px-5 py-3 border-b border-border/40 bg-surface/40">
        <div className="flex items-center gap-3 min-w-0">
          <button
            type="button"
            onClick={() => {
              setSelectedRun(runID);
              navigate('/runs');
            }}
            className="inline-flex items-center gap-1 text-[11px] text-text-muted hover:text-text-primary"
          >
            <ArrowLeft size={12} />
            Back to Runs
          </button>
          <div className="h-4 w-px bg-border/50" />
          <div className="min-w-0">
            <div className="text-[10px] uppercase tracking-[0.3em] text-text-muted/70">
              Run
            </div>
            <div className="font-mono text-sm text-text-primary truncate" title={runID}>
              {shortRunID(runID)}
            </div>
          </div>
          {summary?.parent_run_id && (
            <button
              type="button"
              onClick={() => navigate(`/runs/${summary.parent_run_id}`)}
              className="inline-flex items-center gap-1 text-[11px] text-primary hover:underline"
            >
              <GitBranch size={11} />
              parent {shortRunID(summary.parent_run_id)}
            </button>
          )}
        </div>
        <StatusPill tone={tone} size="inspector" />
      </header>

      <div className="grid grid-cols-[2fr_1fr] flex-1 min-h-0">
        <section className="flex flex-col min-h-0 border-r border-border/30">
          <div className="flex items-center justify-between px-5 py-2.5 border-b border-border/30 bg-surface/40">
            <div className="inline-flex items-center gap-2 text-[11px] text-text-muted">
              <Activity size={11} aria-hidden />
              <span className="uppercase tracking-[0.14em]">Span waterfall</span>
              <span className="text-text-muted/60">({events.length} events)</span>
            </div>
            <input
              type="text"
              placeholder="Filter spans…"
              value={spanFilter}
              onChange={(e) => setSpanFilter(e.target.value)}
              className="h-6 px-2 text-[11px] bg-background/60 border border-border/40 rounded text-text-secondary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50 w-44"
            />
          </div>
          <div className="flex-1 min-h-0 overflow-auto scrollbar-dark py-3">
            <RunWaterfall events={events} filter={spanFilter.trim() || undefined} />
          </div>
          <div className="px-5 py-2 border-t border-border/30 flex items-center gap-3 text-[10px] text-text-muted">
            <Metadata
              label="Skill"
              value={summary?.skill ?? '—'}
              mono
            />
            <Metadata
              label="Started"
              value={formatAbsoluteTime(summary?.started_at)}
            />
            <Metadata
              label="Duration"
              value={formatDurationBetween(summary?.started_at, summary?.completed_at)}
              mono
            />
            <Metadata label="Events" value={String(events.length)} mono />
          </div>
        </section>

        <section className="flex flex-col min-h-0 overflow-auto scrollbar-dark px-5 py-4">
          <RunOutputView runID={runID} runTrace={runTrace} />
        </section>
      </div>
    </div>
  );
}

function ErrorState({ message, onBack }: { message: string; onBack: () => void }) {
  return (
    <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 bg-background">
      <AlertCircle size={28} className="text-status-error" />
      <p className="text-sm text-status-error/90">{message}</p>
      <button
        type="button"
        onClick={onBack}
        className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md border border-border/40 text-xs text-text-secondary hover:bg-surface-highlight/30"
      >
        <ArrowLeft size={11} />
        Back to Runs
      </button>
    </div>
  );
}

function Metadata({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <span className="inline-flex items-center gap-1">
      <span className="uppercase tracking-[0.14em] text-text-muted/70">{label}:</span>
      <span className={cn('text-text-secondary', mono && 'font-mono tabular-nums')}>
        {value}
      </span>
    </span>
  );
}

export default RunDetailWorkspace;
