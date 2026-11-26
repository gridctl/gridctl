import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type Ref,
} from 'react';
import { cn } from '../../lib/cn';
import { GATEWAY_NODE_ID } from '../../lib/graph/edges';
import {
  ROW_KIND_LABELS,
  ROW_PIN_LABELS,
  ROW_STATE_LABELS,
  matchesFilter,
  sortStackRows,
  type StackFilter,
  type StackRow,
  type StackSortDirection,
  type StackSortKey,
} from '../../lib/stackRows';
import { useStackRows } from '../../hooks/useStackRows';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { requestInspectorLeave } from '../../stores/useInspectorToolsGuardStore';
import { StatusDot } from '../ui/StatusDot';
import { EmptyState } from '../ui/EmptyState';
import type { NodeStatus } from '../../types';

export interface StackTableHandle {
  /** Move focus to the active row's name button. */
  focusActiveRow: () => void;
}

interface StackTableProps {
  filter: StackFilter;
  onClearFilter: () => void;
  ref?: Ref<StackTableHandle>;
}

const COLUMNS: { key: StackSortKey; label: string; align?: 'right' }[] = [
  { key: 'name', label: 'Name' },
  { key: 'kind', label: 'Kind' },
  { key: 'status', label: 'Status' },
  { key: 'transport', label: 'Transport' },
  { key: 'tools', label: 'Tools', align: 'right' },
  { key: 'replicas', label: 'Replicas', align: 'right' },
  { key: 'pin', label: 'Pin' },
];

const PAGE_STEP = 10;
const EN_DASH = '\u2013';
const ROW_ID_ATTR = 'data-stack-row-id';

function dotProps(row: StackRow): { status: NodeStatus; label: string } {
  if (row.state === 'linked') return { status: 'running', label: 'linked' };
  return { status: row.state, label: ROW_STATE_LABELS[row.state].toLowerCase() };
}

function selectRow(row: StackRow) {
  requestInspectorLeave(
    () => {
      useStackStore.getState().selectNode(row.id);
      useUIStore.getState().setSidebarOpen(true);
    },
    { targetServer: row.kind === 'mcp-server' ? row.name : undefined },
  );
}

// The row id of the name button that holds focus, read before this render
// commits. A poll that re-sorts rows can move the focused row's DOM node,
// which drops focus; the layout effect below puts it back on the same row.
function focusedRowId(): string | null {
  if (typeof document === 'undefined') return null;
  const el = document.activeElement;
  return el instanceof HTMLElement ? el.getAttribute(ROW_ID_ATTR) : null;
}

/**
 * Linear view of the Stack: one row per MCP server, resource, and linked
 * client. Rows select the same node id as the graph, so the inspector is
 * shared. The name buttons form one roving tab stop.
 */
export function StackTable({ filter, onClearFilter, ref }: StackTableProps) {
  const rows = useStackRows();
  const selectedNodeId = useStackStore((s) => s.selectedNodeId);
  const gatewayInfo = useStackStore((s) => s.gatewayInfo);
  const sidebarOpen = useUIStore((s) => s.sidebarOpen);

  const [sortKey, setSortKey] = useState<StackSortKey>('status');
  const [sortDir, setSortDir] = useState<StackSortDirection>('asc');
  const [activeId, setActiveId] = useState<string | null>(null);

  const visible = useMemo(
    () => sortStackRows(rows.filter((row) => matchesFilter(row, filter)), sortKey, sortDir),
    [rows, filter, sortKey, sortDir],
  );

  const visibleIds = useMemo(() => new Set(visible.map((row) => row.id)), [visible]);
  const effectiveActiveId =
    activeId !== null && visibleIds.has(activeId)
      ? activeId
      : selectedNodeId !== null && visibleIds.has(selectedNodeId)
        ? selectedNodeId
        : (visible[0]?.id ?? null);

  const buttonsRef = useRef(new Map<string, HTMLButtonElement>());
  const rowsRef = useRef(new Map<string, HTMLTableRowElement>());

  useImperativeHandle(ref, () => ({
    focusActiveRow: () => {
      if (effectiveActiveId) buttonsRef.current.get(effectiveActiveId)?.focus();
    },
  }), [effectiveActiveId]);

  const heldFocusId = focusedRowId();
  useLayoutEffect(() => {
    if (!heldFocusId) return;
    const button = buttonsRef.current.get(heldFocusId);
    if (button && document.activeElement !== button) button.focus({ preventScroll: true });
  }, [heldFocusId, visible]);

  // A selection from outside the table (palette, graph, toasts) becomes the
  // active row and is scrolled into view; focus stays where it is.
  // Adjusted during render, not in an effect, so the tab stop moves in the
  // same commit as the selection.
  const [prevSelected, setPrevSelected] = useState(selectedNodeId);
  const [scrollTarget, setScrollTarget] = useState<{ id: string } | null>(null);
  if (prevSelected !== selectedNodeId) {
    setPrevSelected(selectedNodeId);
    if (selectedNodeId !== null && visibleIds.has(selectedNodeId) && selectedNodeId !== activeId) {
      setActiveId(selectedNodeId);
      setScrollTarget({ id: selectedNodeId });
    }
  }
  useEffect(() => {
    if (scrollTarget) rowsRef.current.get(scrollTarget.id)?.scrollIntoView?.({ block: 'nearest' });
  }, [scrollTarget]);

  const onSort = useCallback((key: StackSortKey) => {
    if (key === sortKey) {
      setSortDir((dir) => (dir === 'asc' ? 'desc' : 'asc'));
      return;
    }
    setSortKey(key);
    setSortDir('asc');
  }, [sortKey]);

  const onBodyKeyDown = useCallback((event: KeyboardEvent<HTMLTableSectionElement>) => {
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
    const target = event.target;
    if (!(target instanceof HTMLElement)) return;
    const id = target.getAttribute(ROW_ID_ATTR);
    if (!id) return;
    const index = visible.findIndex((row) => row.id === id);
    if (index === -1) return;
    const last = visible.length - 1;
    let next: number;
    switch (event.key) {
      case 'ArrowDown':
      case 'j':
        next = Math.min(last, index + 1);
        break;
      case 'ArrowUp':
      case 'k':
        next = Math.max(0, index - 1);
        break;
      case 'Home':
        next = 0;
        break;
      case 'End':
        next = last;
        break;
      case 'PageDown':
        next = Math.min(last, index + PAGE_STEP);
        break;
      case 'PageUp':
        next = Math.max(0, index - PAGE_STEP);
        break;
      default:
        return;
    }
    event.preventDefault();
    const row = visible[next];
    setActiveId(row.id);
    buttonsRef.current.get(row.id)?.focus();
    if (next !== index && sidebarOpen && selectedNodeId !== null) selectRow(row);
  }, [visible, sidebarOpen, selectedNodeId]);

  const counts = useMemo(() => {
    const c = { 'mcp-server': 0, resource: 0, client: 0 };
    for (const row of rows) c[row.kind] += 1;
    return c;
  }, [rows]);

  const gatewaySelected = selectedNodeId === GATEWAY_NODE_ID;

  return (
    <div className="absolute inset-0 flex flex-col bg-background">
      <div className="flex flex-shrink-0 items-center gap-3 border-b border-border-subtle px-3 py-2 text-xs text-text-muted">
        {gatewayInfo && (
          <span className="min-w-0 truncate">
            <span className="text-text-secondary font-medium">{gatewayInfo.name}</span>{' '}
            <span className="font-mono">{gatewayInfo.version}</span>
          </span>
        )}
        <span className="font-mono">
          {counts['mcp-server']} {counts['mcp-server'] === 1 ? 'MCP server' : 'MCP servers'}
          {' · '}
          {counts.resource} {counts.resource === 1 ? 'resource' : 'resources'}
          {' · '}
          {counts.client} {counts.client === 1 ? 'client' : 'clients'}
        </span>
        <button
          type="button"
          aria-pressed={gatewaySelected}
          onClick={() =>
            requestInspectorLeave(() => {
              useStackStore.getState().selectNode(GATEWAY_NODE_ID);
              useUIStore.getState().setSidebarOpen(true);
            })
          }
          className={cn(
            'ml-auto inline-flex h-7 flex-shrink-0 items-center rounded-md border px-2.5 text-xs font-medium transition-colors',
            gatewaySelected
              ? 'bg-primary/10 text-primary border-primary/25'
              : 'text-text-secondary border-border-subtle hover:text-text-primary hover:bg-surface-highlight',
          )}
        >
          Inspect gateway
        </button>
      </div>

      {rows.length === 0 ? (
        <EmptyState inline title="No MCP servers, resources, or clients" />
      ) : visible.length === 0 ? (
        <EmptyState
          inline
          title="No matches"
          action={
            <button
              type="button"
              onClick={onClearFilter}
              className="inline-flex h-7 items-center rounded-md border border-border-subtle px-2.5 text-xs font-medium text-text-secondary hover:text-text-primary hover:bg-surface-highlight transition-colors"
            >
              Clear filters
            </button>
          }
        />
      ) : (
        <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark">
          <table className="w-full table-fixed border-collapse text-sm">
            <caption className="sr-only">Stack contents</caption>
            <colgroup>
              <col className="w-[28%]" />
              <col className="w-[12%]" />
              <col className="w-[16%]" />
              <col className="w-[11%]" />
              <col className="w-[9%]" />
              <col className="w-[10%]" />
              <col className="w-[14%]" />
            </colgroup>
            <thead>
              <tr>
                {COLUMNS.map((col) => {
                  const active = col.key === sortKey;
                  return (
                    <th
                      key={col.key}
                      scope="col"
                      aria-sort={active ? (sortDir === 'asc' ? 'ascending' : 'descending') : undefined}
                      className={cn(
                        'sticky top-0 z-sticky bg-surface border-b border-border px-3 py-1.5 text-left text-xs font-medium',
                        col.align === 'right' && 'text-right',
                      )}
                    >
                      <button
                        type="button"
                        onClick={() => onSort(col.key)}
                        className={cn(
                          'inline-flex items-center gap-1 rounded-sm transition-colors',
                          active ? 'text-primary' : 'text-text-muted hover:text-text-secondary',
                        )}
                      >
                        {col.label}
                        {active && <span aria-hidden="true">{sortDir === 'asc' ? '\u2191' : '\u2193'}</span>}
                      </button>
                    </th>
                  );
                })}
              </tr>
            </thead>
            <tbody onKeyDown={onBodyKeyDown}>
              {visible.map((row) => {
                const selected = row.id === selectedNodeId;
                const dot = dotProps(row);
                return (
                  <tr
                    key={row.id}
                    ref={(el) => {
                      if (el) rowsRef.current.set(row.id, el);
                      else rowsRef.current.delete(row.id);
                    }}
                    aria-current={selected ? 'true' : undefined}
                    onClick={() => {
                      setActiveId(row.id);
                      selectRow(row);
                    }}
                    className={cn(
                      'cursor-pointer border-b border-border-subtle transition-colors',
                      selected ? 'bg-primary/[0.06]' : 'hover:bg-surface-highlight/40',
                    )}
                  >
                    <td className="px-3 py-1.5">
                      <button
                        type="button"
                        ref={(el) => {
                          if (el) buttonsRef.current.set(row.id, el);
                          else buttonsRef.current.delete(row.id);
                        }}
                        {...{ [ROW_ID_ATTR]: row.id }}
                        tabIndex={row.id === effectiveActiveId ? 0 : -1}
                        title={row.name}
                        className={cn(
                          'block max-w-full truncate rounded-sm text-left font-medium',
                          selected ? 'text-primary' : 'text-text-primary',
                        )}
                      >
                        {row.name}
                      </button>
                    </td>
                    <td className="px-3 py-1.5 text-text-secondary">{ROW_KIND_LABELS[row.kind]}</td>
                    <td className="px-3 py-1.5">
                      <span className="inline-flex items-center gap-1.5 text-text-secondary">
                        <span aria-hidden="true" className="inline-flex">
                          <StatusDot status={dot.status} label={dot.label} pulse={false} />
                        </span>
                        {ROW_STATE_LABELS[row.state]}
                      </span>
                    </td>
                    <td className="px-3 py-1.5 font-mono text-xs text-text-secondary">
                      {row.transport ? row.transport.toUpperCase() : EN_DASH}
                    </td>
                    <td className="px-3 py-1.5 text-right font-mono text-text-secondary">
                      {row.toolCount ?? EN_DASH}
                    </td>
                    <td className="px-3 py-1.5 text-right font-mono text-text-secondary">
                      {row.replicas === null ? (
                        EN_DASH
                      ) : row.replicas.target !== undefined ? (
                        <span aria-label={`${row.replicas.current} of target ${row.replicas.target} replicas`}>
                          {row.replicas.current}/{row.replicas.target}
                        </span>
                      ) : (
                        row.replicas.current
                      )}
                    </td>
                    <td className="px-3 py-1.5 text-text-secondary">
                      {row.pin ? ROW_PIN_LABELS[row.pin] : EN_DASH}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
