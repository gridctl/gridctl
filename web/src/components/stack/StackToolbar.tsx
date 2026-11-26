import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { ChevronDown, Network, Search, Table, X } from 'lucide-react';
import { cn } from '../../lib/cn';
import { isModalDialogOpen, isTextInputTarget } from '../../lib/dom';
import {
  STACK_FACETS,
  attentionRank,
  facetCounts,
  isFilterActive,
  matchesFilter,
  type StackFacet,
  type StackFilter,
} from '../../lib/stackRows';
import { useStackRows } from '../../hooks/useStackRows';
import { useUIStore } from '../../stores/useUIStore';
import { useAccessLensStore } from '../../stores/useAccessLensStore';
import { useCanvasActionsStore } from '../../stores/useCanvasActionsStore';
import { Popover } from '../ui/Popover';

interface StackToolbarProps {
  filter: StackFilter;
  view: 'graph' | 'table';
  onQueryChange: (query: string) => void;
  onFacetsChange: (facets: Set<StackFacet>) => void;
  /** ArrowDown from the filter input in table view. */
  onFocusTable?: () => void;
}

const BUTTON = 'inline-flex h-7 flex-shrink-0 items-center gap-1.5 rounded-md border px-2.5 text-xs font-medium transition-colors';
const BUTTON_IDLE = 'text-text-muted border-border-subtle hover:text-text-secondary hover:bg-surface-highlight';
const BUTTON_ON = 'bg-primary/10 text-primary border-primary/25';

const VIEW_OPTIONS = [
  { key: 'graph', label: 'Graph view', icon: Network },
  { key: 'table', label: 'Table view', icon: Table },
] as const;

/** Stack workspace toolbar: name filter, status facets, match count, and the view toggle. */
export function StackToolbar({ filter, view, onQueryChange, onFacetsChange, onFocusTable }: StackToolbarProps) {
  const rows = useStackRows();
  const lensEnabled = useAccessLensStore((s) => s.enabled);
  const setStackPrefs = useUIStore((s) => s.setStackPrefs);
  const inputRef = useRef<HTMLInputElement>(null);
  const statusTriggerRef = useRef<HTMLButtonElement>(null);
  const [statusOpen, setStatusOpen] = useState(false);

  const counts = useMemo(() => facetCounts(rows), [rows]);
  const active = isFilterActive(filter);
  const matchedIds = useMemo(
    () => (active ? rows.filter((row) => matchesFilter(row, filter)).map((row) => row.id) : []),
    [rows, filter, active],
  );
  const attentionCount = useMemo(() => rows.filter((row) => attentionRank(row) < 3).length, [rows]);

  // `/` focuses the filter from anywhere in the workspace, except while
  // typing in a field, inside a dialog, or with a modifier held.
  useEffect(() => {
    const onKeyDown = (event: globalThis.KeyboardEvent) => {
      if (event.key !== '/' || event.defaultPrevented) return;
      if (event.metaKey || event.ctrlKey || event.altKey) return;
      if (isTextInputTarget(event.target) || isModalDialogOpen()) return;
      if (event.target instanceof Element && event.target.closest('[role="dialog"], [role="alertdialog"]')) return;
      const input = inputRef.current;
      if (!input) return;
      event.preventDefault();
      input.focus();
      input.select();
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, []);

  const toggleFacet = (facet: StackFacet) => {
    const next = new Set(filter.facets);
    if (next.has(facet)) next.delete(facet);
    else next.add(facet);
    onFacetsChange(next);
  };

  const onInputKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Escape') {
      if (filter.query !== '') onQueryChange('');
      else event.currentTarget.blur();
      return;
    }
    if (event.key === 'Enter') {
      if (view !== 'graph' || !active || matchedIds.length === 0) return;
      event.preventDefault();
      useCanvasActionsStore.getState().fitView({
        nodes: matchedIds.map((id) => ({ id })),
        padding: 0.25,
        duration: 300,
        maxZoom: 1.5,
      });
      return;
    }
    if (event.key === 'ArrowDown' && view === 'table' && onFocusTable) {
      event.preventDefault();
      onFocusTable();
    }
  };

  const attentionOn = filter.facets.has('attention');
  const statusFacets = STACK_FACETS.filter(({ facet }) => counts[facet] > 0 || filter.facets.has(facet));

  return (
    <div className="flex h-10 flex-shrink-0 items-center gap-2 border-b border-border bg-surface px-3">
      <div className="relative w-56 min-w-0 flex-shrink">
        <Search size={13} aria-hidden="true" className="pointer-events-none absolute left-2 top-1/2 -translate-y-1/2 text-text-muted" />
        <input
          ref={inputRef}
          type="text"
          value={filter.query}
          onChange={(e) => onQueryChange(e.target.value)}
          onKeyDown={onInputKeyDown}
          aria-label="Filter stack by name"
          placeholder="Filter by name"
          spellCheck={false}
          autoComplete="off"
          className="h-7 w-full rounded-md border border-border-subtle bg-background pl-7 pr-7 text-xs text-text-primary placeholder:text-text-muted"
        />
        {filter.query !== '' && (
          <button
            type="button"
            aria-label="Clear name filter"
            title="Clear name filter"
            onClick={() => {
              onQueryChange('');
              inputRef.current?.focus();
            }}
            className="absolute right-0.5 top-1/2 inline-flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded-sm text-text-muted hover:text-text-secondary"
          >
            <X size={12} aria-hidden="true" />
          </button>
        )}
      </div>

      {(attentionCount > 0 || attentionOn) && (
        <button
          type="button"
          aria-pressed={attentionOn}
          onClick={() => toggleFacet('attention')}
          className={cn(BUTTON, attentionOn ? BUTTON_ON : BUTTON_IDLE)}
        >
          Needs attention
          <span className="font-mono">{attentionCount}</span>
        </button>
      )}

      <div className="relative flex-shrink-0">
        <button
          ref={statusTriggerRef}
          type="button"
          aria-expanded={statusOpen}
          aria-haspopup="dialog"
          onClick={() => setStatusOpen((open) => !open)}
          className={cn(BUTTON, filter.facets.size > 0 ? BUTTON_ON : BUTTON_IDLE)}
        >
          Status
          {filter.facets.size > 0 && <span className="font-mono">{filter.facets.size}</span>}
          <ChevronDown size={12} aria-hidden="true" />
        </button>
        <Popover
          open={statusOpen}
          onClose={() => setStatusOpen(false)}
          triggerRef={statusTriggerRef}
          aria-label="Status filter"
          className="absolute left-0 top-full mt-1 w-56 p-1.5"
        >
          {statusFacets.length === 0 ? (
            <p className="px-2 py-1.5 text-xs text-text-muted">Nothing to filter yet</p>
          ) : (
            <ul className="space-y-0.5">
              {statusFacets.map(({ facet, label }) => (
                <li key={facet}>
                  <label className="flex cursor-pointer items-center gap-2 rounded-sm px-2 py-1 text-xs text-text-secondary hover:bg-surface-highlight">
                    <input
                      type="checkbox"
                      checked={filter.facets.has(facet)}
                      onChange={() => toggleFacet(facet)}
                    />
                    <span className="flex-1">{label}</span>
                    <span className="font-mono text-text-muted">{counts[facet]}</span>
                  </label>
                </li>
              ))}
            </ul>
          )}
          <div className="mt-1 border-t border-border-subtle pt-1">
            <button
              type="button"
              onClick={() => onFacetsChange(new Set())}
              disabled={filter.facets.size === 0}
              className="w-full rounded-sm px-2 py-1 text-left text-xs text-text-muted hover:text-text-secondary hover:bg-surface-highlight disabled:opacity-50 disabled:hover:bg-transparent"
            >
              Clear
            </button>
          </div>
        </Popover>
      </div>

      <div className="flex-1" />

      {active && (
        <span role="status" className="flex-shrink-0 font-mono text-xs text-text-muted">
          {matchedIds.length} of {rows.length}
        </span>
      )}

      <div role="group" aria-label="View mode" className="flex flex-shrink-0 items-center gap-1">
        {VIEW_OPTIONS.map(({ key, label, icon: Icon }) => {
          const disabled = key === 'table' && lensEnabled;
          return (
            <button
              key={key}
              type="button"
              aria-pressed={view === key}
              aria-label={label}
              disabled={disabled}
              title={disabled ? 'Close Access Lens to use the table view' : label}
              onClick={() => setStackPrefs({ view: key })}
              className={cn(
                'inline-flex h-7 w-7 items-center justify-center rounded-md border transition-colors disabled:cursor-not-allowed disabled:opacity-50',
                view === key ? BUTTON_ON : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight border-transparent',
              )}
            >
              <Icon size={14} aria-hidden="true" />
            </button>
          );
        })}
      </div>
    </div>
  );
}
