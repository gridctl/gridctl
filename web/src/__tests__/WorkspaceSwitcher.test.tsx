import { describe, it, expect, afterEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { WorkspaceSwitcher } from '../components/shell/WorkspaceSwitcher';
import { WORKSPACE_CONFIG } from '../types/workspace';
import { useToolsDirtyStore } from '../stores/useToolsDirtyStore';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <WorkspaceSwitcher />
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

describe('WorkspaceSwitcher — Tools dirty guard', () => {
  afterEach(() => {
    useToolsDirtyStore.setState({ dirty: false, exitNavTarget: null });
  });

  it('cancels leaving /tools with a dirty editor and stashes the target', () => {
    useToolsDirtyStore.setState({ dirty: true, exitNavTarget: null });
    renderAt('/tools');

    fireEvent.click(screen.getByRole('link', { name: 'Metrics' }));

    expect(useToolsDirtyStore.getState().exitNavTarget).toBe('/metrics');
    // The NavLink was cancelled — Tools stays the active link.
    expect(screen.getByRole('link', { name: 'Tools' })).toHaveAttribute(
      'aria-current',
      'page',
    );
    expect(screen.getByRole('link', { name: 'Metrics' })).not.toHaveAttribute('aria-current');
  });

  it('does not intercept when the Tools editor is clean', () => {
    renderAt('/tools');

    fireEvent.click(screen.getByRole('link', { name: 'Metrics' }));

    expect(useToolsDirtyStore.getState().exitNavTarget).toBeNull();
    expect(screen.getByRole('link', { name: 'Metrics' })).toHaveAttribute(
      'aria-current',
      'page',
    );
  });
});