import { describe, it, expect, beforeEach } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Navigate, Route, Routes, useLocation } from 'react-router';
import { LEGACY_REDIRECTS } from '../routes';
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
        <Route path="/stack" element={<div>stack-page</div>} />
        <Route path="/library" element={<div>library-page</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

function renderRedirect(initial: string) {
  return render(
    <MemoryRouter initialEntries={[initial]}>
      <Routes>
        {LEGACY_REDIRECTS.map((redirect) => (
          <Route
            key={redirect.from}
            path={redirect.from}
            element={<Navigate to={redirect.to} replace />}
          />
        ))}
        <Route path="/library" element={<LocationProbe />} />
        <Route path="/stack" element={<StackProbe />} />
        <Route path="/traces" element={<TracesProbe />} />
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
        <Route path="/stack" element={<div>stack-page</div>} />
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
      <span data-testid="library-search">{location.search}</span>
      library-page
    </div>
  );
}

function TracesProbe() {
  const location = useLocation();
  return (
    <div>
      <span data-testid="traces-pathname">{location.pathname}</span>
      <span data-testid="traces-search">{location.search}</span>
      traces-page
    </div>
  );
}

function StackProbe() {
  const location = useLocation();
  return (
    <div>
      <span data-testid="stack-pathname">{location.pathname}</span>
      stack-page
    </div>
  );
}

describe('resolveLandingWorkspace', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('defaults to /stack when nothing is stored and no skills', () => {
    expect(resolveLandingWorkspace({ stackId: null, hasSkills: false })).toBe('stack');
  });

  it('routes skill-declaring stacks to /library', () => {
    expect(resolveLandingWorkspace({ stackId: 'stack-a', hasSkills: true })).toBe('library');
  });

  it('prefers the per-stack localStorage override over the heuristic', () => {
    localStorage.setItem(`${LAST_WORKSPACE_PER_STACK_PREFIX}stack-a`, 'stack');
    // Even though hasSkills is true, the per-stack pin wins.
    expect(resolveLandingWorkspace({ stackId: 'stack-a', hasSkills: true })).toBe('stack');
  });

  it('falls back to the global localStorage key when no per-stack pin exists', () => {
    localStorage.setItem(LAST_WORKSPACE_GLOBAL_KEY, 'library');
    expect(resolveLandingWorkspace({ stackId: null, hasSkills: false })).toBe('library');
  });

  it('ignores invalid localStorage values', () => {
    localStorage.setItem(LAST_WORKSPACE_GLOBAL_KEY, 'nonsense');
    expect(resolveLandingWorkspace({ stackId: null, hasSkills: false })).toBe('stack');
  });

  it('falls back when a pre-rename build stored the retired topology id', () => {
    localStorage.setItem(LAST_WORKSPACE_GLOBAL_KEY, 'topology');
    localStorage.setItem(`${LAST_WORKSPACE_PER_STACK_PREFIX}stack-a`, 'topology');
    expect(resolveLandingWorkspace({ stackId: 'stack-a', hasSkills: false })).toBe('stack');
    expect(resolveLandingWorkspace({ stackId: 'stack-a', hasSkills: true })).toBe('library');
  });
});

describe('RootRedirect (integration)', () => {
  beforeEach(() => {
    localStorage.clear();
    useStackStore.setState({ gatewayInfo: null });
    useRegistryStore.setState({ skills: null });
  });

  it('sends visitors with no skills declared to /stack', () => {
    useRegistryStore.setState({ skills: [] });
    renderRoot('/');
    expect(screen.getByText('stack-page')).toBeInTheDocument();
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
    localStorage.setItem(`${LAST_WORKSPACE_PER_STACK_PREFIX}stack-a`, 'stack');
    useRegistryStore.setState({
      // @ts-expect-error partial AgentSkill is fine for the test
      skills: [{ name: 'triage', state: 'active' }],
    });
    renderRoot('/');
    expect(screen.getByText('stack-page')).toBeInTheDocument();
  });
});

describe('legacy workspace redirects', () => {
  it('redirects /agent → /library?kind=agent', () => {
    renderRedirect('/agent');
    expect(screen.getByText('library-page')).toBeInTheDocument();
    expect(screen.getByTestId('library-pathname').textContent).toBe('/library');
    expect(screen.getByTestId('library-search').textContent).toBe('?kind=agent');
  });

  it('redirects /skills → /library', () => {
    renderRedirect('/skills');
    expect(screen.getByText('library-page')).toBeInTheDocument();
  });

  it('redirects /runs → /traces?view=runs', () => {
    renderRedirect('/runs');
    expect(screen.getByText('traces-page')).toBeInTheDocument();
    expect(screen.getByTestId('traces-pathname').textContent).toBe('/traces');
    expect(screen.getByTestId('traces-search').textContent).toBe('?view=runs');
  });

  it('redirects /runs/:runID → /traces?view=runs', () => {
    renderRedirect('/runs/abc123');
    expect(screen.getByText('traces-page')).toBeInTheDocument();
    expect(screen.getByTestId('traces-pathname').textContent).toBe('/traces');
    expect(screen.getByTestId('traces-search').textContent).toBe('?view=runs');
  });

  it('redirects /topology → /stack', () => {
    renderRedirect('/topology');
    expect(screen.getByText('stack-page')).toBeInTheDocument();
    expect(screen.getByTestId('stack-pathname').textContent).toBe('/stack');
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
    // No skills declared, so RootRedirect lands on /stack rather than a blank page.
    expect(screen.getByText('stack-page')).toBeInTheDocument();
  });
});
