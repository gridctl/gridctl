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
    fetchSecurityReport: vi.fn().mockResolvedValue({ checks: [] }),
    fetchOptimizeReport: vi.fn().mockResolvedValue({ findings: [], health_score: 0, generated_at: '2026-09-13T00:00:00Z' }),
    fetchPolicy: vi.fn().mockResolvedValue({ configured: false, rules: [], tools: {} }),
  };
});

import { Sidebar } from '../components/layout/Sidebar';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';

const serverNode: Node = {
  id: 'mcp-vault',
  position: { x: 0, y: 0 },
  data: {
    type: 'mcp-server',
    name: 'vault',
    transport: 'stdio',
    initialized: true,
    toolCount: 2,
    tools: ['list_paths', 'read_secret'],
    status: 'running',
  },
};

const gatewayNode: Node = {
  id: 'gateway',
  position: { x: 0, y: 0 },
  data: { type: 'gateway', name: 'gw', version: '0.0.0' },
};

function renderInspector() {
  return render(
    <MemoryRouter>
      <aside aria-label="Stack inspector">
        <Sidebar />
      </aside>
    </MemoryRouter>,
  );
}

function addNodeWrapper(id: string): HTMLElement {
  const el = document.createElement('div');
  el.className = 'react-flow__node';
  el.setAttribute('data-id', id);
  el.tabIndex = 0;
  document.body.appendChild(el);
  return el;
}

describe('Stack inspector focus', () => {
  beforeEach(() => {
    useStackStore.setState({
      selectedNodeId: null,
      nodes: [serverNode, gatewayNode],
      mcpServers: [],
      clients: [],
    });
    useUIStore.setState({ sidebarOpen: true, inspectorFocusRequest: null });
  });

  afterEach(() => {
    cleanup();
    document.body.innerHTML = '';
  });

  it('focuses the server heading for a pending request and clears it', () => {
    useStackStore.setState({ selectedNodeId: 'mcp-vault' });
    useUIStore.setState({ inspectorFocusRequest: 'mcp-vault' });
    renderInspector();
    expect(document.activeElement).toBe(screen.getByRole('heading', { name: 'vault' }));
    expect(screen.getByRole('heading', { name: 'vault' })).toHaveAttribute('tabindex', '-1');
    expect(useUIStore.getState().inspectorFocusRequest).toBeNull();
  });

  it('focuses the gateway heading for a pending request and clears it', () => {
    useStackStore.setState({ selectedNodeId: 'gateway' });
    useUIStore.setState({ inspectorFocusRequest: 'gateway' });
    renderInspector();
    expect(document.activeElement).toBe(screen.getByRole('heading', { name: 'gw' }));
    expect(useUIStore.getState().inspectorFocusRequest).toBeNull();
  });

  it('does not move focus without a request', () => {
    const outside = document.createElement('button');
    document.body.appendChild(outside);
    outside.focus();
    useStackStore.setState({ selectedNodeId: 'mcp-vault' });
    renderInspector();
    expect(document.activeElement).toBe(outside);
  });

  it('clears a request that matches no node, and it does not carry over to a later mouse selection', () => {
    const outside = document.createElement('button');
    document.body.appendChild(outside);
    outside.focus();
    useStackStore.setState({ selectedNodeId: 'mcp-missing' });
    useUIStore.setState({ inspectorFocusRequest: 'mcp-missing' });
    renderInspector();
    expect(useUIStore.getState().inspectorFocusRequest).toBeNull();
    expect(document.activeElement).toBe(outside);

    act(() => useStackStore.getState().selectNode('mcp-vault'));
    expect(screen.getByRole('heading', { name: 'vault' })).toBeInTheDocument();
    expect(document.activeElement).toBe(outside);
  });

  it('clears a request for a different node than the selection without focusing', () => {
    const outside = document.createElement('button');
    document.body.appendChild(outside);
    outside.focus();
    useStackStore.setState({ selectedNodeId: 'mcp-vault' });
    useUIStore.setState({ inspectorFocusRequest: 'gateway' });
    renderInspector();
    expect(useUIStore.getState().inspectorFocusRequest).toBeNull();
    expect(document.activeElement).toBe(outside);
  });

  it('returns focus to the node when closed with focus inside the inspector', () => {
    const wrapper = addNodeWrapper('mcp-vault');
    useStackStore.setState({ selectedNodeId: 'mcp-vault' });
    renderInspector();
    const close = screen.getByRole('button', { name: 'Close inspector' });
    close.focus();
    fireEvent.click(close);
    expect(document.activeElement).toBe(wrapper);
    expect(useUIStore.getState().sidebarOpen).toBe(false);
    expect(useStackStore.getState().selectedNodeId).toBeNull();
  });

  it('returns focus to the gateway node when the gateway sidebar closes', () => {
    const wrapper = addNodeWrapper('gateway');
    useStackStore.setState({ selectedNodeId: 'gateway' });
    renderInspector();
    const close = screen.getByRole('button', { name: 'Close gateway sidebar' });
    expect(close).toHaveAttribute('type', 'button');
    close.focus();
    fireEvent.click(close);
    expect(document.activeElement).toBe(wrapper);
  });

  it('leaves focus on an unrelated element outside the inspector when closing', () => {
    addNodeWrapper('mcp-vault');
    const outside = document.createElement('button');
    document.body.appendChild(outside);
    useStackStore.setState({ selectedNodeId: 'mcp-vault' });
    renderInspector();
    outside.focus();
    fireEvent.click(screen.getByRole('button', { name: 'Close inspector' }));
    expect(document.activeElement).toBe(outside);
  });

  it('does nothing on close when the node no longer exists', () => {
    const other = addNodeWrapper('gateway');
    useStackStore.setState({ selectedNodeId: 'mcp-vault' });
    renderInspector();
    const close = screen.getByRole('button', { name: 'Close inspector' });
    close.focus();
    expect(() => fireEvent.click(close)).not.toThrow();
    expect(useUIStore.getState().sidebarOpen).toBe(false);
    expect(document.activeElement).not.toBe(other);
  });

  it('keeps collapsed section controls out of the inspector', () => {
    useStackStore.setState({ selectedNodeId: 'mcp-vault' });
    renderInspector();
    const availability = screen.getByRole('button', { name: /availability/i });
    expect(availability).toHaveAttribute('aria-expanded', 'false');
    const panelId = availability.getAttribute('aria-controls') as string;
    expect(document.getElementById(panelId)).toBeEmptyDOMElement();
    expect(screen.queryByRole('button', { name: /disable/i })).not.toBeInTheDocument();
  });
});
