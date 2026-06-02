import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, useLocation } from 'react-router-dom';

// React Flow primitives are mocked the same way the other node-component tests
// do, so the pill can render outside a real <ReactFlow> provider.
vi.mock('@xyflow/react', () => ({
  Handle: ({ id }: { id: string }) => <div data-testid={`handle-${id}`} />,
  Position: { Left: 'left', Right: 'right' },
  MarkerType: { ArrowClosed: 'arrowclosed' },
}));

// Usage is fetched best-effort when the popover opens; mock the one-shot.
vi.mock('../lib/api', () => ({
  fetchToolUsage: vi.fn().mockResolvedValue({ servers: {} }),
}));

import ToolNode from '../components/graph/ToolNode';
import { fetchToolUsage } from '../lib/api';
import { useStackStore } from '../stores/useStackStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import type { MCPServerStatus, ToolNodeData } from '../types';

function LocationProbe() {
  const loc = useLocation();
  return <div data-testid="location">{loc.pathname + loc.search}</div>;
}

const data: ToolNodeData = {
  type: 'tool',
  name: 'search-repos',
  serverName: 'github',
  serverNodeId: 'mcp-github',
};

function renderNode(nodeData: ToolNodeData = data) {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <ToolNode data={nodeData} />
      <LocationProbe />
    </MemoryRouter>,
  );
}

const trigger = () => screen.getByRole('button', { name: /show details for github tool search-repos/i });

beforeEach(() => {
  useStackStore.setState({
    toolCatalog: [
      {
        name: 'github__search-repos',
        description: 'Search repositories',
        inputSchema: { type: 'object', properties: { q: { type: 'string' } } },
      },
    ],
    // Default to no selection so the lens stays inactive for the base tests.
    selectedNodeId: null,
  });
  // Reset the Access Lens so the default tests render in inspect-only mode.
  useAccessLensStore.getState().clearDraft();
  useAccessLensStore.setState({ enabled: false });
  (fetchToolUsage as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({ servers: {} });
});

describe('ToolNode', () => {
  it('renders the unprefixed tool name', () => {
    renderNode();
    expect(screen.getByText('search-repos')).toBeInTheDocument();
  });

  it('exposes a keyboard-activatable trigger with aria-expanded', () => {
    renderNode();
    expect(trigger()).toHaveAttribute('aria-expanded', 'false');
  });

  it('opens a detail popover on click with the prefixed name and description', async () => {
    renderNode();
    fireEvent.click(trigger());
    expect(await screen.findByText('github__search-repos')).toBeInTheDocument();
    expect(screen.getByText('Search repositories')).toBeInTheDocument();
    expect(trigger()).toHaveAttribute('aria-expanded', 'true');
  });

  it('closes on re-click', async () => {
    renderNode();
    fireEvent.click(trigger());
    expect(await screen.findByText('github__search-repos')).toBeInTheDocument();
    fireEvent.click(trigger());
    expect(screen.queryByText('github__search-repos')).not.toBeInTheDocument();
  });

  it('closes on Escape', async () => {
    renderNode();
    fireEvent.click(trigger());
    expect(await screen.findByText('github__search-repos')).toBeInTheDocument();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByText('github__search-repos')).not.toBeInTheDocument();
  });

  it('renders empty states when the tool is absent from the catalog', async () => {
    useStackStore.setState({ toolCatalog: [] });
    renderNode();
    fireEvent.click(trigger());
    expect(await screen.findByText(/No description available/i)).toBeInTheDocument();
  });

  it('copies the prefixed name to the clipboard', async () => {
    const writeText = vi.fn();
    Object.assign(navigator, { clipboard: { writeText } });
    renderNode();
    fireEvent.click(trigger());
    fireEvent.click(await screen.findByRole('button', { name: /copy name/i }));
    expect(writeText).toHaveBeenCalledWith('github__search-repos');
  });

  it('deep-links to the Tools workspace and closes', async () => {
    renderNode();
    fireEvent.click(trigger());
    fireEvent.click(await screen.findByRole('button', { name: /open in tools/i }));
    expect(screen.getByTestId('location')).toHaveTextContent('/tools?server=github&q=search-repos');
    expect(screen.queryByText('github__search-repos')).not.toBeInTheDocument();
  });

  it('shows a best-effort usage line when usage data is available', async () => {
    (fetchToolUsage as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      servers: { github: { 'search-repos': { calls: 3, lastCalledAt: new Date().toISOString() } } },
    });
    renderNode();
    fireEvent.click(trigger());
    expect(await screen.findByText(/Last used/i)).toBeInTheDocument();
  });

  it('omits the usage line when no usage data exists', async () => {
    renderNode();
    fireEvent.click(trigger());
    // Wait for the popover (and its usage fetch) to settle, then assert absence.
    await screen.findByText('github__search-repos');
    expect(screen.queryByText(/Last used/i)).not.toBeInTheDocument();
  });
});

describe('ToolNode (Access Lens edit mode)', () => {
  const GITHUB: MCPServerStatus = {
    name: 'github',
    transport: 'http',
    initialized: true,
    toolCount: 2,
    tools: ['search-repos', 'create-issue'],
  };

  // Put the lens in edit mode for the github server of the selected client.
  function enterEditMode() {
    useStackStore.setState({ mcpServers: [GITHUB], selectedNodeId: 'client-gemini' });
    useAccessLensStore.getState().seed({
      slug: 'gemini',
      name: 'Gemini',
      baseline: ['github'],
      savedTools: [],
      createsBlock: false,
      serverTools: { github: ['search-repos', 'create-issue'] },
    });
    useAccessLensStore.setState({ enabled: true });
  }

  it('renders the pill as a checked toggle (server is All by default)', () => {
    enterEditMode();
    renderNode();
    const toggle = screen.getByRole('checkbox', { name: /revoke github tool search-repos/i });
    expect(toggle).toHaveAttribute('aria-checked', 'true');
  });

  it('clicking the pill removes just that tool (all minus one)', () => {
    enterEditMode();
    renderNode();
    fireEvent.click(screen.getByRole('checkbox', { name: /revoke github tool search-repos/i }));
    const s = useAccessLensStore.getState();
    expect(s.toolMode.github).toBe('custom');
    // search-repos removed; the other tool survives.
    expect(s.customSel.github).toEqual(['create-issue']);
  });

  it('keeps an info button for inspecting while editing', async () => {
    enterEditMode();
    renderNode();
    fireEvent.click(screen.getByRole('button', { name: /show details for github tool search-repos/i }));
    expect(await screen.findByText('github__search-repos')).toBeInTheDocument();
  });
});
