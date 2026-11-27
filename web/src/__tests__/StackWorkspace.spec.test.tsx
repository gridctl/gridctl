import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router';
import { StackWorkspace, clampSidebarWidth, SPEC_MAX, SPEC_MIN } from '../components/workspaces/StackWorkspace';
import { useUIStore } from '../stores/useUIStore';
import { useStackStore } from '../stores/useStackStore';
import { useAccessLensStore } from '../stores/useAccessLensStore';

vi.mock('../components/graph/Canvas', () => ({ Canvas: () => <div data-testid="canvas" /> }));
vi.mock('../components/layout/Sidebar', () => ({ Sidebar: () => <div data-testid="sidebar" /> }));
vi.mock('../components/stack/AccessLens', () => ({ AccessLens: () => null }));
vi.mock('../components/spec/SpecTab', () => ({ SpecTab: () => <div data-testid="spec-tab" /> }));
vi.mock('../hooks/useNodeParamSync', () => ({ useNodeParamSync: () => {} }));
vi.mock('../hooks/usePolling', () => ({
  usePolling: () => ({ refresh: async () => {} }),
  usePollingRefresh: () => async () => {},
}));

function Location() {
  const location = useLocation();
  return <div data-testid="location">{location.search}</div>;
}

function renderAt(search: string) {
  return render(
    <MemoryRouter initialEntries={[`/stack${search}`]}>
      <StackWorkspace />
      <Location />
    </MemoryRouter>,
  );
}

function grid(): HTMLElement {
  return screen.getByTestId('stack-grid');
}

beforeEach(() => {
  cleanup();
  Element.prototype.setPointerCapture = vi.fn();
  Element.prototype.releasePointerCapture = vi.fn();
  Element.prototype.hasPointerCapture = vi.fn(() => true);
  useUIStore.setState({ sidebarOpen: false });
  useStackStore.setState({ selectedNodeId: null });
  useAccessLensStore.setState({ slideOverOpen: false });
});

describe('clampSidebarWidth with the spec bounds', () => {
  it('clamps to the spec constants and the 40 to 60 percent rule', () => {
    expect(clampSidebarWidth(2000, 0, SPEC_MIN, SPEC_MAX)).toBe(960);
    expect(clampSidebarWidth(100, 0, SPEC_MIN, SPEC_MAX)).toBe(360);
    expect(clampSidebarWidth(2000, 1000, SPEC_MIN, SPEC_MAX)).toBe(600);
    expect(clampSidebarWidth(2000, 2000, SPEC_MIN, SPEC_MAX)).toBe(960);
  });
});

describe('StackWorkspace spec column', () => {
  it('renders the spec in the right track at the default width, outside the canvas column', () => {
    renderAt('?spec=1');
    const pane = screen.getByRole('complementary', { name: 'Stack spec' });
    expect(pane.parentElement).toBe(grid());
    expect(screen.getByTestId('canvas').closest('aside')).toBeNull();
    expect(grid().style.gridTemplateColumns).toBe('minmax(0, 1fr) min(560px, 60%)');
  });

  it('resizes the spec column within its bounds and keeps the inspector width separate', () => {
    renderAt('?spec=1');
    const handle = screen.getByTestId('spec-tab').parentElement?.parentElement?.previousElementSibling as HTMLElement;
    fireEvent.pointerDown(handle, { pointerId: 1, button: 0, clientX: 500 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientX: 400 });
    fireEvent.pointerUp(handle, { pointerId: 1 });
    expect(grid().style.gridTemplateColumns).toBe('minmax(0, 1fr) min(660px, 60%)');

    fireEvent.pointerDown(handle, { pointerId: 1, button: 0, clientX: 2000 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientX: 0 });
    fireEvent.pointerUp(handle, { pointerId: 1 });
    expect(grid().style.gridTemplateColumns).toBe('minmax(0, 1fr) min(960px, 60%)');

    act(() => useUIStore.getState().setSidebarOpen(true));
    expect(grid().style.gridTemplateColumns).toBe('minmax(0, 1fr) min(320px, 60%)');
  });

  it('closes the inspector and the access editor when the spec opens, keeping the selection', () => {
    useUIStore.setState({ sidebarOpen: true });
    useStackStore.setState({ selectedNodeId: 'mcp-github' });
    useAccessLensStore.setState({ slideOverOpen: true });
    renderAt('?spec=1');
    expect(useUIStore.getState().sidebarOpen).toBe(false);
    expect(useAccessLensStore.getState().slideOverOpen).toBe(false);
    expect(useStackStore.getState().selectedNodeId).toBe('mcp-github');
    expect(screen.getByRole('complementary', { name: 'Stack spec' })).toBeInTheDocument();
    expect(screen.queryByRole('complementary', { name: 'Stack inspector' })).toBeNull();
    expect(screen.getByTestId('location')).toHaveTextContent('?spec=1');
  });

  it('closes the spec when the inspector opens', () => {
    renderAt('?spec=1');
    act(() => useUIStore.getState().setSidebarOpen(true));
    expect(screen.getByTestId('location')).not.toHaveTextContent('spec=1');
    expect(screen.queryByRole('complementary', { name: 'Stack spec' })).toBeNull();
    expect(screen.getByRole('complementary', { name: 'Stack inspector' })).toBeInTheDocument();
  });

  it('closes the spec when the access editor opens', () => {
    renderAt('?spec=1');
    act(() => useAccessLensStore.getState().openSlideOver());
    expect(screen.getByTestId('location')).not.toHaveTextContent('spec=1');
    expect(screen.queryByRole('complementary', { name: 'Stack spec' })).toBeNull();
  });

  it('removes the flag from the close button and keeps other parameters', () => {
    renderAt('?spec=1&node=mcp-github');
    fireEvent.click(screen.getByRole('button', { name: 'Close spec pane' }));
    expect(screen.getByTestId('location')).toHaveTextContent('?node=mcp-github');
    expect(grid().style.gridTemplateColumns).toBe('minmax(0, 1fr) 0px');
  });
});
