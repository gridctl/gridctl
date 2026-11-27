import { useEffect, useId, useRef, useState } from 'react';
import { SlidersHorizontal, X } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Popover } from '../ui/Popover';
import { useRunsStore } from '../../stores/useRunsStore';
import {
  RUN_DISPOSITIONS,
  RUN_HIDDEN_FILTERS,
  RUN_TIME_RANGES,
  activeHiddenFilters,
  hasActiveRunFilters,
  localInputToRFC3339,
  rfc3339ToLocalInput,
  type RunTextFilterKey,
  type RunsTimeRange,
} from '../../lib/runsFilters';

const CONTROL =
  'h-6 px-1.5 text-3xs bg-background/60 border border-border/40 rounded text-text-secondary';

interface RunsFilterBarProps {
  servers: string[];
}

// The Runs filter row: five visible filters, the More filters popover with
// the ID filters, chips for active hidden filters, and Clear filters. Text
// inputs keep a local draft and commit after a pause, on Enter, or on blur.
export function RunsFilterBar({ servers }: RunsFilterBarProps) {
  const filters = useRunsStore((s) => s.filters);
  const setFilters = useRunsStore((s) => s.setFilters);
  const clearFilters = useRunsStore((s) => s.clearFilters);
  const [moreOpen, setMoreOpen] = useState(false);
  const [resetToken, setResetToken] = useState(0);
  const moreRef = useRef<HTMLButtonElement>(null);
  const fromId = useId();
  const toId = useId();

  const hidden = activeHiddenFilters(filters);
  const active = hasActiveRunFilters(filters);
  const serverOptions = filters.server && !servers.includes(filters.server) ? [filters.server, ...servers] : servers;
  const inverted =
    filters.range === 'custom' &&
    filters.since !== '' &&
    filters.until !== '' &&
    new Date(filters.since).getTime() > new Date(filters.until).getTime();

  const setText = (key: RunTextFilterKey) => (value: string) => setFilters({ [key]: value });

  // Pressing a clear control must not blur a focused input first: the blur
  // would commit the draft the click is about to clear.
  const keepFocus = (e: React.MouseEvent) => e.preventDefault();

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2">
        <select
          aria-label="Filter by server"
          className={cn(CONTROL, 'max-w-[160px]')}
          value={filters.server}
          onChange={(e) => setFilters({ server: e.target.value })}
        >
          <option value="">All servers</option>
          {serverOptions.map((s) => (
            <option key={s} value={s}>
              {s}
            </option>
          ))}
        </select>
        <select
          aria-label="Filter by disposition"
          className={CONTROL}
          value={filters.disposition}
          onChange={(e) => setFilters({ disposition: e.target.value })}
        >
          <option value="">All dispositions</option>
          {RUN_DISPOSITIONS.map((d) => (
            <option key={d} value={d}>
              {d}
            </option>
          ))}
        </select>
        <DebouncedTextInput
          ariaLabel="Filter by tool"
          placeholder="Tool (exact name)"
          className={cn(CONTROL, 'w-36')}
          value={filters.tool}
          onCommit={setText('tool')}
          resetToken={resetToken}
        />
        <DebouncedTextInput
          ariaLabel="Filter by client"
          placeholder="Client (exact name)"
          className={cn(CONTROL, 'w-36')}
          value={filters.client}
          onCommit={setText('client')}
          resetToken={resetToken}
        />
        <select
          aria-label="Time range"
          className={CONTROL}
          value={filters.range}
          onChange={(e) => setFilters({ range: e.target.value as RunsTimeRange })}
        >
          {RUN_TIME_RANGES.map((r) => (
            <option key={r.value} value={r.value}>
              {r.label}
            </option>
          ))}
        </select>
        <div className="relative">
          <button
            ref={moreRef}
            type="button"
            aria-expanded={moreOpen}
            aria-haspopup="dialog"
            onClick={() => setMoreOpen((v) => !v)}
            className={cn(
              'h-6 px-2 text-3xs font-medium rounded border transition-colors flex items-center gap-1',
              hidden.length > 0
                ? 'bg-primary/10 text-primary border-primary/30'
                : 'bg-background/60 text-text-muted border-border/40 hover:text-text-secondary hover:border-border/60',
            )}
          >
            <SlidersHorizontal size={9} aria-hidden="true" />
            {hidden.length > 0 ? `More filters (${hidden.length})` : 'More filters'}
          </button>
          <Popover
            open={moreOpen}
            onClose={() => setMoreOpen(false)}
            triggerRef={moreRef}
            aria-label="More filters"
            className="absolute left-0 top-full mt-1 w-64 p-2 space-y-1.5"
          >
            {RUN_HIDDEN_FILTERS.map(({ key, label, placeholder }) => (
              <LabeledTextInput
                key={key}
                label={label}
                placeholder={placeholder}
                value={filters[key]}
                onCommit={setText(key)}
                resetToken={resetToken}
              />
            ))}
          </Popover>
        </div>
        {active && (
          <button
            type="button"
            onMouseDown={keepFocus}
            onClick={() => {
              clearFilters();
              setResetToken((t) => t + 1);
            }}
            className="h-6 px-1.5 text-3xs text-text-muted hover:text-text-secondary transition-colors flex items-center gap-1 rounded hover:bg-surface-highlight/30"
          >
            <X size={9} aria-hidden="true" />
            Clear filters
          </button>
        )}
      </div>
      {filters.range === 'custom' && (
        <div className="flex flex-wrap items-center gap-2 text-3xs">
          <label htmlFor={fromId} className="text-text-muted">
            From
          </label>
          <input
            id={fromId}
            type="datetime-local"
            className={CONTROL}
            value={rfc3339ToLocalInput(filters.since)}
            onChange={(e) => setFilters({ since: localInputToRFC3339(e.target.value) })}
          />
          <label htmlFor={toId} className="text-text-muted">
            To
          </label>
          <input
            id={toId}
            type="datetime-local"
            className={CONTROL}
            value={rfc3339ToLocalInput(filters.until)}
            onChange={(e) => setFilters({ until: localInputToRFC3339(e.target.value) })}
          />
          {inverted && <span className="text-status-pending">From is after To</span>}
        </div>
      )}
      {hidden.length > 0 && (
        <div className="flex flex-wrap items-center gap-1.5">
          {hidden.map(({ key, label, value }) => (
            <span
              key={key}
              title={`${label}: ${value}`}
              className="flex items-center gap-1 h-6 pl-2 pr-1 text-3xs font-mono rounded border bg-primary/10 text-primary border-primary/30 max-w-[240px]"
            >
              <span className="truncate">
                {label}: {value}
              </span>
              <button
                type="button"
                aria-label={`Remove ${label} filter`}
                onMouseDown={keepFocus}
                onClick={() => setFilters({ [key]: '' })}
                className="flex-shrink-0 rounded p-0.5 hover:bg-primary/15"
              >
                <X size={9} aria-hidden="true" />
              </button>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}

function LabeledTextInput({
  label,
  ...rest
}: { label: string } & Omit<DebouncedTextInputProps, 'ariaLabel' | 'className' | 'id'>) {
  const id = useId();
  return (
    <div className="flex items-center justify-between gap-2">
      <label htmlFor={id} className="text-3xs text-text-secondary whitespace-nowrap">
        {label}
      </label>
      <DebouncedTextInput id={id} className={cn(CONTROL, 'w-36')} {...rest} />
    </div>
  );
}

interface DebouncedTextInputProps {
  /** Committed value from the store. */
  value: string;
  onCommit: (value: string) => void;
  /** Bumped by Clear filters to drop drafts and pending timers. */
  resetToken: number;
  delayMs?: number;
  id?: string;
  ariaLabel?: string;
  placeholder?: string;
  className?: string;
}

function DebouncedTextInput({
  value,
  onCommit,
  resetToken,
  delayMs = 300,
  id,
  ariaLabel,
  placeholder,
  className,
}: DebouncedTextInputProps) {
  const [draft, setDraft] = useState(value);
  const [seen, setSeen] = useState({ value, resetToken });
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pending = useRef<string | null>(null);
  const latest = useRef({ value, onCommit });

  // Resync the draft when the committed value changes from outside (chip,
  // Clear, URL), but not when it already matches what is being typed.
  if (seen.value !== value || seen.resetToken !== resetToken) {
    setSeen({ value, resetToken });
    if (seen.resetToken !== resetToken || draft.trim() !== value) setDraft(value);
  }

  useEffect(() => {
    latest.current = { value, onCommit };
  }, [value, onCommit]);

  const cancel = () => {
    if (timer.current != null) {
      clearTimeout(timer.current);
      timer.current = null;
    }
  };

  useEffect(() => {
    if (timer.current != null) {
      clearTimeout(timer.current);
      timer.current = null;
    }
    pending.current = null;
  }, [resetToken]);

  // An input that unmounts with a pending draft (the More filters popover
  // closing) commits it rather than dropping what was typed.
  useEffect(
    () => () => {
      if (timer.current != null) clearTimeout(timer.current);
      timer.current = null;
      const next = pending.current;
      pending.current = null;
      if (next != null && next.trim() !== latest.current.value) latest.current.onCommit(next.trim());
    },
    [],
  );

  const commit = (next: string) => {
    cancel();
    pending.current = null;
    const trimmed = next.trim();
    if (trimmed === latest.current.value) return;
    latest.current.onCommit(trimmed);
  };

  return (
    <input
      id={id}
      type="text"
      aria-label={ariaLabel}
      placeholder={placeholder}
      className={className}
      value={draft}
      onChange={(e) => {
        const next = e.target.value;
        setDraft(next);
        cancel();
        pending.current = next;
        timer.current = setTimeout(() => commit(next), delayMs);
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter') commit(draft);
      }}
      onBlur={() => commit(draft)}
    />
  );
}
