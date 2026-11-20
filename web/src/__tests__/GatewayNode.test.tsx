import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router';
import '@testing-library/jest-dom';

// React Flow primitives are mocked the same way the other node-component tests
// do, so the node can render outside a real <ReactFlow> provider.
vi.mock('@xyflow/react', () => ({
  Handle: ({ id }: { id: string }) => <div data-testid={`handle-${id}`} />,
  Position: { Left: 'left', Right: 'right' },
}));

import GatewayNode from '../components/graph/GatewayNode';
import { AccessLens } from '../components/stack/AccessLens';
import { useUIStore } from '../stores/useUIStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';
import { useStackStore } from '../stores/useStackStore';
import type { ClientStatus, GatewayNodeData } from '../types';

const baseData: GatewayNodeData = {
  type: 'gateway',
  name: 'acme-stack',
  version: 'v9.9.9',
  serverCount: 2,
  resourceCount: 0,
  clientCount: 0,
  totalToolCount: 3,
  sessions: 0,
  codeMode: 'on',
  toolDiscovery: null,
  totalSkills: 0,
  activeSkills: 0,
};

function renderNode(data: GatewayNodeData = baseData) {
  return render(<GatewayNode data={data} />);
}

describe('GatewayNode', () => {
  // The node is the primary surface for the gateway's identity now that the
  // header no longer carries the version.
  it('renders the gateway name and version', () => {
    renderNode();
    expect(screen.getByText('acme-stack')).toBeInTheDocument();
    expect(screen.getByText('v9.9.9')).toBeInTheDocument();
  });

  it('renders Code Mode as read-only status, not an action', () => {
    renderNode();
    const codeMode = screen.getByText('Code Mode').closest('[role="status"]');
    expect(codeMode).not.toBeNull();
    // A status pill must not carry action affordances.
    expect(codeMode).not.toHaveAttribute('onclick');
    expect(codeMode!.tagName).toBe('SPAN');
    expect(codeMode!.className).not.toMatch(/cursor-pointer/);
    expect(codeMode!.className).not.toMatch(/hover:/);
  });

  it('omits the Code Mode pill when code mode is off', () => {
    renderNode({ ...baseData, codeMode: 'off' });
    expect(screen.queryByText('Code Mode')).not.toBeInTheDocument();
  });

  it('renders the deferred discovery badge from data.toolDiscovery', () => {
    renderNode({ ...baseData, toolDiscovery: 'deferred' });
    const badge = screen.getByText('Deferred discovery').closest('[role="status"]');
    expect(badge).not.toBeNull();
    expect(badge!.tagName).toBe('SPAN');
  });

  it('omits the deferred discovery badge when toolDiscovery is null', () => {
    renderNode({ ...baseData, toolDiscovery: null });
    expect(screen.queryByText('Deferred discovery')).not.toBeInTheDocument();
  });

  it('keeps Gateway Active as a read-only status pill', () => {
    renderNode();
    const active = screen.getByText('Gateway Active').closest('[role="status"]');
    expect(active).not.toBeNull();
    expect(active!.className).not.toMatch(/cursor-pointer/);
  });
});

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}</div>;
}

// The Skills row navigates in-shell, so it needs a router; the cases above
// render with no skills and stay router-free.
function renderWithRouter(data: GatewayNodeData, extra?: React.ReactNode) {
  return render(
    <MemoryRouter initialEntries={['/stack']}>
      <GatewayNode data={data} />
      {extra}
      <Routes>
        <Route path="*" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  );
}

const withSkills: GatewayNodeData = { ...baseData, totalSkills: 4, activeSkills: 2 };

describe('GatewayNode Skills row', () => {
  beforeEach(() => {
    useAccessLensStore.getState().clearDraft();
    useAccessLensStore.setState({ enabled: false, isSaving: false, exitNavTarget: null });
    useUIStore.setState({ registryDetached: false, sidebarOpen: true });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    useAccessLensStore.getState().clearDraft();
    useAccessLensStore.setState({ enabled: false, exitNavTarget: null });
  });

  it('renders the Skills row as a labeled button', () => {
    renderWithRouter(withSkills);
    const button = screen.getByRole('button', { name: 'Open skills in the Library' });
    expect(button.tagName).toBe('BUTTON');
    expect(button).toHaveTextContent('Skills');
    expect(button).toHaveTextContent('2/4');
    expect(button).toHaveClass('nodrag');
  });

  it('navigates to the Library workspace in-shell without opening a window', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    renderWithRouter(withSkills);

    fireEvent.click(screen.getByRole('button', { name: 'Open skills in the Library' }));

    expect(screen.getByTestId('location')).toHaveTextContent('/library');
    expect(open).not.toHaveBeenCalled();
    expect(useUIStore.getState().registryDetached).toBe(false);
    expect(useUIStore.getState().sidebarOpen).toBe(true);
  });

  it('routes through the Access Lens guard when the draft is dirty', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    useAccessLensStore.setState({ enabled: true, clientSlug: 'x', draft: ['a'], baseline: [] });
    renderWithRouter(withSkills);

    fireEvent.click(screen.getByRole('button', { name: 'Open skills in the Library' }));

    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    expect(useAccessLensStore.getState().exitNavTarget).toBe('/library');
    expect(open).not.toHaveBeenCalled();
  });

  it('navigates to the Library once the dirty draft is discarded', () => {
    const cursor: ClientStatus = {
      name: 'Cursor',
      slug: 'cursor',
      detected: true,
      linked: true,
      transport: 'native HTTP',
      effectiveScope: { configured: true, unscoped: false, servers: ['github'], tools: ['github__search'] },
    };
    useStackStore.setState({ clients: [cursor], selectedNodeId: null });
    renderWithRouter(
      withSkills,
      <AccessLens
        servers={[
          { name: 'github', transport: 'http', initialized: true, toolCount: 1, tools: ['search'] },
          { name: 'gitlab', transport: 'http', initialized: true, toolCount: 1, tools: ['list'] },
        ]}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Toggle Access Lens' }));
    act(() => {
      useStackStore.setState({ selectedNodeId: 'client-cursor' });
    });
    act(() => {
      useAccessLensStore.getState().toggleServer('gitlab');
    });

    fireEvent.click(screen.getByRole('button', { name: 'Open skills in the Library' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    expect(screen.getByText(/Leave Access Lens and discard unsaved access changes/i)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Discard changes' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/library');
    expect(useAccessLensStore.getState().exitNavTarget).toBeNull();
  });
});
