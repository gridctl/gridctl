import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import {
  useClientScopeEditor,
  baselineServers,
} from '../hooks/useClientScopeEditor';
import type { ClientStatus } from '../types';
import * as apiModule from '../lib/api';
import { ClientScopeError } from '../lib/api';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

const mockStoreState = {
  setClients: vi.fn(),
  setGatewayStatus: vi.fn(),
};

vi.mock('../stores/useStackStore', () => ({
  useStackStore: Object.assign(vi.fn(), { getState: () => mockStoreState }),
}));

const ALL_SERVERS = ['github', 'gitlab', 'atlassian'];

function client(scope?: ClientStatus['effectiveScope']): ClientStatus {
  return {
    name: 'Cursor',
    slug: 'cursor',
    detected: true,
    linked: true,
    transport: 'native SSE',
    effectiveScope: scope,
  };
}

beforeEach(() => {
  mockStoreState.setClients.mockReset();
  mockStoreState.setGatewayStatus.mockReset();
  vi.restoreAllMocks();
});

describe('baselineServers', () => {
  it('returns all servers for an unscoped (no-block) client', () => {
    expect(baselineServers(client(undefined), ALL_SERVERS)).toEqual([
      'atlassian',
      'github',
      'gitlab',
    ]);
  });

  it('returns all servers when the scope is configured but unscoped', () => {
    const c = client({ configured: true, unscoped: true, servers: [], tools: [] });
    expect(baselineServers(c, ALL_SERVERS)).toEqual(['atlassian', 'github', 'gitlab']);
  });

  it('returns the scoped servers for a narrowed client', () => {
    const c = client({ configured: true, unscoped: false, servers: ['github'], tools: [] });
    expect(baselineServers(c, ALL_SERVERS)).toEqual(['github']);
  });

  it('returns nothing for a default-deny unlisted client', () => {
    const c = client({ configured: true, unscoped: false, servers: [], tools: [] });
    expect(baselineServers(c, ALL_SERVERS)).toEqual([]);
  });
});

describe('useClientScopeEditor', () => {
  it('seeds the selection from the client baseline and is not dirty', () => {
    const c = client({ configured: true, unscoped: false, servers: ['github'], tools: [] });
    const { result } = renderHook(() => useClientScopeEditor(c, ALL_SERVERS));
    expect([...result.current.selected].sort()).toEqual(['github']);
    expect(result.current.dirty).toBe(false);
  });

  it('marks createsBlock when no clients block is configured yet', () => {
    const { result } = renderHook(() => useClientScopeEditor(client(undefined), ALL_SERVERS));
    expect(result.current.createsBlock).toBe(true);
  });

  it('becomes dirty on toggle and clears when reverted', () => {
    const c = client({ configured: true, unscoped: false, servers: ['github'], tools: [] });
    const { result } = renderHook(() => useClientScopeEditor(c, ALL_SERVERS));

    act(() => result.current.toggle('gitlab'));
    expect(result.current.dirty).toBe(true);
    expect(result.current.selected.has('gitlab')).toBe(true);

    act(() => result.current.toggle('gitlab'));
    expect(result.current.dirty).toBe(false);
  });

  it('saves the selected servers as a server-level profile and refreshes', async () => {
    const c = client({ configured: true, unscoped: false, servers: ['github'], tools: [] });
    const update = vi
      .spyOn(apiModule, 'updateClientScope')
      .mockResolvedValue({
        client: 'cursor',
        profileKey: 'cursor',
        servers: ['github', 'gitlab'],
        tools: [],
        reloaded: true,
      });
    vi.spyOn(apiModule, 'fetchClients').mockResolvedValue([]);
    vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({} as never);

    const { result } = renderHook(() => useClientScopeEditor(c, ALL_SERVERS));
    act(() => result.current.toggle('gitlab'));
    await act(async () => {
      await result.current.save();
    });

    // The server-level editor omits the tools axis so an existing tool
    // allow-list on the profile is preserved.
    expect(update).toHaveBeenCalledWith('cursor', { servers: ['github', 'gitlab'] });
    await waitFor(() => expect(mockStoreState.setClients).toHaveBeenCalled());
  });

  it('cannot save with zero servers selected (empty means all, not none)', () => {
    const c = client({ configured: true, unscoped: false, servers: ['github'], tools: [] });
    const { result } = renderHook(() => useClientScopeEditor(c, ALL_SERVERS));
    act(() => result.current.clearAll());
    expect(result.current.selected.size).toBe(0);
    expect(result.current.dirty).toBe(true);
    expect(result.current.canSave).toBe(false);
  });

  it('surfaces a 409 conflict instead of throwing', async () => {
    const c = client({ configured: true, unscoped: false, servers: ['github'], tools: [] });
    vi.spyOn(apiModule, 'updateClientScope').mockRejectedValue(
      new ClientScopeError('stack_modified', 'changed on disk', 'reload it', 409),
    );

    const { result } = renderHook(() => useClientScopeEditor(c, ALL_SERVERS));
    act(() => result.current.toggle('gitlab'));
    await act(async () => {
      await result.current.save();
    });

    expect(result.current.conflict).toBeTruthy();
    expect(result.current.isSaving).toBe(false);
  });
});
