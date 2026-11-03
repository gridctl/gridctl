import { describe, it, expect, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { PinDriftBadge } from '../components/pins/PinDriftBadge';
import { PinsWorkspace } from '../components/workspaces/PinsWorkspace';
import { ContextPinChip } from '../components/context/ContextPinChip';
import { usePinsStore } from '../stores/usePinsStore';
import type { ContextPin } from '../lib/api';
import * as api from '../lib/api';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));
vi.mock('../hooks/usePolling', () => ({
  requestGatewayPoll: vi.fn(),
  usePolling: () => ({ refresh: vi.fn() }),
}));

function pin(status: ContextPin['status'], findings: ContextPin['findings'] = []): ContextPin {
  return {
    hash: 'c1:abc',
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    status,
    findings,
    document: 'old',
  };
}

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

afterEach(() => {
  usePinsStore.setState({ pins: null, skillPins: null, contextPins: null, contextPinsError: null });
  vi.restoreAllMocks();
});

describe('context pin store and badge', () => {
  it('loads context pins and records a failure', () => {
    usePinsStore.getState().setContextPins({ style: pin('drift') });
    expect(usePinsStore.getState().contextPins?.style.status).toBe('drift');
    usePinsStore.getState().setContextPinsError('Pins unavailable');
    expect(usePinsStore.getState().contextPinsError).toBe('Pins unavailable');
  });

  it('counts fragment drift and deep-links after servers and skills', () => {
    usePinsStore.setState({
      pins: {},
      skillPins: {},
      contextPins: { style: pin('drift') },
    });
    render(
      <MemoryRouter>
        <PinDriftBadge />
        <Routes>
          <Route path="*" element={<LocationProbe />} />
        </Routes>
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByText('Pins: 1 drifted (1 fragment)'));
    expect(screen.getByTestId('location').textContent).toBe('/pins?kind=context&fragment=style&view=drift');
  });
});

describe('context pin workspace', () => {
  it('parses kind=context and fragment', () => {
    usePinsStore.setState({
      pins: {},
      skillPins: {},
      contextPins: { style: pin('drift'), tools: pin('pinned') },
    });
    render(
      <CommandRegistryProvider>
        <MemoryRouter initialEntries={['/pins?kind=context&fragment=style']}>
          <Routes>
            <Route path="/pins" element={<PinsWorkspace />} />
          </Routes>
        </MemoryRouter>
      </CommandRegistryProvider>,
    );
    expect(screen.getByRole('button', { name: 'Context' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: /^style/ })).toHaveAttribute('aria-current', 'true');
  });
});

describe('ContextPinChip', () => {
  it('renders drift and hides a pinned fragment', () => {
    const onReview = vi.fn();
    const { rerender } = render(<ContextPinChip status="drift" onReview={onReview} />);
    fireEvent.click(screen.getByRole('button', { name: 'Pin drift' }));
    expect(onReview).toHaveBeenCalled();
    rerender(<ContextPinChip status="pinned" onReview={onReview} />);
    expect(screen.queryByRole('button', { name: 'Pin drift' })).toBeNull();
    rerender(<ContextPinChip status="unpinned" onReview={onReview} />);
    expect(screen.getByRole('button', { name: 'Unpinned' })).toBeInTheDocument();
  });
});

describe('context pin approve', () => {
  it('requires a reason when findings exist and sends expected_hash', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue({
      fragment: 'style',
      status: 'drift',
      composite_hash: 'abc',
      old_document: 'old',
      new_document: 'new',
      findings: [{ code: 'P001', severity: 'warn', confidence: 'high', field: 'body', message: 'hidden' }],
    });
    const approve = vi.spyOn(api, 'approveContextPin').mockResolvedValue();
    usePinsStore.setState({
      pins: {},
      skillPins: {},
      contextPins: {
        style: pin('drift', [{ code: 'P001', severity: 'warn', confidence: 'high', field: 'body', message: 'hidden' }]),
      },
    });
    render(
      <CommandRegistryProvider>
        <MemoryRouter initialEntries={['/pins?kind=context&fragment=style']}>
          <Routes>
            <Route path="/pins" element={<PinsWorkspace />} />
          </Routes>
        </MemoryRouter>
      </CommandRegistryProvider>,
    );
    const button = await screen.findByRole('button', { name: 'Approve' });
    expect(button).toBeDisabled();
    fireEvent.change(screen.getByLabelText('Approval reason'), { target: { value: 'reviewed' } });
    expect(button).toBeEnabled();
    fireEvent.click(button);
    await waitFor(() => expect(approve).toHaveBeenCalledWith('style', 'abc', 'reviewed'));
  });

  it('approves without a reason when the diff has no findings', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue({
      fragment: 'tools',
      status: 'drift',
      composite_hash: 'def',
      old_document: 'old',
      new_document: 'new',
      findings: [],
    });
    const approve = vi.spyOn(api, 'approveContextPin').mockResolvedValue();
    usePinsStore.setState({
      pins: {},
      skillPins: {},
      contextPins: { tools: pin('drift') },
    });
    render(
      <CommandRegistryProvider>
        <MemoryRouter initialEntries={['/pins?kind=context&fragment=tools']}>
          <Routes>
            <Route path="/pins" element={<PinsWorkspace />} />
          </Routes>
        </MemoryRouter>
      </CommandRegistryProvider>,
    );
    const button = await screen.findByRole('button', { name: 'Approve' });
    expect(button).toBeEnabled();
    fireEvent.click(button);
    await waitFor(() => expect(approve).toHaveBeenCalledWith('tools', 'def', undefined));
  });
});
