import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { StatusBar } from '../components/layout/StatusBar';
import { useStackStore } from '../stores/useStackStore';
import type { MCPServerStatus } from '../types';

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
  });
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
    expect(screen.getByText('MCP')).toBeInTheDocument();
    // Two unhealthy servers surface here, where the header used to mirror them.
    expect(screen.getByText('err')).toBeInTheDocument();
  });

  it('omits the unhealthy indicator when all servers are healthy', () => {
    useStackStore.setState({ mcpServers: [server('s1', true), server('s2', true)] });
    renderBar();
    expect(screen.getByText('MCP')).toBeInTheDocument();
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
});
