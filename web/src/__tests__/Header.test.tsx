import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router';
import { Header } from '../components/layout/Header';
import { useStackStore } from '../stores/useStackStore';
import { useAuthStore } from '../stores/useAuthStore';
import { showToast } from '../components/ui/Toast';
import { fetchStackHealth, fetchStackPlan, triggerReload } from '../lib/api';
import { useSpecStore } from '../stores/useSpecStore';
import { useUIStore } from '../stores/useUIStore';
import { useWizardStore } from '../stores/useWizardStore';
import { ForgetCredentialDialog } from '../components/shell/ForgetCredentialDialog';
import headerSource from '../components/layout/Header.tsx?raw';
import headerMenuSource from '../components/shell/HeaderMenu.tsx?raw';
import type { PlanDiff } from '../types';
import type { MCPServerStatus } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));
vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>();
  return {
    ...actual,
    triggerReload: vi.fn(),
    fetchStackPlan: vi.fn(),
    fetchStackHealth: vi.fn(),
  };
});

const SERVERS: MCPServerStatus[] = [
  { name: 'github', transport: 'http', initialized: true, healthy: true, toolCount: 2, tools: ['search', 'create'] },
  { name: 'gitlab', transport: 'http', initialized: true, healthy: true, toolCount: 1, tools: ['list'] },
];

function renderWithDialog() {
  return render(
    <MemoryRouter initialEntries={['/stack']}>
      <Header />
      <ForgetCredentialDialog />
    </MemoryRouter>,
  );
}

function renderHeader() {
  return render(
    <MemoryRouter initialEntries={['/stack']}>
      <Header />
    </MemoryRouter>,
  );
}

const EMPTY_PLAN: PlanDiff = { hasChanges: false, items: [], summary: 'No changes detected.' };
const CHANGE_PLAN: PlanDiff = {
  hasChanges: true,
  items: [{ action: 'change', kind: 'mcp-server', name: 'github', details: ['image: a → b'] }],
  summary: '1 to change',
};
const HEALTH = {
  validation: { status: 'valid' as const, errorCount: 0, warningCount: 0 },
  drift: { status: 'drifted' as const, changed: ['github'] },
  dependencies: { status: 'resolved' as const },
};

function moreTrigger() {
  return screen.getByRole('button', { name: 'More actions' });
}

function openMore() {
  fireEvent.click(moreTrigger());
  return screen.getByRole('menu', { name: 'More actions' });
}

function clickReload() {
  if (!screen.queryByRole('menu', { name: 'More actions' })) openMore();
  fireEvent.click(screen.getByRole('menuitem', { name: 'Reload Config' }));
}

// The reload flow is idle once the More trigger stops reporting busy.
async function waitForReloadIdle() {
  await waitFor(() => expect(moreTrigger()).not.toHaveAttribute('aria-busy'));
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.mocked(fetchStackPlan).mockResolvedValue(EMPTY_PLAN);
  vi.mocked(fetchStackHealth).mockResolvedValue(HEALTH);
  vi.mocked(triggerReload).mockResolvedValue({ success: true, message: 'Reloaded: 1 modified' });
  useSpecStore.setState({
    health: null,
    plan: null,
    planLoading: false,
    planError: null,
    diffModalOpen: false,
    diffModalMode: 'apply',
  });
  useAuthStore.setState({ credential: null, authRequired: false, isAuthenticated: false });
  useUIStore.setState({ forgetCredentialOpen: false });
  useStackStore.setState({
    gatewayInfo: { name: 'acme-stack', version: 'v9.9.9' },
    mcpServers: SERVERS,
    connectionStatus: 'connected',
  });
});

describe('Header', () => {
  it('does not mirror connection state or server count (the StatusBar owns those)', () => {
    renderHeader();
    // The redundant header chips are gone.
    expect(screen.queryByText('Connected')).not.toBeInTheDocument();
    expect(screen.queryByText('Disconnected')).not.toBeInTheDocument();
    expect(screen.queryByText(/active/i)).not.toBeInTheDocument();
  });

  // The gateway name and version are both properties of the gateway, surfaced
  // on the canvas node and inspector sidebar. Neither belongs in the header.
  // gatewayInfo is populated in beforeEach, so these assertions prove the
  // header omits values that are available to it rather than an empty store.
  it('drops the gateway-name chip and the version beside the logo', () => {
    renderHeader();
    expect(screen.queryByText('acme-stack')).not.toBeInTheDocument();
    expect(screen.queryByText('v9.9.9')).not.toBeInTheDocument();
  });

  it('keeps the persistence quick-toggle while connected', () => {
    renderHeader();
    expect(screen.getByText(/Persistence:/)).toBeInTheDocument();
  });

  it('sizes the persistence trigger as a 32 px target and hides its label below 1440 px', () => {
    renderHeader();
    const label = screen.getByText(/Persistence:/);
    expect(label).toHaveClass('max-[1439px]:sr-only');
    expect(label).not.toHaveClass('max-sm:sr-only');
    const trigger = label.closest('button')!;
    expect(trigger).toHaveClass('min-h-8', 'min-w-8', 'justify-center', 'px-3', 'max-sm:px-2');
    expect(trigger).toHaveAttribute('title', 'Persistence: Off');
  });

  it('hides the persistence toggle when disconnected', () => {
    useStackStore.setState({ connectionStatus: 'disconnected' });
    renderHeader();
    expect(screen.queryByText(/Persistence:/)).not.toBeInTheDocument();
  });

  it('keeps header actions mounted and lets the workspace strip scroll', () => {
    renderHeader();
    const create = screen.getByRole('button', { name: 'Create new' });
    expect(screen.getByRole('button', { name: 'Command Palette (Ctrl+K)' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Help' })).toBeInTheDocument();
    expect(moreTrigger()).toBeInTheDocument();
    expect(create.parentElement).toHaveClass('shrink-0');
    expect(screen.getByRole('navigation', { name: 'Workspaces' })).toHaveClass('overflow-x-auto');
  });

  it('moves Refresh, Reload Config, and Forget credential into the More actions menu', () => {
    useAuthStore.setState({ credential: { mode: 'bearer', header: 'Authorization', token: 'held' } });
    renderHeader();
    expect(screen.queryByRole('button', { name: /^Refresh/ })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Reload Config' })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^Forget credential/ })).not.toBeInTheDocument();
    const trigger = moreTrigger();
    expect(trigger).toHaveAttribute('aria-haspopup', 'menu');
    expect(trigger).toHaveAttribute('aria-expanded', 'false');

    const menu = openMore();
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    const items = within(menu).getAllByRole('menuitem');
    expect(items.map((item) => item.textContent?.replace(/Ctrl|Shift|R$/g, '').trim())).toEqual([
      'Refresh',
      'Reload Config',
      'Forget credential…',
    ]);
    expect(within(menu).getByRole('separator')).toBeInTheDocument();
  });

  it('puts the header cluster in order with Create new as the primary button', () => {
    renderHeader();
    const create = screen.getByRole('button', { name: 'Create new' });
    expect(create).toHaveAttribute('title', 'Create new');
    expect(create).toHaveClass('h-[34px]', 'min-w-[34px]', 'px-0', 'min-[1360px]:px-3');
    expect(within(create).getByText('Create new')).toHaveClass('hidden', 'min-[1360px]:inline');
    const cluster = create.parentElement!;
    const names = Array.from(cluster.querySelectorAll('button'))
      .filter((b) => b.parentElement === cluster || b.parentElement?.parentElement === cluster)
      .map((b) => b.getAttribute('aria-label') ?? b.getAttribute('title'));
    expect(names.slice(-5)).toEqual([
      'Preferences',
      'Command Palette (Ctrl+K)',
      'Help',
      'Create new',
      'More actions',
    ]);
  });

  it('opens the creation wizard from Create new', () => {
    useWizardStore.setState({ isOpen: false });
    renderHeader();
    fireEvent.click(screen.getByRole('button', { name: 'Create new' }));
    expect(useWizardStore.getState().isOpen).toBe(true);
    act(() => {
      useWizardStore.setState({ isOpen: false });
    });
  });

  it('shows the Mac chord hint on the Refresh item on macOS', () => {
    const spy = vi.spyOn(window.navigator, 'platform', 'get').mockReturnValue('MacIntel');
    try {
      renderHeader();
      expect(screen.getByRole('button', { name: 'Command Palette (⌘K)' })).toBeInTheDocument();
      const menu = openMore();
      const refresh = within(menu).getByRole('menuitem', { name: 'Refresh' });
      expect(refresh).toHaveTextContent('⌘⇧R');
    } finally {
      spy.mockRestore();
    }
  });

  it('offers Forget credential only while a credential is held', () => {
    renderHeader();
    let menu = openMore();
    expect(within(menu).queryByRole('menuitem', { name: /Forget credential/ })).not.toBeInTheDocument();
    expect(within(menu).queryByRole('separator')).not.toBeInTheDocument();
    cleanup();
    useAuthStore.setState({ credential: { mode: 'bearer', header: 'Authorization', token: 'not-rendered' } });
    renderHeader();
    menu = openMore();
    expect(within(menu).getByRole('menuitem', { name: 'Forget credential…' })).toBeInTheDocument();
  });

  it('confirms before forgetting the credential and returns focus to More actions', async () => {
    useAuthStore.setState({ credential: { mode: 'bearer', header: 'Authorization', token: 'not-rendered' } });
    renderWithDialog();
    openMore();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Forget credential…' }));

    let dialog = screen.getByRole('alertdialog', { name: 'Forget gateway credential?' });
    expect(useAuthStore.getState().credential).not.toBeNull();
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    await waitFor(() => expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveFocus());
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(useAuthStore.getState().credential).not.toBeNull();
    expect(moreTrigger()).toHaveFocus();

    openMore();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Forget credential…' }));
    screen.getByRole('alertdialog', { name: 'Forget gateway credential?' });
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(useAuthStore.getState().credential).not.toBeNull();
    expect(moreTrigger()).toHaveFocus();

    openMore();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Forget credential…' }));
    dialog = screen.getByRole('alertdialog', { name: 'Forget gateway credential?' });
    expect(screen.queryByText('not-rendered')).not.toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Forget credential' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(useAuthStore.getState().credential).toBeNull();
    expect(useAuthStore.getState().authRequired).toBe(true);
    expect(useUIStore.getState().forgetCredentialOpen).toBe(false);
    expect(screen.queryByText('not-rendered')).not.toBeInTheDocument();
  });

  it('spins the More trigger and disables Refresh while refreshing', () => {
    const onRefresh = vi.fn();
    render(
      <MemoryRouter initialEntries={['/stack']}>
        <Header onRefresh={onRefresh} isRefreshing />
      </MemoryRouter>,
    );
    const trigger = moreTrigger();
    expect(trigger).toHaveAttribute('aria-busy', 'true');
    expect(trigger).not.toHaveClass('animate-spin');
    expect(trigger.querySelector('svg')).toHaveClass('animate-spin');
    openMore();
    const refresh = screen.getByRole('menuitem', { name: 'Refresh' });
    expect(refresh).toBeDisabled();
    expect(refresh.querySelector('svg')).toHaveClass('animate-spin');
    // Focus skips the disabled Refresh item.
    expect(screen.getByRole('menuitem', { name: 'Reload Config' })).toHaveFocus();
  });

  it('calls onRefresh from the Refresh item and closes the menu', () => {
    const onRefresh = vi.fn();
    render(
      <MemoryRouter initialEntries={['/stack']}>
        <Header onRefresh={onRefresh} />
      </MemoryRouter>,
    );
    expect(moreTrigger()).not.toHaveAttribute('aria-busy');
    openMore();
    fireEvent.click(screen.getByRole('menuitem', { name: 'Refresh' }));
    expect(onRefresh).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
  });

  it('opens Keyboard shortcuts and links Documentation from the Help menu', () => {
    useUIStore.setState({ shortcutsOpen: false });
    renderHeader();
    const help = screen.getByRole('button', { name: 'Help' });
    expect(help).toHaveAttribute('aria-haspopup', 'menu');
    fireEvent.click(help);
    const menu = screen.getByRole('menu', { name: 'Help' });
    const docs = within(menu).getByRole('menuitem', { name: 'Documentation' });
    expect(docs.tagName).toBe('A');
    expect(docs).toHaveAttribute('href', 'https://github.com/gridctl/gridctl/tree/main/docs');
    expect(docs).toHaveAttribute('target', '_blank');
    expect(docs).toHaveAttribute('rel', 'noreferrer');
    fireEvent.click(within(menu).getByRole('menuitem', { name: 'Keyboard shortcuts' }));
    expect(useUIStore.getState().shortcutsOpen).toBe(true);
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    useUIStore.setState({ shortcutsOpen: false });
  });

  it('renders RefreshCw once and never RotateCcw', () => {
    expect(headerSource).not.toMatch(/RotateCcw/);
    expect(headerMenuSource).not.toMatch(/RotateCcw/);
    expect(headerSource.match(/icon: RefreshCw\b|<RefreshCw\b/g)).toHaveLength(1);
  });

  it('toasts a failed reload and does not render a banner', async () => {
    vi.mocked(triggerReload).mockRejectedValue(new Error('reload broke'));
    renderHeader();
    clickReload();
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('error', expect.stringContaining('Reload failed'), expect.anything()));
    expect(fetchStackPlan).toHaveBeenCalledTimes(1);
    expect(screen.queryByText(/reload broke/)).not.toBeInTheDocument();
  });

  it('previews a plan with changes and reloads only on confirm', async () => {
    vi.mocked(fetchStackPlan).mockResolvedValue(CHANGE_PLAN);
    renderHeader();
    clickReload();
    const dialog = await screen.findByRole('dialog', { name: 'Reload Config' });
    expect(dialog).toHaveTextContent('github');
    expect(dialog).toHaveTextContent('image: a → b');
    expect(triggerReload).not.toHaveBeenCalled();
    await waitFor(() => expect(useSpecStore.getState().health).toEqual(HEALTH));
    fireEvent.click(screen.getByRole('button', { name: 'Reload' }));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Reloaded: 1 modified'));
    expect(triggerReload).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('does not reload after a failed pre-flight until Reload without preview', async () => {
    vi.mocked(fetchStackPlan).mockRejectedValue(new Error('candidate invalid'));
    renderHeader();
    clickReload();
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Reload preview failed: candidate invalid');
    expect(triggerReload).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Reload without preview' }));
    await waitFor(() => expect(triggerReload).toHaveBeenCalledTimes(1));
    await waitForReloadIdle();
    expect(triggerReload).toHaveBeenCalledTimes(1);
  });

  it('stops silently when the pre-flight is cancelled', async () => {
    const abort = Object.assign(new Error('aborted'), { name: 'AbortError' });
    vi.mocked(fetchStackPlan).mockRejectedValue(abort);
    renderHeader();
    clickReload();
    await waitForReloadIdle();
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(triggerReload).not.toHaveBeenCalled();
    expect(showToast).not.toHaveBeenCalled();
  });

  it('ignores a second click while the pre-flight is pending', async () => {
    let resolve!: (plan: PlanDiff) => void;
    vi.mocked(fetchStackPlan).mockReturnValue(new Promise((r) => { resolve = r; }));
    renderHeader();
    clickReload();
    expect(moreTrigger()).toHaveAttribute('aria-busy', 'true');
    openMore();
    const item = screen.getByRole('menuitem', { name: 'Reload Config' });
    expect(item).toBeDisabled();
    fireEvent.click(item);
    expect(fetchStackPlan).toHaveBeenCalledTimes(1);
    await act(async () => { resolve(EMPTY_PLAN); });
    await waitFor(() => expect(showToast).toHaveBeenCalledTimes(1));
    expect(triggerReload).toHaveBeenCalledTimes(1);
    expect(fetchStackPlan).toHaveBeenCalledTimes(1);
  });

  it('reloads directly with an empty plan and shows the server message', async () => {
    renderHeader();
    clickReload();
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Reloaded: 1 modified'));
    expect(triggerReload).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    await waitFor(() => expect(fetchStackHealth).toHaveBeenCalled());
  });

  it('releases the guard when the preview is dismissed', async () => {
    vi.mocked(fetchStackPlan).mockResolvedValue(CHANGE_PLAN);
    renderHeader();
    clickReload();
    await screen.findByRole('dialog', { name: 'Reload Config' });
    clickReload();
    expect(fetchStackPlan).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    await waitForReloadIdle();
    clickReload();
    expect(fetchStackPlan).toHaveBeenCalledTimes(2);
    expect(triggerReload).not.toHaveBeenCalled();
  });

  it('releases the guard on Escape and on the Close button', async () => {
    vi.mocked(fetchStackPlan).mockRejectedValue(new Error('candidate invalid'));
    renderHeader();
    clickReload();
    await screen.findByRole('alert');
    fireEvent.keyDown(document, { key: 'Escape' });
    await waitForReloadIdle();
    clickReload();
    await screen.findByRole('alert');
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    await waitForReloadIdle();
    clickReload();
    expect(fetchStackPlan).toHaveBeenCalledTimes(3);
    expect(triggerReload).not.toHaveBeenCalled();
  });

  it('does not write a health result after the preview was dismissed', async () => {
    let resolveHealth!: (h: typeof HEALTH) => void;
    vi.mocked(fetchStackHealth).mockReturnValue(new Promise((r) => { resolveHealth = r; }));
    vi.mocked(fetchStackPlan).mockResolvedValue(CHANGE_PLAN);
    renderHeader();
    clickReload();
    await screen.findByRole('dialog', { name: 'Reload Config' });
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    await act(async () => { resolveHealth(HEALTH); });
    expect(useSpecStore.getState().health).toBeNull();
  });

  it('navigates to /library?kind=context when the wizard Global Context tile is chosen', async () => {
    function LocationProbe() {
      const location = useLocation();
      return <div data-testid="header-location">{location.pathname + location.search}</div>;
    }
    useWizardStore.setState({ isOpen: true, currentStep: 'type' });
    render(
      <MemoryRouter initialEntries={['/stack']}>
        <Header />
        <LocationProbe />
      </MemoryRouter>,
    );
    const tile = await screen.findByText('Global Context');
    fireEvent.click(tile);
    await waitFor(() => {
      expect(screen.getByTestId('header-location')).toHaveTextContent('/library?kind=context');
    });
    expect(useWizardStore.getState().isOpen).toBe(false);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });
});
