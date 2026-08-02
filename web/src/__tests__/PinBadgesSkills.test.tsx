import { describe, it, expect, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { PinDriftBadge } from '../components/pins/PinDriftBadge';
import { PinFindingsBadge } from '../components/pins/PinFindingsBadge';
import { usePinsStore } from '../stores/usePinsStore';
import type { ServerPins, SkillPin } from '../lib/api';

function serverPins(status: ServerPins['status']): ServerPins {
  return {
    server_hash: 'h2:abc',
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    tool_count: 0,
    status,
    tools: {},
  };
}

function skillPin(status: SkillPin['status'], overrides: Partial<SkillPin> = {}): SkillPin {
  return {
    skill_hash: 's1:abc',
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    status,
    ...overrides,
  };
}

// Rendered into the DOM (not a module variable) so react-hooks/globals
// stays happy; tests read it via the location testid.
function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function lastLocation(): string {
  return screen.getByTestId('location').textContent ?? '';
}

function renderBadge(el: React.ReactElement) {
  return render(
    <MemoryRouter>
      {el}
      <Routes>
        <Route path="*" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => {
  usePinsStore.setState({ pins: null, skillPins: null });
});

describe('PinDriftBadge with skills', () => {
  it('counts skill drift and calls it out beside the total', () => {
    usePinsStore.setState({
      pins: { github: serverPins('drift') },
      skillPins: { 'incident-triage': skillPin('drift') },
    });
    renderBadge(<PinDriftBadge />);
    expect(screen.getByText('Pins: 2 drifted (1 skill)')).toBeInTheDocument();
  });

  it('keeps the server-only label unchanged without skill drift', () => {
    usePinsStore.setState({
      pins: { github: serverPins('drift') },
      skillPins: { 'incident-triage': skillPin('pinned') },
    });
    renderBadge(<PinDriftBadge />);
    expect(screen.getByText('Pins: 1 drifted')).toBeInTheDocument();
  });

  it('renders on a skill-only stack and deep-links to the skill review', () => {
    usePinsStore.setState({
      pins: {},
      skillPins: { 'incident-triage': skillPin('drift') },
    });
    renderBadge(<PinDriftBadge />);
    fireEvent.click(screen.getByText('Pins: 1 drifted (1 skill)'));
    expect(lastLocation()).toBe('/pins?kind=skill&skill=incident-triage&view=drift');
  });
});

describe('PinFindingsBadge with skills', () => {
  it('adds a skills segment to the label and deep-links to the flagged skill', () => {
    usePinsStore.setState({
      pins: {},
      skillPins: {
        flagged: skillPin('pinned', {
          findings: [
            { code: 'P001', severity: 'warn', confidence: 'high', field: 'body', message: 'm' },
          ],
        }),
      },
    });
    renderBadge(<PinFindingsBadge />);
    expect(screen.getByText('Findings: 1 skill')).toBeInTheDocument();
    fireEvent.click(screen.getByText('Findings: 1 skill'));
    expect(lastLocation()).toBe('/pins?kind=skill&skill=flagged&view=findings');
  });

  it('stays hidden when only info findings exist', () => {
    usePinsStore.setState({
      pins: {},
      skillPins: {
        quiet: skillPin('pinned', {
          findings: [
            { code: 'P004', severity: 'info', confidence: 'low', field: 'body', message: 'm' },
          ],
        }),
      },
    });
    const { container } = renderBadge(<PinFindingsBadge />);
    expect(container.querySelector('button')).toBeNull();
  });
});
