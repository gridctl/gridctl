import { useCallback, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { cn } from '../../../lib/cn';
import { formatRelativeTime } from '../../../lib/time';
import type { AgentRunSummary } from '../../../lib/agent-runs';
import { useRunsForSkill } from './useRunsForSkill';
import {
  statusTone,
  type RunStatusTone,
} from '../../runs/status';
import {
  buildRunTree,
  flattenRunTree,
  type RunRowNode,
} from '../../runs/tree';

interface RunsListProps {
  /**
   * Bump to force a re-fetch — AgentIDE increments this when a new
   * run is launched so the sidebar reflects the new row immediately.
   */
  refreshKey: number;
  /**
   * Currently selected run id (from URL). The row gets a highlight
   * so the operator can scan back to where they are.
   */
  activeRunID: string | null;
}

/**
 * RunsList is the Slice C surface — the SkillSidebar's `Runs` tab
 * body. Renders the ~100 most recent runs across all skills, sorted
 * by `started_at` desc, with child runs (non-empty `parent_run_id`)
 * indented under their parent and a disclosure caret on parent rows.
 *
 * Clicking a row sets `?skill=<r.skill>&run=<r.run_id>` so the IDE's
 * existing trace overlay path activates. The list is a primary
 * surface for MCP-client users (who arrive at the IDE with no prior
 * skill context), so failure is loud — we surface the error with a
 * retry button rather than silently degrading.
 */
export function RunsList({ refreshKey, activeRunID }: RunsListProps) {
  const [, setParams] = useSearchParams();
  const { runs, loading, error, refresh } = useRunsForSkill({
    fetchLimit: 100,
    refreshKey,
  });

  const tree = useMemo(() => buildRunTree(runs), [runs]);

  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());

  const toggleCollapsed = useCallback((id: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }, []);

  const handleSelectRun = useCallback(
    (run: AgentRunSummary) => {
      setParams(
        (curr) => {
          const next = new URLSearchParams(curr);
          if (run.skill) next.set('skill', run.skill);
          else next.delete('skill');
          next.set('run', run.run_id);
          return next;
        },
        { replace: true },
      );
    },
    [setParams],
  );

  if (loading && runs.length === 0) {
    return (
      <div className="px-5 py-8 text-center" aria-live="polite">
        <span className="font-mono text-[10px] text-text-muted/70 animate-pulse uppercase tracking-[0.3em]">
          loading runs…
        </span>
      </div>
    );
  }

  if (error) {
    return (
      <div className="mx-5 my-4 px-3 py-3 rounded-md border border-status-error/30 bg-status-error/5">
        <div className="font-sans text-status-error/80 text-[10px] uppercase tracking-[0.3em] mb-1">
          couldn't load runs
        </div>
        <p className="font-mono text-[11px] text-status-error/90 mb-2 break-words">{error}</p>
        <button
          type="button"
          onClick={refresh}
          className={cn(
            'inline-flex items-center px-2 py-1 rounded',
            'font-mono text-[10px] uppercase tracking-[0.16em]',
            'border border-status-error/30 text-status-error',
            'hover:bg-status-error/10 transition-colors',
            'focus:outline-none focus-visible:ring-1 focus-visible:ring-status-error/50',
          )}
        >
          retry
        </button>
      </div>
    );
  }

  if (runs.length === 0) {
    return (
      <div className="mx-5 my-8 text-center text-text-muted text-xs leading-relaxed">
        <div className="font-sans text-text-muted/40 text-[10px] uppercase tracking-[0.4em] mb-2">
          no runs yet
        </div>
        <p>
          Launch a skill from the <code className="font-mono text-text-secondary">Skills</code>{' '}
          tab — its run lands here the moment it starts.
        </p>
      </div>
    );
  }

  // Flatten the tree honouring collapsed state.
  const rows = flattenRunTree(tree, collapsed);

  return (
    <ul
      role="list"
      className="px-2 space-y-px"
      aria-label="Recent agent runs"
    >
      {rows.map((row) => (
        <RunRow
          key={row.run.run_id}
          row={row}
          isActive={activeRunID === row.run.run_id}
          isCollapsed={collapsed.has(row.run.run_id)}
          hasChildren={row.children.length > 0}
          onSelect={handleSelectRun}
          onToggle={toggleCollapsed}
        />
      ))}
    </ul>
  );
}

interface RunRowProps {
  row: RunRowNode;
  isActive: boolean;
  isCollapsed: boolean;
  hasChildren: boolean;
  onSelect: (run: AgentRunSummary) => void;
  onToggle: (id: string) => void;
}

function RunRow({ row, isActive, isCollapsed, hasChildren, onSelect, onToggle }: RunRowProps) {
  const { run, depth } = row;
  const tone = statusTone(run.status);
  const startedDate = run.started_at ? new Date(run.started_at) : null;
  const startedValid = startedDate && !isNaN(startedDate.getTime()) ? startedDate : null;

  return (
    <li
      style={{ paddingLeft: `${depth * 14}px` }}
      className="relative"
    >
      {depth > 0 && (
        <span
          aria-hidden
          className="pointer-events-none absolute left-2 top-0 bottom-0 border-l border-border-subtle/60"
          style={{ left: `${depth * 14 - 8}px` }}
        />
      )}
      <button
        type="button"
        onClick={() => onSelect(run)}
        aria-current={isActive ? 'true' : undefined}
        className={cn(
          'group w-full text-left px-2 py-1.5 rounded-md',
          'border border-transparent transition-colors',
          'focus:outline-none focus-visible:ring-1 focus-visible:ring-primary/60',
          isActive
            ? 'bg-surface-elevated/80 border-border-subtle'
            : 'hover:bg-surface/50 hover:border-border-subtle/60',
        )}
      >
        <div className="flex items-center gap-2">
          {hasChildren ? (
            <span
              role="button"
              tabIndex={-1}
              aria-label={isCollapsed ? 'expand children' : 'collapse children'}
              onClick={(e) => {
                e.stopPropagation();
                onToggle(run.run_id);
              }}
              className={cn(
                'inline-flex w-3.5 h-3.5 items-center justify-center',
                'text-text-muted hover:text-text-primary transition-transform',
                isCollapsed ? '' : 'rotate-90',
              )}
            >
              <span aria-hidden className="text-[9px] leading-none">▶</span>
            </span>
          ) : (
            <span aria-hidden className="inline-block w-3.5" />
          )}
          <StatusDot tone={tone} />
          <span className="font-mono text-[11px] text-text-primary tabular-nums">
            {run.run_id.slice(0, 8)}
          </span>
          <span className="flex-1 min-w-0 font-mono text-[11px] text-text-muted truncate">
            {run.skill ?? '—'}
          </span>
        </div>
        <div className="mt-0.5 flex items-center justify-between gap-2 pl-[22px] font-mono text-[10px] text-text-muted">
          <span className={cn('uppercase tracking-[0.16em]', tone.text)}>
            {run.status || 'unknown'}
          </span>
          <div className="flex items-center gap-2">
            {startedValid && (
              <span className="tabular-nums">{formatRelativeTime(startedValid)}</span>
            )}
            <span className="text-text-muted/30">·</span>
            <span className="tabular-nums">{run.event_count} ev</span>
          </div>
        </div>
      </button>
    </li>
  );
}

// The IDE sidebar prefers a dot-only status row (no icon, no label —
// the depth indent + skill name is already the row's main signal).
// StatusDot renders the shared tone with the IDE's glow halo.
function StatusDot({ tone }: { tone: RunStatusTone }) {
  return (
    <span
      aria-hidden
      className={cn('inline-block w-1.5 h-1.5 rounded-full', tone.dot)}
      style={tone.glow ? { boxShadow: tone.glow } : undefined}
    />
  );
}
