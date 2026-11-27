import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, render } from '@testing-library/react';
import { StrictMode, Suspense, lazy, useEffect, useRef } from 'react';
import { MemoryRouter, Route, Routes, useLocation, useNavigate, type NavigateFunction } from 'react-router';
import {
  NODE_PARAM_GRACE_MS,
  decideNodeParam,
  isSelectableNodeId,
  useNodeParamSync,
} from '../hooks/useNodeParamSync';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import { usePinsStore } from '../stores/usePinsStore';
import { showToast } from '../components/ui/Toast';
import type { ClientStatus, GatewayStatus } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

const toast = vi.mocked(showToast);
const originalSelectNode = useStackStore.getState().selectNode;

let navigateRef: NavigateFunction;
let locations: string[] = [];

// Records each distinct location (by key), so a replace write counts as one change.
function Probe() {
  const location = useLocation();
  const navigate = useNavigate();
  const lastKey = useRef<string | null>(null);
  useEffect(() => {
    navigateRef = navigate;
    if (lastKey.current === location.key) return;
    lastKey.current = location.key;
    locations.push(location.pathname + location.search);
  });
  return null;
}

function Hook() {
  useNodeParamSync();
  return null;
}

function renderAt(entries: string[], opts: { strict?: boolean; index?: number } = {}) {
  const tree = (
    <MemoryRouter initialEntries={entries} initialIndex={opts.index}>
      <Probe />
      <Routes>
        <Route path="/stack" element={<Hook />} />
        <Route path="*" element={null} />
      </Routes>
    </MemoryRouter>
  );
  return render(opts.strict ? <StrictMode>{tree}</StrictMode> : tree);
}

const url = () => locations[locations.length - 1];
const changes = () => locations.length - 1;
const selected = () => useStackStore.getState().selectedNodeId;

function status(servers: string[] = ['a', 'b']): GatewayStatus {
  return {
    gateway: { name: 'gw', version: '0.0.0' },
    'mcp-servers': servers.map((name) => ({
      name,
      transport: 'http',
      initialized: true,
      healthy: true,
      toolCount: 0,
      tools: [],
    })),
  };
}

const linkedClient: ClientStatus = {
  name: 'X',
  slug: 'x',
  detected: true,
  linked: true,
  transport: 'http',
};

function loadStatus(servers?: string[]) {
  act(() => useStackStore.getState().setGatewayStatus(status(servers)));
}

function spySelect() {
  const spy = vi.fn(originalSelectNode);
  useStackStore.setState({ selectNode: spy });
  return spy;
}

function advance(ms: number) {
  act(() => {
    vi.advanceTimersByTime(ms);
  });
}

beforeEach(() => {
  vi.useFakeTimers();
  localStorage.clear();
  locations = [];
  toast.mockClear();
  usePinsStore.setState({ pins: null });
  useUIStore.setState({ sidebarOpen: false });
  useStackStore.setState({
    gatewayInfo: null,
    nodes: [],
    edges: [],
    mcpServers: [],
    clients: [],
    selectedNodeId: null,
    missingSelection: null,
    draggedPositions: new Map(),
    positionsHydratedFor: null,
    positionsCompact: null,
    expandedServers: new Set(),
    selectNode: originalSelectNode,
  });
});

afterEach(() => {
  vi.useRealTimers();
});

describe('isSelectableNodeId', () => {
  it('accepts the gateway and prefixed ids', () => {
    for (const id of ['gateway', 'mcp-a', 'resource-db', 'client-x', 'skill-group-ops']) {
      expect(isSelectableNodeId(id)).toBe(true);
    }
  });

  it('rejects tool ids, bare prefixes, other strings, and long values', () => {
    for (const id of ['tool-mcp-a-x', 'tool-overflow-mcp-a', 'mcp-', 'client-', 'skill-group-', 'gatewayx', 'x', '']) {
      expect(isSelectableNodeId(id)).toBe(false);
    }
    expect(isSelectableNodeId(`mcp-${'a'.repeat(252)}`)).toBe(true);
    expect(isSelectableNodeId(`mcp-${'a'.repeat(253)}`)).toBe(false);
  });
});

describe('decideNodeParam', () => {
  it('is a no-op when the inputs match the last run', () => {
    const run = { raw: 'mcp-a', selected: null, exists: false };
    expect(decideNodeParam(run, run, 'mcp-a')).toEqual({ action: { kind: 'none' }, pending: 'mcp-a' });
  });

  it('lets a store change win and discards pending', () => {
    expect(
      decideNodeParam(
        { raw: 'mcp-x', selected: 'mcp-a', exists: false },
        { raw: 'mcp-x', selected: null, exists: false },
        'mcp-x',
      ),
    ).toEqual({ action: { kind: 'write' }, pending: null });
  });

  it('applies, pends, or rejects a changed parameter', () => {
    expect(decideNodeParam({ raw: 'mcp-a', selected: null, exists: true }, null, null).action).toEqual({
      kind: 'apply',
      id: 'mcp-a',
    });
    expect(decideNodeParam({ raw: 'mcp-a', selected: null, exists: false }, null, null)).toEqual({
      action: { kind: 'pend', id: 'mcp-a' },
      pending: 'mcp-a',
    });
    expect(decideNodeParam({ raw: 'tool-x', selected: null, exists: false }, null, null).action).toEqual({
      kind: 'reject',
      value: 'tool-x',
    });
  });

  it('writes from the store for an empty or missing parameter', () => {
    expect(decideNodeParam({ raw: '', selected: null, exists: false }, null, null).action.kind).toBe('write');
    expect(decideNodeParam({ raw: null, selected: 'mcp-a', exists: false }, null, null).action.kind).toBe(
      'write',
    );
    expect(decideNodeParam({ raw: null, selected: null, exists: false }, null, null).action.kind).toBe('none');
  });
});

describe('useNodeParamSync', () => {
  it('selects a node present on arrival and opens the sidebar without changing the URL', () => {
    loadStatus();
    const spy = spySelect();
    renderAt(['/stack?node=mcp-a']);
    expect(selected()).toBe('mcp-a');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(url()).toBe('/stack?node=mcp-a');
    expect(changes()).toBe(0);
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it('works for gateway, resource, and skill-group ids', () => {
    loadStatus();
    renderAt(['/stack?node=gateway']);
    expect(selected()).toBe('gateway');
    act(() => {
      useStackStore.setState({
        nodes: [...useStackStore.getState().nodes, { id: 'resource-db', position: { x: 0, y: 0 }, data: {} }],
      });
    });
    act(() => navigateRef('/stack?node=resource-db'));
    expect(selected()).toBe('resource-db');
    act(() => {
      useStackStore.setState({
        nodes: [...useStackStore.getState().nodes, { id: 'skill-group-ops', position: { x: 0, y: 0 }, data: {} }],
      });
    });
    act(() => navigateRef('/stack?node=skill-group-ops'));
    expect(selected()).toBe('skill-group-ops');
    expect(toast).not.toHaveBeenCalled();
  });

  it('selects a node that loads after mount, with no toast', () => {
    renderAt(['/stack?node=mcp-a']);
    expect(selected()).toBeNull();
    loadStatus();
    expect(selected()).toBe('mcp-a');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(url()).toBe('/stack?node=mcp-a');
    advance(NODE_PARAM_GRACE_MS * 2);
    expect(toast).not.toHaveBeenCalled();
  });

  it('selects a client node that arrives after the first status within the grace period', () => {
    loadStatus();
    renderAt(['/stack?node=client-x']);
    advance(NODE_PARAM_GRACE_MS - 100);
    expect(selected()).toBeNull();
    act(() => useStackStore.getState().setClients([linkedClient]));
    expect(selected()).toBe('client-x');
    advance(NODE_PARAM_GRACE_MS);
    expect(toast).not.toHaveBeenCalled();
    expect(url()).toBe('/stack?node=client-x');
  });

  it('gives up on a missing node after the grace period, but not before status loads', () => {
    renderAt(['/stack?node=mcp-missing']);
    advance(NODE_PARAM_GRACE_MS * 3);
    expect(toast).not.toHaveBeenCalled();
    expect(url()).toBe('/stack?node=mcp-missing');

    loadStatus();
    advance(NODE_PARAM_GRACE_MS - 1);
    expect(toast).not.toHaveBeenCalled();
    advance(1);
    expect(toast).toHaveBeenCalledTimes(1);
    expect(toast).toHaveBeenCalledWith('error', 'Node "mcp-missing" not found');
    expect(url()).toBe('/stack');
    expect(selected()).toBeNull();
  });

  it('drops a tool id at once with one toast', () => {
    renderAt(['/stack?node=tool-mcp-a-x']);
    expect(toast).toHaveBeenCalledTimes(1);
    expect(toast).toHaveBeenCalledWith('error', 'Node "tool-mcp-a-x" not found');
    expect(url()).toBe('/stack');
  });

  it('drops an oversized value at once, truncating the toast text', () => {
    const value = `mcp-${'a'.repeat(300)}`;
    renderAt([`/stack?node=${value}&spec=1`]);
    expect(toast).toHaveBeenCalledTimes(1);
    expect(toast).toHaveBeenCalledWith('error', `Node "${value.slice(0, 80)}" not found`);
    expect(url()).toBe('/stack?spec=1');
  });

  it('removes an empty parameter with no toast', () => {
    renderAt(['/stack?node=']);
    expect(url()).toBe('/stack');
    expect(toast).not.toHaveBeenCalled();
  });

  it('replaces an empty parameter with the store selection', () => {
    loadStatus();
    act(() => useStackStore.getState().selectNode('mcp-a'));
    renderAt(['/stack?node=']);
    expect(url()).toBe('/stack?node=mcp-a');
    expect(selected()).toBe('mcp-a');
    expect(toast).not.toHaveBeenCalled();
  });

  it('writes store changes to the URL and preserves other parameters', () => {
    loadStatus();
    renderAt(['/stack?spec=1']);
    act(() => useStackStore.getState().selectNode('mcp-b'));
    expect(url()).toBe('/stack?spec=1&node=mcp-b');
    expect(changes()).toBe(1);
    act(() => useStackStore.getState().selectNode(null));
    expect(url()).toBe('/stack?spec=1');
    expect(changes()).toBe(2);
  });

  it('replaces history entries so Back leaves the workspace in one step', () => {
    loadStatus();
    renderAt(['/logs', '/stack'], { index: 1 });
    act(() => useStackStore.getState().selectNode('mcp-a'));
    act(() => useStackStore.getState().selectNode('mcp-b'));
    expect(url()).toBe('/stack?node=mcp-b');
    act(() => navigateRef(-1));
    expect(url()).toBe('/logs');
  });

  it('keeps a store selection on mount without the parameter and writes it', () => {
    loadStatus();
    act(() => useStackStore.getState().selectNode('mcp-b'));
    renderAt(['/stack']);
    expect(url()).toBe('/stack?node=mcp-b');
    expect(selected()).toBe('mcp-b');
    expect(changes()).toBe(1);
  });

  it('re-adds the parameter when it is removed while mounted', () => {
    loadStatus();
    renderAt(['/stack?node=mcp-a']);
    act(() => navigateRef('/stack?spec=1'));
    expect(url()).toBe('/stack?spec=1&node=mcp-a');
    expect(selected()).toBe('mcp-a');
  });

  it('lets the URL win on a real change', () => {
    loadStatus();
    renderAt(['/stack?node=mcp-a']);
    act(() => navigateRef('/stack?node=mcp-b'));
    expect(selected()).toBe('mcp-b');
    expect(url()).toBe('/stack?node=mcp-b');
  });

  it('lets the URL win over a stale store selection on mount', () => {
    loadStatus();
    act(() => useStackStore.getState().selectNode('mcp-b'));
    renderAt(['/stack?node=mcp-a']);
    expect(selected()).toBe('mcp-a');
    expect(url()).toBe('/stack?node=mcp-a');
    expect(changes()).toBe(0);
  });

  it('cancels pending without a toast when the user selects a node', () => {
    loadStatus();
    renderAt(['/stack?node=mcp-missing']);
    advance(1000);
    act(() => useStackStore.getState().selectNode('mcp-a'));
    expect(url()).toBe('/stack?node=mcp-a');
    advance(NODE_PARAM_GRACE_MS * 2);
    expect(toast).not.toHaveBeenCalled();
    expect(selected()).toBe('mcp-a');
  });

  it('abandons pending without a toast when the parameter is removed', () => {
    loadStatus();
    renderAt(['/stack?node=mcp-missing']);
    advance(1000);
    act(() => navigateRef('/stack?spec=1'));
    advance(NODE_PARAM_GRACE_MS * 2);
    expect(toast).not.toHaveBeenCalled();
    expect(url()).toBe('/stack?spec=1');
  });

  it('rewrites from a prior store selection when a pending parameter is removed', () => {
    loadStatus();
    act(() => useStackStore.getState().selectNode('mcp-a'));
    renderAt(['/stack?node=mcp-missing']);
    expect(selected()).toBe('mcp-a');
    act(() => navigateRef('/stack?spec=1'));
    advance(NODE_PARAM_GRACE_MS * 2);
    expect(toast).not.toHaveBeenCalled();
    expect(url()).toBe('/stack?spec=1&node=mcp-a');
    expect(selected()).toBe('mcp-a');
  });

  it('replaces pending when the parameter points at another node', () => {
    loadStatus();
    renderAt(['/stack?node=mcp-x']);
    act(() => navigateRef('/stack?node=mcp-a'));
    expect(selected()).toBe('mcp-a');
    advance(NODE_PARAM_GRACE_MS * 2);
    expect(toast).not.toHaveBeenCalled();
    loadStatus(['a', 'b', 'x']);
    expect(selected()).toBe('mcp-a');
    expect(url()).toBe('/stack?node=mcp-a');
  });

  it('gives a replacing pending id a fresh grace period', () => {
    loadStatus();
    renderAt(['/stack?node=mcp-x']);
    advance(NODE_PARAM_GRACE_MS - 1000);
    act(() => navigateRef('/stack?node=mcp-y'));
    advance(NODE_PARAM_GRACE_MS - 1);
    expect(toast).not.toHaveBeenCalled();
    advance(1);
    expect(toast).toHaveBeenCalledTimes(1);
    expect(toast).toHaveBeenCalledWith('error', 'Node "mcp-y" not found');
  });

  it('does nothing on an unchanged status poll', () => {
    loadStatus();
    renderAt(['/stack?node=mcp-a']);
    act(() => useStackStore.getState().selectNode('mcp-b'));
    const before = changes();
    loadStatus();
    loadStatus();
    expect(changes()).toBe(before);
    expect(selected()).toBe('mcp-b');
  });

  it('does nothing while unmounted', () => {
    loadStatus();
    const view = renderAt(['/stack']);
    view.unmount();
    locations = [];
    act(() => useStackStore.getState().selectNode('mcp-a'));
    expect(locations).toEqual([]);
  });

  it('does not undo a workspace change that is still loading', async () => {
    loadStatus();
    let resolveChunk: (mod: { default: () => null }) => void = () => {};
    const Lazy = lazy(
      () => new Promise<{ default: () => null }>((resolve) => {
        resolveChunk = resolve;
      }),
    );
    render(
      <MemoryRouter initialEntries={['/stack?node=mcp-a']}>
        <Probe />
        <Routes>
          <Route path="/stack" element={<Suspense fallback={null}><Hook /></Suspense>} />
          <Route path="/logs" element={<Suspense fallback={null}><Lazy /></Suspense>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(selected()).toBe('mcp-a');
    act(() => {
      navigateRef('/logs');
    });
    expect(url()).toBe('/stack?node=mcp-a');
    act(() => useStackStore.getState().selectNode(null));
    expect(url()).toBe('/stack?node=mcp-a');
    await act(async () => {
      resolveChunk({ default: () => null });
      await Promise.resolve();
    });
    expect(url()).toBe('/logs');
  });

  describe('under StrictMode', () => {
    it('selects on arrival with no location change and one selectNode call', () => {
      loadStatus();
      const spy = spySelect();
      renderAt(['/stack?node=mcp-a'], { strict: true });
      expect(selected()).toBe('mcp-a');
      expect(changes()).toBe(0);
      expect(spy).toHaveBeenCalledTimes(1);
      expect(url()).toBe('/stack?node=mcp-a');
    });

    it('writes a store selection on mount exactly once', () => {
      loadStatus();
      act(() => useStackStore.getState().selectNode('mcp-b'));
      renderAt(['/stack'], { strict: true });
      expect(changes()).toBe(1);
      expect(url()).toBe('/stack?node=mcp-b');
    });

    it('drops an invalid value with one toast and one location change', () => {
      renderAt(['/stack?node=tool-mcp-a-x'], { strict: true });
      expect(toast).toHaveBeenCalledTimes(1);
      expect(changes()).toBe(1);
      expect(url()).toBe('/stack');
    });

    it('gives up with exactly one toast', () => {
      loadStatus();
      renderAt(['/stack?node=mcp-missing'], { strict: true });
      advance(NODE_PARAM_GRACE_MS * 2);
      expect(toast).toHaveBeenCalledTimes(1);
      expect(changes()).toBe(1);
      expect(url()).toBe('/stack');
    });

    it('writes one location change per store change', () => {
      loadStatus();
      const spy = spySelect();
      renderAt(['/stack'], { strict: true });
      act(() => useStackStore.getState().selectNode('mcp-a'));
      expect(changes()).toBe(1);
      act(() => useStackStore.getState().selectNode('mcp-b'));
      expect(changes()).toBe(2);
      act(() => useStackStore.getState().selectNode(null));
      expect(changes()).toBe(3);
      // Only the three calls above; the hook selected nothing itself.
      expect(spy).toHaveBeenCalledTimes(3);
    });
  });
});
