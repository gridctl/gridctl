import { describe, it, expect } from 'vitest';
import { globToRegExp, planBulkAction } from '../lib/toolBulk';
import type { MCPServerStatus } from '../types';

function srv(name: string, tools: string[], toolWhitelist?: string[]): MCPServerStatus {
  return { name, tools, toolWhitelist } as unknown as MCPServerStatus;
}

describe('globToRegExp', () => {
  it('matches a trailing wildcard', () => {
    const re = globToRegExp('delete_*');
    expect(re.test('delete_row')).toBe(true);
    expect(re.test('delete_')).toBe(true);
    expect(re.test('list_rows')).toBe(false);
    expect(re.test('soft_delete_row')).toBe(false); // anchored
  });

  it('treats ? as a single char and escapes regex metachars', () => {
    expect(globToRegExp('get_?').test('get_x')).toBe(true);
    expect(globToRegExp('get_?').test('get_xy')).toBe(false);
    // A literal dot must not act as "any char".
    expect(globToRegExp('a.b').test('axb')).toBe(false);
    expect(globToRegExp('a.b').test('a.b')).toBe(true);
  });
});

describe('planBulkAction — expose-all', () => {
  it('targets only servers that currently restrict tools', () => {
    const servers = [
      srv('github', ['a', 'b'], ['a']), // restricts → change to []
      srv('atlassian', ['x'], []), // already expose-all → unchanged
      srv('gitlab', ['m', 'n']), // no whitelist → unchanged
    ];
    const plan = planBulkAction(servers, 'expose-all', '');
    expect(plan.entries).toEqual([{ name: 'github', tools: [], hidden: [] }]);
    expect(plan.blocked).toEqual([]);
  });
});

describe('planBulkAction — hide-pattern', () => {
  it('hides matching exposed tools and keeps the rest as an explicit whitelist', () => {
    const servers = [
      srv('github', ['create_issue', 'delete_issue', 'delete_repo']), // expose-all
    ];
    const plan = planBulkAction(servers, 'hide-pattern', 'delete_*');
    expect(plan.matchedTools).toBe(2);
    expect(plan.entries).toEqual([
      { name: 'github', tools: ['create_issue'], hidden: ['delete_issue', 'delete_repo'] },
    ]);
    expect(plan.blocked).toEqual([]);
  });

  it('operates on the currently-exposed set when a whitelist exists', () => {
    const servers = [srv('s', ['a', 'b', 'delete_x', 'delete_y'], ['a', 'delete_x'])];
    const plan = planBulkAction(servers, 'hide-pattern', 'delete_*');
    // Only delete_x is exposed-and-matching; delete_y is already hidden.
    expect(plan.matchedTools).toBe(1);
    expect(plan.entries).toEqual([{ name: 's', tools: ['a'], hidden: ['delete_x'] }]);
  });

  it('skips servers with no match', () => {
    const plan = planBulkAction([srv('s', ['a', 'b'])], 'hide-pattern', 'delete_*');
    expect(plan.entries).toEqual([]);
    expect(plan.matchedTools).toBe(0);
  });

  it('blocks (does not re-expose) a server whose every exposed tool matches', () => {
    // Hiding delete_* would leave zero exposed tools; [] = expose-all, so we
    // must refuse rather than silently re-expose everything.
    const servers = [srv('s', ['delete_a', 'delete_b'])];
    const plan = planBulkAction(servers, 'hide-pattern', 'delete_*');
    expect(plan.entries).toEqual([]);
    expect(plan.blocked).toEqual(['s']);
  });

  it('returns an empty plan for a blank pattern', () => {
    const plan = planBulkAction([srv('s', ['a'])], 'hide-pattern', '   ');
    expect(plan.entries).toEqual([]);
    expect(plan.matchedTools).toBe(0);
  });
});
