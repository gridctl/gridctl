import { describe, it, expect } from 'vitest';
import type { Edge, Node } from '@xyflow/react';
import { reuseGraphIdentities, structurallyEqual } from '../lib/graph/identity';

function node(id: string, data: Record<string, unknown> = { n: 1 }, extra: Partial<Node> = {}): Node {
  return {
    id,
    type: 'mcpServer',
    position: { x: 0, y: 0 },
    data,
    draggable: true,
    ...extra,
  };
}

describe('structurallyEqual', () => {
  it('matches nested objects and arrays and rejects a changed leaf', () => {
    expect(structurallyEqual({ a: [1, { b: 2 }] }, { a: [1, { b: 2 }] })).toBe(true);
    expect(structurallyEqual({ a: [1, { b: 2 }] }, { a: [1, { b: 3 }] })).toBe(false);
    expect(structurallyEqual(Number.NaN, Number.NaN)).toBe(true);
  });
});

describe('reuseGraphIdentities', () => {
  it('keeps measured and selected by returning the previous node when data is equal', () => {
    const prevNodes = [node('a', { n: 1 }, { measured: { width: 10, height: 20 }, selected: true })];
    const nextNodes = [node('a', { n: 1 })];
    const result = reuseGraphIdentities(prevNodes, nextNodes, [], []);
    expect(result.nodes).toBe(prevNodes);
    expect(result.nodes[0].measured).toEqual({ width: 10, height: 20 });
    expect(result.nodes[0].selected).toBe(true);
  });

  it('copies measured and selected onto a changed node and returns a new array', () => {
    const prevNodes = [node('a', { n: 1 }, {
      measured: { width: 8, height: 9 },
      selected: true,
      dragging: false,
    })];
    const nextNodes = [node('a', { n: 2 })];
    const result = reuseGraphIdentities(prevNodes, nextNodes, [], []);
    expect(result.nodes).not.toBe(prevNodes);
    expect(result.nodes[0]).not.toBe(prevNodes[0]);
    expect(result.nodes[0].data).toEqual({ n: 2 });
    expect(result.nodes[0].measured).toEqual({ width: 8, height: 9 });
    expect(result.nodes[0].selected).toBe(true);
    expect(result.nodes[0].dragging).toBe(false);
  });

  it('returns the previous arrays when every element is reused in the same order', () => {
    const prevNodes = [node('a'), node('b')];
    const nextNodes = [node('a'), node('b')];
    const prevEdges: Edge[] = [{ id: 'e', source: 'a', target: 'b' }];
    const nextEdges: Edge[] = [{ id: 'e', source: 'a', target: 'b' }];
    const result = reuseGraphIdentities(prevNodes, nextNodes, prevEdges, nextEdges);
    expect(result.nodes).toBe(prevNodes);
    expect(result.edges).toBe(prevEdges);
  });

  it('returns a new array when the order changes, reusing the previous objects', () => {
    const prevNodes = [node('a'), node('b')];
    const result = reuseGraphIdentities(prevNodes, [node('b'), node('a')], [], []);
    expect(result.nodes).not.toBe(prevNodes);
    expect(result.nodes[0]).toBe(prevNodes[1]);
    expect(result.nodes[1]).toBe(prevNodes[0]);
  });
});
