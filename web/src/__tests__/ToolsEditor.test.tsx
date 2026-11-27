import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ToolsEditor } from '../components/sidebar/ToolsEditor';
import { TOOL_NAME_DELIMITER } from '../lib/constants';
import type { Tool } from '../types';
import * as apiModule from '../lib/api';
import { SetServerToolsError } from '../lib/api';
import { InspectorSection } from '../components/inspector/InspectorSection';
import {
  requestInspectorLeave,
  useInspectorToolsGuardStore,
} from '../stores/useInspectorToolsGuardStore';

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
const THREE = ['query', 'insert', 'delete_row'];

function stubRefresh() {
  vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({
    gateway: { name: 'x', version: '1' },
    'mcp-servers': [],
  });
  vi.spyOn(apiModule, 'fetchTools').mockResolvedValue({ tools: [] });
  vi.spyOn(apiModule, 'fetchToolCatalog').mockResolvedValue({ tools: [] });
}

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
  useInspectorToolsGuardStore.setState({ dirtyServer: null, pendingLeave: null });
  vi.restoreAllMocks();
});

describe('ToolsEditor', () => {
  it('renders every discovered tool with the server prefix stripped', () => {
    render(<ToolsEditor serverName={SERVER} savedTools={[]} />);
    expect(screen.getByRole('option', { name: 'query' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'insert' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'delete_row' })).toBeInTheDocument();
  });

  it('seeds selection from the saved whitelist', () => {
    render(<ToolsEditor serverName={SERVER} savedTools={['query']} />);
    expect(screen.getByRole('option', { name: 'query' })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('option', { name: 'insert' })).toHaveAttribute('aria-checked', 'false');
  });

  it('seeds every tool as selected when saved whitelist is empty (no curation)', () => {
    render(<ToolsEditor serverName={SERVER} savedTools={[]} />);
    expect(screen.getByRole('option', { name: 'query' })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('option', { name: 'insert' })).toHaveAttribute('aria-checked', 'true');
  });

  it('disables Save when clean, enables it with a diff count when dirty', () => {
    render(<ToolsEditor serverName={SERVER} savedTools={['query']} />);
    const save = screen.getByRole('button', { name: /^Saved$/ });
    expect(save).toBeDisabled();

    fireEvent.click(screen.getByRole('option', { name: 'insert' }));
    const dirtySave = screen.getByRole('button', { name: /Save 1 change & Reload/ });
    expect(dirtySave).toBeEnabled();
  });

  it('distinguishes no allow-list, a saved allow-list, and an unsaved draft in the count line', () => {
    mockStoreState.toolCatalog = [tool('query', 'Run a SQL query'), tool('insert', 'Insert a row')];
    mockStoreState.tools = mockStoreState.toolCatalog;
    const { unmount } = render(<ToolsEditor serverName={SERVER} savedTools={[]} />);
    expect(screen.getByText('All 2 tools exposed (no allow-list)')).toBeInTheDocument();
    unmount();

    render(<ToolsEditor serverName={SERVER} savedTools={['query']} />);
    expect(screen.getByText('1 of 2 exposed')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('option', { name: 'insert' }));
    expect(screen.getByText('2 of 2 selected (unsaved)')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: /clear all tools/i }));
    expect(screen.getByRole('alert')).toHaveTextContent(/cannot save an empty selection/i);
  });

  it('blocks saving an empty selection and offers an in-place Discard', () => {
    render(<ToolsEditor serverName={SERVER} savedTools={['query']} />);
    fireEvent.click(screen.getByRole('button', { name: /clear all tools/i }));

    // Danger copy replaces the neutral expose-all help text; Save is gated.
    expect(screen.getByText(/cannot save an empty selection/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Save 1 change & Reload/ })).toBeDisabled();

    fireEvent.click(screen.getByRole('button', { name: /discard unsaved tool changes/i }));
    expect(screen.queryByText(/cannot save an empty selection/i)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^Saved$/ })).toBeDisabled();
  });

  it('saves with the current selection and refreshes store caches', async () => {
    const setSpy = vi
      .spyOn(apiModule, 'setServerTools')
      .mockResolvedValue({ server: SERVER, tools: ['query'], reloaded: true, reloadedAt: 'now' });
    const fetchStatusSpy = vi
      .spyOn(apiModule, 'fetchStatus')
      .mockResolvedValue({ gateway: { name: 'x', version: '1' }, 'mcp-servers': [] });
    const fetchToolsSpy = vi.spyOn(apiModule, 'fetchTools').mockResolvedValue({ tools: [] });
    vi.spyOn(apiModule, 'fetchToolCatalog').mockResolvedValue({ tools: [] });

    render(<ToolsEditor serverName={SERVER} savedTools={[]} />);
    fireEvent.click(screen.getByRole('option', { name: 'insert' }));
    fireEvent.click(screen.getByRole('option', { name: 'delete_row' }));

    fireEvent.click(screen.getByRole('button', { name: /Save 2 changes & Reload/ }));

    await waitFor(() => expect(setSpy).toHaveBeenCalledTimes(1));
    expect(setSpy).toHaveBeenCalledWith(SERVER, ['query']);
    await waitFor(() => expect(fetchStatusSpy).toHaveBeenCalled());
    expect(fetchToolsSpy).toHaveBeenCalled();
  });

  it('sends an empty whitelist when the user selects every tool (expose-all semantics)', async () => {
    const setSpy = vi
      .spyOn(apiModule, 'setServerTools')
      .mockResolvedValue({ server: SERVER, tools: [], reloaded: true, reloadedAt: 'now' });
    vi.spyOn(apiModule, 'fetchStatus').mockResolvedValue({
      gateway: { name: 'x', version: '1' },
      'mcp-servers': [],
    });
    vi.spyOn(apiModule, 'fetchTools').mockResolvedValue({ tools: [] });
    vi.spyOn(apiModule, 'fetchToolCatalog').mockResolvedValue({ tools: [] });

    // Start curated: only "query" is saved. User selects the remaining two,
    // bringing selection up to the full set of 3. The handler should
    // normalize to an empty array.
    render(<ToolsEditor serverName={SERVER} savedTools={['query']} />);
    fireEvent.click(screen.getByRole('option', { name: 'insert' }));
    fireEvent.click(screen.getByRole('option', { name: 'delete_row' }));

    fireEvent.click(screen.getByRole('button', { name: /Save 2 changes & Reload/ }));
    await waitFor(() => expect(setSpy).toHaveBeenCalledTimes(1));
    expect(setSpy).toHaveBeenCalledWith(SERVER, []);
  });

  it('prompts to discard when the user switches nodes with unsaved changes', async () => {
    const { rerender } = render(<ToolsEditor serverName="db" savedTools={['query']} />);
    // Make a pending change.
    fireEvent.click(screen.getByRole('option', { name: 'insert' }));
    expect(screen.getByRole('button', { name: /Save 1 change & Reload/ })).toBeEnabled();

    // User clicks a different server node in the graph. The Sidebar re-renders
    // the editor with the new server name; the editor must intercept and
    // prompt before discarding the in-flight edit.
    mockStoreState.tools = [tool('foo')];
    mockStoreState.toolCatalog = [tool('foo')];
    rerender(<ToolsEditor serverName="other" savedTools={[]} />);

    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent('Discard unsaved changes to db?');

    const keep = screen.getByRole('button', { name: /Keep editing/ });
    await waitFor(() => expect(keep).toHaveFocus());
    // Escape means Keep editing: it requests the restore, and the prompt
    // stays up because the mocked store cannot move the selection back.
    fireEvent.keyDown(dialog, { key: 'Escape' });
    expect(mockStoreState.selectNode).toHaveBeenCalledWith('mcp-db');
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
  });

  it('never saves a draft to another server', async () => {
    const setSpy = vi
      .spyOn(apiModule, 'setServerTools')
      .mockResolvedValue({ server: 'db-a', tools: ['insert', 'query'], reloaded: true, reloadedAt: 'now' });
    stubRefresh();
    const { rerender } = render(
      <ToolsEditor serverName="db-a" savedTools={[]} serverTools={THREE} />,
    );
    fireEvent.click(screen.getByRole('option', { name: 'delete_row' }));

    rerender(<ToolsEditor serverName="db-b" savedTools={[]} serverTools={THREE} />);

    expect(screen.getByRole('alertdialog')).toHaveTextContent('Discard unsaved changes to db-a?');
    for (const button of screen.queryAllByRole('button', { name: /Save .* & Reload/ })) {
      expect(button).toBeDisabled();
    }
    fireEvent.click(screen.getByRole('option', { name: 'query' }));
    expect(screen.getByRole('option', { name: 'query' })).toHaveAttribute('aria-checked', 'true');
    fireEvent.click(screen.getByRole('option', { name: 'delete_row' }));
    expect(screen.getByRole('option', { name: 'delete_row' })).toHaveAttribute('aria-checked', 'false');
    expect(screen.getByRole('button', { name: /select all tools/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /clear all tools/i })).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: /^Saved$/ }));
    expect(setSpy).not.toHaveBeenCalled();

    // Keep editing with unchanged props cannot unlock: the prompt stays.
    fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }));
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
    expect(setSpy).not.toHaveBeenCalled();

    // Back on the owner, the draft is intact and saves to the owner.
    rerender(<ToolsEditor serverName="db-a" savedTools={[]} serverTools={THREE} />);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'delete_row' })).toHaveAttribute('aria-checked', 'false');
    const save = screen.getByRole('button', { name: /Save 1 change & Reload/ });
    expect(save).toBeEnabled();
    fireEvent.click(save);
    await waitFor(() => expect(setSpy).toHaveBeenCalledTimes(1));
    expect(setSpy).toHaveBeenCalledWith('db-a', ['insert', 'query']);
  });

  it('adopts the new server on Discard from the locked prompt', () => {
    const setSpy = vi.spyOn(apiModule, 'setServerTools');
    const { rerender } = render(
      <ToolsEditor serverName="db-a" savedTools={[]} serverTools={THREE} />,
    );
    fireEvent.click(screen.getByRole('option', { name: 'delete_row' }));
    rerender(<ToolsEditor serverName="db-b" savedTools={['query']} serverTools={THREE} />);

    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'query' })).toHaveAttribute('aria-checked', 'true');
    expect(screen.getByRole('option', { name: 'insert' })).toHaveAttribute('aria-checked', 'false');
    expect(screen.getByRole('button', { name: /^Saved$/ })).toBeDisabled();
    expect(setSpy).not.toHaveBeenCalled();
  });

  it('switches servers without a prompt when the draft is clean', () => {
    const { rerender } = render(
      <ToolsEditor serverName="db-a" savedTools={[]} serverTools={THREE} />,
    );
    rerender(<ToolsEditor serverName="db-b" savedTools={['query']} serverTools={THREE} />);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'insert' })).toHaveAttribute('aria-checked', 'false');
    expect(screen.getByRole('button', { name: /^Saved$/ })).toBeDisabled();
  });

  it('warns on unload only while the draft is dirty', () => {
    render(<ToolsEditor serverName={SERVER} savedTools={['query']} />);
    const clean = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(clean);
    expect(clean.defaultPrevented).toBe(false);

    fireEvent.click(screen.getByRole('option', { name: 'insert' }));
    const dirty = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(dirty);
    expect(dirty.defaultPrevented).toBe(true);

    fireEvent.click(screen.getByRole('button', { name: /discard unsaved tool changes/i }));
    const discarded = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(discarded);
    expect(discarded.defaultPrevented).toBe(false);
  });

  it('publishes the dirty server and clears it on unmount', () => {
    const { unmount } = render(<ToolsEditor serverName={SERVER} savedTools={['query']} />);
    expect(useInspectorToolsGuardStore.getState().dirtyServer).toBeNull();
    fireEvent.click(screen.getByRole('option', { name: 'insert' }));
    expect(useInspectorToolsGuardStore.getState().dirtyServer).toBe(SERVER);
    unmount();
    expect(useInspectorToolsGuardStore.getState().dirtyServer).toBeNull();
  });

  it('raises a visible prompt from a collapsed Tools section', () => {
    render(
      <InspectorSection title="Tools" defaultOpen>
        <ToolsEditor serverName={SERVER} savedTools={['query']} />
      </InspectorSection>,
    );
    fireEvent.click(screen.getByRole('option', { name: 'insert' }));
    fireEvent.click(screen.getByRole('button', { name: /^Tools/ }));
    expect(screen.getByRole('option', { name: 'insert', hidden: true })).not.toBeVisible();

    const proceed = vi.fn();
    act(() => requestInspectorLeave(proceed));

    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toBeVisible();
    expect(dialog.closest('[hidden]')).toBeNull();
    expect(dialog.closest('.opacity-0')).toBeNull();
    // The dialog root (the fixed overlay) sits under a portal wrapper that is
    // a direct child of document.body, outside the section's panel.
    expect(dialog.parentElement?.parentElement?.parentElement).toBe(document.body);
    expect(proceed).not.toHaveBeenCalled();
  });

  it('holds a pending leave until the user decides', () => {
    render(<ToolsEditor serverName={SERVER} savedTools={['query']} />);

    // Clean editor and a matching target pass straight through.
    const clean = vi.fn();
    requestInspectorLeave(clean);
    expect(clean).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByRole('option', { name: 'insert' }));
    const same = vi.fn();
    requestInspectorLeave(same, { targetServer: SERVER });
    expect(same).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();

    const kept = vi.fn();
    act(() => requestInspectorLeave(kept));
    expect(screen.getByRole('alertdialog')).toHaveTextContent('Discard unsaved changes to db?');
    expect(kept).not.toHaveBeenCalled();
    // The editor is inert while the leave is pending.
    expect(screen.getByRole('button', { name: /Save 1 change & Reload/ })).toBeDisabled();
    expect(screen.getByRole('button', { name: /discard unsaved tool changes/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /select all tools/i })).toBeDisabled();
    fireEvent.click(screen.getByRole('option', { name: 'delete_row' }));
    expect(screen.getByRole('option', { name: 'delete_row' })).toHaveAttribute('aria-checked', 'false');

    fireEvent.click(screen.getByRole('button', { name: 'Keep editing' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(kept).not.toHaveBeenCalled();
    expect(screen.getByRole('option', { name: 'insert' })).toHaveAttribute('aria-checked', 'true');

    const left = vi.fn();
    act(() => requestInspectorLeave(left));
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(left).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'insert' })).toHaveAttribute('aria-checked', 'false');
    expect(useInspectorToolsGuardStore.getState().dirtyServer).toBeNull();
  });

  it('renders every server tool when the global store is empty (code mode)', () => {
    // Simulates the catalog being empty (e.g. it failed to load), so the
    // store has nothing prefixed for this server. The editor must still list
    // every tool using the per-server `tools` field from status.
    mockStoreState.tools = [];
    mockStoreState.toolCatalog = [];
    render(
      <ToolsEditor
        serverName={SERVER}
        savedTools={[]}
        serverTools={['query', 'insert', 'delete_row']}
      />,
    );
    expect(screen.getByRole('option', { name: 'query' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'insert' })).toBeInTheDocument();
    expect(screen.getByRole('option', { name: 'delete_row' })).toBeInTheDocument();
    // Empty whitelist means "all tools exposed" → every row pre-checked.
    expect(screen.getByRole('option', { name: 'query' })).toHaveAttribute('aria-checked', 'true');
  });

  it('merges descriptions from the global store onto server-advertised tools', () => {
    render(
      <ToolsEditor
        serverName={SERVER}
        savedTools={[]}
        serverTools={['query', 'insert', 'delete_row']}
      />,
    );
    expect(screen.getByText('Run a SQL query')).toBeInTheDocument();
    expect(screen.getByText('Insert a row')).toBeInTheDocument();
  });

  it('shows an inline conflict alert with a Reload-file action on 409', async () => {
    vi.spyOn(apiModule, 'setServerTools').mockRejectedValue(
      new SetServerToolsError('stack_modified', 'File changed', 'Reload the file.', 409),
    );

    render(<ToolsEditor serverName={SERVER} savedTools={[]} />);
    fireEvent.click(screen.getByRole('option', { name: 'insert' }));
    fireEvent.click(screen.getByRole('button', { name: /Save 1 change & Reload/ }));

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
    expect(screen.getByRole('alert')).toHaveTextContent('modified outside the canvas');
    expect(screen.getByRole('button', { name: /Reload stack file from disk/ })).toBeInTheDocument();
  });
});
