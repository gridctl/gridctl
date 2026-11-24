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

  it('returns focus from <body> to the inspected node wrapper', () => {
    const wrapper = document.createElement('div');
    wrapper.className = 'react-flow__node';
    wrapper.setAttribute('data-id', 'mcp-github');
    wrapper.tabIndex = 0;
    document.body.appendChild(wrapper);
    (document.activeElement as HTMLElement | null)?.blur();
    render(
      <MemoryRouter initialEntries={['/stack']}>
        <Routes>
          <Route path="*" element={<AppShell />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(document.activeElement).toBe(document.body);
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(document.activeElement).toBe(wrapper);
    expect(useStackStore.getState().selectedNodeId).toBeNull();
    wrapper.remove();
  });

  it('does not take focus from an element outside the inspector', () => {
    const wrapper = document.createElement('div');
    wrapper.className = 'react-flow__node';
    wrapper.setAttribute('data-id', 'mcp-github');
    wrapper.tabIndex = 0;
    const other = document.createElement('button');
    document.body.append(wrapper, other);
    render(
      <MemoryRouter initialEntries={['/stack']}>
        <Routes>
          <Route path="*" element={<AppShell />} />
        </Routes>
      </MemoryRouter>,
    );
    other.focus();
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(document.activeElement).toBe(other);
    wrapper.remove();
    other.remove();
  });
});
