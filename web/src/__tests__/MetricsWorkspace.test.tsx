import { describe, it, expect, beforeEach, vi } from 'vitest';
import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import '@testing-library/jest-dom';
import { MetricsWorkspace } from '../components/workspaces/MetricsWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore, COMPACT_MODE_DEFAULTS } from '../stores/useUIStore';
import type { MCPServerStatus, TokenMetricsResponse, TokenUsage } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));
vi.mock('../lib/download', async (importActual) => {
  const actual = await importActual<typeof import('../lib/download')>();
  return { ...actual, downloadTextFile: vi.fn() };
});
vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({
    openDetachedWindow: vi.fn(),
    closeDetachedWindow: vi.fn(),
    broadcastStateUpdate: vi.fn(),
    broadcastSelectionChange: vi.fn(),
  }),
}));
const statusRefresh = vi.fn(() => Promise.resolve());
vi.mock('../hooks/usePolling', async (importActual) => {
  const actual = await importActual<typeof import('../hooks/usePolling')>();
  return { ...actual, usePollingStatusRefresh: () => statusRefresh };
});
vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchTokenMetrics: vi.fn(),
    fetchOptimizeReport: vi.fn(),
    clearTokenMetrics: vi.fn().mockResolvedValue(undefined),
    fetchToolUsage: vi.fn().mockResolvedValue({
      servers: {
        github: {
          create_issue: { calls: 4, lastCalledAt: '2026-07-01T00:00:00Z', inputTokens: 120, outputTokens: 80 },
          list_repos: { calls: 1, inputTokens: 30, outputTokens: 10 },
        },
      },
    }),
  };
});

// Imported after the mock factory so the mock-then-import order is visible.
import { fetchTokenMetrics, fetchOptimizeReport, fetchToolUsage, clearTokenMetrics, HTTPError } from '../lib/api';
import { downloadTextFile } from '../lib/download';
import { showToast } from '../components/ui/Toast';
import { BreakdownTable } from '../components/metrics/metricsShared';
import { SERVER_COLUMNS } from '../components/metrics/metricsData';

function server(name: string): MCPServerStatus {
  return { name, transport: 'stdio', initialized: true, tools: [], healthy: true } as unknown as MCPServerStatus;
}

const tokenUsage: TokenUsage = {
  session: { input_tokens: 100, output_tokens: 40, total_tokens: 140 },
  per_server: {
    github: { input_tokens: 60, output_tokens: 20, total_tokens: 80 },
    atlassian: { input_tokens: 40, output_tokens: 20, total_tokens: 60 },
  },
  per_client: { claude: { input_tokens: 100, output_tokens: 40, total_tokens: 140 } },
  format_savings: { original_tokens: 0, formatted_tokens: 0, saved_tokens: 0, savings_percent: 0 },
};

// Series fixtures. The mocked fetcher echoes the requested range (the hook
// discards responses whose range does not match the active one), and the
// default window is empty — the common Live case for an idle stack. Tests
// that exercise the window math swap in the seeded buckets.
const emptyTokenSeries: TokenMetricsResponse = {
  range: '30m',
  interval: '1m',
  data_points: [],
  per_server: {},
};
// Values chosen so the compact-formatted KPI cards ("1.2k", "567", "1.8k")
// cannot collide with any number in the Overview preview tables.
const seededTokenSeries: TokenMetricsResponse = {
  range: '30m',
  interval: '1m',
  data_points: [
    { timestamp: '2026-01-01T00:00:00Z', input_tokens: 1000, output_tokens: 400, total_tokens: 1400 },
    { timestamp: '2026-01-01T00:01:00Z', input_tokens: 234, output_tokens: 167, total_tokens: 401 },
  ],
  per_server: {},
};

function mockSeries(tokens: TokenMetricsResponse) {
  vi.mocked(fetchTokenMetrics).mockImplementation((range = '1h') =>
    Promise.resolve({ ...tokens, range }),
  );
}

function seed(over: Partial<ReturnType<typeof useStackStore.getState>> = {}) {
  useStackStore.setState({
    isLoading: false,
    mcpServers: [server('github'), server('atlassian')],
    tokenUsage,
    ...over,
  });
}

beforeEach(() => {
  vi.clearAllMocks();
  useUIStore.setState({ compactMode: { ...COMPACT_MODE_DEFAULTS } });
  seed();
  mockSeries(emptyTokenSeries);
  vi.mocked(fetchOptimizeReport).mockResolvedValue({ findings: [], health_score: 100, generated_at: 't' });
});

function renderAt(path = '/metrics') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <MetricsWorkspace />
    </MemoryRouter>,
  );
}

describe('MetricsWorkspace', () => {
  it('renders the scope navigator, the window label, and the session line', async () => {
    renderAt();
    expect(screen.getByRole('button', { name: /^overview/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^clients/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^servers/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^tools/i })).toBeInTheDocument();
    // The Models scope left with the cost layer.
    expect(screen.queryByRole('button', { name: /^models/i })).not.toBeInTheDocument();
    // KPI cards are window-scoped (labeled by the active range); the session
    // snapshot renders once, on its own explicitly labeled line.
    expect(await screen.findByText('Total Tokens')).toBeInTheDocument();
    expect(screen.getByText('Last 30m')).toBeInTheDocument();
    expect(screen.getByText(/Session total: 140 tokens/)).toBeInTheDocument();
  });

  it('binds the KPI cards to the ranged series, not the session snapshot', async () => {
    mockSeries(seededTokenSeries);
    renderAt();
    // Window sums: 1.2k in / 567 out / 1.8k total — not the 140-token
    // session snapshot, which stays on the session line.
    expect(await screen.findByText('1.8k')).toBeInTheDocument();
    expect(screen.getByText('1.2k')).toBeInTheDocument();
    expect(screen.getByText('567')).toBeInTheDocument();
    expect(screen.getByText(/Session total: 140 tokens/)).toBeInTheDocument();
  });

  it('shows the format-savings card with an em dash until a conversion saved tokens', async () => {
    renderAt();
    expect(await screen.findByText(/Format Savings/)).toBeInTheDocument();
    expect(screen.getByText('—')).toBeInTheDocument();
  });

  it('promotes measured format savings into the fourth KPI card', async () => {
    seed({
      tokenUsage: {
        ...tokenUsage,
        format_savings: { original_tokens: 1000, formatted_tokens: 660, saved_tokens: 340, savings_percent: 34 },
      },
    });
    renderAt();
    expect(await screen.findByText('34%')).toBeInTheDocument();
    expect(screen.getByText(/340 tokens saved by output-format conversion/)).toBeInTheDocument();
  });

  it('notes an idle window instead of presenting lifetime numbers unlabeled', async () => {
    renderAt();
    expect(await screen.findByText(/No activity in this window/)).toBeInTheDocument();
  });

  it('switches the window when a range is selected and mirrors it to ?range=', async () => {
    renderAt();
    fireEvent.click(screen.getByRole('button', { name: '24h' }));
    // The control reads back from the URL param, so a pressed state proves
    // the round-trip; the fetch carries the new range to the API.
    expect(await screen.findByText('Last 24h')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '24h' })).toHaveAttribute('aria-pressed', 'true');
    expect(vi.mocked(fetchTokenMetrics)).toHaveBeenLastCalledWith('24h');
  });

  it('restores the range from a ?range= deep link', () => {
    renderAt('/metrics?range=24h');
    expect(screen.getByRole('button', { name: '24h' })).toHaveAttribute('aria-pressed', 'true');
    expect(vi.mocked(fetchTokenMetrics)).toHaveBeenCalledWith('24h');
  });

  it('names the full blast radius in the clear confirm', async () => {
    renderAt();
    fireEvent.click(screen.getByTitle('Clear Metrics'));
    const dialog = await screen.findByRole('alertdialog', { name: /clear all metrics/i });
    expect(dialog).toHaveTextContent(/all recorded token and tool usage history/);
  });

  it('offers no pricing-manager entry point anywhere in the workspace', async () => {
    renderAt();
    expect(await screen.findByText('Total Tokens')).toBeInTheDocument();
    expect(screen.queryByText(/pricing/i)).not.toBeInTheDocument();
    expect(screen.queryByTitle('Edit pricing models')).not.toBeInTheDocument();
  });

  it('switches to the servers breakdown and selects a row into the inspector', async () => {
    renderAt();
    fireEvent.click(screen.getByRole('button', { name: /^servers/i }));

    // The breakdown table now lists each server, labeled as session totals.
    expect(await screen.findByText('Per-Server · session totals')).toBeInTheDocument();
    expect(screen.getByText('github')).toBeInTheDocument();
    expect(screen.getByText('atlassian')).toBeInTheDocument();

    // Selecting a row opens the inspector.
    fireEvent.click(screen.getByText('github'));
    expect(await screen.findByLabelText('Close inspector')).toBeInTheDocument();
  });

  it('selects the last row on ArrowUp with nothing selected', async () => {
    renderAt('/metrics?scope=servers');
    await screen.findByText('Per-Server · session totals');
    const rows = () => screen.getAllByRole('row').filter((r) => r.hasAttribute('aria-selected'));
    expect(rows().length).toBeGreaterThan(1);
    expect(rows().every((r) => r.getAttribute('aria-selected') === 'false')).toBe(true);

    fireEvent.keyDown(document, { key: 'ArrowUp' });
    await waitFor(() => expect(rows().at(-1)).toHaveAttribute('aria-selected', 'true'));
  });

  it('moves focus with the selection after a row is clicked', async () => {
    renderAt('/metrics?scope=servers');
    await screen.findByText('Per-Server · session totals');
    const rows = () => screen.getAllByRole('row').filter((r) => r.hasAttribute('aria-selected'));
    const first = rows()[0];
    fireEvent.click(first);
    first.focus();
    await waitFor(() => expect(rows()[0]).toHaveAttribute('aria-selected', 'true'));

    fireEvent.keyDown(first, { key: 'ArrowDown' });
    await waitFor(() => expect(rows()[1]).toHaveAttribute('aria-selected', 'true'));
    expect(rows()[1]).toHaveFocus();
  });

  it('explains an empty per-entity series instead of hiding the sparkline', async () => {
    // Also pins the ?scope=&selected= deep link the Traces pivot relies on.
    // The full suffix disambiguates the inspector note from the center
    // column's focus note, which shares the same opening sentence.
    renderAt('/metrics?scope=servers&selected=github');
    expect(await screen.findByLabelText('Close inspector')).toBeInTheDocument();
    expect(
      await screen.findByText(/No samples for github in this window\. The numbers above are session totals\./),
    ).toBeInTheDocument();
  });

  it('falls back to overview for the retired models scope', async () => {
    renderAt('/metrics?scope=models');
    // Unknown scopes normalize to overview, whose previews render.
    expect(await screen.findByText('Top Servers · session totals')).toBeInTheDocument();
  });

  it('shows the per-tool breakdown with server-qualified names under the tools scope', async () => {
    renderAt('/metrics?scope=tools');
    expect(await screen.findByText('Per-Tool · session totals')).toBeInTheDocument();
    // Rows render server › tool so name collisions across servers stay distinct.
    expect(await screen.findByText('create_issue')).toBeInTheDocument();
    expect(screen.getByText('list_repos')).toBeInTheDocument();
  });

  it('selects a tool row into the inspector with its call count', async () => {
    renderAt('/metrics?scope=tools');
    fireEvent.click(await screen.findByText('create_issue'));
    // The inspector shows the tool's KPI grid, including its call count.
    const inspector = await screen.findByRole('complementary', { name: 'Metrics inspector' });
    expect(await within(inspector).findByText('Calls')).toBeInTheDocument();
  });

  it('shows the onboarding empty state when there is no traffic', async () => {
    seed({ tokenUsage: null });
    renderAt();
    // The first-load skeleton clears once the (empty) series resolves.
    expect(await screen.findByText('Your metrics home')).toBeInTheDocument();
    expect(screen.getByText(/Metrics populate as tools are called/)).toBeInTheDocument();
  });

  it('focuses the center chart on a selected server with fleet context', async () => {
    mockSeries({ ...seededTokenSeries, per_server: { github: seededTokenSeries.data_points } });
    renderAt('/metrics?scope=servers&selected=github');
    expect(await screen.findByText('github · Token Usage')).toBeInTheDocument();
    // Focused-share line: entity equals fleet here, so 100% of window.
    expect(screen.getByText(/github: 1,801 tokens .* 100% of window/)).toBeInTheDocument();
    // Clear affordance restores the fleet view.
    fireEvent.click(screen.getByRole('button', { name: /clear focus on github/i }));
    expect(await screen.findByText('Token Usage Over Time')).toBeInTheDocument();
    expect(screen.queryByText('github · Token Usage')).not.toBeInTheDocument();
  });

  it('keeps the fleet chart with an honest note when the entity series is empty', async () => {
    mockSeries(seededTokenSeries); // per_server stays {}
    renderAt('/metrics?scope=servers&selected=github');
    expect(
      await screen.findByText(/No samples for github in this window\. The chart shows the whole stack as context\./),
    ).toBeInTheDocument();
    // Fleet titles stay — the entity's name never labels fleet data.
    expect(screen.getByText('Token Usage Over Time')).toBeInTheDocument();
    expect(screen.queryByText('github · Token Usage')).not.toBeInTheDocument();
  });

  it('explains the token gap for a selected client', async () => {
    mockSeries(seededTokenSeries);
    renderAt('/metrics?scope=clients&selected=claude');
    expect(
      await screen.findByText(/The chart shows the whole stack; per-client token series is not recorded/),
    ).toBeInTheDocument();
    expect(screen.getByText('Token Usage Over Time')).toBeInTheDocument();
  });

  it('shows top-5 previews on Overview and jumps to the full scope', async () => {
    renderAt();
    expect(await screen.findByText('Top Servers · session totals')).toBeInTheDocument();
    expect(await screen.findByText('Top Tools · session totals')).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole('button', { name: 'View all' })[0]);
    expect(await screen.findByText('Per-Server · session totals')).toBeInTheDocument();
  });

  it('deep-links an unused-server finding to its zero-traffic row', async () => {
    // The production shape: unused_server is the headline actionable finding,
    // and its server by definition has no recorded traffic — the breakdown
    // unions stack servers as zero rows so the link has somewhere to land.
    seed({ mcpServers: [server('github'), server('atlassian'), server('zapier')] });
    vi.mocked(fetchOptimizeReport).mockResolvedValue({
      findings: [
        {
          id: 'f1',
          heuristic: 'unused_server',
          severity: 'warn',
          title: 'Unused server: zapier',
          summary: 'No calls recorded for this server.',
          server: 'zapier',
          impact_tokens_per_week: 2_250_000,
          remediation: '',
          detected_at: 't',
        },
      ],
      health_score: 90,
      generated_at: 't',
    });
    renderAt();
    expect(await screen.findByText('Optimize findings')).toBeInTheDocument();
    expect(screen.getByText('~2.3M tok/wk')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /Unused server: zapier/ }));
    expect(await screen.findByText('Per-Server · session totals')).toBeInTheDocument();
    expect(await screen.findByLabelText('Close inspector')).toBeInTheDocument();
  });

  it('deep-links a tool-level finding via the server-qualified row key', async () => {
    // No current Go heuristic emits an actionable tool finding (unused_tool
    // is info), but the join must hold for future ones: server__tool, never
    // the bare tool name.
    vi.mocked(fetchOptimizeReport).mockResolvedValue({
      findings: [
        {
          id: 'f2',
          heuristic: 'future_tool_heuristic',
          severity: 'warn',
          title: 'Heavy tool: list_repos',
          summary: 'Synthetic tool-level finding.',
          server: 'github',
          tool: 'list_repos',
          remediation: '',
          detected_at: 't',
        },
      ],
      health_score: 90,
      generated_at: 't',
    });
    renderAt();
    fireEvent.click(await screen.findByRole('button', { name: /Heavy tool: list_repos/ }));
    expect(await screen.findByText('Per-Tool · session totals')).toBeInTheDocument();
    expect(await screen.findByRole('columnheader', { name: /Calls/ })).toBeInTheDocument();
  });

  it('collapses to a quiet line when only info findings exist', async () => {
    vi.mocked(fetchOptimizeReport).mockResolvedValue({
      findings: [
        {
          id: 'i1',
          heuristic: 'need_more_data',
          severity: 'info',
          title: 'Need more data',
          summary: 'Re-run after 24 hours of activity.',
          remediation: '',
          detected_at: 't',
        },
      ],
      health_score: 100,
      generated_at: 't',
    });
    renderAt();
    expect(await screen.findByText(/Optimize: Re-run after 24 hours/)).toBeInTheDocument();
    expect(screen.queryByText('Optimize findings')).not.toBeInTheDocument();
  });

  it('filters the tools list by search with a result count', async () => {
    renderAt('/metrics?scope=tools');
    expect(await screen.findByText('create_issue')).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText('Filter tools by name or server'), { target: { value: 'create' } });
    expect(await screen.findByText('1 of 2 tools')).toBeInTheDocument();
    expect(screen.queryByText('list_repos')).not.toBeInTheDocument();
    expect(screen.getByText('create_issue')).toBeInTheDocument();
  });

  it('restores tool filters from the URL and clears to the full list', async () => {
    renderAt('/metrics?scope=tools&q=zzz');
    expect(await screen.findByText('No tools match the current filters.')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(await screen.findByText('create_issue')).toBeInTheDocument();
  });

  it('focuses the tools search on /', async () => {
    renderAt('/metrics?scope=tools');
    await screen.findByText('create_issue');
    fireEvent.keyDown(document, { key: '/' });
    expect(screen.getByLabelText('Filter tools by name or server')).toHaveFocus();
  });

  it('keeps the dashboard under a banner when a refresh fails', async () => {
    mockSeries(seededTokenSeries);
    renderAt();
    expect(await screen.findByText('1.8k')).toBeInTheDocument();

    vi.mocked(fetchTokenMetrics).mockRejectedValue(new Error('Gateway connection unavailable'));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Gateway connection unavailable');
    expect(alert).toHaveTextContent('Last updated');
    expect(screen.queryByText('Metrics unavailable')).not.toBeInTheDocument();
    expect(screen.getByText('1.8k')).toBeInTheDocument();
    expect(screen.getByText('Total Tokens')).toBeInTheDocument();

    mockSeries(seededTokenSeries);
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => {
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });
  });

  it('shows the failed state when the first load fails', async () => {
    vi.mocked(fetchTokenMetrics).mockRejectedValue(new Error('Gateway connection unavailable'));
    renderAt();
    expect(await screen.findByText('Metrics unavailable')).toBeInTheDocument();
    expect(screen.getByText('Gateway connection unavailable')).toBeInTheDocument();
    expect(screen.queryByText('Total Tokens')).not.toBeInTheDocument();
  });

  it('shows loading, not the previous range error, while a range switch is in flight', async () => {
    mockSeries(seededTokenSeries);
    const { container } = renderAt();
    expect(await screen.findByText('1.8k')).toBeInTheDocument();

    vi.mocked(fetchTokenMetrics).mockRejectedValue(new Error('live window failed'));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    expect(await screen.findByText('live window failed')).toBeInTheDocument();

    let rejectSwitch: (err: Error) => void = () => {};
    vi.mocked(fetchTokenMetrics).mockImplementation(
      () => new Promise<TokenMetricsResponse>((_resolve, reject) => { rejectSwitch = reject; }),
    );
    fireEvent.click(screen.getByRole('button', { name: '24h' }));

    await waitFor(() => {
      expect(vi.mocked(fetchTokenMetrics)).toHaveBeenLastCalledWith('24h');
    });
    expect(screen.queryByText('live window failed')).not.toBeInTheDocument();
    expect(screen.queryByText('Metrics unavailable')).not.toBeInTheDocument();
    expect(screen.queryByText('1.8k')).not.toBeInTheDocument();
    expect(container.querySelector('.animate-pulse')).not.toBeNull();

    rejectSwitch(new Error('day window failed'));
    expect(await screen.findByText('Metrics unavailable')).toBeInTheDocument();
    expect(screen.getByText('day window failed')).toBeInTheDocument();
  });
});

describe('MetricsWorkspace breakdown columns and export', () => {
  const toolUsageFixture = {
    servers: {
      github: {
        create_issue: { calls: 4, lastCalledAt: '2026-07-01T00:00:00Z', inputTokens: 120, outputTokens: 80 },
        list_repos: { calls: 1, inputTokens: 30, outputTokens: 10 },
      },
    },
  };

  beforeEach(() => {
    vi.mocked(fetchToolUsage).mockResolvedValue(toolUsageFixture);
  });

  const headers = (scope: HTMLElement = document.body) =>
    within(scope)
      .getAllByRole('columnheader')
      .map((h) => h.textContent);
  const dataRows = () => screen.getAllByRole('row').filter((r) => r.hasAttribute('aria-selected'));
  const cellsOf = (text: string) => {
    const r = dataRows().find((row) => row.textContent?.includes(text));
    if (!r) throw new Error(`no row containing ${text}`);
    return within(r)
      .getAllByRole('cell')
      .map((c) => c.textContent ?? '');
  };

  it('shows Calls, Avg/call, and Last used on Tools and sorts by Calls', async () => {
    renderAt('/metrics?scope=tools');
    await screen.findByText('create_issue');
    expect(headers()).toEqual(['Tool', 'Calls', 'Input', 'Output', 'Total', 'Avg/call', 'Last used']);

    const calls = screen.getByRole('columnheader', { name: /Calls/ });
    fireEvent.click(calls);
    await waitFor(() => expect(calls).toHaveAttribute('aria-sort', 'descending'));
    expect(dataRows().map((r) => r.textContent?.includes('create_issue'))).toEqual([true, false]);
    fireEvent.click(calls);
    await waitFor(() => expect(calls).toHaveAttribute('aria-sort', 'ascending'));
    expect(dataRows()[0]).toHaveTextContent('list_repos');
  });

  it('renders per-call averages and a dash for an unrecorded Last used, sorted last', async () => {
    renderAt('/metrics?scope=tools');
    await screen.findByText('create_issue');
    const issue = cellsOf('create_issue');
    expect(issue[5]).toBe('50');
    expect(issue[6]).toMatch(/ago$/);
    const repos = cellsOf('list_repos');
    expect(repos[5]).toBe('40');
    expect(repos[6]).toContain('–');
    expect(repos[6]).toContain('no recorded calls');

    const lastUsed = screen.getByRole('columnheader', { name: /Last used/ });
    fireEvent.click(lastUsed);
    await waitFor(() => expect(lastUsed).toHaveAttribute('aria-sort', 'descending'));
    expect(dataRows().at(-1)).toHaveTextContent('list_repos');
    fireEvent.click(lastUsed);
    await waitFor(() => expect(lastUsed).toHaveAttribute('aria-sort', 'ascending'));
    expect(dataRows().at(-1)).toHaveTextContent('list_repos');
  });

  it('aggregates server calls from tool usage and shows 0 calls for an idle server', async () => {
    renderAt('/metrics?scope=servers');
    await screen.findByText('Per-Server · session totals');
    expect(headers()).toEqual([
      'Server',
      'Calls',
      'Last 30m tokens',
      'Input',
      'Output',
      'Total',
      'Avg/call',
      'Last used',
    ]);
    await waitFor(() => expect(cellsOf('github')[1]).toBe('5'));
    const atlassian = cellsOf('atlassian');
    expect(atlassian[1]).toBe('0');
    expect(atlassian[6]).toContain('–');
    expect(atlassian[7]).toContain('–');
  });

  it('sums the ranged series into the window column and follows the range', async () => {
    mockSeries({
      ...seededTokenSeries,
      per_server: {
        github: [
          { timestamp: '2026-01-01T00:00:00Z', input_tokens: 5, output_tokens: 1, total_tokens: 6 },
          { timestamp: '2026-01-01T00:01:00Z', input_tokens: 3, output_tokens: 2, total_tokens: 5 },
        ],
      },
    });
    renderAt('/metrics?scope=servers');
    await screen.findByText('Per-Server · session totals');
    expect(cellsOf('github')[2]).toBe('11');
    expect(cellsOf('atlassian')[2]).toBe('0');

    mockSeries({
      ...seededTokenSeries,
      per_server: { github: [{ timestamp: 't', input_tokens: 70, output_tokens: 7, total_tokens: 77 }] },
    });
    fireEvent.click(screen.getByRole('button', { name: '1h' }));
    expect(await screen.findByRole('columnheader', { name: /Last 1h tokens/ })).toBeInTheDocument();
    await waitFor(() => expect(cellsOf('github')[2]).toBe('77'));
  });

  it('never shows the previous or a zero window value while a range switch is pending', async () => {
    mockSeries({
      ...seededTokenSeries,
      per_server: { github: [{ timestamp: 't', input_tokens: 5, output_tokens: 6, total_tokens: 11 }] },
    });
    renderAt('/metrics?scope=servers');
    await screen.findByText('Per-Server · session totals');
    expect(cellsOf('github')[2]).toBe('11');

    let resolveSwitch: (r: TokenMetricsResponse) => void = () => {};
    vi.mocked(fetchTokenMetrics).mockImplementation(
      () => new Promise<TokenMetricsResponse>((resolve) => { resolveSwitch = resolve; }),
    );
    fireEvent.click(screen.getByRole('button', { name: '1h' }));
    await waitFor(() => expect(vi.mocked(fetchTokenMetrics)).toHaveBeenLastCalledWith('1h'));
    // The unresolved series holds the loading skeleton: no window cell
    // renders, so neither the stale 11 nor a placeholder 0 can appear.
    expect(screen.queryByText('Per-Server · session totals')).not.toBeInTheDocument();
    expect(screen.queryByRole('columnheader', { name: /tokens$/ })).not.toBeInTheDocument();

    await act(async () => {
      resolveSwitch({
        ...seededTokenSeries,
        range: '1h',
        per_server: { github: [{ timestamp: 't', input_tokens: 70, output_tokens: 7, total_tokens: 77 }] },
      });
    });
    expect(await screen.findByRole('columnheader', { name: /Last 1h tokens/ })).toBeInTheDocument();
    expect(cellsOf('github')[2]).toBe('77');
  });

  it('renders a dash, never 0, in window cells while the series is unresolved', () => {
    render(
      <BreakdownTable
        rows={[{ name: 'github', input: 1, output: 1, total: 2 }]}
        nameLabel="Server"
        sortColumn="total"
        sortDirection="desc"
        columns={SERVER_COLUMNS}
        windowLabel="Last 1h"
      />,
    );
    const cells = within(screen.getAllByRole('row')[1]).getAllByRole('cell');
    expect(cells[2].textContent).toContain('–');
    expect(cells[2].textContent).not.toContain('0');
  });

  it('keeps server token columns with dashes when tool usage fails', async () => {
    vi.mocked(fetchToolUsage).mockRejectedValue(new Error('usage down'));
    renderAt('/metrics?scope=servers');
    await screen.findByText('Per-Server · session totals');
    const github = cellsOf('github');
    expect(github[5]).toBe('80');
    expect(github[1]).toContain('–');
    expect(github[6]).toContain('–');
    expect(github[7]).toContain('–');
  });

  it('captions the window column with its exact header text and notes the server average', async () => {
    renderAt('/metrics?scope=servers&range=1h');
    await screen.findByText('Per-Server · session totals');
    const header = screen.getByRole('columnheader', { name: /tokens$/ });
    expect(header.textContent).toBe('Last 1h tokens');
    expect(
      screen.getByText(`The ${header.textContent} column follows the range control. Every other number is a session total.`),
    ).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: /Avg\/call/ })).toHaveAttribute(
      'title',
      expect.stringMatching(/from per-tool usage/),
    );
  });

  it('keeps Name, Input, Output, and Total on Clients and the Overview previews', async () => {
    const { unmount } = renderAt();
    expect(await screen.findByText('Top Servers · session totals')).toBeInTheDocument();
    const tables = screen.getAllByRole('table');
    expect(tables.length).toBeGreaterThanOrEqual(2);
    for (const t of tables) expect(headers(t).slice(1)).toEqual(['Input', 'Output', 'Total']);
    expect(screen.queryByRole('button', { name: /Export CSV/ })).not.toBeInTheDocument();
    unmount();

    renderAt('/metrics?scope=clients');
    expect(await screen.findByText('Top Clients · session totals')).toBeInTheDocument();
    expect(headers()).toEqual(['Client', 'Input', 'Output', 'Total']);
  });

  it('exports the filtered, sorted tools table as CSV and toasts the count', async () => {
    renderAt('/metrics?scope=tools&q=create');
    await screen.findByText('create_issue');
    fireEvent.click(screen.getByRole('button', { name: /Export CSV/ }));
    const [csv, filename, mime] = vi.mocked(downloadTextFile).mock.calls[0];
    expect(csv).toBe(
      'server,tool,calls,input_tokens,output_tokens,total_tokens,avg_tokens_per_call,last_used\r\n' +
        'github,create_issue,4,120,80,200,50,2026-07-01T00:00:00Z\r\n',
    );
    expect(filename).toMatch(/^gridctl-metrics-tools-.+\.csv$/);
    expect(mime).toBe('text/csv;charset=utf-8');
    expect(showToast).toHaveBeenCalledWith('success', 'Exported 1 row as CSV');
  });

  it('exports servers and clients with their headers', async () => {
    const { unmount } = renderAt('/metrics?scope=servers');
    await screen.findByText('Per-Server · session totals');
    await waitFor(() => expect(cellsOf('github')[1]).toBe('5'));
    fireEvent.click(screen.getByRole('button', { name: /Export CSV/ }));
    let [csv, filename] = vi.mocked(downloadTextFile).mock.calls[0];
    const lines = csv.split('\r\n');
    expect(lines[0]).toBe(
      'server,calls,window,window_tokens,input_tokens,output_tokens,total_tokens,avg_tokens_per_call,last_used',
    );
    expect(lines[1]).toBe('github,5,30m,0,60,20,80,48,2026-07-01T00:00:00Z');
    expect(filename).toMatch(/^gridctl-metrics-servers-/);
    expect(showToast).toHaveBeenCalledWith('success', 'Exported 2 rows as CSV');
    unmount();

    renderAt('/metrics?scope=clients');
    await screen.findByText('Top Clients · session totals');
    fireEvent.click(screen.getByRole('button', { name: /Export CSV/ }));
    [csv, filename] = vi.mocked(downloadTextFile).mock.calls[1];
    expect(csv).toBe('client,input_tokens,output_tokens,total_tokens\r\nclaude,100,40,140\r\n');
    expect(filename).toMatch(/^gridctl-metrics-clients-/);
  });

  it('disables export when the displayed table has no rows', async () => {
    renderAt('/metrics?scope=tools&q=zzz');
    expect(await screen.findByText('No tools match the current filters.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Export CSV/ })).toBeDisabled();
  });

  it('scrolls the newly selected row into view once per selection change', async () => {
    const scrollSpy = vi.fn();
    const original = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = scrollSpy;
    try {
      renderAt('/metrics?scope=tools');
      await screen.findByText('create_issue');
      expect(scrollSpy).not.toHaveBeenCalled();
      fireEvent.keyDown(document, { key: 'ArrowDown' });
      await waitFor(() => expect(dataRows()[0]).toHaveAttribute('aria-selected', 'true'));
      expect(scrollSpy).toHaveBeenCalledTimes(1);
      expect(scrollSpy).toHaveBeenCalledWith({ block: 'nearest' });
      expect(scrollSpy.mock.contexts[0]).toBe(dataRows()[0]);

      // A poll landing with the same selection re-renders the rows only.
      act(() => {
        useStackStore.setState({ tokenUsage: { ...tokenUsage } });
      });
      expect(scrollSpy).toHaveBeenCalledTimes(1);
    } finally {
      Element.prototype.scrollIntoView = original;
    }
  });

  it('scrolls a ?selected= deep-linked row into view on first render', async () => {
    const scrollSpy = vi.fn();
    const original = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = scrollSpy;
    try {
      renderAt('/metrics?scope=servers&selected=github');
      await screen.findByText('Per-Server · session totals');
      const github = dataRows().find((r) => r.textContent?.includes('github'));
      expect(github).toHaveAttribute('aria-selected', 'true');
      await waitFor(() => expect(scrollSpy).toHaveBeenCalledTimes(1));
      expect(scrollSpy).toHaveBeenCalledWith({ block: 'nearest' });
      expect(scrollSpy.mock.contexts[0]).toBe(github);
    } finally {
      Element.prototype.scrollIntoView = original;
    }
  });
});

function deferred<T>() {
  let resolve!: (v: T) => void;
  const promise = new Promise<T>((r) => { resolve = r; });
  return { promise, resolve };
}

const changedUsage: TokenUsage = {
  ...tokenUsage,
  session: { input_tokens: 500, output_tokens: 200, total_tokens: 700 },
  per_server: {
    ...tokenUsage.per_server,
    github: { input_tokens: 300, output_tokens: 99, total_tokens: 399 },
  },
};

async function confirmClear() {
  fireEvent.click(screen.getByTitle('Clear Metrics'));
  const dialog = await screen.findByRole('alertdialog', { name: /clear all metrics/i });
  fireEvent.click(within(dialog).getByRole('button', { name: 'Clear' }));
}

describe('MetricsWorkspace clear, refresh, and pause feedback', () => {
  it('keeps every number and toasts once when clear fails', async () => {
    vi.mocked(clearTokenMetrics).mockRejectedValueOnce(new HTTPError(500, '500 Internal Server Error'));
    renderAt('/metrics?scope=servers');
    await screen.findByText('Per-Server · session totals');
    const fetchesBefore = vi.mocked(fetchTokenMetrics).mock.calls.length;
    await confirmClear();
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('error', expect.any(String), expect.anything()));
    const text = vi.mocked(showToast).mock.calls[0][1];
    expect(text.startsWith('Clear metrics failed')).toBe(true);
    expect(text.split('Clear metrics failed').length).toBe(2);
    expect(screen.getByText(/Session total: 140 tokens/)).toBeInTheDocument();
    expect(screen.getByText('github')).toBeInTheDocument();
    expect(screen.getByText('atlassian')).toBeInTheDocument();
    expect(vi.mocked(fetchTokenMetrics).mock.calls.length).toBe(fetchesBefore);
    expect(showToast).not.toHaveBeenCalledWith('success', 'Metrics cleared');
  });

  it('refreshes status and tool usage alongside the series after a clear', async () => {
    renderAt();
    await screen.findByText('Total Tokens');
    await waitFor(() => expect(fetchToolUsage).toHaveBeenCalledTimes(1));
    const pending = deferred<TokenMetricsResponse>();
    vi.mocked(fetchTokenMetrics).mockReturnValueOnce(pending.promise);
    await confirmClear();
    await waitFor(() => expect(clearTokenMetrics).toHaveBeenCalledTimes(1));
    // Sent before the post-clear series fetch resolves.
    await waitFor(() => expect(statusRefresh).toHaveBeenCalledTimes(1));
    expect(fetchToolUsage).toHaveBeenCalledTimes(2);
    expect(showToast).not.toHaveBeenCalled();
    await act(async () => { pending.resolve(emptyTokenSeries); });
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Metrics cleared'));
  });

  it('refetches the series on Resume and not on Pause', async () => {
    renderAt();
    await screen.findByText('Total Tokens');
    const before = vi.mocked(fetchTokenMetrics).mock.calls.length;
    fireEvent.click(screen.getByTitle('Pause live updates'));
    expect(vi.mocked(fetchTokenMetrics).mock.calls.length).toBe(before);
    fireEvent.click(screen.getByTitle('Resume live updates'));
    await waitFor(() => expect(vi.mocked(fetchTokenMetrics).mock.calls.length).toBe(before + 1));
  });

  it('announces a refresh only after it settles', async () => {
    renderAt();
    await screen.findByText('Total Tokens');
    const pending = deferred<TokenMetricsResponse>();
    vi.mocked(fetchTokenMetrics).mockReturnValueOnce(pending.promise);
    fireEvent.click(screen.getByTitle('Refresh'));
    expect(screen.queryByText('Metrics refreshed')).not.toBeInTheDocument();
    await act(async () => { pending.resolve(emptyTokenSeries); });
    expect(await screen.findByText('Metrics refreshed')).toBeInTheDocument();
    expect(statusRefresh).toHaveBeenCalled();
  });

  it('shows no focus chip for a selected client and keeps its note', async () => {
    mockSeries(seededTokenSeries);
    renderAt('/metrics?scope=clients&selected=claude');
    expect(
      await screen.findByText(/The chart shows the whole stack; per-client token series is not recorded/),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /clear focus on/i })).not.toBeInTheDocument();
  });

  it('shows no focus chip for a server with no samples and keeps its note', async () => {
    mockSeries(seededTokenSeries);
    renderAt('/metrics?scope=servers&selected=github');
    expect(
      await screen.findByText(/No samples for github in this window\. The chart shows the whole stack as context\./),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /clear focus on/i })).not.toBeInTheDocument();
  });

  it('holds the tables and session line while paused on Live', async () => {
    renderAt('/metrics?scope=servers');
    await screen.findByText('Per-Server · session totals');
    fireEvent.click(screen.getByTitle('Pause live updates'));
    act(() => { useStackStore.setState({ tokenUsage: changedUsage }); });
    expect(screen.getByText(/Session total: 140 tokens/)).toBeInTheDocument();
    expect(screen.queryByText('399')).not.toBeInTheDocument();

    fireEvent.click(screen.getByTitle('Resume live updates'));
    expect(await screen.findByText(/Session total: 700 tokens/)).toBeInTheDocument();
    expect(screen.getByText('399')).toBeInTheDocument();
  });

  it('applies a refresh while paused and stays paused', async () => {
    renderAt('/metrics?scope=servers');
    await screen.findByText('Per-Server · session totals');
    fireEvent.click(screen.getByTitle('Pause live updates'));
    act(() => { useStackStore.setState({ tokenUsage: changedUsage }); });
    expect(screen.getByText(/Session total: 140 tokens/)).toBeInTheDocument();
    fireEvent.click(screen.getByTitle('Refresh'));
    expect(await screen.findByText(/Session total: 700 tokens/)).toBeInTheDocument();
    expect(screen.getByTitle('Resume live updates')).toBeInTheDocument();
    act(() => { useStackStore.setState({ tokenUsage }); });
    expect(screen.getByText(/Session total: 700 tokens/)).toBeInTheDocument();
  });

  it('never holds the tables on a non-live range', async () => {
    renderAt('/metrics?scope=servers');
    await screen.findByText('Per-Server · session totals');
    fireEvent.click(screen.getByTitle('Pause live updates'));
    fireEvent.click(screen.getByRole('button', { name: '1h' }));
    act(() => { useStackStore.setState({ tokenUsage: changedUsage }); });
    expect(await screen.findByText(/Session total: 700 tokens/)).toBeInTheDocument();
  });

  it('releases the pause hold once after clearing while paused', async () => {
    const clearedUsage: TokenUsage = {
      ...tokenUsage,
      session: { input_tokens: 0, output_tokens: 0, total_tokens: 0 },
      per_server: {},
      per_client: {},
    };
    statusRefresh.mockImplementationOnce(async () => {
      useStackStore.setState({ tokenUsage: clearedUsage });
    });
    renderAt('/metrics?scope=servers');
    await screen.findByText('Per-Server · session totals');
    fireEvent.click(screen.getByTitle('Pause live updates'));
    await confirmClear();
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Metrics cleared'));
    expect(screen.queryByText(/Session total: 140 tokens/)).not.toBeInTheDocument();
    expect(screen.getByTitle('Resume live updates')).toBeInTheDocument();
  });

  it('still reports a successful clear when a follow-up refresh rejects', async () => {
    statusRefresh.mockRejectedValueOnce(new Error('status blip'));
    renderAt();
    await screen.findByText('Total Tokens');
    await confirmClear();
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Metrics cleared'));
    expect(showToast).not.toHaveBeenCalledWith('error', expect.anything(), expect.anything());
  });
});

describe('MetricsWorkspace Optimize findings card', () => {
  it('renders visible summary without title tooltips, unit explanation, and Tools audit link', async () => {
    const summary = 'Server zapier registered 42 tools with no observed calls (~500 prompts/week).';
    vi.mocked(fetchOptimizeReport).mockResolvedValue({
      findings: [
        {
          id: 'f-unused',
          heuristic: 'unused_server',
          severity: 'warn',
          title: 'Unused server: zapier',
          summary,
          server: 'zapier',
          impact_tokens_per_week: 2_250_000,
          remediation: 'mcp-servers:\n  - name: zapier\n    enabled: false',
          detected_at: 't',
        },
      ],
      health_score: 85,
      generated_at: 't',
    });
    const { container } = renderAt();
    expect(await screen.findByText('Optimize findings')).toBeInTheDocument();
    expect(screen.getByText(summary)).toBeInTheDocument();
    expect(container.querySelector(`[title="${summary}"]`)).toBeNull();
    expect(screen.getByText('~2.3M tok/wk')).toBeInTheDocument();
    expect(
      screen.getByText(
        "tok/wk: Estimated schema tokens sent per week for this server's tools. Not recorded usage.",
      ),
    ).toBeInTheDocument();

    const auditLink = screen.getByRole('link', { name: 'Review in Tools audit: zapier' });
    expect(auditLink).toHaveAttribute('href', '/tools?server=zapier&audit=1');
    expect(auditLink).toHaveTextContent('Review in Tools audit');
    const titleButton = screen.getByRole('button', { name: 'Unused server: zapier' });
    expect(titleButton.className).not.toContain('truncate');
    expect(titleButton.className).toContain('break-words');
  });

  it('explains format_savings_shortfall and omits the Tools audit link', async () => {
    vi.mocked(fetchOptimizeReport).mockResolvedValue({
      findings: [
        {
          id: 'f-format',
          heuristic: 'format_savings_shortfall',
          severity: 'warn',
          title: 'Enable TOON output on github',
          summary: 'Other servers save tokens with TOON output.',
          server: 'github',
          impact_tokens_per_week: 50_000,
          remediation: 'output_format: toon',
          detected_at: 't',
        },
      ],
      health_score: 92,
      generated_at: 't',
    });
    renderAt();
    expect(await screen.findByText('Optimize findings')).toBeInTheDocument();
    expect(screen.getByText('~50k tok/wk')).toBeInTheDocument();
    expect(
      screen.getByText(
        'tok/wk: Projected output tokens saved per week, from savings measured on other servers.',
      ),
    ).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /Tools audit/i })).not.toBeInTheDocument();
  });

  it('omits both figure and unit line when impact is zero or absent', async () => {
    vi.mocked(fetchOptimizeReport).mockResolvedValue({
      findings: [
        {
          id: 'f-zero',
          heuristic: 'unused_server',
          severity: 'warn',
          title: 'Unused server: zero',
          summary: 'Zero impact finding.',
          server: 'zero',
          impact_tokens_per_week: 0,
          remediation: '',
          detected_at: 't',
        },
        {
          id: 'f-absent',
          heuristic: 'schema_overhead',
          severity: 'warn',
          title: 'Stack-wide note',
          summary: 'No server and no impact.',
          remediation: '',
          detected_at: 't',
        },
      ],
      health_score: 95,
      generated_at: 't',
    });
    renderAt();
    expect(await screen.findByText('Optimize findings')).toBeInTheDocument();
    expect(screen.getByText('Zero impact finding.')).toBeInTheDocument();
    expect(screen.getByText('No server and no impact.')).toBeInTheDocument();
    expect(screen.queryByText(/tok\/wk/)).not.toBeInTheDocument();
    // Stack-wide finding has a plain-text title and no Tools link.
    expect(screen.queryByRole('button', { name: 'Stack-wide note' })).not.toBeInTheDocument();
    const stackTitle = screen.getByText('Stack-wide note');
    expect(stackTitle).toBeInTheDocument();
    expect(stackTitle.className).not.toContain('truncate');
    expect(stackTitle.className).toContain('break-words');
  });

  it('toggles per-finding remediation with aria-expanded and aria-controls only while open', async () => {
    const snippet = 'mcp-servers:\n  - name: zapier\n    tools: [send_email]';
    vi.mocked(fetchOptimizeReport).mockResolvedValue({
      findings: [
        {
          id: 'f-with-fix',
          heuristic: 'unused_server',
          severity: 'warn',
          title: 'Unused server: zapier',
          summary: 'Zapier is unused.',
          server: 'zapier',
          remediation: snippet,
          detected_at: 't',
        },
        {
          id: 'f-no-fix',
          heuristic: 'unused_server',
          severity: 'warn',
          title: 'Unused server: empty',
          summary: 'Empty remediation finding.',
          server: 'empty',
          remediation: '   \n  ',
          detected_at: 't',
        },
      ],
      health_score: 85,
      generated_at: 't',
    });
    renderAt();
    expect(await screen.findByText('Optimize findings')).toBeInTheDocument();

    const fixButtons = screen.getAllByRole('button', { name: 'Show fix' });
    expect(fixButtons).toHaveLength(1);
    const btn = fixButtons[0];
    expect(btn).toHaveAttribute('aria-expanded', 'false');
    expect(btn).not.toHaveAttribute('aria-controls');
    expect(screen.queryByText(/tools: \[send_email\]/)).not.toBeInTheDocument();

    fireEvent.click(btn);
    expect(btn).toHaveTextContent('Hide fix');
    expect(btn).toHaveAttribute('aria-expanded', 'true');
    const controlsId = btn.getAttribute('aria-controls');
    expect(controlsId).toBeTruthy();
    const pre = document.getElementById(controlsId!);
    expect(pre).not.toBeNull();
    expect(pre?.tagName).toBe('PRE');
    expect(pre?.textContent).toBe(snippet);

    fireEvent.click(btn);
    expect(btn).toHaveTextContent('Show fix');
    expect(btn).toHaveAttribute('aria-expanded', 'false');
    expect(btn).not.toHaveAttribute('aria-controls');
    expect(document.getElementById(controlsId!)).toBeNull();
  });

  it('opens the gateway sidebar on /stack from the overflow button when more than three findings exist', async () => {
    useStackStore.setState({ selectedNodeId: null });
    useUIStore.setState({ sidebarOpen: false });
    vi.mocked(fetchOptimizeReport).mockResolvedValue({
      findings: [1, 2, 3, 4].map((n) => ({
        id: `f-${n}`,
        heuristic: 'unused_server',
        severity: 'warn' as const,
        title: `Unused server: srv-${n}`,
        summary: `Server srv-${n} has no calls.`,
        server: `srv-${n}`,
        remediation: '',
        detected_at: 't',
      })),
      health_score: 70,
      generated_at: 't',
    });

    render(
      <MemoryRouter initialEntries={['/metrics']}>
        <Routes>
          <Route path="/metrics" element={<MetricsWorkspace />} />
          <Route path="/stack" element={<div>stack-sentinel</div>} />
        </Routes>
      </MemoryRouter>,
    );

    const overflow = await screen.findByRole('button', {
      name: "+1 more in the gateway sidebar's Optimize section",
    });
    fireEvent.click(overflow);

    expect(await screen.findByText('stack-sentinel')).toBeInTheDocument();
    expect(useStackStore.getState().selectedNodeId).toBe('gateway');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });

  it('renders the info-only line without truncation or a title attribute', async () => {
    const summary = 'Re-run after 24 hours of activity so projections have enough history.';
    vi.mocked(fetchOptimizeReport).mockResolvedValue({
      findings: [
        {
          id: 'i-only',
          heuristic: 'need_more_data',
          severity: 'info',
          title: 'Need more data',
          summary,
          remediation: '',
          detected_at: 't',
        },
      ],
      health_score: 100,
      generated_at: 't',
    });
    renderAt();
    const line = await screen.findByText(`Optimize: ${summary}`);
    expect(line).not.toHaveAttribute('title');
    expect(line).not.toHaveClass('line-clamp-1');
  });
});
