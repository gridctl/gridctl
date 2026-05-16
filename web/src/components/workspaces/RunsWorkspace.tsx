import { useEffect, useMemo, useRef } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import { RunsFilterBar } from '../runs/RunsFilterBar';
import { RunsGrid } from '../runs/RunsGrid';
import { RunInspector } from '../runs/RunInspector';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { useRunsStore, RUNS_DEFAULT_FILTERS } from '../../stores/useRunsStore';
import type { RunsFilters } from '../../stores/useRunsStore';

const FILTER_KEYS = ['status', 'skill', 'since', 'parent'] as const;

function filtersFromSearchParams(params: URLSearchParams): RunsFilters {
  return {
    status: params.get('status') ?? RUNS_DEFAULT_FILTERS.status,
    skill: params.get('skill') ?? RUNS_DEFAULT_FILTERS.skill,
    since: params.get('since') ?? RUNS_DEFAULT_FILTERS.since,
    parent: params.get('parent') ?? RUNS_DEFAULT_FILTERS.parent,
  };
}

function filtersAreEqual(a: RunsFilters, b: RunsFilters): boolean {
  return (
    a.status === b.status &&
    a.skill === b.skill &&
    a.since === b.since &&
    a.parent === b.parent
  );
}

/**
 * RunsWorkspace renders the /runs grid + inspector inside AppShell.
 * URL binding is the single source of truth for the filter state:
 * the store mirrors the URL, not the other way around, so back/forward
 * navigation and reload preserve filters without an extra
 * persistence layer.
 */
export function RunsWorkspace() {
  const [searchParams, setSearchParams] = useSearchParams();
  const navigate = useNavigate();
  const filters = useRunsStore((s) => s.filters);
  const setFilters = useRunsStore((s) => s.setFilters);
  const selectedRunID = useRunsStore((s) => s.selectedRunID);
  const setSelectedRun = useRunsStore((s) => s.setSelectedRun);
  const loadRuns = useRunsStore((s) => s.loadRuns);
  const runs = useRunsStore((s) => s.runs);

  // URL → store (incoming).
  useEffect(() => {
    const next = filtersFromSearchParams(searchParams);
    if (!filtersAreEqual(next, filters)) {
      setFilters(next);
    }
  }, [searchParams]); // eslint-disable-line react-hooks/exhaustive-deps

  // Store → URL (outgoing). Strip filter keys so unrelated params
  // (e.g. ?compact, ?stack) survive the rewrite.
  useEffect(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        for (const key of FILTER_KEYS) {
          const val = filters[key];
          if (val && val !== RUNS_DEFAULT_FILTERS[key]) {
            next.set(key, val);
          } else {
            next.delete(key);
          }
        }
        return next;
      },
      { replace: true },
    );
  }, [filters, setSearchParams]);

  // Re-fetch whenever the filters change. The store does the deep
  // compare against its own state — this hook just triggers it.
  const lastLoadedFilters = useRef<RunsFilters | null>(null);
  useEffect(() => {
    if (lastLoadedFilters.current && filtersAreEqual(lastLoadedFilters.current, filters)) {
      return;
    }
    lastLoadedFilters.current = filters;
    void loadRuns();
  }, [filters, loadRuns]);

  const skillOptions = useMemo(() => {
    const seen = new Set<string>();
    for (const r of runs) {
      if (r.skill) seen.add(r.skill);
    }
    return [...seen].sort();
  }, [runs]);

  return (
    <div className="absolute inset-0 flex flex-col bg-background">
      <RunsFilterBar skillOptions={skillOptions} />

      <WorkspaceShell
        workspace="runs"
        defaultRightPct={25}
        minRightPx={300}
        right={
          selectedRunID ? (
            <div className="h-full bg-surface/40 border-l border-border/30">
              <RunInspector
                runID={selectedRunID}
                onClose={() => setSelectedRun(null)}
                onOpenDetail={() => navigate(`/runs/${selectedRunID}`)}
              />
            </div>
          ) : undefined
        }
      >
        <RunsGrid onOpenDetail={(id) => navigate(`/runs/${id}`)} />
      </WorkspaceShell>
    </div>
  );
}

export default RunsWorkspace;
