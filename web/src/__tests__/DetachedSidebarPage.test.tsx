import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router';
import type { SharedStatus } from '../hooks/useSharedStatus';
import type { GatewayStatus, MCPServerStatus } from '../types';

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
import { useInspectorToolsGuardStore } from '../stores/useInspectorToolsGuardStore';

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
  it('asks before a node change discards a tool draft, even from a collapsed section', async () => {
    useInspectorToolsGuardStore.setState({ dirtyServer: null, pendingLeave: null });
    const cache = { ...STATUS['mcp-servers'][0], name: 'cache', tools: ['get', 'set'] };
    setShared({ status: { ...STATUS, 'mcp-servers': [...STATUS['mcp-servers'], cache] } });
    renderAt('/sidebar?node=db');
    const section = screen.getByRole('button', { name: /^Tools/ });
    fireEvent.click(section);
    fireEvent.click(await screen.findByRole('option', { name: 'insert' }));
    fireEvent.click(section);

    const pick = (name: string) => {
      fireEvent.click(screen.getByRole('button', { name: /^(db|cache)$/ }));
      fireEvent.click(screen.getByRole('menuitemradio', { name: new RegExp(name) }));
    };
    pick('cache');
    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent('Discard unsaved changes to db?');
    expect(dialog.closest('[hidden]')).toBeNull();
    expect(document.title).toBe('Gridctl - Details: db');

    fireEvent.click(within(dialog).getByRole('button', { name: 'Keep editing' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(document.title).toBe('Gridctl - Details: db');
    expect(screen.getByRole('option', { name: 'insert', hidden: true })).toHaveAttribute(
      'aria-checked',
      'false',
    );

    pick('cache');
    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Discard' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(document.title).toBe('Gridctl - Details: cache');
    expect(setServerTools).not.toHaveBeenCalled();
  });

  it('shows health under State and the evidence sections collapsed between Status and Tools', async () => {
    const db = {
      ...STATUS['mcp-servers'][0],
      endpoint: 'http://db.internal:9000/mcp',
      healthy: false,
      lastCheck: new Date().toISOString(),
      healthError: 'db refused the connection',
      stderrTail: ['fatal: db exited'],
      replicas: [{ replicaId: 0, state: 'unhealthy', healthy: false, inFlight: 0, execution: { mode: 'hardened', eligible: true } }],
    } as unknown as MCPServerStatus;
    setShared({ status: { ...STATUS, 'mcp-servers': [db] } as unknown as GatewayStatus });
    renderAt('/sidebar?node=db');
    const statusToggle = screen.getByRole('button', { name: /^Status/ });
    const panel = document.getElementById(statusToggle.getAttribute('aria-controls')!)!;
    const inOrder = [
      within(panel).getByText('State'),
      within(panel).getByText('Health'),
      within(panel).getByText('Last Check'),
      within(panel).getByText('db refused the connection'),
      within(panel).getByText('fatal: db exited'),
      within(panel).getByText('Transport'),
      within(panel).getByText('Endpoint'),
    ];
    for (let i = 1; i < inOrder.length; i++) {
      expect(inOrder[i - 1].compareDocumentPosition(inOrder[i]) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
    expect(within(panel).getByText('Endpoint')).toHaveClass('log-text');
    expect(within(panel).getByRole('button', { name: 'Copy endpoint' })).toBeInTheDocument();
    expect(within(panel).queryByText(/Security evidence|Execution evidence|Refresh report/)).not.toBeInTheDocument();

    const security = screen.getByRole('button', { name: /^Security evidence/ });
    const execution = screen.getByRole('button', { name: /^Execution evidence/ });
    const tools = screen.getByRole('button', { name: /^Tools/ });
    expect(statusToggle.compareDocumentPosition(security) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(security.compareDocumentPosition(execution) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(execution.compareDocumentPosition(tools) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(security).toHaveAttribute('aria-expanded', 'false');
    expect(execution).toHaveAttribute('aria-expanded', 'false');
    expect(within(execution).getByText('1 of 1 replicas eligible')).toBeInTheDocument();
    await waitFor(() => expect(within(security).getByText('No checks for this server')).toBeInTheDocument());
    expect(fetchSecurityReport).toHaveBeenCalledTimes(1);
  });

  it('gives a resource a copyable image and an Execution evidence section', () => {
    setShared({
      status: { ...STATUS, resources: [{ name: 'cache', image: 'redis:7.2-alpine', status: 'running' }] } as unknown as GatewayStatus,
    });
    renderAt('/sidebar?node=cache');
    expect(screen.getByText('Image')).toHaveClass('log-text');
    expect(screen.getByRole('button', { name: 'Copy image' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^Security evidence/ })).not.toBeInTheDocument();
    const execution = screen.getByRole('button', { name: /^Execution evidence/ });
    expect(execution).toHaveAttribute('aria-expanded', 'false');
    expect(within(execution).getByText('Not covered for resources')).toBeInTheDocument();
  });

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

  it('titles the window with the selected node and renders one notifications region', () => {
    document.title = '';
    renderAt('/sidebar?node=db');
    expect(document.title).toBe('Gridctl - Details: db');
    expect(screen.getAllByRole('region', { name: 'Notifications' })).toHaveLength(1);
    expect(screen.getByRole('main')).toBeInTheDocument();
  });

  it('opens the node menu, labels MCP servers, and closes on Escape with focus returned', () => {
    renderAt('/sidebar');
    const trigger = screen.getByRole('button', { name: 'Select node...' });
    expect(trigger).toHaveAttribute('aria-haspopup', 'menu');
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(trigger);
    const menu = screen.getByRole('menu', { name: 'Node' });
    expect(within(menu).getByRole('menuitemradio', { name: /db/ })).toHaveTextContent('MCP server');
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(trigger).toHaveFocus();
  });

  it('closes the node menu on an outside click and selects a node', () => {
    renderAt('/sidebar');
    const trigger = screen.getByRole('button', { name: 'Select node...' });
    fireEvent.click(trigger);
    fireEvent.mouseDown(screen.getByRole('main'));
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    fireEvent.click(trigger);
    fireEvent.click(screen.getByRole('menuitemradio', { name: /db/ }));
    expect(screen.queryByRole('menu')).not.toBeInTheDocument();
    expect(document.title).toBe('Gridctl - Details: db');
  });

  it('zooms through the inspector preference, not the log font size', () => {
    localStorage.clear();
    renderAt('/sidebar');
    fireEvent.click(screen.getByRole('button', { name: 'Increase font size' }));
    expect(localStorage.getItem('gridctl-pref-inspector-zoom')).toBe('12');
    expect(localStorage.getItem('gridctl-pref-log-font-size')).toBeNull();
    localStorage.clear();
  });

  it('shows the disconnected banner when status fails with a node list still present', () => {
    setShared({ error: 'Status failed: down.' });
    renderAt('/sidebar?node=db');
    const alerts = screen.getAllByRole('alert');
    expect(alerts).toHaveLength(1);
    expect(alerts[0]).toHaveTextContent('Status failed: down.');
    expect(screen.getAllByText('db').length).toBeGreaterThan(0);
    fireEvent.click(within(alerts[0]).getByRole('button', { name: 'Retry' }));
    expect(shared.current.refresh).toHaveBeenCalledTimes(1);
  });

  it('shows one alert when the gateway is unavailable', () => {
    setShared({ status: null, settled: true, error: 'Status failed: down.' });
    renderAt('/sidebar?node=db');
    expect(screen.getAllByRole('alert')).toHaveLength(1);
  });
});
