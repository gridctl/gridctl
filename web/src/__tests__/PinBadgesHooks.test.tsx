import { describe, it, expect, afterEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { PinDriftBadge } from '../components/pins/PinDriftBadge';
import { usePinsStore } from '../stores/usePinsStore';
import type { ContextPin, ServerPins, SkillPin } from '../lib/api';
import type { HookPin } from '../types';

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

function skillPin(status: SkillPin['status']): SkillPin {
  return { skill_hash: 's1:abc', pinned_at: '', last_verified_at: '', status };
}

function contextPin(status: 'pinned' | 'drift'): ContextPin {
  return { status } as ContextPin;
}

function hookPin(status: HookPin['status']): HookPin {
  return {
    definition_hash: 'h1:abc',
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    status,
  };
}

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function renderBadge() {
  return render(
    <MemoryRouter>
      <PinDriftBadge />
      <Routes>
        <Route path="*" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => {
  usePinsStore.setState({ pins: null, skillPins: null, contextPins: null, hookPins: null });
});

describe('PinDriftBadge with hooks', () => {
  it('counts hook drift, calls it out, and deep-links to the hook review', () => {
    usePinsStore.setState({ pins: {}, hookPins: { 'block-force-push': hookPin('drift') } });
    renderBadge();
    fireEvent.click(screen.getByText('Pins: 1 drifted (1 hook)'));
    expect(screen.getByTestId('location')).toHaveTextContent(
      '/pins?kind=hook&hook=block-force-push&view=drift',
    );
  });

  it('renders on hook drift before the server poll lands', () => {
    usePinsStore.setState({ pins: null, hookPins: { b: hookPin('drift'), a: hookPin('drift') } });
    renderBadge();
    fireEvent.click(screen.getByText('Pins: 2 drifted (2 hooks)'));
    expect(screen.getByTestId('location')).toHaveTextContent('/pins?kind=hook&hook=a&view=drift');
  });

  it('renders OK when only hooks are pinned', () => {
    usePinsStore.setState({ pins: {}, hookPins: { a: hookPin('pinned') } });
    renderBadge();
    expect(screen.getByText('Pins: OK')).toBeInTheDocument();
  });

  it('orders the callouts server, skill, fragment, hook and lets earlier kinds win the link', () => {
    usePinsStore.setState({
      pins: { github: serverPins('drift') },
      skillPins: { triage: skillPin('drift') },
      contextPins: { rules: contextPin('drift') },
      hookPins: { guard: hookPin('drift') },
    });
    renderBadge();
    fireEvent.click(screen.getByText('Pins: 4 drifted (1 skill, 1 fragment, 1 hook)'));
    expect(screen.getByTestId('location')).toHaveTextContent('/pins?server=github&view=drift');
  });

  it.each([
    [{ pins: { github: serverPins('drift') } }, 'Pins: 1 drifted'],
    [{ pins: {}, skillPins: { triage: skillPin('drift') } }, 'Pins: 1 drifted (1 skill)'],
    [{ pins: {}, contextPins: { rules: contextPin('drift') } }, 'Pins: 1 drifted (1 fragment)'],
    [
      { pins: { github: serverPins('drift') }, skillPins: { triage: skillPin('drift') }, contextPins: { rules: contextPin('drift') } },
      'Pins: 3 drifted (1 skill, 1 fragment)',
    ],
    [{ pins: { github: serverPins('pinned') } }, 'Pins: OK'],
  ])('keeps the label unchanged without hook drift (%#)', (state, label) => {
    usePinsStore.setState({ ...state, hookPins: { guard: hookPin('pinned') } });
    renderBadge();
    expect(screen.getByText(label)).toBeInTheDocument();
  });
});
