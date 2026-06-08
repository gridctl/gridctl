import { useCallback, useEffect, useId, useMemo, useRef, useState } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { X } from 'lucide-react';
import { cn } from '../../lib/cn';
import { usePricingModels } from '../../hooks/usePricingModels';
import { UNKNOWN_MODEL_NOTE } from './constants';

// Above this many visible rows the option list renders through
// @tanstack/react-virtual. The unfiltered LiteLLM snapshot is ~2,700 IDs —
// past cmdk's documented unvirtualized ceiling — but a few keystrokes of
// filtering lands well under the threshold, where plain rendering keeps
// jsdom-testable, zero-overhead behavior.
const VIRTUALIZE_AT = 150;

const ITEM_HEIGHT = 26;
const HEADER_HEIGHT = 22;

// The option list groups model IDs by provider. Provider-prefixed IDs
// ("anthropic/claude-...") group under their prefix; bare IDs ("gpt-4o",
// "claude-opus-4-7") form the leading "models" group, which carries the
// flagship aliases users reach for most.
type Row =
  | { kind: 'header'; label: string }
  | { kind: 'item'; id: string };

function buildRows(models: string[], query: string): Row[] {
  const q = query.trim().toLowerCase();
  const filtered = q === '' ? models : models.filter((m) => m.toLowerCase().includes(q));

  const bare: string[] = [];
  const byProvider = new Map<string, string[]>();
  for (const id of filtered) {
    const slash = id.indexOf('/');
    if (slash <= 0) {
      bare.push(id);
    } else {
      const provider = id.slice(0, slash);
      const group = byProvider.get(provider);
      if (group) group.push(id);
      else byProvider.set(provider, [id]);
    }
  }

  const rows: Row[] = [];
  if (bare.length > 0) {
    rows.push({ kind: 'header', label: 'models' });
    for (const id of bare) rows.push({ kind: 'item', id });
  }
  for (const provider of [...byProvider.keys()].sort()) {
    rows.push({ kind: 'header', label: provider });
    for (const id of byProvider.get(provider)!) rows.push({ kind: 'item', id });
  }
  return rows;
}

export interface ModelPickerProps {
  /** The currently saved model ID; empty string means none declared. */
  value: string;
  /** Called with the chosen ID on Enter or option click; '' clears. */
  onCommit: (model: string) => void;
  /** Called on Escape. The parent usually closes the editor. */
  onCancel?: () => void;
  placeholder?: string;
  disabled?: boolean;
  autoFocus?: boolean;
  /** Width class for the input; defaults to the metrics-cell width. */
  widthClass?: string;
  /** Non-null renders the error state (red border + title). */
  error?: string | null;
  /**
   * Commit the draft when focus leaves the input (form-field semantics, used
   * by the wizard). Inline cells keep the default Enter-to-save semantics.
   */
  commitOnBlur?: boolean;
}

/**
 * ModelPicker is the shared combobox over the pricing snapshot's canonical
 * model IDs (GET /api/pricing/models): live substring filtering, provider
 * grouping, full keyboard control (arrows, Enter, Escape), and free-text
 * pass-through — unknown IDs are valid everywhere, they just price as $0.
 * The list virtualizes above VIRTUALIZE_AT rows so the full ~2,700-ID
 * snapshot stays responsive.
 */
export function ModelPicker({
  value,
  onCommit,
  onCancel,
  placeholder = 'claude-opus-4-7',
  disabled = false,
  autoFocus = false,
  widthClass = 'w-52',
  error = null,
  commitOnBlur = false,
}: ModelPickerProps) {
  const models = usePricingModels();
  const [draft, setDraft] = useState(value);
  const [open, setOpen] = useState(autoFocus);
  const [active, setActive] = useState(-1);
  const listId = useId();
  const scrollRef = useRef<HTMLDivElement>(null);

  const rows = useMemo(() => buildRows(models, draft), [models, draft]);
  const knownSet = useMemo(() => new Set(models), [models]);
  const isUnknown = draft.trim() !== '' && models.length > 0 && !knownSet.has(draft.trim());

  // Full ID of the highlighted row, shown verbatim in the footer so an ID
  // wider than the popover stays readable during keyboard navigation.
  const activeRow = open && active >= 0 ? rows[active] : undefined;
  const activeId = activeRow?.kind === 'item' ? activeRow.id : '';

  const virtualize = rows.length > VIRTUALIZE_AT;
  const virtualizer = useVirtualizer({
    count: virtualize ? rows.length : 0,
    getScrollElement: () => scrollRef.current,
    estimateSize: (index) => (rows[index]?.kind === 'header' ? HEADER_HEIGHT : ITEM_HEIGHT),
    overscan: 8,
  });

  // Clamp the active row when filtering shrinks the list.
  useEffect(() => {
    if (active >= rows.length) setActive(-1);
  }, [rows.length, active]);

  const move = useCallback(
    (dir: 1 | -1) => {
      if (rows.length === 0) return;
      setOpen(true);
      let next = active;
      for (let step = 0; step < rows.length; step++) {
        next += dir;
        if (next < 0) next = rows.length - 1;
        if (next >= rows.length) next = 0;
        if (rows[next].kind === 'item') break;
      }
      setActive(next);
      if (virtualize) {
        virtualizer.scrollToIndex(next);
      } else {
        document.getElementById(`${listId}-row-${next}`)?.scrollIntoView({ block: 'nearest' });
      }
    },
    [rows, active, virtualize, virtualizer, listId],
  );

  const commit = useCallback(
    (model: string) => {
      setOpen(false);
      onCommit(model.trim());
    },
    [onCommit],
  );

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    switch (e.key) {
      case 'ArrowDown':
        e.preventDefault();
        move(1);
        break;
      case 'ArrowUp':
        e.preventDefault();
        move(-1);
        break;
      case 'Enter': {
        e.preventDefault();
        const activeRow = open && active >= 0 ? rows[active] : undefined;
        if (activeRow && activeRow.kind === 'item') {
          commit(activeRow.id);
        } else {
          commit(draft);
        }
        break;
      }
      case 'Escape':
        e.preventDefault();
        // Stop the keydown here so a host SlideOver's document-level Escape
        // listener does not also close the whole panel.
        e.stopPropagation();
        setOpen(false);
        onCancel?.();
        break;
      default:
        break;
    }
  };

  const renderRow = (row: Row, index: number, style?: React.CSSProperties) => {
    if (row.kind === 'header') {
      return (
        <div
          key={`h-${row.label}-${index}`}
          id={`${listId}-row-${index}`}
          style={style}
          className="px-2 pt-1.5 pb-0.5 text-[9px] uppercase tracking-wider text-text-muted/60 select-none"
        >
          {row.label}
        </div>
      );
    }
    const isActive = index === active;
    const isSelected = row.id === value;
    return (
      <div
        key={row.id}
        id={`${listId}-row-${index}`}
        role="option"
        aria-selected={isSelected}
        style={style}
        onMouseDown={(e) => {
          // Commit before the input's blur fires so the parent save flow
          // sees the click, not a cancel.
          e.preventDefault();
          commit(row.id);
        }}
        onMouseMove={() => {
          if (!isActive) setActive(index);
        }}
        className={cn(
          'px-2 py-1 text-[11px] font-mono cursor-pointer truncate',
          isActive ? 'bg-primary/[0.12] text-text-primary' : 'text-text-secondary',
          isSelected && !isActive && 'bg-primary/[0.04]',
        )}
        title={row.id}
      >
        {row.id}
      </div>
    );
  };

  return (
    <div className={cn('relative inline-block text-left', widthClass)}>
      <div className="flex items-center gap-1">
        <input
          autoFocus={autoFocus}
          role="combobox"
          aria-expanded={open}
          aria-controls={listId}
          aria-activedescendant={active >= 0 ? `${listId}-row-${active}` : undefined}
          aria-label="Pricing model"
          value={draft}
          disabled={disabled}
          placeholder={placeholder}
          onChange={(e) => {
            setDraft(e.target.value);
            setActive(-1);
            setOpen(true);
          }}
          onFocus={() => setOpen(true)}
          onBlur={() => {
            setOpen(false);
            if (commitOnBlur && draft.trim() !== value) {
              onCommit(draft.trim());
            }
          }}
          onKeyDown={handleKeyDown}
          title={error ?? 'Enter to save, Esc to cancel. Clear and save to remove.'}
          className={cn(
            'w-full rounded bg-surface-highlight/60 border px-1.5 py-0.5 text-[11px] font-mono',
            'text-text-primary placeholder:text-text-muted/50 focus:outline-none',
            error ? 'border-status-error/60' : 'border-border/50 focus:border-primary/50',
          )}
        />
        {draft !== '' && !disabled && (
          <button
            type="button"
            aria-label="Clear model"
            onMouseDown={(e) => {
              e.preventDefault();
              setDraft('');
              setActive(-1);
              setOpen(true);
            }}
            className="p-0.5 text-text-muted hover:text-text-secondary transition-colors flex-shrink-0"
          >
            <X size={10} />
          </button>
        )}
      </div>

      {open && rows.length > 0 && (
        <div
          className="absolute left-0 top-full mt-1 z-50 min-w-full w-max max-w-[min(420px,90vw)] rounded-md border border-border/50 bg-surface-elevated shadow-xl overflow-hidden"
        >
          <div
            ref={scrollRef}
            id={listId}
            role="listbox"
            aria-label="Known pricing models"
            className="overflow-y-auto scrollbar-dark"
            style={{ maxHeight: 240 }}
          >
            {virtualize ? (
              <div style={{ height: virtualizer.getTotalSize(), position: 'relative' }}>
                {virtualizer.getVirtualItems().map((v) =>
                  renderRow(rows[v.index], v.index, {
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    right: 0,
                    height: v.size,
                    transform: `translateY(${v.start}px)`,
                  }),
                )}
              </div>
            ) : (
              rows.map((row, i) => renderRow(row, i))
            )}
          </div>
          <div className="px-2 py-1 border-t border-border/30 text-[9px] text-text-muted/60 select-none">
            {activeId ? (
              <span className="font-mono text-text-secondary break-all">{activeId}</span>
            ) : models.length > 0 ? (
              `${models.length} models · LiteLLM snapshot`
            ) : (
              'Loading models…'
            )}
          </div>
        </div>
      )}

      {isUnknown && (
        <p className="mt-0.5 text-[10px] leading-snug text-status-pending">{UNKNOWN_MODEL_NOTE}</p>
      )}
    </div>
  );
}

export default ModelPicker;
