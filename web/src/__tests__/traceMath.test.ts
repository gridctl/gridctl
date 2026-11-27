import { describe, it, expect } from 'vitest';
import { computeSelfTimes, computeCriticalPath, preorderSpanRows } from '../lib/traceMath';
import type { SpanInterval, TreeRowInput } from '../lib/traceMath';

function at(ms: number): string {
  return new Date(Date.UTC(2026, 6, 23, 10, 0, 0, ms)).toISOString();
}

describe('preorderSpanRows', () => {
  // Start-time order would be root, A, B, C.
  const tree: TreeRowInput[] = [
    { spanId: 'root', parentSpanId: '', startTime: at(0) },
    { spanId: 'B', parentSpanId: 'root', startTime: at(1) },
    { spanId: 'C', parentSpanId: 'A', startTime: at(2) },
    { spanId: 'A', parentSpanId: 'root', startTime: at(0) },
  ];

  it('emits each parent followed by its subtree', () => {
    const rows = preorderSpanRows(tree);
    expect(rows.map((r) => r.span.spanId)).toEqual(['root', 'A', 'C', 'B']);
    expect(rows.map((r) => r.depth)).toEqual([0, 1, 2, 1]);
  });

  it('reports sibling positions and effective parents', () => {
    const byId = new Map(preorderSpanRows(tree).map((r) => [r.span.spanId, r]));
    expect(byId.get('root')).toMatchObject({ parentId: null, posInSet: 1, setSize: 1 });
    expect(byId.get('A')).toMatchObject({ parentId: 'root', posInSet: 1, setSize: 2 });
    expect(byId.get('B')).toMatchObject({ parentId: 'root', posInSet: 2, setSize: 2 });
    expect(byId.get('C')).toMatchObject({ parentId: 'A', posInSet: 1, setSize: 1 });
  });

  it('puts a child listed before its parent after the parent', () => {
    const rows = preorderSpanRows([
      { spanId: 'kid', parentSpanId: 'p', startTime: at(0) },
      { spanId: 'p', parentSpanId: '', startTime: at(0) },
    ]);
    expect(rows.map((r) => r.span.spanId)).toEqual(['p', 'kid']);
  });

  it('breaks start-time ties by input order and sorts unparseable starts last', () => {
    const rows = preorderSpanRows([
      { spanId: 'root', parentSpanId: '', startTime: at(0) },
      { spanId: 'bad', parentSpanId: 'root', startTime: 'not a time' },
      { spanId: 'x', parentSpanId: 'root', startTime: at(5) },
      { spanId: 'y', parentSpanId: 'root', startTime: at(5) },
    ]);
    expect(rows.map((r) => r.span.spanId)).toEqual(['root', 'x', 'y', 'bad']);
  });

  it('treats an orphan as a depth-0 root in the shared root group', () => {
    const rows = preorderSpanRows([
      { spanId: 'root', parentSpanId: '', startTime: at(0) },
      { spanId: 'orphan', parentSpanId: 'missing', startTime: at(1) },
    ]);
    expect(rows).toHaveLength(2);
    expect(rows[0]).toMatchObject({ depth: 0, parentId: null, posInSet: 1, setSize: 2 });
    expect(rows[1]).toMatchObject({ depth: 0, parentId: null, posInSet: 2, setSize: 2 });
    expect(rows[1].span.spanId).toBe('orphan');
  });

  it('returns every span of a parent cycle once, with the broken root at depth 0', () => {
    const rows = preorderSpanRows([
      { spanId: 'a', parentSpanId: 'b', startTime: at(0) },
      { spanId: 'b', parentSpanId: 'a', startTime: at(1) },
    ]);
    expect(rows.map((r) => r.span.spanId)).toEqual(['a', 'b']);
    expect(rows[0]).toMatchObject({ depth: 0, parentId: null, posInSet: 1, setSize: 1 });
    expect(rows[1]).toMatchObject({ depth: 1, parentId: 'a', posInSet: 1, setSize: 1 });
  });

  it('treats a self-parented span as a root', () => {
    const rows = preorderSpanRows([{ spanId: 'a', parentSpanId: 'a', startTime: at(0) }]);
    expect(rows).toEqual([expect.objectContaining({ depth: 0, parentId: null, posInSet: 1, setSize: 1 })]);
  });

  it('returns nothing for no spans', () => {
    expect(preorderSpanRows([])).toEqual([]);
  });
});

// Sequential tool-call shape: root > routing, client call, format conversion.
const sequential: SpanInterval[] = [
  { spanId: 'root', parentSpanId: '', start: 0, end: 100 },
  { spanId: 'routing', parentSpanId: 'root', start: 0, end: 1 },
  { spanId: 'client', parentSpanId: 'root', start: 1, end: 95 },
  { spanId: 'format', parentSpanId: 'root', start: 95, end: 96 },
];

describe('computeSelfTimes', () => {
  it('subtracts merged child coverage from the parent', () => {
    const self = computeSelfTimes(sequential);
    // Children cover [0,1] + [1,95] + [95,96] = 96 of the root's 100.
    expect(self.get('root')).toBe(4);
    expect(self.get('routing')).toBe(1);
    expect(self.get('client')).toBe(94);
    expect(self.get('format')).toBe(1);
  });

  it('merges overlapping children instead of double-counting', () => {
    const spans: SpanInterval[] = [
      { spanId: 'root', parentSpanId: '', start: 0, end: 100 },
      { spanId: 'a', parentSpanId: 'root', start: 0, end: 60 },
      { spanId: 'b', parentSpanId: 'root', start: 40, end: 90 },
    ];
    // Union of [0,60] and [40,90] is [0,90]: self = 10, not 100 - 110.
    expect(computeSelfTimes(spans).get('root')).toBe(10);
  });

  it('clips children overflowing the parent and floors at zero', () => {
    const spans: SpanInterval[] = [
      { spanId: 'root', parentSpanId: '', start: 0, end: 50 },
      { spanId: 'a', parentSpanId: 'root', start: 0, end: 80 },
    ];
    expect(computeSelfTimes(spans).get('root')).toBe(0);
  });

  it('treats orphaned parents as roots', () => {
    const spans: SpanInterval[] = [
      { spanId: 'only', parentSpanId: 'missing', start: 0, end: 30 },
    ];
    expect(computeSelfTimes(spans).get('only')).toBe(30);
  });
});

describe('computeCriticalPath', () => {
  it('walks the full sequential chain', () => {
    const path = computeCriticalPath(sequential);
    expect(path).toEqual(new Set(['root', 'routing', 'client', 'format']));
  });

  it('picks the latest finisher among overlapping async children', () => {
    const spans: SpanInterval[] = [
      { spanId: 'root', parentSpanId: '', start: 0, end: 100 },
      { spanId: 'a', parentSpanId: 'root', start: 0, end: 50 },
      { spanId: 'b', parentSpanId: 'root', start: 30, end: 90 },
    ];
    const path = computeCriticalPath(spans);
    // Greedy limitation (shared with Jaeger): after descending into b, the
    // walk continues from b.start = 30, and a (ending at 50) no longer fits.
    expect(path).toEqual(new Set(['root', 'b']));
  });

  it('descends through nested children', () => {
    const spans: SpanInterval[] = [
      { spanId: 'root', parentSpanId: '', start: 0, end: 100 },
      { spanId: 'client', parentSpanId: 'root', start: 5, end: 95 },
      { spanId: 'downstream', parentSpanId: 'client', start: 10, end: 90 },
    ];
    const path = computeCriticalPath(spans);
    expect(path).toEqual(new Set(['root', 'client', 'downstream']));
  });

  it('terminates on zero-duration children instead of looping', () => {
    // Sub-millisecond spans (e.g. mcp.routing) truncate to start === end.
    const spans: SpanInterval[] = [
      { spanId: 'root', parentSpanId: '', start: 0, end: 10 },
      { spanId: 'zero', parentSpanId: 'root', start: 5, end: 5 },
      { spanId: 'b', parentSpanId: 'root', start: 0, end: 3 },
      { spanId: 'c', parentSpanId: 'root', start: 6, end: 9 },
    ];
    const path = computeCriticalPath(spans);
    expect(path.has('root')).toBe(true);
    expect(path.has('c')).toBe(true);
    // The zero-duration span may join the path but must be picked only once.
    expect(path.size).toBeLessThanOrEqual(4);
  });

  it('walks the latest-finishing root when the trace has multiple roots', () => {
    const spans: SpanInterval[] = [
      { spanId: 'r1', parentSpanId: '', start: 0, end: 10 },
      { spanId: 'r2', parentSpanId: '', start: 0, end: 20 },
      { spanId: 'child', parentSpanId: 'r2', start: 1, end: 19 },
    ];
    expect(computeCriticalPath(spans)).toEqual(new Set(['r2', 'child']));
  });

  it('handles zero-duration spans in self-time without negatives', () => {
    const spans: SpanInterval[] = [
      { spanId: 'root', parentSpanId: '', start: 0, end: 10 },
      { spanId: 'zero', parentSpanId: 'root', start: 5, end: 5 },
    ];
    const self = computeSelfTimes(spans);
    expect(self.get('root')).toBe(10);
    expect(self.get('zero')).toBe(0);
  });

  it('returns empty for no spans', () => {
    expect(computeCriticalPath([]).size).toBe(0);
  });

  it('handles a single root span', () => {
    const path = computeCriticalPath([
      { spanId: 'root', parentSpanId: '', start: 0, end: 10 },
    ]);
    expect(path).toEqual(new Set(['root']));
  });
});
