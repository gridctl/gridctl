import { describe, it, expect, afterEach } from 'vitest';
import { motionDuration, prefersReducedMotion } from '../lib/motion';

function stubMatchMedia(matches: (query: string) => boolean) {
  window.matchMedia = ((query: string) => ({
    matches: matches(query),
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia;
}

describe('motionDuration', () => {
  afterEach(() => {
    delete (window as { matchMedia?: unknown }).matchMedia;
  });

  it('returns the input when matchMedia is missing', () => {
    delete (window as { matchMedia?: unknown }).matchMedia;
    expect(prefersReducedMotion()).toBe(false);
    expect(motionDuration(400)).toBe(400);
  });

  it('returns the input when the reduced-motion query does not match', () => {
    stubMatchMedia(() => false);
    expect(motionDuration(300)).toBe(300);
  });

  it('returns 0 when the reduced-motion query matches', () => {
    stubMatchMedia((query) => query === '(prefers-reduced-motion: reduce)');
    expect(prefersReducedMotion()).toBe(true);
    expect(motionDuration(300)).toBe(0);
  });

  it('reads the preference at call time', () => {
    let reduce = false;
    stubMatchMedia(() => reduce);
    expect(motionDuration(200)).toBe(200);
    reduce = true;
    expect(motionDuration(200)).toBe(0);
  });
});
