import { describe, it, expect, afterEach, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router';
import { WorkspaceSwitcher } from '../components/shell/WorkspaceSwitcher';
import { WORKSPACE_CONFIG } from '../types/workspace';
import { leavesWorkspace, useNavigationGuardStore } from '../stores/useNavigationGuardStore';
import { NavigationGuardDialog } from '../components/shell/NavigationGuardDialog';

function LocationProbe() {
  const loc = useLocation();
  return <div data-testid="location">{loc.pathname}</div>;
}

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <WorkspaceSwitcher />
      <NavigationGuardDialog />
      <Routes>
        <Route path="*" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('WorkspaceSwitcher', () => {
  it('renders one link per workspace, in WORKSPACE_CONFIG order', () => {
    renderAt('/stack');
    const nav = screen.getByRole('navigation', { name: 'Workspaces' });
    expect(nav).toBeInTheDocument();
    const links = screen.getAllByRole('link');
    expect(links).toHaveLength(WORKSPACE_CONFIG.length);
    expect(links.map((link) => link.getAttribute('aria-label'))).toEqual(
      WORKSPACE_CONFIG.map((w) => w.label),
    );
    expect(screen.queryAllByRole('tab')).toHaveLength(0);
  });

  it('marks the active workspace with aria-current=page and others with none', () => {
    renderAt('/library');
    for (const ws of WORKSPACE_CONFIG) {
      const link = screen.getByRole('link', { name: ws.label });
      if (ws.id === 'library') {
        expect(link).toHaveAttribute('aria-current', 'page');
      } else {
        expect(link).not.toHaveAttribute('aria-current');
      }
    }
  });

  it('marks the Library link active for deep-link paths', () => {
    renderAt('/library/incident-triage');
    expect(screen.getByRole('link', { name: 'Library' })).toHaveAttribute(
      'aria-current',
      'page',
    );
  });

  it('marks the Variables link active for /vault', () => {
    renderAt('/vault');
    expect(screen.getByRole('link', { name: 'Variables' })).toHaveAttribute(
      'aria-current',
      'page',
    );
  });

  it('links each pill to its workspace route', () => {
    renderAt('/stack');
    for (const ws of WORKSPACE_CONFIG) {
      expect(screen.getByRole('link', { name: ws.label })).toHaveAttribute(
        'href',
        `/${ws.id}`,
      );
    }
  });
});

describe('WorkspaceSwitcher navigation guard', () => {
  let unregister: (() => void) | null = null;

  beforeEach(() => {
    useNavigationGuardStore.setState({ guards: [], pending: null });
  });

  afterEach(() => {
    unregister?.();
    unregister = null;
    useNavigationGuardStore.setState({ guards: [], pending: null });
  });

  function registerToolsGuard() {
    unregister = useNavigationGuardStore.getState().register({
      id: 'tools',
      label: 'Tools for github',
      blocks: (to) => leavesWorkspace(to, 'tools'),
    });
  }

  it('intercepts a pill click when a guard blocks the target', () => {
    registerToolsGuard();
    renderAt('/tools');

    fireEvent.click(screen.getByRole('link', { name: 'Metrics' }));

    expect(screen.getByTestId('location')).toHaveTextContent('/tools');
    expect(screen.getByRole('alertdialog')).toHaveTextContent('Tools for github');
    expect(screen.getByRole('link', { name: 'Tools' })).toHaveAttribute('aria-current', 'page');

    fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
    expect(screen.getByTestId('location')).toHaveTextContent('/metrics');
  });

  it('keeps the route when the confirm is cancelled', () => {
    registerToolsGuard();
    renderAt('/tools');

    fireEvent.click(screen.getByRole('link', { name: 'Metrics' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/tools');
  });

  it('does not intercept when the guard does not block the target', () => {
    registerToolsGuard();
    renderAt('/tools');

    fireEvent.click(screen.getByRole('link', { name: 'Tools' }));

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(useNavigationGuardStore.getState().pending).toBeNull();
  });

  it('does not intercept a modified click', () => {
    registerToolsGuard();
    renderAt('/tools');

    fireEvent.click(screen.getByRole('link', { name: 'Metrics' }), { metaKey: true });
    fireEvent.click(screen.getByRole('link', { name: 'Metrics' }), { ctrlKey: true });
    fireEvent.click(screen.getByRole('link', { name: 'Metrics' }), { shiftKey: true });

    expect(useNavigationGuardStore.getState().pending).toBeNull();
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
  });

  it('navigates natively with nothing registered', () => {
    renderAt('/tools');

    fireEvent.click(screen.getByRole('link', { name: 'Metrics' }));

    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByTestId('location')).toHaveTextContent('/metrics');
    expect(screen.getByRole('link', { name: 'Metrics' })).toHaveAttribute('aria-current', 'page');
  });
});
