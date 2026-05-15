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
  it('renders three workspace pills inside a tablist', () => {
    renderAt('/topology');
    const tablist = screen.getByRole('tablist', { name: /workspace/i });
    expect(tablist).toBeInTheDocument();
    expect(screen.getAllByRole('tab')).toHaveLength(3);
    expect(screen.getByRole('tab', { name: 'Topology' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Skills' })).toBeInTheDocument();
    expect(screen.getByRole('tab', { name: 'Runs' })).toBeInTheDocument();
  });

  it('marks the active workspace with aria-selected=true and others false', () => {
    renderAt('/skills');
    const topology = screen.getByRole('tab', { name: 'Topology' });
    const skills = screen.getByRole('tab', { name: 'Skills' });
    const runs = screen.getByRole('tab', { name: 'Runs' });

    expect(skills).toHaveAttribute('aria-selected', 'true');
    expect(topology).toHaveAttribute('aria-selected', 'false');
    expect(runs).toHaveAttribute('aria-selected', 'false');
  });

  it('treats nested workspace paths as active', () => {
    renderAt('/runs/abc123');
    expect(screen.getByRole('tab', { name: 'Runs' })).toHaveAttribute('aria-selected', 'true');
  });

  it('links each pill to its workspace route', () => {
    renderAt('/topology');
    expect(screen.getByRole('tab', { name: 'Topology' })).toHaveAttribute('href', '/topology');
    expect(screen.getByRole('tab', { name: 'Skills' })).toHaveAttribute('href', '/skills');
    expect(screen.getByRole('tab', { name: 'Runs' })).toHaveAttribute('href', '/runs');
  });
});
