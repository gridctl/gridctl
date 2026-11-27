import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { NodeChange } from '@xyflow/react';
import { useStackStore } from '../stores/useStackStore';
import { usePinsStore } from '../stores/usePinsStore';
import { useUIStore } from '../stores/useUIStore';
import { STACK_LAYOUT_VERSION, stackPositionsPrefName } from '../lib/stackPositions';
import { prefKey } from '../lib/preferences';
import type { GatewayStatus } from '../types';

function status(stackName: string | undefined, servers = ['github', 'jira']): GatewayStatus {
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
    ...(stackName !== undefined ? { stack_name: stackName } : {}),
  };
}

function key(stack: string): string {
  return prefKey(stackPositionsPrefName(stack));
}

function savePayload(stack: string, positions: Record<string, { x: number; y: number }>, compact = true) {
  localStorage.setItem(
    key(stack),
    JSON.stringify({ v: 1, layout: STACK_LAYOUT_VERSION, compact, positions }),
  );
}

function dragEnd(id: string, x: number, y: number): NodeChange {
  return { type: 'position', id, position: { x, y }, dragging: false };
}

function positionOf(id: string) {
  return useStackStore.getState().nodes.find((n) => n.id === id)?.position;
}

describe('useStackStore persisted positions', () => {
  beforeEach(() => {
    localStorage.clear();
    useUIStore.setState({ compactCards: true });
    usePinsStore.setState({ pins: null });
    useStackStore.setState({
      nodes: [],
      edges: [],
      draggedPositions: new Map(),
      positionsHydratedFor: null,
      positionsCompact: null,
      expandedServers: new Set(),
      clients: [],
      gatewayInfo: null,
      mcpServers: [],
      selectedNodeId: null,
      missingSelection: null,
      stackName: '',
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('places a saved node at its saved position on the first status', () => {
    savePayload('a', { 'mcp-github': { x: 1234, y: 567 } });
    useStackStore.getState().setGatewayStatus(status('a'));
    expect(positionOf('mcp-github')).toEqual({ x: 1234, y: 567 });
    expect(useStackStore.getState().draggedPositions.get('mcp-github')).toEqual({ x: 1234, y: 567 });
  });

  it('hydrates the stackless stack on its first status', () => {
    savePayload('', { 'mcp-github': { x: 99, y: 88 } });
    useStackStore.getState().setGatewayStatus(status(undefined));
    expect(positionOf('mcp-github')).toEqual({ x: 99, y: 88 });
  });

  it('does not read storage or replace the map on a poll for the same stack', () => {
    savePayload('a', { 'mcp-github': { x: 1, y: 2 } });
    useStackStore.getState().setGatewayStatus(status('a'));
    const map = useStackStore.getState().draggedPositions;
    const nodes = useStackStore.getState().nodes;
    savePayload('a', { 'mcp-github': { x: 500, y: 500 } });
    const getItem = vi.spyOn(Storage.prototype, 'getItem');
    const setItem = vi.spyOn(Storage.prototype, 'setItem');
    const removeItem = vi.spyOn(Storage.prototype, 'removeItem');
    useStackStore.getState().setGatewayStatus(status('a'));
    expect(useStackStore.getState().draggedPositions).toBe(map);
    expect(useStackStore.getState().nodes).toBe(nodes);
    expect(getItem).not.toHaveBeenCalled();
    expect(setItem).not.toHaveBeenCalled();
    expect(removeItem).not.toHaveBeenCalled();
  });

  it('replaces the map when the stack name changes', () => {
    savePayload('a', { 'mcp-github': { x: 1, y: 2 } });
    savePayload('b', { 'mcp-jira': { x: 3, y: 4 } });
    useStackStore.getState().setGatewayStatus(status('a'));
    useStackStore.getState().setGatewayStatus(status('b'));
    expect(Object.fromEntries(useStackStore.getState().draggedPositions)).toEqual({
      'mcp-jira': { x: 3, y: 4 },
    });
    useStackStore.getState().setGatewayStatus(status('c'));
    expect(useStackStore.getState().draggedPositions.size).toBe(0);
  });

  it('ignores positions saved in the other card mode', () => {
    savePayload('a', { 'mcp-github': { x: 1234, y: 567 } }, false);
    useStackStore.getState().setGatewayStatus(status('a'));
    expect(useStackStore.getState().draggedPositions.size).toBe(0);
    expect(positionOf('mcp-github')).not.toEqual({ x: 1234, y: 567 });
  });

  it('ignores a corrupt payload and renders the automatic layout', () => {
    useStackStore.getState().setGatewayStatus(status('fresh'));
    const auto = positionOf('mcp-github');
    useStackStore.setState({ positionsHydratedFor: null, nodes: [], edges: [] });
    localStorage.setItem(key('fresh'), '{broken');
    useStackStore.getState().setGatewayStatus(status('fresh'));
    expect(positionOf('mcp-github')).toEqual(auto);
  });

  it('saves a drag end under the current stack with the current card mode', () => {
    useUIStore.setState({ compactCards: false });
    useStackStore.getState().setGatewayStatus(status('a'));
    useStackStore.getState().recordDraggedPositions([dragEnd('mcp-github', 10, 20)]);
    expect(JSON.parse(localStorage.getItem(key('a'))!)).toEqual({
      v: 1,
      layout: STACK_LAYOUT_VERSION,
      compact: false,
      positions: { 'mcp-github': { x: 10, y: 20 } },
    });
  });

  it('writes nothing while a drag is in progress', () => {
    useStackStore.getState().setGatewayStatus(status('a'));
    const setItem = vi.spyOn(Storage.prototype, 'setItem');
    const map = useStackStore.getState().draggedPositions;
    useStackStore.getState().recordDraggedPositions([
      { type: 'position', id: 'mcp-github', position: { x: 1, y: 1 }, dragging: true },
      { type: 'select', id: 'mcp-github', selected: true },
    ]);
    expect(setItem).not.toHaveBeenCalled();
    expect(useStackStore.getState().draggedPositions).toBe(map);
  });

  it('moves a re-dragged node to the newest end', () => {
    useStackStore.getState().setGatewayStatus(status('a'));
    const record = useStackStore.getState().recordDraggedPositions;
    record([dragEnd('mcp-github', 1, 1)]);
    record([dragEnd('mcp-jira', 2, 2)]);
    record([dragEnd('mcp-github', 3, 3)]);
    expect([...useStackStore.getState().draggedPositions.keys()]).toEqual(['mcp-jira', 'mcp-github']);
    expect(Object.keys(JSON.parse(localStorage.getItem(key('a'))!).positions)).toEqual([
      'mcp-jira',
      'mcp-github',
    ]);
  });

  it('keeps saved entries for nodes not currently in the graph', () => {
    savePayload('a', { 'mcp-gone': { x: 7, y: 8 } });
    useStackStore.getState().setGatewayStatus(status('a'));
    useStackStore.getState().recordDraggedPositions([dragEnd('mcp-github', 1, 1)]);
    expect(Object.keys(JSON.parse(localStorage.getItem(key('a'))!).positions)).toEqual([
      'mcp-gone',
      'mcp-github',
    ]);
  });

  it('resetLayout empties the map and removes the stored value', () => {
    useStackStore.getState().setGatewayStatus(status('a'));
    const auto = positionOf('mcp-github');
    useStackStore.getState().recordDraggedPositions([dragEnd('mcp-github', 4321, 1)]);
    useStackStore.getState().refreshNodesAndEdges();
    expect(positionOf('mcp-github')).toEqual({ x: 4321, y: 1 });
    useStackStore.getState().resetLayout();
    expect(useStackStore.getState().draggedPositions.size).toBe(0);
    expect(localStorage.getItem(key('a'))).toBeNull();
    expect(positionOf('mcp-github')).toEqual(auto);
  });

  it('a reload after the card toggle shows the automatic layout', () => {
    useStackStore.getState().setGatewayStatus(status('a'));
    useStackStore.getState().recordDraggedPositions([dragEnd('mcp-github', 4321, 1)]);
    // The toggle flips the mode first, then resets (Canvas).
    useUIStore.getState().toggleCompactCards();
    useStackStore.getState().resetLayout();
    expect(localStorage.getItem(key('a'))).toBeNull();
    useStackStore.setState({ positionsHydratedFor: null, draggedPositions: new Map() });
    useStackStore.getState().setGatewayStatus(status('a'));
    expect(useStackStore.getState().draggedPositions.size).toBe(0);
  });

  it('drops positions from the other card mode when the toggle ran without the canvas', () => {
    useStackStore.getState().setGatewayStatus(status('a'));
    useStackStore.getState().recordDraggedPositions([dragEnd('mcp-github', 4321, 1)]);
    // Table view: the toggle flips the mode and nothing calls resetLayout.
    useUIStore.getState().toggleCompactCards();
    useStackStore.getState().recordDraggedPositions([dragEnd('mcp-jira', 7, 8)]);
    const payload = JSON.parse(localStorage.getItem(key('a'))!);
    expect(payload.compact).toBe(false);
    expect(payload.positions).toEqual({ 'mcp-jira': { x: 7, y: 8 } });
    useStackStore.setState({ positionsHydratedFor: null, positionsCompact: null, draggedPositions: new Map() });
    useStackStore.getState().setGatewayStatus(status('a'));
    expect([...useStackStore.getState().draggedPositions.keys()]).toEqual(['mcp-jira']);
  });

  it('saves and restores tool fan-out positions with the rest', () => {
    const toolId = 'tool-mcp-github-github-tool';
    savePayload('a', { [toolId]: { x: 42, y: 24 } });
    useStackStore.getState().setGatewayStatus({
      ...status('a'),
      'mcp-servers': [
        { name: 'github', transport: 'http', initialized: true, healthy: true, toolCount: 1, tools: ['github-tool'] },
      ],
    });
    useStackStore.getState().expandServer('mcp-github');
    expect(positionOf(toolId)).toEqual({ x: 42, y: 24 });
    useStackStore.getState().recordDraggedPositions([dragEnd(toolId, 5, 6)]);
    expect(JSON.parse(localStorage.getItem(key('a'))!).positions[toolId]).toEqual({ x: 5, y: 6 });
  });
});
