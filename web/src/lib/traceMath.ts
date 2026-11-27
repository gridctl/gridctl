/**
 * Pure span-tree math for the trace waterfall: self-time and critical path.
 * Operates on a minimal interval shape so callers can derive start/end from
 * whatever the API served (endTime may be absent and derived from duration).
 */

export interface SpanInterval {
  spanId: string;
  /** Empty string for root spans. */
  parentSpanId: string;
  /** Milliseconds since epoch. */
  start: number;
  /** Milliseconds since epoch; must be >= start. */
  end: number;
}

function childrenByParent(spans: SpanInterval[]): Map<string, SpanInterval[]> {
  const ids = new Set(spans.map((s) => s.spanId));
  const map = new Map<string, SpanInterval[]>();
  for (const span of spans) {
    // Treat spans whose parent is not in the trace as roots.
    const key = ids.has(span.parentSpanId) ? span.parentSpanId : '';
    const list = map.get(key);
    if (list) list.push(span);
    else map.set(key, [span]);
  }
  return map;
}

export interface TreeRowInput {
  spanId: string;
  /** Empty string for root spans. */
  parentSpanId: string;
  startTime: string;
}

export interface TreeRow<T extends TreeRowInput> {
  span: T;
  depth: number;
  /** Span ID of the row this one was emitted under; null for every root. */
  parentId: string | null;
  /** 1-based position among rows sharing the same effective parent. */
  posInSet: number;
  setSize: number;
}

/**
 * Pre-order (depth-first) rows for the waterfall: each parent is followed by
 * its subtree, siblings sort by start time with input order as the tiebreak,
 * and unparseable start times sort last. Spans whose parent is missing are
 * roots; spans unreachable from a root (a parent cycle) are emitted as roots.
 */
export function preorderSpanRows<T extends TreeRowInput>(spans: T[]): TreeRow<T>[] {
  const ids = new Set(spans.map((s) => s.spanId));
  const order = new Map<T, number>();
  spans.forEach((s, i) => order.set(s, i));
  const startMs = (s: T) => {
    const t = new Date(s.startTime).getTime();
    return Number.isFinite(t) ? t : Infinity;
  };
  const bySibling = (a: T, b: T) => {
    const sa = startMs(a);
    const sb = startMs(b);
    if (sa !== sb) return sa < sb ? -1 : 1;
    return order.get(a)! - order.get(b)!;
  };

  const children = new Map<string, T[]>();
  const roots: T[] = [];
  for (const span of spans) {
    if (span.parentSpanId && ids.has(span.parentSpanId) && span.parentSpanId !== span.spanId) {
      const list = children.get(span.parentSpanId);
      if (list) list.push(span);
      else children.set(span.parentSpanId, [span]);
    } else {
      roots.push(span);
    }
  }
  for (const list of children.values()) list.sort(bySibling);
  roots.sort(bySibling);

  const emitted = new Set<T>();
  const subtrees: TreeRow<T>[][] = [];
  const emit = (span: T, depth: number, parentId: string | null, out: TreeRow<T>[]) => {
    emitted.add(span);
    out.push({ span, depth, parentId, posInSet: 0, setSize: 0 });
    const kids = (children.get(span.spanId) ?? []).filter((c) => !emitted.has(c));
    kids.forEach((child, i) => {
      const before = out.length;
      emit(child, depth + 1, span.spanId, out);
      out[before].posInSet = i + 1;
      out[before].setSize = kids.length;
    });
  };
  for (const root of roots) {
    const out: TreeRow<T>[] = [];
    emit(root, 0, null, out);
    subtrees.push(out);
  }
  // Cycle members are unreachable from any root; break each cycle at its
  // earliest unemitted span in sibling order.
  const leftovers = spans.filter((s) => !emitted.has(s)).sort(bySibling);
  for (const span of leftovers) {
    if (emitted.has(span)) continue;
    const out: TreeRow<T>[] = [];
    emit(span, 0, null, out);
    subtrees.push(out);
  }

  // Roots, including cycle-broken ones, form one sibling group in emit order.
  subtrees.forEach((rows, i) => {
    rows[0].posInSet = i + 1;
    rows[0].setSize = subtrees.length;
  });
  return subtrees.flat();
}

/**
 * Self-time per span: its duration minus the union of its children's
 * intervals (merged, clipped to the parent), floored at zero. Matches the
 * standard Jaeger "Self Time" definition.
 */
export function computeSelfTimes(spans: SpanInterval[]): Map<string, number> {
  const byParent = childrenByParent(spans);
  const result = new Map<string, number>();
  for (const span of spans) {
    const children = byParent.get(span.spanId) ?? [];
    const intervals = children
      .map((c) => ({ start: Math.max(c.start, span.start), end: Math.min(c.end, span.end) }))
      .filter((iv) => iv.end > iv.start)
      .sort((a, b) => a.start - b.start);
    let covered = 0;
    let cursor = -Infinity;
    for (const iv of intervals) {
      const start = Math.max(iv.start, cursor);
      if (iv.end > start) {
        covered += iv.end - start;
        cursor = iv.end;
      }
    }
    result.set(span.spanId, Math.max(0, span.end - span.start - covered));
  }
  return result;
}

/**
 * Critical path membership, following Jaeger UI's greedy walk: from each span
 * on the path, repeatedly pick the latest-finishing child that ends within
 * the still-unaccounted window, descend into it, then continue the walk
 * leftward from that child's start.
 *
 * Known limitation (shared with Jaeger): heavily overlapping async children
 * can shadow one another; the walk favors the latest finisher rather than
 * computing true DAG slack.
 */
export function computeCriticalPath(spans: SpanInterval[]): Set<string> {
  const path = new Set<string>();
  if (spans.length === 0) return path;

  const byParent = childrenByParent(spans);
  const roots = byParent.get('') ?? [];
  if (roots.length === 0) return path;
  // With multiple roots (shouldn't happen in a well-formed trace), walk the
  // latest-finishing one.
  const root = roots.reduce((a, b) => (b.end > a.end ? b : a));

  const walk = (span: SpanInterval): void => {
    path.add(span.spanId);
    // Candidates are consumed as they are picked: a zero-duration child
    // (start === end, common after ms truncation) would otherwise satisfy
    // `end <= bound` forever once bound lands on its start and loop the walk.
    const remaining = (byParent.get(span.spanId) ?? []).map((c) => ({
      ...c,
      // Truncate children overflowing the parent before the walk.
      end: Math.min(c.end, span.end),
    }));
    let bound = span.end;
    for (;;) {
      let lfcIndex = -1;
      for (let i = 0; i < remaining.length; i++) {
        const child = remaining[i];
        if (child.end <= bound && (lfcIndex < 0 || child.end > remaining[lfcIndex].end)) {
          lfcIndex = i;
        }
      }
      if (lfcIndex < 0) break;
      const [lfc] = remaining.splice(lfcIndex, 1);
      walk(lfc);
      bound = lfc.start;
    }
  };

  walk(root);
  return path;
}
