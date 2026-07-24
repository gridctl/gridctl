import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { TracesWorkspace } from '../components/workspaces/TracesWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useTracesStore } from '../stores/useTracesStore';
import { useUIStore, COMPACT_MODE_DEFAULTS } from '../stores/useUIStore';
import type { TraceDetail, TraceSummary } from '../lib/api';
import type { MCPServerStatus } from '../types';

vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({
    openDetachedWindow: vi.fn(),
    closeDetachedWindow: vi.fn(),
    broadcastStateUpdate: vi.fn(),
    broadcastSelectionChange: vi.fn(),
  }),
}));

vi.mock('../lib/clipboard', () => ({
  copyTextToClipboard: vi.fn().mockResolvedValue(undefined),
}));

const summary: TraceSummary = {
  traceId: 'abc123def4567890',
  rootSpanId: 's1',
  operation: 'github › create_issue',
  tool: 'create_issue',
  client: 'claude-code',
  server: 'github',
  startTime: '2026-07-23T10:00:00Z',
  duration: 42,
  spanCount: 1,
  hasError: false,
  status: 'ok',
};

// An infra trace: no tool, root kept a non-tool-call operation. Hidden by the
// default Tool calls segment.
const infraSummary: TraceSummary = {
  traceId: 'ffff0000ffff0000',
  rootSpanId: 's9',
  operation: 'session.handshake',
  tool: '',
  client: '',
  server: '',
  startTime: '2026-07-23T10:00:01Z',
  duration: 3,
  spanCount: 1,
  hasError: false,
  status: 'ok',
};

// Mirrors the real /api/traces/{id} payload: parentSpanId ('' for roots),
// endTime optional. Hand-built fixtures drifting from the served shape is
// exactly the bug class this file guards against.
const detail: TraceDetail = {
  traceId: 'abc123def4567890',
  spans: [
    {
      spanId: 's1',
      parentSpanId: '',
      name: 'github › create_issue',
      startTime: '2026-07-23T10:00:00.000Z',
      endTime: '2026-07-23T10:00:00.042Z',
      duration: 42,
      status: 'ok',
      attributes: { 'server.name': 'github', 'gen_ai.usage.input_tokens': '', 'custom.key': 'v1' },
      events: [],
    },
    {
      spanId: 's2',
      parentSpanId: 's1',
      name: 'mcp.client.call_tool',
      startTime: '2026-07-23T10:00:00.005Z',
      // endTime intentionally absent: the UI must derive it from duration.
      duration: 35,
      status: 'ok',
      attributes: {
        'server.name': 'github',
        'tool.name': 'create_issue',
        'mcp.client.name': 'claude-code',
        'gen_ai.cost.usd': '0.0123',
      },
      events: [],
    },
  ],
};

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchTraces: vi.fn(),
    fetchTraceDetail: vi.fn(),
    fetchTraceOTLP: vi.fn(),
  };
});

import { fetchTraces, fetchTraceDetail } from '../lib/api';
import { copyTextToClipboard } from '../lib/clipboard';

function server(name: string): MCPServerStatus {
  return { name, transport: 'stdio', initialized: true, tools: [], healthy: true } as unknown as MCPServerStatus;
}

function renderAt(initialEntry: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/traces" element={<TracesWorkspace />} />
        <Route path="/logs" element={<div data-testid="logs-probe" />} />
        <Route path="/metrics" element={<div data-testid="metrics-probe" />} />
      </Routes>
    </MemoryRouter>,
  );
}

function listEnvelope(traces: TraceSummary[], overrides?: Partial<Awaited<ReturnType<typeof fetchTraces>>>) {
  return {
    traces,
    total: traces.length,
    tracingEnabled: true,
    bufferSize: traces.length,
    bufferCapacity: 1000,
    ...overrides,
  };
}

beforeEach(() => {
  vi.mocked(fetchTraces).mockResolvedValue(listEnvelope([summary]));
  vi.mocked(fetchTraceDetail).mockResolvedValue(detail);
  vi.mocked(copyTextToClipboard).mockClear();
  useStackStore.setState({ mcpServers: [server('github')] });
  useUIStore.setState({
    compactMode: { ...COMPACT_MODE_DEFAULTS },
    tracesDetached: false,
    tracesPrefs: { segment: 'tool-calls', server: '' },
  });
  useTracesStore.setState({
    traces: [],
    total: 0,
    tracingEnabled: true,
    bufferSize: 0,
    bufferCapacity: 0,
    isLoading: false,
    isPaused: false,
    error: null,
    filters: { segment: 'tool-calls', server: '', errorsOnly: false, minDuration: null, timeRange: 'all', search: '' },
    selectedTraceId: null,
    traceDetail: null,
    isLoadingDetail: false,
    detailError: null,
  });
});

describe('TracesWorkspace', () => {
  it('shows the global trace list with the tool as the primary label', async () => {
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('create_issue')).toBeInTheDocument();
    });
    expect(screen.getByText('claude-code')).toBeInTheDocument();
    expect(vi.mocked(fetchTraces)).toHaveBeenCalled();
  });

  it('hides infra traces behind the Tool calls segment and counts them', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(listEnvelope([summary, infraSummary]));
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('create_issue')).toBeInTheDocument();
    });
    expect(screen.queryByText('session.handshake')).toBeNull();
    expect(screen.getByText(/1 infra hidden/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'All' }));
    await waitFor(() => {
      expect(screen.getByText('session.handshake')).toBeInTheDocument();
    });
  });

  it('offers Show all when only infra traces are in the buffer', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(listEnvelope([infraSummary]));
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('No tool-call traces yet')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Show all' }));
    await waitFor(() => {
      expect(screen.getByText('session.handshake')).toBeInTheDocument();
    });
  });

  it('explains how to enable tracing when the gateway has no buffer', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(
      listEnvelope([], { tracingEnabled: false, bufferCapacity: 0 }),
    );
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('Tracing is disabled')).toBeInTheDocument();
    });
    expect(screen.getByText(/gateway\.tracing/)).toBeInTheDocument();
  });

  it('pauses live updates from the control bar', async () => {
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('create_issue')).toBeInTheDocument();
    });
    expect(screen.getByText('Live')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /Pause live updates/i }));
    expect(useTracesStore.getState().isPaused).toBe(true);
    expect(screen.getByText('Paused')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Resume live updates/i })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });

  it('copies the full trace ID from the row action', async () => {
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('create_issue')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTitle(/Copy trace ID abc123de/));
    await waitFor(() => {
      expect(vi.mocked(copyTextToClipboard)).toHaveBeenCalledWith('abc123def4567890');
    });
  });

  it('navigates rows with j/k and opens with Enter, ignoring typing targets', async () => {
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('create_issue')).toBeInTheDocument();
    });

    // Typing 'j' in the search box must not move the highlight.
    const search = screen.getByPlaceholderText('Search traces…');
    fireEvent.keyDown(search, { key: 'j' });
    fireEvent.keyDown(document.body, { key: 'j' });
    fireEvent.keyDown(document.body, { key: 'Enter' });
    await waitFor(() => {
      expect(vi.mocked(fetchTraceDetail)).toHaveBeenCalledWith('abc123def4567890');
    });

    // Escape closes the waterfall.
    await waitFor(() => {
      expect(screen.getByText('mcp.client.call_tool')).toBeInTheDocument();
    });
    fireEvent.keyDown(document.body, { key: 'Escape' });
    await waitFor(() => {
      expect(useTracesStore.getState().selectedTraceId).toBeNull();
    });
  });

  it('resolves a ?trace= deep link into the selected waterfall', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(vi.mocked(fetchTraceDetail)).toHaveBeenCalledWith('abc123def4567890');
    });
    await waitFor(() => {
      expect(screen.getByText('github › create_issue')).toBeInTheDocument();
    });
  });

  it('restores the All segment from a ?seg=all deep link', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(listEnvelope([summary, infraSummary]));
    renderAt('/traces?seg=all');
    await waitFor(() => {
      expect(screen.getByText('session.handshake')).toBeInTheDocument();
    });
    expect(useTracesStore.getState().filters.segment).toBe('all');
  });

  it('renders a finite waterfall duration and indents child spans', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('mcp.client.call_tool')).toBeInTheDocument();
    });
    expect(screen.getByText(/2 spans · 42ms/)).toBeInTheDocument();
    expect(screen.queryByText(/NaN/)).toBeNull();
    // Child span (parentSpanId: s1) indents one level; the root does not.
    expect(screen.getByText('mcp.client.call_tool').parentElement).toHaveStyle({ paddingLeft: '12px' });
    expect(screen.getByText('github › create_issue').parentElement).toHaveStyle({ paddingLeft: '0px' });
  });

  it('shows self time for parent spans in the waterfall', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('mcp.client.call_tool')).toBeInTheDocument();
    });
    // Root 42ms minus child coverage 35ms = 7ms self.
    expect(screen.getByText(/self 7\.0ms/)).toBeInTheDocument();
  });

  it('derives span End when endTime is absent and never shows Invalid Date', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('mcp.client.call_tool')).toBeInTheDocument();
    });
    // s2 has no endTime; its detail must derive End from startTime + duration.
    fireEvent.click(screen.getByText('mcp.client.call_tool'));
    await waitFor(() => {
      expect(screen.getByText('End')).toBeInTheDocument();
    });
    expect(screen.queryByText(/Invalid Date/)).toBeNull();
    expect(screen.queryByText(/NaN/)).toBeNull();
  });

  it('promotes MCP attributes and shows the cost pill in span detail', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('mcp.client.call_tool')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('mcp.client.call_tool'));
    await waitFor(() => {
      expect(screen.getByText('MCP')).toBeInTheDocument();
    });
    // Promoted fields render as labels, cost as a formatted pill. ('Client'
    // and 'Server' are unique here: the split-view table drops both columns.)
    expect(screen.getByText('Client')).toBeInTheDocument();
    expect(screen.getByText('Server')).toBeInTheDocument();
    expect(screen.getAllByText('$0.012').length).toBeGreaterThan(0);
    // Promoted keys don't repeat under Other attributes.
    expect(screen.queryByText('mcp.client.name')).toBeNull();
  });

  it('drops empty-string attributes and collapses the rest under Other', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('github › create_issue')).toBeInTheDocument();
    });
    // Root span: server.name promotes, custom.key goes to Other, the empty
    // gen_ai counter disappears entirely.
    fireEvent.click(screen.getByText('github › create_issue'));
    await waitFor(() => {
      expect(screen.getByText('Other attributes (1)')).toBeInTheDocument();
    });
    expect(screen.queryByText('custom.key')).toBeNull(); // collapsed
    fireEvent.click(screen.getByText('Other attributes (1)'));
    expect(screen.getByText('custom.key')).toBeInTheDocument();
    expect(screen.queryByText('gen_ai.usage.input_tokens')).toBeNull();
  });

  it('shows the evicted-trace empty state when the detail 404s', async () => {
    vi.mocked(fetchTraceDetail).mockRejectedValue(new Error('trace not found'));
    renderAt('/traces?trace=gone0000gone0000');
    await waitFor(() => {
      expect(screen.getByText('Trace no longer in buffer')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Clear selection' }));
    await waitFor(() => {
      expect(useTracesStore.getState().selectedTraceId).toBeNull();
    });
  });

  it('mirrors a row selection into the URL', async () => {
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('create_issue')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('create_issue'));
    await waitFor(() => {
      expect(useTracesStore.getState().selectedTraceId).toBe('abc123def4567890');
    });
  });

  it('pivots from the trace detail to logs filtered by the trace id', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('github › create_issue')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole('button', { name: /View logs/ }));
    expect(screen.getByTestId('logs-probe')).toBeInTheDocument();
  });

  it('pivots from the trace detail to metrics for the same server', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('github › create_issue')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole('button', { name: /View metrics/ }));
    expect(screen.getByTestId('metrics-probe')).toBeInTheDocument();
  });

  it('shows buffer pressure next to the result count', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(
      listEnvelope([summary], { bufferSize: 950, bufferCapacity: 1000 }),
    );
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText(/950\/1000/)).toBeInTheDocument();
    });
  });
});
