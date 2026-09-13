import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

vi.mock('../lib/api', async (importActual) => {
  const actual = await importActual<typeof import('../lib/api')>();
  return {
    ...actual,
    fetchSecurityReport: vi.fn().mockResolvedValue({
      schema_version: 'gridctl.security-report.v1',
      generated_at: '2026-09-13T00:00:00Z',
      source: { kind: 'gateway', display: 'localhost' },
      coverage: { status: 'partial', predicates_total: 0, predicates_evaluated: 0, predicates_unknown: 0, included_scopes: [], excluded_scopes: [], unknown_gaps: 0 },
      checks: [],
      limitations: [],
      fail_count: 0,
      warn_count: 0,
      unknown_count: 0,
      not_applicable_count: 0,
      pass_count: 0,
    }),
    fetchOptimizeReport: vi.fn().mockResolvedValue({ findings: [], health_score: 0, generated_at: '2026-09-13T00:00:00Z' }),
  };
});

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
