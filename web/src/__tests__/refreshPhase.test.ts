import { describe, it, expect } from 'vitest';
import { refreshPhase } from '../lib/refreshPhase';

describe('refreshPhase', () => {
  it('is loading before any load completes and nothing has failed', () => {
    expect(refreshPhase({ error: null, hasLoaded: false })).toBe('loading');
    expect(refreshPhase({ error: undefined, hasLoaded: false })).toBe('loading');
  });

  it('is failed when a load fails and nothing is loaded', () => {
    expect(refreshPhase({ error: 'boom', hasLoaded: false })).toBe('failed');
  });

  it('is stale when a refresh fails after a successful load', () => {
    expect(refreshPhase({ error: 'boom', hasLoaded: true })).toBe('stale');
  });

  it('is ready when loaded with no error', () => {
    expect(refreshPhase({ error: null, hasLoaded: true })).toBe('ready');
    expect(refreshPhase({ error: '', hasLoaded: true })).toBe('ready');
  });
});
