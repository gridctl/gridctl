import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, fireEvent, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { AppShell } from '../components/shell/AppShell';
import { leavesWorkspace, useNavigationGuardStore } from '../stores/useNavigationGuardStore';

vi.mock('../components/layout/Header', () => ({ Header: () => null }));
vi.mock('../components/layout/StatusBar', () => ({ StatusBar: () => null }));
vi.mock('../components/palette/CommandPalette', () => ({ CommandPalette: () => null }));
vi.mock('../components/ui/Toast', () => ({ ToastContainer: () => null }));
vi.mock('../hooks/usePolling', () => ({ usePolling: () => ({ refresh: async () => {} }) }));
vi.mock('../hooks/useGlobalCommands', () => ({ useGlobalCommands: () => {} }));

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname}</div>;
}

function renderShell(path: string) {
  render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="*" element={<AppShell />} />
      </Routes>
      <LocationProbe />
    </MemoryRouter>,
  );
}

function reset() {
  useNavigationGuardStore.setState({ guards: [], hosts: 0, pending: null });
}

describe('AppShell navigation shortcuts', () => {
  beforeEach(reset);
  afterEach(reset);

  it('holds Cmd+J behind a dirty Tools draft until confirmed', () => {
    renderShell('/tools');
    useNavigationGuardStore.getState().register({
      id: 'tools',
      label: 'Tools for github',
      blocks: (to) => leavesWorkspace(to, 'tools'),
    });
    fireEvent.keyDown(window, { key: 'j', metaKey: true });
    expect(screen.getByTestId('location')).toHaveTextContent('/tools');
    expect(screen.getByText('Discard unsaved changes')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/logs');
  });

  it('holds Cmd+2 behind an unconditional guard with focus on a button', () => {
    renderShell('/stack');
    const onDiscard = vi.fn();
    useNavigationGuardStore.getState().register({ id: 'skill', label: 'skill "triage"', onDiscard });
    const button = document.createElement('button');
    document.body.appendChild(button);
    button.focus();
    fireEvent.keyDown(button, { key: '2', metaKey: true });
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    expect(screen.getByText(/skill "triage"/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
    expect(onDiscard).not.toHaveBeenCalled();

    fireEvent.keyDown(button, { key: '2', metaKey: true });
    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(onDiscard).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('location')).toHaveTextContent('/library');
    button.remove();
  });

  it('switches workspaces with no dialog when nothing is dirty', () => {
    renderShell('/stack');
    fireEvent.keyDown(window, { key: '5', metaKey: true });
    expect(screen.getByTestId('location')).toHaveTextContent('/metrics');
    fireEvent.keyDown(window, { key: 'j', metaKey: true });
    expect(screen.getByTestId('location')).toHaveTextContent('/logs');
    expect(screen.queryByText('Discard unsaved changes')).not.toBeInTheDocument();
  });

  it('lets a workspace switch through a guard that does not block it', () => {
    renderShell('/stack');
    useNavigationGuardStore.getState().register({
      id: 'lens',
      label: 'Access Lens',
      blocks: (to) => leavesWorkspace(to, 'stack'),
    });
    fireEvent.keyDown(window, { key: '1', metaKey: true });
    expect(screen.queryByText('Discard unsaved changes')).not.toBeInTheDocument();
    fireEvent.keyDown(window, { key: '4', metaKey: true });
    expect(screen.getByText('Discard unsaved changes')).toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
  });
});
