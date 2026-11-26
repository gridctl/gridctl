import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

import { SELECTION_MISSING_GRACE_MS, useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import { showToast } from '../components/ui/Toast';
import type { GatewayStatus, MCPServerStatus } from '../types';

function server(name: string): MCPServerStatus {
  return { name, transport: 'http', initialized: true, toolCount: 0, tools: [] };
}

function status(...names: string[]): GatewayStatus {
  return { gateway: { name: 'g', version: '1' }, 'mcp-servers': names.map(server) };
}

function deliver(...names: string[]) {
  useStackStore.getState().setGatewayStatus(status(...names));
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-11-26T12:00:00Z'));
  useStackStore.setState({
    selectedNodeId: null,
    missingSelection: null,
    nodes: [],
    edges: [],
    mcpServers: [],
    gatewayInfo: null,
  });
  useUIStore.setState({ sidebarOpen: false });
  vi.mocked(showToast).mockClear();
});

afterEach(() => {
  vi.useRealTimers();
});

describe('missing selection reconcile', () => {
  it('keeps a briefly absent selection and drops it after the grace period', () => {
    deliver('a', 'b');
    useStackStore.getState().selectNode('mcp-a');
    useUIStore.getState().setSidebarOpen(true);

    deliver('b');
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-a');
    expect(useStackStore.getState().missingSelection).toMatchObject({ id: 'mcp-a', name: 'a' });
    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(showToast).not.toHaveBeenCalled();

    vi.advanceTimersByTime(SELECTION_MISSING_GRACE_MS - 1000);
    deliver('a', 'b');
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-a');
    expect(useStackStore.getState().missingSelection).toBeNull();
    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(showToast).not.toHaveBeenCalled();
    const returned = useStackStore.getState().nodes.find((n) => n.id === 'mcp-a');
    expect(returned?.selected).toBe(true);
    expect(useStackStore.getState().nodes.find((n) => n.id === 'mcp-b')?.selected).toBeFalsy();

    deliver('b');
    expect(useStackStore.getState().missingSelection?.name).toBe('a');
    vi.advanceTimersByTime(SELECTION_MISSING_GRACE_MS - 1);
    deliver('b');
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-a');

    vi.advanceTimersByTime(1);
    deliver('b');
    expect(useStackStore.getState().selectedNodeId).toBeNull();
    expect(useStackStore.getState().missingSelection).toBeNull();
    expect(useUIStore.getState().sidebarOpen).toBe(false);
    expect(showToast).toHaveBeenCalledTimes(1);
    expect(showToast).toHaveBeenCalledWith('warning', 'a is no longer in the stack');
  });

  it('records the missing name in the same write that removes the node', () => {
    deliver('a', 'b');
    useStackStore.getState().selectNode('mcp-a');
    const seen: Array<{ hasNode: boolean; name?: string }> = [];
    const unsubscribe = useStackStore.subscribe((s) => {
      seen.push({
        hasNode: s.nodes.some((n) => n.id === 'mcp-a'),
        name: s.missingSelection?.name,
      });
    });
    deliver('b');
    unsubscribe();
    for (const state of seen) {
      if (!state.hasNode) expect(state.name).toBe('a');
    }
    expect(seen.some((s) => !s.hasNode)).toBe(true);
  });

  it('writes nothing for an unchanged poll while the selection is missing', () => {
    deliver('a', 'b');
    useStackStore.getState().selectNode('mcp-a');
    deliver('b');
    const before = useStackStore.getState().missingSelection;
    const nodes = useStackStore.getState().nodes;
    useStackStore.getState().refreshNodesAndEdges();
    expect(useStackStore.getState().missingSelection).toBe(before);
    expect(useStackStore.getState().nodes).toBe(nodes);
  });

  it('drops without a toast when the name is unknown', () => {
    deliver('b');
    useStackStore.getState().selectNode('mcp-ghost');
    useUIStore.getState().setSidebarOpen(true);
    useStackStore.getState().refreshNodesAndEdges();
    expect(useStackStore.getState().missingSelection).toMatchObject({ id: 'mcp-ghost' });
    expect(useStackStore.getState().missingSelection?.name).toBeUndefined();

    vi.advanceTimersByTime(SELECTION_MISSING_GRACE_MS);
    useStackStore.getState().refreshNodesAndEdges();
    expect(useStackStore.getState().selectedNodeId).toBeNull();
    expect(useUIStore.getState().sidebarOpen).toBe(false);
    expect(showToast).not.toHaveBeenCalled();
  });

  it('selectNode resets a recorded missing selection', () => {
    deliver('a', 'b');
    useStackStore.getState().selectNode('mcp-a');
    deliver('b');
    expect(useStackStore.getState().missingSelection).not.toBeNull();
    useStackStore.getState().selectNode('mcp-b');
    expect(useStackStore.getState().missingSelection).toBeNull();
  });
});
