import { useCallback, useMemo, useState } from 'react';
import { ChevronRight, ChevronDown, AlertCircle, RefreshCw, Loader2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { formatRelativeTime } from '../../lib/time';
import { useRunsStore } from '../../stores/useRunsStore';
import { statusTone } from './status';
import { StatusPill } from './StatusPill';
import { formatDurationBetween, shortRunID } from './format';
import { buildRunTree, flattenRunTree, type RunRowNode } from './tree';

interface RunsGridProps {
  /** Triggered when a row is double-clicked or the run-id link is
   *  clicked — navigates to /runs/:id. */
  onOpenDetail: (runID: string) => void;
}

export function RunsGrid({ onOpenDetail }: RunsGridProps) {
  const runs = useRunsStore((s) => s.runs);
  const loading = useRunsStore((s) => s.loading);
  const loadingMore = useRunsStore((s) => s.loadingMore);
  const error = useRunsStore((s) => s.error);
  const nextCursor = useRunsStore((s) => s.nextCursor);
  const selectedRunID = useRunsStore((s) => s.selectedRunID);
  const setSelectedRun = useRunsStore((s) => s.setSelectedRun);
  const loadRuns = useRunsStore((s) => s.loadRuns);
  const loadMore = useRunsStore((s) => s.loadMore);

  const tree = useMemo(() => buildRunTree(runs), [runs]);
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  const rows = useMemo(() => flattenRunTree(tree, collapsed), [tree, collapsed]);

  const handleToggle = useCallback(
    (id: string) => {
      setCollapsed((prev) => {
        const next = new Set(prev);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        return next;
      });
    },
    [setCollapsed],
  );

  if (error && runs.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full gap-3 px-6">
        <AlertCircle size={24} className="text-status-error" />
        <p className="text-sm text-status-error/90 text-center max-w-md">{error}</p>
        <button
          type="button"
          onClick={() => void loadRuns()}
          className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-md border border-border/50 text-xs hover:bg-surface-highlight/40"
        >
          <RefreshCw size={12} /> Retry
        </button>
      </div>
    );
  }

  if (!loading && runs.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center h-full gap-2 text-text-muted">
        <p className="text-sm">No runs match your filters.</p>
        <p className="text-[11px] text-text-muted/60">
          Launch a skill from the Skills workspace and it'll appear here live.
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col h-full min-h-0">
      <div className="flex-1 min-h-0 overflow-auto scrollbar-dark">
        <table className="w-full text-[12px]">
          <thead className="sticky top-0 z-10 bg-surface/95 backdrop-blur">
            <tr className="border-b border-border/40 text-[10px] uppercase tracking-[0.16em] text-text-muted">
              <Th className="w-[24%]">Run</Th>
              <Th className="w-[18%]">Skill</Th>
              <Th className="w-[14%]">Status</Th>
              <Th className="w-[12%]">Started</Th>
              <Th className="w-[10%]">Duration</Th>
              <Th className="w-[8%] text-right">Events</Th>
              <Th className="w-[14%]">Error</Th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <RunRow
                key={row.run.run_id}
                node={row}
                isActive={row.run.run_id === selectedRunID}
                isCollapsed={collapsed.has(row.run.run_id)}
                onSelect={() => setSelectedRun(row.run.run_id)}
                onOpen={() => onOpenDetail(row.run.run_id)}
                onToggle={() => handleToggle(row.run.run_id)}
              />
            ))}
          </tbody>
        </table>
      </div>

      <div className="flex items-center justify-between px-4 py-2 border-t border-border/30 bg-surface/40 text-[11px] text-text-muted">
        <span>
          {runs.length} run{runs.length === 1 ? '' : 's'}
          {nextCursor ? ' (more available)' : ''}
        </span>
        <div className="flex items-center gap-2">
          {loading && <Loader2 size={12} className="animate-spin" aria-hidden />}
          {nextCursor && (
            <button
              type="button"
              onClick={() => void loadMore()}
              disabled={loadingMore}
              className="inline-flex items-center gap-1 px-2 py-1 rounded border border-border/40 text-text-secondary hover:bg-surface-highlight/30 disabled:opacity-60"
            >
              {loadingMore ? (
                <>
                  <Loader2 size={11} className="animate-spin" /> Loading…
                </>
              ) : (
                'Load more'
              )}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

interface RunRowProps {
  node: RunRowNode;
  isActive: boolean;
  isCollapsed: boolean;
  onSelect: () => void;
  onOpen: () => void;
  onToggle: () => void;
}

function RunRow({
  node,
  isActive,
  isCollapsed,
  onSelect,
  onOpen,
  onToggle,
}: RunRowProps) {
  const { run, depth, children } = node;
  const tone = statusTone(run.status);
  const hasChildren = children.length > 0;
  const startedDate = run.started_at ? new Date(run.started_at) : null;
  const startedValid =
    startedDate && !Number.isNaN(startedDate.getTime()) ? startedDate : null;

  return (
    <tr
      onClick={onSelect}
      onDoubleClick={onOpen}
      aria-selected={isActive}
      className={cn(
        'cursor-pointer border-b border-border/15 transition-colors group',
        'border-l-2 border-l-transparent',
        isActive
          ? 'bg-primary/5 border-l-primary'
          : ['hover:bg-surface-highlight/30', tone.rowAccent.replace('border-l-', 'hover:border-l-')],
      )}
    >
      <Td>
        <div className="flex items-center gap-1.5" style={{ paddingLeft: `${depth * 14}px` }}>
          {hasChildren ? (
            <button
              type="button"
              aria-label={isCollapsed ? 'Expand children' : 'Collapse children'}
              onClick={(e) => {
                e.stopPropagation();
                onToggle();
              }}
              className="inline-flex w-4 h-4 items-center justify-center text-text-muted hover:text-text-primary"
            >
              {isCollapsed ? <ChevronRight size={11} /> : <ChevronDown size={11} />}
            </button>
          ) : (
            <span aria-hidden className="inline-block w-4" />
          )}
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onOpen();
            }}
            className="font-mono text-[11.5px] text-text-primary hover:underline focus:outline-none focus:underline tabular-nums"
            title={run.run_id}
          >
            {shortRunID(run.run_id)}
          </button>
        </div>
      </Td>
      <Td className="font-mono text-text-secondary truncate">{run.skill ?? '—'}</Td>
      <Td>
        <StatusPill tone={tone} />
      </Td>
      <Td className="text-text-muted tabular-nums">
        {startedValid ? formatRelativeTime(startedValid) : '—'}
      </Td>
      <Td className="text-text-muted font-mono tabular-nums">
        {formatDurationBetween(run.started_at, run.completed_at)}
      </Td>
      <Td className="text-right text-text-muted tabular-nums">{run.event_count}</Td>
      <Td className="text-status-error/80 truncate">
        <span title={run.error}>{run.error ?? ''}</span>
      </Td>
    </tr>
  );
}

function Th({ className, children }: { className?: string; children: React.ReactNode }) {
  return (
    <th
      className={cn('px-3 py-2 text-left font-medium align-middle', className)}
      scope="col"
    >
      {children}
    </th>
  );
}

function Td({ className, children }: { className?: string; children: React.ReactNode }) {
  return (
    <td className={cn('px-3 py-2 align-middle max-w-0', className)}>{children}</td>
  );
}

