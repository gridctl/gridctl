import { describe, it, expect, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom';
import { RootRedirect } from '../components/shell/RootRedirect';
import {
  resolveLandingWorkspace,
  LAST_WORKSPACE_GLOBAL_KEY,
  LAST_WORKSPACE_PER_STACK_PREFIX,
} from '../lib/landing-workspace';
import { useStackStore } from '../stores/useStackStore';
import { useRegistryStore } from '../stores/useRegistryStore';

function renderRoot(initial = '/') {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/" element={<RootRedirect />} />
        <Route path="/topology" element={<div>topology-page</div>} />
        <Route path="/library" element={<div>library-page</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

function renderRedirect(initial: string) {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/agent" element={<Navigate to="/library" replace />} />
        <Route path="/skills" element={<Navigate to="/library" replace />} />
        <Route path="/runs" element={<Navigate to="/library" replace />} />
        <Route path="/runs/:runID" element={<Navigate to="/library" replace />} />
        <Route path="/library" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  );
}

// Mirrors the catch-all in routes.tsx: unmatched URLs redirect to "/", where
// RootRedirect resolves the landing workspace.
function renderCatchAll(initial: string) {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        <Route path="/" element={<RootRedirect />} />
        <Route path="/topology" element={<div>topology-page</div>} />
        <Route path="/library" element={<div>library-page</div>} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </MemoryRouter>,
  );
}

function LocationProbe() {
  const location = useLocation();
  return (
    <div>
      <span data-testid="library-pathname">{location.pathname}</span>
      library-page
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

  it('routes skill-declaring stacks to /library', () => {
    expect(resolveLandingWorkspace({ stackId: 'stack-a', hasSkills: true })).toBe('library');
  });

  it('prefers the per-stack localStorage override over the heuristic', () => {
    localStorage.setItem(`${LAST_WORKSPACE_PER_STACK_PREFIX}stack-a`, 'topology');
    // Even though hasSkills is true, the per-stack pin wins.
    expect(resolveLandingWorkspace({ stackId: 'stack-a', hasSkills: true })).toBe('topology');
  });

  it('falls back to the global localStorage key when no per-stack pin exists', () => {
    localStorage.setItem(LAST_WORKSPACE_GLOBAL_KEY, 'library');
    expect(resolveLandingWorkspace({ stackId: null, hasSkills: false })).toBe('library');
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

  it('sends visitors with skills declared to /library', () => {
    useRegistryStore.setState({
      skills: [
        // Minimal shape — only `length > 0` matters for the heuristic.
        // @ts-expect-error partial AgentSkill is fine for the test
        { name: 'triage', state: 'active' },
      ],
    });
    renderRoot('/');
    expect(screen.getByText('library-page')).toBeInTheDocument();
  });

  it('honors a per-stack localStorage override', () => {
    useStackStore.setState({ gatewayInfo: { name: 'stack-a' } as never });
    localStorage.setItem(`${LAST_WORKSPACE_PER_STACK_PREFIX}stack-a`, 'topology');
    useRegistryStore.setState({
      // @ts-expect-error partial AgentSkill is fine for the test
      skills: [{ name: 'triage', state: 'active' }],
    });
    renderRoot('/');
    expect(screen.getByText('topology-page')).toBeInTheDocument();
  });
});

describe('legacy workspace redirects', () => {
  it('redirects /agent → /library', () => {
    renderRedirect('/agent');
    expect(screen.getByText('library-page')).toBeInTheDocument();
    expect(screen.getByTestId('library-pathname').textContent).toBe('/library');
  });

  it('redirects /skills → /library', () => {
    renderRedirect('/skills');
    expect(screen.getByText('library-page')).toBeInTheDocument();
  });

  it('redirects /runs → /library', () => {
    renderRedirect('/runs');
    expect(screen.getByText('library-page')).toBeInTheDocument();
  });

  it('redirects /runs/:runID → /library', () => {
    renderRedirect('/runs/abc123');
    expect(screen.getByText('library-page')).toBeInTheDocument();
  });
});

describe('catch-all route', () => {
  beforeEach(() => {
    localStorage.clear();
    useStackStore.setState({ gatewayInfo: null });
    useRegistryStore.setState({ skills: [] });
  });

  it('redirects an unknown URL to the resolved landing workspace', () => {
    renderCatchAll('/does-not-exist');
    // No skills declared, so RootRedirect lands on /topology rather than a blank page.
    expect(screen.getByText('topology-page')).toBeInTheDocument();
  });
});
