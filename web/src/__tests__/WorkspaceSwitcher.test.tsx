import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { WorkspaceSwitcher } from '../components/shell/WorkspaceSwitcher';

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <WorkspaceSwitcher />
    </MemoryRouter>,
  );
}

describe('WorkspaceSwitcher', () => {
  it('renders two workspace pills inside a tablist in the configured order', () => {
    renderAt('/topology');
    const tablist = screen.getByRole('tablist', { name: /workspace/i });
    expect(tablist).toBeInTheDocument();
    const tabs = screen.getAllByRole('tab');
    expect(tabs).toHaveLength(2);
    expect(tabs.map((t) => t.textContent?.trim())).toEqual([
      'Topology',
      'Library',
    ]);
  });

  it('marks the active workspace with aria-selected=true and others false', () => {
    renderAt('/library');
    const topology = screen.getByRole('tab', { name: 'Topology' });
    const library = screen.getByRole('tab', { name: 'Library' });

    expect(library).toHaveAttribute('aria-selected', 'true');
    expect(topology).toHaveAttribute('aria-selected', 'false');
  });

  it('marks the Library pill active for /library and deep-link paths', () => {
    renderAt('/library/incident-triage');
    expect(screen.getByRole('tab', { name: 'Library' })).toHaveAttribute('aria-selected', 'true');
  });

  it('links each pill to its workspace route', () => {
    renderAt('/topology');
    expect(screen.getByRole('tab', { name: 'Topology' })).toHaveAttribute('href', '/topology');
    expect(screen.getByRole('tab', { name: 'Library' })).toHaveAttribute('href', '/library');
  });
});
