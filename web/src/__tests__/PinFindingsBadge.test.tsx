import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router';
import { PinFindingsBadge } from '../components/pins/PinFindingsBadge';
import { usePinsStore } from '../stores/usePinsStore';
import type { PinFinding, ServerPins } from '../lib/api';

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function renderBadge() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <PinFindingsBadge />
      <Routes>
        <Route path="*" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  );
}

function serverPins(findings?: PinFinding[]): ServerPins {
  return {
    server_hash: 'h2:abc',
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    tool_count: 1,
    status: 'pinned',
    tools: {
      tool: {
        hash: 'h2:abc',
        name: 'tool',
        pinned_at: '2026-07-01T00:00:00Z',
        ...(findings ? { findings } : {}),
      },
    },
  };
}

const warnFinding: PinFinding = {
  code: 'P001',
  severity: 'warn',
  confidence: 'high',
  field: 'description',
  message: 'hidden-instruction phrasing',
};

beforeEach(() => {
  usePinsStore.setState({ pins: null });
});

afterEach(() => {
  usePinsStore.setState({ pins: null });
});

describe('PinFindingsBadge', () => {
  it('deep-links to the first server with warn-or-critical findings', () => {
    usePinsStore.setState({
      pins: {
        zeta: serverPins([warnFinding]),
        beta: serverPins([warnFinding]),
        clean: serverPins(),
      },
    });

    renderBadge();
    fireEvent.click(screen.getByRole('button', { name: /findings: 2 servers/i }));

    expect(screen.getByTestId('location')).toHaveTextContent('/pins?server=beta');
  });

  it('renders nothing when only info findings exist', () => {
    usePinsStore.setState({
      pins: { quiet: serverPins([{ ...warnFinding, severity: 'info' }]) },
    });

    renderBadge();

    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });
});
