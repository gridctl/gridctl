import { useContext, useEffect, useRef, useState } from 'react';
import {
  UNSAFE_NavigationContext,
  useHref,
  useLocation,
  useSearchParams,
  type Location,
} from 'react-router';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import { showToast } from '../components/ui/Toast';
import { GATEWAY_NODE_ID } from '../lib/graph/edges';

// How long a well-formed ?node= id may stay absent after the gateway first
// responds before the link is dropped. Client nodes arrive after the first
// status, so absence at the first status is not proof of a miss.
export const NODE_PARAM_GRACE_MS = 5000;

const NODE_PARAM = 'node';
const MAX_NODE_ID_LENGTH = 256;
const SELECTABLE_PREFIXES = ['mcp-', 'resource-', 'client-', 'skill-group-'];

export function isSelectableNodeId(value: string): boolean {
  if (value.length > MAX_NODE_ID_LENGTH) return false;
  if (value === GATEWAY_NODE_ID) return true;
  return SELECTABLE_PREFIXES.some((prefix) => value.length > prefix.length && value.startsWith(prefix));
}

export interface NodeParamRun {
  raw: string | null;
  selected: string | null;
  exists: boolean;
}

export type NodeParamAction =
  | { kind: 'none' }
  | { kind: 'apply'; id: string }
  | { kind: 'write' }
  | { kind: 'pend'; id: string }
  | { kind: 'reject'; value: string };

// Decides one reconciliation run between the ?node= parameter and the store
// selection. `last` holds the inputs of the previous run (null before the
// first); `pending` is a valid id waiting for its node to appear. Returns the
// action and the pending id after this run. "write" means: set ?node= from the
// live store selection, or remove it.
export function decideNodeParam(
  current: NodeParamRun,
  last: NodeParamRun | null,
  pending: string | null,
): { action: NodeParamAction; pending: string | null } {
  const { raw, selected, exists } = current;
  if (last && raw === last.raw && selected === last.selected && exists === last.exists) {
    return { action: { kind: 'none' }, pending };
  }
  const param = raw || null;
  const paramChanged = !last || raw !== last.raw;
  const selectedChanged = last !== null && selected !== last.selected;
  if (paramChanged) pending = null;

  if (selectedChanged) {
    return { action: raw !== selected ? { kind: 'write' } : { kind: 'none' }, pending: null };
  }
  if (paramChanged && param !== null && param !== selected) {
    if (!isSelectableNodeId(param)) return { action: { kind: 'reject', value: param }, pending: null };
    if (exists) return { action: { kind: 'apply', id: param }, pending: null };
    return { action: { kind: 'pend', id: param }, pending: param };
  }
  if (pending !== null && pending === param && exists) {
    return { action: { kind: 'apply', id: pending }, pending: null };
  }
  if (pending === null && raw !== selected) {
    return { action: { kind: 'write' }, pending: null };
  }
  return { action: { kind: 'none' }, pending };
}

function missToast(value: string): void {
  showToast('error', `Node "${value.slice(0, 80)}" not found`);
}

type SetSearchParams = ReturnType<typeof useSearchParams>[1];

// One replace write: ?node= becomes the live store selection, or is removed.
// Skipped once the router has moved to another path: a workspace change can
// stay pending while its lazy chunk loads, and a write relative to the
// rendered /stack location would replace that navigation's history entry.
function writeFromStore(setSearchParams: SetSearchParams, stillHere: () => boolean): void {
  if (!stillHere()) return;
  setSearchParams(
    (prev) => {
      const next = new URLSearchParams(prev);
      const selected = useStackStore.getState().selectedNodeId;
      if (selected) next.set(NODE_PARAM, selected);
      else next.delete(NODE_PARAM);
      return next;
    },
    { replace: true },
  );
}

// Keeps ?node= on the Stack workspace in step with the store selection. Called
// once from StackWorkspace; does nothing while unmounted.
export function useNodeParamSync(): void {
  const [searchParams, setSearchParams] = useSearchParams();
  const raw = searchParams.get(NODE_PARAM);
  const param = raw || null;

  const selectedNodeId = useStackStore((s) => s.selectedNodeId);
  const paramNodeExists = useStackStore((s) => param !== null && s.nodes.some((n) => n.id === param));
  const hasGateway = useStackStore((s) => s.gatewayInfo !== null);

  // Declarative routers pass their history as the navigator; its location
  // moves at once on navigate, before the transition renders it.
  const { navigator } = useContext(UNSAFE_NavigationContext);
  const renderedPath = useHref({ pathname: useLocation().pathname });

  const setParamsRef = useRef(setSearchParams);
  const rawRef = useRef(raw);
  const stillHereRef = useRef(() => true);
  useEffect(() => {
    setParamsRef.current = setSearchParams;
    rawRef.current = raw;
    stillHereRef.current = () => {
      const live = (navigator as { location?: Location }).location;
      return !live || live.pathname === renderedPath;
    };
  });

  const lastRef = useRef<NodeParamRun | null>(null);
  const pendingRef = useRef<string | null>(null);
  const [pending, setPending] = useState<string | null>(null);

  useEffect(() => {
    const selected = useStackStore.getState().selectedNodeId;
    const { action, pending: nextPending } = decideNodeParam(
      { raw, selected, exists: paramNodeExists },
      lastRef.current,
      pendingRef.current,
    );
    switch (action.kind) {
      case 'apply':
        useStackStore.getState().selectNode(action.id);
        useUIStore.getState().setSidebarOpen(true);
        break;
      case 'reject':
        missToast(action.value);
        writeFromStore(setParamsRef.current, stillHereRef.current);
        break;
      case 'write':
        writeFromStore(setParamsRef.current, stillHereRef.current);
        break;
      default:
        break;
    }
    if (nextPending !== pendingRef.current) {
      pendingRef.current = nextPending;
      setPending(nextPending);
    }
    lastRef.current = {
      raw,
      selected: useStackStore.getState().selectedNodeId,
      exists: paramNodeExists,
    };
  }, [raw, selectedNodeId, paramNodeExists]);

  // The grace period starts only once the gateway has answered, so a slow
  // first connection does not drop a valid link.
  useEffect(() => {
    if (pending === null || !hasGateway) return;
    const timer = setTimeout(() => {
      if (pendingRef.current !== pending || (rawRef.current || null) !== pending) return;
      if (useStackStore.getState().nodes.some((n) => n.id === pending)) return;
      pendingRef.current = null;
      setPending(null);
      missToast(pending);
      writeFromStore(setParamsRef.current, stillHereRef.current);
    }, NODE_PARAM_GRACE_MS);
    return () => clearTimeout(timer);
  }, [pending, hasGateway]);
}
