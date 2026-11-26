import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, cleanup, fireEvent, render, screen, within } from '@testing-library/react';
import { useState } from 'react';
import type { Node } from '@xyflow/react';
import { StackToolbar } from '../components/stack/StackToolbar';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import { useCanvasActionsStore } from '../stores/useCanvasActionsStore';
import type { StackFacet } from '../lib/stackRows';

function server(name: string, data: Record<string, unknown> = {}): Node {
  return { id: `mcp-${name}`, position: { x: 0, y: 0 }, data: { type: 'mcp-server', name, status: 'running', ...data } };
}

const baseNodes: Node[] = [
  { id: 'gateway', position: { x: 0, y: 0 }, data: { type: 'gateway', name: 'gw' } },
  server('alpha'),
  server('beta', { status: 'error' }),
  server('gamma', { pinStatus: 'drift' }),
  { id: 'client-claude', position: { x: 0, y: 0 }, data: { type: 'client', name: 'Claude', status: 'running' } },
];

const onFocusTable = vi.fn();

function Harness({ view = 'graph' as 'graph' | 'table' }) {
  const [query, setQuery] = useState('');
  const [facets, setFacets] = useState<Set<StackFacet>>(new Set());
  return (
    <>
      <input aria-label="Other field" />
      <div role="dialog" aria-label="Some dialog">
        <button type="button">Inside dialog</button>
      </div>
      <StackToolbar
        filter={{ query, facets }}
        view={view}
        onQueryChange={setQuery}
        onFacetsChange={setFacets}
        onFocusTable={onFocusTable}
      />
    </>
  );
}

const input = () => screen.getByRole('textbox', { name: 'Filter stack by name' });

beforeEach(() => {
  cleanup();
  onFocusTable.mockClear();
  useStackStore.setState({ nodes: baseNodes });
  useUIStore.setState({ stackPrefs: { view: null } });
  useAccessLensStore.setState({ enabled: false });
  useCanvasActionsStore.setState({ fitView: vi.fn() });
});

describe('StackToolbar', () => {
  it('types into the filter and shows the match count only while active', () => {
    render(<Harness />);
    expect(input()).toHaveAttribute('placeholder', 'Filter by name');
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
    fireEvent.change(input(), { target: { value: 'al' } });
    expect(input()).toHaveValue('al');
    expect(screen.getByRole('status')).toHaveTextContent('1 of 4');
    fireEvent.click(screen.getByRole('button', { name: 'Clear name filter' }));
    expect(input()).toHaveValue('');
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('focuses the filter on / outside fields and dialogs', () => {
    render(<Harness />);
    fireEvent.keyDown(screen.getByRole('textbox', { name: 'Other field' }), { key: '/' });
    expect(input()).not.toHaveFocus();
    fireEvent.keyDown(screen.getByRole('button', { name: 'Inside dialog' }), { key: '/' });
    expect(input()).not.toHaveFocus();
    fireEvent.keyDown(document.body, { key: '/', ctrlKey: true });
    expect(input()).not.toHaveFocus();
    fireEvent.keyDown(document.body, { key: '/' });
    expect(input()).toHaveFocus();
  });

  it('removes the / listener on unmount', () => {
    const { unmount } = render(<Harness />);
    unmount();
    const event = new KeyboardEvent('keydown', { key: '/', cancelable: true, bubbles: true });
    document.body.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(false);
  });

  it('clears on Escape, then blurs', () => {
    render(<Harness />);
    act(() => input().focus());
    fireEvent.change(input(), { target: { value: 'x' } });
    fireEvent.keyDown(input(), { key: 'Escape' });
    expect(input()).toHaveValue('');
    expect(input()).toHaveFocus();
    fireEvent.keyDown(input(), { key: 'Escape' });
    expect(input()).not.toHaveFocus();
  });

  it('shows the attention chip only with attention rows and toggles the facet', () => {
    const { unmount } = render(<Harness />);
    const chip = screen.getByRole('button', { name: /Needs attention/ });
    expect(chip).toHaveTextContent('2');
    expect(chip).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(chip);
    expect(chip).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('status')).toHaveTextContent('2 of 4');
    unmount();

    useStackStore.setState({ nodes: [server('alpha')] });
    render(<Harness />);
    expect(screen.queryByRole('button', { name: /Needs attention/ })).not.toBeInTheDocument();
  });

  it('lists only present facets with counts and clears them', () => {
    render(<Harness />);
    const trigger = screen.getByRole('button', { name: /^Status/ });
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    fireEvent.click(trigger);
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    const menu = screen.getByRole('dialog', { name: 'Status filter' });
    const labels = within(menu).getAllByRole('checkbox').map((box) => box.closest('label')?.textContent);
    expect(labels).toEqual(['Needs attention2', 'Running2', 'Error1', 'Linked1', 'Pin drift1']);

    fireEvent.click(within(menu).getByRole('checkbox', { name: /Linked/ }));
    fireEvent.click(within(menu).getByRole('checkbox', { name: /Error/ }));
    expect(trigger).toHaveTextContent('2');
    expect(screen.getByRole('status')).toHaveTextContent('2 of 4');
    fireEvent.click(within(menu).getByRole('button', { name: 'Clear' }));
    expect(within(menu).getAllByRole('checkbox').every((box) => !(box as HTMLInputElement).checked)).toBe(true);
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('writes the view preference from the toggle', () => {
    render(<Harness />);
    const group = screen.getByRole('group', { name: 'View mode' });
    expect(within(group).getByRole('button', { name: 'Graph view' })).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(within(group).getByRole('button', { name: 'Table view' }));
    expect(useUIStore.getState().stackPrefs.view).toBe('table');
    fireEvent.click(within(group).getByRole('button', { name: 'Graph view' }));
    expect(useUIStore.getState().stackPrefs.view).toBe('graph');
  });

  it('disables the Table button while Access Lens is enabled', () => {
    useAccessLensStore.setState({ enabled: true });
    render(<Harness />);
    const table = screen.getByRole('button', { name: 'Table view' });
    expect(table).toBeDisabled();
    expect(table).toHaveAttribute('title', 'Close Access Lens to use the table view');
  });

  it('frames the matches on Enter in graph view', () => {
    render(<Harness />);
    const fitView = useCanvasActionsStore.getState().fitView as ReturnType<typeof vi.fn>;
    fireEvent.keyDown(input(), { key: 'Enter' });
    expect(fitView).not.toHaveBeenCalled();
    fireEvent.change(input(), { target: { value: 'a' } });
    fireEvent.keyDown(input(), { key: 'Enter' });
    expect(fitView).toHaveBeenCalledWith({
      nodes: [{ id: 'mcp-alpha' }, { id: 'mcp-beta' }, { id: 'mcp-gamma' }, { id: 'client-claude' }],
      padding: 0.25,
      duration: 300,
      maxZoom: 1.5,
    });
    fitView.mockClear();
    fireEvent.change(input(), { target: { value: 'zzz' } });
    fireEvent.keyDown(input(), { key: 'Enter' });
    expect(fitView).not.toHaveBeenCalled();
  });

  it('does nothing on Enter in table view and moves to the table on ArrowDown', () => {
    render(<Harness view="table" />);
    const fitView = useCanvasActionsStore.getState().fitView as ReturnType<typeof vi.fn>;
    fireEvent.change(input(), { target: { value: 'a' } });
    fireEvent.keyDown(input(), { key: 'Enter' });
    expect(fitView).not.toHaveBeenCalled();
    fireEvent.keyDown(input(), { key: 'ArrowDown' });
    expect(onFocusTable).toHaveBeenCalledTimes(1);
  });
});
