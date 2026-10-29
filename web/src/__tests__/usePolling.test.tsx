import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { renderHook, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { requestGatewayPoll, usePolling } from '../hooks/usePolling';
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
  }

  it('keeps the shell callback after a non-registering instance unmounts', async () => {
    stubPoll();
    useAuthStore.setState({ authRequired: false, generation: Symbol() });
    const first = renderHook(() => usePolling({ registerRetry: true }), { wrapper });
    const second = renderHook(() => usePolling(), { wrapper });
    await waitFor(() => expect(api.fetchStatus).toHaveBeenCalledTimes(2));
    second.unmount();
    requestGatewayPoll();
    await waitFor(() => expect(api.fetchStatus).toHaveBeenCalledTimes(3));
    first.unmount();
    const after = vi.mocked(api.fetchStatus).mock.calls.length;
    requestGatewayPoll();
    expect(api.fetchStatus).toHaveBeenCalledTimes(after);
  });

  it('propagates a status connection failure to pins and the registry', async () => {
    stubPoll();
    vi.mocked(api.fetchStatus).mockRejectedValue(new GatewayRequestError('connection', 'down'));
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
});
