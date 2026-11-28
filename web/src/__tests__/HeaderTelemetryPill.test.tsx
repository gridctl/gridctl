import { beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { HeaderTelemetryPill } from '../components/telemetry/HeaderTelemetryPill';
import { useStackStore } from '../stores/useStackStore';
import { useTelemetryStore } from '../stores/useTelemetryStore';
import type { MCPServerStatus, ResolvedTelemetry } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));
vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    updateStackTelemetry: vi.fn(),
    wipeTelemetry: vi.fn(),
  };
});

function makeServer(name: string, enabled?: boolean): MCPServerStatus {
  return {
    name,
    transport: 'http',
    initialized: true,
    healthy: true,
    toolCount: 0,
    tools: [],
    ...(enabled !== undefined ? { enabled } : {}),
  };
}

function seedStores(config: ResolvedTelemetry, servers: MCPServerStatus[]) {
  useTelemetryStore.setState({ config, inventory: [], rawSpec: null, error: null });
  useStackStore.setState({ mcpServers: servers });
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  seedStores({ global: {}, servers: {} }, []);
});

describe('HeaderTelemetryPill', () => {
  it('shows Persistence: Per-server with active styling and a popover note when an enabled server overrides metrics on', () => {
    seedStores(
      {
        global: { logs: false, metrics: false, traces: false },
        servers: { github: { metrics: true } },
      },
      [makeServer('github', true)],
    );

    render(<HeaderTelemetryPill />);

    const label = screen.getByText('Persistence: Per-server');
    expect(label).toBeInTheDocument();
    expect(label).toHaveClass('text-status-running');
    const trigger = label.closest('button')!;
    expect(trigger).toHaveClass('border-status-running/30');
    expect(trigger).toHaveAttribute('title', 'Persistence: Per-server');

    fireEvent.click(trigger);
    const dialog = screen.getByRole('dialog', { name: 'Stack telemetry persistence' });
    const metricsRow = within(dialog).getByRole('button', { name: /metrics.*on for 1 server/i });
    expect(metricsRow).toHaveTextContent('on for 1 server');
    expect(metricsRow).toHaveTextContent('Off');
    expect(metricsRow).toHaveAttribute('aria-pressed', 'false');

    const logsRow = within(dialog).getByRole('button', { name: /^logs/i });
    expect(logsRow).not.toHaveTextContent(/on for|off for/i);
  });

  it('ignores overrides on disabled servers for both the pill label and popover notes', () => {
    seedStores(
      {
        global: { logs: false, metrics: false, traces: false },
        servers: { github: { metrics: true } },
      },
      [makeServer('github', false)],
    );

    render(<HeaderTelemetryPill />);

    const label = screen.getByText('Persistence: Off');
    expect(label).toBeInTheDocument();
    expect(label).toHaveClass('text-text-secondary');
    const trigger = label.closest('button')!;
    expect(trigger).toHaveClass('border-border/50');

    fireEvent.click(trigger);
    const dialog = screen.getByRole('dialog', { name: 'Stack telemetry persistence' });
    const metricsRow = within(dialog).getByRole('button', { name: /metrics/i });
    expect(metricsRow).not.toHaveTextContent(/on for/i);
  });

  it('pluralizes multiple diverging servers and reports opt-outs when global is on', () => {
    seedStores(
      {
        global: { logs: false, metrics: true, traces: false },
        servers: {
          github: { logs: true, metrics: false },
          jira: { logs: true, metrics: false },
          disabledOne: { logs: true, metrics: false },
        },
      },
      [makeServer('github'), makeServer('jira', true), makeServer('disabledOne', false)],
    );

    render(<HeaderTelemetryPill />);

    expect(screen.getByText('Persistence: Metrics')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Persistence: Metrics' }));

    const dialog = screen.getByRole('dialog', { name: 'Stack telemetry persistence' });
    const logsRow = within(dialog).getByRole('button', { name: /logs.*on for 2 servers/i });
    expect(logsRow).toHaveTextContent('Off');
    const metricsRow = within(dialog).getByRole('button', { name: /metrics.*off for 2 servers/i });
    expect(metricsRow).toHaveTextContent('On');
    expect(metricsRow).toHaveAttribute('aria-pressed', 'true');
  });
});
