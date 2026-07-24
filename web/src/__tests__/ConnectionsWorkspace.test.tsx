import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter } from 'react-router-dom';
import ConnectionsWorkspace from '../components/workspaces/ConnectionsWorkspace';
import { useStackStore } from '../stores/useStackStore';
import * as api from '../lib/api';
import type { ClientStatus } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

function client(overrides: Partial<ClientStatus> & { slug: string }): ClientStatus {
  return {
    name: overrides.slug,
    detected: false,
    linked: false,
    transport: 'native SSE',
    ...overrides,
  };
}

const clients: ClientStatus[] = [
  client({
    slug: 'claude',
    name: 'Claude Desktop',
    detected: true,
    linked: true,
    declared: true,
    configPath: '/home/u/claude.json',
  }),
  client({
    slug: 'cursor',
    name: 'Cursor',
    detected: true,
    linked: true,
    declared: true,
    linkEntry: { group: 'dev' },
    configPath: '/home/u/.cursor/mcp.json',
  }),
  client({ slug: 'grok', name: 'Grok Build', detected: true, configPath: '/home/u/.grok/config.toml' }),
  client({ slug: 'zed', name: 'Zed' }),
];

function renderWorkspace() {
  return render(
    <MemoryRouter initialEntries={['/connections']}>
      <ConnectionsWorkspace />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  useStackStore.setState({ clients });
});

afterEach(() => {
  useStackStore.setState({ clients: [] });
  vi.restoreAllMocks();
});

describe('ConnectionsWorkspace', () => {
  it('renders every client with status badges', () => {
    renderWorkspace();
    expect(screen.getByText('Claude Desktop')).toBeInTheDocument();
    expect(screen.getAllByText('Linked')).toHaveLength(2);
    expect(screen.getAllByText('Declared')).toHaveLength(2);
    // Detected-but-unlinked badge for grok.
    expect(screen.getByText('Detected')).toBeInTheDocument();
    expect(screen.getByText('Not installed on this machine')).toBeInTheDocument();
  });

  it('disables the toggle for undetected clients', () => {
    renderWorkspace();
    expect(screen.getByRole('switch', { name: 'Link Zed' })).toBeDisabled();
    expect(screen.getByRole('switch', { name: 'Link Grok Build' })).toBeEnabled();
  });

  it('reflects connected state: linked clients on, others off', () => {
    renderWorkspace();
    // Linked (and declared) clients start on; toggle = connected, so an
    // imperatively linked client without a link: entry also reads on.
    expect(screen.getByRole('switch', { name: 'Link Claude Desktop' })).toBeChecked();
    expect(screen.getByRole('switch', { name: 'Link Cursor' })).toBeChecked();
    expect(screen.getByRole('switch', { name: 'Link Grok Build' })).not.toBeChecked();
    expect(screen.getByRole('switch', { name: 'Link Zed' })).not.toBeChecked();
  });

  it('stages a link, previews the diff, and applies it', async () => {
    const preview = vi.spyOn(api, 'previewClientLink').mockResolvedValue({
      client: 'grok',
      serverName: 'gridctl',
      configPath: '/home/u/.grok/config.toml',
      before: '{}',
      after: '{ "mcp_servers": { "gridctl": {} } }',
      stackDiff: '+  - grok',
    });
    const link = vi.spyOn(api, 'linkClient').mockResolvedValue({
      client: 'grok',
      serverName: 'gridctl',
      linked: true,
      declared: true,
    });
    vi.spyOn(api, 'fetchClients').mockResolvedValue(clients);

    renderWorkspace();
    fireEvent.click(screen.getByRole('switch', { name: 'Link Grok Build' }));
    expect(screen.getByText('1 pending change')).toBeInTheDocument();

    fireEvent.click(screen.getByText('Review & Apply'));
    await waitFor(() => expect(preview).toHaveBeenCalledWith('grok'));
    expect(await screen.findByText(/mcp_servers/)).toBeInTheDocument();
    expect(screen.getByText(/\+\s+- grok/)).toBeInTheDocument();

    fireEvent.click(screen.getByText('Apply changes'));
    await waitFor(() => expect(link).toHaveBeenCalledWith('grok'));
    await waitFor(() =>
      expect(screen.queryByText('Review connection changes')).not.toBeInTheDocument(),
    );
  });

  it('stages an unlink and calls the delete endpoint', async () => {
    const unlink = vi.spyOn(api, 'unlinkClient').mockResolvedValue({
      client: 'claude',
      serverName: 'gridctl',
      linked: false,
      declared: false,
    });
    vi.spyOn(api, 'fetchClients').mockResolvedValue(clients);

    renderWorkspace();
    fireEvent.click(screen.getByRole('switch', { name: 'Link Claude Desktop' }));
    fireEvent.click(screen.getByText('Review & Apply'));
    expect(screen.getByText('Unlink Claude Desktop')).toBeInTheDocument();

    fireEvent.click(screen.getByText('Apply changes'));
    await waitFor(() => expect(unlink).toHaveBeenCalledWith('claude'));
  });

  it('keeps failed changes staged for retry', async () => {
    vi.spyOn(api, 'linkClient').mockRejectedValue(
      new api.ClientLinkError('link_conflict', 'conflict', undefined, 409),
    );
    vi.spyOn(api, 'fetchClients').mockResolvedValue(clients);

    renderWorkspace();
    fireEvent.click(screen.getByRole('switch', { name: 'Link Grok Build' }));
    fireEvent.click(screen.getByText('Review & Apply'));
    fireEvent.click(screen.getByText('Apply changes'));

    await waitFor(() =>
      expect(screen.queryByText('Review connection changes')).not.toBeInTheDocument(),
    );
    expect(screen.getByText('1 pending change')).toBeInTheDocument();
  });

  it('discard clears staged changes', () => {
    renderWorkspace();
    fireEvent.click(screen.getByRole('switch', { name: 'Link Grok Build' }));
    fireEvent.click(screen.getByText('Discard'));
    expect(screen.queryByText(/pending change/)).not.toBeInTheDocument();
  });

  it('renders an empty state when no clients are reported', () => {
    useStackStore.setState({ clients: [] });
    renderWorkspace();
    expect(screen.getByText('No client registry available')).toBeInTheDocument();
  });
});
