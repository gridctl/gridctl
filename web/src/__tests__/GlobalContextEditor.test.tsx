import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { CommandRegistryProvider, useCommandRegistry } from '../hooks/useCommandRegistry';
import { ContextWorkspace } from '../components/context/ContextWorkspace';
import { useContextStore } from '../stores/useContextStore';
import { useNavigationGuardStore } from '../stores/useNavigationGuardStore';
import { NavigationGuardDialog } from '../components/shell/NavigationGuardDialog';
import type { ContextDoc } from '../lib/api';
import type { RegistryKind } from '../lib/registryKind';
import connectionsSource from '../components/workspaces/ConnectionsWorkspace.tsx?raw';
import headerSource from '../components/layout/Header.tsx?raw';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchGlobalContext: vi.fn(),
    saveGlobalContext: vi.fn(),
    scanGlobalContext: vi.fn(),
    initGlobalContext: vi.fn(),
    syncGlobalContext: vi.fn(),
    adoptGlobalContext: vi.fn(),
    unsyncGlobalContext: vi.fn(),
    fetchGlobalContextDiff: vi.fn(),
    fetchProposals: vi.fn().mockResolvedValue({ schema_version: 1, proposals: [], skipped: [], truncated: false }),
  };
});

import { AuthError } from '../lib/gatewayRequest';
import {
  adoptGlobalContext,
  fetchGlobalContext,
  fetchGlobalContextDiff,
  initGlobalContext,
  saveGlobalContext,
  scanGlobalContext,
  syncGlobalContext,
} from '../lib/api';
import * as toastMod from '../components/ui/Toast';

const emptyDoc: ContextDoc = {
  canonical: { path: '/home/u/.gridctl/context/AGENTS.md', exists: false, content: '' },
  needs_sync: false,
  clients: [],
};

const readyDoc: ContextDoc = {
  canonical: {
    path: '/home/u/.gridctl/context/AGENTS.md',
    exists: true,
    content: '# Rules\n',
  },
  needs_sync: true,
  clients: [
    {
      slug: 'claude-code',
      name: 'Claude Code',
      supported: true,
      available: true,
      strategy: 'dedicated-file',
      target_path: '/home/u/.claude/rules/gridctl.md',
      state: 'in-sync',
    },
    {
      slug: 'gemini',
      name: 'Gemini CLI',
      supported: true,
      available: true,
      strategy: 'import-shim',
      target_path: '/home/u/.gemini/GEMINI.md',
      state: 'never-synced',
    },
    {
      slug: 'opencode',
      name: 'OpenCode',
      supported: true,
      available: true,
      strategy: 'block',
      target_path: '/home/u/.config/opencode/AGENTS.md',
      state: 'drifted',
    },
    {
      slug: 'cursor',
      name: 'Cursor',
      supported: false,
      available: false,
      state: 'unsupported',
      detail: 'global User Rules are stored in app-internal storage; no supported file path',
    },
  ],
};

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function CommandProbe() {
  const { commands } = useCommandRegistry();
  return (
    <div data-testid="commands">
      {commands.map((c) => (
        <button key={c.id} data-testid={`cmd:${c.id}`} onClick={c.onSelect}>
          {c.label}
        </button>
      ))}
    </div>
  );
}

function renderContext(
  options: {
    url?: string;
    onKindChange?: (kind: RegistryKind) => void;
    withGuardHost?: boolean;
    withCommands?: boolean;
  } = {},
) {
  const {
    url = '/library?kind=context',
    onKindChange = vi.fn(),
    withGuardHost = false,
    withCommands = false,
  } = options;
  const result = render(
    <CommandRegistryProvider>
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route
            path="/library"
            element={
              <>
                <ContextWorkspace onKindChange={onKindChange} />
                {withCommands && <CommandProbe />}
              </>
            }
          />
          <Route path="*" element={<div data-testid="other-route" />} />
        </Routes>
        <LocationProbe />
        {withGuardHost && <NavigationGuardDialog />}
      </MemoryRouter>
    </CommandRegistryProvider>,
  );
  return { ...result, onKindChange };
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  useContextStore.setState({ doc: null, loading: false, error: null });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('ContextWorkspace single-file mode', () => {
  it('shows a loading state with no inspector before the first fetch resolves', () => {
    vi.mocked(fetchGlobalContext).mockReturnValue(new Promise(() => {}));
    renderContext();
    expect(screen.getByText('Loading global context…')).toBeInTheDocument();
    expect(screen.queryByRole('complementary', { name: 'Global context inspector' })).not.toBeInTheDocument();
  });

  it('shows the adoption-first setup view with no inspector when no canonical file exists', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(emptyDoc);
    vi.mocked(scanGlobalContext).mockResolvedValue([
      { slug: 'claude-code', name: 'Claude Code', path: '/home/u/.claude/CLAUDE.md', exists: true, size: 42 },
      { slug: 'gemini', name: 'Gemini CLI', path: '/home/u/.gemini/GEMINI.md', exists: false, size: 0 },
    ]);

    renderContext();

    await waitFor(() => {
      expect(screen.getByText('Set up your global context')).toBeInTheDocument();
    });
    expect(screen.queryByRole('complementary', { name: 'Global context inspector' })).not.toBeInTheDocument();
    // Existing file offered for import; non-existing one is not.
    expect(screen.getByText('Import from Claude Code')).toBeInTheDocument();
    expect(screen.queryByText('Import from Gemini CLI')).not.toBeInTheDocument();
    expect(screen.getByText('Start from the starter template')).toBeInTheDocument();
    // Nothing was written during the scan.
    expect(initGlobalContext).not.toHaveBeenCalled();
  });

  it('creates the canonical file from an imported client', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(emptyDoc);
    vi.mocked(scanGlobalContext).mockResolvedValue([
      { slug: 'claude-code', name: 'Claude Code', path: '/home/u/.claude/CLAUDE.md', exists: true, size: 42 },
    ]);
    vi.mocked(initGlobalContext).mockResolvedValue(readyDoc);

    renderContext();
    await waitFor(() => {
      expect(screen.getByText('Import from Claude Code')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Import from Claude Code'));
    fireEvent.click(screen.getByText('Create canonical file'));

    await waitFor(() => {
      expect(initGlobalContext).toHaveBeenCalledWith({ source: 'client', client: 'claude-code' });
    });
    // The editor view replaces the setup view.
    await waitFor(() => {
      expect(screen.getByLabelText('Canonical global context')).toHaveValue('# Rules\n');
    });
  });

  it('defaults the setup choice to the first existing client file', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(emptyDoc);
    vi.mocked(scanGlobalContext).mockResolvedValue([
      { slug: 'claude-code', name: 'Claude Code', path: '/home/u/.claude/CLAUDE.md', exists: true, size: 42 },
    ]);
    vi.mocked(initGlobalContext).mockResolvedValue(readyDoc);

    renderContext();
    await waitFor(() => {
      expect(screen.getByText('Import from Claude Code')).toBeInTheDocument();
    });

    expect(screen.getByRole('radio', { name: /Import from Claude Code/ })).toBeChecked();

    // Creating without touching the radios imports rather than templating.
    fireEvent.click(screen.getByText('Create canonical file'));
    await waitFor(() => {
      expect(initGlobalContext).toHaveBeenCalledWith({ source: 'client', client: 'claude-code' });
    });
  });

  it('reopens the source picker from the editor and replaces with force', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    vi.mocked(scanGlobalContext).mockResolvedValue([
      { slug: 'claude-code', name: 'Claude Code', path: '/home/u/.claude/CLAUDE.md', exists: true, size: 42 },
    ]);
    vi.mocked(initGlobalContext).mockResolvedValue(readyDoc);

    renderContext();
    await waitFor(() => {
      expect(screen.getByText('Import')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Import'));
    await waitFor(() => {
      expect(screen.getByText('Import from Claude Code')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Replace canonical'));
    await waitFor(() => {
      expect(initGlobalContext).toHaveBeenCalledWith({
        source: 'client',
        client: 'claude-code',
        force: true,
      });
    });
  });

  it('renders per-client state chips and drift review action in the Clients inspector tab', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);

    renderContext();

    const inspector = await screen.findByRole('complementary', { name: 'Global context inspector' });
    expect(within(inspector).getByRole('tab', { name: 'Clients' })).toHaveAttribute('aria-selected', 'true');
    expect(within(inspector).getByText('1 client needs attention')).toBeInTheDocument();
    expect(within(inspector).getByText('Claude Code')).toBeInTheDocument();
    expect(within(inspector).getByText('in-sync')).toBeInTheDocument();
    expect(within(inspector).getByText('never-synced')).toBeInTheDocument();
    expect(within(inspector).getByText('drifted')).toBeInTheDocument();
    expect(within(inspector).getByText('unsupported')).toBeInTheDocument();
    // Drifted client gets a Review action; unsupported client shows its reason.
    expect(within(inspector).getByText('Review')).toBeInTheDocument();
    expect(within(inspector).getByText(/app-internal storage/)).toBeInTheDocument();
    // No collapsible Clients strip toggle remains above the editor.
    expect(screen.queryByRole('button', { name: /Clients/i })).not.toBeInTheDocument();
  });

  it('switches inspector tabs, reflects tab=details in the URL, and links to Pins', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue({
      ...readyDoc,
      pins: { drifted: ['AGENTS.md'], unpinned: [], missing: [] },
    });

    renderContext({ url: '/library?kind=context&tab=details' });

    const inspector = await screen.findByRole('complementary', { name: 'Global context inspector' });
    const detailsTab = within(inspector).getByRole('tab', { name: 'Details' });
    const clientsTab = within(inspector).getByRole('tab', { name: 'Clients' });
    expect(detailsTab).toHaveAttribute('aria-selected', 'true');
    expect(clientsTab).toHaveAttribute('aria-selected', 'false');

    const panel = within(inspector).getByRole('tabpanel', { name: 'Details' });
    expect(panel).toHaveTextContent('/home/u/.gridctl/context/AGENTS.md');
    expect(panel).toHaveTextContent('Pin drift');
    const pinsLink = within(panel).getByRole('link', { name: 'Review in Pins' });
    expect(pinsLink).toHaveAttribute('href', '/pins?kind=context&fragment=AGENTS.md&view=drift');

    fireEvent.click(clientsTab);
    expect(clientsTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('location').textContent).toBe('/library?kind=context');

    fireEvent.click(detailsTab);
    expect(detailsTab).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('location').textContent).toBe('/library?kind=context&tab=details');
  });

  it('drops a stale fragment parameter silently in single-file mode', async () => {
    const toastSpy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);

    renderContext({ url: '/library?kind=context&fragment=10-style' });
    await screen.findByLabelText('Canonical global context');

    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/library?kind=context');
    });
    expect(toastSpy).not.toHaveBeenCalled();
  });

  it('handles ?client= deep links for drifted, clean, and unknown clients', async () => {
    const toastSpy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    vi.mocked(fetchGlobalContextDiff).mockResolvedValue('@@ -1 +1 @@\n-a\n+b\n');

    // Drifted client opens drift review and removes client from the URL.
    renderContext({ url: '/library?kind=context&tab=details&client=opencode' });
    expect(await screen.findByText('OpenCode was edited')).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/library?kind=context');
    });
    expect(screen.getByRole('tab', { name: 'Clients' })).toHaveAttribute('aria-selected', 'true');
    cleanup();

    // Non-drifted client spotlights its row, selects Clients, and opens no dialog.
    useContextStore.setState({ doc: null, loading: false, error: null });
    renderContext({ url: '/library?kind=context&tab=details&client=claude-code' });
    await screen.findByLabelText('Canonical global context');
    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/library?kind=context');
    });
    expect(screen.getByRole('tab', { name: 'Clients' })).toHaveAttribute('aria-selected', 'true');
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    const claudeItem = screen.getByText('Claude Code').closest('li');
    expect(claudeItem).toHaveAttribute('data-spotlight', 'true');
    cleanup();

    // Unknown slug toasts once and is removed.
    useContextStore.setState({ doc: null, loading: false, error: null });
    renderContext({ url: '/library?kind=context&client=missing-client' });
    await screen.findByLabelText('Canonical global context');
    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/library?kind=context');
    });
    expect(toastSpy).toHaveBeenCalledWith('error', 'Client "missing-client" not found');
  });

  it('links to Pins from the save toast when the canonical pin drifts', async () => {
    const toastSpy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    vi.mocked(saveGlobalContext).mockResolvedValue({
      ...readyDoc,
      canonical: { ...readyDoc.canonical, content: '# Edited\n' },
      pin_status: 'drift',
    });

    renderContext();
    const editor = await screen.findByLabelText('Canonical global context');
    fireEvent.change(editor, { target: { value: '# Edited\n' } });
    fireEvent.click(screen.getByRole('button', { name: /^save$/i }));

    await waitFor(() => {
      expect(toastSpy).toHaveBeenCalledWith(
        'success',
        'Canonical context saved. Run a sync to propagate. Pin drift: review and approve in Pins.',
        expect.objectContaining({
          action: expect.objectContaining({ label: 'Review in Pins' }),
        }),
      );
    });
    const opts = toastSpy.mock.calls.find((c) => c[0] === 'success')?.[2];
    act(() => {
      opts?.action?.onClick();
    });
    expect(screen.getByTestId('location').textContent).toBe('/pins?kind=context&fragment=AGENTS.md&view=drift');
  });

  it('sync all calls the API and summarizes results', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    vi.mocked(syncGlobalContext).mockResolvedValue({
      dry_run: false,
      has_failures: false,
      results: [
        {
          slug: 'gemini',
          name: 'Gemini CLI',
          strategy: 'import-shim',
          target_path: '/home/u/.gemini/GEMINI.md',
          action: 'created',
        },
      ],
    });

    renderContext();
    await waitFor(() => {
      expect(screen.getByText('Sync all')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Sync all'));
    await waitFor(() => {
      expect(syncGlobalContext).toHaveBeenCalledWith();
    });
  });

  it('confirms before discarding canonical edits on kind switch', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    const onKindChange = vi.fn();
    renderContext({ onKindChange });
    const editor = await screen.findByLabelText('Canonical global context');

    fireEvent.change(editor, { target: { value: '# Edited rules\n' } });
    fireEvent.click(screen.getByRole('button', { name: 'Skills' }));

    expect(onKindChange).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/unsaved edits/i);
    expect(screen.getByLabelText('Canonical global context')).toHaveValue('# Edited rules\n');

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onKindChange).not.toHaveBeenCalled();
    expect(screen.getByLabelText('Canonical global context')).toHaveValue('# Edited rules\n');

    fireEvent.click(screen.getByRole('button', { name: 'Skills' }));
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(onKindChange).toHaveBeenCalledWith('skill');
  });

  it('switches kind directly when the canonical editor has no edits', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    const onKindChange = vi.fn();
    renderContext({ onKindChange });
    await screen.findByLabelText('Canonical global context');

    fireEvent.click(screen.getByRole('button', { name: 'Skills' }));
    expect(onKindChange).toHaveBeenCalledWith('skill');
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('closes the import dialog alone on Escape without leaving the editor', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    const onKindChange = vi.fn();
    renderContext({ onKindChange });
    await screen.findByLabelText('Canonical global context');

    fireEvent.click(screen.getByRole('button', { name: /^import$/i }));
    const importDialog = screen.getByRole('dialog', { name: 'Import global context' });
    const button = importDialog.querySelector('button');
    expect(button).toBeTruthy();
    fireEvent.keyDown(button as HTMLElement, { key: 'Escape' });

    expect(screen.queryByRole('dialog', { name: 'Import global context' })).not.toBeInTheDocument();
    expect(screen.getByLabelText('Canonical global context')).toBeInTheDocument();
    expect(onKindChange).not.toHaveBeenCalled();
  });

  it('prevents unload while canonical edits are pending and clears on unmount', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    const { unmount } = renderContext();
    const editor = await screen.findByLabelText('Canonical global context');

    const clean = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(clean);
    expect(clean.defaultPrevented).toBe(false);

    fireEvent.change(editor, { target: { value: '# Edited rules\n' } });
    const dirty = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(dirty);
    expect(dirty.defaultPrevented).toBe(true);

    unmount();
    const after = new Event('beforeunload', { cancelable: true });
    window.dispatchEvent(after);
    expect(after.defaultPrevented).toBe(false);
  });

  it('registers segment palette commands and guards dirty sync and kind switches', async () => {
    const toastSpy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    vi.mocked(syncGlobalContext).mockResolvedValue({ dry_run: false, has_failures: false, results: [] });
    const onKindChange = vi.fn();
    renderContext({ onKindChange, withCommands: true });
    const editor = await screen.findByLabelText('Canonical global context');

    expect(screen.getByTestId('cmd:library:context-refresh')).toHaveTextContent('Library: Refresh Global Context');
    expect(screen.getByTestId('cmd:library:context-sync-all')).toHaveTextContent(
      'Library: Sync Global Context to All Clients',
    );
    expect(screen.getByTestId('cmd:library:view-skills')).toBeInTheDocument();
    expect(screen.getByTestId('cmd:library:view-agents')).toBeInTheDocument();
    expect(screen.getByTestId('cmd:library:view-hooks')).toBeInTheDocument();
    expect(screen.getByTestId('cmd:library:view-packs')).toBeInTheDocument();
    expect(screen.getByTestId('cmd:library:view-proposals')).toBeInTheDocument();

    // Sync while dirty warns instead of syncing.
    fireEvent.change(editor, { target: { value: '# Dirty\n' } });
    fireEvent.click(screen.getByTestId('cmd:library:context-sync-all'));
    expect(syncGlobalContext).not.toHaveBeenCalled();
    expect(toastSpy).toHaveBeenCalledWith('warning', 'Save or revert your changes before syncing');

    // Palette kind switch while dirty opens the discard confirm.
    fireEvent.click(screen.getByTestId('cmd:library:view-agents'));
    expect(onKindChange).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/unsaved edits/i);
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(onKindChange).toHaveBeenCalledWith('agent');
  });

  it('keeps ConnectionsWorkspace and Header free of editor and markdown imports', () => {
    expect(connectionsSource).not.toMatch(/GlobalContextEditor|MarkdownPreview/);
    expect(headerSource).not.toMatch(/GlobalContextEditor|MarkdownPreview/);
  });
});

describe('ContextWorkspace load failures', () => {
  it('shows a failed state with Retry on the first load, then recovers', async () => {
    vi.mocked(fetchGlobalContext).mockRejectedValueOnce(new Error('offline'));
    renderContext();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Global context unavailable');
    expect(alert).toHaveTextContent('Load global context failed: offline.');
    expect(screen.queryByText('Loading global context…')).not.toBeInTheDocument();
    expect(screen.queryByRole('complementary', { name: 'Global context inspector' })).not.toBeInTheDocument();

    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(fetchGlobalContext).toHaveBeenCalledTimes(2);
    expect(screen.queryByText('Global context unavailable')).not.toBeInTheDocument();
    expect(await screen.findByText('Claude Code')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('never shows the loading line and the failed state together', async () => {
    useContextStore.setState({ doc: null, loading: true, error: 'Load global context failed: offline.' });
    vi.mocked(fetchGlobalContext).mockReturnValue(new Promise(() => {}));
    renderContext();
    expect(screen.getByText('Loading global context…')).toBeInTheDocument();
    expect(screen.queryByText('Global context unavailable')).not.toBeInTheDocument();
  });

  it('describes a credential failure instead of the raw message', async () => {
    vi.mocked(fetchGlobalContext).mockRejectedValueOnce(new AuthError('Authentication required'));
    renderContext();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Load global context paused. Verify your gateway credential to resume.');
    expect(alert).not.toHaveTextContent('Authentication required');
  });

  it('keeps the editor under a banner with Retry when a refresh fails', async () => {
    useContextStore.setState({ doc: readyDoc, loading: false, error: null });
    vi.mocked(fetchGlobalContext).mockRejectedValueOnce(new Error('offline'));
    renderContext();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Load global context failed: offline.');
    expect(alert).not.toHaveTextContent('Refresh failed');
    expect(screen.getByText('Claude Code')).toBeInTheDocument();
    expect(screen.queryByText('Global context unavailable')).not.toBeInTheDocument();

    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
    expect(screen.getByText('Claude Code')).toBeInTheDocument();
  });
});

describe('ContextWorkspace navigation guard', () => {
  beforeEach(() => {
    useNavigationGuardStore.setState({ guards: [], hosts: 0, pending: null });
  });

  afterEach(() => {
    useNavigationGuardStore.setState({ guards: [], hosts: 0, pending: null });
  });

  it('registers a blocking guard labelled Global Context while edited and drops it on unmount', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    const { unmount } = renderContext({ withGuardHost: true });
    const editor = await screen.findByLabelText('Canonical global context');
    expect(useNavigationGuardStore.getState().guards).toHaveLength(0);

    fireEvent.change(editor, { target: { value: '# Edited rules\n' } });
    expect(useNavigationGuardStore.getState().isBlocked('/logs')).toBe(true);
    expect(useNavigationGuardStore.getState().isBlocked('/library')).toBe(true);

    const proceed = vi.fn();
    act(() => useNavigationGuardStore.getState().guard('/logs', proceed));
    expect(screen.getByText(/unsaved changes in Global Context/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(proceed).toHaveBeenCalledTimes(1);

    unmount();
    expect(useNavigationGuardStore.getState().guards).toHaveLength(0);
  });

  it('drops the guard once the edit is saved', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    const saved: ContextDoc = { ...readyDoc, canonical: { ...readyDoc.canonical, content: '# Edited rules\n' } };
    vi.mocked(saveGlobalContext).mockResolvedValue(saved);
    renderContext();
    const editor = await screen.findByLabelText('Canonical global context');

    fireEvent.change(editor, { target: { value: '# Edited rules\n' } });
    expect(useNavigationGuardStore.getState().guards).toHaveLength(1);

    fireEvent.click(screen.getByRole('button', { name: /^save$/i }));
    await waitFor(() => expect(saveGlobalContext).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(useNavigationGuardStore.getState().guards).toHaveLength(0));
  });
});

function deferred<T>() {
  let resolve!: (v: T) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe('single-file drift review evidence', () => {
  async function openReview() {
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    renderContext();
    fireEvent.click(await screen.findByText('Review'));
    await screen.findByText('OpenCode was edited');
  }

  it('gates Adopt and Overwrite until the diff loads, leaving Cancel enabled', async () => {
    const d = deferred<string>();
    vi.mocked(fetchGlobalContextDiff).mockReturnValue(d.promise);
    vi.mocked(adoptGlobalContext).mockResolvedValue(readyDoc);
    await openReview();

    expect(screen.getByText('Loading diff…')).toBeInTheDocument();
    const adopt = screen.getByRole('button', { name: 'Adopt into canon' });
    const overwrite = screen.getByRole('button', { name: 'Overwrite client' });
    expect(adopt).toBeDisabled();
    expect(adopt).toHaveAttribute('title', 'Review the diff before choosing');
    expect(overwrite).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeEnabled();
    expect(fetchGlobalContextDiff).toHaveBeenCalledWith('opencode');

    await act(async () => d.resolve('--- canonical\n+++ target\n@@ -1 +1 @@\n-a\n+b\n'));
    await waitFor(() => expect(adopt).toBeEnabled());
    expect(overwrite).toBeEnabled();
    expect(adopt).not.toHaveAttribute('title');
    const diff = screen.getByLabelText('Diff for OpenCode');
    expect(diff.querySelector('[data-kind="added"]')).toHaveTextContent('+b');

    fireEvent.click(adopt);
    await waitFor(() => expect(adoptGlobalContext).toHaveBeenCalledWith('opencode'));
  });

  it('shows a failed load as an alert with Retry and the single-file CLI route', async () => {
    vi.mocked(fetchGlobalContextDiff)
      .mockRejectedValueOnce(new Error('diff exploded'))
      .mockResolvedValueOnce('@@ -1 +1 @@\n-a\n+b\n');
    await openReview();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('diff exploded');
    expect(alert).toHaveTextContent('gridctl ctx diff opencode');
    expect(alert).toHaveTextContent('gridctl ctx adopt opencode');
    expect(alert).toHaveTextContent('gridctl ctx sync opencode --force');
    expect(screen.queryByLabelText('Diff for OpenCode')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Adopt into canon' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Overwrite client' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeEnabled();

    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Adopt into canon' })).toBeEnabled());
    expect(fetchGlobalContextDiff).toHaveBeenCalledTimes(2);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Diff for OpenCode')).not.toHaveTextContent('diff exploded');
  });

  it('escapes hidden characters in the failed-load message', async () => {
    vi.mocked(fetchGlobalContextDiff).mockRejectedValueOnce(new Error('bad\u202e'));
    await openReview();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('bad\\u202e');
    expect(alert.textContent).not.toContain('\u202e');
  });

  it('treats an empty diff as ready with no line-level differences', async () => {
    vi.mocked(fetchGlobalContextDiff).mockResolvedValue('');
    await openReview();

    expect(
      await screen.findByText('No line-level differences between the canonical content and this file.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Adopt into canon' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Overwrite client' })).toBeEnabled();
  });
});
