import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import '@testing-library/jest-dom';
import { MetricsWorkspace } from '../components/workspaces/MetricsWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore, COMPACT_MODE_DEFAULTS } from '../stores/useUIStore';
import type { MCPServerStatus, TokenMetricsResponse, TokenUsage } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));
vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({
    openDetachedWindow: vi.fn(),
    closeDetachedWindow: vi.fn(),
    broadcastStateUpdate: vi.fn(),
    broadcastSelectionChange: vi.fn(),
  }),
}));
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
import { fetchTokenMetrics, fetchOptimizeReport } from '../lib/api';

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
    expect(await screen.findByText('Clear all metrics?')).toBeInTheDocument();
    expect(screen.getByText(/all recorded token and tool usage history/)).toBeInTheDocument();
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
    // The inspector shows the tool's KPI grid (Calls is tools-only).
    expect(await screen.findByText('Calls')).toBeInTheDocument();
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
    expect(screen.getByText('2.3M tok/wk')).toBeInTheDocument();
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
    expect(await screen.findByText('Calls')).toBeInTheDocument();
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
});
