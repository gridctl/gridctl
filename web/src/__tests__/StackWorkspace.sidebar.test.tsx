import { beforeEach, describe, expect, it, vi } from 'vitest';
import '@testing-library/jest-dom';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { StackWorkspace, clampSidebarWidth } from '../components/workspaces/StackWorkspace';
import { useUIStore } from '../stores/useUIStore';

vi.mock('../components/graph/Canvas', () => ({ Canvas: () => <div data-testid="canvas" /> }));
vi.mock('../components/layout/Sidebar', () => ({ Sidebar: () => <div data-testid="sidebar" /> }));
vi.mock('../components/stack/AccessLens', () => ({ AccessLens: () => null }));
vi.mock('../components/spec/SpecPane', () => ({ SpecPane: () => null }));
vi.mock('../hooks/usePolling', () => ({
  usePolling: () => ({ refresh: async () => {} }),
  usePollingRefresh: () => async () => {},
}));

beforeEach(() => {
  cleanup();
  Element.prototype.setPointerCapture = vi.fn();
  Element.prototype.releasePointerCapture = vi.fn();
  Element.prototype.hasPointerCapture = vi.fn(() => true);
  useUIStore.setState({ sidebarOpen: true });
});

describe('clampSidebarWidth', () => {
  it('uses 40 to 60 percent below the constant floors and the constants once the container is wide', () => {
    expect(clampSidebarWidth(320, 390)).toBe(234);
    expect(clampSidebarWidth(10, 390)).toBe(156);
    expect(clampSidebarWidth(10, 768)).toBe(280);
    expect(clampSidebarWidth(900, 768)).toBe(460);
    expect(clampSidebarWidth(10, 1000)).toBe(280);
    expect(clampSidebarWidth(900, 1000)).toBe(600);
    expect(clampSidebarWidth(10, 0)).toBe(280);
    expect(clampSidebarWidth(900, 0)).toBe(600);
  });
});

describe('StackWorkspace sidebar track', () => {
  it('caps the open track at 60 percent and clamps a drag against the container', () => {
    render(
      <MemoryRouter>
        <StackWorkspace />
      </MemoryRouter>,
    );
    const grid = screen.getByTestId('canvas').parentElement?.parentElement as HTMLElement;
    expect(grid.style.gridTemplateColumns).toContain('min(320px, 60%)');

    Object.defineProperty(grid, 'clientWidth', { value: 390, configurable: true });
    const handle = screen.getByTestId('sidebar').parentElement?.previousElementSibling as HTMLElement;
    fireEvent.pointerDown(handle, { pointerId: 1, button: 0, clientX: 100 });
    fireEvent.pointerMove(handle, { pointerId: 1, clientX: 400 });
    fireEvent.pointerUp(handle, { pointerId: 1 });

    expect(grid.style.gridTemplateColumns).toContain('min(156px, 60%)');
  });
});
