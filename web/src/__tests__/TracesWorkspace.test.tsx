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

const summary: TraceSummary = {
  traceId: 'abc123def4567890',
  rootSpanId: 's1',
  operation: 'tools/call create_issue',
  server: 'github',
  startTime: '2026-07-23T10:00:00Z',
  duration: 42,
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
      attributes: { 'server.name': 'github', 'gen_ai.usage.input_tokens': '' },
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
      attributes: { 'server.name': 'github', 'tool.name': 'create_issue' },
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
  };
});

import { fetchTraces, fetchTraceDetail } from '../lib/api';

function server(name: string): MCPServerStatus {
  return { name, transport: 'stdio', initialized: true, tools: [], healthy: true } as unknown as MCPServerStatus;
}

function renderAt(initialEntry: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <Routes>
        <Route path="/traces" element={<TracesWorkspace />} />
        <Route path="/logs" element={<div data-testid="logs-probe" />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.mocked(fetchTraces).mockResolvedValue({ traces: [summary], total: 1 });
  vi.mocked(fetchTraceDetail).mockResolvedValue(detail);
  useStackStore.setState({ mcpServers: [server('github')] });
  useUIStore.setState({ compactMode: { ...COMPACT_MODE_DEFAULTS }, tracesDetached: false });
  useTracesStore.setState({
    traces: [],
    total: 0,
    isLoading: false,
    error: null,
    filters: { server: '', errorsOnly: false, minDuration: null, search: '' },
    selectedTraceId: null,
    traceDetail: null,
    isLoadingDetail: false,
    detailError: null,
  });
});

describe('TracesWorkspace', () => {
  it('shows the global trace list without any selection', async () => {
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('tools/call create_issue')).toBeInTheDocument();
    });
    expect(vi.mocked(fetchTraces)).toHaveBeenCalled();
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

  it('drops empty-string attributes from the span detail', async () => {
    renderAt('/traces?trace=abc123def4567890');
    await waitFor(() => {
      expect(screen.getByText('github › create_issue')).toBeInTheDocument();
    });
    // Root span carries one real attribute and one empty gen_ai counter.
    fireEvent.click(screen.getByText('github › create_issue'));
    await waitFor(() => {
      expect(screen.getByText('Attributes (1)')).toBeInTheDocument();
    });
    expect(screen.queryByText('gen_ai.usage.input_tokens')).toBeNull();
  });

  it('mirrors a row selection into the URL', async () => {
    renderAt('/traces');
    await waitFor(() => {
      expect(screen.getByText('tools/call create_issue')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('tools/call create_issue'));
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
});
