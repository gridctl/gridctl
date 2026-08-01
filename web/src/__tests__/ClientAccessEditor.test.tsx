import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { ClientAccessEditor } from '../components/workspaces/ClientAccessEditor';
import { useStackStore } from '../stores/useStackStore';
import { TOOL_NAME_DELIMITER } from '../lib/constants';
import * as api from '../lib/api';
import type { ClientStatus, MCPServerStatus, Tool } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

const D = TOOL_NAME_DELIMITER;

function server(name: string, tools: string[]): MCPServerStatus {
  return {
    name,
    transport: 'stdio',
    initialized: true,
    toolCount: tools.length,
    tools,
    healthy: true,
  } as unknown as MCPServerStatus;
}

function linkedClient(scope?: ClientStatus['effectiveScope']): ClientStatus {
  return {
    name: 'Cursor',
    slug: 'cursor',
    detected: true,
    linked: true,
    transport: 'native HTTP',
    effectiveScope: scope,
  };
}

const SERVERS = [server('github', ['a', 'b']), server('gitlab', ['x'])];

beforeEach(() => {
  const catalog = [
    { name: `github${D}a`, description: 'Tool A', inputSchema: {} },
    { name: `github${D}b`, description: 'Tool B', inputSchema: {} },
    { name: `gitlab${D}x`, description: 'Tool X', inputSchema: {} },
  ] as Tool[];
  useStackStore.setState({
    clients: [
      linkedClient({
        configured: true,
        unscoped: false,
        servers: ['github', 'gitlab'],
        tools: [`github${D}a`, `github${D}b`, `gitlab${D}x`],
      }),
    ],
    toolCatalog: catalog,
  });
});

function renderEditor() {
  return render(<ClientAccessEditor isOpen onClose={vi.fn()} servers={SERVERS} />);
}

describe('ClientAccessEditor — tool axis', () => {
  it('renders an All/Custom tool group for each granted server', () => {
    renderEditor();
    // Both granted servers expose the All/Custom toggle; full-coverage saved
    // lists seed as All. (Three "All" buttons: two tool groups plus the
    // pane's select-all-servers action.)
    expect(screen.getAllByRole('button', { name: 'All' })).toHaveLength(3);
    expect(screen.getAllByRole('button', { name: 'Custom' })).toHaveLength(2);
    expect(screen.getByText(/All 2 tools of/)).toBeInTheDocument();
    // The "managed in stack.yaml" note must not render alongside the live
    // tool editor.
    expect(screen.queryByText(/managed in stack\.yaml/i)).not.toBeInTheDocument();
  });

  it('narrowing a server to Custom enables save and writes the flattened allow-list', async () => {
    const updateSpy = vi
      .spyOn(api, 'updateClientScope')
      .mockResolvedValue({
        client: 'cursor',
        profileKey: 'cursor',
        reloaded: true,
      } as unknown as Awaited<ReturnType<typeof api.updateClientScope>>);
    vi.spyOn(api, 'fetchClients').mockResolvedValue([]);
    vi.spyOn(api, 'fetchStatus').mockResolvedValue({
      gateway: { name: 'g', version: '1' },
      'mcp-servers': [],
    } as unknown as Awaited<ReturnType<typeof api.fetchStatus>>);

    renderEditor();
    // Switch github (the first group) to Custom; the seeded selection covers
    // the full set, so drop one tool to actually narrow it.
    fireEvent.click(screen.getAllByRole('button', { name: 'Custom' })[0]);
    fireEvent.click(screen.getByRole('checkbox', { name: /Tool A/ }));

    const save = screen.getByRole('button', { name: /save client access and reload/i });
    expect(save).toBeEnabled();
    fireEvent.click(save);

    await waitFor(() =>
      expect(updateSpy).toHaveBeenCalledWith('cursor', {
        servers: ['github', 'gitlab'],
        tools: [`github${D}b`, `gitlab${D}x`],
      }),
    );
  });

  it('blocks save with a visible warning when a Custom selection is empty', () => {
    renderEditor();
    fireEvent.click(screen.getAllByRole('button', { name: 'Custom' })[0]);
    // Clear both seeded tools via the tri-state parent ("Clear all").
    fireEvent.click(screen.getByRole('checkbox', { name: /clear all/i }));

    expect(screen.getByText(/custom tool selection with nothing picked/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Saved' })).toBeDisabled();
  });
});
