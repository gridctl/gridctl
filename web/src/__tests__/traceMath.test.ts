import { describe, it, expect } from 'vitest';
import { computeSelfTimes, computeCriticalPath } from '../lib/traceMath';
import type { SpanInterval } from '../lib/traceMath';

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
