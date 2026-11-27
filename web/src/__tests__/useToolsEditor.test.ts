import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { toWireWhitelist, useToolsEditor } from '../hooks/useToolsEditor';
import { TOOL_NAME_DELIMITER } from '../lib/constants';
import type { Tool } from '../types';
import * as apiModule from '../lib/api';
import { SetServerToolsError } from '../lib/api';
import { showToast } from '../components/ui/Toast';

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
}));

const mockStoreState: {
  tools: Tool[];
  toolCatalog: Tool[];
  setGatewayStatus: ReturnType<typeof vi.fn>;
  setTools: ReturnType<typeof vi.fn>;
  setToolCatalog: ReturnType<typeof vi.fn>;
  selectNode: ReturnType<typeof vi.fn>;
} = {
  tools: [],
  toolCatalog: [],
  setGatewayStatus: vi.fn(),
  setTools: vi.fn(),
  setToolCatalog: vi.fn(),
  selectNode: vi.fn(),
};

vi.mock('../stores/useStackStore', () => ({
  useStackStore: Object.assign(
    vi.fn((selector: (s: { tools: Tool[]; toolCatalog: Tool[] }) => unknown) =>
      selector(mockStoreState),
    ),
    {
      getState: () => mockStoreState,
    },
  ),
}));

const SERVER = 'db';

function tool(name: string, description?: string): Tool {
  return {
    name: `${SERVER}${TOOL_NAME_DELIMITER}${name}`,
    description,
    inputSchema: {},
  };
}

beforeEach(() => {
  const catalog = [
    tool('query', 'Run a SQL query'),
    tool('insert', 'Insert a row'),
    tool('delete_row', 'Delete a row'),
  ];
  // The editor derives rows + descriptions from the catalog, not `tools`.
  mockStoreState.tools = catalog;
  mockStoreState.toolCatalog = catalog;
  mockStoreState.setGatewayStatus.mockReset();
  mockStoreState.setTools.mockReset();
  mockStoreState.setToolCatalog.mockReset();
  mockStoreState.selectNode.mockReset();
  vi.restoreAllMocks();
});

describe('useToolsEditor', () => {
  it('derives every advertised tool, prefix-stripped', () => {
    const { result } = renderHook(() => useToolsEditor(SERVER, []));
    expect(result.current.allTools.map((t) => t.name).sort()).toEqual([
      'delete_row',
      'insert',
      'query',
    ]);
  });

  it('seeds selection from the saved whitelist', () => {
    const { result } = renderHook(() => useToolsEditor(SERVER, ['query']));
    expect(result.current.selected.has('query')).toBe(true);
    expect(result.current.selected.has('insert')).toBe(false);
    expect(result.current.dirty).toBe(false);
  });

  it('seeds every tool as selected when the saved whitelist is empty (expose-all)', () => {
    const { result } = renderHook(() => useToolsEditor(SERVER, []));
    expect(result.current.selected.size).toBe(3);
    expect(result.current.dirty).toBe(false);
  });

  it('keeps toggle identity across consecutive toggles', () => {
    const { result, rerender } = renderHook(() => useToolsEditor(SERVER, ['query']));
    const toggle = result.current.toggle;
    act(() => result.current.toggle('insert'));
    expect(result.current.toggle).toBe(toggle);
    rerender();
    expect(result.current.toggle).toBe(toggle);
    expect(result.current.selected.has('insert')).toBe(true);
  });

  it('toggles a tool and tracks dirty + diff count', () => {
    const { result } = renderHook(() => useToolsEditor(SERVER, ['query']));

    act(() => result.current.toggle('insert'));
    expect(result.current.selected.has('insert')).toBe(true);
    expect(result.current.dirty).toBe(true);
    expect(result.current.diffCount).toBe(1);

    // Toggling back to the saved state clears the dirty flag.
    act(() => result.current.toggle('insert'));
    expect(result.current.dirty).toBe(false);
    expect(result.current.diffCount).toBe(0);
  });

  it('selectAll selects every tool; clearAll empties the selection', () => {
    const { result } = renderHook(() => useToolsEditor(SERVER, ['query']));

    act(() => result.current.selectAll());
    expect(result.current.selected.size).toBe(3);

    act(() => result.current.clearAll());
    expect(result.current.selected.size).toBe(0);
    expect(result.current.dirty).toBe(true);
  });

  it('saves the curated selection (canonical, sorted) and refreshes store caches', async () => {
    const setSpy = vi
      .spyOn(apiModule, 'setServerTools')
      .mockResolvedValue({ server: SERVER, tools: ['insert', 'query'], reloaded: true, reloadedAt: 'now' });
    const fetchStatusSpy = vi
      .spyOn(apiModule, 'fetchStatus')
      .mockResolvedValue({ gateway: { name: 'x', version: '1' }, 'mcp-servers': [] });
    const fetchToolsSpy = vi.spyOn(apiModule, 'fetchTools').mockResolvedValue({ tools: [] });
    vi.spyOn(apiModule, 'fetchToolCatalog').mockResolvedValue({ tools: [] });

    const { result } = renderHook(() => useToolsEditor(SERVER, ['query']));
    act(() => result.current.toggle('insert'));

    await act(async () => {
      await result.current.handleSave();
    });

    expect(setSpy).toHaveBeenCalledTimes(1);
    expect(setSpy).toHaveBeenCalledWith(SERVER, ['insert', 'query']);
    await waitFor(() => expect(fetchStatusSpy).toHaveBeenCalled());
    expect(fetchToolsSpy).toHaveBeenCalled();
    expect(mockStoreState.setGatewayStatus).toHaveBeenCalled();
    expect(mockStoreState.setTools).toHaveBeenCalled();
  });

  it('sends an empty whitelist when every tool is selected (expose-all wire semantics)', async () => {
    const setSpy = vi
      .spyOn(apiModule, 'setServerTools')
      .mockResolvedValue({ server: SERVER, tools: [], reloaded: true, reloadedAt: 'now' });
    vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({
      gateway: { name: 'x', version: '1' },
      'mcp-servers': [],
    });
    vi.spyOn(apiModule, 'fetchTools').mockResolvedValue({ tools: [] });
    vi.spyOn(apiModule, 'fetchToolCatalog').mockResolvedValue({ tools: [] });

    // Start curated on "query"; selecting the remaining two brings the
    // selection to the full set, which must normalize to [].
    const { result } = renderHook(() => useToolsEditor(SERVER, ['query']));
    act(() => result.current.toggle('insert'));
    act(() => result.current.toggle('delete_row'));

    await act(async () => {
      await result.current.handleSave();
    });

    expect(setSpy).toHaveBeenCalledTimes(1);
    expect(setSpy).toHaveBeenCalledWith(SERVER, []);
  });

  it('disableTools deselects the named tools and saves the remainder', async () => {
    const setSpy = vi
      .spyOn(apiModule, 'setServerTools')
      .mockResolvedValue({ server: SERVER, tools: ['query'], reloaded: true, reloadedAt: 'now' });
    vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({
      gateway: { name: 'x', version: '1' },
      'mcp-servers': [],
    });
    vi.spyOn(apiModule, 'fetchTools').mockResolvedValue({ tools: [] });
    vi.spyOn(apiModule, 'fetchToolCatalog').mockResolvedValue({ tools: [] });

    // Saved whitelist exposes query + insert (2 of 3 tools); disable insert.
    const { result } = renderHook(() => useToolsEditor(SERVER, ['query', 'insert']));

    await act(async () => {
      await result.current.disableTools(['insert']);
    });

    // Remaining selection is the curated whitelist, persisted as-is (not []).
    expect(setSpy).toHaveBeenCalledWith(SERVER, ['query']);
    expect(result.current.selected.has('insert')).toBe(false);
    expect(result.current.selected.has('query')).toBe(true);
  });

  it('refuses to save an empty selection (Clear then Save would re-expose all)', async () => {
    const setSpy = vi.spyOn(apiModule, 'setServerTools');

    // Curated whitelist; clearing it and saving would send [], which the
    // backend reads as "expose all" — the inverse of the user's intent.
    const { result } = renderHook(() => useToolsEditor(SERVER, ['query', 'insert']));

    act(() => result.current.clearAll());
    expect(result.current.saveBlocked).toBe(true);

    await act(async () => {
      await result.current.handleSave();
    });

    expect(setSpy).not.toHaveBeenCalled();
    expect(showToast).toHaveBeenCalledWith('error', expect.stringContaining('empty selection'));
  });

  it('permits saving an empty selection when the server advertises no tools', async () => {
    mockStoreState.tools = [];
    mockStoreState.toolCatalog = [];
    const setSpy = vi
      .spyOn(apiModule, 'setServerTools')
      .mockResolvedValue({ server: SERVER, tools: [], reloaded: true, reloadedAt: 'now' });
    vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({
      gateway: { name: 'x', version: '1' },
      'mcp-servers': [],
    });
    vi.spyOn(apiModule, 'fetchTools').mockResolvedValue({ tools: [] });
    vi.spyOn(apiModule, 'fetchToolCatalog').mockResolvedValue({ tools: [] });

    // No discovered tools: [] is the only expressible save, so the guard must
    // not block it. Defensive — the UI keeps Save disabled while clean, so
    // this pins the guard's exemption rather than a reachable user flow.
    const { result } = renderHook(() => useToolsEditor(SERVER, []));
    expect(result.current.saveBlocked).toBe(false);

    await act(async () => {
      await result.current.handleSave();
    });

    expect(setSpy).toHaveBeenCalledWith(SERVER, []);
  });

  it('disableTools refuses to disable every exposed tool (would re-expose all)', async () => {
    const setSpy = vi.spyOn(apiModule, 'setServerTools');

    // Curated whitelist exposes exactly query + insert; disabling both would
    // empty the whitelist, which means "expose all" — the opposite intent.
    const { result } = renderHook(() => useToolsEditor(SERVER, ['query', 'insert']));

    await act(async () => {
      await result.current.disableTools(['query', 'insert']);
    });

    // No save attempted; the live selection is untouched.
    expect(setSpy).not.toHaveBeenCalled();
    expect(result.current.selected.has('query')).toBe(true);
    expect(result.current.selected.has('insert')).toBe(true);
  });

  describe('disableTools with other draft edits', () => {
    function mockRefresh() {
      vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({
        gateway: { name: 'x', version: '1' },
        'mcp-servers': [],
      });
      vi.spyOn(apiModule, 'fetchTools').mockResolvedValue({ tools: [] });
      vi.spyOn(apiModule, 'fetchToolCatalog').mockResolvedValue({ tools: [] });
    }

    it('saves only the saved list minus the names and keeps unrelated draft edits', async () => {
      const setSpy = vi
        .spyOn(apiModule, 'setServerTools')
        .mockResolvedValue({ server: SERVER, tools: ['query'], reloaded: true, reloadedAt: 'now' });
      mockRefresh();
      const { result, rerender } = renderHook(
        ({ saved }: { saved: string[] }) => useToolsEditor(SERVER, saved),
        { initialProps: { saved: ['query', 'insert'] } },
      );
      act(() => result.current.toggle('delete_row'));

      await act(async () => {
        await result.current.disableTools(['insert']);
      });

      expect(setSpy).toHaveBeenCalledTimes(1);
      expect(setSpy).toHaveBeenCalledWith(SERVER, ['query']);
      expect(result.current.selected.has('delete_row')).toBe(true);
      expect(result.current.selected.has('query')).toBe(true);
      expect(result.current.selected.has('insert')).toBe(false);
      expect(result.current.dirty).toBe(true);

      // The poll adopts the new baseline; only the unrelated edit is a diff.
      rerender({ saved: ['query'] });
      expect(result.current.dirty).toBe(true);
      expect(result.current.diffCount).toBe(1);
      expect([...result.current.selected].sort()).toEqual(['delete_row', 'query']);
    });

    it('keeps a draft uncheck of another saved tool out of the request', async () => {
      const setSpy = vi
        .spyOn(apiModule, 'setServerTools')
        .mockResolvedValue({ server: SERVER, tools: ['query'], reloaded: true, reloadedAt: 'now' });
      mockRefresh();
      const { result } = renderHook(() => useToolsEditor(SERVER, ['query', 'insert']));
      act(() => result.current.toggle('query'));

      await act(async () => {
        await result.current.disableTools(['insert']);
      });

      expect(setSpy).toHaveBeenCalledWith(SERVER, ['query']);
      expect(result.current.selected.has('query')).toBe(false);
      expect(result.current.selected.has('insert')).toBe(false);
    });

    it('sends nothing for names enabled only in the draft and drops them from it', async () => {
      const setSpy = vi.spyOn(apiModule, 'setServerTools');
      const { result } = renderHook(() => useToolsEditor(SERVER, ['query', 'insert']));
      act(() => result.current.toggle('delete_row'));

      await act(async () => {
        await result.current.disableTools(['delete_row']);
      });

      expect(setSpy).not.toHaveBeenCalled();
      expect(result.current.selected.has('delete_row')).toBe(false);
      expect(result.current.dirty).toBe(false);
    });

    it('refuses when the saved list would empty even if the draft has other tools', async () => {
      const setSpy = vi.spyOn(apiModule, 'setServerTools');
      const { result } = renderHook(() => useToolsEditor(SERVER, ['insert']));
      act(() => result.current.toggle('query'));

      await act(async () => {
        await result.current.disableTools(['insert']);
      });

      expect(setSpy).not.toHaveBeenCalled();
      expect(showToast).toHaveBeenCalledWith('error', expect.stringContaining("Can't disable every exposed tool"));
      expect([...result.current.selected].sort()).toEqual(['insert', 'query']);
    });

    it('restores the pre-call draft on a hidden_tool_reference refusal', async () => {
      vi.spyOn(apiModule, 'setServerTools').mockRejectedValue(
        new SetServerToolsError(
          'hidden_tool_reference',
          "groups[ops].tools[0]: tool 'db__insert' is hidden",
          'Keep it exposed.',
          400,
          [{ field: 'groups[ops].tools[0]', message: "tool 'db__insert' is hidden" }],
        ),
      );
      const { result } = renderHook(() => useToolsEditor(SERVER, ['query', 'insert']));
      act(() => result.current.toggle('delete_row'));
      const before = [...result.current.selected].sort();

      await act(async () => {
        await result.current.disableTools(['insert']);
      });

      expect([...result.current.selected].sort()).toEqual(before);
      expect(result.current.hiddenReference).toContain('groups ops (db__insert)');
    });

    it('restores the pre-call draft on a stack_modified conflict', async () => {
      vi.spyOn(apiModule, 'setServerTools').mockRejectedValue(
        new SetServerToolsError('stack_modified', 'File changed', 'Reload the file.', 409),
      );
      const { result } = renderHook(() => useToolsEditor(SERVER, ['query', 'insert']));
      act(() => result.current.toggle('delete_row'));
      const before = [...result.current.selected].sort();

      await act(async () => {
        await result.current.disableTools(['insert']);
      });

      expect([...result.current.selected].sort()).toEqual(before);
      expect(result.current.conflict).toBe('Reload the file.');
    });

    it('restores the pre-call draft on unknown_tool and other server errors', async () => {
      for (const code of ['unknown_tool', 'invalid_request'] as const) {
        vi.mocked(showToast).mockClear();
        vi.spyOn(apiModule, 'setServerTools').mockRejectedValue(
          new SetServerToolsError(code, 'bad', undefined, 400),
        );
        const { result } = renderHook(() => useToolsEditor(SERVER, ['query', 'insert']));
        act(() => result.current.toggle('delete_row'));
        const before = [...result.current.selected].sort();

        await act(async () => {
          await result.current.disableTools(['insert']);
        });

        expect([...result.current.selected].sort()).toEqual(before);
        expect(showToast).toHaveBeenCalledWith('error', expect.any(String), expect.anything());
      }
    });

    it('restores the pre-call draft on a network failure', async () => {
      vi.spyOn(apiModule, 'setServerTools').mockRejectedValue(new TypeError('Failed to fetch'));
      const { result } = renderHook(() => useToolsEditor(SERVER, ['query', 'insert']));
      act(() => result.current.toggle('delete_row'));
      const before = [...result.current.selected].sort();

      await act(async () => {
        await result.current.disableTools(['insert']);
      });

      expect([...result.current.selected].sort()).toEqual(before);
      expect(showToast).toHaveBeenCalledWith('error', expect.any(String), expect.anything());
    });

    it('restores the pre-call draft before an AuthError propagates', async () => {
      vi.spyOn(apiModule, 'setServerTools').mockRejectedValue(new apiModule.AuthError('denied'));
      const { result } = renderHook(() => useToolsEditor(SERVER, ['query', 'insert']));
      act(() => result.current.toggle('delete_row'));
      const before = [...result.current.selected].sort();

      let thrown: unknown;
      await act(async () => {
        try {
          await result.current.disableTools(['insert']);
        } catch (err) {
          thrown = err;
        }
      });

      expect(thrown).toBeInstanceOf(apiModule.AuthError);
      expect([...result.current.selected].sort()).toEqual(before);
    });

    it('keeps the removals after reload_failed because the write persisted', async () => {
      vi.spyOn(apiModule, 'setServerTools').mockRejectedValue(
        new SetServerToolsError('reload_failed', 'reload broke', undefined, 500),
      );
      mockRefresh();
      const { result } = renderHook(() => useToolsEditor(SERVER, ['query', 'insert']));
      act(() => result.current.toggle('delete_row'));

      await act(async () => {
        await result.current.disableTools(['insert']);
      });

      expect(result.current.selected.has('insert')).toBe(false);
      expect(result.current.selected.has('delete_row')).toBe(true);
    });
  });

  it('reverts the checkbox and lists the group when a hidden tool reference is refused', async () => {
    vi.spyOn(apiModule, 'setServerTools').mockRejectedValue(
      new SetServerToolsError(
        'hidden_tool_reference',
        "groups[ops].tools[0]: tool 'vault__read_secret' is hidden by mcp-servers[vault].tools and can never be called",
        'Remove the tool from the named group or keep it exposed on this server.',
        400,
        [{ field: 'groups[ops].tools[0]', message: "tool 'vault__read_secret' is hidden by mcp-servers[vault].tools and can never be called" }],
      ),
    );

    const { result } = renderHook(() => useToolsEditor(SERVER, ['query', 'read_secret']));
    act(() => result.current.toggle('read_secret'));
    expect(result.current.selected.has('read_secret')).toBe(false);

    await act(async () => {
      await result.current.handleSave();
    });

    expect(result.current.selected.has('read_secret')).toBe(true);
    expect(result.current.hiddenReference).toContain('groups ops (vault__read_secret)');
    expect(result.current.hiddenReference).toContain('Remove the tool from the named group');

    vi.spyOn(apiModule, 'setServerTools').mockResolvedValue({
      server: SERVER,
      tools: ['query'],
      reloaded: true,
      reloadedAt: 'now',
    });
    vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({
      gateway: { name: 'x', version: '1' },
      'mcp-servers': [],
    });
    vi.spyOn(apiModule, 'fetchTools').mockResolvedValue({ tools: [] });
    vi.spyOn(apiModule, 'fetchToolCatalog').mockResolvedValue({ tools: [] });
    await act(async () => {
      await result.current.handleSave();
    });
    expect(result.current.hiddenReference).toBeNull();
  });

  it('clears a hidden-tool notice on reload and when the server changes', async () => {
    vi.spyOn(apiModule, 'setServerTools').mockRejectedValue(
      new SetServerToolsError(
        'hidden_tool_reference',
        "groups[ops].tools[0]: tool 'vault__read_secret' is hidden",
        'Remove the tool from the named group or keep it exposed on this server.',
        400,
        [{ field: 'groups[ops].tools[0]', message: "tool 'vault__read_secret' is hidden" }],
      ),
    );
    vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({
      gateway: { name: 'x', version: '1' },
      'mcp-servers': [],
    });
    vi.spyOn(apiModule, 'fetchTools').mockResolvedValue({ tools: [] });
    vi.spyOn(apiModule, 'fetchToolCatalog').mockResolvedValue({ tools: [] });

    const { result, rerender } = renderHook(
      ({ name }: { name: string }) => useToolsEditor(name, ['query', 'read_secret']),
      { initialProps: { name: SERVER } },
    );
    act(() => result.current.toggle('read_secret'));
    await act(async () => {
      await result.current.handleSave();
    });
    expect(result.current.hiddenReference).toContain('groups ops');

    await act(async () => {
      await result.current.handleReloadFromDisk();
    });
    expect(result.current.hiddenReference).toBeNull();

    act(() => result.current.toggle('read_secret'));
    await act(async () => {
      await result.current.handleSave();
    });
    expect(result.current.hiddenReference).not.toBeNull();
    rerender({ name: 'other' });
    expect(result.current.hiddenReference).toBeNull();
  });

  it('surfaces a 409 stack_modified error as a conflict instead of throwing', async () => {
    vi.spyOn(apiModule, 'setServerTools').mockRejectedValue(
      new SetServerToolsError('stack_modified', 'File changed', 'Reload the file.', 409),
    );

    const { result } = renderHook(() => useToolsEditor(SERVER, ['query']));
    act(() => result.current.toggle('insert'));

    await act(async () => {
      await result.current.handleSave();
    });

    expect(result.current.conflict).toBe('Reload the file.');
  });

  it('lists every tool from the server-advertised list when the global store is empty (code mode)', () => {
    mockStoreState.tools = [];
    const { result } = renderHook(() =>
      useToolsEditor(SERVER, [], ['query', 'insert', 'delete_row']),
    );
    expect(result.current.allTools.map((t) => t.name).sort()).toEqual([
      'delete_row',
      'insert',
      'query',
    ]);
    // Empty whitelist → all rows selected.
    expect(result.current.selected.size).toBe(3);
  });

  it('prompts to discard when the server switches with unsaved edits', () => {
    const { result, rerender } = renderHook(
      ({ server, saved }: { server: string; saved: string[] }) =>
        useToolsEditor(server, saved),
      { initialProps: { server: 'db', saved: ['query'] } },
    );

    act(() => result.current.toggle('insert'));
    expect(result.current.dirty).toBe(true);

    // A different server is selected out from under the dirty editor.
    mockStoreState.tools = [tool('foo')];
    rerender({ server: 'other', saved: [] });

    expect(result.current.discardPrompt).toBe('db');

    // "Keep editing" requests the restore; the prompt stays until the hook
    // is actually rendered for the owner again.
    act(() => result.current.handleKeepEditing());
    expect(mockStoreState.selectNode).toHaveBeenCalledWith('mcp-db');
    expect(result.current.discardPrompt).toBe('db');

    rerender({ server: 'db', saved: ['query'] });
    expect(result.current.discardPrompt).toBeNull();
    expect(result.current.locked).toBe(false);
    expect(result.current.selected.has('insert')).toBe(true);
  });

  it('calls the injected restore instead of the Stack store', () => {
    const onRestoreServer = vi.fn();
    const { result, rerender } = renderHook(
      ({ server }: { server: string }) =>
        useToolsEditor(server, ['query'], undefined, { onRestoreServer }),
      { initialProps: { server: 'db' } },
    );
    act(() => result.current.toggle('insert'));
    rerender({ server: 'other' });

    act(() => result.current.handleKeepEditing());
    expect(onRestoreServer).toHaveBeenCalledWith('db');
    expect(mockStoreState.selectNode).not.toHaveBeenCalled();
  });

  it('locks a dirty draft to its owner from the first render after a switch', async () => {
    const setSpy = vi.spyOn(apiModule, 'setServerTools');
    const lockedByRender: boolean[] = [];
    const { result, rerender } = renderHook(
      ({ server }: { server: string }) => {
        const editor = useToolsEditor(server, [], ['query', 'insert', 'delete_row']);
        lockedByRender.push(editor.locked);
        return editor;
      },
      { initialProps: { server: 'db-a' } },
    );
    act(() => result.current.toggle('delete_row'));
    lockedByRender.length = 0;

    rerender({ server: 'db-b' });
    expect(lockedByRender[0]).toBe(true);
    expect(result.current.locked).toBe(true);
    expect(result.current.owner).toBe('db-a');
    expect(result.current.discardPrompt).toBe('db-a');

    const before = [...result.current.selected].sort();
    act(() => result.current.toggle('delete_row'));
    act(() => result.current.selectAll());
    act(() => result.current.clearAll());
    await act(async () => {
      await result.current.handleSave();
    });
    await act(async () => {
      await result.current.disableTools(['query']);
    });

    expect(setSpy).not.toHaveBeenCalled();
    expect(showToast).not.toHaveBeenCalled();
    expect([...result.current.selected].sort()).toEqual(before);
  });

  it('adopts the new server with no lock when the draft is clean', () => {
    mockStoreState.toolCatalog = [tool('query'), tool('insert'), { ...tool('foo'), name: `other${TOOL_NAME_DELIMITER}foo` }];
    const { result, rerender } = renderHook(
      ({ server, saved }: { server: string; saved: string[] }) => useToolsEditor(server, saved),
      { initialProps: { server: 'db', saved: ['query'] } },
    );
    rerender({ server: 'other', saved: [] });
    expect(result.current.locked).toBe(false);
    expect(result.current.discardPrompt).toBeNull();
    expect([...result.current.selected]).toEqual(['foo']);
    expect(result.current.dirty).toBe(false);
  });

  it('does not plant the old server baseline when a hidden-reference rejection lands after a switch', async () => {
    let reject: (err: unknown) => void = () => {};
    const setSpy = vi
      .spyOn(apiModule, 'setServerTools')
      .mockReturnValue(new Promise((_, r) => { reject = r; }));
    const all = ['query', 'insert', 'delete_row'];
    const { result, rerender } = renderHook(
      ({ server, saved }: { server: string; saved: string[] }) =>
        useToolsEditor(server, saved, all),
      { initialProps: { server: 'db-a', saved: ['query', 'insert'] } },
    );
    act(() => result.current.toggle('insert'));
    let pending: Promise<void> = Promise.resolve();
    act(() => {
      pending = result.current.handleSave();
    });
    act(() => result.current.handleDiscard());
    rerender({ server: 'db-b', saved: ['query'] });
    expect(result.current.owner).toBe('db-b');
    expect(result.current.dirty).toBe(false);

    await act(async () => {
      reject(new SetServerToolsError('hidden_tool_reference', 'x', undefined, 400, []));
      await pending;
    });

    expect(result.current.owner).toBe('db-b');
    expect(result.current.dirty).toBe(false);
    expect([...result.current.selected]).toEqual(['query']);
    expect(result.current.hiddenReference).toBeNull();
    expect(setSpy).toHaveBeenCalledTimes(1);
  });

  it('leaves a locked draft alone when a hidden-reference rejection lands after a switch', async () => {
    let reject: (err: unknown) => void = () => {};
    vi.spyOn(apiModule, 'setServerTools').mockReturnValue(
      new Promise((_, r) => { reject = r; }),
    );
    const all = ['query', 'insert', 'delete_row'];
    const { result, rerender } = renderHook(
      ({ server, saved }: { server: string; saved: string[] }) =>
        useToolsEditor(server, saved, all),
      { initialProps: { server: 'db-a', saved: ['query', 'insert'] } },
    );
    act(() => result.current.toggle('insert'));
    let pending: Promise<void> = Promise.resolve();
    act(() => {
      pending = result.current.handleSave();
    });
    rerender({ server: 'db-b', saved: ['query'] });
    expect(result.current.discardPrompt).toBe('db-a');

    await act(async () => {
      reject(new SetServerToolsError('hidden_tool_reference', 'x', undefined, 400, []));
      await pending;
    });

    // The draft stays with its owner and the prompt stays up.
    expect(result.current.locked).toBe(true);
    expect(result.current.discardPrompt).toBe('db-a');
    expect(result.current.selected.has('insert')).toBe(false);
    expect(result.current.hiddenReference).toBeNull();
  });

  it('keeps a dirty draft through a poll that reshuffles the saved list', () => {
    const { result, rerender } = renderHook(
      ({ saved }: { saved: string[] }) => useToolsEditor(SERVER, saved),
      { initialProps: { saved: ['query', 'insert'] } },
    );
    act(() => result.current.toggle('delete_row'));
    rerender({ saved: ['insert', 'query'] });
    expect(result.current.locked).toBe(false);
    expect(result.current.selected.has('delete_row')).toBe(true);
    expect(result.current.dirty).toBe(true);
  });
});

describe('toWireWhitelist', () => {
  it('returns [] only when the selection contains every tool', () => {
    expect(toWireWhitelist(['read', 'write'], ['write', 'read'])).toEqual([]);
    expect(toWireWhitelist(['read', 'write', 'extra'], ['read', 'write'])).toEqual([]);
  });

  it('keeps a same-sized selection with different names', () => {
    expect(toWireWhitelist(['query', 'insert'], ['read', 'write'])).toEqual(['query', 'insert']);
    expect(toWireWhitelist(['read'], ['read', 'write'])).toEqual(['read']);
  });

  it('passes the selection through when the server has no tools', () => {
    expect(toWireWhitelist([], [])).toEqual([]);
  });
});
