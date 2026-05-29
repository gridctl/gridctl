import { describe, it, expect } from 'vitest';
import type { Node, Edge } from '@xyflow/react';

import {
  computeHighlightState,
  isNodeDimmed,
  isEdgeDimmed,
} from '../hooks/usePathHighlight';

/**
 * Build a small butterfly-shaped graph:
 *
 *   client-a ─┐
 *             ├─> gateway ─> mcp-x   (highlightable)
 *   client-b ─┘           ─> mcp-y   (highlightable)
 *                         ─> resource-r   (not highlightable)
 *                         ─> skill-group-s (not highlightable)
 */
function makeGraph(): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = [
    { id: 'client-a', position: { x: 0, y: 0 }, data: { type: 'client' } },
    { id: 'client-b', position: { x: 0, y: 0 }, data: { type: 'client' } },
    { id: 'gateway', position: { x: 0, y: 0 }, data: { type: 'gateway' } },
    { id: 'mcp-x', position: { x: 0, y: 0 }, data: { type: 'mcp-server' } },
    { id: 'mcp-y', position: { x: 0, y: 0 }, data: { type: 'mcp-server' } },
    { id: 'resource-r', position: { x: 0, y: 0 }, data: { type: 'resource' } },
    { id: 'skill-group-s', position: { x: 0, y: 0 }, data: { type: 'skill-group' } },
  ];

  const edges: Edge[] = [
    {
      id: 'edge-client-gateway-a',
      source: 'client-a',
      target: 'gateway',
      data: { relationType: 'client-to-gateway', isHighlightable: true },
    },
    {
      id: 'edge-client-gateway-b',
      source: 'client-b',
      target: 'gateway',
      data: { relationType: 'client-to-gateway', isHighlightable: true },
    },
    {
      id: 'edge-gateway-mcp-x',
      source: 'gateway',
      target: 'mcp-x',
      data: { relationType: 'gateway-to-server', isHighlightable: true },
    },
    {
      id: 'edge-gateway-mcp-y',
      source: 'gateway',
      target: 'mcp-y',
      data: { relationType: 'gateway-to-server', isHighlightable: true },
    },
    {
      id: 'edge-gateway-resource-r',
      source: 'gateway',
      target: 'resource-r',
      data: { relationType: 'gateway-to-resource', isHighlightable: false },
    },
    {
      id: 'edge-gateway-skill-group-s',
      source: 'gateway',
      target: 'skill-group-s',
      data: { relationType: 'gateway-to-skill-group', isHighlightable: false },
    },
  ];

  return { nodes, edges };
}

describe('computeHighlightState', () => {
  it('returns no highlight and no selection when nothing is selected', () => {
    const { nodes, edges } = makeGraph();
    const state = computeHighlightState(nodes, edges, null);

    expect(state.hasSelection).toBe(false);
    expect(state.highlightedNodeIds.size).toBe(0);
    expect(state.highlightedEdgeIds.size).toBe(0);
  });

  it('highlights the transitive client -> gateway -> reachable-servers path', () => {
    const { nodes, edges } = makeGraph();
    const state = computeHighlightState(nodes, edges, 'client-a');

    expect(state.hasSelection).toBe(true);
    expect([...state.highlightedNodeIds].sort()).toEqual([
      'client-a',
      'gateway',
      'mcp-x',
      'mcp-y',
    ]);
    expect([...state.highlightedEdgeIds].sort()).toEqual([
      'edge-client-gateway-a',
      'edge-gateway-mcp-x',
      'edge-gateway-mcp-y',
    ]);
  });

  it('does not highlight non-highlightable neighbors (resources, skill groups)', () => {
    const { nodes, edges } = makeGraph();
    const state = computeHighlightState(nodes, edges, 'client-a');

    expect(state.highlightedNodeIds.has('resource-r')).toBe(false);
    expect(state.highlightedNodeIds.has('skill-group-s')).toBe(false);
    expect(state.highlightedEdgeIds.has('edge-gateway-resource-r')).toBe(false);
    expect(state.highlightedEdgeIds.has('edge-gateway-skill-group-s')).toBe(false);
  });

  it('does not highlight a sibling client sharing the gateway', () => {
    const { nodes, edges } = makeGraph();
    const state = computeHighlightState(nodes, edges, 'client-a');

    expect(state.highlightedNodeIds.has('client-b')).toBe(false);
    expect(state.highlightedEdgeIds.has('edge-client-gateway-b')).toBe(false);
  });

  it('honors scoping when a server edge is dropped from the graph', () => {
    // Simulates a future per-client scope where mcp-y is not reachable:
    // removing the gateway -> mcp-y edge narrows the highlight automatically.
    const { nodes, edges } = makeGraph();
    const scoped = edges.filter((e) => e.id !== 'edge-gateway-mcp-y');
    const state = computeHighlightState(nodes, scoped, 'client-a');

    expect(state.highlightedNodeIds.has('mcp-x')).toBe(true);
    expect(state.highlightedNodeIds.has('mcp-y')).toBe(false);
  });

  it('highlights only the selected node for non-client node types', () => {
    const { nodes, edges } = makeGraph();
    const state = computeHighlightState(nodes, edges, 'mcp-x');

    expect(state.hasSelection).toBe(true);
    expect([...state.highlightedNodeIds]).toEqual(['mcp-x']);
    expect(state.highlightedEdgeIds.size).toBe(0);
  });

  it('highlights just the id when the selected node is missing', () => {
    const { edges } = makeGraph();
    const state = computeHighlightState([], edges, 'ghost');

    expect(state.hasSelection).toBe(true);
    expect([...state.highlightedNodeIds]).toEqual(['ghost']);
    expect(state.highlightedEdgeIds.size).toBe(0);
  });

  it('treats reselecting null as a reset that clears all highlighting', () => {
    const { nodes, edges } = makeGraph();
    const selected = computeHighlightState(nodes, edges, 'client-a');
    expect(selected.hasSelection).toBe(true);

    const reset = computeHighlightState(nodes, edges, null);
    expect(reset.hasSelection).toBe(false);
    expect(reset.highlightedNodeIds.size).toBe(0);
    expect(reset.highlightedEdgeIds.size).toBe(0);
  });
});

describe('computeHighlightState with expanded tool fan-out', () => {
  // Graph where client-a reaches mcp-x (expanded, 1 tool) but not mcp-z.
  // mcp-z is also expanded but off the client's path.
  function makeGraphWithTools(): { nodes: Node[]; edges: Edge[] } {
    const { nodes, edges } = makeGraph();
    nodes.push(
      { id: 'mcp-z', position: { x: 0, y: 0 }, data: { type: 'mcp-server' } },
      { id: 'tool-mcp-x-search', position: { x: 0, y: 0 }, data: { type: 'tool', serverNodeId: 'mcp-x' } },
      { id: 'tool-overflow-mcp-x', position: { x: 0, y: 0 }, data: { type: 'tool-overflow', serverNodeId: 'mcp-x' } },
      { id: 'tool-mcp-z-secret', position: { x: 0, y: 0 }, data: { type: 'tool', serverNodeId: 'mcp-z' } }
    );
    edges.push(
      { id: 'edge-tool-mcp-x-search', source: 'mcp-x', target: 'tool-mcp-x-search', data: { relationType: 'server-to-tool', isHighlightable: false } },
      { id: 'edge-tool-overflow-mcp-x', source: 'mcp-x', target: 'tool-overflow-mcp-x', data: { relationType: 'server-to-tool', isHighlightable: false } },
      { id: 'edge-tool-mcp-z-secret', source: 'mcp-z', target: 'tool-mcp-z-secret', data: { relationType: 'server-to-tool', isHighlightable: false } }
    );
    return { nodes, edges };
  }

  it('highlights the tools (and overflow) of a reachable, expanded server', () => {
    const { nodes, edges } = makeGraphWithTools();
    const state = computeHighlightState(nodes, edges, 'client-a');

    expect(state.highlightedNodeIds.has('tool-mcp-x-search')).toBe(true);
    expect(state.highlightedNodeIds.has('tool-overflow-mcp-x')).toBe(true);
    expect(state.highlightedEdgeIds.has('edge-tool-mcp-x-search')).toBe(true);
    expect(state.highlightedEdgeIds.has('edge-tool-overflow-mcp-x')).toBe(true);
  });

  it('does not highlight tools of a server outside the client path', () => {
    const { nodes, edges } = makeGraphWithTools();
    const state = computeHighlightState(nodes, edges, 'client-a');

    // mcp-z is not reachable from client-a, so its tools stay dimmed.
    expect(state.highlightedNodeIds.has('mcp-z')).toBe(false);
    expect(state.highlightedNodeIds.has('tool-mcp-z-secret')).toBe(false);
    expect(state.highlightedEdgeIds.has('edge-tool-mcp-z-secret')).toBe(false);
  });
});

describe('computeHighlightState with per-client effective scope', () => {
  // client-a reaches servers x and y at the gateway, each with tools. The
  // client's effectiveScope narrows what is actually highlighted.
  function makeScopedGraph(scope: unknown): { nodes: Node[]; edges: Edge[] } {
    const nodes: Node[] = [
      { id: 'client-a', position: { x: 0, y: 0 }, data: { type: 'client', effectiveScope: scope } },
      { id: 'gateway', position: { x: 0, y: 0 }, data: { type: 'gateway' } },
      { id: 'mcp-x', position: { x: 0, y: 0 }, data: { type: 'mcp-server', name: 'x' } },
      { id: 'mcp-y', position: { x: 0, y: 0 }, data: { type: 'mcp-server', name: 'y' } },
      { id: 'tool-mcp-x-search', position: { x: 0, y: 0 }, data: { type: 'tool', serverNodeId: 'mcp-x', serverName: 'x', name: 'search' } },
      { id: 'tool-mcp-x-write', position: { x: 0, y: 0 }, data: { type: 'tool', serverNodeId: 'mcp-x', serverName: 'x', name: 'write' } },
      { id: 'tool-mcp-y-read', position: { x: 0, y: 0 }, data: { type: 'tool', serverNodeId: 'mcp-y', serverName: 'y', name: 'read' } },
    ];
    const edges: Edge[] = [
      { id: 'e-c-g', source: 'client-a', target: 'gateway', data: { relationType: 'client-to-gateway', isHighlightable: true } },
      { id: 'e-g-x', source: 'gateway', target: 'mcp-x', data: { relationType: 'gateway-to-server', isHighlightable: true } },
      { id: 'e-g-y', source: 'gateway', target: 'mcp-y', data: { relationType: 'gateway-to-server', isHighlightable: true } },
      { id: 'e-x-search', source: 'mcp-x', target: 'tool-mcp-x-search', data: { relationType: 'server-to-tool', isHighlightable: false } },
      { id: 'e-x-write', source: 'mcp-x', target: 'tool-mcp-x-write', data: { relationType: 'server-to-tool', isHighlightable: false } },
      { id: 'e-y-read', source: 'mcp-y', target: 'tool-mcp-y-read', data: { relationType: 'server-to-tool', isHighlightable: false } },
    ];
    return { nodes, edges };
  }

  it('narrows reachable servers to the client scope (server-level)', () => {
    const { nodes, edges } = makeScopedGraph({
      configured: true,
      unscoped: false,
      servers: ['x'],
      tools: ['x__search', 'x__write'],
    });
    const state = computeHighlightState(nodes, edges, 'client-a');

    expect(state.highlightedNodeIds.has('mcp-x')).toBe(true);
    expect(state.highlightedNodeIds.has('mcp-y')).toBe(false); // out of scope -> dimmed
    expect(state.highlightedEdgeIds.has('e-g-x')).toBe(true);
    expect(state.highlightedEdgeIds.has('e-g-y')).toBe(false);
    // Gateway still on the path.
    expect(state.highlightedNodeIds.has('gateway')).toBe(true);
  });

  it('dims tools of an in-scope server that are not in the tool scope', () => {
    const { nodes, edges } = makeScopedGraph({
      configured: true,
      unscoped: false,
      servers: ['x'],
      tools: ['x__search'], // only search is in scope; write is not
    });
    const state = computeHighlightState(nodes, edges, 'client-a');

    expect(state.highlightedNodeIds.has('tool-mcp-x-search')).toBe(true);
    expect(state.highlightedNodeIds.has('tool-mcp-x-write')).toBe(false);
    expect(state.highlightedEdgeIds.has('e-x-search')).toBe(true);
    expect(state.highlightedEdgeIds.has('e-x-write')).toBe(false);
  });

  it('shows a scoped-to-nothing client reaching only the gateway', () => {
    const { nodes, edges } = makeScopedGraph({
      configured: true,
      unscoped: false,
      servers: [],
      tools: [],
    });
    const state = computeHighlightState(nodes, edges, 'client-a');

    expect([...state.highlightedNodeIds].sort()).toEqual(['client-a', 'gateway']);
    expect(state.highlightedNodeIds.has('mcp-x')).toBe(false);
    expect(state.highlightedNodeIds.has('tool-mcp-x-search')).toBe(false);
  });

  it('preserves full gateway-exposed reach when scope is unscoped', () => {
    const { nodes, edges } = makeScopedGraph({
      configured: true,
      unscoped: true,
      servers: [],
      tools: [],
    });
    const state = computeHighlightState(nodes, edges, 'client-a');

    // unscoped: no narrowing, so both servers and all their tools light up.
    expect(state.highlightedNodeIds.has('mcp-x')).toBe(true);
    expect(state.highlightedNodeIds.has('mcp-y')).toBe(true);
    expect(state.highlightedNodeIds.has('tool-mcp-x-write')).toBe(true);
  });

  it('preserves full reach when no clients block is configured', () => {
    const { nodes, edges } = makeScopedGraph({
      configured: false,
      unscoped: true,
      servers: [],
      tools: [],
    });
    const state = computeHighlightState(nodes, edges, 'client-a');
    expect(state.highlightedNodeIds.has('mcp-x')).toBe(true);
    expect(state.highlightedNodeIds.has('mcp-y')).toBe(true);
  });
});

describe('isNodeDimmed / isEdgeDimmed', () => {
  it('dims nothing when there is no selection', () => {
    const state = computeHighlightState([], [], null);
    expect(isNodeDimmed('client-a', state)).toBe(false);
    expect(isEdgeDimmed('edge-client-gateway-a', state)).toBe(false);
  });

  it('dims nodes and edges outside the highlighted path', () => {
    const { nodes, edges } = makeGraph();
    const state = computeHighlightState(nodes, edges, 'client-a');

    // On the path -> not dimmed.
    expect(isNodeDimmed('mcp-x', state)).toBe(false);
    expect(isEdgeDimmed('edge-gateway-mcp-x', state)).toBe(false);

    // Off the path -> dimmed (faded, not hidden).
    expect(isNodeDimmed('resource-r', state)).toBe(true);
    expect(isNodeDimmed('client-b', state)).toBe(true);
    expect(isEdgeDimmed('edge-gateway-resource-r', state)).toBe(true);
  });
});
