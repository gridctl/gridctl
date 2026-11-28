import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter } from 'react-router';
import { DetachedMetricsPage } from '../pages/DetachedMetricsPage';
import type { TokenMetricsResponse } from '../types';
import type { SharedStatus } from '../hooks/useSharedStatus';

vi.mock('../hooks/useBroadcastChannel', () => ({
  useDetachedWindowSync: vi.fn(),
  useBroadcastChannel: () => ({ postMessage: vi.fn() }),
}));

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchTokenMetrics: vi.fn(),
    fetchStatus: vi.fn(),
    fetchToolUsage: vi.fn(),
    fetchLimits: vi.fn(),
  };
});

vi.mock('../components/ui/Toast', async (importActual) => {
  const actual = await importActual<typeof import('../components/ui/Toast')>();
  return { ...actual, showToast: vi.fn() };
});

vi.mock('../lib/download', async (importActual) => {
  const actual = await importActual<typeof import('../lib/download')>();
  return { ...actual, downloadTextFile: vi.fn() };
});

import { fetchLimits, fetchStatus, fetchTokenMetrics, fetchToolUsage } from '../lib/api';
import { downloadTextFile } from '../lib/download';
import type { GatewayStatus } from '../types';

const statusOverride: { current: SharedStatus | null } = { current: null };

vi.mock('../hooks/useSharedStatus', async (importActual) => {
  const actual = await importActual<typeof import('../hooks/useSharedStatus')>();
  return {
    ...actual,
    useSharedStatus: (opts: { enabled: boolean }) => {
      const real = actual.useSharedStatus(opts);
      return statusOverride.current ?? real;
    },
  };
});

const seeded: TokenMetricsResponse = {
  range: '30m',
  interval: '1m',
  data_points: [
    { timestamp: '2026-01-01T00:00:00Z', input_tokens: 1000, output_tokens: 400, total_tokens: 1400 },
    { timestamp: '2026-01-01T00:01:00Z', input_tokens: 234, output_tokens: 167, total_tokens: 401 },
  ],
  per_server: {},
};

function renderPage() {
  return render(
    <MemoryRouter>
      <DetachedMetricsPage />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(fetchStatus).mockResolvedValue({ gateway: { name: 'g', version: 'v' }, 'mcp-servers': [] } as never);
  vi.mocked(fetchToolUsage).mockResolvedValue({ servers: {} } as never);
  vi.mocked(fetchLimits).mockResolvedValue({ configured: false } as never);
});

describe('DetachedMetricsPage refresh states', () => {
  it('renders the notifications region', async () => {
    vi.mocked(fetchTokenMetrics).mockImplementation((range = '30m') => Promise.resolve({ ...seeded, range }));
    renderPage();
    expect(screen.getByRole('region', { name: 'Notifications' })).toBeInTheDocument();
    expect(await screen.findByText('1.8k')).toBeInTheDocument();
  });

  it('keeps the dashboard under a banner when a refresh fails', async () => {
    vi.mocked(fetchTokenMetrics).mockImplementation((range = '30m') => Promise.resolve({ ...seeded, range }));
    renderPage();
    expect(await screen.findByText('1.8k')).toBeInTheDocument();

    vi.mocked(fetchTokenMetrics).mockRejectedValue(new Error('Gateway connection unavailable'));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Gateway connection unavailable');
    expect(alert).toHaveTextContent('Last updated');
    expect(screen.getByText('1.8k')).toBeInTheDocument();
    expect(screen.queryByText('Metrics unavailable')).not.toBeInTheDocument();

    vi.mocked(fetchTokenMetrics).mockImplementation((range = '30m') => Promise.resolve({ ...seeded, range }));
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => {
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });
  });

  it('shows the failed state when the first load fails', async () => {
    vi.mocked(fetchTokenMetrics).mockRejectedValue(new Error('Gateway connection unavailable'));
    renderPage();
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Metrics unavailable');
    expect(alert).toHaveTextContent('Gateway connection unavailable');
    expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
  });

  it('reads Metrics in the header and renders one notifications region and a title', async () => {
    document.title = '';
    vi.mocked(fetchTokenMetrics).mockImplementation((range = '30m') => Promise.resolve({ ...seeded, range }));
    renderPage();
    expect(screen.getByRole('banner')).toHaveTextContent('Metrics');
    expect(screen.queryByText('Token Metrics')).not.toBeInTheDocument();
    expect(screen.getAllByRole('region', { name: 'Notifications' })).toHaveLength(1);
    expect(screen.getAllByRole('main')).toHaveLength(1);
    expect(document.title).toBe('Gridctl - Metrics');
    expect(screen.getByRole('button', { name: 'Fullscreen' })).toBeInTheDocument();
    expect(await screen.findByText('1.8k')).toBeInTheDocument();
  });
});

describe('DetachedMetricsPage disconnected banner', () => {
  afterEach(() => {
    statusOverride.current = null;
  });

  it('shows the banner when status fails and metrics are loaded', async () => {
    statusOverride.current = { status: null, source: null, settled: true, error: 'Status failed: down.', refresh: vi.fn() };
    vi.mocked(fetchTokenMetrics).mockImplementation((range = '30m') => Promise.resolve({ ...seeded, range }));
    renderPage();
    await waitFor(() => expect(fetchTokenMetrics).toHaveBeenCalled());
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Status failed: down.');
  });

  it('shows only the metrics error block when the metrics load also fails', async () => {
    statusOverride.current = { status: null, source: null, settled: true, error: 'Status failed: down.', refresh: vi.fn() };
    vi.mocked(fetchTokenMetrics).mockRejectedValue(new Error('Gateway connection unavailable'));
    renderPage();
    await screen.findByText('Metrics unavailable');
    expect(screen.getAllByRole('alert')).toHaveLength(1);
    expect(screen.queryByText('Status failed: down.')).not.toBeInTheDocument();
  });
});

describe('DetachedMetricsPage breakdown columns and export', () => {
  const series: TokenMetricsResponse = {
    ...seeded,
    per_server: {
      github: [
        { timestamp: '2026-01-01T00:00:00Z', input_tokens: 5, output_tokens: 1, total_tokens: 6 },
        { timestamp: '2026-01-01T00:01:00Z', input_tokens: 3, output_tokens: 2, total_tokens: 5 },
      ],
    },
  };

  beforeEach(() => {
    vi.mocked(fetchTokenMetrics).mockImplementation((range = '30m') => Promise.resolve({ ...series, range }));
    vi.mocked(fetchToolUsage).mockResolvedValue({
      servers: {
        github: {
          create_issue: { calls: 4, lastCalledAt: '2026-07-01T00:00:00Z', inputTokens: 120, outputTokens: 80 },
          list_repos: { calls: 1, inputTokens: 30, outputTokens: 10 },
        },
      },
    });
    statusOverride.current = {
      status: {
        'mcp-servers': [{ name: 'github' }, { name: 'atlassian' }],
        token_usage: {
          session: { input_tokens: 100, output_tokens: 40, total_tokens: 140 },
          per_server: {
            github: { input_tokens: 60, output_tokens: 20, total_tokens: 80 },
            atlassian: { input_tokens: 40, output_tokens: 20, total_tokens: 60 },
          },
          per_client: { claude: { input_tokens: 100, output_tokens: 40, total_tokens: 140 } },
          format_savings: { original_tokens: 0, formatted_tokens: 0, saved_tokens: 0, savings_percent: 0 },
        },
      } as unknown as GatewayStatus,
      source: 'self',
      settled: true,
      error: null,
      refresh: vi.fn().mockResolvedValue(undefined),
    };
  });

  afterEach(() => {
    statusOverride.current = null;
  });

  function panel(label: string): HTMLElement {
    let el: HTMLElement | null = screen.getByText(label);
    while (el && !el.querySelector('table')) el = el.parentElement;
    if (!el) throw new Error(`no table under ${label}`);
    return el;
  }

  const headersIn = (el: HTMLElement) =>
    within(el.querySelector('table')!)
      .getAllByRole('columnheader')
      .map((h) => h.textContent);

  async function ready() {
    renderPage();
    await screen.findByText('Per-Tool · session totals');
    await waitFor(() => expect(screen.getByText('create_issue')).toBeInTheDocument());
  }

  it('shows the Tools and Servers columns, the window header and caption, and four Clients columns', async () => {
    await ready();
    expect(headersIn(panel('Per-Tool · session totals'))).toEqual([
      'Tool',
      'Calls',
      'Input',
      'Output',
      'Total',
      'Avg/call',
      'Last used',
    ]);
    expect(headersIn(panel('Per-Server · session totals'))).toEqual([
      'Server',
      'Calls',
      'Last 30m tokens',
      'Input',
      'Output',
      'Total',
      'Avg/call',
      'Last used',
    ]);
    expect(
      screen.getByText('The Last 30m tokens column follows the range control. Every other number is a session total.'),
    ).toBeInTheDocument();
    expect(headersIn(panel('Top Clients · session totals'))).toEqual(['Client', 'Input', 'Output', 'Total']);
  });

  it('exports each of the three tables with its filename kind and header row', async () => {
    await ready();
    const cases: Array<[string, string, string]> = [
      ['Top Clients · session totals', 'clients', 'client,input_tokens,output_tokens,total_tokens'],
      [
        'Per-Server · session totals',
        'servers',
        'server,calls,window,window_tokens,input_tokens,output_tokens,total_tokens,avg_tokens_per_call,last_used',
      ],
      [
        'Per-Tool · session totals',
        'tools',
        'server,tool,calls,input_tokens,output_tokens,total_tokens,avg_tokens_per_call,last_used',
      ],
    ];
    expect(screen.getAllByRole('button', { name: /Export CSV/ })).toHaveLength(3);
    for (const [label, kind, header] of cases) {
      vi.mocked(downloadTextFile).mockClear();
      fireEvent.click(within(panel(label)).getByRole('button', { name: /Export CSV/ }));
      const [csv, filename] = vi.mocked(downloadTextFile).mock.calls[0];
      expect(filename).toMatch(new RegExp(`^gridctl-metrics-${kind}-.+\\.csv$`));
      expect(csv.split('\r\n')[0]).toBe(header);
    }
  });

  it('sums the ranged series into the server window column', async () => {
    await ready();
    const rows = within(panel('Per-Server · session totals').querySelector('table')!).getAllByRole('row');
    const github = rows.find((r) => r.textContent?.startsWith('github'))!;
    await waitFor(() => expect(within(github).getAllByRole('cell')[2].textContent).toBe('11'));
  });
});
