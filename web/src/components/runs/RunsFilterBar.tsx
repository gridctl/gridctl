import { useMemo } from 'react';
import { Filter, X } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useRunsStore, RUNS_DEFAULT_FILTERS } from '../../stores/useRunsStore';
import { FILTERABLE_STATUSES } from './status';

const SINCE_OPTIONS: { value: string; label: string }[] = [
  { value: '5m', label: 'Last 5 min' },
  { value: '1h', label: 'Last hour' },
  { value: '24h', label: 'Last 24 h' },
  { value: '7d', label: 'Last 7 days' },
  { value: '', label: 'All time' },
];

interface RunsFilterBarProps {
  /** Skills the operator can choose from — populated from the
   *  currently loaded page so the dropdown stays anchored in what's
   *  actually present. */
  skillOptions: string[];
}

export function RunsFilterBar({ skillOptions }: RunsFilterBarProps) {
  const filters = useRunsStore((s) => s.filters);
  const setFilters = useRunsStore((s) => s.setFilters);
  const resetFilters = useRunsStore((s) => s.resetFilters);

  const hasNonDefault = useMemo(
    () =>
      filters.status !== RUNS_DEFAULT_FILTERS.status ||
      filters.skill !== RUNS_DEFAULT_FILTERS.skill ||
      filters.since !== RUNS_DEFAULT_FILTERS.since ||
      filters.parent !== RUNS_DEFAULT_FILTERS.parent,
    [filters],
  );

  return (
    <div className="flex flex-wrap items-center gap-2 px-4 py-3 border-b border-border/30 bg-surface/40">
      <Filter size={12} className="text-text-muted/70" aria-hidden />

      <FilterChip label="Status">
        <select
          aria-label="Filter runs by status"
          value={filters.status}
          onChange={(e) => setFilters({ status: e.target.value })}
          className={chipInputClass}
        >
          <option value="">Any</option>
          {FILTERABLE_STATUSES.map((s) => (
            <option key={s.value} value={s.value}>
              {s.label}
            </option>
          ))}
        </select>
      </FilterChip>

      <FilterChip label="Skill">
        <select
          aria-label="Filter runs by skill"
          value={filters.skill}
          onChange={(e) => setFilters({ skill: e.target.value })}
          className={chipInputClass}
        >
          <option value="">Any</option>
          {skillOptions.map((skill) => (
            <option key={skill} value={skill}>
              {skill}
            </option>
          ))}
        </select>
      </FilterChip>

      <FilterChip label="Since">
        <select
          aria-label="Filter runs by start window"
          value={filters.since}
          onChange={(e) => setFilters({ since: e.target.value })}
          className={chipInputClass}
        >
          {SINCE_OPTIONS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
      </FilterChip>

      <FilterChip label="Parent">
        <input
          type="text"
          placeholder="run_id"
          aria-label="Filter runs by parent run ID"
          value={filters.parent}
          onChange={(e) => setFilters({ parent: e.target.value })}
          className={cn(chipInputClass, 'font-mono w-44 placeholder:text-text-muted/40')}
        />
      </FilterChip>

      {hasNonDefault && (
        <button
          type="button"
          onClick={resetFilters}
          className="ml-1 inline-flex items-center gap-1 px-2 py-1 text-[11px] text-text-muted hover:text-text-primary transition-colors"
        >
          <X size={11} />
          Reset
        </button>
      )}
    </div>
  );
}

const chipInputClass = cn(
  'h-7 px-2 text-[11px] rounded-md',
  'bg-background/60 border border-border/40 text-text-secondary',
  'focus:outline-none focus:border-primary/50 focus:bg-background',
);

function FilterChip({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="inline-flex items-center gap-1.5">
      <span className="text-[10px] font-medium uppercase tracking-[0.16em] text-text-muted/70">
        {label}
      </span>
      {children}
    </label>
  );
}
