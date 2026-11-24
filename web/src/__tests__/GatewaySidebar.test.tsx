import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';

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
    fetchPolicy: vi.fn().mockResolvedValue({ configured: false, rules: [], tools: {} }),
  };
});

import { GatewaySidebar } from '../components/gateway/GatewaySidebar';
import { fetchPolicy } from '../lib/api';
import { useRegistryStore } from '../stores/useRegistryStore';
import { useStackStore } from '../stores/useStackStore';
import type { AgentSkill } from '../types';
import { NavigationGuardDialog } from '../components/shell/NavigationGuardDialog';
import { leavesWorkspace, useNavigationGuardStore } from '../stores/useNavigationGuardStore';

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

  it('renders not configured when the policy block is absent', async () => {
    renderSidebar();
    expect(await screen.findByText(/policy: not configured/i)).toBeInTheDocument();
  });

  it('renders the configured policy summary', async () => {
    vi.mocked(fetchPolicy).mockResolvedValue({
      configured: true,
      default: 'allow',
      block_destructive: true,
      rules: [{ index: 0, match: 'vault__*', action: 'block' }],
      tools: {},
    });
    renderSidebar();
    expect(await screen.findByText(/default allow, 1 rule, block_destructive on/i)).toBeInTheDocument();
  });

  it('omits the count when skills have not loaded yet', () => {
    useRegistryStore.setState({ skills: null });
    renderSidebar();
    const link = screen.getByRole('link', { name: /manage skills/i });
    expect(link.textContent).not.toMatch(/\(\d+\)/);
  });

  it('opens its sections by default and reports their state', () => {
    renderSidebar();
    for (const name of [/optimize/i, /policy/i, /security evidence/i]) {
      const toggle = screen.getByRole('button', { name });
      expect(toggle).toHaveAttribute('type', 'button');
      expect(toggle).toHaveAttribute('aria-expanded', 'true');
      expect(document.getElementById(toggle.getAttribute('aria-controls') as string)).toBeInTheDocument();
    }
  });

  it('leaves the heading without a tabindex when no headingRef is passed', () => {
    renderSidebar();
    expect(screen.getByRole('heading', { level: 2 })).not.toHaveAttribute('tabindex');
  });

  describe('Manage Skills with an unsaved Access Lens draft', () => {
    function LocationProbe() {
      return <div data-testid="location">{useLocation().pathname}</div>;
    }
    const resetGuards = () => useNavigationGuardStore.setState({ guards: [], hosts: 0, pending: null });

    beforeEach(resetGuards);

    it('asks before leaving Stack and keeps the draft on cancel', () => {
      const onDiscard = vi.fn();
      render(
        <MemoryRouter initialEntries={['/stack']}>
          <Routes>
            <Route path="*" element={<GatewaySidebar onClose={() => {}} />} />
          </Routes>
          <NavigationGuardDialog />
          <LocationProbe />
        </MemoryRouter>,
      );
      useNavigationGuardStore.getState().register({
        id: 'lens',
        label: 'Access Lens for Cursor',
        blocks: (to) => leavesWorkspace(to, 'stack'),
        onDiscard,
      });

      fireEvent.click(screen.getByRole('link', { name: /manage skills/i }));
      expect(screen.getByText('Discard unsaved changes')).toBeInTheDocument();
      expect(screen.getByTestId('location')).toHaveTextContent('/stack');
      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
      expect(screen.getByTestId('location')).toHaveTextContent('/stack');
      expect(onDiscard).not.toHaveBeenCalled();

      fireEvent.click(screen.getByRole('link', { name: /manage skills/i }));
      fireEvent.click(screen.getByRole('button', { name: 'Discard & leave' }));
      expect(onDiscard).toHaveBeenCalledTimes(1);
      expect(screen.getByTestId('location')).toHaveTextContent('/library');
      resetGuards();
    });
  });
});
