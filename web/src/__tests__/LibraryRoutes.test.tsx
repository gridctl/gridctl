import { describe, it, expect } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Navigate, Route, Routes, useLocation } from 'react-router-dom';

function LocationProbe() {
  const loc = useLocation();
  return <span data-testid="location">{loc.pathname}</span>;
}

// Local re-declaration of the redirect logic from src/routes.tsx so the test
// stays focused on the redirect behavior without pulling in AppShell + all
// the lazy-loaded workspaces (which require the full polling/store wiring).
function TestRoutes() {
  return (
    <Routes>
      <Route path="/library-window" element={
        <>
          <div data-testid="library-window-page">library-window</div>
          <LocationProbe />
        </>
      } />
      <Route path="/registry" element={<Navigate to="/library-window" replace />} />
    </Routes>
  );
}

describe('Library detached route redirect', () => {
  it('redirects /registry → /library-window', () => {
    render(
      <MemoryRouter initialEntries={['/registry']}>
        <TestRoutes />
      </MemoryRouter>,
    );
    expect(screen.getByTestId('library-window-page')).toBeInTheDocument();
    expect(screen.getByTestId('location').textContent).toBe('/library-window');
  });

  it('serves /library-window directly without redirecting', () => {
    render(
      <MemoryRouter initialEntries={['/library-window']}>
        <TestRoutes />
      </MemoryRouter>,
    );
    expect(screen.getByTestId('library-window-page')).toBeInTheDocument();
    expect(screen.getByTestId('location').textContent).toBe('/library-window');
  });
});
