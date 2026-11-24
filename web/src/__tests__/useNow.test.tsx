import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, render } from '@testing-library/react';
import { useNow } from '../hooks/useNow';

const NOW = new Date('2026-11-24T12:00:00Z').getTime();

function setup(intervalMs: number) {
  const values: number[] = [];
  function Probe() {
    values.push(useNow(intervalMs));
    return null;
  }
  return { values, ...render(<Probe />) };
}

describe('useNow', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('starts at the mount time, then advances once per interval with one render per tick', async () => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
    const { values, unmount } = setup(1000);
    expect(values).toEqual([NOW]);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(values).toEqual([NOW, NOW + 1000]);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(1000);
    });
    expect(values).toEqual([NOW, NOW + 1000, NOW + 2000]);

    unmount();
    expect(vi.getTimerCount()).toBe(0);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    expect(values).toHaveLength(3);
  });
});
