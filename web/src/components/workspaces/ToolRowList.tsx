import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type RefObject } from 'react';
import { useCommandState } from 'cmdk';
import { defaultRangeExtractor, useVirtualizer, type Range } from '@tanstack/react-virtual';
import { cn } from '../../lib/cn';
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import { policyChip, policyDetailText } from '../../lib/groups';
import { formatLastUsed, type AuditState } from '../../lib/toolAudit';
import type { PolicyToolStatus } from '../../lib/api';
import type { ToolRow } from '../../hooks/useToolsEditor';
import type { ToolUsageStat } from '../../types';
import { joinToolGroups, ToolListRow } from './ToolListRow';

// Windowed rendering kicks in above this row count, the same threshold the
// Logs stream uses.
const VIRTUALIZE_AT = 200;
const ESTIMATED_ROW_HEIGHT = 48;
// Used until the list element is measured, so the first render never mounts
// zero rows (cmdk would show its empty message).
const INITIAL_RECT = { width: 0, height: 480 };

interface ToolRowListProps {
  serverName: string;
  shown: ToolRow[];
  selected: Set<string>;
  selectedTool: string | null;
  auditByTool: Map<string, AuditState>;
  usage?: Record<string, ToolUsageStat>;
  groupsFor: (tool: string) => string[];
  policyTools?: Record<string, PolicyToolStatus>;
  onSelect: (name: string) => void;
  onToggle: (name: string) => void;
  /**
   * The Command.List element: the scroll container in virtual mode. Passed as
   * state, not a ref: the list's ref attaches after this child's layout
   * effects, so a ref would leave the virtualizer without a scroll element.
   */
  listElement: HTMLDivElement | null;
  /** Set by the Command keydown handler when a cursor key is pressed on an option. */
  keyNavRef: RefObject<boolean>;
}

// ToolRowList renders the cmdk items for one server. It sits inside <Command>
// so it can read cmdk's cursor, which drives the row tab stop, focus-follow for
// keyboard moves, and the pinned rows of the virtual window.
export function ToolRowList({
  serverName,
  shown,
  selected,
  selectedTool,
  auditByTool,
  usage,
  groupsFor,
  policyTools,
  onSelect,
  onToggle,
  listElement,
  keyNavRef,
}: ToolRowListProps) {
  const cursor = useCommandState((s) => s.value);

  const rowRefs = useRef<Map<string, HTMLDivElement>>(new Map());
  // One ref callback for the list. A fresh callback per row would both defeat
  // memo and re-register the node on every parent render.
  const registerRef = useCallback((name: string, el: HTMLDivElement | null) => {
    if (el) rowRefs.current.set(name, el);
    else rowRefs.current.delete(name);
  }, []);

  const indexByName = useMemo(() => {
    const map = new Map<string, number>();
    shown.forEach((row, i) => map.set(row.name, i));
    return map;
  }, [shown]);
  const cursorIndex = cursor ? (indexByName.get(cursor) ?? -1) : -1;

  // A hover moves the cursor without moving focus, so the focused row is
  // tracked separately and pinned too; otherwise scrolling would unmount it.
  const [focusedName, setFocusedName] = useState<string | null>(null);
  useEffect(() => {
    if (!listElement) return;
    // closest(): a click-focused checkbox sits inside its option.
    const itemName = (t: EventTarget | null) =>
      t instanceof Element ? (t.closest('[cmdk-item]')?.getAttribute('data-value') ?? null) : null;
    const onFocusIn = (e: FocusEvent) => setFocusedName(itemName(e.target));
    const onFocusOut = (e: FocusEvent) => setFocusedName(itemName(e.relatedTarget));
    listElement.addEventListener('focusin', onFocusIn);
    listElement.addEventListener('focusout', onFocusOut);
    return () => {
      listElement.removeEventListener('focusin', onFocusIn);
      listElement.removeEventListener('focusout', onFocusOut);
    };
  }, [listElement]);
  const focusedIndex = focusedName ? (indexByName.get(focusedName) ?? -1) : -1;

  const virtualize = shown.length > VIRTUALIZE_AT;
  // cmdk steps through rendered items in DOM order and resets its cursor when
  // the selected item unmounts. Keeping the ends and the cursor's neighbors
  // mounted keeps arrows, Home, and End correct; the focused row stays too.
  const rangeExtractor = useCallback(
    (range: Range) => {
      const indexes = new Set(defaultRangeExtractor(range));
      const pinned = [0, range.count - 1];
      if (cursorIndex >= 0) pinned.push(cursorIndex - 1, cursorIndex, cursorIndex + 1);
      if (focusedIndex >= 0) pinned.push(focusedIndex);
      for (const i of pinned) if (i >= 0 && i < range.count) indexes.add(i);
      return [...indexes].sort((a, b) => a - b);
    },
    [cursorIndex, focusedIndex],
  );
  const virtualizer = useVirtualizer({
    count: virtualize ? shown.length : 0,
    getScrollElement: () => listElement,
    estimateSize: () => ESTIMATED_ROW_HEIGHT,
    overscan: 8,
    getItemKey: (index) => shown[index]?.name ?? index,
    rangeExtractor,
    initialRect: INITIAL_RECT,
  });
  const virtualizerRef = useRef(virtualizer);
  virtualizerRef.current = virtualizer;
  const indexByNameRef = useRef(indexByName);
  indexByNameRef.current = indexByName;

  // Focus follows the cursor only after a keyboard move from a focused row;
  // a hover also moves the cursor and must not steal focus.
  useLayoutEffect(() => {
    if (!keyNavRef.current) return;
    keyNavRef.current = false;
    if (!cursor) return;
    rowRefs.current.get(cursor)?.querySelector<HTMLElement>('[cmdk-item]')?.focus();
  }, [cursor, keyNavRef]);

  // Scroll the selected tool into view (e.g. after a global-search reveal).
  // `block: 'nearest'` no-ops when the row is already visible, so clicking a
  // visible row to select it never jumps the list. In virtual mode the row may
  // not be mounted, so the virtualizer scrolls to its index instead.
  // Each selection is revealed once, so a filter that crosses the threshold
  // does not scroll back to an unchanged selection. The extra dependencies only
  // let a reveal that could not run yet (no list element in virtual mode) run
  // once it can.
  const revealedRef = useRef<string | null>(null);
  useEffect(() => {
    if (!selectedTool) {
      revealedRef.current = null;
      return;
    }
    if (revealedRef.current === selectedTool) return;
    if (virtualize) {
      if (!listElement) return;
      const index = indexByNameRef.current.get(selectedTool);
      if (index === undefined) return;
      virtualizerRef.current.scrollToIndex(index, { align: 'auto' });
      revealedRef.current = selectedTool;
      return;
    }
    const row = rowRefs.current.get(selectedTool);
    if (!row) return;
    // Optional-chain: scrollIntoView is absent in jsdom and older embeds.
    row.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' });
    revealedRef.current = selectedTool;
  }, [selectedTool, virtualize, listElement]);

  const renderRow = (opt: ToolRow, index?: number) => {
    const auditState = auditByTool.get(opt.name) ?? null;
    const verdict = policyTools?.[`${serverName}${TOOL_NAME_DELIMITER}${opt.name}`];
    const chip = policyChip(verdict);
    return (
      <ToolListRow
        key={opt.name}
        opt={opt}
        groups={joinToolGroups(groupsFor(opt.name))}
        lastUsed={
          auditState && auditState !== 'disabled'
            ? formatLastUsed(usage?.[opt.name]?.lastCalledAt)
            : ''
        }
        isEnabled={selected.has(opt.name)}
        isActive={selectedTool === opt.name}
        isCursor={cursor === opt.name}
        auditState={auditState}
        policyLabel={chip?.label ?? ''}
        policyTitle={chip?.title ?? ''}
        policyTone={chip?.tone ?? ''}
        policyText={chip ? (policyDetailText(verdict) ?? '') : ''}
        setSize={index === undefined ? undefined : shown.length}
        posInSet={index === undefined ? undefined : index + 1}
        onSelect={onSelect}
        onToggle={onToggle}
        registerRef={registerRef}
      />
    );
  };

  if (!virtualize) return <>{shown.map((opt) => renderRow(opt))}</>;

  // Each row is its wrapper's last child here, so the row's own
  // last:border-b-0 always applies; the divider moves to the wrapper.
  return (
    <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
      {virtualizer.getVirtualItems().map((item) => (
        <div
          key={item.key}
          data-index={item.index}
          ref={virtualizer.measureElement}
          className={cn(
            'absolute top-0 left-0 w-full',
            item.index < shown.length - 1 && 'border-b border-border/20',
          )}
          style={{ transform: `translateY(${item.start}px)` }}
        >
          {renderRow(shown[item.index], item.index)}
        </div>
      ))}
    </div>
  );
}
