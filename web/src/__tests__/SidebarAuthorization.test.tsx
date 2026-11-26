import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import type { Node } from '@xyflow/react';

vi.mock('../hooks/useWindowManager', () => ({
  useWindowManager: () => ({ openDetachedWindow: vi.fn(), focusDetachedWindow: vi.fn() }),
}));

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchAuthServers: vi.fn().mockResolvedValue([]),
    fetchSecurityReport: vi.fn().mockResolvedValue({ checks: [] }),
    fetchOptimizeReport: vi.fn().mockResolvedValue({ findings: [], health_score: 0, generated_at: '2026-09-13T00:00:00Z' }),
    fetchPolicy: vi.fn().mockResolvedValue({ configured: false, rules: [], tools: {} }),
  };
});

import { Sidebar } from '../components/layout/Sidebar';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import type { MCPServerStatus } from '../types';

type AuthStatus = MCPServerStatus['authStatus'];

function serverNode(name: string, authStatus: AuthStatus): Node {
  return {
    id: `mcp-${name}`,
    position: { x: 0, y: 0 },
    data: {
      type: 'mcp-server',
      name,
      transport: 'http',
      initialized: true,
      toolCount: 0,
      tools: [],
      external: true,
      status: 'running',
      authStatus,
    },
  };
}

function serverStatus(name: string, authStatus: AuthStatus): MCPServerStatus {
  return { name, transport: 'http', initialized: true, toolCount: 0, tools: [], external: true, authStatus };
}

function renderInspector() {
  return render(
    <MemoryRouter>
      <Sidebar />
    </MemoryRouter>,
  );
}

function authorizationToggle(): HTMLElement {
  return screen.getByRole('button', { name: /^Authorization$/ });
}

beforeEach(() => {
  useStackStore.setState({
    selectedNodeId: null,
    missingSelection: null,
    nodes: [serverNode('a', 'authorized'), serverNode('b', 'needs_auth')],
    mcpServers: [serverStatus('a', 'authorized'), serverStatus('b', 'needs_auth')],
    clients: [],
  });
  useUIStore.setState({ sidebarOpen: true, inspectorFocusRequest: null });
});

afterEach(() => {
  cleanup();
});

describe('Stack inspector Authorization section', () => {
  it('opens for a needs-auth server selected after another OAuth server', () => {
    useStackStore.setState({ selectedNodeId: 'mcp-a' });
    renderInspector();
    expect(authorizationToggle()).toHaveAttribute('aria-expanded', 'false');

    act(() => useStackStore.setState({ selectedNodeId: 'mcp-b' }));
    expect(authorizationToggle()).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByLabelText('Authorization for b')).toBeVisible();
    expect(screen.getByRole('button', { name: /Authorize/ })).toBeVisible();
  });

  it('opens when the selected server moves from authorized to needs_auth', () => {
    useStackStore.setState({ selectedNodeId: 'mcp-a' });
    renderInspector();
    expect(authorizationToggle()).toHaveAttribute('aria-expanded', 'false');

    act(() =>
      useStackStore.setState({
        mcpServers: [serverStatus('a', 'needs_auth'), serverStatus('b', 'needs_auth')],
      }),
    );
    expect(authorizationToggle()).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByLabelText('Authorization for a')).toBeVisible();
  });

  it('stays open and keeps its content when authorization completes', () => {
    useStackStore.setState({ selectedNodeId: 'mcp-b' });
    renderInspector();
    const section = screen.getByLabelText('Authorization for b');
    expect(authorizationToggle()).toHaveAttribute('aria-expanded', 'true');

    act(() =>
      useStackStore.setState({
        mcpServers: [serverStatus('a', 'authorized'), serverStatus('b', 'authorized')],
      }),
    );
    expect(authorizationToggle()).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByLabelText('Authorization for b')).toBe(section);
  });
});

describe('Stack inspector missing selection', () => {
  it('shows a closable empty state naming the missing node', () => {
    useStackStore.setState({
      selectedNodeId: 'mcp-gone',
      nodes: [],
      missingSelection: { id: 'mcp-gone', name: 'gone', since: Date.now() },
    });
    renderInspector();
    expect(screen.getByText('gone is not in the stack')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Close inspector' }));
    expect(useStackStore.getState().selectedNodeId).toBeNull();
    expect(useUIStore.getState().sidebarOpen).toBe(false);
  });

  it('uses a generic title when the name is unknown', () => {
    useStackStore.setState({ selectedNodeId: 'mcp-gone', nodes: [] });
    renderInspector();
    expect(screen.getByText('This node is not in the stack')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Close inspector' })).toBeInTheDocument();
  });

  it('renders nothing without a selection', () => {
    useStackStore.setState({ selectedNodeId: null, nodes: [] });
    const { container } = renderInspector();
    expect(container).toBeEmptyDOMElement();
  });
});
