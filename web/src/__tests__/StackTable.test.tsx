import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import type { Node } from '@xyflow/react';
import { createRef } from 'react';
import { StackTable, type StackTableHandle } from '../components/stack/StackTable';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import { useInspectorToolsGuardStore } from '../stores/useInspectorToolsGuardStore';
import { EMPTY_STACK_FILTER, type StackFacet, type StackFilter } from '../lib/stackRows';

function server(name: string, data: Record<string, unknown> = {}): Node {
  return {
    id: `mcp-${name}`,
    position: { x: 0, y: 0 },
    data: { type: 'mcp-server', name, status: 'running', transport: 'http', toolCount: 2, ...data },
  };
}

const gateway: Node = { id: 'gateway', position: { x: 0, y: 0 }, data: { type: 'gateway', name: 'gw', version: '1.2.3' } };
const resource: Node = { id: 'resource-db', position: { x: 0, y: 0 }, data: { type: 'resource', name: 'db', status: 'stopped' } };
const client: Node = { id: 'client-claude', position: { x: 0, y: 0 }, data: { type: 'client', name: 'Claude', status: 'running', transport: 'http' } };

function setNodes(nodes: Node[]) {
  act(() => {
    useStackStore.setState({ nodes });
  });
}

function filterOf(query = '', facets: StackFacet[] = []): StackFilter {
  return { query, facets: new Set(facets) };
}

function nameButtons() {
  const body = screen.getAllByRole('rowgroup')[1];
  return within(body).getAllByRole('button');
}

function names() {
  return nameButtons().map((b) => b.textContent);
}

beforeEach(() => {
  cleanup();
  useStackStore.setState({
    nodes: [
      gateway,
      server('alpha'),
      server('beta', { status: 'error' }),
      server('gamma', { autoscale: { current: 1, target: 3 }, pinStatus: 'drift' }),
      resource,
      client,
    ],
    gatewayInfo: { name: 'gw', version: '1.2.3' },
    selectedNodeId: null,
  });
  useUIStore.setState({ sidebarOpen: false });
  useInspectorToolsGuardStore.setState({ dirtyServer: null, pendingLeave: null });
});

describe('StackTable', () => {
  it('renders the seven headers and one row per server, resource, and client', () => {
    render(<StackTable filter={EMPTY_STACK_FILTER} onClearFilter={() => {}} />);
    expect(screen.getAllByRole('columnheader').map((h) => h.textContent?.replace(/[↑↓]/g, ''))).toEqual([
      'Name', 'Kind', 'Status', 'Transport', 'Tools', 'Replicas', 'Pin',
    ]);
    expect(screen.getByText('Stack contents')).toBeInTheDocument();
    // Status ascending by default: attention first, linked client last.
    expect(names()).toEqual(['beta', 'gamma', 'db', 'alpha', 'Claude']);
    expect(screen.getByLabelText('1 of target 3 replicas')).toHaveTextContent('1/3');
    expect(screen.getByText('Drift')).toBeInTheDocument();
    expect(screen.getByText(/3 MCP servers · 1 resource · 1 client/)).toBeInTheDocument();
  });

  it('moves and flips aria-sort on header clicks', () => {
    render(<StackTable filter={EMPTY_STACK_FILTER} onClearFilter={() => {}} />);
    const header = (label: string) => screen.getByRole('columnheader', { name: new RegExp(`^${label}`) });
    expect(header('Status')).toHaveAttribute('aria-sort', 'ascending');
    expect(header('Name')).not.toHaveAttribute('aria-sort');

    fireEvent.click(within(header('Name')).getByRole('button'));
    expect(header('Name')).toHaveAttribute('aria-sort', 'ascending');
    expect(header('Status')).not.toHaveAttribute('aria-sort');
    expect(names()).toEqual(['alpha', 'beta', 'Claude', 'db', 'gamma']);

    fireEvent.click(within(header('Name')).getByRole('button'));
    expect(header('Name')).toHaveAttribute('aria-sort', 'descending');
    expect(names()).toEqual(['gamma', 'db', 'Claude', 'beta', 'alpha']);
  });

  it('selects the node and opens the inspector on a row click, once', () => {
    const selectNode = vi.spyOn(useStackStore.getState(), 'selectNode');
    render(<StackTable filter={EMPTY_STACK_FILTER} onClearFilter={() => {}} />);
    fireEvent.click(screen.getByRole('button', { name: 'alpha' }));
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-alpha');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(screen.getByRole('button', { name: 'alpha' }).closest('tr')).toHaveAttribute('aria-current', 'true');
    selectNode.mockRestore();

    fireEvent.click(screen.getByText('Stopped').closest('tr') as HTMLElement);
    expect(useStackStore.getState().selectedNodeId).toBe('resource-db');
  });

  it('keeps one tab stop and moves it with the keys, clamping at the ends', () => {
    render(<StackTable filter={EMPTY_STACK_FILTER} onClearFilter={() => {}} />);
    const stops = () => nameButtons().filter((b) => b.tabIndex === 0).map((b) => b.textContent);
    expect(stops()).toEqual(['beta']);

    const press = (key: string) => fireEvent.keyDown(document.activeElement as Element, { key });
    act(() => nameButtons()[0].focus());
    press('ArrowDown');
    expect(document.activeElement).toHaveTextContent('gamma');
    press('j');
    expect(document.activeElement).toHaveTextContent('db');
    press('k');
    expect(document.activeElement).toHaveTextContent('gamma');
    press('ArrowUp');
    press('ArrowUp');
    expect(document.activeElement).toHaveTextContent('beta');
    press('End');
    expect(document.activeElement).toHaveTextContent('Claude');
    press('ArrowDown');
    expect(document.activeElement).toHaveTextContent('Claude');
    press('Home');
    expect(document.activeElement).toHaveTextContent('beta');
    press('PageDown');
    expect(document.activeElement).toHaveTextContent('Claude');
    press('PageUp');
    expect(document.activeElement).toHaveTextContent('beta');
    expect(stops()).toEqual(['beta']);
    // Movement with the inspector closed does not select.
    expect(useStackStore.getState().selectedNodeId).toBeNull();
  });

  it('ignores movement keys with a modifier held', () => {
    render(<StackTable filter={EMPTY_STACK_FILTER} onClearFilter={() => {}} />);
    act(() => nameButtons()[0].focus());
    fireEvent.keyDown(document.activeElement as Element, { key: 'ArrowDown', metaKey: true });
    expect(document.activeElement).toHaveTextContent('beta');
  });

  it('changes the inspected item while the inspector is open', () => {
    render(<StackTable filter={EMPTY_STACK_FILTER} onClearFilter={() => {}} />);
    fireEvent.click(screen.getByRole('button', { name: 'beta' }));
    act(() => screen.getByRole('button', { name: 'beta' }).focus());
    fireEvent.keyDown(document.activeElement as Element, { key: 'ArrowDown' });
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-gamma');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });

  it('keeps focus on the same server when a store update re-sorts rows', () => {
    render(<StackTable filter={EMPTY_STACK_FILTER} onClearFilter={() => {}} />);
    act(() => screen.getByRole('button', { name: 'alpha' }).focus());
    const nodes = useStackStore.getState().nodes.map((n) =>
      n.id === 'mcp-alpha' ? { ...n, data: { ...n.data, status: 'error' } } : n,
    );
    setNodes(nodes);
    expect(names()[0]).toBe('alpha');
    expect(document.activeElement).toHaveTextContent('alpha');
    expect(nameButtons().filter((b) => b.tabIndex === 0).map((b) => b.textContent)).toEqual(['alpha']);
  });

  it('moves the tab stop to an outside selection without moving focus', () => {
    const scroll = vi.fn();
    Element.prototype.scrollIntoView = scroll;
    render(<StackTable filter={EMPTY_STACK_FILTER} onClearFilter={() => {}} />);
    act(() => useStackStore.getState().selectNode('mcp-alpha'));
    expect(screen.getByRole('button', { name: 'alpha' }).tabIndex).toBe(0);
    expect(document.activeElement).toBe(document.body);
    expect(scroll).toHaveBeenCalledWith({ block: 'nearest' });
    delete (Element.prototype as { scrollIntoView?: unknown }).scrollIntoView;
  });

  it('focuses the active row through its handle', () => {
    const ref = createRef<StackTableHandle>();
    render(<StackTable ref={ref} filter={EMPTY_STACK_FILTER} onClearFilter={() => {}} />);
    act(() => ref.current?.focusActiveRow());
    expect(document.activeElement).toHaveTextContent('beta');
  });

  it('filters rows and offers Clear filters when nothing matches', () => {
    const onClear = vi.fn();
    const { rerender } = render(<StackTable filter={filterOf('', ['linked'])} onClearFilter={onClear} />);
    expect(names()).toEqual(['Claude']);
    rerender(<StackTable filter={filterOf('nothing')} onClearFilter={onClear} />);
    expect(screen.getByText('No matches')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it('renders the plain empty state with no rows', () => {
    setNodes([gateway]);
    render(<StackTable filter={EMPTY_STACK_FILTER} onClearFilter={() => {}} />);
    expect(screen.getByText('No MCP servers, resources, or clients')).toBeInTheDocument();
    expect(screen.queryByRole('table')).not.toBeInTheDocument();
  });

  it('inspects the gateway', () => {
    render(<StackTable filter={EMPTY_STACK_FILTER} onClearFilter={() => {}} />);
    const button = screen.getByRole('button', { name: 'Inspect gateway' });
    expect(button).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(button);
    expect(useStackStore.getState().selectedNodeId).toBe('gateway');
    expect(useUIStore.getState().sidebarOpen).toBe(true);
    expect(button).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByText('gw')).toBeInTheDocument();
  });

  it('labels every row state as text', () => {
    const states = ['running', 'stopped', 'error', 'initializing', 'idle', 'needs-auth', 'restarting'];
    setNodes([
      ...states.map((status) => server(status, { status })),
      server('off', { enabled: false }),
      client,
    ]);
    render(<StackTable filter={EMPTY_STACK_FILTER} onClearFilter={() => {}} />);
    for (const label of ['Running', 'Stopped', 'Error', 'Initializing', 'Idle', 'Needs authorization', 'Restarting', 'Disabled', 'Linked']) {
      expect(screen.getAllByRole('cell').some((c) => c.textContent === label), label).toBe(true);
    }
  });
});
