import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ToolsEditor } from '../components/sidebar/ToolsEditor';
import { TOOL_NAME_DELIMITER } from '../lib/constants';
import type { Tool } from '../types';
import * as apiModule from '../lib/api';
import { SetServerToolsError } from '../lib/api';

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
}));

const mockStoreState: {
  tools: Tool[];
  setGatewayStatus: ReturnType<typeof vi.fn>;
  setTools: ReturnType<typeof vi.fn>;
  selectNode: ReturnType<typeof vi.fn>;
} = {
  tools: [],
  setGatewayStatus: vi.fn(),
  setTools: vi.fn(),
  selectNode: vi.fn(),
};

vi.mock('../stores/useStackStore', () => ({
  useStackStore: Object.assign(
    vi.fn((selector: (s: { tools: Tool[] }) => unknown) => selector(mockStoreState)),
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
  mockStoreState.tools = [
    tool('query', 'Run a SQL query'),
    tool('insert', 'Insert a row'),
    tool('delete_row', 'Delete a row'),
  ];
  mockStoreState.setGatewayStatus.mockReset();
  mockStoreState.setTools.mockReset();
  mockStoreState.selectNode.mockReset();
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

  it('saves with the current selection and refreshes store caches', async () => {
    const setSpy = vi
      .spyOn(apiModule, 'setServerTools')
      .mockResolvedValue({ server: SERVER, tools: ['query'], reloaded: true, reloadedAt: 'now' });
    const fetchStatusSpy = vi
      .spyOn(apiModule, 'fetchStatus')
      .mockResolvedValue({ gateway: { name: 'x', version: '1' }, 'mcp-servers': [] });
    const fetchToolsSpy = vi.spyOn(apiModule, 'fetchTools').mockResolvedValue({ tools: [] });

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
    rerender(<ToolsEditor serverName="other" savedTools={[]} />);

    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent('Discard unsaved changes to db?');

    // Clicking "Keep editing" restores the prior selection in the store.
    fireEvent.click(screen.getByRole('button', { name: /Keep editing/ }));
    expect(mockStoreState.selectNode).toHaveBeenCalledWith('mcp-db');
  });

  it('renders every server tool when the global store is empty (code mode)', () => {
    // Simulates code mode: /api/tools returns only the meta-tools, so the
    // global store has nothing prefixed for this server. The editor must
    // still list every tool using the per-server `tools` field from status.
    mockStoreState.tools = [];
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
