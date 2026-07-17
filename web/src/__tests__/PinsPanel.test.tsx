import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { PinsPanel } from '../components/pins/PinsPanel';
import { usePinsStore } from '../stores/usePinsStore';
import * as api from '../lib/api';
import type { ServerPins } from '../lib/api';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

function serverPins(status: ServerPins['status']): ServerPins {
  return {
    server_hash: 'h2:abc',
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    tool_count: 3,
    status,
    tools: {},
  };
}

// Surfaces the current location in the DOM so navigation from row clicks is
// observable without reassigning module state from inside a component.
function LocationProbe() {
  const loc = useLocation();
  return <div data-testid="location">{`${loc.pathname}${loc.search}`}</div>;
}

function renderPanel() {
  return render(
    <MemoryRouter initialEntries={['/stack']}>
      <LocationProbe />
      <Routes>
        <Route path="*" element={<PinsPanel />} />
      </Routes>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  usePinsStore.setState({
    pins: {
      github: serverPins('pinned'),
      zapier: serverPins('drift'),
    },
  });
});

afterEach(() => {
  usePinsStore.setState({ pins: null });
  vi.restoreAllMocks();
});

describe('PinsPanel', () => {
  it('navigates to the Pins workspace when a row is clicked', () => {
    renderPanel();

    fireEvent.click(screen.getByText('zapier'));
    expect(screen.getByTestId('location')).toHaveTextContent('/pins?server=zapier');
  });

  it('exposes a keyboard-accessible affordance for the row drill-down', () => {
    renderPanel();

    fireEvent.click(screen.getByRole('button', { name: /open github in pins workspace/i }));
    expect(screen.getByTestId('location')).toHaveTextContent('/pins?server=github');
  });

  it('approves without navigating when the Approve button is clicked', async () => {
    const approve = vi.spyOn(api, 'approveServerPins').mockResolvedValue(undefined);
    vi.spyOn(api, 'fetchServerPins').mockResolvedValue({
      github: serverPins('pinned'),
      zapier: serverPins('pinned'),
    });

    renderPanel();

    fireEvent.click(screen.getByRole('button', { name: /^approve$/i }));

    await waitFor(() => expect(approve).toHaveBeenCalledWith('zapier'));
    expect(screen.getByTestId('location')).toHaveTextContent('/stack');
  });

  it('shows no Approve button for pinned servers', () => {
    usePinsStore.setState({ pins: { github: serverPins('pinned') } });
    renderPanel();

    expect(screen.queryByRole('button', { name: /^approve$/i })).not.toBeInTheDocument();
  });
});
