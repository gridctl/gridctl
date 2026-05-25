import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import { useToolsEditor } from '../hooks/useToolsEditor';
import { TOOL_NAME_DELIMITER } from '../lib/constants';
import type { Tool } from '../types';
import * as apiModule from '../lib/api';
import { SetServerToolsError } from '../lib/api';

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

    // "Keep editing" re-selects the original server node in the store.
    act(() => result.current.handleKeepEditing());
    expect(mockStoreState.selectNode).toHaveBeenCalledWith('mcp-db');
    expect(result.current.discardPrompt).toBeNull();
  });
});
