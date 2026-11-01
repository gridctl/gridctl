import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { Header } from '../components/layout/Header';
import { useStackStore } from '../stores/useStackStore';
import { useAuthStore } from '../stores/useAuthStore';
import { showToast } from '../components/ui/Toast';
import { triggerReload } from '../lib/api';
import type { MCPServerStatus } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));
vi.mock('../lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../lib/api')>();
  return {
    ...actual,
    triggerReload: vi.fn(),
    fetchStackSpec: vi.fn().mockRejectedValue(new Error('spec unavailable')),
    validateStackSpec: vi.fn(),
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

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
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
    fireEvent.click(screen.getByRole('button', { name: 'Reload Config' }));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('error', expect.stringContaining('Reload failed'), expect.anything()));
    expect(screen.queryByText(/reload broke/)).not.toBeInTheDocument();
  });
});
