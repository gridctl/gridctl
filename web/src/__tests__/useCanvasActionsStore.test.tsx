import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { AppShell } from '../components/shell/AppShell';
import { useCanvasActionsStore } from '../stores/useCanvasActionsStore';

vi.mock('../components/layout/Header', () => ({ Header: () => null }));
vi.mock('../components/layout/StatusBar', () => ({ StatusBar: () => null }));
vi.mock('../components/palette/CommandPalette', () => ({ CommandPalette: () => null }));
vi.mock('../components/ui/Toast', () => ({ ToastContainer: () => null }));
vi.mock('../hooks/usePolling', () => ({ usePolling: () => ({ refresh: async () => {} }) }));
vi.mock('../hooks/useGlobalCommands', () => ({ useGlobalCommands: () => {} }));

describe('useCanvasActionsStore', () => {
  it('is a no-op until Canvas registers, and clears on unmount of the registration', () => {
    const fitView = vi.fn();
    useCanvasActionsStore.getState().fitView();
    expect(fitView).not.toHaveBeenCalled();

    useCanvasActionsStore.getState().register({
      fitView,
      zoomIn: vi.fn(),
      zoomOut: vi.fn(),
    });
    useCanvasActionsStore.getState().fitView({ padding: 0.2, duration: 300 });
    expect(fitView).toHaveBeenCalledWith({ padding: 0.2, duration: 300 });

    useCanvasActionsStore.getState().clear();
    useCanvasActionsStore.getState().fitView();
    expect(fitView).toHaveBeenCalledTimes(1);
  });

  it('shell fit shortcut calls the registered canvas action on Stack', () => {
    const fitView = vi.fn();
    useCanvasActionsStore.getState().register({
      fitView,
      zoomIn: vi.fn(),
      zoomOut: vi.fn(),
    });
    render(
      <MemoryRouter initialEntries={['/stack']}>
        <Routes>
          <Route path="*" element={<AppShell />} />
        </Routes>
      </MemoryRouter>,
    );
    fireEvent.keyDown(window, { key: 'f' });
    expect(fitView).toHaveBeenCalledWith({ padding: 0.2, duration: 300 });
    useCanvasActionsStore.getState().clear();
  });
});
