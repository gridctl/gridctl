import { describe, it, expect } from 'vitest';
import {
  coldWindowCaveat,
  isStaleUsage,
  isUsageWindowCold,
  isWindowLongerThanTracking,
  staleUnknownReason,
  MIN_OBSERVATION_MS,
} from '../lib/skillUsage';
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

const DAY = 24 * 60 * 60 * 1000;

describe('isWindowLongerThanTracking', () => {
  it('is true when nothing was returned', () => {
    expect(isWindowLongerThanTracking(null, 7 * DAY, NOW)).toBe(true);
  });

  it('is true when observedSince is missing or unparseable', () => {
    expect(isWindowLongerThanTracking(usage(null), 7 * DAY, NOW)).toBe(true);
    expect(isWindowLongerThanTracking(usage('nope'), 7 * DAY, NOW)).toBe(true);
  });

  // The whole point: three days of tracking cannot answer a 30-day question.
  it('is true when the window outruns the tracking period', () => {
    const since = new Date(NOW - 3 * DAY).toISOString();
    expect(isWindowLongerThanTracking(usage(since), 30 * DAY, NOW)).toBe(true);
  });

  it('is false once tracking covers the window', () => {
    const since = new Date(NOW - 40 * DAY).toISOString();
    expect(isWindowLongerThanTracking(usage(since), 30 * DAY, NOW)).toBe(false);
  });
});

describe('isStaleUsage', () => {
  const stat = (calls: number, lastCalledAt: string | null) => ({ calls, lastCalledAt });

  it('is true for a call older than the window', () => {
    expect(isStaleUsage(stat(3, new Date(NOW - 10 * DAY).toISOString()), 7 * DAY, NOW)).toBe(true);
  });

  it('is false for a call inside the window', () => {
    expect(isStaleUsage(stat(3, new Date(NOW - 2 * DAY).toISOString()), 7 * DAY, NOW)).toBe(false);
  });

  // Never-used is the other facet; folding it in here would double-count.
  it('is false for a skill with no recorded calls', () => {
    expect(isStaleUsage(undefined, 7 * DAY, NOW)).toBe(false);
    expect(isStaleUsage(stat(0, null), 7 * DAY, NOW)).toBe(false);
  });

  // "Used, when is unknown" must not be guessed as old.
  it('is false when the call has no usable timestamp', () => {
    expect(isStaleUsage(stat(3, null), 7 * DAY, NOW)).toBe(false);
    expect(isStaleUsage(stat(3, 'not-a-date'), 7 * DAY, NOW)).toBe(false);
  });
});

describe('staleUnknownReason', () => {
  it('is null when the question is answerable', () => {
    const since = new Date(NOW - 40 * DAY).toISOString();
    expect(staleUnknownReason(usage(since), 30 * DAY, '30 days', NOW)).toBeNull();
  });

  it('reports the cold window first, since that is the more basic problem', () => {
    const since = new Date(NOW - 60 * 1000).toISOString();
    expect(staleUnknownReason(usage(since), 30 * DAY, '30 days', NOW)).toMatch(/less than a day/i);
  });

  it('names the window when tracking is warm but too short for it', () => {
    const since = new Date(NOW - 3 * DAY).toISOString();
    const reason = staleUnknownReason(usage(since), 30 * DAY, '30 days', NOW);
    expect(reason).toMatch(/less than 30 days/i);
    expect(reason).toMatch(/shorter window/i);
  });
});
