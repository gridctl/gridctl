import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { AppShell } from '../components/shell/AppShell';
import { useStackStore } from '../stores/useStackStore';
import { useUIStore } from '../stores/useUIStore';

vi.mock('../components/layout/Header', () => ({ Header: () => null }));
vi.mock('../components/layout/StatusBar', () => ({ StatusBar: () => null }));
vi.mock('../components/palette/CommandPalette', () => ({ CommandPalette: () => null }));
vi.mock('../components/ui/Toast', () => ({ ToastContainer: () => null }));
vi.mock('../hooks/usePolling', () => ({ usePolling: () => ({ refresh: async () => {} }) }));
vi.mock('../hooks/useGlobalCommands', () => ({ useGlobalCommands: () => {} }));

describe('AppShell Escape', () => {
  beforeEach(() => {
    useStackStore.setState({ selectedNodeId: 'mcp-github' });
    useUIStore.setState({ sidebarOpen: true });
  });

  it('clears the selected node and closes the sidebar', () => {
    render(
      <MemoryRouter initialEntries={['/stack']}>
        <Routes>
          <Route path="*" element={<AppShell />} />
        </Routes>
      </MemoryRouter>,
    );
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(useStackStore.getState().selectedNodeId).toBeNull();
    expect(useUIStore.getState().sidebarOpen).toBe(false);
  });
});
