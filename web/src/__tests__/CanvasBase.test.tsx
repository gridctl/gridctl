import { describe, it, expect, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
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
});
