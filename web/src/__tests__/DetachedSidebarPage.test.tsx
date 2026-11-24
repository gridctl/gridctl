import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router';
import type { SharedStatus } from '../hooks/useSharedStatus';
import type { GatewayStatus } from '../types';

vi.mock('../hooks/useBroadcastChannel', () => ({
  useDetachedWindowSync: vi.fn(),
  useBroadcastChannel: () => ({ postMessage: vi.fn() }),
}));

const shared: { current: SharedStatus } = {
  current: { status: null, source: null, settled: false, error: null, refresh: vi.fn() },
};

vi.mock('../hooks/useSharedStatus', () => ({
  useSharedStatus: () => shared.current,
}));

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchTools: vi.fn(),
    fetchToolCatalog: vi.fn(),
    fetchStatus: vi.fn(),
    setServerTools: vi.fn(),
    fetchSecurityReport: vi.fn(),
  };
});

import { DetachedSidebarPage } from '../pages/DetachedSidebarPage';
import {
  fetchSecurityReport,
  fetchStatus,
  fetchToolCatalog,
  fetchTools,
  setServerTools,
} from '../lib/api';
import { useAuthStore } from '../stores/useAuthStore';

const STATUS = {
  gateway: { name: 'main', version: '0' },
  'mcp-servers': [
    {
      name: 'db',
      transport: 'stdio',
      endpoint: '',
      initialized: true,
      toolCount: 2,
      tools: ['query', 'insert'],
    },
  ],
  resources: [],
} as unknown as GatewayStatus;

function setShared(next: Partial<SharedStatus>) {
  shared.current = { ...shared.current, ...next };
}

function renderAt(entry: string) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/sidebar" element={<DetachedSidebarPage />} />
      </Routes>
    </MemoryRouter>,
  );
}

const region = () => screen.getByRole('region', { name: 'Notifications' });

async function saveOneToolChange() {
  renderAt('/sidebar?node=db');
  fireEvent.click(screen.getByRole('button', { name: /Tools/ }));
  fireEvent.click(await screen.findByRole('option', { name: 'insert' }));
  fireEvent.click(screen.getByRole('button', { name: /Save 1 change/ }));
}

beforeEach(() => {
  useAuthStore.setState({ authRequired: false });
  shared.current = {
    status: STATUS,
    source: 'self',
    settled: true,
    error: null,
    refresh: vi.fn().mockResolvedValue(undefined),
  };
  vi.mocked(fetchTools).mockResolvedValue({ tools: [] } as never);
  vi.mocked(fetchToolCatalog).mockResolvedValue({ tools: [] } as never);
  vi.mocked(fetchStatus).mockResolvedValue(STATUS);
  vi.mocked(fetchSecurityReport).mockResolvedValue({
    schema_version: 'gridctl.security-report.v1',
    generated_at: '2026-09-13T00:00:00Z',
    source: { kind: 'gateway', display: 'localhost' },
    coverage: { status: 'partial', predicates_total: 0, predicates_evaluated: 0, predicates_unknown: 0, included_scopes: [], excluded_scopes: [], unknown_gaps: 0 },
    checks: [],
    limitations: [],
    fail_count: 0,
    warn_count: 0,
    unknown_count: 0,
    not_applicable_count: 0,
    pass_count: 0,
  } as never);
});

afterEach(() => {
  // Toasts are module state and error toasts never time out.
  const live = screen.queryByRole('region', { name: 'Notifications' });
  if (live) {
    within(live).queryAllByRole('button', { name: 'Dismiss' }).forEach((b) => fireEvent.click(b));
  }
  useAuthStore.setState({ authRequired: false });
  vi.clearAllMocks();
});

describe('DetachedSidebarPage', () => {
  it('renders the notifications region', () => {
    renderAt('/sidebar');
    expect(region()).toBeInTheDocument();
  });

  it('shows an error toast when a tool save fails', async () => {
    vi.mocked(setServerTools).mockRejectedValue(new Error('boom'));
    await saveOneToolChange();
    const alert = await within(region()).findByRole('alert');
    expect(alert).toHaveTextContent('boom');
  });

  it('shows the reload warning when a save is not applied', async () => {
    vi.mocked(setServerTools).mockResolvedValue({ reloaded: false } as never);
    await saveOneToolChange();
    await waitFor(() => {
      expect(region()).toHaveTextContent('Run "gridctl reload"');
    });
  });

  it('shows the gateway as unavailable with Retry when status never loads', () => {
    setShared({ status: null, settled: true, error: 'Status failed: down.' });
    renderAt('/sidebar?node=db');
    expect(screen.getByRole('heading', { name: 'Gateway unavailable' })).toBeInTheDocument();
    expect(screen.getByText('Status failed: down.')).toBeInTheDocument();
    expect(screen.queryByText('Select a node to view details')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(shared.current.refresh).toHaveBeenCalledTimes(1);
  });

  it('names an unknown node as not found', () => {
    renderAt('/sidebar?node=ghost');
    expect(screen.getByText('Node "ghost" not found')).toBeInTheDocument();
    expect(screen.queryByText('Select a node to view details')).not.toBeInTheDocument();
    expect(screen.queryByText('Gateway unavailable')).not.toBeInTheDocument();
  });

  it('asks for a selection when status is loaded and no node is chosen', () => {
    renderAt('/sidebar');
    expect(screen.getByText('Select a node to view details')).toBeInTheDocument();
  });

  it('stays neutral and renders without error while a credential is required', () => {
    useAuthStore.setState({ authRequired: true });
    setShared({ status: null, settled: true, error: null });
    renderAt('/sidebar?node=db');
    expect(screen.queryByText('Something went wrong')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Reload Window' })).not.toBeInTheDocument();
    expect(screen.queryByText('Gateway unavailable')).not.toBeInTheDocument();
    expect(screen.queryByText(/not found/)).not.toBeInTheDocument();
    expect(screen.getByText('Select a node to view details')).toBeInTheDocument();
  });

  it('header Refresh refetches status through the shared hook and inventory', async () => {
    renderAt('/sidebar');
    vi.mocked(fetchTools).mockClear();
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    await waitFor(() => expect(shared.current.refresh).toHaveBeenCalledTimes(1));
    expect(fetchTools).toHaveBeenCalled();
    expect(fetchToolCatalog).toHaveBeenCalled();
  });
});
