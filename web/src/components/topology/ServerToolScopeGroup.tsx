import { useMemo, useState } from 'react';
import { Check, ChevronDown, ChevronRight, Minus, Search } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useFuzzySearch } from '../../hooks/useFuzzySearch';
import type { ToolMode } from '../../stores/useAccessLensStore';

// One available tool: an UNPREFIXED name plus an optional human description
// (sourced by the caller from the global tool catalog).
export interface ScopeTool {
  name: string;
  description?: string;
}

export interface ServerToolScopeGroupProps {
  serverName: string;
  availableTools: ScopeTool[];
  mode: ToolMode;
  // Currently-selected UNPREFIXED tool names (meaningful only in custom mode).
  selected: Set<string>;
  onModeChange: (mode: ToolMode) => void;
  onToggleTool: (tool: string) => void;
  // Select every currently-visible (filtered) tool.
  onSelectAll: (visible: string[]) => void;
  // Clear the currently-visible (filtered) tools from the selection.
  onClear: (visible: string[]) => void;
  disabled?: boolean;
}

// Show an in-group search once a server has more than this many tools. Kept low
// so large OpenAPI / code-mode servers are covered; tiny servers skip the noise.
const SEARCH_THRESHOLD = 6;

// ServerToolScopeGroup is the per-server tool-exposure control: an "All tools"
// (default) <-> "Custom" toggle, and in custom mode a searchable, tri-state
// checklist of the server's tools. It is deliberately store-agnostic — all draft
// state is owned by the caller — so the Tools-workspace client editor can adopt
// it later by wiring the same props to its own controller.
export function ServerToolScopeGroup({
  serverName,
  availableTools,
  mode,
  selected,
  onModeChange,
  onToggleTool,
  onSelectAll,
  onClear,
  disabled,
}: ServerToolScopeGroupProps) {
  const [query, setQuery] = useState('');
  // The tool checklist is collapsed by default so the slide-over reads as a
  // compact access summary, not a second tool dashboard. The canvas pills are
  // the primary way to pick tools; this list is the searchable fallback for
  // large servers and the keyboard path, opened on demand.
  const [expanded, setExpanded] = useState(false);
  const total = availableTools.length;
  const selectedCount = selected.size;

  const visible = useFuzzySearch(availableTools, query);
  const showSearch = mode === 'custom' && total > SEARCH_THRESHOLD;

  // Choosing "Custom" in the panel opens the list immediately so the operator
  // can pick; choosing it on the canvas (a pill click) leaves it collapsed.
  function handleMode(next: ToolMode) {
    onModeChange(next);
    if (next === 'custom') setExpanded(true);
  }

  // Tri-state parent over the VISIBLE tools: checked when all visible are
  // selected, indeterminate when only some are.
  const visibleSelected = useMemo(
    () => visible.filter((t) => selected.has(t.name)).length,
    [visible, selected],
  );
  const allVisibleSelected = visible.length > 0 && visibleSelected === visible.length;
  const someVisibleSelected = visibleSelected > 0 && !allVisibleSelected;

  const empty = mode === 'custom' && selectedCount === 0;

  function handleParentToggle() {
    if (disabled) return;
    const visibleNames = visible.map((t) => t.name);
    if (allVisibleSelected) onClear(visibleNames);
    else onSelectAll(visibleNames);
  }

  return (
    <div className="pl-6 pr-2 pb-2 pt-1 space-y-2">
      {/* All / Custom segmented toggle. "All" reads as a complete, positive
          state — never an indeterminate checkbox. */}
      <div className="flex items-center gap-2">
        <span className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">tools</span>
        <div className="ml-auto flex items-center gap-1.5">
          {mode === 'custom' && (
            <span className="font-mono text-[10px] text-primary" aria-live="polite">
              {selectedCount}/{total}
            </span>
          )}
          <div className="inline-flex rounded-md border border-border/50 overflow-hidden">
            <ModeButton active={mode === 'all'} disabled={disabled} onClick={() => handleMode('all')}>
              All
            </ModeButton>
            <ModeButton
              active={mode === 'custom'}
              disabled={disabled}
              onClick={() => handleMode('custom')}
            >
              Custom
            </ModeButton>
          </div>
        </div>
      </div>

      {mode === 'all' && (
        <p className="text-[10px] text-text-muted/70 leading-relaxed">
          All {total} tool{total === 1 ? '' : 's'} of{' '}
          <span className="font-mono text-text-secondary">{serverName}</span> are exposed.
        </p>
      )}

      {mode === 'custom' && !expanded && (
        <button
          type="button"
          onClick={() => setExpanded(true)}
          className="w-full flex items-center gap-1.5 text-left text-[10px] text-text-muted/80 hover:text-text-secondary transition-colors"
        >
          <ChevronRight size={11} aria-hidden="true" />
          <span>
            {selectedCount} of {total} tools selected
            {empty && <span className="text-status-pending"> — none yet</span>}
          </span>
          <span className="ml-auto text-secondary">Edit tools</span>
        </button>
      )}

      {mode === 'custom' && expanded && (
        <div className="space-y-1.5">
          <button
            type="button"
            onClick={() => setExpanded(false)}
            className="w-full flex items-center gap-1.5 text-left text-[10px] text-text-muted/80 hover:text-text-secondary transition-colors"
          >
            <ChevronDown size={11} aria-hidden="true" />
            <span>{selectedCount} of {total} tools selected</span>
            <span className="ml-auto text-secondary">Collapse</span>
          </button>

          {showSearch && (
            <div className="relative">
              <Search
                size={11}
                className="absolute left-2 top-1/2 -translate-y-1/2 text-text-muted/60"
                aria-hidden="true"
              />
              <input
                type="text"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={`Filter ${total} tools…`}
                aria-label={`Filter ${serverName} tools`}
                disabled={disabled}
                className="w-full rounded-md border border-border/50 bg-background/70 pl-6 pr-2 py-1 text-[11px] text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50"
              />
            </div>
          )}

          {/* Tri-state select-all parent over the visible tools. */}
          <button
            type="button"
            role="checkbox"
            aria-checked={allVisibleSelected ? 'true' : someVisibleSelected ? 'mixed' : 'false'}
            onClick={handleParentToggle}
            disabled={disabled || visible.length === 0}
            className="w-full flex items-center gap-2 px-1 py-1 text-left hover:bg-surface-highlight/30 rounded transition-colors disabled:opacity-50"
          >
            <TriStateBox checked={allVisibleSelected} indeterminate={someVisibleSelected} />
            <span className="text-[10px] text-text-muted">
              {allVisibleSelected ? 'Clear all' : 'Select all'}
              {query.trim() && ` (${visible.length} shown)`}
            </span>
          </button>

          <div className="rounded-md border border-border/30 bg-background/40 max-h-56 overflow-y-auto scrollbar-dark divide-y divide-border/15">
            {visible.length === 0 && (
              <p className="px-3 py-3 text-[10px] text-text-muted/60 italic text-center">
                No tools match &ldquo;{query}&rdquo;.
              </p>
            )}
            {visible.map((tool) => {
              const isOn = selected.has(tool.name);
              return (
                <button
                  key={tool.name}
                  type="button"
                  role="checkbox"
                  aria-checked={isOn}
                  onClick={() => onToggleTool(tool.name)}
                  disabled={disabled}
                  title={tool.description}
                  className="w-full flex items-start gap-2 px-2.5 py-1.5 text-left hover:bg-surface-highlight/40 transition-colors disabled:opacity-60"
                >
                  <span
                    className={cn(
                      'mt-0.5 w-3.5 h-3.5 rounded border flex items-center justify-center flex-shrink-0 transition-colors',
                      isOn ? 'bg-white/15 border-white/70' : 'border-border/60 bg-background/50',
                    )}
                  >
                    {isOn && <Check size={9} className="text-white" aria-hidden="true" />}
                  </span>
                  <span className="min-w-0 flex-1">
                    <span
                      className={cn(
                        'block text-[11px] font-mono truncate',
                        isOn ? 'text-text-primary' : 'text-text-secondary',
                      )}
                    >
                      {tool.name}
                    </span>
                    {tool.description && (
                      <span className="block text-[10px] text-text-muted/70 truncate leading-snug">
                        {tool.description}
                      </span>
                    )}
                  </span>
                </button>
              );
            })}
          </div>

          {empty && (
            <p className="text-[10px] text-status-pending" role="status">
              Select at least one tool, or switch back to All. An empty custom list cannot be saved.
            </p>
          )}
        </div>
      )}
    </div>
  );
}

function ModeButton({
  active,
  disabled,
  onClick,
  children,
}: {
  active: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-pressed={active}
      className={cn(
        'px-2 py-0.5 text-[10px] font-medium transition-colors disabled:opacity-50',
        active
          ? 'bg-primary/20 text-primary'
          : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight/40',
      )}
    >
      {children}
    </button>
  );
}

function TriStateBox({ checked, indeterminate }: { checked: boolean; indeterminate: boolean }) {
  return (
    <span
      className={cn(
        'w-3.5 h-3.5 rounded border flex items-center justify-center flex-shrink-0 transition-colors',
        checked || indeterminate ? 'bg-white/15 border-white/70' : 'border-border/60 bg-background/50',
      )}
    >
      {checked && <Check size={9} className="text-white" aria-hidden="true" />}
      {indeterminate && !checked && <Minus size={9} className="text-white" aria-hidden="true" />}
    </span>
  );
}

export default ServerToolScopeGroup;
