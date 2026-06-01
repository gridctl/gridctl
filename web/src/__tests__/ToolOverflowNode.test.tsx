import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter } from 'react-router-dom';

vi.mock('@xyflow/react', () => ({
  Handle: ({ id }: { id: string }) => <div data-testid={`handle-${id}`} />,
  Position: { Left: 'left', Right: 'right' },
  MarkerType: { ArrowClosed: 'arrowclosed' },
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

function renderNode() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <ToolOverflowNode data={data} />
    </MemoryRouter>,
  );
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
