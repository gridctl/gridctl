import { describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { useLoadedEvidence } from '../hooks/useLoadedEvidence';

describe('useLoadedEvidence', () => {
  it('is idle and fetches nothing when there is no key', () => {
    const fetcher = vi.fn(() => Promise.resolve('diff'));
    const { result } = renderHook(() => useLoadedEvidence(null, fetcher));
    expect(result.current.status).toBe('idle');
    expect(fetcher).not.toHaveBeenCalled();
  });

  it('moves from loading to ready once the key has a result', async () => {
    const fetcher = vi.fn(() => Promise.resolve('diff'));
    const { result } = renderHook(() => useLoadedEvidence('a', fetcher));
    expect(result.current.status).toBe('loading');
    await waitFor(() => expect(result.current).toMatchObject({ status: 'ready', text: 'diff' }));
  });
});
