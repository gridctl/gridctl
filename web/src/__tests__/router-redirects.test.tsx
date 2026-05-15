import { describe, it, expect, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import { RootRedirect } from '../components/shell/RootRedirect';
import {
  resolveLandingWorkspace,
  LAST_WORKSPACE_GLOBAL_KEY,
  LAST_WORKSPACE_PER_STACK_PREFIX,
} from '../lib/landing-workspace';
import { AgentRedirect } from '../components/shell/AgentRedirect';
import { useStackStore } from '../stores/useStackStore';
import { useRegistryStore } from '../stores/useRegistryStore';

function renderRoot(initial = '/') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/" element={<RootRedirect />} />
        <Route path="/topology" element={<div>topology-page</div>} />
        <Route path="/skills" element={<div>skills-page</div>} />
        <Route path="/runs" element={<div>runs-page</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

function renderAgent(initial: string) {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/agent" element={<AgentRedirect />} />
        <Route
          path="/skills"
          element={
            <SkillsCapture />
          }
        />
      </Routes>
    </MemoryRouter>,
  );
}

function SkillsCapture() {
  // Echo the router-managed search + hash so we can assert preservation.
  // MemoryRouter never writes to window.location, so we read via useLocation.
  const location = useLocation();
  return (
    <div>
      <span data-testid="skills-search">{location.search}</span>
      <span data-testid="skills-hash">{location.hash}</span>
      skills-page
    </div>
  );
}

describe('resolveLandingWorkspace', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('defaults to /topology when nothing is stored and no skills', () => {
    expect(resolveLandingWorkspace({ stackId: null, hasSkills: false })).toBe('topology');
  });

  it('routes skill-declaring stacks to /skills', () => {
    expect(resolveLandingWorkspace({ stackId: 'stack-a', hasSkills: true })).toBe('skills');
  });

  it('prefers the per-stack localStorage override over the heuristic', () => {
    localStorage.setItem(`${LAST_WORKSPACE_PER_STACK_PREFIX}stack-a`, 'runs');
    // Even though hasSkills is true, the per-stack pin wins.
    expect(resolveLandingWorkspace({ stackId: 'stack-a', hasSkills: true })).toBe('runs');
  });

  it('falls back to the global localStorage key when no per-stack pin exists', () => {
    localStorage.setItem(LAST_WORKSPACE_GLOBAL_KEY, 'skills');
    expect(resolveLandingWorkspace({ stackId: null, hasSkills: false })).toBe('skills');
  });

  it('ignores invalid localStorage values', () => {
    localStorage.setItem(LAST_WORKSPACE_GLOBAL_KEY, 'nonsense');
    expect(resolveLandingWorkspace({ stackId: null, hasSkills: false })).toBe('topology');
  });
});

describe('RootRedirect (integration)', () => {
  beforeEach(() => {
    localStorage.clear();
    useStackStore.setState({ gatewayInfo: null });
    useRegistryStore.setState({ skills: null });
  });

  it('sends visitors with no skills declared to /topology', () => {
    useRegistryStore.setState({ skills: [] });
    renderRoot('/');
    expect(screen.getByText('topology-page')).toBeInTheDocument();
  });

  it('sends visitors with skills declared to /skills', () => {
    useRegistryStore.setState({
      skills: [
        // Minimal shape — only `length > 0` matters for the heuristic.
        // @ts-expect-error partial AgentSkill is fine for the test
        { name: 'triage', state: 'active' },
      ],
    });
    renderRoot('/');
    expect(screen.getByText('skills-page')).toBeInTheDocument();
  });

  it('honors a per-stack localStorage override', () => {
    useStackStore.setState({ gatewayInfo: { name: 'stack-a' } as never });
    localStorage.setItem(`${LAST_WORKSPACE_PER_STACK_PREFIX}stack-a`, 'runs');
    useRegistryStore.setState({
      // @ts-expect-error partial AgentSkill is fine for the test
      skills: [{ name: 'triage', state: 'active' }],
    });
    renderRoot('/');
    expect(screen.getByText('runs-page')).toBeInTheDocument();
  });
});

describe('AgentRedirect', () => {
  it('preserves query parameters when redirecting /agent → /skills', () => {
    renderAgent('/agent?skill=triage_input&run=abc');
    expect(screen.getByText('skills-page')).toBeInTheDocument();
    expect(screen.getByTestId('skills-search').textContent).toBe('?skill=triage_input&run=abc');
  });

  it('redirects /agent with no query to /skills cleanly', () => {
    renderAgent('/agent');
    expect(screen.getByText('skills-page')).toBeInTheDocument();
    expect(screen.getByTestId('skills-search').textContent).toBe('');
  });
});
