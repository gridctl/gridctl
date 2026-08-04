import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import ConnectionsWorkspace from '../components/workspaces/ConnectionsWorkspace';
import {
  clientHealth,
  sortClients,
  unjoinedAgentSlugs,
} from '../components/connections/connectionsModel';
import { useStackStore } from '../stores/useStackStore';
import { useContextStore } from '../stores/useContextStore';
import { useRegistryStore } from '../stores/useRegistryStore';
import type { ClientStatus, WiringRow } from '../types';
import type { ContextDoc } from '../lib/api';

vi.mock('../components/ui/Toast', async () => {
  const actual = await vi.importActual<typeof import('../components/ui/Toast')>('../components/ui/Toast');
  return { ...actual, showToast: vi.fn() };
});

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchClients: vi.fn(),
    fetchSessions: vi.fn(),
    fetchWiringStatus: vi.fn(),
    adoptWiringEntry: vi.fn(),
    fetchAgentProjectionStatus: vi.fn(),
    fetchGlobalContext: vi.fn(),
    linkClient: vi.fn(),
    unlinkClient: vi.fn(),
    previewClientLink: vi.fn(),
    syncGlobalContext: vi.fn(),
    unsyncGlobalContext: vi.fn(),
  };
});

import { showToast } from '../components/ui/Toast';
import {
  adoptWiringEntry,
  fetchClients,
  fetchAgentProjectionStatus,
  fetchGlobalContext,
  fetchSessions,
  fetchWiringStatus,
} from '../lib/api';

function client(overrides: Partial<ClientStatus> & { slug: string; name: string }): ClientStatus {
  return {
    detected: true,
    linked: false,
    transport: 'http',
    ...overrides,
  } as ClientStatus;
}

const claude = client({ slug: 'claude', name: 'Claude Desktop', linked: true, configPath: '/home/u/.claude.json' });
const cursor = client({ slug: 'cursor', name: 'Cursor' });
const ghost = client({ slug: 'zed', name: 'Zed', detected: false });

const foreignRow: WiringRow = {
  client: 'claude',
  name: 'gridctl',
  channel: 'config-entry',
  target: '/home/u/.claude.json',
  state: 'foreign',
  detail: 'entry was not recorded by gridctl',
  remediation: 'adopt to take ownership',
};

const emptyContextDoc: ContextDoc = {
  canonical: { path: '/home/u/.gridctl/context/AGENTS.md', exists: false, content: '' },
  needs_sync: false,
  clients: [],
};

function renderHub(initialEntry = '/connections') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <ConnectionsWorkspace />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  useStackStore.setState({ clients: [claude, cursor, ghost], sessionEntries: null });
  useContextStore.setState({ doc: null, loading: false, error: null });
  useRegistryStore.setState({ agentStatuses: null });
  vi.mocked(fetchClients).mockResolvedValue([claude, cursor, ghost]);
  vi.mocked(fetchWiringStatus).mockResolvedValue([foreignRow]);
  vi.mocked(fetchAgentProjectionStatus).mockResolvedValue([]);
  vi.mocked(fetchGlobalContext).mockResolvedValue(emptyContextDoc);
  vi.mocked(fetchSessions).mockResolvedValue({ count: 0, sessions: [], entries: [] });
});

describe('clientHealth', () => {
  it('joins wiring, context, and agent drift per slug', () => {
    const health = clientHealth(
      'claude',
      [foreignRow],
      [
        {
          slug: 'claude',
          name: 'Claude Desktop',
          supported: true,
          available: true,
          state: 'drifted',
        },
      ],
      [
        {
          agent: 'reviewer',
          client: 'claude',
          channel: 'copy',
          target: '/x',
          render: 'identity',
          state: 'stale',
        },
      ],
    );
    expect(health.attention).toBe(true);
    expect(health.reasons).toEqual([
      'wiring foreign',
      'context drifted',
      '1 agent projection stale',
    ]);
  });

  it('treats missing wiring as an opportunity, not attention', () => {
    const health = clientHealth(
      'cursor',
      [{ ...foreignRow, client: 'cursor', state: 'missing' }],
      null,
      null,
    );
    expect(health.attention).toBe(false);
  });

  it('surfaces agent slugs that join no client instead of dropping them', () => {
    const slugs = unjoinedAgentSlugs(
      [
        { agent: 'a', client: 'copilot', channel: 'copy', target: '/x', render: 'lossy', state: 'in-sync' },
        { agent: 'a', client: 'claude', channel: 'copy', target: '/y', render: 'identity', state: 'in-sync' },
      ],
      new Set(['claude']),
    );
    // copilot is the documented non-provisioner agent target; it must
    // surface here rather than vanish in the join.
    expect(slugs).toEqual(['copilot']);
  });
});

describe('sortClients', () => {
  it('orders attention, connected, detected, rest', () => {
    const order = sortClients([ghost, cursor, claude], (slug) =>
      slug === 'cursor' ? { attention: true, reasons: ['wiring drifted'] } : { attention: false, reasons: [] },
    ).map((c) => c.slug);
    expect(order).toEqual(['cursor', 'claude', 'zed']);
  });
});

describe('ConnectionsWorkspace hub', () => {
  it('renders the rail and a detail pane with the two axes separately labeled', async () => {
    renderHub();
    // Default selection prefers the attention client (claude has a
    // foreign wiring row).
    await waitFor(() => {
      expect(screen.getByText('Ownership')).toBeInTheDocument();
    });
    expect(screen.getByText('Activity')).toBeInTheDocument();
    // The wiring section auto-expands on attention and speaks the state
    // vocabulary verbatim.
    expect(await screen.findByText('foreign')).toBeInTheDocument();
    expect(screen.getByText('entry was not recorded by gridctl')).toBeInTheDocument();
  });

  it('adopts a foreign wiring entry through the confirm dialog', async () => {
    vi.mocked(adoptWiringEntry).mockResolvedValue({
      client: 'claude',
      name: 'gridctl',
      action: 'adopted',
    });
    renderHub();
    const adopt = await screen.findByRole('button', { name: 'Adopt' });
    fireEvent.click(adopt);
    const dialog = await screen.findByRole('alertdialog');
    expect(within(dialog).getByText(/Record ownership/)).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Adopt' }));
    await waitFor(() => {
      expect(adoptWiringEntry).toHaveBeenCalledWith('claude', 'gridctl');
    });
  });

  it('shows the explicit non-target copy for clients outside the agent projection table', async () => {
    renderHub('/connections?client=claude');
    // The section is collapsed when quiet; expand it. claude (the
    // desktop slug) is not an agentsync target; the section must say so
    // instead of rendering blank space.
    fireEvent.click(await screen.findByRole('button', { name: /^Agents/ }));
    expect(await screen.findByTestId('not-agent-target')).toHaveTextContent(
      /Not an agent projection target/,
    );
  });

  it('never renders "0 sessions": the sessionless copy explains the absence', async () => {
    renderHub('/connections?client=claude');
    fireEvent.click(await screen.findByRole('button', { name: /^Sessions/ }));
    expect(await screen.findByTestId('sessionless-copy')).toHaveTextContent(/sessionless by design/);
  });

  it('buckets unattributable sessions honestly instead of guessing', async () => {
    vi.mocked(fetchSessions).mockResolvedValue({
      count: 2,
      sessions: ['s-1', 's-2'],
      entries: [
        { id: 's-1', generation: 'handshake', protocolVersion: '2025-11-25', accessId: 'claude' },
        { id: 's-2', generation: 'handshake', clientName: 'mystery-client' },
      ],
    });
    renderHub('/connections?client=claude');
    expect(
      await screen.findByText('1 session not matched to a linked client'),
    ).toBeInTheDocument();
    // The attributed one lands on claude's detail pane.
    expect(await screen.findByText(/1 active session/)).toBeInTheDocument();
  });

  it('spotlights the first detected-unlinked client from the wizard route', async () => {
    renderHub('/connections?spotlight=unlinked');
    // cursor is detected and unlinked; the detail pane should land on it.
    await waitFor(() => {
      const pane = screen.getAllByText('Cursor');
      expect(pane.length).toBeGreaterThan(0);
    });
    await waitFor(() => {
      expect(screen.getByRole('heading', { name: 'Cursor' })).toBeInTheDocument();
    });
  });

  it('toasts and clears an unknown ?client deep link once loaded', async () => {
    renderHub('/connections?client=nope');
    await waitFor(() => {
      expect(showToast).toHaveBeenCalledWith('error', 'Client "nope" not found');
    });
    // Selection falls back to the default (the attention client).
    expect(await screen.findByRole('heading', { name: 'Claude Desktop' })).toBeInTheDocument();
  });
});
