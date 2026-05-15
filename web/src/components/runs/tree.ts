import type { AgentRunSummary } from '../../lib/agent-runs';

/**
 * RunRowNode is the depth-tagged tree node both the global Runs grid
 * and the Agent IDE sidebar render against. Lifted to a shared module
 * so changing the shape (e.g. adding a "muted" parent flag) only
 * touches one place.
 */
export interface RunRowNode {
  run: AgentRunSummary;
  depth: number;
  children: RunRowNode[];
}

/**
 * buildRunTree groups runs by `parent_run_id` and returns a
 * depth-tagged forest sorted in the input order (which the server
 * already returns newest-first). Runs whose parent is not in the
 * current window become roots so they don't vanish.
 *
 * The depth assignment runs as a BFS pass after the tree is fully
 * linked — otherwise a child surfaced before its grandparent would
 * mis-indent under the source-order traversal.
 */
export function buildRunTree(runs: AgentRunSummary[]): RunRowNode[] {
  const byID = new Map<string, RunRowNode>();
  for (const r of runs) {
    byID.set(r.run_id, { run: r, depth: 0, children: [] });
  }
  const roots: RunRowNode[] = [];
  for (const r of runs) {
    const node = byID.get(r.run_id)!;
    const parentID = r.parent_run_id;
    if (parentID && byID.has(parentID)) {
      byID.get(parentID)!.children.push(node);
    } else {
      roots.push(node);
    }
  }
  const queue: RunRowNode[] = [...roots];
  while (queue.length > 0) {
    const n = queue.shift()!;
    for (const c of n.children) {
      c.depth = n.depth + 1;
      queue.push(c);
    }
  }
  return roots;
}

/**
 * flattenRunTree walks the depth-tagged forest in display order,
 * skipping children of nodes whose ID is in the `collapsed` set.
 */
export function flattenRunTree(
  roots: RunRowNode[],
  collapsed: Set<string>,
): RunRowNode[] {
  const out: RunRowNode[] = [];
  const walk = (node: RunRowNode) => {
    out.push(node);
    if (collapsed.has(node.run.run_id)) return;
    for (const c of node.children) walk(c);
  };
  for (const r of roots) walk(r);
  return out;
}
