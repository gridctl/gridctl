import { describe, it, expect } from 'vitest';
import {
  auditWindowMs,
  classifyTool,
  effectiveEnabledTools,
  formatLastUsed,
  unusedEnabledTools,
} from '../lib/toolAudit';
import type { MCPServerStatus } from '../types';

const NOW = Date.parse('2026-05-24T12:00:00Z');
const W7D = auditWindowMs('7d');

function srv(tools: string[], toolWhitelist?: string[]): MCPServerStatus {
  return { name: 's', tools, toolWhitelist } as unknown as MCPServerStatus;
}

describe('classifyTool', () => {
  it('is disabled when not enabled, regardless of usage', () => {
    expect(classifyTool(false, '2026-05-24T11:00:00Z', W7D, NOW)).toBe('disabled');
  });

  it('is used when last call falls within the window', () => {
    // 1 day ago, window 7d.
    expect(classifyTool(true, '2026-05-23T12:00:00Z', W7D, NOW)).toBe('used');
  });

  it('is unused when the last call is older than the window', () => {
    // 14 days ago, window 7d.
    expect(classifyTool(true, '2026-05-10T12:00:00Z', W7D, NOW)).toBe('unused');
  });

  it('is unused when there is no recorded call', () => {
    expect(classifyTool(true, undefined, W7D, NOW)).toBe('unused');
  });

  it('is unused on an unparseable timestamp (honest fallback)', () => {
    expect(classifyTool(true, 'not-a-date', W7D, NOW)).toBe('unused');
  });
});

describe('effectiveEnabledTools', () => {
  it('returns the whitelist when it is non-empty', () => {
    expect([...effectiveEnabledTools(srv(['a', 'b', 'c'], ['a', 'b']))].sort()).toEqual(['a', 'b']);
  });

  it('returns every advertised tool when the whitelist is empty (expose-all)', () => {
    expect([...effectiveEnabledTools(srv(['a', 'b'], []))].sort()).toEqual(['a', 'b']);
  });

  it('returns every advertised tool when the whitelist is absent', () => {
    expect([...effectiveEnabledTools(srv(['a', 'b']))].sort()).toEqual(['a', 'b']);
  });
});

describe('unusedEnabledTools', () => {
  it('counts only exposed tools with no recent activity', () => {
    // a used (recent), b exposed but idle, c not exposed (disabled).
    const usage = { a: { calls: 3, lastCalledAt: '2026-05-24T11:00:00Z' } };
    expect(unusedEnabledTools(srv(['a', 'b', 'c'], ['a', 'b']), usage, W7D, NOW)).toEqual(['b']);
  });

  it('treats every exposed tool as unused when usage is undefined', () => {
    expect(unusedEnabledTools(srv(['a', 'b'], []), undefined, W7D, NOW).sort()).toEqual(['a', 'b']);
  });
});

describe('formatLastUsed', () => {
  it('returns a placeholder when never recorded', () => {
    expect(formatLastUsed(undefined)).toBe('no recorded calls');
    expect(formatLastUsed('not-a-date')).toBe('no recorded calls');
  });

  it('renders days for multi-day gaps', () => {
    const threeDaysAgo = new Date(Date.now() - 3 * 24 * 60 * 60 * 1000).toISOString();
    expect(formatLastUsed(threeDaysAgo)).toBe('3d ago');
  });

  it('renders weeks for multi-week gaps', () => {
    const twoWeeksAgo = new Date(Date.now() - 14 * 24 * 60 * 60 * 1000).toISOString();
    expect(formatLastUsed(twoWeeksAgo)).toBe('2w ago');
  });
});
