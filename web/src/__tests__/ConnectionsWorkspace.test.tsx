import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, cleanup, render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter } from 'react-router';
import ConnectionsWorkspace from '../components/workspaces/ConnectionsWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useContextStore } from '../stores/useContextStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import { useAuthStore } from '../stores/useAuthStore';
import { POLLING } from '../lib/constants';
import * as api from '../lib/api';
import type { ClientStatus } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

function client(overrides: Partial<ClientStatus> & { slug: string }): ClientStatus {
  return {
    name: overrides.slug,
    detected: false,
    linked: false,
    transport: 'native HTTP',
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
  useStackStore.setState({ clients, sessionEntries: null });
  useContextStore.setState({ doc: null, loading: false, error: null });
  useRegistryStore.setState({ agentStatuses: null });
  useAuthStore.setState({ authRequired: false, generation: Symbol() });
  vi.spyOn(api, 'fetchClients').mockResolvedValue(clients);
  vi.spyOn(api, 'fetchWiringStatus').mockResolvedValue([]);
  vi.spyOn(api, 'fetchAgentProjectionStatus').mockResolvedValue([]);
  vi.spyOn(api, 'fetchGlobalContext').mockResolvedValue({
    canonical: { path: '/fixture/context/AGENTS.md', exists: false, content: '' },
    needs_sync: false,
    clients: [],
  });
  vi.spyOn(api, 'fetchModelsStatus').mockResolvedValue({
    policy_path: '/fixture/models/policy.yaml', policy_exists: false, needs_attention: false, targets: [],
  });
  vi.spyOn(api, 'fetchSessions').mockResolvedValue({ count: 0, sessions: [], entries: [] });
  vi.spyOn(api, 'previewClientLink').mockResolvedValue({
    client: 'grok', serverName: 'gridctl', configPath: '/fixture/client.json', before: '{}', after: '{}', stackDiff: '',
  });
});

afterEach(() => {
  cleanup();
  useStackStore.setState({ clients: [] });
  vi.restoreAllMocks();
  vi.useRealTimers();
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

describe('ConnectionsWorkspace', () => {
  it.each(['initial', 'post-apply'] as const)('ignores late %s health results after unmount', async (phase) => {
    const models = deferred<Awaited<ReturnType<typeof api.fetchModelsStatus>>>();
    if (phase === 'initial') vi.mocked(api.fetchModelsStatus).mockReturnValueOnce(models.promise);
    const view = renderWorkspace();
    if (phase === 'post-apply') {
      await act(async () => {});
      vi.mocked(api.fetchModelsStatus).mockReturnValueOnce(models.promise);
      vi.spyOn(api, 'unlinkClient').mockResolvedValue({ client: 'claude', serverName: 'gridctl', linked: false, declared: false });
      fireEvent.click(screen.getByRole('switch', { name: 'Link Claude Desktop' }));
      fireEvent.click(screen.getByText('Review & Apply'));
      fireEvent.click(screen.getByText('Apply changes'));
      await waitFor(() => expect(api.fetchModelsStatus).toHaveBeenCalledTimes(2));
    }
    view.unmount();
    useStackStore.setState({ clients: [] });
    useRegistryStore.setState({ agentStatuses: null });
    await act(async () => { models.reject(new Error('Models unavailable')); });
    expect(useStackStore.getState().clients).toEqual([]);
    expect(useRegistryStore.getState().agentStatuses).toBeNull();
  });

  it('does not start a health refresh when a mutation settles after unmount', async () => {
    const unlink = deferred<Awaited<ReturnType<typeof api.unlinkClient>>>();
    vi.spyOn(api, 'unlinkClient').mockReturnValue(unlink.promise);
    const view = renderWorkspace();
    fireEvent.click(screen.getByRole('switch', { name: 'Link Claude Desktop' }));
    fireEvent.click(screen.getByText('Review & Apply'));
    fireEvent.click(screen.getByText('Apply changes'));
    await waitFor(() => expect(api.unlinkClient).toHaveBeenCalledOnce());
    view.unmount();
    await act(async () => { unlink.resolve({ client: 'claude', serverName: 'gridctl', linked: false, declared: false }); });
    expect(api.fetchModelsStatus).toHaveBeenCalledOnce();
  });

  it('stops an in-flight apply batch on credential rejection without replaying it', async () => {
    const unlink = deferred<Awaited<ReturnType<typeof api.unlinkClient>>>();
    vi.spyOn(api, 'unlinkClient').mockReturnValue(unlink.promise);
    const link = vi.spyOn(api, 'linkClient');
    renderWorkspace();
    fireEvent.click(screen.getByRole('switch', { name: 'Link Claude Desktop' }));
    fireEvent.click(screen.getByRole('switch', { name: 'Link Grok Build' }));
    fireEvent.click(screen.getByText('Review & Apply'));
    fireEvent.click(screen.getByText('Apply changes'));
    await waitFor(() => expect(api.unlinkClient).toHaveBeenCalledOnce());
    act(() => useAuthStore.getState().rejectGeneration(useAuthStore.getState().generation));
    await act(async () => { unlink.reject(new api.AuthError()); });
    expect(link).not.toHaveBeenCalled();
    expect(api.fetchModelsStatus).toHaveBeenCalledOnce();
    await act(async () => useAuthStore.setState({ authRequired: false, generation: Symbol() }));
    expect(api.fetchModelsStatus).toHaveBeenCalledTimes(2);
    expect(api.unlinkClient).toHaveBeenCalledOnce();
    expect(link).not.toHaveBeenCalled();
    expect(screen.getByText('Apply changes')).toBeEnabled();
  });

  it('keeps newer health data when a superseded request succeeds late', async () => {
    const oldClients = deferred<ClientStatus[]>();
    vi.mocked(api.fetchClients).mockReturnValueOnce(oldClients.promise);
    renderWorkspace();
    await act(async () => useAuthStore.setState({ generation: Symbol() }));
    expect(api.fetchClients).toHaveBeenCalledTimes(2);
    await act(async () => { oldClients.resolve([client({ slug: 'stale-client' })]); });
    expect(useStackStore.getState().clients).toEqual(clients);
    expect(screen.queryByText('stale-client')).not.toBeInTheDocument();
  });

  it('discards old health results and resumes reads after credential verification', async () => {
    const models = deferred<Awaited<ReturnType<typeof api.fetchModelsStatus>>>();
    const context = deferred<Awaited<ReturnType<typeof api.fetchGlobalContext>>>();
    vi.mocked(api.fetchModelsStatus).mockReturnValueOnce(models.promise);
    vi.mocked(api.fetchGlobalContext).mockReturnValueOnce(context.promise);
    renderWorkspace();
    act(() => useAuthStore.getState().rejectGeneration(useAuthStore.getState().generation));
    await act(async () => {
      context.resolve({ canonical: { path: '/old', exists: false, content: '' }, needs_sync: false, clients: [] });
      models.reject(new Error('Old request failed'));
    });
    expect(useContextStore.getState().doc).toBeNull();
    expect(useRegistryStore.getState().agentStatuses).toBeNull();
    expect(api.fetchModelsStatus).toHaveBeenCalledOnce();
    await act(async () => useAuthStore.setState({ authRequired: false, generation: Symbol() }));
    expect(api.fetchModelsStatus).toHaveBeenCalledTimes(2);
    expect(useContextStore.getState().doc?.canonical.path).toBe('/fixture/context/AGENTS.md');
  });

  it('pauses session polling and ignores its in-flight result during re-entry', async () => {
    vi.useFakeTimers();
    const sessions = deferred<Awaited<ReturnType<typeof api.fetchSessions>>>();
    vi.mocked(api.fetchSessions).mockReturnValueOnce(sessions.promise);
    renderWorkspace();
    act(() => useAuthStore.getState().rejectGeneration(useAuthStore.getState().generation));
    await act(async () => {
      sessions.resolve({ count: 1, sessions: ['old-session'], entries: [{ id: 'old-session', generation: 'handshake' }] });
      await vi.advanceTimersByTimeAsync(POLLING.SESSIONS * 2);
    });
    expect(useStackStore.getState().sessionEntries).toBeNull();
    expect(api.fetchSessions).toHaveBeenCalledOnce();
    await act(async () => useAuthStore.setState({ authRequired: false, generation: Symbol() }));
    expect(api.fetchSessions).toHaveBeenCalledTimes(2);
    expect(useStackStore.getState().sessionEntries).toEqual([]);
  });

  it('renders every client with status badges', () => {
    renderWorkspace();
    // The name renders in the rail row, and again in the detail pane when
    // the client is the default selection.
    expect(screen.getAllByText('Claude Desktop').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Linked').length).toBeGreaterThanOrEqual(2);
    expect(screen.getAllByText('Declared').length).toBeGreaterThanOrEqual(2);
    // Detected-but-unlinked badge for grok.
    expect(screen.getByText('Detected')).toBeInTheDocument();
    // Undetected clients still get a rail row (with a disabled toggle);
    // the "not installed" detail lives in the detail pane now.
    expect(screen.getByText('Zed')).toBeInTheDocument();
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
