import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { act, cleanup, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import ConnectionsWorkspace from '../components/workspaces/ConnectionsWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useContextStore } from '../stores/useContextStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import { useAuthStore } from '../stores/useAuthStore';
import { useUIStore } from '../stores/useUIStore';
import { NavigationGuardDialog } from '../components/shell/NavigationGuardDialog';
import { POLLING } from '../lib/constants';
import * as api from '../lib/api';
import type { ClientStatus } from '../types';
import { WORKSPACE_LABELS } from '../types/workspace';

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

function expectOneConnectionsHeading() {
  expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
  const h1 = screen.getByRole('heading', { level: 1, name: WORKSPACE_LABELS.connections });
  expect(h1.className).not.toMatch(/sr-only|uppercase|tracking-/);
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

describe('ConnectionsWorkspace', () => {
  it('renders one Connections heading while loaded, loading, empty, and failed', async () => {
    renderWorkspace();
    expectOneConnectionsHeading();
    cleanup();

    useStackStore.setState({ clients: [] });
    const clientsFetch = deferred<ClientStatus[]>();
    vi.mocked(api.fetchClients).mockReturnValue(clientsFetch.promise);
    renderWorkspace();
    expect(screen.getByText('Loading connections…')).toBeInTheDocument();
    expectOneConnectionsHeading();

    await act(async () => { clientsFetch.resolve([]); });
    expect(await screen.findByText('No clients available')).toBeInTheDocument();
    expectOneConnectionsHeading();
    cleanup();

    useStackStore.setState({ clients: [] });
    vi.mocked(api.fetchClients).mockRejectedValue(new Error('clients down'));
    renderWorkspace();
    expect(await screen.findByText('Connections unavailable')).toBeInTheDocument();
    expectOneConnectionsHeading();
  });

  it.each(['initial', 'post-apply'] as const)('ignores late %s health results after unmount', async (phase) => {
    const wiring = deferred<Awaited<ReturnType<typeof api.fetchWiringStatus>>>();
    if (phase === 'initial') vi.mocked(api.fetchWiringStatus).mockReturnValueOnce(wiring.promise);
    const view = renderWorkspace();
    if (phase === 'post-apply') {
      await act(async () => {});
      vi.mocked(api.fetchWiringStatus).mockReturnValueOnce(wiring.promise);
      vi.spyOn(api, 'unlinkClient').mockResolvedValue({ client: 'claude', serverName: 'gridctl', linked: false, declared: false });
      fireEvent.click(screen.getByRole('switch', { name: 'Link Claude Desktop' }));
      fireEvent.click(screen.getByText('Review & Apply'));
      fireEvent.click(screen.getByText('Apply changes'));
      await waitFor(() => expect(api.fetchWiringStatus).toHaveBeenCalledTimes(2));
    }
    view.unmount();
    useStackStore.setState({ clients: [] });
    useRegistryStore.setState({ agentStatuses: null });
    await act(async () => { wiring.reject(new Error('Wiring unavailable')); });
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
    expect(api.fetchWiringStatus).toHaveBeenCalledOnce();
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
    expect(api.fetchWiringStatus).toHaveBeenCalledOnce();
    await act(async () => useAuthStore.setState({ authRequired: false, generation: Symbol() }));
    expect(api.fetchWiringStatus).toHaveBeenCalledTimes(2);
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
    const wiring = deferred<Awaited<ReturnType<typeof api.fetchWiringStatus>>>();
    const context = deferred<Awaited<ReturnType<typeof api.fetchGlobalContext>>>();
    vi.mocked(api.fetchWiringStatus).mockReturnValueOnce(wiring.promise);
    vi.mocked(api.fetchGlobalContext).mockReturnValueOnce(context.promise);
    renderWorkspace();
    act(() => useAuthStore.getState().rejectGeneration(useAuthStore.getState().generation));
    await act(async () => {
      context.resolve({ canonical: { path: '/old', exists: false, content: '' }, needs_sync: false, clients: [] });
      wiring.reject(new Error('Old request failed'));
    });
    expect(useContextStore.getState().doc).toBeNull();
    expect(useRegistryStore.getState().agentStatuses).toBeNull();
    expect(api.fetchWiringStatus).toHaveBeenCalledOnce();
    await act(async () => useAuthStore.setState({ authRequired: false, generation: Symbol() }));
    expect(api.fetchWiringStatus).toHaveBeenCalledTimes(2);
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

  it('moves focus with the selection and keeps it on a focused connect switch', async () => {
    renderWorkspace();
    const rows = () => screen.getAllByRole('button', { name: /^Inspect / });
    fireEvent.click(rows()[0]);
    rows()[0].focus();
    await waitFor(() => expect(rows()[0]).toHaveAttribute('aria-current', 'true'));

    fireEvent.keyDown(rows()[0], { key: 'ArrowDown' });
    await waitFor(() => expect(rows()[1]).toHaveAttribute('aria-current', 'true'));
    expect(rows()[1]).toHaveFocus();
    expect(fireEvent.keyDown(rows()[1], { key: 'Enter' })).toBe(false);
    expect(rows()[1]).toHaveAttribute('aria-current', 'true');

    const toggle = within(rows()[1]).getByRole('switch');
    toggle.focus();
    fireEvent.keyDown(toggle, { key: 'j' });
    await waitFor(() => expect(rows()[2]).toHaveAttribute('aria-current', 'true'));
    expect(toggle).toHaveFocus();
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

  it('shows wiring as unavailable and retries the failed fetch', async () => {
    vi.mocked(api.fetchWiringStatus).mockRejectedValue(new Error('Wiring unavailable'));
    renderWorkspace();
    await waitFor(() => {
      expect(screen.getByText('Ownership').parentElement).toHaveTextContent('unavailable');
    });
    expect(screen.getByText('Ownership').parentElement).not.toHaveTextContent('loading');
    expect(screen.getByText('Wiring status could not be loaded.')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(api.fetchWiringStatus).toHaveBeenCalledTimes(2));
  });

  it('renders a failed state when clients cannot be loaded', async () => {
    useStackStore.setState({ clients: [] });
    vi.mocked(api.fetchClients).mockRejectedValue(new Error('clients down'));
    renderWorkspace();
    expect(await screen.findByText('Connections unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No clients available')).not.toBeInTheDocument();
  });

  it('renders the teaching empty state when no clients are detected', async () => {
    useStackStore.setState({ clients: [] });
    vi.mocked(api.fetchClients).mockResolvedValue([]);
    renderWorkspace();
    await waitFor(() => expect(api.fetchClients).toHaveBeenCalled());
    await act(async () => {});
    expect(screen.getByText('No clients available')).toBeInTheDocument();
    expect(screen.queryByText('Connections unavailable')).not.toBeInTheDocument();
  });

  it('shows loading, not the teaching empty state, until the health fetch settles', async () => {
    useStackStore.setState({ clients: [] });
    const clientsFetch = deferred<ClientStatus[]>();
    vi.mocked(api.fetchClients).mockReturnValue(clientsFetch.promise);
    renderWorkspace();
    expect(screen.getByText('Loading connections…')).toBeInTheDocument();
    expect(screen.queryByText('No clients available')).not.toBeInTheDocument();
    expect(screen.queryByText('Connections unavailable')).not.toBeInTheDocument();

    await act(async () => { clientsFetch.resolve([]); });
    expect(await screen.findByText('No clients available')).toBeInTheDocument();
    expect(screen.queryByText('Loading connections…')).not.toBeInTheDocument();
    expect(screen.queryByText('Connections unavailable')).not.toBeInTheDocument();
  });

  it('keeps the failed state while a client retry is in flight', async () => {
    useStackStore.setState({ clients: [] });
    vi.mocked(api.fetchClients).mockRejectedValue(new Error('clients down'));
    renderWorkspace();
    expect(await screen.findByText('Connections unavailable')).toBeInTheDocument();

    const retry = deferred<ClientStatus[]>();
    vi.mocked(api.fetchClients).mockReturnValue(retry.promise);
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(screen.getByText('Connections unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No clients available')).not.toBeInTheDocument();
    expect(screen.queryByText('Loading connections…')).not.toBeInTheDocument();

    await act(async () => { retry.reject(new Error('clients down')); });
    await waitFor(() => expect(api.fetchClients).toHaveBeenCalledTimes(2));
    expect(screen.getByText('Connections unavailable')).toBeInTheDocument();
    expect(screen.queryByText('No clients available')).not.toBeInTheDocument();
  });
});

describe('ConnectionsWorkspace — staged change guard', () => {
  function LocationProbe() {
    const location = useLocation();
    return <div data-testid="location">{location.pathname + location.search}</div>;
  }

  function renderGuarded() {
    return render(
      <MemoryRouter initialEntries={['/connections?client=grok']}>
        <Routes>
          <Route path="/connections" element={<ConnectionsWorkspace />} />
          <Route path="/tools" element={<div>TOOLS PAGE</div>} />
          <Route path="/library" element={<div>LIBRARY PAGE</div>} />
        </Routes>
        <NavigationGuardDialog />
        <LocationProbe />
      </MemoryRouter>,
    );
  }

  afterEach(() => {
    useUIStore.setState({ resetDialogOpen: false });
  });

  function beforeUnloadPrevented() {
    const e = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(e);
    return e.defaultPrevented;
  }

  it('warns on reload while changes are staged and not after Discard', () => {
    renderWorkspace();
    expect(beforeUnloadPrevented()).toBe(false);
    fireEvent.click(screen.getByRole('switch', { name: 'Link Grok Build' }));
    expect(beforeUnloadPrevented()).toBe(true);
    fireEvent.click(screen.getByText('Discard'));
    expect(beforeUnloadPrevented()).toBe(false);
  });

  it('does not warn on reload after a full apply', async () => {
    vi.spyOn(api, 'linkClient').mockResolvedValue({
      client: 'grok', serverName: 'gridctl', linked: true, declared: true,
    });
    renderWorkspace();
    fireEvent.click(screen.getByRole('switch', { name: 'Link Grok Build' }));
    fireEvent.click(screen.getByText('Review & Apply'));
    fireEvent.click(await screen.findByText('Apply changes'));
    await waitFor(() => expect(screen.queryByText(/pending change/)).not.toBeInTheDocument());
    expect(beforeUnloadPrevented()).toBe(false);
  });

  it('does not warn while the reset dialog is open', () => {
    renderWorkspace();
    fireEvent.click(screen.getByRole('switch', { name: 'Link Grok Build' }));
    act(() => useUIStore.setState({ resetDialogOpen: true }));
    expect(beforeUnloadPrevented()).toBe(false);
  });

  it('holds Edit in Tools behind the shared confirm while changes are staged', async () => {
    renderGuarded();
    fireEvent.click(screen.getByRole('switch', { name: 'Link Grok Build' }));
    fireEvent.click(screen.getByRole('button', { name: /access scope/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Edit in Tools' }));

    expect(
      await screen.findByRole('alertdialog', { name: 'Discard unsaved changes' }),
    ).toHaveTextContent('1 staged connection change');
    expect(screen.getByTestId('location')).toHaveTextContent('/connections?client=grok');

    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(await screen.findByText('TOOLS PAGE')).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/tools?client=grok');
  });

  it('navigates to Tools at once with nothing staged', async () => {
    renderGuarded();
    fireEvent.click(screen.getByRole('button', { name: /access scope/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Edit in Tools' }));
    expect(await screen.findByText('TOOLS PAGE')).toBeInTheDocument();
    expect(screen.queryByRole('alertdialog')).toBeNull();
  });

  it('does not prompt when selecting another client in the rail', async () => {
    renderGuarded();
    fireEvent.click(screen.getByRole('switch', { name: 'Link Grok Build' }));
    fireEvent.click(screen.getByRole('button', { name: 'Inspect Cursor' }));
    await waitFor(() =>
      expect(screen.getByTestId('location')).toHaveTextContent('/connections?client=cursor'),
    );
    expect(screen.queryByRole('alertdialog')).toBeNull();
    expect(screen.getByText('1 pending change')).toBeInTheDocument();
  });

  it('navigates Review in Global Context to /library?kind=context&client=<slug> without a dialog', async () => {
    vi.mocked(api.fetchGlobalContext).mockResolvedValue({
      canonical: { path: '/fixture/context/AGENTS.md', exists: true, content: '# Rules\n' },
      needs_sync: true,
      clients: [
        {
          slug: 'grok',
          name: 'Grok Build',
          supported: true,
          available: true,
          strategy: 'block',
          target_path: '/home/u/.grok/AGENTS.md',
          state: 'drifted',
        },
      ],
    });
    renderGuarded();
    const review = await screen.findByRole('button', { name: 'Review' });
    fireEvent.click(review);
    expect(await screen.findByText('LIBRARY PAGE')).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/library?kind=context&client=grok');
    expect(screen.queryByRole('dialog')).toBeNull();
  });

  it('guards Review in Global Context when connection changes are staged', async () => {
    vi.mocked(api.fetchGlobalContext).mockResolvedValue({
      canonical: { path: '/fixture/context/AGENTS.md', exists: true, content: '# Rules\n' },
      needs_sync: true,
      clients: [
        {
          slug: 'grok',
          name: 'Grok Build',
          supported: true,
          available: true,
          strategy: 'block',
          target_path: '/home/u/.grok/AGENTS.md',
          state: 'drifted',
        },
      ],
    });
    renderGuarded();
    fireEvent.click(screen.getByRole('switch', { name: 'Link Grok Build' }));
    const review = await screen.findByRole('button', { name: 'Review' });
    fireEvent.click(review);
    expect(
      await screen.findByRole('alertdialog', { name: 'Discard unsaved changes' }),
    ).toHaveTextContent('1 staged connection change');
    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(await screen.findByText('LIBRARY PAGE')).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/library?kind=context&client=grok');
  });
});
