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
    transport: 'native HTTP',
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

describe('useClientScopeEditor — tool axis', () => {
  const D = '__';
  const UNIVERSE = { github: ['a', 'b'], gitlab: ['x'] };
  const TWO_SERVERS = ['github', 'gitlab'];

  function scoped(servers: string[], tools: string[]): ClientStatus {
    return client({ configured: true, unscoped: false, servers, tools });
  }

  it('seeds All mode from a full-coverage allow-list and stays clean', () => {
    // Saved list enumerates every tool of both servers → intent is "all";
    // the flattened baseline is [] so an untouched editor is never dirty.
    const c = scoped(TWO_SERVERS, [`github${D}a`, `github${D}b`, `gitlab${D}x`]);
    const { result } = renderHook(() => useClientScopeEditor(c, TWO_SERVERS, UNIVERSE));
    expect(result.current.toolMode).toEqual({ github: 'all', gitlab: 'all' });
    expect(result.current.dirty).toBe(false);
    expect(result.current.toolsTouched).toBe(false);
  });

  it('seeds Custom mode from a strict-subset allow-list', () => {
    const c = scoped(TWO_SERVERS, [`github${D}a`, `gitlab${D}x`]);
    const { result } = renderHook(() => useClientScopeEditor(c, TWO_SERVERS, UNIVERSE));
    expect(result.current.toolMode.github).toBe('custom');
    expect(result.current.customSel.github).toEqual(['a']);
    expect(result.current.toolMode.gitlab).toBe('all');
    expect(result.current.dirty).toBe(false);
  });

  it('narrowing one server enumerates the untouched All servers on save', async () => {
    const updateSpy = vi
      .spyOn(apiModule, 'updateClientScope')
      .mockResolvedValue({ client: 'cursor', profileKey: 'cursor', reloaded: true, servers: [], tools: [] } as unknown as Awaited<ReturnType<typeof apiModule.updateClientScope>>);
    vi.spyOn(apiModule, 'fetchClients').mockResolvedValue([]);
    vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({
      gateway: { name: 'g', version: '1' },
      'mcp-servers': [],
    } as unknown as Awaited<ReturnType<typeof apiModule.fetchStatus>>);

    const c = scoped(TWO_SERVERS, [`github${D}a`, `github${D}b`, `gitlab${D}x`]);
    const { result } = renderHook(() => useClientScopeEditor(c, TWO_SERVERS, UNIVERSE));

    // Seeded customSel carries the full set; switching to Custom and dropping
    // one tool narrows github to ['b'].
    act(() => result.current.setServerToolMode('github', 'custom'));
    act(() => result.current.toggleTool('github', 'a'));
    expect(result.current.dirty).toBe(true);

    await act(() => result.current.save());

    // The global allow-list must carry gitlab's full set or it would vanish.
    expect(updateSpy).toHaveBeenCalledWith('cursor', {
      servers: ['github', 'gitlab'],
      tools: [`github${D}b`, `gitlab${D}x`],
    });
  });

  it('omits the tool axis on save when untouched (preserves stack.yaml lists)', async () => {
    const updateSpy = vi
      .spyOn(apiModule, 'updateClientScope')
      .mockResolvedValue({ client: 'cursor', profileKey: 'cursor', reloaded: true, servers: [], tools: [] } as unknown as Awaited<ReturnType<typeof apiModule.updateClientScope>>);
    vi.spyOn(apiModule, 'fetchClients').mockResolvedValue([]);
    vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({
      gateway: { name: 'g', version: '1' },
      'mcp-servers': [],
    } as unknown as Awaited<ReturnType<typeof apiModule.fetchStatus>>);

    const c = scoped(TWO_SERVERS, [`github${D}a`, `gitlab${D}x`]);
    const { result } = renderHook(() => useClientScopeEditor(c, TWO_SERVERS, UNIVERSE));

    // A server-only change with an untouched tool axis.
    act(() => result.current.toggle('gitlab'));
    await act(() => result.current.save());

    expect(updateSpy).toHaveBeenCalledWith('cursor', { servers: ['github'] });
  });

  it('blocks save while a granted server has an empty Custom selection', () => {
    const c = scoped(TWO_SERVERS, [`github${D}a`, `github${D}b`, `gitlab${D}x`]);
    const { result } = renderHook(() => useClientScopeEditor(c, TWO_SERVERS, UNIVERSE));

    act(() => result.current.setServerToolMode('github', 'custom'));
    act(() => result.current.clearTools('github', ['a', 'b']));
    expect(result.current.emptyCustomGrant).toBe(true);
    expect(result.current.canSave).toBe(false);
  });

  it('reset restores the seeded tool intent', () => {
    const c = scoped(TWO_SERVERS, [`github${D}a`, `gitlab${D}x`]);
    const { result } = renderHook(() => useClientScopeEditor(c, TWO_SERVERS, UNIVERSE));

    act(() => result.current.setServerToolMode('github', 'all'));
    act(() => result.current.toggleTool('gitlab', 'x'));
    expect(result.current.dirty).toBe(true);

    act(() => result.current.reset());
    expect(result.current.toolMode.github).toBe('custom');
    expect(result.current.customSel.github).toEqual(['a']);
    expect(result.current.dirty).toBe(false);
  });
});

describe('useClientScopeEditor — tool axis guards', () => {
  const D = '__';
  const UNIVERSE = { github: ['a', 'b'], gitlab: ['x'] };
  const TWO_SERVERS = ['github', 'gitlab'];

  function scoped(servers: string[], tools: string[]): ClientStatus {
    return client({ configured: true, unscoped: false, servers, tools });
  }

  it('blocks a tool-axis save while a granted server has no reported universe', () => {
    // gitlab has not reported tools yet: its universe is empty, so flattening
    // a github narrowing would enumerate zero gitlab tools and hide it.
    const partial = { github: ['a', 'b'], gitlab: [] as string[] };
    const c = scoped(TWO_SERVERS, [`github${D}a`, `github${D}b`]);
    const { result } = renderHook(() =>
      useClientScopeEditor(c, TWO_SERVERS, partial, ['gitlab']),
    );

    act(() => result.current.setServerToolMode('github', 'custom'));
    act(() => result.current.toggleTool('github', 'a'));

    expect(result.current.toolAxisBlocked).toBe(true);
    expect(result.current.canSave).toBe(false);
  });

  it('omits the tool axis when edits were reverted (touched but not dirty)', async () => {
    const updateSpy = vi
      .spyOn(apiModule, 'updateClientScope')
      .mockResolvedValue({ client: 'cursor', profileKey: 'cursor', reloaded: true } as unknown as Awaited<ReturnType<typeof apiModule.updateClientScope>>);
    vi.spyOn(apiModule, 'fetchClients').mockResolvedValue([]);
    vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({
      gateway: { name: 'g', version: '1' },
      'mcp-servers': [],
    } as unknown as Awaited<ReturnType<typeof apiModule.fetchStatus>>);

    const c = scoped(TWO_SERVERS, [`github${D}a`, `github${D}b`, `gitlab${D}x`]);
    const { result } = renderHook(() => useClientScopeEditor(c, TWO_SERVERS, UNIVERSE));

    // Toggle a tool off and back on: touched, but the flattened intent equals
    // the baseline, so the axis must be omitted (replacing a stack.yaml
    // enumeration with [] would widen future tools into scope).
    act(() => result.current.setServerToolMode('github', 'custom'));
    act(() => result.current.toggleTool('github', 'a'));
    act(() => result.current.toggleTool('github', 'a'));
    act(() => result.current.setServerToolMode('github', 'all'));
    act(() => result.current.toggle('gitlab'));

    await act(() => result.current.save());
    expect(updateSpy).toHaveBeenCalledWith('cursor', { servers: ['github'] });
  });
});
