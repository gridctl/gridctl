import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, fireEvent, within, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { TracesWorkspace } from '../components/workspaces/TracesWorkspace';
import { useStackStore } from '../stores/useStackStore';
import { useTracesStore } from '../stores/useTracesStore';
import { useUIStore, COMPACT_MODE_DEFAULTS } from '../stores/useUIStore';
import { workspaceLayoutStorageKey } from '../hooks/useWorkspaceLayout';
import type { Span, TraceDetail, TraceSummary } from '../lib/api';
import { TraceRow } from '../components/traces/TraceRow';
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
    fetchRuns: vi.fn(),
    fetchRunsStatus: vi.fn(),
  };
});

import { fetchTraces, fetchTraceDetail, fetchRuns, fetchRunsStatus } from '../lib/api';
import { emptyFilters, useRunsStore } from '../stores/useRunsStore';
import type { RunRecord } from '../lib/api';
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
  // react-resizable-panels persists layouts per panel combination; clear so a
  // collapsed or resized layout never bleeds into the next test.
  localStorage.clear();
  vi.mocked(fetchTraces).mockResolvedValue(listEnvelope([summary]));
  vi.mocked(fetchTraceDetail).mockResolvedValue(detail);
  vi.mocked(copyTextToClipboard).mockClear();
  useStackStore.setState({ mcpServers: [server('github')] });
  useUIStore.setState({
    compactMode: { ...COMPACT_MODE_DEFAULTS },
    tracesDetached: false,
    tracesPrefs: { segment: 'tool-calls', server: '', nameColPct: 30 },
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
    lastLoadedAt: 0,
    loadedFilters: null,
    filters: { segment: 'tool-calls', server: '', errorsOnly: false, minDuration: null, timeRange: 'all', search: '' },
    limit: 100,
    selectedTraceId: null,
    traceDetail: null,
    isLoadingDetail: false,
    detailError: null,
  });
});

function SearchProbe() {
  const location = useLocation();
  return <span data-testid="location-search">{location.search}</span>;
}

describe('TracesWorkspace', () => {
  it('wires traces and runs tabs to one tabpanel and moves with ArrowRight', () => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: (query: string) => ({
        matches: false,
        media: query,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
        addListener: () => undefined,
        removeListener: () => undefined,
        dispatchEvent: () => false,
      }),
    });
    render(
      <MemoryRouter initialEntries={['/traces']}>
        <SearchProbe />
        <Routes>
          <Route path="/traces" element={<TracesWorkspace />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    expect(screen.getByRole('heading', { level: 1, name: 'Traces' })).toBeInTheDocument();
    const h1 = screen.getByRole('heading', { level: 1, name: 'Traces' });
    expect(h1.className).not.toMatch(/sr-only/);
    expect(screen.getByRole('tablist', { name: 'Observability view' })).toHaveClass('rounded-lg', 'border');
    const traces = screen.getByRole('tab', { name: 'Traces' });
    const runs = screen.getByRole('tab', { name: 'Runs' });
    expect(traces).toHaveClass('bg-primary/10', 'text-primary');
    expect(runs).not.toHaveClass('bg-primary/10');
    expect(runs.className).not.toContain('text-text-muted/60');
    expect(runs.className).not.toMatch(/uppercase|tracking-/);
    expect(traces).toHaveAttribute('aria-controls', 'traces-tabpanel');
    expect(runs).toHaveAttribute('aria-controls', 'traces-tabpanel');
    const panel = document.getElementById('traces-tabpanel');
    expect(panel).toHaveAttribute('role', 'tabpanel');
    expect(panel).toHaveAttribute('aria-labelledby', 'traces-tab-traces');

    traces.focus();
    fireEvent.keyDown(traces, { key: 'ArrowRight' });
    expect(runs).toHaveAttribute('aria-selected', 'true');
    expect(screen.getByTestId('location-search')).toHaveTextContent('?view=runs');
    expect(panel).toHaveAttribute('aria-labelledby', 'traces-tab-runs');
    expect(runs).toHaveClass('bg-primary/10', 'text-primary');
    expect(traces).not.toHaveClass('bg-primary/10');
    expect(traces.className).not.toContain('text-text-muted/60');
  });

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

  it('promotes MCP attributes in span detail without a cost pill', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('mcp.client.call_tool')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('mcp.client.call_tool'));
    await waitFor(() => {
      expect(screen.getByText('MCP')).toBeInTheDocument();
    });
    // Promoted fields render as labels inside the drawer. Scoped to the
    // drawer: the list headers also say Client/Server.
    const drawer = within(screen.getByTestId('span-detail'));
    expect(drawer.getByText('Client')).toBeInTheDocument();
    expect(drawer.getByText('Server')).toBeInTheDocument();
    // The cost attribute (still emitted by an older gateway) is no longer
    // promoted; it stays available under Other attributes only.
    expect(screen.queryByText('$0.012')).toBeNull();
    // Promoted keys don't repeat under Other attributes.
    expect(screen.queryByText('mcp.client.name')).toBeNull();
  });

  it('prefers gridctl attribute keys and does not derive a server from the span name', async () => {
    vi.mocked(fetchTraceDetail).mockResolvedValue({
      traceId: 'abc123def4567890',
      spans: [
        {
          spanId: 's1',
          parentSpanId: '',
          name: 'tools/call ping',
          startTime: '2026-07-23T10:00:00.000Z',
          endTime: '2026-07-23T10:00:00.042Z',
          duration: 42,
          status: 'ok',
          attributes: {
            'gridctl.server.name': 'primary',
            'server.name': 'legacy',
            'gridctl.usage.cache_read_tokens': '12',
            'gridctl.usage.cache_creation_tokens': '7',
          },
          events: [],
        },
        {
          spanId: 's2',
          parentSpanId: 's1',
          name: 'tools/call echo',
          startTime: '2026-07-23T10:00:00.005Z',
          duration: 35,
          status: 'ok',
          attributes: {},
          events: [],
        },
      ],
    });
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('tools/call ping')).toBeInTheDocument();
    });
    expect(screen.getByText('primary')).toBeInTheDocument();
    expect(screen.queryByText('legacy')).toBeNull();
    // The old waterfall fallback split span.name and would label this row
    // with the whole name. The legend must not grow a second copy.
    expect(screen.getAllByText('tools/call echo')).toHaveLength(1);

    fireEvent.click(screen.getByText('tools/call ping'));
    const drawer = await screen.findByTestId('span-detail');
    expect(within(drawer).getByText('primary')).toBeInTheDocument();
    expect(within(drawer).queryByText('legacy')).toBeNull();
    expect(within(drawer).getByText('Cache read tokens')).toBeInTheDocument();
    expect(within(drawer).getByText('12')).toBeInTheDocument();
    expect(within(drawer).getByText('Cache creation tokens')).toBeInTheDocument();
    expect(within(drawer).getByText('7')).toBeInTheDocument();
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

  it('adds resize separators per stage: none, list|waterfall plus span column, +detail drawer', async () => {
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('create_issue')).toBeInTheDocument();
    });
    // Stage A: list only, no separators.
    expect(screen.queryAllByRole('separator')).toHaveLength(0);

    // Stage B: list | waterfall, plus the span-name column splitter.
    fireEvent.click(screen.getByText('create_issue'));
    await waitFor(() => {
      expect(screen.getByText('mcp.client.call_tool')).toBeInTheDocument();
    });
    expect(screen.getAllByRole('separator')).toHaveLength(2);

    // Stage C: waterfall over the span-detail bottom drawer.
    fireEvent.click(screen.getByText('mcp.client.call_tool'));
    await waitFor(() => {
      expect(screen.getByTestId('span-detail')).toBeInTheDocument();
    });
    expect(screen.getAllByRole('separator')).toHaveLength(3);
  });

  it('closes span detail on the first Escape and the waterfall on the second', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('mcp.client.call_tool')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('mcp.client.call_tool'));
    await waitFor(() => {
      expect(screen.getByTestId('span-detail')).toBeInTheDocument();
    });

    fireEvent.keyDown(document.body, { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByTestId('span-detail')).toBeNull();
    });
    // The waterfall survives the first Escape.
    expect(useTracesStore.getState().selectedTraceId).toBe('abc123def4567890');

    fireEvent.keyDown(document.body, { key: 'Escape' });
    await waitFor(() => {
      expect(useTracesStore.getState().selectedTraceId).toBeNull();
    });
  });

  it('toggles list collapse with [ without losing the selected trace', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('mcp.client.call_tool')).toBeInTheDocument();
    });
    const listPanel = screen.getByTestId('trace-list');
    expect(listPanel.style.flexGrow).not.toBe('0');

    fireEvent.keyDown(window, { key: '[' });
    expect(listPanel.style.flexGrow).toBe('0');
    expect(useTracesStore.getState().selectedTraceId).toBe('abc123def4567890');

    fireEvent.keyDown(window, { key: '[' });
    expect(listPanel.style.flexGrow).not.toBe('0');
  });

  it('ignores [ while typing in the search input', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('mcp.client.call_tool')).toBeInTheDocument();
    });
    const listPanel = screen.getByTestId('trace-list');
    fireEvent.keyDown(screen.getByPlaceholderText('Search traces…'), { key: '[' });
    expect(listPanel.style.flexGrow).not.toBe('0');
  });

  it('ignores [ while a modal dialog is open', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('mcp.client.call_tool')).toBeInTheDocument();
    });
    const listPanel = screen.getByTestId('trace-list');
    const dialog = document.createElement('div');
    dialog.setAttribute('role', 'dialog');
    dialog.setAttribute('aria-modal', 'true');
    document.body.appendChild(dialog);
    expect(fireEvent.keyDown(document.body, { key: '[' })).toBe(true);
    expect(listPanel.style.flexGrow).not.toBe('0');
    dialog.remove();
  });

  it('restores a persisted list|waterfall layout from the traces split key', async () => {
    // Drags cannot be simulated in jsdom (no pointer capture or geometry), so
    // assert the restore path instead: a saved layout under the workspace key
    // must drive the mounted panel sizes.
    localStorage.setItem(
      `react-resizable-panels:${workspaceLayoutStorageKey('traces', 'split')}:trace-list:trace-waterfall`,
      JSON.stringify({ 'trace-list': 20, 'trace-waterfall': 80 }),
    );
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('mcp.client.call_tool')).toBeInTheDocument();
    });
    expect(screen.getByTestId('trace-list').style.flexGrow).toBe('20');
    expect(screen.getByTestId('trace-waterfall').style.flexGrow).toBe('80');
  });

  it('shows no skeleton on a pending poll after a completed empty load', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(listEnvelope([]));
    renderAt('/traces');
    expect(await screen.findByText('No traces yet')).toBeInTheDocument();

    vi.mocked(fetchTraces).mockReturnValue(new Promise(() => {}));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    expect(useTracesStore.getState().isLoading).toBe(true);
    expect(screen.queryByTestId('traces-skeleton')).not.toBeInTheDocument();
    expect(screen.getByText('No traces yet')).toBeInTheDocument();
  });

  it('keeps rows under a banner when a poll fails and clears it on the next success', async () => {
    renderAt('/traces');
    expect(await screen.findByText('create_issue')).toBeInTheDocument();

    vi.mocked(fetchTraces).mockRejectedValue(new Error('trace buffer offline'));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('trace buffer offline');
    expect(screen.getByText('create_issue')).toBeInTheDocument();
    expect(screen.queryByText('Traces unavailable')).not.toBeInTheDocument();

    // A pending poll keeps the error up instead of clearing it at start.
    let resolvePoll: (v: ReturnType<typeof listEnvelope>) => void = () => {};
    vi.mocked(fetchTraces).mockReturnValue(new Promise((resolve) => { resolvePoll = resolve; }));
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(useTracesStore.getState().error).toBe('trace buffer offline');
    expect(screen.getByRole('alert')).toBeInTheDocument();

    await act(async () => {
      resolvePoll(listEnvelope([summary]));
    });
    await waitFor(() => expect(screen.queryByRole('alert')).not.toBeInTheDocument());
    expect(useTracesStore.getState().error).toBeNull();
  });

  it('shows the skeleton while the first load is pending', async () => {
    vi.mocked(fetchTraces).mockReturnValue(new Promise(() => {}));
    renderAt('/traces');
    expect(await screen.findByTestId('traces-skeleton')).toBeInTheDocument();
  });

  it('shows the failed state on a first-load failure', async () => {
    vi.mocked(fetchTraces).mockRejectedValue(new Error('trace buffer offline'));
    renderAt('/traces');
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Traces unavailable');
    expect(alert).toHaveTextContent('trace buffer offline');
  });

  it('shows the failed state, not old rows, after a failed server filter change', async () => {
    renderAt('/traces');
    expect(await screen.findByText('create_issue')).toBeInTheDocument();

    vi.mocked(fetchTraces).mockRejectedValue(new Error('filtered read failed'));
    act(() => {
      useTracesStore.getState().setFilters({ server: 'zapier' });
    });
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Traces unavailable');
    expect(screen.queryByText('create_issue')).not.toBeInTheDocument();
    expect(screen.queryByRole('grid', { name: 'Traces' })).not.toBeInTheDocument();
    expect(useTracesStore.getState().traces).toHaveLength(0);
    expect(useTracesStore.getState().loadedFilters).toBeNull();
  });
});

describe('useTracesStore loadTraces', () => {
  it('keeps rows when only a client-side filter changed before a failure', async () => {
    vi.mocked(fetchTraces).mockResolvedValueOnce(listEnvelope([summary]));
    await useTracesStore.getState().loadTraces();
    expect(useTracesStore.getState().loadedFilters).toEqual({ server: '', errorsOnly: false, minDuration: null });

    useTracesStore.getState().setFilters({ search: 'create' });
    vi.mocked(fetchTraces).mockRejectedValueOnce(new Error('boom'));
    await useTracesStore.getState().loadTraces();
    const s = useTracesStore.getState();
    expect(s.traces).toHaveLength(1);
    expect(s.error).toBe('boom');
    expect(s.loadedFilters).not.toBeNull();
  });

  it('drops rows when a server-side filter changed before a failure', async () => {
    vi.mocked(fetchTraces).mockResolvedValueOnce(listEnvelope([summary]));
    await useTracesStore.getState().loadTraces();

    useTracesStore.getState().setFilters({ server: 'zapier' });
    vi.mocked(fetchTraces).mockRejectedValueOnce(new Error('boom'));
    await useTracesStore.getState().loadTraces();
    const s = useTracesStore.getState();
    expect(s.traces).toEqual([]);
    expect(s.total).toBe(0);
    expect(s.loadedFilters).toBeNull();
  });

  it('does not clear the error when a load starts', async () => {
    useTracesStore.setState({ error: 'earlier failure' });
    vi.mocked(fetchTraces).mockReturnValueOnce(new Promise(() => {}));
    void useTracesStore.getState().loadTraces();
    expect(useTracesStore.getState().error).toBe('earlier failure');
    expect(useTracesStore.getState().isLoading).toBe(true);
  });

  it('ignores a late failure from a superseded request', async () => {
    let rejectA: (err: Error) => void = () => {};
    vi.mocked(fetchTraces).mockReturnValueOnce(new Promise((_, reject) => { rejectA = reject; }));
    const loadA = useTracesStore.getState().loadTraces();

    useTracesStore.getState().setFilters({ server: 'github' });
    vi.mocked(fetchTraces).mockResolvedValueOnce(listEnvelope([summary]));
    await useTracesStore.getState().loadTraces();

    rejectA(new Error('stale request failed'));
    await loadA;
    const s = useTracesStore.getState();
    expect(s.traces).toHaveLength(1);
    expect(s.error).toBeNull();
    expect(s.isLoading).toBe(false);
    expect(s.loadedFilters).toEqual({ server: 'github', errorsOnly: false, minDuration: null });
  });

  it('ignores a late success from a superseded request', async () => {
    let resolveA: (value: Awaited<ReturnType<typeof fetchTraces>>) => void = () => {};
    vi.mocked(fetchTraces).mockReturnValueOnce(new Promise((resolve) => { resolveA = resolve; }));
    const loadA = useTracesStore.getState().loadTraces();

    useTracesStore.getState().setFilters({ server: 'github' });
    vi.mocked(fetchTraces).mockResolvedValueOnce(listEnvelope([summary]));
    await useTracesStore.getState().loadTraces();

    resolveA(listEnvelope([summary, infraSummary]));
    await loadA;
    const s = useTracesStore.getState();
    expect(s.traces).toHaveLength(1);
    expect(s.loadedFilters).toEqual({ server: 'github', errorsOnly: false, minDuration: null });
  });
});

function manySummaries(n: number): TraceSummary[] {
  return Array.from({ length: n }, (_, i) => ({
    ...summary,
    traceId: `row${String(i).padStart(13, '0')}`,
  }));
}

function span(over: Partial<Span> & { spanId: string }): Span {
  return {
    parentSpanId: '',
    name: 'child',
    startTime: '2026-07-23T10:00:00.000Z',
    duration: 1,
    status: 'ok',
    attributes: {},
    events: [],
    ...over,
  };
}

describe('TracesWorkspace list window', () => {
  it('discloses the buffer window and loads one more page', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(listEnvelope(manySummaries(100), { bufferSize: 742 }));
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText(/newest 100 of 742 in buffer/)).toBeInTheDocument();
    });
    expect(screen.getByText(/Showing the newest 100 of 742 traces in buffer/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
    await waitFor(() => {
      expect(vi.mocked(fetchTraces)).toHaveBeenLastCalledWith(expect.objectContaining({ limit: 200 }));
    });
  });

  it('shows no window disclosure when the whole buffer is loaded', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(listEnvelope([summary], { bufferSize: 950 }));
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('create_issue')).toBeInTheDocument();
    });
    expect(screen.queryByText(/in buffer/)).toBeNull();
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
  });

  it('uses the loaded wording under a server-side filter', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(listEnvelope(manySummaries(100), { bufferSize: 742 }));
    renderAt('/traces?server=github');
    await waitFor(() => {
      expect(screen.getByText(/newest 100 loaded/)).toBeInTheDocument();
    });
    expect(screen.queryByText(/of 742/)).toBeNull();
    expect(screen.getByText(/Older matching traces may exist/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Load more' })).toBeInTheDocument();
  });

  it('offers no Load more once the loaded rows equal the buffer count', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(listEnvelope(manySummaries(100), { bufferSize: 100 }));
    renderAt('/traces?server=github');
    await waitFor(() => {
      expect(screen.getAllByText('create_issue').length).toBeGreaterThan(0);
    });
    expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull();
    expect(screen.queryByText(/newest 100/)).toBeNull();
  });

  it('says only the loaded traces were searched when a search misses a truncated list', async () => {
    vi.mocked(fetchTraces).mockResolvedValue(listEnvelope(manySummaries(100), { bufferSize: 742 }));
    renderAt('/traces?q=nomatch');
    await waitFor(() => {
      expect(screen.getByText('No traces match your filters')).toBeInTheDocument();
    });
    expect(screen.getByText('Only the newest 100 traces were searched.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Clear filters' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Load more' })).toBeInTheDocument();
  });

  it('discloses the window and offers Load more when every loaded row is infra', async () => {
    const infraRows = Array.from({ length: 100 }, (_, i) => ({
      ...infraSummary,
      traceId: `infra${String(i).padStart(11, '0')}`,
    }));
    vi.mocked(fetchTraces).mockResolvedValue(listEnvelope(infraRows, { bufferSize: 742 }));
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('No tool-call traces yet')).toBeInTheDocument();
    });
    expect(screen.getByText(/Only the newest 100 traces were searched\./)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Show all' })).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Load more' }));
    await waitFor(() => {
      expect(vi.mocked(fetchTraces)).toHaveBeenLastCalledWith(expect.objectContaining({ limit: 200 }));
    });
  });

  it('opens a pasted full trace ID that no loaded row has', async () => {
    const id = 'ABCDEF0123456789ABCDEF0123456789';
    renderAt(`/traces?q=${id}`);
    const open = await screen.findByRole('button', { name: /^Open trace/ });
    expect(open).toHaveTextContent('Open trace abcdef0123456789…');
    fireEvent.click(open);
    await waitFor(() => {
      expect(vi.mocked(fetchTraceDetail)).toHaveBeenCalledWith(id.toLowerCase());
    });
    expect(useTracesStore.getState().selectedTraceId).toBe(id.toLowerCase());
  });

  it('offers Open trace even when the ID matches an infra trace hidden by the segment', async () => {
    const id = 'ffff0000ffff0000ffff0000ffff0000';
    vi.mocked(fetchTraces).mockResolvedValue(listEnvelope([{ ...infraSummary, traceId: id }]));
    renderAt(`/traces?q=${id}`);
    expect(await screen.findByRole('button', { name: /^Open trace/ })).toBeInTheDocument();
  });

  it('offers no Open trace for a partial ID', async () => {
    renderAt('/traces?q=0123456789abcdef');
    await waitFor(() => {
      expect(screen.getByText('No traces match your filters')).toBeInTheDocument();
    });
    expect(screen.queryByRole('button', { name: /^Open trace/ })).toBeNull();
  });
});

describe('TracesWorkspace labels and markers', () => {
  it('puts status in the first cell of each trace row', async () => {
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('create_issue')).toBeInTheDocument();
    });
    const row = document.getElementById('trace-row-abc123def4567890')!;
    expect(row.querySelectorAll('td')[0]).toHaveTextContent('ok');
    const headers = screen.getAllByRole('columnheader');
    expect(headers[0]).toHaveTextContent('Status');
    expect(headers.filter((h) => h.textContent === 'Status')).toHaveLength(1);
  });

  it('renders a shaped status glyph with a name in compact rows', () => {
    const { rerender } = render(
      <table><tbody>
        <TraceRow trace={summary} relativeLabel="now" heat={0} isSelected={false} isHighlighted={false} compact onActivate={() => {}} />
      </tbody></table>,
    );
    const firstCell = document.querySelector('td')!;
    expect(within(firstCell).getByRole('img', { name: 'ok' })).toBeInTheDocument();
    rerender(
      <table><tbody>
        <TraceRow trace={{ ...summary, status: 'error' }} relativeLabel="now" heat={0} isSelected={false} isHighlighted={false} compact onActivate={() => {}} />
      </tbody></table>,
    );
    expect(within(document.querySelector('td')!).getByRole('img', { name: 'error' })).toBeInTheDocument();
  });

  it('names bare tool-call spans and labels serverless spans as gateway', async () => {
    vi.mocked(fetchTraceDetail).mockResolvedValue({
      traceId: 'abc123def4567890',
      spans: [
        span({ spanId: 'r', name: 'tools/call', duration: 5, attributes: { 'gen_ai.tool.name': 'execute' } }),
        span({ spanId: 'c', parentSpanId: 'r', name: 'tools/call create_issue', attributes: { 'server.name': 'github', 'gen_ai.tool.name': 'create_issue' } }),
      ],
    });
    renderAt('/traces?trace=abc123def4567890');
    expect(await screen.findByText('tools/call execute')).toHaveAttribute('title', 'tools/call execute');
    expect(screen.getByText('tools/call create_issue')).toBeInTheDocument();
    expect(screen.getByText('gateway')).toBeInTheDocument();
    expect(screen.getByText('github', { selector: 'span.font-mono' })).toBeInTheDocument();
  });

  it('names the trace error icon and marks error spans without color', async () => {
    vi.mocked(fetchTraceDetail).mockResolvedValue({
      traceId: 'abc123def4567890',
      spans: [span({ spanId: 'r', name: 'root', status: 'error' })],
    });
    renderAt('/traces?trace=abc123def4567890');
    expect(await screen.findByRole('img', { name: 'Trace has errors' })).toBeInTheDocument();
    expect(screen.getByRole('img', { name: 'error' })).toBeInTheDocument();
  });

  it('marks spans above the p95 as slow', async () => {
    const spans = Array.from({ length: 21 }, (_, i) =>
      span({ spanId: `s${i}`, parentSpanId: i === 0 ? '' : 's0', name: i === 20 ? 'slowpoke' : `span${i}`, duration: i === 20 ? 500 : 10 }),
    );
    vi.mocked(fetchTraceDetail).mockResolvedValue({ traceId: 'abc123def4567890', spans });
    renderAt('/traces?trace=abc123def4567890');
    const name = await screen.findByText('slowpoke');
    expect(within(name.parentElement!).getByText('slow')).toBeInTheDocument();
    expect(screen.getAllByText('slow', { selector: 'span.flex-shrink-0' })).toHaveLength(1);
  });

  it('gives the server filter and search input accessible names', async () => {
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('create_issue')).toBeInTheDocument();
    });
    expect(screen.getByLabelText('Filter by server').tagName).toBe('SELECT');
    expect(screen.getByLabelText('Search loaded traces').tagName).toBe('INPUT');
  });

  it('names the span ID copy button', async () => {
    renderAt('/traces?trace=abc123def4567890');
    fireEvent.click(await screen.findByText('mcp.client.call_tool'));
    expect(await screen.findByRole('button', { name: 'Copy span ID' })).toBeInTheDocument();
  });
});

const otherSummary: TraceSummary = {
  ...summary,
  traceId: 'bbbb2222cccc3333',
  rootSpanId: 'x1',
  tool: 'list_issues',
  operation: 'github › list_issues',
};

// Root with two children and a grandchild. Start-time order would put B
// before C; tree order puts C under A.
const treeDetail: TraceDetail = {
  traceId: 'abc123def4567890',
  spans: [
    span({ spanId: 'r', name: 'root.op', startTime: '2026-07-23T10:00:00.000Z', duration: 40 }),
    span({ spanId: 'b', parentSpanId: 'r', name: 'child.b', startTime: '2026-07-23T10:00:00.003Z', duration: 5 }),
    span({ spanId: 'a', parentSpanId: 'r', name: 'child.a', startTime: '2026-07-23T10:00:00.001Z', duration: 35 }),
    span({ spanId: 'c', parentSpanId: 'a', name: 'grandchild.c', startTime: '2026-07-23T10:00:00.004Z', duration: 2 }),
  ],
};

const OPEN_TRACE = 'abc123def4567890';

// Opens the tree trace and moves the list highlight onto the other trace, so
// a key leaking to the list would change aria-activedescendant or, for
// Enter, open a different trace.
async function openTree() {
  vi.mocked(fetchTraces).mockResolvedValue(listEnvelope([summary, otherSummary]));
  vi.mocked(fetchTraceDetail).mockResolvedValue(treeDetail);
  renderAt(`/traces?trace=${OPEN_TRACE}`);
  const tree = await screen.findByRole('tree', { name: 'Spans' });
  await waitFor(() => {
    expect(document.getElementById(`trace-row-${otherSummary.traceId}`)).toBeInTheDocument();
  });
  const grid = screen.getByRole('grid', { name: 'Traces' });
  fireEvent.keyDown(document.body, { key: 'j' });
  expect(grid).toHaveAttribute('aria-activedescendant', `trace-row-${otherSummary.traceId}`);
  return { tree, grid };
}

function row(name: string): HTMLElement {
  return screen.getByRole('treeitem', { name: new RegExp(`^${name.replace('.', '\\.')},`) });
}

function expectNoLeak(grid: HTMLElement) {
  expect(useTracesStore.getState().selectedTraceId).toBe(OPEN_TRACE);
  expect(grid).toHaveAttribute('aria-activedescendant', `trace-row-${otherSummary.traceId}`);
}

describe('TraceWaterfall keyboard access', () => {
  it('exposes the spans as a tree with one tab stop, levels, positions, and names', async () => {
    const { tree } = await openTree();
    const items = within(tree).getAllByRole('treeitem');
    expect(items).toHaveLength(4);
    expect(items.filter((el) => el.getAttribute('tabindex') === '0')).toHaveLength(1);
    expect(items.filter((el) => el.getAttribute('tabindex') === '-1')).toHaveLength(3);
    expect(items[0]).toHaveAttribute('tabindex', '0');
    // Parent-first order, not start-time order.
    expect(items.map((el) => el.dataset.spanId)).toEqual(['r', 'a', 'c', 'b']);
    expect(row('child.a')).toHaveAttribute('aria-level', '2');
    expect(row('grandchild.c')).toHaveAttribute('aria-level', '3');
    expect(row('child.a')).toHaveAttribute('aria-posinset', '1');
    expect(row('child.a')).toHaveAttribute('aria-setsize', '2');
    expect(row('child.b')).toHaveAttribute('aria-posinset', '2');
    expect(row('grandchild.c')).toHaveAttribute('aria-setsize', '1');
    for (const el of items) expect(el).toHaveAttribute('aria-selected', 'false');
    expect(items[1]).toHaveAccessibleName('child.a, 35.0ms, ok');
    expect(items[0]).not.toHaveAttribute('aria-expanded');
    // Inset focus ring, never suppressed.
    expect(items[0].className).toContain('focus-visible:-outline-offset-2');
    expect(items[0].className).not.toContain('outline-none');
    // The existing indentation contract holds.
    expect(screen.getByText('grandchild.c').parentElement).toHaveStyle({ paddingLeft: '24px' });
  });

  it('appends slow to the name of a span above the p95', async () => {
    const spans = Array.from({ length: 21 }, (_, i) =>
      span({ spanId: `s${i}`, parentSpanId: i === 0 ? '' : 's0', name: i === 20 ? 'slowpoke' : `span${i}`, duration: i === 20 ? 500 : 10 }),
    );
    vi.mocked(fetchTraceDetail).mockResolvedValue({ traceId: OPEN_TRACE, spans });
    renderAt(`/traces?trace=${OPEN_TRACE}`);
    await screen.findByText('slowpoke');
    expect(screen.getByRole('treeitem', { name: 'slowpoke, 500.0ms, ok, slow' })).toBeInTheDocument();
  });

  it('moves focus with arrows, j, Home, End, Left, and Right without touching the trace list', async () => {
    const { grid } = await openTree();
    const press = (from: HTMLElement, key: string, to: HTMLElement) => {
      fireEvent.keyDown(from, { key });
      expect(document.activeElement).toBe(to);
      expect(to).toHaveAttribute('tabindex', '0');
      expect(from === to || from.getAttribute('tabindex') === '-1').toBe(true);
      expectNoLeak(grid);
    };
    const root = row('root.op');
    root.focus();
    press(root, 'ArrowDown', row('child.a'));
    press(row('child.a'), 'j', row('grandchild.c'));
    press(row('grandchild.c'), 'k', row('child.a'));
    press(row('child.a'), 'ArrowUp', root);
    // No wrap at the edges, and the key is still consumed.
    press(root, 'ArrowUp', root);
    press(root, 'End', row('child.b'));
    press(row('child.b'), 'ArrowDown', row('child.b'));
    press(row('child.b'), 'Home', root);
    press(root, 'ArrowRight', row('child.a'));
    press(row('child.a'), 'ArrowRight', row('grandchild.c'));
    // A leaf has no first child.
    press(row('grandchild.c'), 'ArrowRight', row('grandchild.c'));
    press(row('grandchild.c'), 'ArrowLeft', row('child.a'));
    press(row('child.a'), 'ArrowLeft', root);
    press(root, 'ArrowLeft', root);
    // Selection never follows focus.
    expect(screen.queryByTestId('span-detail')).toBeNull();
  });

  it('leaves keys with Ctrl, Meta, or Alt to the page', async () => {
    await openTree();
    const root = row('root.op');
    root.focus();
    expect(fireEvent.keyDown(root, { key: 'ArrowDown', ctrlKey: true })).toBe(true);
    expect(document.activeElement).toBe(root);
  });

  it('opens and closes span detail with Enter or Space and keeps focus on the row', async () => {
    const { grid } = await openTree();
    const a = row('child.a');
    a.focus();
    fireEvent.keyDown(a, { key: 'Enter' });
    expect(await screen.findByTestId('span-detail')).toBeInTheDocument();
    expect(a).toHaveAttribute('aria-selected', 'true');
    expect(document.activeElement).toBe(a);
    expectNoLeak(grid);

    fireEvent.keyDown(a, { key: 'Enter' });
    await waitFor(() => expect(screen.queryByTestId('span-detail')).toBeNull());
    expect(a).toHaveAttribute('aria-selected', 'false');

    fireEvent.keyDown(a, { key: ' ' });
    expect(await screen.findByTestId('span-detail')).toBeInTheDocument();
    expect(a).toHaveAttribute('aria-selected', 'true');
    expectNoLeak(grid);
  });

  it('keeps the selected row as the tab stop after a click', async () => {
    await openTree();
    fireEvent.click(screen.getByText('child.b'));
    await screen.findByTestId('span-detail');
    expect(row('child.b')).toHaveAttribute('tabindex', '0');
    expect(row('root.op')).toHaveAttribute('tabindex', '-1');
  });

  it('closes the drawer then the waterfall with Escape on a row, returning focus to the grid', async () => {
    const { grid } = await openTree();
    const a = row('child.a');
    a.focus();
    fireEvent.keyDown(a, { key: 'Enter' });
    await screen.findByTestId('span-detail');

    fireEvent.keyDown(a, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('span-detail')).toBeNull());
    expect(document.activeElement).toBe(a);
    expect(useTracesStore.getState().selectedTraceId).toBe(OPEN_TRACE);

    fireEvent.keyDown(a, { key: 'Escape' });
    await waitFor(() => expect(useTracesStore.getState().selectedTraceId).toBeNull());
    expect(document.activeElement).toBe(grid);
  });

  it('returns focus to the span row on Escape inside span detail, one step per press', async () => {
    await openTree();
    const a = row('child.a');
    a.focus();
    fireEvent.keyDown(a, { key: 'Enter' });
    const close = await screen.findByRole('button', { name: 'Close span detail' });
    close.focus();
    fireEvent.keyDown(close, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('span-detail')).toBeNull());
    expect(document.activeElement).toBe(row('child.a'));
    expect(useTracesStore.getState().selectedTraceId).toBe(OPEN_TRACE);
  });

  it('returns focus to the span row when Close span detail is activated', async () => {
    await openTree();
    fireEvent.click(screen.getByText('child.b'));
    const close = await screen.findByRole('button', { name: 'Close span detail' });
    close.focus();
    fireEvent.click(close);
    await waitFor(() => expect(screen.queryByTestId('span-detail')).toBeNull());
    expect(document.activeElement).toBe(row('child.b'));
  });

  it('returns focus to the span row on Escape from the drawer separator', async () => {
    await openTree();
    fireEvent.click(screen.getByText('child.a'));
    await screen.findByTestId('span-detail');
    const sep = document.getElementById('sep-span-drawer')!;
    expect(sep).toHaveAttribute('tabindex', '0');
    sep.focus();
    fireEvent.keyDown(sep, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('span-detail')).toBeNull());
    expect(document.activeElement).toBe(row('child.a'));
    expect(useTracesStore.getState().selectedTraceId).toBe(OPEN_TRACE);
  });

  it('returns focus to the span row on Shift+Escape inside span detail', async () => {
    await openTree();
    fireEvent.click(screen.getByText('child.a'));
    const close = await screen.findByRole('button', { name: 'Close span detail' });
    close.focus();
    fireEvent.keyDown(close, { key: 'Escape', shiftKey: true });
    await waitFor(() => expect(screen.queryByTestId('span-detail')).toBeNull());
    expect(document.activeElement).toBe(row('child.a'));
    expect(useTracesStore.getState().selectedTraceId).toBe(OPEN_TRACE);
  });

  it('returns focus to the grid on Escape from the list|waterfall separator', async () => {
    const { grid } = await openTree();
    const sep = document.getElementById('sep-trace-list')!;
    expect(sep).toHaveAttribute('tabindex', '0');
    sep.focus();
    fireEvent.keyDown(sep, { key: 'Escape' });
    await waitFor(() => expect(useTracesStore.getState().selectedTraceId).toBeNull());
    expect(document.activeElement).toBe(grid);
  });

  it('stops handled tree and splitter keys from reaching document listeners', async () => {
    await openTree();
    const spy = vi.fn();
    document.addEventListener('keydown', spy);
    try {
      const root = row('root.op');
      root.focus();
      fireEvent.keyDown(root, { key: 'ArrowDown' });
      fireEvent.keyDown(row('child.a'), { key: 'Enter' });
      await screen.findByTestId('span-detail');
      const splitter = screen.getByRole('separator', { name: 'Resize span name column' });
      fireEvent.keyDown(splitter, { key: 'ArrowRight' });
      expect(spy).not.toHaveBeenCalled();
      // Escape on a row is left to the ladder.
      fireEvent.keyDown(row('child.a'), { key: 'Escape' });
      expect(spy).toHaveBeenCalledTimes(1);
    } finally {
      document.removeEventListener('keydown', spy);
    }
  });

  it('returns focus to the grid when Close waterfall is activated from inside it', async () => {
    const { grid } = await openTree();
    const close = screen.getByRole('button', { name: 'Close waterfall' });
    close.focus();
    fireEvent.click(close);
    await waitFor(() => expect(useTracesStore.getState().selectedTraceId).toBeNull());
    expect(document.activeElement).toBe(grid);
  });

  it('moves no focus when the waterfall closes with focus elsewhere', async () => {
    await openTree();
    const search = screen.getByPlaceholderText('Search traces…');
    search.focus();
    fireEvent.keyDown(document.body, { key: 'Escape' });
    await waitFor(() => expect(useTracesStore.getState().selectedTraceId).toBeNull());
    expect(document.activeElement).toBe(search);
  });

  it('makes span detail a focusable region', async () => {
    await openTree();
    fireEvent.click(screen.getByText('child.a'));
    const region = await screen.findByRole('region', { name: 'Span detail' });
    expect(region).toHaveAttribute('tabindex', '0');
    expect(region.className).toContain('focus-visible:-outline-offset-2');
  });

  it('resizes the span name column with arrows, Home, and End within its bounds', async () => {
    const { grid } = await openTree();
    const splitter = screen.getByRole('separator', { name: 'Resize span name column' });
    expect(splitter).toHaveAttribute('tabindex', '0');
    expect(splitter).toHaveAttribute('aria-orientation', 'vertical');
    expect(splitter).toHaveAttribute('aria-valuemin', '18');
    expect(splitter).toHaveAttribute('aria-valuemax', '45');
    expect(splitter).toHaveAttribute('aria-valuenow', '30');
    splitter.focus();
    const pct = () => useUIStore.getState().tracesPrefs.nameColPct;

    fireEvent.keyDown(splitter, { key: 'ArrowRight' });
    expect(pct()).toBe(31);
    expect(splitter).toHaveAttribute('aria-valuenow', '31');
    fireEvent.keyDown(splitter, { key: 'ArrowLeft' });
    expect(pct()).toBe(30);
    fireEvent.keyDown(splitter, { key: 'End' });
    expect(pct()).toBe(45);
    fireEvent.keyDown(splitter, { key: 'ArrowRight' });
    expect(pct()).toBe(45);
    fireEvent.keyDown(splitter, { key: 'Home' });
    expect(pct()).toBe(18);
    fireEvent.keyDown(splitter, { key: 'ArrowLeft' });
    expect(pct()).toBe(18);
    fireEvent.keyDown(splitter, { key: 'ArrowRight', metaKey: true });
    expect(pct()).toBe(18);
    expectNoLeak(grid);
  });
});

describe('TracesWorkspace run and trace links', () => {
  const run: RunRecord = {
    schemaVersion: 1,
    recorderInstanceId: 'r',
    sequence: 1,
    attemptId: 'a1',
    startedAt: '2026-07-23T10:00:00Z',
    returnedAt: '2026-07-23T10:00:00.042Z',
    durationMs: 42,
    requestedName: 'github__create_issue',
    resolvedServer: 'github',
    resolvedTool: 'create_issue',
    disposition: 'completed',
    stage: 'downstream',
    reason: 'ok',
    traceId: 'abc123def4567890',
  };

  beforeEach(() => {
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: (query: string) => ({
        matches: false,
        media: query,
        addEventListener: () => undefined,
        removeEventListener: () => undefined,
        addListener: () => undefined,
        removeListener: () => undefined,
        dispatchEvent: () => false,
      }),
    });
    useRunsStore.setState({
      records: [],
      warnings: [],
      partial: false,
      status: null,
      isLoading: false,
      hasLoaded: false,
      loadedFilters: null,
      error: null,
      errorStatus: null,
      appendFailed: false,
      selectedId: null,
      filters: emptyFilters(),
      nextCursor: null,
      statusError: null,
      mutationError: null,
      lastQuery: null,
      loadSeq: 0,
    });
    vi.mocked(fetchRuns).mockReset();
    vi.mocked(fetchRuns).mockResolvedValue({ records: [run], warnings: [], partial: false, wipeEpoch: 0 });
    vi.mocked(fetchRunsStatus).mockResolvedValue({
      enabled: true,
      effective: true,
      writer_health: 'ok',
      queue_depth: 0,
      queue_capacity: 1024,
      drops: {},
      failures: {},
      historical_loss: 'unknown',
      wipe_epoch: 0,
      recordingNote: 'note',
      privacyNote: 'privacy',
      retentionNote: 'retention',
      inventory: { stack: 'demo', signal: 'runs', path: '/tmp/runs', sizeBytes: 10, fileCount: 1 },
    });
  });

  function renderWithProbe(initialEntry: string) {
    return render(
      <MemoryRouter initialEntries={[initialEntry]}>
        <SearchProbe />
        <Routes>
          <Route path="/traces" element={<TracesWorkspace />} />
          <Route path="/logs" element={<div data-testid="logs-probe" />} />
        </Routes>
      </MemoryRouter>,
    );
  }

  it('counts loaded run records in the header', async () => {
    renderWithProbe('/traces?view=runs');
    await waitFor(() => expect(screen.getByText('1 run record loaded')).toBeInTheDocument());
  });

  it('opens a run trace in the Traces tab through the store', async () => {
    renderWithProbe('/traces?view=runs');
    fireEvent.click(await screen.findByText('github › create_issue'));
    fireEvent.click(screen.getByRole('button', { name: 'View trace' }));
    expect(useTracesStore.getState().selectedTraceId).toBe('abc123def4567890');
    await waitFor(() => expect(screen.getByRole('tab', { name: 'Traces' })).toHaveAttribute('aria-selected', 'true'));
    expect(screen.getByTestId('location-search').textContent).not.toContain('view=');
    await waitFor(() => expect(vi.mocked(fetchTraceDetail)).toHaveBeenCalledWith('abc123def4567890'));
  });

  it('lands on the evicted-trace state when the run trace is gone', async () => {
    vi.mocked(fetchTraceDetail).mockRejectedValue(new Error('trace not found'));
    renderWithProbe('/traces?view=runs');
    fireEvent.click(await screen.findByText('github › create_issue'));
    fireEvent.click(screen.getByRole('button', { name: 'View trace' }));
    await waitFor(() => expect(screen.getByText('Trace no longer in buffer')).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: 'Clear selection' }));
    await waitFor(() => expect(useTracesStore.getState().selectedTraceId).toBeNull());
  });

  it('opens logs for a run trace', async () => {
    renderWithProbe('/traces?view=runs');
    fireEvent.click(await screen.findByText('github › create_issue'));
    fireEvent.click(screen.getByRole('button', { name: 'View logs' }));
    await waitFor(() => expect(screen.getByTestId('logs-probe')).toBeInTheDocument());
    expect(screen.getByTestId('location-search')).toHaveTextContent('?trace=abc123def4567890');
  });

  it('opens the runs of a trace with only the trace filter, even with a Traces server filter', async () => {
    useRunsStore.setState({ selectedId: 'stale', filters: { ...emptyFilters(), tool: 'old' } });
    renderWithProbe('/traces?trace=abc123def4567890&server=github');
    await waitFor(() => expect(useTracesStore.getState().filters.server).toBe('github'));
    const viewRuns = await screen.findByRole('button', { name: 'View runs' });
    expect(viewRuns).toHaveAttribute('title', 'View run records for this trace');
    fireEvent.click(viewRuns);
    await waitFor(() => expect(screen.getByRole('tab', { name: 'Runs' })).toHaveAttribute('aria-selected', 'true'));
    await waitFor(() => expect(fetchRuns).toHaveBeenCalledTimes(1));
    expect(useRunsStore.getState().filters).toEqual({ ...emptyFilters(), trace: 'abc123def4567890' });
    expect(useRunsStore.getState().selectedId).toBeNull();
    expect(vi.mocked(fetchRuns).mock.calls[0][0]?.trace).toBe('abc123def4567890');
    expect(vi.mocked(fetchRuns).mock.calls[0][0]?.server).toBeUndefined();
    const search = screen.getByTestId('location-search').textContent ?? '';
    expect(search).toContain('view=runs');
    expect(search).not.toContain('server=');
  });
});
