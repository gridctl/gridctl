import { describe, it, expect } from 'vitest';
import { buildRunTree, flattenRunTree } from '../components/runs/tree';
import type { AgentRunSummary } from '../lib/agent-runs';

function r(id: string, parent?: string): AgentRunSummary {
  return {
    run_id: id,
    status: 'ok',
    event_count: 0,
    parent_run_id: parent,
  };
}

describe('buildRunTree / flattenRunTree', () => {
  it('roots orphans whose parent is not in the window', () => {
    const runs = [r('child', 'missing'), r('standalone')];
    const tree = buildRunTree(runs);
    expect(tree.map((n) => n.run.run_id).sort()).toEqual(['child', 'standalone']);
    for (const n of tree) expect(n.depth).toBe(0);
  });

  it('assigns BFS depth even when grandchildren appear before grandparents', () => {
    // Source order surfaces a grandchild ahead of its grandparent.
    // A naive parent-attach-and-set-depth pass would mis-indent.
    const runs = [
      r('grandchild', 'child'),
      r('child', 'root'),
      r('root'),
    ];
    const tree = buildRunTree(runs);
    expect(tree).toHaveLength(1);
    const root = tree[0];
    expect(root.run.run_id).toBe('root');
    expect(root.depth).toBe(0);
    expect(root.children).toHaveLength(1);
    const child = root.children[0];
    expect(child.run.run_id).toBe('child');
    expect(child.depth).toBe(1);
    expect(child.children).toHaveLength(1);
    expect(child.children[0].run.run_id).toBe('grandchild');
    expect(child.children[0].depth).toBe(2);
  });

  it('flattenRunTree honours the collapsed set', () => {
    const tree = buildRunTree([r('root'), r('child', 'root'), r('grandchild', 'child')]);
    const all = flattenRunTree(tree, new Set());
    expect(all.map((n) => n.run.run_id)).toEqual(['root', 'child', 'grandchild']);

    const collapsedRoot = flattenRunTree(tree, new Set(['root']));
    expect(collapsedRoot.map((n) => n.run.run_id)).toEqual(['root']);

    const collapsedChild = flattenRunTree(tree, new Set(['child']));
    expect(collapsedChild.map((n) => n.run.run_id)).toEqual(['root', 'child']);
  });
});
