import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { WorkspaceSwitcher } from '../components/shell/WorkspaceSwitcher';
import { WORKSPACE_CONFIG } from '../types/workspace';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <WorkspaceSwitcher />
    </MemoryRouter>,
  );
}

describe('WorkspaceSwitcher', () => {
  it('renders one pill per workspace, in WORKSPACE_CONFIG order', () => {
    renderAt('/topology');
    const tablist = screen.getByRole('tablist', { name: /workspace/i });
    expect(tablist).toBeInTheDocument();
    const tabs = screen.getAllByRole('tab');
    expect(tabs).toHaveLength(WORKSPACE_CONFIG.length);
    expect(tabs.map((t) => t.textContent?.trim())).toEqual(
      WORKSPACE_CONFIG.map((w) => w.label),
    );
  });

  it('marks the active workspace with aria-selected=true and others false', () => {
    renderAt('/library');
    for (const ws of WORKSPACE_CONFIG) {
      const tab = screen.getByRole('tab', { name: ws.label });
      expect(tab).toHaveAttribute(
        'aria-selected',
        ws.id === 'library' ? 'true' : 'false',
      );
    }
  });

  it('marks the Library pill active for /library and deep-link paths', () => {
    renderAt('/library/incident-triage');
    expect(screen.getByRole('tab', { name: 'Library' })).toHaveAttribute(
      'aria-selected',
      'true',
    );
  });

  it('marks the Variables pill active for /vault', () => {
    renderAt('/vault');
    expect(screen.getByRole('tab', { name: 'Variables' })).toHaveAttribute(
      'aria-selected',
      'true',
    );
  });

  it('links each pill to its workspace route', () => {
    renderAt('/topology');
    for (const ws of WORKSPACE_CONFIG) {
      expect(screen.getByRole('tab', { name: ws.label })).toHaveAttribute(
        'href',
        `/${ws.id}`,
      );
    }
  });
});
