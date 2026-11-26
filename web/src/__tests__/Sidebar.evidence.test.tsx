import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import type { Node } from '@xyflow/react';
import type { MCPServerStatus, SecurityReport } from '../types';

vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({ openDetachedWindow: vi.fn(), focusDetachedWindow: vi.fn() }),
}));

vi.mock('../hooks/usePolicy', () => ({
  usePolicy: () => ({ report: null, error: null }),
}));

const fetchSecurityReport = vi.fn();

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchSecurityReport: (...args: unknown[]) => fetchSecurityReport(...args),
  };
});

import { Sidebar } from '../components/layout/Sidebar';
import { GatewaySidebar } from '../components/gateway/GatewaySidebar';
import { useStackStore } from '../stores/useStackStore';

const evidence = { basis: 'declared', availability: 'available', freshness: 'unknown' };

const report: SecurityReport = {
  schema_version: 'gridctl.security-report.v1',
  generated_at: '2026-09-13T12:00:00Z',
  source: { kind: 'gateway', display: 'localhost:8180' },
  coverage: {
    status: 'partial',
    predicates_total: 3,
    predicates_evaluated: 3,
    predicates_unknown: 1,
    included_scopes: [],
    excluded_scopes: [],
    unknown_gaps: 1,
  },
  checks: [
    {
      id: 'pin.schema.continuity.fetch',
      predicate: 'pin.schema.continuity',
      subject: { kind: 'server', name: 'fetch' },
      outcome: 'fail',
      reason_code: 'pin_status_drift',
      explanation: 'Stored pin status is drift for fetch.',
      evidence,
    },
    {
      id: 'source.declared.fetch',
      predicate: 'source.declared',
      subject: { kind: 'server', name: 'fetch' },
      outcome: 'unknown',
      reason_code: 'source_undeclared',
      explanation: 'Authored source identity for fetch is unavailable.',
      evidence,
    },
  ],
  limitations: [],
  fail_count: 1,
  warn_count: 0,
  unknown_count: 1,
  not_applicable_count: 0,
  pass_count: 0,
};

const FULL_CONTAINER_ID = '4f1c2b3a9d8e7f6a5b4c3d2e1f0a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1';
const LONG_ENDPOINT = 'http://fetch.internal.example.com:9000/mcp/with/a/very/long/path/that/would/not/fit';
const LONG_IMAGE = 'ghcr.io/example/organization/fetch-mcp-server:2026.11.26-a1b2c3d4e5f6';

function serverNode(name: string, overrides: Record<string, unknown> = {}): Node {
  return {
    id: `mcp-${name}`,
    position: { x: 0, y: 0 },
    data: {
      type: 'mcp-server',
      name,
      transport: 'stdio',
      initialized: true,
      toolCount: 0,
      tools: [],
      status: 'error',
      enabled: true,
      ...overrides,
    },
  };
}

const unhealthy = {
  healthy: false,
  lastCheck: new Date().toISOString(),
  healthError: 'connection refused by fetch',
  endpoint: LONG_ENDPOINT,
  containerId: FULL_CONTAINER_ID,
  image: LONG_IMAGE,
  kind: 'Python container',
  protocolVersion: '2025-06-18',
  replicas: [
    { replicaId: 0, state: 'unhealthy', healthy: false, inFlight: 0, restartAttempts: 2, exit: { code: 3, oomKilled: false, status: 'exited' } },
  ],
  stderrTail: ['panic: fetch failed to bind'],
};

const fetchStatus = {
  name: 'fetch',
  transport: 'stdio',
  initialized: true,
  toolCount: 0,
  tools: [],
  replicas: [
    { replicaId: 0, healthy: false, state: 'unhealthy', inFlight: 0, execution: { mode: 'hardened', eligible: false, outcome: 'mismatch' } },
  ],
} as unknown as MCPServerStatus;

const resourceNode: Node = {
  id: 'resource-db',
  position: { x: 0, y: 0 },
  data: { type: 'resource', name: 'db', image: LONG_IMAGE, status: 'running' },
};

const clientNode: Node = {
  id: 'client-cursor',
  position: { x: 0, y: 0 },
  data: { type: 'client', name: 'Cursor', slug: 'cursor', transport: 'http', configPath: '/home/user/.cursor/mcp.json', status: 'running' },
};

function show(selectedNodeId: string, nodes: Node[], mcpServers: MCPServerStatus[] = [fetchStatus]) {
  useStackStore.setState({ selectedNodeId, nodes, mcpServers, sessions: 0 });
  return render(
    <MemoryRouter>
      <Sidebar />
    </MemoryRouter>,
  );
}

function select(id: string) {
  act(() => useStackStore.setState({ selectedNodeId: id }));
}

function statusPanel(): HTMLElement {
  const toggle = screen.getByRole('button', { name: /^Status/ });
  return document.getElementById(toggle.getAttribute('aria-controls')!)!;
}

function precedes(a: Element, b: Element) {
  return Boolean(a.compareDocumentPosition(b) & globalThis.Node.DOCUMENT_POSITION_FOLLOWING);
}

const securityHeader = () => screen.getByRole('button', { name: /^Security evidence/ });
const executionHeader = () => screen.getByRole('button', { name: /^Execution evidence/ });

describe('Stack inspector status and evidence', () => {
  beforeEach(() => {
    fetchSecurityReport.mockReset();
    fetchSecurityReport.mockResolvedValue(report);
  });

  afterEach(() => {
    Object.assign(navigator, { clipboard: undefined });
  });

  it('puts health, last check, the error, and crash details before Transport', async () => {
    show('mcp-fetch', [serverNode('fetch', unhealthy)]);
    const panel = statusPanel();
    const state = within(panel).getByText('State');
    const health = within(panel).getByText('Health');
    const lastCheck = within(panel).getByText('Last Check');
    const error = within(panel).getByText('connection refused by fetch');
    const crash = within(panel).getByText('Replica 0');
    const stderr = within(panel).getByText('panic: fetch failed to bind');
    const transport = within(panel).getByText('Transport');
    const protocol = within(panel).getByText('Protocol');
    const kind = within(panel).getByText('Kind');
    const endpoint = within(panel).getByText('Endpoint');
    const container = within(panel).getByText('Container');

    const order = [state, health, lastCheck, error, crash, stderr, transport, protocol, kind, endpoint, container];
    for (let i = 1; i < order.length; i++) {
      expect(precedes(order[i - 1], order[i])).toBe(true);
    }
    expect(within(panel).getByText('Unhealthy')).toBeInTheDocument();
    await screen.findByText('1 failing, 1 unknown');
  });

  it('keeps both evidence blocks out of the Status section', async () => {
    show('mcp-fetch', [serverNode('fetch', unhealthy)]);
    await screen.findByText('1 failing, 1 unknown');
    const panel = statusPanel();
    expect(within(panel).queryByText(/Security evidence|Execution evidence/)).not.toBeInTheDocument();
    expect(within(panel).queryByText(/passive report|Refresh report|active replicas/)).not.toBeInTheDocument();
    expect(within(panel).queryByRole('button', { name: /Refresh report/ })).not.toBeInTheDocument();
  });

  it('shows crash details before any health check has reported', () => {
    show('mcp-fetch', [serverNode('fetch', { ...unhealthy, healthy: undefined, healthError: undefined })]);
    const panel = statusPanel();
    expect(within(panel).queryByText('Health')).not.toBeInTheDocument();
    expect(within(panel).getByText('panic: fetch failed to bind')).toBeInTheDocument();
    expect(precedes(within(panel).getByText('Replica 0'), within(panel).getByText('Transport'))).toBe(true);
  });

  it('places the evidence sections after Availability and before MCP catalog, collapsed', async () => {
    show('mcp-fetch', [serverNode('fetch', unhealthy)]);
    await screen.findByText('1 failing, 1 unknown');
    const availability = screen.getByRole('button', { name: /^Availability/ });
    const catalog = screen.getByRole('button', { name: /^MCP catalog/ });
    expect(precedes(availability, securityHeader())).toBe(true);
    expect(precedes(securityHeader(), executionHeader())).toBe(true);
    expect(precedes(executionHeader(), catalog)).toBe(true);
    expect(securityHeader()).toHaveAttribute('aria-expanded', 'false');
    expect(executionHeader()).toHaveAttribute('aria-expanded', 'false');
    expect(within(executionHeader()).getByText('0 of 1 replicas eligible')).toBeInTheDocument();
  });

  it('starts both sections collapsed on the next server after they were opened', async () => {
    const other = { ...fetchStatus, name: 'other' } as MCPServerStatus;
    show('mcp-fetch', [serverNode('fetch', unhealthy), serverNode('other')], [fetchStatus, other]);
    await screen.findByText('1 failing, 1 unknown');
    expect(securityHeader()).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(securityHeader());
    fireEvent.click(executionHeader());
    expect(securityHeader()).toHaveAttribute('aria-expanded', 'true');
    expect(executionHeader()).toHaveAttribute('aria-expanded', 'true');
    expect(await screen.findByText(/fail: pin.schema.continuity/)).toBeInTheDocument();

    select('mcp-other');
    expect(screen.getByRole('heading', { name: 'other' })).toBeInTheDocument();
    expect(securityHeader()).toHaveAttribute('aria-expanded', 'false');
    expect(executionHeader()).toHaveAttribute('aria-expanded', 'false');
    await screen.findByText('No checks for this server');
  });

  it('reads the security report once per server selection', async () => {
    show('mcp-fetch', [serverNode('fetch', unhealthy), serverNode('other')]);
    await screen.findByText('1 failing, 1 unknown');
    expect(fetchSecurityReport).toHaveBeenCalledTimes(1);

    fireEvent.click(securityHeader());
    await screen.findByText(/fail: pin.schema.continuity/);
    expect(fetchSecurityReport).toHaveBeenCalledTimes(1);

    select('mcp-other');
    await screen.findByText('No checks for this server');
    expect(fetchSecurityReport).toHaveBeenCalledTimes(2);
  });

  it('updates the summary from the body Refresh without a second fetch per mount', async () => {
    show('mcp-fetch', [serverNode('fetch', unhealthy)]);
    await screen.findByText('1 failing, 1 unknown');
    fireEvent.click(securityHeader());
    fetchSecurityReport.mockResolvedValue({ ...report, checks: [report.checks[1]] });
    fireEvent.click(await screen.findByRole('button', { name: 'Refresh report' }));
    await waitFor(() => expect(within(securityHeader()).getByText('1 unknown')).toBeInTheDocument());
    expect(fetchSecurityReport).toHaveBeenCalledTimes(2);
    expect(screen.queryByText(/fail: pin.schema.continuity/)).not.toBeInTheDocument();
  });

  it('summarizes a pending, failed, and empty report in the header', async () => {
    let resolve: (value: SecurityReport) => void = () => {};
    fetchSecurityReport.mockReturnValue(new Promise<SecurityReport>((r) => { resolve = r; }));
    const { unmount } = show('mcp-fetch', [serverNode('fetch', unhealthy)]);
    expect(within(securityHeader()).getByText('Loading report')).toBeInTheDocument();
    await act(async () => resolve({ ...report, checks: [] }));
    expect(within(securityHeader()).getByText('No checks for this server')).toBeInTheDocument();
    unmount();

    fetchSecurityReport.mockRejectedValue(new Error('nope'));
    show('mcp-fetch', [serverNode('fetch', unhealthy)]);
    await waitFor(() => expect(within(securityHeader()).getByText('Report unavailable')).toBeInTheDocument());
    fireEvent.click(securityHeader());
    expect(screen.getByRole('alert')).toHaveTextContent('Security evidence report is unavailable.');
    expect(securityHeader().textContent ?? '').not.toMatch(/secure|safe|all clear/i);
  });

  it('wraps identifiers instead of truncating them', () => {
    show('mcp-fetch', [serverNode('fetch', { ...unhealthy, ssh: true, sshHost: 'bastion.example.com', openapi: true, openapiSpec: 'https://example.com/specs/openapi.json' })]);
    const panel = statusPanel();
    for (const value of [LONG_ENDPOINT, FULL_CONTAINER_ID, LONG_IMAGE, 'bastion.example.com', 'https://example.com/specs/openapi.json', 'Python container']) {
      const el = within(panel).getAllByText(value).find((node) => node.getAttribute('tabindex') === '0')!;
      expect(el).toBeDefined();
      expect(el).not.toHaveAttribute('title');
      expect(el).toHaveClass('break-all', 'min-w-0');
      expect(el.className).not.toMatch(/truncate|max-w-/);
    }
    expect(panel.innerHTML).not.toMatch(/max-w-\[180px\]|max-w-\[200px\]/);
    expect(screen.queryByRole('button', { name: /copy ssh/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /copy spec/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /copy kind/i })).not.toBeInTheDocument();
  });

  it('copies the full endpoint, image, and container ID', async () => {
    vi.useFakeTimers();
    try {
      const writeText = vi.fn().mockResolvedValue(undefined);
      Object.assign(navigator, { clipboard: { writeText } });
      show('mcp-fetch', [serverNode('fetch', unhealthy)]);
      for (const [name, value] of [
        ['Copy endpoint', LONG_ENDPOINT],
        ['Copy image', LONG_IMAGE],
        ['Copy container ID', FULL_CONTAINER_ID],
      ] as const) {
        const button = screen.getByRole('button', { name });
        await act(async () => {
          fireEvent.click(button);
        });
        expect(writeText).toHaveBeenLastCalledWith(value);
        expect(button).toHaveClass('text-status-running');
        act(() => vi.advanceTimersByTime(1500));
        expect(button).not.toHaveClass('text-status-running');
      }
    } finally {
      vi.useRealTimers();
    }
  });

  it('shows Copy failed when the browser refuses a copy', async () => {
    Object.assign(navigator, { clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } });
    Object.defineProperty(document, 'execCommand', { value: () => false, configurable: true, writable: true });
    try {
      show('mcp-fetch', [serverNode('fetch', unhealthy)]);
      const button = screen.getByRole('button', { name: 'Copy endpoint' });
      await act(async () => {
        fireEvent.click(button);
      });
      expect(button).toHaveTextContent('Copy failed');
    } finally {
      Object.defineProperty(document, 'execCommand', { value: undefined, configurable: true, writable: true });
    }
  });

  it.each([['unknown'], ['']])('omits the Container row for %j', (containerId) => {
    show('mcp-fetch', [serverNode('fetch', { ...unhealthy, containerId })]);
    expect(within(statusPanel()).queryByText('Container')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Copy container ID' })).not.toBeInTheDocument();
  });

  it('shows an unknown endpoint without a copy button', () => {
    show('mcp-fetch', [serverNode('fetch', { ...unhealthy, endpoint: 'unknown' })]);
    expect(within(statusPanel()).getByText('unknown')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Copy endpoint' })).not.toBeInTheDocument();
  });

  it('gives a resource a copyable image and an Execution evidence section after Actions', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    show('resource-db', [resourceNode], []);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Copy image' }));
    });
    expect(writeText).toHaveBeenCalledWith(LONG_IMAGE);
    expect(screen.queryByRole('button', { name: /^Security evidence/ })).not.toBeInTheDocument();
    expect(precedes(screen.getByRole('button', { name: /^Actions/ }), executionHeader())).toBe(true);
    expect(within(executionHeader()).getByText('Not covered for resources')).toBeInTheDocument();
    expect(executionHeader()).toHaveAttribute('aria-expanded', 'false');
    expect(fetchSecurityReport).not.toHaveBeenCalled();
  });

  it('gives a client a copyable config path and no evidence sections', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    show('client-cursor', [clientNode], []);
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: 'Copy config path' }));
    });
    expect(writeText).toHaveBeenCalledWith('/home/user/.cursor/mcp.json');
    expect(screen.queryByRole('button', { name: /evidence/i })).not.toBeInTheDocument();
  });

  it('anchors the accent line to the inspector root', () => {
    const { container } = show('mcp-fetch', [serverNode('fetch', unhealthy)]);
    const root = container.firstElementChild!;
    expect(root).toHaveClass('relative');
    expect(root.firstElementChild).toHaveClass('absolute', 'left-0');
  });

  it('anchors the gateway accent line to the gateway inspector root', () => {
    useStackStore.setState({ selectedNodeId: null, mcpServers: [] });
    const { container } = render(
      <MemoryRouter>
        <GatewaySidebar onClose={() => {}} />
      </MemoryRouter>,
    );
    const root = container.firstElementChild!;
    expect(root).toHaveClass('relative');
    expect(root.firstElementChild).toHaveClass('absolute', 'left-0');
  });
});
