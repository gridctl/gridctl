import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useInterval } from '../hooks/useInterval';

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, 'visibilityState', {
    configurable: true,
    get: () => state,
  });
  document.dispatchEvent(new Event('visibilitychange'));
}

async function flush() {
  await act(async () => {
    await Promise.resolve();
  });
}

describe('useInterval', () => {
  afterEach(() => {
    vi.useRealTimers();
    setVisibility('visible');
  });

  it('fires on mount and then on the interval while visible', async () => {
    vi.useFakeTimers();
    const callback = vi.fn();
    renderHook(() => useInterval(callback, 1000));
    await flush();
    expect(callback).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it('does not fire when disabled, and fires when enabled becomes true', async () => {
    vi.useFakeTimers();
    const callback = vi.fn();
    const { rerender } = renderHook(
      ({ enabled }) => useInterval(callback, 1000, { enabled }),
      { initialProps: { enabled: false } },
    );
    await flush();
    await act(async () => {
      vi.advanceTimersByTime(5000);
    });
    expect(callback).not.toHaveBeenCalled();
    rerender({ enabled: true });
    await flush();
    expect(callback).toHaveBeenCalledTimes(1);
  });

  it('pauses while hidden and catches up only when the last run is stale', async () => {
    vi.useFakeTimers();
    const callback = vi.fn();
    renderHook(() => useInterval(callback, 1000));
    await flush();
    expect(callback).toHaveBeenCalledTimes(1);

    await act(async () => {
      vi.advanceTimersByTime(200);
      setVisibility('hidden');
    });
    expect(callback).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(5000);
    });
    expect(callback).toHaveBeenCalledTimes(1);

    await act(async () => {
      setVisibility('visible');
    });
    await flush();
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it('schedules the remainder when a visibility return is still fresh', async () => {
    vi.useFakeTimers();
    const callback = vi.fn();
    renderHook(() => useInterval(callback, 1000));
    await flush();

    await act(async () => {
      vi.advanceTimersByTime(400);
      setVisibility('hidden');
      setVisibility('visible');
    });
    expect(callback).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(599);
    });
    expect(callback).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it('does not double-fire when a tick and a visibility event coincide', async () => {
    vi.useFakeTimers();
    const callback = vi.fn();
    renderHook(() => useInterval(callback, 1000));
    await flush();

    await act(async () => {
      vi.advanceTimersByTime(1000);
      setVisibility('hidden');
      setVisibility('visible');
    });
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it('uses the hidden heartbeat and does not fire on the hide transition when fresh', async () => {
    vi.useFakeTimers();
    const callback = vi.fn();
    renderHook(() => useInterval(callback, 1000, { hiddenIntervalMs: 30000 }));
    await flush();

    await act(async () => {
      setVisibility('hidden');
    });
    expect(callback).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(29999);
    });
    expect(callback).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it('fires on hide only when the last run is already older than the heartbeat', async () => {
    vi.useFakeTimers();
    const callback = vi.fn();
    renderHook(() => useInterval(callback, 60000, { hiddenIntervalMs: 30000 }));
    await flush();
    expect(callback).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(30000);
      setVisibility('hidden');
    });
    await flush();
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it('skips a tick while the previous callback is still pending', async () => {
    vi.useFakeTimers();
    let release: () => void = () => {};
    const callback = vi.fn(() => new Promise<void>((resolve) => {
      release = resolve;
    }));
    renderHook(() => useInterval(callback, 1000));
    await flush();
    expect(callback).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(callback).toHaveBeenCalledTimes(1);
    await act(async () => {
      release();
      await Promise.resolve();
    });
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it('keeps firing after a callback rejects once', async () => {
    vi.useFakeTimers();
    const callback = vi.fn()
      .mockRejectedValueOnce(new Error('boom'))
      .mockResolvedValue(undefined);
    renderHook(() => useInterval(callback, 1000));
    await flush();
    expect(callback).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it('keeps firing after a callback throws once', async () => {
    vi.useFakeTimers();
    const callback = vi.fn()
      .mockImplementationOnce(() => {
        throw new Error('boom');
      });
    renderHook(() => useInterval(callback, 1000));
    await flush();
    expect(callback).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    expect(callback).toHaveBeenCalledTimes(2);
  });

  it('does not immediate-fire when only the interval changes', async () => {
    vi.useFakeTimers();
    const callback = vi.fn();
    const { rerender } = renderHook(
      ({ intervalMs }) => useInterval(callback, intervalMs),
      { initialProps: { intervalMs: 1000 } },
    );
    await flush();
    expect(callback).toHaveBeenCalledTimes(1);
    rerender({ intervalMs: 5000 });
    await flush();
    expect(callback).toHaveBeenCalledTimes(1);
    await act(async () => {
      vi.advanceTimersByTime(5000);
    });
    expect(callback).toHaveBeenCalledTimes(2);
  });
});
