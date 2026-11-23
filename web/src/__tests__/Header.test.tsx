import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { Header } from '../components/layout/Header';
import { useStackStore } from '../stores/useStackStore';
import { useAuthStore } from '../stores/useAuthStore';
import { showToast } from '../components/ui/Toast';
import { fetchStackHealth, fetchStackPlan, triggerReload } from '../lib/api';
import { useSpecStore } from '../stores/useSpecStore';
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

function clickReload() {
  fireEvent.click(screen.getByRole('button', { name: 'Reload Config' }));
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

  it('hides the persistence toggle when disconnected', () => {
    useStackStore.setState({ connectionStatus: 'disconnected' });
    renderHeader();
    expect(screen.queryByText(/Persistence:/)).not.toBeInTheDocument();
  });

  it('keeps header actions mounted and lets the workspace strip scroll', () => {
    renderHeader();
    const create = screen.getByRole('button', { name: 'Create new' });
    expect(screen.getByRole('button', { name: 'Refresh (⌘⇧R)' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Reload Config' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Command Palette (⌘K)' })).toBeInTheDocument();
    expect(create.closest('div')).toHaveClass('shrink-0');
    expect(screen.getByRole('navigation', { name: 'Workspaces' })).toHaveClass('overflow-x-auto');
  });

  it('offers Forget credential only while a credential is held', () => {
    renderHeader();
    expect(screen.queryByRole('button', { name: 'Forget credential' })).not.toBeInTheDocument();
    act(() => {
      useAuthStore.setState({ credential: { mode: 'bearer', header: 'Authorization', token: 'not-rendered' } });
    });
    cleanup();
    renderHeader();
    fireEvent.click(screen.getByRole('button', { name: 'Forget credential' }));
    expect(useAuthStore.getState().credential).toBeNull();
    expect(useAuthStore.getState().authRequired).toBe(true);
    expect(screen.queryByText('not-rendered')).not.toBeInTheDocument();
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
    await waitFor(() => expect(screen.getByRole('button', { name: 'Reload Config' })).not.toBeDisabled());
    expect(triggerReload).toHaveBeenCalledTimes(1);
  });

  it('stops silently when the pre-flight is cancelled', async () => {
    const abort = Object.assign(new Error('aborted'), { name: 'AbortError' });
    vi.mocked(fetchStackPlan).mockRejectedValue(abort);
    renderHeader();
    clickReload();
    await waitFor(() => expect(screen.getByRole('button', { name: 'Reload Config' })).not.toBeDisabled());
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(triggerReload).not.toHaveBeenCalled();
    expect(showToast).not.toHaveBeenCalled();
  });

  it('ignores a second click while the pre-flight is pending', async () => {
    let resolve!: (plan: PlanDiff) => void;
    vi.mocked(fetchStackPlan).mockReturnValue(new Promise((r) => { resolve = r; }));
    renderHeader();
    const button = screen.getByRole('button', { name: 'Reload Config' });
    fireEvent.click(button);
    fireEvent.click(button);
    expect(fetchStackPlan).toHaveBeenCalledTimes(1);
    expect(button).toBeDisabled();
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
    await waitFor(() => expect(screen.getByRole('button', { name: 'Reload Config' })).not.toBeDisabled());
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
    await waitFor(() => expect(screen.getByRole('button', { name: 'Reload Config' })).not.toBeDisabled());
    clickReload();
    await screen.findByRole('alert');
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Reload Config' })).not.toBeDisabled());
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
});
