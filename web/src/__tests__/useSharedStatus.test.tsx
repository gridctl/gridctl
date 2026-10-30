import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { act, renderHook } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { fetchStatus } from '../lib/api';
import { POLLING } from '../lib/constants';
import { STATUS_STALE_MS, useSharedStatus } from '../hooks/useSharedStatus';
import type { BroadcastMessage } from '../hooks/useBroadcastChannel';
import type { GatewayStatus } from '../types';

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return { ...actual, fetchStatus: vi.fn() };
});

const status = {
  gateway: { name: 'main', version: '0' },
  'mcp-servers': [],
  resources: [],
  sessions: 0,
} as GatewayStatus;

function wrapper({ children }: { children: ReactNode }) {
  return <MemoryRouter>{children}</MemoryRouter>;
}

function postSnapshot(channel: BroadcastChannel, at = Date.now()) {
  const message: BroadcastMessage = {
    type: 'STATUS_SNAPSHOT',
    source: 'main',
    payload: { status, at },
    timestamp: at,
  };
  channel.postMessage(message);
}

async function flushMessages() {
  await act(async () => {
    await Promise.resolve();
    vi.advanceTimersByTime(0);
    await Promise.resolve();
  });
}

describe('useSharedStatus', () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('does not fetch status while snapshots keep arriving', async () => {
    vi.useFakeTimers();
    vi.mocked(fetchStatus).mockResolvedValue(status);
    const main = new BroadcastChannel('gridctl-sync');
    const view = renderHook(() => useSharedStatus({ enabled: true }), { wrapper });
    await act(async () => {
      await Promise.resolve();
    });
    postSnapshot(main);
    await flushMessages();
    await act(async () => {
      vi.advanceTimersByTime(50);
      await Promise.resolve();
    });
    expect(view.result.current.source).toBe('broadcast');
    await act(async () => {
      vi.advanceTimersByTime(STATUS_STALE_MS - 100);
    });
    postSnapshot(main, Date.now());
    await flushMessages();
    await act(async () => {
      vi.advanceTimersByTime(STATUS_STALE_MS - 1);
    });
    expect(fetchStatus).not.toHaveBeenCalled();
    view.unmount();
    main.close();
  });

  it('starts a self-poll after the staleness window with no snapshots', async () => {
    vi.useFakeTimers();
    vi.mocked(fetchStatus).mockResolvedValue(status);
    const view = renderHook(() => useSharedStatus({ enabled: true }), { wrapper });
    await act(async () => {
      vi.advanceTimersByTime(STATUS_STALE_MS - 1);
    });
    expect(fetchStatus).not.toHaveBeenCalled();
    await act(async () => {
      vi.advanceTimersByTime(1);
    });
    await flushMessages();
    expect(fetchStatus).toHaveBeenCalledTimes(1);
    expect(view.result.current.source).toBe('self');
    view.unmount();
  });

  it('keeps self-polling after one isolated snapshot', async () => {
    vi.useFakeTimers();
    vi.mocked(fetchStatus).mockResolvedValue(status);
    const main = new BroadcastChannel('gridctl-sync');
    const view = renderHook(() => useSharedStatus({ enabled: true }), { wrapper });
    await act(async () => {
      vi.advanceTimersByTime(STATUS_STALE_MS);
    });
    await flushMessages();
    expect(fetchStatus).toHaveBeenCalledTimes(1);
    postSnapshot(main);
    await flushMessages();
    expect(view.result.current.source).toBe('broadcast');
    await act(async () => {
      vi.advanceTimersByTime(POLLING.STATUS);
      await Promise.resolve();
    });
    expect(fetchStatus).toHaveBeenCalledTimes(2);
    view.unmount();
    main.close();
  });

  it('stops self-polling only after two snapshots inside the staleness window', async () => {
    vi.useFakeTimers();
    vi.mocked(fetchStatus).mockResolvedValue(status);
    const main = new BroadcastChannel('gridctl-sync');
    const view = renderHook(() => useSharedStatus({ enabled: true }), { wrapper });
    await act(async () => {
      vi.advanceTimersByTime(STATUS_STALE_MS);
    });
    await flushMessages();
    expect(fetchStatus).toHaveBeenCalledTimes(1);
    postSnapshot(main);
    await flushMessages();
    await act(async () => {
      vi.advanceTimersByTime(1000);
    });
    postSnapshot(main, Date.now());
    await flushMessages();
    const afterPair = vi.mocked(fetchStatus).mock.calls.length;
    await act(async () => {
      vi.advanceTimersByTime(POLLING.STATUS * 2);
    });
    expect(fetchStatus).toHaveBeenCalledTimes(afterPair);
    expect(view.result.current.source).toBe('broadcast');
    view.unmount();
    main.close();
  });
});
