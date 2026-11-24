import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, waitFor, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { StatusBar } from '../components/layout/StatusBar';
import { useStackStore } from '../stores/useStackStore';
import { useSpecStore } from '../stores/useSpecStore';
import { usePinsStore } from '../stores/usePinsStore';
import { fetchLimits, type ServerPins } from '../lib/api';
import { formatCompactNumber } from '../lib/format';
import { formatRelativeTime } from '../lib/time';
import type { MCPServerStatus, SpecHealth } from '../types';

// Pass-through spies: StatusBar calls formatCompactNumber once per render of
// the bar (with a token count), and the freshness leaf calls
// formatRelativeTime once per render of itself.
vi.mock('../lib/format', async (importActual) => {
  const actual = await importActual<typeof import('../lib/format')>();
  return { ...actual, formatCompactNumber: vi.fn(actual.formatCompactNumber) };
});
vi.mock('../lib/time', async (importActual) => {
  const actual = await importActual<typeof import('../lib/time')>();
  return { ...actual, formatRelativeTime: vi.fn(actual.formatRelativeTime) };
});

// StatusBar mounts LimitsBadge, whose hook polls GET /api/limits; keep the
// test hermetic instead of letting a real fetch fail in jsdom.
vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchLimits: vi.fn().mockResolvedValue({ configured: false, entries: [] }),
  };
});

function server(name: string, healthy: boolean): MCPServerStatus {
  return { name, transport: 'http', initialized: true, healthy, toolCount: 1, tools: ['t'] };
}

function specHealth(override?: string): SpecHealth {
  return {
    validation: { status: 'warnings', errorCount: 0, warningCount: 2 },
    drift: { status: 'in-sync' },
    dependencies: { status: 'resolved' },
    ...(override ? { override } : {}),
  };
}

function serverPins(status: ServerPins['status'], withFinding = false): ServerPins {
  return {
    server_hash: 'h',
    pinned_at: '2026-11-01T00:00:00Z',
    last_verified_at: '2026-11-01T00:00:00Z',
    tool_count: 1,
    status,
    tools: {
      t: {
        hash: 'h',
        name: 't',
        pinned_at: '2026-11-01T00:00:00Z',
        ...(withFinding
          ? { findings: [{ code: 'c', severity: 'warn' as const, confidence: 'high' as const, field: 'description', message: 'm' }] }
          : {}),
      },
    },
  };
}

// The bar's direct children, after the absolute accent line: connection
// pill, divider, alert group, counter group.
function groups(container: HTMLElement) {
  const bar = container.firstElementChild as HTMLElement;
  return {
    bar,
    alerts: bar.children[3] as HTMLElement,
    counters: bar.children[4] as HTMLElement,
  };
}

function renderBar() {
  return render(
    <MemoryRouter>
      <StatusBar />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  cleanup();
  useStackStore.setState({
    mcpServers: [],
    resources: [],
    sessions: 0,
    codeMode: 'off',
    toolDiscovery: null,
    tokenUsage: null,
    featureDetails: [],
    connectionStatus: 'connected',
    lastUpdated: null,
    error: null,
    gatewayInfo: null,
  });
  useSpecStore.setState({ health: null });
  usePinsStore.setState({ pins: null, skillPins: null, contextPins: null });
  vi.mocked(fetchLimits).mockResolvedValue({ configured: false, entries: [] });
});

describe('StatusBar', () => {
  it('truncates a long disconnect error and stays on one row', () => {
    const error = 'x'.repeat(200);
    useStackStore.setState({ connectionStatus: 'disconnected', error });
    renderBar();
    const label = screen.getByText(error);
    expect(label).toHaveClass('truncate');
    expect(label).toHaveAttribute('title', error);
    expect(label.closest('.h-8')).toHaveClass('whitespace-nowrap');
  });

  it('hides tokenizer, savings, and sessions at the narrow-bar breakpoint', () => {
    useStackStore.setState({
      sessions: 3,
      sessionEntries: null,
      gatewayInfo: { name: 'acme', version: 'v1', tokenizer: 'embedded' },
      tokenUsage: {
        session: { input_tokens: 10, output_tokens: 0, total_tokens: 10 },
        per_server: {},
        format_savings: { original_tokens: 100, formatted_tokens: 70, saved_tokens: 30, savings_percent: 30 },
      },
    });
    renderBar();
    expect(screen.getByText('sessions').parentElement?.parentElement).toHaveClass('max-[800px]:hidden');
    expect(screen.getByText('embedded').parentElement).toHaveClass('max-[800px]:hidden');
    expect(screen.getByText('30% saved').parentElement).toHaveClass('max-[800px]:hidden');
  });

  it('owns the connection indicator', () => {
    renderBar();
    expect(screen.getByText('Connected')).toBeInTheDocument();
  });

  it('owns the server-count and unhealthy indicators', () => {
    useStackStore.setState({
      mcpServers: [server('s1', true), server('s2', false), server('s3', false)],
    });
    renderBar();
    expect(screen.getByText('MCP servers')).toBeInTheDocument();
    // Two unhealthy servers surface here, where the header used to mirror them.
    expect(screen.getByText('err')).toBeInTheDocument();
  });

  it('omits the unhealthy indicator when all servers are healthy', () => {
    useStackStore.setState({ mcpServers: [server('s1', true), server('s2', true)] });
    renderBar();
    expect(screen.getByText('MCP servers')).toBeInTheDocument();
    expect(screen.queryByText('err')).not.toBeInTheDocument();
  });

  it('renders the deferred discovery pill from the store and hides it when null', () => {
    useStackStore.setState({ toolDiscovery: 'deferred' });
    renderBar();
    expect(screen.getByText('Deferred discovery')).toBeInTheDocument();
    cleanup();
    useStackStore.setState({ toolDiscovery: null });
    renderBar();
    expect(screen.queryByText('Deferred discovery')).not.toBeInTheDocument();
  });

  it('shows an aggregate experimental chip when flags are enabled', () => {
    useStackStore.setState({
      featureDetails: [
        { name: 'test_flag', stage: 'experimental', description: 'd' },
        { name: 'other_flag', stage: 'experimental', description: 'd' },
      ],
    });
    renderBar();
    const chip = screen.getByRole('button', { name: /2 experimental flags enabled/i });
    expect(chip).toBeInTheDocument();
    expect(chip).toHaveTextContent('experimental');
  });

  it('hides the experimental chip when no flags are enabled', () => {
    renderBar();
    expect(screen.queryByText('experimental')).not.toBeInTheDocument();
  });

  it('lets the bar shrink below its content width', () => {
    const { container } = renderBar();
    const { bar } = groups(container);
    expect(bar).toHaveClass('min-w-0', 'whitespace-nowrap', 'gap-5');
    expect(bar).not.toHaveClass('justify-between');
  });

  it('places alert badges before the counters', () => {
    useStackStore.setState({ mcpServers: [server('s1', true)] });
    useSpecStore.setState({ health: specHealth() });
    usePinsStore.setState({ pins: { s1: serverPins('drift') } });
    renderBar();
    const spec = screen.getByRole('button', { name: 'Spec: 2 warnings' });
    const pins = screen.getByRole('button', { name: 'Pins: 1 drifted' });
    const servers = screen.getByText('MCP servers');
    expect(spec.compareDocumentPosition(servers) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(pins.compareDocumentPosition(servers) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('shrinks the alert group and wraps and clips the counter group', () => {
    useSpecStore.setState({ health: specHealth() });
    const { container } = renderBar();
    const { alerts, counters } = groups(container);
    expect(alerts).toHaveClass('min-w-0', 'empty:hidden');
    expect(alerts).not.toHaveClass('shrink-0');
    expect(alerts).toContainElement(screen.getByRole('button', { name: 'Spec: 2 warnings' }));
    expect(counters).toHaveClass('min-w-0', 'flex-1', 'flex-wrap', 'content-start', 'h-full', 'overflow-clip');
    expect(counters).not.toHaveClass('overflow-hidden');
  });

  it('keeps every counter one row tall so a wrapped item sits below the clip', () => {
    useStackStore.setState({
      mcpServers: [server('s1', true)],
      sessions: 2,
      codeMode: 'on',
      featureDetails: [{ name: 'f', stage: 'experimental', description: 'd' }],
      lastUpdated: new Date(),
    });
    const { container } = renderBar();
    const { counters } = groups(container);
    for (const item of Array.from(counters.children)) {
      expect(item).toHaveClass('h-full');
    }
    // The zero-width lead item keeps a too-wide first counter from being cut.
    expect(counters.firstElementChild).toHaveAttribute('aria-hidden', 'true');
    expect(counters.firstElementChild).toHaveClass('w-0', '-mr-5', 'shrink-0');
  });

  it('renders Updated as the last counter, pushed right', () => {
    useStackStore.setState({ mcpServers: [server('s1', true)], lastUpdated: new Date() });
    const { container } = renderBar();
    const { counters } = groups(container);
    const updated = screen.getByText(/^Updated /).parentElement;
    expect(counters.lastElementChild).toBe(updated);
    expect(updated).toHaveClass('ml-auto');
  });

  it('works without an Updated item', () => {
    useStackStore.setState({ mcpServers: [server('s1', true)], lastUpdated: null });
    const { container } = renderBar();
    const { counters } = groups(container);
    expect(screen.queryByText(/^Updated /)).not.toBeInTheDocument();
    expect(counters.lastElementChild).toHaveTextContent('MCP servers');
  });

  it('leaves the alert group empty when every badge is idle', async () => {
    useStackStore.setState({ mcpServers: [server('s1', true)], lastUpdated: new Date() });
    usePinsStore.setState({ pins: {}, skillPins: {}, contextPins: {} });
    const { container } = renderBar();
    await waitFor(() => expect(fetchLimits).toHaveBeenCalled());
    const { alerts } = groups(container);
    expect(alerts.childNodes).toHaveLength(0);
    // The trailing divider is the group's ::after, so it hides with the group.
    expect(alerts.className).toContain("after:content-['']");
    expect(alerts.className).toContain('after:w-px');
    const dividers = Array.from(container.firstElementChild!.children).filter((el) => el.classList.contains('w-px'));
    expect(dividers).toHaveLength(1);
  });

  it('makes every alert badge label truncatable with its full text kept', async () => {
    vi.mocked(fetchLimits).mockResolvedValue({
      configured: true,
      entries: [{ kind: 'rate', scope: 'server', key: 's1', state: 'exceeded' }],
    });
    useStackStore.setState({
      mcpServers: [server('s1', true), { ...server('s2', true), authStatus: 'needs_auth' }],
    });
    useSpecStore.setState({ health: specHealth('/stacks/stack.override.yaml') });
    usePinsStore.setState({ pins: { s1: serverPins('drift'), s2: serverPins('pinned', true) } });
    const { container } = renderBar();
    await screen.findByText('1 rate limit exceeded');
    const { alerts } = groups(container);
    const labels = [
      'Spec: 2 warnings',
      'Pins: 1 drifted',
      'Findings: 1 server',
      'Auth: 1 pending',
      '1 rate limit exceeded',
    ];
    expect(alerts.children).toHaveLength(labels.length);
    for (const text of labels) {
      const label = screen.getByText(text);
      expect(label).toHaveClass('truncate');
      expect(label.parentElement).toHaveClass('min-w-0');
      const button = label.closest('button')!;
      expect(button).toHaveClass('min-w-0');
      expect(button.querySelector('svg')).toHaveClass('shrink-0');
      expect(button.querySelector('span.rounded-full')).toHaveClass('shrink-0');
      expect(button).toHaveAttribute('title', text === 'Findings: 1 server'
        ? 'Poisoning-scan findings on pinned tools or skills; review in the Pins workspace'
        : text);
    }
    // Accessible names are unchanged by truncation.
    expect(screen.getByRole('button', { name: /Authorization: 1 pending\. Go to s2/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '1 rate limit exceeded. Open Metrics workspace' })).toBeInTheDocument();
    const override = screen.getByText('override: /stacks/stack.override.yaml');
    expect(override).toHaveClass('truncate', 'min-w-0', 'max-w-[32ch]');
    expect(override).toHaveAttribute('title', '/stacks/stack.override.yaml');
  });

  it('keeps the connection pill from shrinking', () => {
    renderBar();
    expect(screen.getByText('Connected').parentElement).toHaveClass('shrink-0');
  });
});

const NOW = new Date('2026-11-24T12:00:00Z').getTime();

async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms);
  });
}

describe('StatusBar freshness', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('ticks the last-updated age during an outage with no parent render or store write', async () => {
    useStackStore.setState({
      lastUpdated: new Date(NOW),
      connectionStatus: 'disconnected',
      error: 'Gateway connection unavailable',
    });
    renderBar();
    expect(screen.getByText('Last updated just now')).toBeInTheDocument();
    await advance(30_000);
    expect(screen.getByText('Last updated 30s ago')).toBeInTheDocument();
    await advance(30_000);
    expect(screen.getByText('Last updated 1m ago')).toBeInTheDocument();
  });

  it('marks the outage age in the warning tone and the connected age in the muted tone', () => {
    useStackStore.setState({ lastUpdated: new Date(NOW), connectionStatus: 'error', error: 'boom' });
    renderBar();
    const stale = screen.getByText('Last updated just now');
    expect(stale).toHaveClass('text-status-pending');
    expect(stale.parentElement!.querySelector('svg')).toHaveClass('text-status-pending');
    expect(stale.parentElement!.querySelector('svg')).toHaveAttribute('aria-hidden', 'true');
    cleanup();

    useStackStore.setState({ connectionStatus: 'connected', error: null });
    renderBar();
    const fresh = screen.getByText('Updated just now');
    expect(fresh).not.toHaveClass('text-status-pending');
    expect(fresh.parentElement).toHaveClass('text-text-muted');
    expect(fresh.parentElement!.querySelector('svg')).not.toHaveClass('text-status-pending');
    expect(screen.queryByText(/Last updated/)).not.toBeInTheDocument();
  });

  it('ages the label while connected when no new poll lands', async () => {
    useStackStore.setState({ lastUpdated: new Date(NOW), connectionStatus: 'connected' });
    renderBar();
    await advance(45_000);
    expect(screen.getByText('Updated 45s ago')).toBeInTheDocument();
  });

  it('renders no freshness item before the first successful update', async () => {
    useStackStore.setState({ lastUpdated: null, connectionStatus: 'disconnected', error: 'x' });
    renderBar();
    await advance(5_000);
    expect(screen.queryByText(/updated/i)).not.toBeInTheDocument();
  });

  it('re-renders only the freshness leaf on each tick', async () => {
    useStackStore.setState({
      lastUpdated: new Date(NOW),
      connectionStatus: 'connected',
      tokenUsage: {
        session: { input_tokens: 10, output_tokens: 0, total_tokens: 10 },
        per_server: {},
        format_savings: { original_tokens: 0, formatted_tokens: 0, saved_tokens: 0, savings_percent: 0 },
      },
    });
    renderBar();
    // Let the mount-time work (LimitsBadge's first poll) settle.
    await advance(0);
    vi.mocked(formatCompactNumber).mockClear();
    vi.mocked(formatRelativeTime).mockClear();

    await advance(1000);
    expect(formatRelativeTime).toHaveBeenCalledTimes(1);
    await advance(1000);
    expect(formatRelativeTime).toHaveBeenCalledTimes(2);
    expect(formatCompactNumber).not.toHaveBeenCalled();
  });
});

describe('StatusBar connection state', () => {
  it('defaults the store to connecting so first paint is not Disconnected', () => {
    expect(useStackStore.getInitialState().connectionStatus).toBe('connecting');
  });

  it('shows a neutral Connecting state', () => {
    useStackStore.setState({ connectionStatus: 'connecting', error: null });
    renderBar();
    const label = screen.getByText('Connecting');
    expect(label).toHaveAttribute('title', 'Connecting');
    expect(screen.queryByText('Disconnected')).not.toBeInTheDocument();
    expect(screen.queryByText('Connected')).not.toBeInTheDocument();
    const item = label.closest('[role="status"]')!;
    expect(item).not.toHaveClass('text-status-error');
    expect(item).toHaveClass('text-text-muted');
    expect(item.querySelector('svg')).toHaveClass('animate-spin');
    expect(item.querySelector('.animate-pulse')).toBeNull();
  });

  // A 401 leaves connectionStatus at 'connecting' with any earlier error
  // retained, so this is also what the bar shows under the credential prompt.
  it('stays Connecting behind a credential prompt and ignores a retained error', () => {
    useStackStore.setState({ connectionStatus: 'connecting', error: 'boom' });
    renderBar();
    expect(screen.getByText('Connecting')).toBeInTheDocument();
    expect(screen.queryByText('boom')).not.toBeInTheDocument();
  });

  it('shows the red error state after a failed poll', () => {
    useStackStore.setState({ connectionStatus: 'disconnected', error: null });
    renderBar();
    const item = screen.getByText('Disconnected').closest('[role="status"]')!;
    expect(item).toHaveClass('text-status-error');
    cleanup();
    useStackStore.setState({ connectionStatus: 'error', error: 'Gateway connection unavailable' });
    renderBar();
    expect(screen.getByText('Gateway connection unavailable').closest('[role="status"]')).toHaveClass('text-status-error');
  });

  it('is a labelled footer landmark whose status region holds only the state label', () => {
    useStackStore.setState({ connectionStatus: 'connected', lastUpdated: new Date() });
    renderBar();
    const bar = screen.getByRole('contentinfo', { name: 'Status bar' });
    expect(bar.tagName).toBe('FOOTER');
    const item = screen.getByText('Connected').closest('[role="status"]')!;
    expect(item).not.toBeNull();
    expect(item.textContent).not.toContain('pdated');
    expect(item.textContent).toBe('Connected');
    for (const svg of Array.from(item.querySelectorAll('svg'))) {
      expect(svg).toHaveAttribute('aria-hidden', 'true');
    }
    expect(item.querySelector('.animate-pulse')).toHaveAttribute('aria-hidden', 'true');
    const updated = screen.getByText(/^Updated /);
    expect(updated.closest('[role="status"], [aria-live]')).toBeNull();
  });
});
