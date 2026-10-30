import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { act, renderHook, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { requestGatewayPoll, usePolling, usePollingRefresh } from '../hooks/usePolling';
import * as api from '../lib/api';
import { HTTPError } from '../lib/api';
import { GatewayRequestError } from '../lib/gatewayRequest';
import { useAuthStore } from '../stores/useAuthStore';
import { usePinsStore } from '../stores/usePinsStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import { useStackStore } from '../stores/useStackStore';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

function wrapper({ children }: { children: ReactNode }) {
  return <MemoryRouter>{children}</MemoryRouter>;
}

const quietStatus = {
  gateway: { name: 'test', version: '0' },
  'mcp-servers': [],
  resources: [],
  sessions: 0,
} as Awaited<ReturnType<typeof api.fetchStatus>>;

describe('usePolling retry registration', () => {
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    useAuthStore.setState({ authRequired: false, generation: Symbol() });
    usePinsStore.setState({ pins: null, skillPins: null, pinsError: null, skillPinsError: null });
    useRegistryStore.setState({ skills: null, error: null });
    useStackStore.setState({ error: null, isLoading: false });
  });

  function stubPoll() {
    vi.spyOn(api, 'fetchStatus').mockResolvedValue(quietStatus);
    vi.spyOn(api, 'fetchTools').mockResolvedValue({ tools: [] });
    vi.spyOn(api, 'fetchToolCatalog').mockResolvedValue({ tools: [] });
    vi.spyOn(api, 'fetchClients').mockResolvedValue([]);
    vi.spyOn(api, 'fetchServerPins').mockResolvedValue({});
    vi.spyOn(api, 'fetchSkillPins').mockResolvedValue({});
    vi.spyOn(api, 'fetchRegistryStatus').mockResolvedValue({ totalSkills: 0, activeSkills: 0 });
    vi.spyOn(api, 'fetchRegistrySkills').mockResolvedValue([]);
    vi.spyOn(api, 'fetchSkillSources').mockResolvedValue([]);
    vi.spyOn(api, 'getTelemetryInventory').mockResolvedValue([]);
    vi.spyOn(api, 'fetchStackSpec').mockResolvedValue({ content: '', path: '' });
    vi.spyOn(api, 'fetchStackHealth').mockResolvedValue({
      validation: { status: 'valid', errorCount: 0, warningCount: 0 },
      drift: { status: 'in-sync' },
      dependencies: { status: 'resolved' },
    });
  }

  it('keeps the shell callback after a refresh accessor unmounts', async () => {
    stubPoll();
    useAuthStore.setState({ authRequired: false, generation: Symbol() });
    const first = renderHook(() => usePolling({ registerRetry: true }), { wrapper });
    const second = renderHook(() => usePollingRefresh(), { wrapper });
    await waitFor(() => expect(api.fetchStatus).toHaveBeenCalledTimes(1));
    expect(api.fetchToolCatalog).toHaveBeenCalledTimes(1);
    second.unmount();
    requestGatewayPoll();
    await waitFor(() => expect(api.fetchStatus).toHaveBeenCalledTimes(2));
    first.unmount();
    const after = vi.mocked(api.fetchStatus).mock.calls.length;
    requestGatewayPoll();
    expect(api.fetchStatus).toHaveBeenCalledTimes(after);
  });

  it('propagates a status connection failure to pins and the registry', async () => {
    stubPoll();
    const down = new GatewayRequestError('connection', 'down');
    vi.mocked(api.fetchStatus).mockRejectedValue(down);
    vi.mocked(api.fetchServerPins).mockRejectedValue(down);
    vi.mocked(api.fetchSkillPins).mockRejectedValue(down);
    vi.mocked(api.fetchRegistryStatus).mockRejectedValue(down);
    vi.mocked(api.fetchRegistrySkills).mockRejectedValue(down);
    useAuthStore.setState({ authRequired: false, generation: Symbol() });
    const view = renderHook(() => usePolling({ registerRetry: true }), { wrapper });
    await waitFor(() => {
      expect(useStackStore.getState().error).toBe('Gateway connection unavailable');
    });
    expect(usePinsStore.getState().pinsError).toBe('Gateway connection unavailable');
    expect(usePinsStore.getState().skillPinsError).toBe('Gateway connection unavailable');
    expect(useRegistryStore.getState().error).toBe('Gateway connection unavailable');
    view.unmount();
  });

  it('treats a 503 registry response as unconfigured, not a failure', async () => {
    stubPoll();
    vi.mocked(api.fetchRegistryStatus).mockRejectedValue(new HTTPError(503, 'API error: 503 Service Unavailable'));
    vi.mocked(api.fetchRegistrySkills).mockRejectedValue(new HTTPError(503, 'API error: 503 Service Unavailable'));
    useAuthStore.setState({ authRequired: false, generation: Symbol() });
    const view = renderHook(() => usePolling({ registerRetry: true }), { wrapper });
    await waitFor(() => expect(useRegistryStore.getState().skills).toEqual([]));
    expect(useRegistryStore.getState().error).toBeNull();
    view.unmount();
  });

  it('records a server-pins failure without clearing a successful skill-pins fetch', async () => {
    stubPoll();
    vi.mocked(api.fetchServerPins).mockRejectedValue(new Error('pins down'));
    useAuthStore.setState({ authRequired: false, generation: Symbol() });
    const view = renderHook(() => usePolling({ registerRetry: true }), { wrapper });
    await waitFor(() => {
      expect(usePinsStore.getState().pinsError).toBe('pins down');
      expect(usePinsStore.getState().skillPins).toEqual({});
    });
    expect(usePinsStore.getState().pins).toBeNull();
    expect(usePinsStore.getState().skillPinsError).toBeNull();
    view.unmount();
  });

  it('records a skill-pins failure without clearing a successful server-pins fetch', async () => {
    stubPoll();
    vi.mocked(api.fetchSkillPins).mockRejectedValue(new Error('skill pins down'));
    useAuthStore.setState({ authRequired: false, generation: Symbol() });
    const view = renderHook(() => usePolling({ registerRetry: true }), { wrapper });
    await waitFor(() => {
      expect(usePinsStore.getState().skillPinsError).toBe('skill pins down');
      expect(usePinsStore.getState().pins).toEqual({});
    });
    expect(usePinsStore.getState().skillPins).toBeNull();
    expect(usePinsStore.getState().pinsError).toBeNull();
    view.unmount();
  });

  it('does not broadcast a hidden heartbeat', async () => {
    vi.useFakeTimers();
    stubPoll();
    useAuthStore.setState({ authRequired: false, generation: Symbol() });
    const snaps: number[] = [];
    const channel = new BroadcastChannel('gridctl-sync');
    channel.onmessage = (event) => {
      if (event.data?.type === 'STATUS_SNAPSHOT') snaps.push(1);
    };
    const view = renderHook(() => usePolling(), { wrapper });
    await act(async () => {
      await Promise.resolve();
      vi.advanceTimersByTime(20);
      await Promise.resolve();
    });
    expect(snaps.length).toBeGreaterThan(0);
    const afterVisible = snaps.length;
    Object.defineProperty(document, 'visibilityState', {
      configurable: true,
      get: () => 'hidden',
    });
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'));
      await Promise.resolve();
    });
    await act(async () => {
      vi.advanceTimersByTime(30000);
      await Promise.resolve();
    });
    expect(snaps.length).toBe(afterVisible);
    expect(vi.mocked(api.fetchStatus).mock.calls.length).toBeGreaterThan(1);
    view.unmount();
    channel.close();
  });
});
