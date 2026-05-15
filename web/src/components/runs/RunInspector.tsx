import { useMemo } from 'react';
import { X, ExternalLink, GitBranch, Layers } from 'lucide-react';
import { useNavigate } from 'react-router-dom';
import { cn } from '../../lib/cn';
import { useRunsStore } from '../../stores/useRunsStore';
import { useRunTrace } from '../agent/ide/useRunTrace';
import { RunOutputView } from '../agent/ide/RunOutputView';
import { statusTone } from './status';
import { StatusPill } from './StatusPill';
import { shortRunID, formatDurationBetween, formatAbsoluteTime } from './format';

interface RunInspectorProps {
  runID: string;
  onClose: () => void;
  onOpenDetail: () => void;
}

/**
 * RunInspector is the right-rail companion to the runs grid. It opens
 * a per-run SSE stream via useRunTrace, surfaces run metadata
 * (ancestry, timing, status) and composes the existing
 * RunOutputView so the inspector stays in lockstep with what
 * developers already know from the Agent IDE.
 *
 * The detail view at /runs/:id is the larger Honeycomb-style
 * waterfall — the inspector intentionally stays scannable.
 */
export function RunInspector({ runID, onClose, onOpenDetail }: RunInspectorProps) {
  const navigate = useNavigate();
  const runTrace = useRunTrace(runID);
  const run = useRunsStore((s) =>
    s.runs.find((r) => r.run_id === runID),
  );

  const tone = useMemo(() => statusTone(run?.status), [run?.status]);
  const children = useRunsStore((s) =>
    s.runs.filter((r) => r.parent_run_id === runID),
  );

  return (
    <aside
      aria-label="Run inspector"
      className="h-full flex flex-col bg-surface/40 backdrop-blur-sm"
    >
      <header className="flex items-center justify-between px-4 py-3 border-b border-border/30">
        <div className="min-w-0">
          <div className="text-[10px] uppercase tracking-[0.3em] text-text-muted/70">Run</div>
          <button
            type="button"
            onClick={onOpenDetail}
            className="block font-mono text-sm text-text-primary hover:underline truncate"
            title={runID}
          >
            {shortRunID(runID)}
          </button>
        </div>
        <button
          type="button"
          onClick={onClose}
          aria-label="Close inspector"
          className="p-1 rounded-md text-text-muted hover:text-text-primary hover:bg-surface-highlight/40"
        >
          <X size={14} />
        </button>
      </header>

      <div className="px-4 py-3 border-b border-border/20 space-y-2">
        <StatusPill tone={tone} />
        <dl className="grid grid-cols-[80px_1fr] gap-y-1.5 text-[11px]">
          <Term label="Skill" />
          <Definition>{run?.skill ?? '—'}</Definition>
          <Term label="Started" />
          <Definition className="font-mono tabular-nums">
            {formatAbsoluteTime(run?.started_at)}
          </Definition>
          <Term label="Duration" />
          <Definition className="font-mono tabular-nums">
            {formatDurationBetween(run?.started_at, run?.completed_at)}
          </Definition>
          {run?.parent_run_id && (
            <>
              <Term label="Parent" />
              <Definition>
                <button
                  type="button"
                  onClick={() => navigate(`/runs/${run.parent_run_id}`)}
                  className="inline-flex items-center gap-1 font-mono text-primary hover:underline"
                >
                  <GitBranch size={10} />
                  {shortRunID(run.parent_run_id)}
                </button>
              </Definition>
            </>
          )}
          {children.length > 0 && (
            <>
              <Term label="Children" />
              <Definition>
                <span className="inline-flex items-center gap-1 text-text-secondary">
                  <Layers size={10} aria-hidden />
                  {children.length}
                </span>
              </Definition>
            </>
          )}
        </dl>
      </div>

      <div className="flex-1 min-h-0 overflow-auto scrollbar-dark px-4 py-4">
        <RunOutputView runID={runID} runTrace={runTrace} />
      </div>

      <footer className="flex items-center gap-2 px-4 py-2 border-t border-border/30 bg-surface/60">
        <button
          type="button"
          onClick={onOpenDetail}
          className="inline-flex items-center gap-1.5 px-2.5 py-1 text-[11px] rounded-md border border-primary/30 text-primary hover:bg-primary/10"
        >
          <ExternalLink size={11} />
          Open detail
        </button>
        {/* TODO(runs-external-telemetry): once we ship the OTel handoff
            for Phoenix / Langfuse / LangSmith, add a launcher here. */}
      </footer>
    </aside>
  );
}

function Term({ label }: { label: string }) {
  return (
    <dt className="text-[10px] uppercase tracking-[0.16em] text-text-muted/70">
      {label}
    </dt>
  );
}

function Definition({
  className,
  children,
}: {
  className?: string;
  children: React.ReactNode;
}) {
  return <dd className={cn('text-text-secondary truncate', className)}>{children}</dd>;
}
