import { describe, it, expect } from 'vitest';
import { summarizeClientReach } from '../lib/clientScope';
import type { ClientScopeResult } from '../types';

const ALL = ['github', 'atlassian', 'gitlab'];

describe('summarizeClientReach', () => {
  it('treats undefined scope as unscoped (reaches all)', () => {
    const r = summarizeClientReach(undefined, ALL);
    expect(r.scoped).toBe(false);
    expect(r.reachableCount).toBe(3);
    expect(r.totalCount).toBe(3);
    expect(r.servers).toEqual(['atlassian', 'github', 'gitlab']); // sorted
  });

  it('treats an unscoped result as reaching all even when configured', () => {
    const scope: ClientScopeResult = { configured: true, unscoped: true, servers: [], tools: [] };
    const r = summarizeClientReach(scope, ALL);
    expect(r.scoped).toBe(false);
    expect(r.reachableCount).toBe(3);
    expect(r.servers).toEqual(['atlassian', 'github', 'gitlab']);
  });

  it('treats a not-configured result as reaching all', () => {
    const scope: ClientScopeResult = { configured: false, unscoped: true, servers: [], tools: [] };
    const r = summarizeClientReach(scope, ALL);
    expect(r.scoped).toBe(false);
    expect(r.reachableCount).toBe(3);
  });

  it('reports the scoped subset (N of M) when configured and not unscoped', () => {
    const scope: ClientScopeResult = {
      configured: true,
      unscoped: false,
      servers: ['gitlab', 'github'],
      tools: [],
    };
    const r = summarizeClientReach(scope, ALL);
    expect(r.scoped).toBe(true);
    expect(r.reachableCount).toBe(2);
    expect(r.totalCount).toBe(3);
    expect(r.servers).toEqual(['github', 'gitlab']); // sorted
  });

  it('handles an empty stack (no servers)', () => {
    const r = summarizeClientReach(undefined, []);
    expect(r.scoped).toBe(false);
    expect(r.reachableCount).toBe(0);
    expect(r.totalCount).toBe(0);
    expect(r.servers).toEqual([]);
  });
});
