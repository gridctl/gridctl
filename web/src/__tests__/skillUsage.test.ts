import { describe, it, expect } from 'vitest';
import { coldWindowCaveat, isUsageWindowCold, MIN_OBSERVATION_MS } from '../lib/skillUsage';
import type { SkillUsageResponse } from '../types';

const NOW = Date.parse('2026-07-27T12:00:00Z');

function usage(observedSince: string | null, skills: SkillUsageResponse['skills'] = {}): SkillUsageResponse {
  return { observedSince, skills };
}

describe('isUsageWindowCold', () => {
  it('is cold when the endpoint gave us nothing', () => {
    expect(isUsageWindowCold(null, NOW)).toBe(true);
  });

  it('is cold when observedSince is null rather than treating it as epoch zero', () => {
    expect(isUsageWindowCold(usage(null), NOW)).toBe(true);
  });

  it('is cold when observedSince is unparseable', () => {
    expect(isUsageWindowCold(usage('not-a-date'), NOW)).toBe(true);
  });

  it('is cold for a window younger than the minimum observation period', () => {
    const since = new Date(NOW - 3 * 60 * 60 * 1000).toISOString();
    expect(isUsageWindowCold(usage(since), NOW)).toBe(true);
  });

  it('is warm once the window passes the minimum observation period', () => {
    const since = new Date(NOW - MIN_OBSERVATION_MS - 1000).toISOString();
    expect(isUsageWindowCold(usage(since), NOW)).toBe(false);
  });

  // The whole point of using time as the discriminator: a registry where every
  // skill genuinely is unused after a long window is a real finding, and an
  // empty map alone must not suppress it.
  it('stays warm on an old window even with an entirely empty usage map', () => {
    const since = new Date(NOW - 30 * 24 * 60 * 60 * 1000).toISOString();
    expect(isUsageWindowCold(usage(since, {}), NOW)).toBe(false);
  });

  it('stays cold on a young window even with recorded calls', () => {
    const since = new Date(NOW - 60 * 1000).toISOString();
    expect(isUsageWindowCold(usage(since, { a: { calls: 4, lastCalledAt: null } }), NOW)).toBe(true);
  });
});

describe('coldWindowCaveat', () => {
  it('returns null when the window is warm', () => {
    const since = new Date(NOW - MIN_OBSERVATION_MS - 1000).toISOString();
    expect(coldWindowCaveat(usage(since), NOW)).toBeNull();
  });

  it('names how long tracking has been running when it knows', () => {
    const since = new Date(NOW - 2 * 60 * 60 * 1000).toISOString();
    expect(coldWindowCaveat(usage(since), NOW)).toContain('2 hours ago');
  });

  it('falls back to a no-start-recorded explanation when observedSince is absent', () => {
    expect(coldWindowCaveat(usage(null), NOW)).toContain('no recorded start');
  });
});
