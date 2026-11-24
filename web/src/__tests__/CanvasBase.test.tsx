import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ReactFlowProvider } from '@xyflow/react';
import { CanvasBase } from '../components/canvas/CanvasBase';

// ResizeObserver isn't implemented in jsdom; React Flow needs it.
class ResizeObserverPolyfill {
  observe() {}
  unobserve() {}
  disconnect() {}
}
// eslint-disable-next-line @typescript-eslint/no-explicit-any
(globalThis as any).ResizeObserver ??= ResizeObserverPolyfill;

describe('CanvasBase', () => {
  it('renders ReactFlow with the supplied nodes', () => {
    const nodes = [{ id: 'a', position: { x: 0, y: 0 }, data: { label: 'A' } }];
    const { container } = render(
      <ReactFlowProvider>
        <CanvasBase nodes={nodes} edges={[]} />
      </ReactFlowProvider>,
    );
    // ReactFlow renders a wrapper with the .react-flow class.
    expect(container.querySelector('.react-flow')).toBeInTheDocument();
  });

  it('renders children inside the React Flow container (e.g. workspace controls)', () => {
    render(
      <ReactFlowProvider>
        <CanvasBase nodes={[]} edges={[]}>
          <div data-testid="control-slot">Controls</div>
        </CanvasBase>
      </ReactFlowProvider>,
    );
    expect(screen.getByTestId('control-slot')).toBeInTheDocument();
  });

  it('exposes aria-label and tabindex on the node wrapper and forwards onKeyDown', () => {
    const onKeyDown = vi.fn();
    const nodes = [{
      id: 'mcp-github',
      position: { x: 0, y: 0 },
      data: { label: 'github' },
      ariaLabel: 'github, MCP server, running',
    }];
    const { container } = render(
      <ReactFlowProvider>
        <CanvasBase nodes={nodes} edges={[]} onKeyDown={onKeyDown} />
      </ReactFlowProvider>,
    );
    const wrapper = container.querySelector('.react-flow__node');
    expect(wrapper).not.toBeNull();
    expect(wrapper).toHaveAttribute('aria-label', 'github, MCP server, running');
    expect(wrapper).toHaveAttribute('tabindex', '0');
    fireEvent.keyDown(wrapper as Element, { key: 'Enter' });
    expect(onKeyDown).toHaveBeenCalled();
  });

  it('forwards onPaneClick to the underlying React Flow', () => {
    const onPaneClick = vi.fn();
    const { container } = render(
      <ReactFlowProvider>
        <CanvasBase nodes={[]} edges={[]} onPaneClick={onPaneClick} />
      </ReactFlowProvider>,
    );
    const pane = container.querySelector('.react-flow__pane');
    expect(pane).toBeInTheDocument();
    if (pane) {
      fireEvent.click(pane);
      expect(onPaneClick).toHaveBeenCalled();
    }
  });

  it('sends no remove change on Backspace or Delete when deleteKeyCode is null', async () => {
    const onNodesChange = vi.fn();
    const onEdgesChange = vi.fn();
    const nodes = [
      { id: 'a', position: { x: 0, y: 0 }, data: { label: 'A' }, selected: true },
      { id: 'b', position: { x: 200, y: 0 }, data: { label: 'B' } },
    ];
    const edges = [{ id: 'a-b', source: 'a', target: 'b', selected: true }];
    render(
      <ReactFlowProvider>
        <CanvasBase
          nodes={nodes}
          edges={edges}
          deleteKeyCode={null}
          edgesFocusable={false}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
        />
      </ReactFlowProvider>,
    );
    for (const key of ['Backspace', 'Delete']) {
      fireEvent.keyDown(document, { key, code: key });
      fireEvent.keyUp(document, { key, code: key });
    }
    // React Flow deletes asynchronously; let any pending deletion settle.
    await new Promise((resolve) => setTimeout(resolve, 0));
    const calls = [...onNodesChange.mock.calls, ...onEdgesChange.mock.calls];
    for (const [changes] of calls) {
      expect((changes as Array<{ type: string }>).some((c) => c.type === 'remove')).toBe(false);
    }
  });

  it('deletes on Backspace by default, so the null opt-out is meaningful', async () => {
    const onNodesChange = vi.fn();
    const nodes = [{ id: 'a', position: { x: 0, y: 0 }, data: { label: 'A' }, selected: true }];
    render(
      <ReactFlowProvider>
        <CanvasBase nodes={nodes} edges={[]} onNodesChange={onNodesChange} />
      </ReactFlowProvider>,
    );
    fireEvent.keyDown(document, { key: 'Backspace', code: 'Backspace' });
    fireEvent.keyUp(document, { key: 'Backspace', code: 'Backspace' });
    await waitFor(() => {
      const removed = onNodesChange.mock.calls.some(([changes]) =>
        (changes as Array<{ type: string }>).some((c) => c.type === 'remove'),
      );
      expect(removed).toBe(true);
    });
  });
});
