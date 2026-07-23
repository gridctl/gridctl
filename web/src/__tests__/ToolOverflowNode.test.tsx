import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter } from 'react-router-dom';

vi.mock('@xyflow/react', () => ({
  Handle: ({ id }: { id: string }) => <div data-testid={`handle-${id}`} />,
  Position: { Left: 'left', Right: 'right' },
  MarkerType: { ArrowClosed: 'arrowclosed' },
  // The detail popover pans the viewport when it would overrun the canvas.
  useReactFlow: () => ({ getViewport: () => ({ x: 0, y: 0, zoom: 1 }), setViewport: vi.fn() }),
}));

vi.mock('../lib/api', () => ({
  fetchToolUsage: vi.fn().mockResolvedValue({ servers: {} }),
}));

import ToolOverflowNode from '../components/graph/ToolOverflowNode';
import { fetchToolUsage } from '../lib/api';
import { useStackStore } from '../stores/useStackStore';
import type { ToolOverflowNodeData } from '../types';

const data: ToolOverflowNodeData = {
  type: 'tool-overflow',
  serverName: 'github',
  serverNodeId: 'mcp-github',
  overflowCount: 2,
  hiddenTools: ['create-issue', 'delete-repo'],
};

function renderNode(nodeData: ToolOverflowNodeData = data) {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <ToolOverflowNode data={nodeData} />
    </MemoryRouter>,
  );
}

function makeData(count: number): ToolOverflowNodeData {
  const hiddenTools = Array.from({ length: count }, (_, i) => `tool-${i}`);
  return { ...data, overflowCount: count, hiddenTools };
}

beforeEach(() => {
  useStackStore.setState({
    toolCatalog: [
      {
        name: 'github__create-issue',
        description: 'Open a new issue',
        inputSchema: { type: 'object' },
      },
    ],
  });
  (fetchToolUsage as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({ servers: {} });
});

describe('ToolOverflowNode', () => {
  it('renders the overflow count', () => {
    renderNode();
    expect(screen.getByText('+2 more')).toBeInTheDocument();
  });

  it('reveals the hidden tools when the list is opened', () => {
    renderNode();
    fireEvent.click(screen.getByRole('button', { name: /show 2 more github tools/i }));
    expect(screen.getByText('create-issue')).toBeInTheDocument();
    expect(screen.getByText('delete-repo')).toBeInTheDocument();
  });

  it('opens the shared detail popover for a hidden tool', async () => {
    renderNode();
    fireEvent.click(screen.getByRole('button', { name: /show 2 more github tools/i }));
    fireEvent.click(screen.getByRole('button', { name: /show details for github tool create-issue/i }));
    expect(await screen.findByText('github__create-issue')).toBeInTheDocument();
    expect(screen.getByText('Open a new issue')).toBeInTheDocument();
  });

  it('shows empty-state detail for a hidden tool missing from the catalog', async () => {
    renderNode();
    fireEvent.click(screen.getByRole('button', { name: /show 2 more github tools/i }));
    fireEvent.click(screen.getByRole('button', { name: /show details for github tool delete-repo/i }));
    expect(await screen.findByText('github__delete-repo')).toBeInTheDocument();
    expect(screen.getByText(/No description available/i)).toBeInTheDocument();
  });

  it('marks the open panel scrollable inside react-flow', () => {
    const { container } = renderNode();
    fireEvent.click(screen.getByRole('button', { name: /show 2 more github tools/i }));
    // node-scroll wins overflow back from the react-flow overflow:visible
    // !important rule; nowheel keeps the wheel from zooming the canvas.
    const panel = container.querySelector('.node-scroll');
    expect(panel).not.toBeNull();
    expect(panel).toHaveClass('nowheel');
  });

  it('lays hidden tools out in columns of 10', () => {
    const { container } = renderNode(makeData(30));
    fireEvent.click(screen.getByRole('button', { name: /show 30 more github tools/i }));
    const list = container.querySelector('ul');
    expect(list).toHaveStyle({
      gridTemplateRows: 'repeat(10, min-content)',
      gridTemplateColumns: 'repeat(3, minmax(0, 1fr))',
    });
    const panel = container.querySelector('.node-scroll');
    expect(panel).toHaveStyle({ width: '600px' });
  });

  it('caps the grid at four columns and grows rows past 40 tools', () => {
    const { container } = renderNode(makeData(100));
    fireEvent.click(screen.getByRole('button', { name: /show 100 more github tools/i }));
    const list = container.querySelector('ul');
    expect(list).toHaveStyle({
      gridTemplateRows: 'repeat(25, min-content)',
      gridTemplateColumns: 'repeat(4, minmax(0, 1fr))',
    });
  });

  it('opens the detail popover past the panel edge so the tool list stays visible', async () => {
    const { container } = renderNode();
    fireEvent.click(screen.getByRole('button', { name: /show 2 more github tools/i }));
    fireEvent.click(screen.getByRole('button', { name: /show details for github tool create-issue/i }));
    const card = (await screen.findByText('github__create-issue')).closest('.w-72');
    // One column of hidden tools -> the panel is 200px wide; the card starts
    // just past its right edge instead of the pill default (left-full), which
    // would cover the panel.
    expect(card).toHaveStyle({ left: '208px', top: '100%' });
    expect(container.querySelector('.left-full')).toBeNull();
  });

  it('marks the detail popover scroller scrollable inside react-flow', async () => {
    const { container } = renderNode();
    fireEvent.click(screen.getByRole('button', { name: /show 2 more github tools/i }));
    fireEvent.click(screen.getByRole('button', { name: /show details for github tool create-issue/i }));
    expect(await screen.findByText('github__create-issue')).toBeInTheDocument();
    const scrollers = container.querySelectorAll('.node-scroll.nowheel');
    // The overflow list panel plus the detail popover's body.
    expect(scrollers.length).toBe(2);
  });

  it('dismisses overlays on Escape', async () => {
    renderNode();
    fireEvent.click(screen.getByRole('button', { name: /show 2 more github tools/i }));
    fireEvent.click(screen.getByRole('button', { name: /show details for github tool create-issue/i }));
    expect(await screen.findByText('github__create-issue')).toBeInTheDocument();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByText('github__create-issue')).not.toBeInTheDocument();
    expect(screen.queryByText('create-issue')).not.toBeInTheDocument();
  });
});
