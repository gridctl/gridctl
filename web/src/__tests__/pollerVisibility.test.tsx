import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { fetchGatewayLogs, fetchToolUsage } from '../lib/api';
import { POLLING } from '../lib/constants';
import { useLogStream } from '../hooks/useLogStream';
import { useToolUsage } from '../hooks/useToolUsage';

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchToolUsage: vi.fn(),
    fetchGatewayLogs: vi.fn(),
  };
});

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

describe('workspace pollers pause while hidden', () => {
  afterEach(() => {
    vi.useRealTimers();
    setVisibility('visible');
    vi.restoreAllMocks();
  });

  it('useToolUsage pauses while hidden and resumes on return', async () => {
    vi.useFakeTimers();
    vi.mocked(fetchToolUsage).mockResolvedValue({ servers: {} });
    renderHook(() => useToolUsage(true));
    await flush();
    expect(fetchToolUsage).toHaveBeenCalledTimes(1);

    await act(async () => {
      setVisibility('hidden');
    });
    await act(async () => {
      vi.advanceTimersByTime(60000);
    });
    expect(fetchToolUsage).toHaveBeenCalledTimes(1);

    await act(async () => {
      setVisibility('visible');
    });
    await flush();
    expect(fetchToolUsage).toHaveBeenCalledTimes(2);
  });

  it('useLogStream pauses while hidden and resumes on return', async () => {
    vi.useFakeTimers();
    vi.mocked(fetchGatewayLogs).mockResolvedValue({ logs: [], total: 0, bufferCapacity: 100 });
    renderHook(() => useLogStream({ active: true }));
    await flush();
    expect(fetchGatewayLogs).toHaveBeenCalledTimes(1);

    await act(async () => {
      setVisibility('hidden');
    });
    await act(async () => {
      vi.advanceTimersByTime(POLLING.LOGS * 5);
    });
    expect(fetchGatewayLogs).toHaveBeenCalledTimes(1);

    await act(async () => {
      setVisibility('visible');
    });
    await flush();
    expect(fetchGatewayLogs).toHaveBeenCalledTimes(2);
  });
});
