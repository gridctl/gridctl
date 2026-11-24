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
import { useNavigationGuardStore } from '../stores/useNavigationGuardStore';
import { NavigationGuardDialog } from '../components/shell/NavigationGuardDialog';
import { WorkspaceSwitcher } from '../components/shell/WorkspaceSwitcher';
import type { ClientStatus, GatewayNodeData, MCPServerStatus } from '../types';

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

const CURSOR: ClientStatus = {
  name: 'Cursor',
  slug: 'cursor',
  detected: true,
  linked: true,
  transport: 'native HTTP',
  effectiveScope: { configured: true, unscoped: false, servers: ['github'], tools: ['github__search'] },
};

const LENS_SERVERS: MCPServerStatus[] = [
  { name: 'github', transport: 'http', initialized: true, toolCount: 2, tools: ['search', 'create'] },
  { name: 'gitlab', transport: 'http', initialized: true, toolCount: 1, tools: ['list'] },
];

function resetGuards() {
  useNavigationGuardStore.setState({ guards: [], pending: null });
}

// Mounts the lens, the shared confirm host, and the workspace pills, then
// opens a draft for Cursor.
function renderDirtyLens(edit: () => void) {
  useStackStore.setState({ clients: [CURSOR], selectedNodeId: null });
  renderWithRouter(
    withSkills,
    <>
      <AccessLens servers={LENS_SERVERS} />
      <WorkspaceSwitcher />
      <NavigationGuardDialog />
    </>,
  );
  fireEvent.click(screen.getByRole('button', { name: 'Toggle Access Lens' }));
  act(() => {
    useStackStore.setState({ selectedNodeId: 'client-cursor' });
  });
  act(edit);
}

describe('GatewayNode Skills row', () => {
  beforeEach(() => {
    useAccessLensStore.getState().clearDraft();
    useAccessLensStore.setState({ enabled: false, isSaving: false });
    useUIStore.setState({ registryDetached: false, sidebarOpen: true });
    resetGuards();
  });

  afterEach(() => {
    vi.restoreAllMocks();
    useAccessLensStore.getState().clearDraft();
    useAccessLensStore.setState({ enabled: false });
    resetGuards();
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

  it('raises the shared confirm with a dirty lens and keeps the draft on cancel', () => {
    const open = vi.spyOn(window, 'open').mockReturnValue(null);
    renderDirtyLens(() => useAccessLensStore.getState().toggleServer('gitlab'));

    fireEvent.click(screen.getByRole('button', { name: 'Open skills in the Library' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    expect(screen.getByRole('alertdialog')).toHaveTextContent('Access Lens for Cursor');
    expect(open).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    expect(useAccessLensStore.getState().enabled).toBe(true);
    expect(useAccessLensStore.getState().draft).toContain('gitlab');
  });

  it('clears the draft, turns the lens off, and lands on the Library once confirmed', () => {
    renderDirtyLens(() => useAccessLensStore.getState().toggleServer('gitlab'));

    fireEvent.click(screen.getByRole('button', { name: 'Open skills in the Library' }));
    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));

    expect(screen.getByTestId('location')).toHaveTextContent('/library');
    expect(useAccessLensStore.getState().enabled).toBe(false);
    expect(useAccessLensStore.getState().clientSlug).toBeNull();
    expect(useNavigationGuardStore.getState().guards).toHaveLength(0);
  });

  it('blocks a tool-only edit on the Skills row and on a pill', () => {
    renderDirtyLens(() => useAccessLensStore.getState().toggleTool('github', 'create'));

    fireEvent.click(screen.getByRole('button', { name: 'Open skills in the Library' }));
    expect(screen.getByRole('alertdialog')).toHaveTextContent('Access Lens for Cursor');
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    fireEvent.click(screen.getByRole('link', { name: 'Metrics' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    expect(screen.getByRole('alertdialog')).toHaveTextContent('Access Lens for Cursor');
  });

  it('does not block a navigation that stays in Stack', () => {
    renderDirtyLens(() => useAccessLensStore.getState().toggleServer('gitlab'));

    const { isBlocked } = useNavigationGuardStore.getState();
    expect(isBlocked('/stack?spec=1')).toBe(false);
    expect(isBlocked('/library')).toBe(true);
  });

  it('leaves the lens alone when Escape cancels the shared confirm', () => {
    renderDirtyLens(() => useAccessLensStore.getState().toggleServer('gitlab'));

    fireEvent.click(screen.getByRole('button', { name: 'Open skills in the Library' }));
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();

    fireEvent.keyDown(document, { key: 'Escape' });

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.queryByText(/discard unsaved access changes/i)).not.toBeInTheDocument();
    expect(useAccessLensStore.getState().enabled).toBe(true);
    expect(useAccessLensStore.getState().draft).toContain('gitlab');
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
  });
});
