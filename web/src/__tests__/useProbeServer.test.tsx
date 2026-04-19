import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import * as api from '../lib/api';
import { ProbeError } from '../lib/api';
import { useProbeServer } from '../hooks/useProbeServer';

describe('useProbeServer', () => {
  beforeEach(() => {
    vi.spyOn(api, 'probeServer');
  });
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('starts idle', () => {
    const { result } = renderHook(() => useProbeServer());
    expect(result.current.loading).toBe(false);
    expect(result.current.tools).toBeNull();
    expect(result.current.error).toBeNull();
  });

  it('transitions loading -> success and stores tools', async () => {
    const mock = vi.mocked(api.probeServer);
    mock.mockResolvedValueOnce({
      tools: [{ name: 'hello', description: '', inputSchema: {} }],
      probedAt: '2025-01-01T00:00:00Z',
      cached: false,
    });
    const { result } = renderHook(() => useProbeServer());

    await act(async () => {
      await result.current.probe({ url: 'https://example.com/mcp' });
    });

    expect(mock).toHaveBeenCalledTimes(1);
    expect(result.current.loading).toBe(false);
    expect(result.current.tools).toHaveLength(1);
    expect(result.current.error).toBeNull();
  });

  it('transitions loading -> error and stores the error', async () => {
    vi.mocked(api.probeServer).mockRejectedValueOnce(
      new ProbeError('probe_timeout', 'timed out', undefined, 422),
    );
    const { result } = renderHook(() => useProbeServer());

    await act(async () => {
      await result.current.probe({ url: 'https://example.com/mcp' });
    });

    expect(result.current.loading).toBe(false);
    expect(result.current.tools).toBeNull();
    expect(result.current.error).toBeInstanceOf(ProbeError);
    expect((result.current.error as ProbeError).code).toBe('probe_timeout');
  });

  it('reset clears state', async () => {
    vi.mocked(api.probeServer).mockResolvedValueOnce({
      tools: [{ name: 'a', description: '', inputSchema: {} }],
      probedAt: '2025-01-01T00:00:00Z',
      cached: false,
    });
    const { result } = renderHook(() => useProbeServer());
    await act(async () => {
      await result.current.probe({ url: 'https://example.com/mcp' });
    });
    act(() => result.current.reset());
    await waitFor(() => expect(result.current.tools).toBeNull());
  });

  it('last probe wins when called twice in quick succession', async () => {
    // First promise: deferred. Second: resolves immediately with different
    // data. The hook should reflect the second call's result.
    let resolveFirst: (v: api.ProbeSuccess) => void = () => {};
    const firstPromise = new Promise<api.ProbeSuccess>((res) => {
      resolveFirst = res;
    });
    vi.mocked(api.probeServer)
      .mockReturnValueOnce(firstPromise)
      .mockResolvedValueOnce({
        tools: [{ name: 'second', description: '', inputSchema: {} }],
        probedAt: '2025-01-01T00:00:00Z',
        cached: false,
      });

    const { result } = renderHook(() => useProbeServer());
    await act(async () => {
      // Kick off the first (it will hang).
      void result.current.probe({ url: 'https://a.example/mcp' });
      // Then the second call — this is the one we expect to show up in state.
      await result.current.probe({ url: 'https://b.example/mcp' });
    });
    expect(result.current.tools?.[0]?.name).toBe('second');

    // Now resolve the first call. The hook must not overwrite the second
    // result with stale data.
    await act(async () => {
      resolveFirst({
        tools: [{ name: 'first', description: '', inputSchema: {} }],
        probedAt: '2025-01-01T00:00:00Z',
        cached: false,
      });
    });
    expect(result.current.tools?.[0]?.name).toBe('second');
  });
});
