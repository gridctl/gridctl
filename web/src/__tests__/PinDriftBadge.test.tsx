import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router';
import { PinDriftBadge } from '../components/pins/PinDriftBadge';
import { usePinsStore } from '../stores/usePinsStore';
import type { ServerPins } from '../lib/api';

// Location probe: renders the current URL so navigation is observable.
function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function renderBadge() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <PinDriftBadge />
      <Routes>
        <Route path="*" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  );
}

function serverPins(status: ServerPins['status']): ServerPins {
  return {
    server_hash: 'h2:abc',
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    tool_count: 1,
    status,
    tools: {},
  };
}

beforeEach(() => {
  usePinsStore.setState({ pins: null });
});

afterEach(() => {
  usePinsStore.setState({ pins: null });
});

describe('PinDriftBadge', () => {
  it('deep-links to the first drifted server', () => {
    usePinsStore.setState({
      pins: {
        zeta: serverPins('drift'),
        alpha: serverPins('drift'),
        clean: serverPins('pinned'),
      },
    });

    renderBadge();
    fireEvent.click(screen.getByRole('button', { name: /pins: 2 drifted/i }));

    expect(screen.getByTestId('location')).toHaveTextContent('/pins?server=alpha&view=drift');
  });

  it('falls back to bare /pins when nothing is drifted', () => {
    usePinsStore.setState({ pins: { clean: serverPins('pinned') } });

    renderBadge();
    fireEvent.click(screen.getByRole('button', { name: /pins: ok/i }));

    expect(screen.getByTestId('location')).toHaveTextContent('/pins');
  });
});
