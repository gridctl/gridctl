import { describe, expect, it, vi } from 'vitest';
import {
  getLoadedYaml,
  groupIssuesByLine,
  loadYaml,
  needsFieldResolution,
  resolveIssueLines,
  sortIssues,
  tokenizeFieldPath,
  type ResolvedIssue,
} from '../lib/lineIssues';
import type { ValidationIssue } from '../types';

const STACK = [
  'version: "1"', //         1
  'name: demo', //           2
  'mcp-servers:', //         3
  '  - name: alpha', //      4
  '    image: alpha:1', //   5
  '  - name: beta', //       6
  '    network: edge', //    7
  '    image: beta:1', //    8
  'groups:', //              9
  '  team.a:', //            10
  '    exclude:', //         11
  '      - alpha', //        12
  '      - beta', //         13
  'clients:', //             14
  '  profiles:', //          15
  '    my-client:', //       16
  '      tools:', //         17
  '        - one', //        18
  '        - two', //        19
  '1: numeric', //           20
].join('\n');

function issue(field: string, extra: Partial<ValidationIssue> = {}): ValidationIssue {
  return { field, message: 'm', severity: 'error', ...extra };
}

async function resolve(content: string, issues: ValidationIssue[]) {
  return resolveIssueLines(content, issues, await loadYaml()).map((i) => i.resolvedLine);
}

describe('tokenizeFieldPath', () => {
  it('splits dots outside brackets and keeps bracket contents verbatim', () => {
    expect(tokenizeFieldPath('clients.profiles[my-client].tools[2]')).toEqual(['clients', 'profiles', 'my-client', 'tools', '2']);
    expect(tokenizeFieldPath('groups[team.a].exclude[0]')).toEqual(['groups', 'team.a', 'exclude', '0']);
    expect(tokenizeFieldPath('mcp-servers[3].network')).toEqual(['mcp-servers', '3', 'network']);
  });

  it('stops at an unclosed bracket', () => {
    expect(tokenizeFieldPath('groups[team.a')).toEqual(['groups']);
  });
});

describe('resolveIssueLines', () => {
  it('prefers the server line over the field path', async () => {
    expect(await resolve(STACK, [issue('mcp-servers[1].network', { line: 2 })])).toEqual([2]);
  });

  it('resolves map keys', async () => {
    expect(await resolve(STACK, [issue('name'), issue('clients.profiles')])).toEqual([2, 15]);
  });

  it('resolves sequence indexes to the item and then the key', async () => {
    expect(await resolve(STACK, [issue('mcp-servers[0]'), issue('mcp-servers[1].network')])).toEqual([4, 7]);
  });

  it('never uses an array index as a line number', async () => {
    expect(await resolve(STACK, [issue('mcp-servers[4].image')])).toEqual([3]);
    expect(await resolve(STACK, [issue('mcp-servers[0].image')])).toEqual([5]);
  });

  it('looks up a sequence item by id or name', async () => {
    expect(await resolve(STACK, [issue('mcp-servers[beta].image')])).toEqual([8]);
    const withIds = 'items:\n  - id: a\n  - id: b\n    x: 1\n';
    expect(await resolve(withIds, [issue('items[b].x')])).toEqual([4]);
  });

  it('keeps a bracketed key that contains a dot as one segment', async () => {
    expect(await resolve(STACK, [issue('groups[team.a].exclude[1]')])).toEqual([13]);
    expect(await resolve(STACK, [issue('clients.profiles[my-client].tools[1]')])).toEqual([19]);
  });

  it('compares non-string keys as strings', async () => {
    expect(await resolve(STACK, [issue('1')])).toEqual([20]);
  });

  it('falls back to the deepest resolved ancestor', async () => {
    expect(await resolve(STACK, [issue('mcp-servers[1].transport')])).toEqual([6]);
    expect(await resolve(STACK, [issue('groups[team.a].include')])).toEqual([10]);
  });

  it('leaves an unresolvable root field unknown', async () => {
    expect(await resolve(STACK, [issue('stack'), issue('variables')])).toEqual([null, null]);
  });

  it('reads a line from a yaml syntax error message', async () => {
    const broken = 'a: 1\nb: [\n';
    const issues = [issue('yaml', { message: 'yaml: line 2: did not find expected node content' })];
    expect(resolveIssueLines(broken, issues, null).map((i) => i.resolvedLine)).toEqual([2]);
    expect(await resolve(broken, issues)).toEqual([2]);
    expect(await resolve(broken, [issue('other', { message: 'line 2' })])).toEqual([null]);
  });

  it('treats a line outside the content as unknown', async () => {
    expect(await resolve('a: 1\nb: 2', [issue('x', { line: 9 }), issue('yaml', { message: 'line 40' })])).toEqual([null, null]);
  });

  it('does not throw on a malformed document and degrades to server and message lines', async () => {
    const broken = 'a: [1, 2\nb: : :\n  - x\n';
    const issues = [
      issue('a'),
      issue('b', { line: 2 }),
      issue('yaml', { message: 'yaml: line 3: mapping values are not allowed' }),
    ];
    expect(await resolve(broken, issues)).toEqual([null, 2, 3]);
  });

  it('uses only server and message lines without the yaml module', () => {
    const issues = [issue('mcp-servers[1].network'), issue('x', { line: 2 })];
    expect(resolveIssueLines(STACK, issues, null).map((i) => i.resolvedLine)).toEqual([null, 2]);
  });
});

describe('needsFieldResolution', () => {
  it('is true only for issues that need the field path', () => {
    expect(needsFieldResolution([issue('x', { line: 3 })])).toBe(false);
    expect(needsFieldResolution([issue('yaml', { message: 'line 3' })])).toBe(false);
    expect(needsFieldResolution([issue('mcp-servers[0]')])).toBe(true);
  });
});

describe('sortIssues and groupIssuesByLine', () => {
  const r = (severity: ValidationIssue['severity'], resolvedLine: number | null, field: string): ResolvedIssue => ({
    field, message: field, severity, resolvedLine,
  });

  it('orders by severity, then line with unlocated last, keeping server order on ties', () => {
    const sorted = sortIssues([
      r('info', 1, 'i1'),
      r('warning', null, 'w-none'),
      r('error', null, 'e-none'),
      r('error', 9, 'e9'),
      r('error', 2, 'e2a'),
      r('error', 2, 'e2b'),
      r('warning', 1, 'w1'),
    ]);
    expect(sorted.map((i) => i.field)).toEqual(['e2a', 'e2b', 'e9', 'e-none', 'w1', 'w-none', 'i1']);
  });

  it('groups located issues by line', () => {
    const grouped = groupIssuesByLine([r('error', 2, 'a'), r('warning', 2, 'b'), r('info', null, 'c')]);
    expect([...grouped.keys()]).toEqual([2]);
    expect(grouped.get(2)?.map((i) => i.field)).toEqual(['a', 'b']);
  });
});

describe('loadYaml', () => {
  it('caches the module after the first load', async () => {
    const mod = await loadYaml();
    expect(getLoadedYaml()).toBe(mod);
    expect(await loadYaml()).toBe(mod);
  });

  it('rejects when the import fails and allows a later retry', async () => {
    vi.resetModules();
    vi.doMock('yaml', () => {
      throw new Error('chunk failed');
    });
    const fresh = await import('../lib/lineIssues');
    await expect(fresh.loadYaml()).rejects.toThrow();
    expect(fresh.getLoadedYaml()).toBeNull();
    vi.doUnmock('yaml');
    await expect(fresh.loadYaml()).resolves.toBeDefined();
    expect(fresh.getLoadedYaml()).not.toBeNull();
  });
});
