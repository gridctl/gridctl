import { describe, it, expect, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { GatewaySidebar } from '../components/gateway/GatewaySidebar';
import { useRegistryStore } from '../stores/useRegistryStore';
import { useStackStore } from '../stores/useStackStore';
import type { AgentSkill } from '../types';

const SAMPLE_SKILLS: AgentSkill[] = [
  // @ts-expect-error partial AgentSkill is fine for the test
  { name: 'a', state: 'active', dir: 'a', fileCount: 1 },
  // @ts-expect-error partial AgentSkill is fine for the test
  { name: 'b', state: 'draft', dir: 'b', fileCount: 1 },
  // @ts-expect-error partial AgentSkill is fine for the test
  { name: 'c', state: 'disabled', dir: 'c', fileCount: 1 },
];

function renderSidebar() {
  return render(
    <MemoryRouter>
      <GatewaySidebar onClose={() => {}} />
    </MemoryRouter>,
  );
}

describe('GatewaySidebar', () => {
  beforeEach(() => {
    useStackStore.setState({ selectedNodeId: null });
    useRegistryStore.setState({ skills: SAMPLE_SKILLS });
  });

  it('renders a "Manage Skills" link pointing to /library', () => {
    renderSidebar();
    const link = screen.getByRole('link', { name: /manage skills/i });
    expect(link).toHaveAttribute('href', '/library');
  });

  it('includes the live skill count in the CTA label', () => {
    renderSidebar();
    const link = screen.getByRole('link', { name: /manage skills/i });
    expect(link.textContent).toContain('(3)');
  });

  it('omits the count when skills have not loaded yet', () => {
    useRegistryStore.setState({ skills: null });
    renderSidebar();
    const link = screen.getByRole('link', { name: /manage skills/i });
    expect(link.textContent).not.toMatch(/\(\d+\)/);
  });
});
