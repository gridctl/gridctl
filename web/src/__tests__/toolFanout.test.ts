import { describe, it, expect } from 'vitest';
import type { Node, Edge } from '@xyflow/react';

import {
  TOOL_FANOUT_CAP,
  computeToolFanout,
  createToolFanout,
  appendToolFanout,
  overflowGridShape,
  toolNodeId,
  overflowNodeId,
} from '../lib/graph/toolFanout';
import { LAYOUT } from '../lib/constants';

function makeServerNode(name: string, tools: string[], x = 800, y = 0): Node {
  return {
    id: `mcp-${name}`,
    type: 'mcpServer',
    position: { x, y },
    data: { type: 'mcp-server', name, tools, toolCount: tools.length },
  };
}

function names(n: number): string[] {
  return Array.from({ length: n }, (_, i) => `tool-${i}`);
}

describe('computeToolFanout (cap logic)', () => {
  it('returns nothing for an empty tool list', () => {
    expect(computeToolFanout([])).toEqual({ visible: [], overflow: [] });
  });

  it('shows every tool with no overflow when below the cap', () => {
    const { visible, overflow } = computeToolFanout(names(5));
    expect(visible).toHaveLength(5);
    expect(overflow).toHaveLength(0);
  });

  it('shows exactly the cap with no overflow node at the boundary', () => {
    const { visible, overflow } = computeToolFanout(names(TOOL_FANOUT_CAP));
    expect(visible).toHaveLength(TOOL_FANOUT_CAP);
    expect(overflow).toHaveLength(0);
  });

  it('caps the visible set and overflows the remainder past the cap', () => {
    const { visible, overflow } = computeToolFanout(names(TOOL_FANOUT_CAP + 2));
    expect(visible).toHaveLength(TOOL_FANOUT_CAP);
    expect(overflow).toHaveLength(2);
    // No tool is both visible and overflowed.
    expect(new Set([...visible, ...overflow]).size).toBe(TOOL_FANOUT_CAP + 2);
  });

  it('honors a custom cap', () => {
    const { visible, overflow } = computeToolFanout(names(4), 2);
    expect(visible).toEqual(['tool-0', 'tool-1']);
    expect(overflow).toEqual(['tool-2', 'tool-3']);
  });
});

describe('overflowGridShape (popover grid)', () => {
  it.each([
    { count: 0, rows: 0, cols: 0 },
    { count: 1, rows: 1, cols: 1 },
    { count: 10, rows: 10, cols: 1 },
    { count: 11, rows: 6, cols: 2 },
    { count: 30, rows: 10, cols: 3 },
    { count: 40, rows: 10, cols: 4 },
    // Past the column cap the grid grows rows (the panel scrolls) not columns.
    { count: 41, rows: 11, cols: 4 },
    { count: 100, rows: 25, cols: 4 },
  ])('shapes $count tools into $rows rows x $cols cols', ({ count, rows, cols }) => {
    expect(overflowGridShape(count)).toEqual({ rows, cols });
  });

  it('never leaves an empty column', () => {
    for (let count = 1; count <= 120; count++) {
      const { rows, cols } = overflowGridShape(count);
      expect(rows * cols).toBeGreaterThanOrEqual(count);
      // Dropping a column must not still fit every tool.
      expect(rows * (cols - 1)).toBeLessThan(count);
    }
  });
});

describe('createToolFanout', () => {
  it('returns nothing for a server with no tools', () => {
    const result = createToolFanout(makeServerNode('empty', []));
    expect(result.nodes).toHaveLength(0);
    expect(result.edges).toHaveLength(0);
  });

  it('builds one tool node and one non-highlightable edge per tool', () => {
    const server = makeServerNode('github', ['search', 'create-pr', 'merge']);
    const { nodes, edges } = createToolFanout(server);

    expect(nodes).toHaveLength(3);
    expect(edges).toHaveLength(3);

    for (const node of nodes) {
      expect(node.type).toBe('tool');
      expect(node.selectable).toBe(false);
      const data = node.data as { type: string; serverNodeId: string };
      expect(data.type).toBe('tool');
      expect(data.serverNodeId).toBe('mcp-github');
    }

    for (const edge of edges) {
      expect(edge.source).toBe('mcp-github');
      expect(edge.sourceHandle).toBe('output');
      expect(edge.targetHandle).toBe('input');
      const meta = edge.data as { relationType: string; isHighlightable: boolean };
      expect(meta.relationType).toBe('server-to-tool');
      // Must stay out of PR 1's client-reach highlight walk.
      expect(meta.isHighlightable).toBe(false);
    }
  });

  it('positions tool nodes in a column to the right of the server', () => {
    const server = makeServerNode('svc', ['a', 'b'], 800, 0);
    const { nodes } = createToolFanout(server);
    const expectedX = 800 + LAYOUT.NODE_WIDTH + LAYOUT.TOOL_OFFSET_X;

    for (const node of nodes) {
      expect(node.position.x).toBe(expectedX);
      expect(node.position.x).toBeGreaterThan(server.position.x);
    }
    // Stacked vertically (distinct y per node).
    expect(nodes[0].position.y).not.toBe(nodes[1].position.y);
  });

  it('caps tool nodes and emits a single overflow node carrying the remainder', () => {
    const tools = names(TOOL_FANOUT_CAP + 5); // 15 tools
    const server = makeServerNode('big', tools);
    const { nodes, edges } = createToolFanout(server);

    const toolNodes = nodes.filter((n) => n.type === 'tool');
    const overflowNodes = nodes.filter((n) => n.type === 'toolOverflow');

    expect(toolNodes).toHaveLength(TOOL_FANOUT_CAP);
    expect(overflowNodes).toHaveLength(1);
    // One edge per mounted node (tools + the overflow node).
    expect(edges).toHaveLength(TOOL_FANOUT_CAP + 1);

    const overflow = overflowNodes[0];
    expect(overflow.id).toBe(overflowNodeId('mcp-big'));
    const data = overflow.data as { overflowCount: number; hiddenTools: string[] };
    expect(data.overflowCount).toBe(5);
    expect(data.hiddenTools).toEqual(tools.slice(TOOL_FANOUT_CAP));
  });

  it('preserves dragged positions for tool nodes', () => {
    const server = makeServerNode('svc', ['a']);
    const id = toolNodeId('mcp-svc', 'a');
    const draggedPositions = new Map([[id, { x: 1234, y: 567 }]]);
    const { nodes } = createToolFanout(server, { draggedPositions });
    expect(nodes[0].position).toEqual({ x: 1234, y: 567 });
  });
});

describe('appendToolFanout', () => {
  const backboneNodes: Node[] = [
    makeServerNode('a', ['a1', 'a2']),
    makeServerNode('b', ['b1']),
    { id: 'gateway', type: 'gateway', position: { x: 400, y: 0 }, data: { type: 'gateway' } },
  ];
  const backboneEdges: Edge[] = [
    { id: 'edge-gateway-mcp-a', source: 'gateway', target: 'mcp-a' },
  ];

  it('returns the backbone unchanged when nothing is expanded', () => {
    const result = appendToolFanout(backboneNodes, backboneEdges, new Set());
    expect(result.nodes).toBe(backboneNodes);
    expect(result.edges).toBe(backboneEdges);
  });

  it('appends fan-out only for expanded servers, leaving the backbone intact', () => {
    const result = appendToolFanout(
      backboneNodes,
      backboneEdges,
      new Set(['mcp-a'])
    );

    // Original backbone nodes/edges are still present.
    expect(result.nodes.slice(0, backboneNodes.length)).toEqual(backboneNodes);
    expect(result.edges.slice(0, backboneEdges.length)).toEqual(backboneEdges);

    // Only server a fanned out (2 tools), server b did not.
    const added = result.nodes.filter((n) => n.type === 'tool');
    expect(added.map((n) => n.id).sort()).toEqual([
      toolNodeId('mcp-a', 'a1'),
      toolNodeId('mcp-a', 'a2'),
    ]);
  });

  it('expands multiple servers independently', () => {
    const result = appendToolFanout(
      backboneNodes,
      backboneEdges,
      new Set(['mcp-a', 'mcp-b'])
    );
    const added = result.nodes.filter((n) => n.type === 'tool');
    expect(added).toHaveLength(3); // 2 from a + 1 from b
  });

  it('ignores expanded ids with no matching server node', () => {
    const result = appendToolFanout(
      backboneNodes,
      backboneEdges,
      new Set(['mcp-ghost'])
    );
    expect(result.nodes.filter((n) => n.type === 'tool')).toHaveLength(0);
  });

  it('stacks expanded servers as non-overlapping vertical bands in one column', () => {
    // Two servers at the same X; bands must share a column and tile vertically.
    const sameXNodes: Node[] = [
      makeServerNode('a', ['a1', 'a2'], 800, 0),
      makeServerNode('b', ['b1'], 800, 300),
    ];
    const result = appendToolFanout(sameXNodes, [], new Set(['mcp-a', 'mcp-b']));

    const toolNodes = result.nodes.filter((n) => n.type === 'tool');
    // All tools share a single column X.
    expect(new Set(toolNodes.map((n) => n.position.x)).size).toBe(1);

    const aTools = result.nodes.filter(
      (n) => (n.data as { serverNodeId?: string }).serverNodeId === 'mcp-a'
    );
    const bTools = result.nodes.filter(
      (n) => (n.data as { serverNodeId?: string }).serverNodeId === 'mcp-b'
    );
    // Server a is above server b, so a's band sits entirely above b's band.
    const aMaxY = Math.max(...aTools.map((n) => n.position.y));
    const bMinY = Math.min(...bTools.map((n) => n.position.y));
    expect(bMinY).toBeGreaterThan(aMaxY);
  });
});
