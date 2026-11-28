import { describe, it, expect, afterEach, vi } from 'vitest';
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { CommandRegistryProvider, useCommandRegistry } from '../hooks/useCommandRegistry';
import { PinDriftBadge } from '../components/pins/PinDriftBadge';
import { PinsWorkspace } from '../components/workspaces/PinsWorkspace';
import { ContextPinChip, ContextPinReviewModal } from '../components/context/ContextPinChip';
import { usePinsStore } from '../stores/usePinsStore';
import type { ContextPin } from '../lib/api';
import * as api from '../lib/api';
import { showToast } from '../components/ui/Toast';
import { HTTPError, type ContextPinsDiff } from '../lib/api';
import { APPROVE_BUTTON_ID, DRIFT_SECTION_ID } from '../components/pins/PinsDriftSection';
import { RESET_BUTTON_ID, TOOLS_SECTION_ID } from '../components/pins/PinsServerDetail';
import { MAX_DIFF_LINES } from '../lib/diff';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn(), copyWithToast: vi.fn() }));
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
    vi.spyOn(api, 'fetchContextPins').mockResolvedValue({
      style: pin('pinned'),
    });
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
    const dialog = screen.getByRole('alertdialog', { name: 'Approve style' });
    expect(dialog).toHaveTextContent('injected into every linked client');
    expect(dialog).toHaveTextContent('1 warn-or-critical scan finding');
    expect(approve).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Approve' }));
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
    vi.spyOn(api, 'fetchContextPins').mockResolvedValue({ tools: pin('pinned') });
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
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    await waitFor(() => expect(approve).toHaveBeenCalledWith('tools', 'def', undefined));
    await waitFor(() => expect(usePinsStore.getState().contextPins?.tools.status).toBe('pinned'));
  });

  it('requires a reason for info findings', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue({
      fragment: 'tools',
      status: 'drift',
      composite_hash: 'def',
      old_document: 'old',
      new_document: 'new',
      findings: [{ code: 'P003', severity: 'info', confidence: 'low', field: 'body', message: 'sensitive-action language' }],
    });
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
    expect(button).toBeDisabled();
    expect(screen.getByText('This fragment carries advisory findings; a reason is required')).toBeInTheDocument();
  });

  it('approves over an info-only finding without a confirmation', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue({
      fragment: 'tools',
      status: 'drift',
      composite_hash: 'def',
      old_document: 'old',
      new_document: 'new',
      findings: [{ code: 'P003', severity: 'info', confidence: 'low', field: 'body', message: 'note' }],
    });
    const approve = vi.spyOn(api, 'approveContextPin').mockResolvedValue();
    vi.spyOn(api, 'fetchContextPins').mockResolvedValue({ tools: pin('pinned') });
    usePinsStore.setState({ pins: {}, skillPins: {}, contextPins: { tools: pin('drift') } });
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
    fireEvent.change(screen.getByLabelText('Approval reason'), { target: { value: 'ok' } });
    fireEvent.click(button);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    await waitFor(() => expect(approve).toHaveBeenCalledWith('tools', 'def', 'ok'));
  });

  it('confirms a diff of 50 or more changed lines', async () => {
    const body = (tag: string) => Array.from({ length: 25 }, (_, i) => `${tag} ${i}`).join('\n');
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue({
      fragment: 'tools',
      status: 'drift',
      composite_hash: 'def',
      old_document: body('old'),
      new_document: body('new'),
      findings: [],
    });
    const approve = vi.spyOn(api, 'approveContextPin').mockResolvedValue();
    vi.spyOn(api, 'fetchContextPins').mockResolvedValue({ tools: pin('pinned') });
    usePinsStore.setState({ pins: {}, skillPins: {}, contextPins: { tools: pin('drift') } });
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
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
    const dialog = screen.getByRole('alertdialog', { name: 'Approve tools' });
    expect(dialog).toHaveTextContent('This is a large change.');
    expect(approve).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Approve' }));
    await waitFor(() => expect(approve).toHaveBeenCalledWith('tools', 'def', undefined));
  });
});

describe('ContextPinReviewModal', () => {
  function renderModal(name = 'style', onClose = vi.fn()) {
    return {
      onClose,
      ...render(
        <MemoryRouter initialEntries={['/library?kind=context']}>
          <ContextPinReviewModal name={name} onClose={onClose} />
          <Routes>
            <Route path="*" element={<LocationProbe />} />
          </Routes>
        </MemoryRouter>,
      ),
    };
  }

  it('resets the pin when the confirm dialog is accepted', async () => {
    vi.spyOn(api, 'fetchContextPins').mockResolvedValue({ style: pin('drift') });
    const reset = vi.spyOn(api, 'resetContextPin').mockResolvedValue();
    const { onClose } = renderModal('style');
    fireEvent.click(await screen.findByRole('button', { name: 'Reset pin for style' }));
    // ConfirmDialog's confirm button carries the same label.
    const buttons = screen.getAllByRole('button', { name: 'Reset pin for style' });
    fireEvent.click(buttons[buttons.length - 1]);
    await waitFor(() =>
      expect(showToast).toHaveBeenCalledWith('success', 'Pin reset for style; it re-pins on the next context sync'),
    );
    expect(reset).toHaveBeenCalledWith('style');
    expect(onClose).toHaveBeenCalled();
  });

  it('shows a loading line, not a missing record, while the fetch is pending', () => {
    vi.spyOn(api, 'fetchContextPins').mockReturnValue(new Promise(() => {}));
    renderModal('style');
    expect(screen.getByText('Loading pin record…')).toBeInTheDocument();
    expect(screen.queryByText(/No pin record/)).not.toBeInTheDocument();
  });

  it('shows a failed state with Retry when the fetch fails', async () => {
    const fetch = vi.spyOn(api, 'fetchContextPins').mockRejectedValue(new Error('offline'));
    renderModal('style');
    expect(await screen.findByText('Pin record unavailable')).toBeInTheDocument();
    expect(screen.getByText('Load pin record failed: offline.')).toBeInTheDocument();
    expect(screen.queryByText(/No pin record/)).not.toBeInTheDocument();

    fetch.mockResolvedValue({ style: pin('drift') });
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByRole('button', { name: 'Reset pin for style' })).toBeInTheDocument();
    expect(screen.queryByText('Pin record unavailable')).not.toBeInTheDocument();
  });

  it('says there is no pin record only after a successful load without one, titled Pin for style', async () => {
    vi.spyOn(api, 'fetchContextPins').mockResolvedValue({});
    const { onClose } = renderModal('style');
    expect(await screen.findByText('No pin record for style.')).toBeInTheDocument();
    expect(screen.getByRole('dialog', { name: 'Pin for style' })).toBeInTheDocument();

    const openLink = screen.getByRole('link', { name: 'Open in Pins' });
    expect(openLink).toHaveAttribute('href', '/pins?kind=context&fragment=style');
    fireEvent.click(openLink);
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('location')).toHaveTextContent('/pins?kind=context&fragment=style');
  });

  it('titles pinned and drifted records accurately and links Open in Pins to the drift view', async () => {
    vi.spyOn(api, 'fetchContextPins').mockResolvedValue({ style: pin('pinned') });
    renderModal('style');
    expect(await screen.findByRole('dialog', { name: 'Pin for style' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open in Pins' })).toHaveAttribute(
      'href',
      '/pins?kind=context&fragment=style',
    );
    cleanup();

    vi.spyOn(api, 'fetchContextPins').mockResolvedValue({ style: pin('drift') });
    const { onClose } = renderModal('style');
    expect(await screen.findByRole('dialog', { name: 'style changed since its pin' })).toBeInTheDocument();
    const openLink = screen.getByRole('link', { name: 'Open in Pins' });
    expect(openLink).toHaveAttribute('href', '/pins?kind=context&fragment=style&view=drift');
    fireEvent.click(openLink);
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('location')).toHaveTextContent('/pins?kind=context&fragment=style&view=drift');
  });
});

function contextDiff(overrides: Partial<ContextPinsDiff> = {}): ContextPinsDiff {
  return {
    fragment: 'style',
    status: 'drift',
    composite_hash: 'abc',
    old_document: 'a\nb',
    new_document: 'a\nc',
    findings: [],
    ...overrides,
  };
}

function renderContext(entry: string) {
  return render(
    <CommandRegistryProvider>
      <MemoryRouter initialEntries={[entry]}>
        <Routes>
          <Route path="/pins" element={<PinsWorkspace />} />
        </Routes>
      </MemoryRouter>
    </CommandRegistryProvider>,
  );
}

function seed(pins: Record<string, ContextPin>) {
  usePinsStore.setState({ pins: {}, skillPins: {}, contextPins: pins });
}

// Diffs of thousands of lines can take longer than the 1s findBy default under coverage.
const LARGE_RENDER_TIMEOUT = 5000;

const lines = (n: number, last: string) =>
  Array.from({ length: n }, (_, i) => (i === n - 1 ? last : `line-${i}`)).join('\n');

describe('context pin detail review', () => {
  it('renders one line diff with prefixed lines and a changed-line count', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(contextDiff());
    seed({ style: pin('drift') });
    renderContext('/pins?kind=context&fragment=style');
    expect(await screen.findByText(/2 lines changed/)).toBeInTheDocument();
    expect(screen.getByText((_, el) => el?.tagName === 'SPAN' && (el.textContent ?? '').startsWith('- b'))).toBeInTheDocument();
    expect(screen.getByText((_, el) => el?.tagName === 'SPAN' && (el.textContent ?? '').startsWith('+ c'))).toBeInTheDocument();
  });

  it('shows a loading line with Approve disabled and never labels the pin as current', () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockReturnValue(new Promise(() => {}));
    seed({ style: pin('drift') });
    renderContext('/pins?kind=context&fragment=style');
    expect(screen.getByText('Loading diff…')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Approve' })).toBeDisabled();
    expect(screen.queryByText('Current document')).not.toBeInTheDocument();
  });

  it('shows an alert with Retry on a failed load and refetches', async () => {
    const fetchDiff = vi
      .spyOn(api, 'fetchContextPinDiff')
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(contextDiff());
    seed({ style: pin('drift') });
    renderContext('/pins?kind=context&fragment=style');
    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Failed to load the pin diff for style. offline');
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText(/2 lines changed/)).toBeInTheDocument();
    expect(fetchDiff).toHaveBeenCalledTimes(2);
  });

  it('reloads the diff after a 409 on approve', async () => {
    const fetchDiff = vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(contextDiff());
    vi.spyOn(api, 'approveContextPin').mockRejectedValue(new HTTPError(409, 'content changed'));
    seed({ style: pin('drift') });
    renderContext('/pins?kind=context&fragment=style');
    await screen.findByText(/2 lines changed/);
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    await waitFor(() => expect(fetchDiff).toHaveBeenCalledTimes(2));
    expect(showToast).toHaveBeenCalledWith('error', expect.stringContaining('Reloading the diff for re-review.'));
  });

  it('toasts the refusal and does not reload the diff after a 400 on approve', async () => {
    const refusal = 'evidence reference looks like a credential';
    const fetchDiff = vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(contextDiff());
    const approve = vi.spyOn(api, 'approveContextPin').mockRejectedValue(new HTTPError(400, refusal));
    seed({ style: pin('drift') });
    renderContext('/pins?kind=context&fragment=style');
    await screen.findByText(/2 lines changed/);
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    await waitFor(() => expect(approve).toHaveBeenCalled());
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('error', refusal, { detail: refusal }));
    expect(fetchDiff).toHaveBeenCalledTimes(1);
  });

  it('sends evidence references with the approval', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(
      contextDiff({ fragment: 'tools', composite_hash: 'def' }),
    );
    const approve = vi.spyOn(api, 'approveContextPin').mockResolvedValue();
    vi.spyOn(api, 'fetchContextPins').mockResolvedValue({ tools: pin('pinned') });
    seed({ tools: pin('drift') });
    renderContext('/pins?kind=context&fragment=tools');
    const button = await screen.findByRole('button', { name: 'Approve' });
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.change(screen.getByLabelText('Evidence references, one per line'), {
      target: { value: 'ref-1\nref-2' },
    });
    fireEvent.click(button);
    await waitFor(() => expect(approve).toHaveBeenCalledWith('tools', 'def', undefined, ['ref-1', 'ref-2']));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Approved content update for tools'));
  });

  it('carries the shared ids and scrolls for view=drift', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(contextDiff());
    seed({ style: pin('drift') });
    const scrollSpy = vi.fn();
    const original = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = scrollSpy;
    try {
      renderContext('/pins?kind=context&fragment=style&view=drift');
      await screen.findByText(/2 lines changed/);
      expect(document.getElementById(APPROVE_BUTTON_ID)).not.toBeNull();
      expect(document.getElementById(DRIFT_SECTION_ID)).not.toBeNull();
      expect(document.getElementById(RESET_BUTTON_ID)).not.toBeNull();
      expect(document.getElementById(TOOLS_SECTION_ID)).not.toBeNull();
      await waitFor(() => expect(scrollSpy).toHaveBeenCalled());
      expect(scrollSpy.mock.contexts.some((el) => (el as Element).id === DRIFT_SECTION_ID)).toBe(true);
    } finally {
      Element.prototype.scrollIntoView = original;
    }
  });

  it('scrolls view=findings to the drift section for a drifted fragment', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(contextDiff());
    seed({ style: pin('drift') });
    const scrollSpy = vi.fn();
    const original = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = scrollSpy;
    try {
      renderContext('/pins?kind=context&fragment=style&view=findings');
      await waitFor(() => expect(scrollSpy).toHaveBeenCalled());
      expect((scrollSpy.mock.contexts[0] as Element).id).toBe(DRIFT_SECTION_ID);
    } finally {
      Element.prototype.scrollIntoView = original;
    }
  });

  it('scrolls view=findings to the pinned-content findings on a pinned fragment', async () => {
    const findings = [{ code: 'P003', severity: 'info' as const, confidence: 'low' as const, field: 'body', message: 'note' }];
    seed({ style: pin('pinned', findings) });
    const scrollSpy = vi.fn();
    const original = Element.prototype.scrollIntoView;
    Element.prototype.scrollIntoView = scrollSpy;
    try {
      renderContext('/pins?kind=context&fragment=style&view=findings');
      await waitFor(() => expect(scrollSpy).toHaveBeenCalled());
      const target = scrollSpy.mock.contexts[0] as Element;
      expect(target.id).toBe(TOOLS_SECTION_ID);
      expect(target).toHaveTextContent('Advisory findings on the pinned content');
      expect(target).toHaveTextContent('Pinned document');
    } finally {
      Element.prototype.scrollIntoView = original;
    }
  });

  it('approves with Ctrl+Enter through the Approve button', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(contextDiff());
    const approve = vi.spyOn(api, 'approveContextPin').mockResolvedValue();
    vi.spyOn(api, 'fetchContextPins').mockResolvedValue({ style: pin('pinned') });
    seed({ style: pin('drift') });
    renderContext('/pins?kind=context&fragment=style');
    const button = await screen.findByRole('button', { name: 'Approve' });
    await waitFor(() => expect(button).toBeEnabled());
    expect(fireEvent.keyDown(document, { key: 'Enter', ctrlKey: true })).toBe(false);
    await waitFor(() => expect(approve).toHaveBeenCalledWith('style', 'abc', undefined));
    expect(approve).toHaveBeenCalledTimes(1);
  });

  it('runs the palette approve and reset commands on the selected fragment', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(contextDiff());
    const approve = vi.spyOn(api, 'approveContextPin').mockResolvedValue();
    vi.spyOn(api, 'fetchContextPins').mockResolvedValue({ style: pin('drift') });
    seed({ style: pin('drift') });
    function CommandProbe() {
      const { commands } = useCommandRegistry();
      return (
        <div>
          {commands
            .filter((c) => c.id === 'pins:approve' || c.id === 'pins:reset')
            .map((c) => (
              <button key={c.id} data-testid={c.id} onClick={c.onSelect}>
                {c.label}
              </button>
            ))}
        </div>
      );
    }
    const run = (id: string) => fireEvent.click(screen.getByTestId(id));
    render(
      <CommandRegistryProvider>
        <MemoryRouter initialEntries={['/pins?kind=context&fragment=style']}>
          <Routes>
            <Route path="/pins" element={<PinsWorkspace />} />
          </Routes>
        </MemoryRouter>
        <CommandProbe />
      </CommandRegistryProvider>,
    );
    const button = await screen.findByRole('button', { name: 'Approve' });
    await waitFor(() => expect(button).toBeEnabled());
    expect(screen.getByTestId('pins:reset')).toHaveTextContent('Pins: Reset Selected Pin');
    run('pins:approve');
    await waitFor(() => expect(approve).toHaveBeenCalledWith('style', 'abc', undefined));
    run('pins:reset');
    expect(await screen.findByRole('alertdialog')).toHaveTextContent('cannot be undone');
    expect(showToast).not.toHaveBeenCalledWith('error', expect.stringMatching(/selected/));
  });

  it('renders approval history and the verified time', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(contextDiff());
    seed({
      style: {
        ...pin('drift'),
        approvals: [
          { approved_at: '2026-07-10T00:00:00Z', approved_hash: 'c1:abc', bound: true, reason: 'Reviewed style edit' },
        ],
      },
    });
    renderContext('/pins?kind=context&fragment=style');
    const history = await screen.findByRole('region', { name: 'Approval history' });
    expect(history).toHaveTextContent('Reviewed style edit');
    expect(screen.getByTitle('2026-07-15T00:00:00Z')).toHaveTextContent(/^verified /);
  });

  it('resets from the Danger section after a confirm that warns it cannot be undone', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(contextDiff());
    const reset = vi.spyOn(api, 'resetContextPin').mockResolvedValue();
    vi.spyOn(api, 'fetchContextPins').mockResolvedValue({});
    seed({ style: pin('drift') });
    renderContext('/pins?kind=context&fragment=style');
    fireEvent.click(await screen.findByRole('button', { name: 'Reset pin for style' }));
    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent('cannot be undone');
    expect(dialog).toHaveTextContent('re-pins from scratch');
    const buttons = screen.getAllByRole('button', { name: 'Reset pin for style' });
    fireEvent.click(buttons[buttons.length - 1]);
    await waitFor(() => expect(reset).toHaveBeenCalledWith('style'));
    await waitFor(() =>
      expect(showToast).toHaveBeenCalledWith('success', 'Pin reset for style; it re-pins on the next context sync'),
    );
  });

  it('toasts a failed reset and keeps the detail mounted', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(contextDiff());
    vi.spyOn(api, 'resetContextPin').mockRejectedValue(new Error('disk full'));
    const unhandled = vi.fn();
    process.on('unhandledRejection', unhandled);
    try {
      seed({ style: pin('drift') });
      renderContext('/pins?kind=context&fragment=style');
      fireEvent.click(await screen.findByRole('button', { name: 'Reset pin for style' }));
      const buttons = screen.getAllByRole('button', { name: 'Reset pin for style' });
      fireEvent.click(buttons[buttons.length - 1]);
      await waitFor(() => expect(showToast).toHaveBeenCalledWith('error', expect.stringContaining('disk full')));
      expect(screen.getByRole('button', { name: 'Reset pin for style' })).toBeInTheDocument();
      await new Promise((r) => setTimeout(r, 0));
      expect(unhandled).not.toHaveBeenCalled();
    } finally {
      process.off('unhandledRejection', unhandled);
    }
  });

  it('offers the full current document past the render cap and clears it during a 409 reload', async () => {
    const big = Array.from({ length: MAX_DIFF_LINES + 10 }, (_, i) => `n${i}`).join('\n');
    const fetchDiff = vi
      .spyOn(api, 'fetchContextPinDiff')
      .mockResolvedValueOnce(contextDiff({ old_document: '', new_document: big }))
      .mockReturnValue(new Promise(() => {}));
    vi.spyOn(api, 'approveContextPin').mockRejectedValue(new HTTPError(409, 'content changed'));
    seed({ style: pin('drift') });
    renderContext('/pins?kind=context&fragment=style');
    expect(
      await screen.findByText(/Full current document \(\d+ lines\)/, undefined, { timeout: LARGE_RENDER_TIMEOUT }),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    const dialog = screen.getByRole('alertdialog', { name: 'Approve style' });
    expect(dialog).toHaveTextContent('The diff shown is truncated');
    expect(dialog).toHaveTextContent('gridctl ctx pins diff style');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Approve' }));
    await waitFor(() => expect(fetchDiff).toHaveBeenCalledTimes(2));
    expect(await screen.findByText('Loading diff…')).toBeInTheDocument();
    expect(screen.queryByText(/Full current document/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Diff truncated/)).not.toBeInTheDocument();
  });

  it('labels an unaligned diff and offers the full current document', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(
      contextDiff({ old_document: lines(501, 'old-end'), new_document: lines(501, 'new-end') }),
    );
    seed({ style: pin('drift') });
    renderContext('/pins?kind=context&fragment=style');
    expect(
      await screen.findByText('style (too large to align)', undefined, { timeout: LARGE_RENDER_TIMEOUT }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/lines changed/)).not.toBeInTheDocument();
    expect(screen.getByText(/too large to compare line by line/)).toBeInTheDocument();
    expect(await screen.findByText('Full current document (501 lines)')).toBeInTheDocument();
  });

  it('shows the equal-content note when both documents are absent', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(
      contextDiff({ old_document: undefined, new_document: undefined }),
    );
    seed({ style: pin('drift') });
    renderContext('/pins?kind=context&fragment=style');
    expect(await screen.findByText(/The current content matches the pinned document/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Approve' })).toBeEnabled();
  });

  it('escapes a non-printable fragment name in the header and diff heading', async () => {
    const name = 'sty\u0007le';
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(contextDiff({ fragment: name }));
    seed({ [name]: pin('drift') });
    renderContext(`/pins?kind=context&fragment=${encodeURIComponent(name)}`);
    expect(await screen.findByText(/2 lines changed/)).toHaveTextContent('sty\\u0007le (2 lines changed)');
    expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent('sty\\u0007le');
    expect(screen.getByRole('heading', { level: 2 }).textContent).not.toContain('\u0007');
    expect(screen.getByRole('button', { name: 'Reset pin for sty\\u0007le' })).toBeInTheDocument();
  });
});

describe('ContextPinReviewModal detail', () => {
  it('renders no shared ids and reflects an in-modal approve in its title', async () => {
    vi.spyOn(api, 'fetchContextPinDiff').mockResolvedValue(contextDiff());
    vi.spyOn(api, 'approveContextPin').mockResolvedValue();
    const fetchPins = vi
      .spyOn(api, 'fetchContextPins')
      .mockResolvedValueOnce({ style: pin('drift') })
      .mockResolvedValue({ style: pin('pinned') });
    render(
      <MemoryRouter initialEntries={['/library?kind=context']}>
        <ContextPinReviewModal name="style" onClose={vi.fn()} />
      </MemoryRouter>,
    );
    expect(await screen.findByText('style changed since its pin')).toBeInTheDocument();
    await screen.findByText(/2 lines changed/);
    for (const id of [APPROVE_BUTTON_ID, DRIFT_SECTION_ID, RESET_BUTTON_ID, TOOLS_SECTION_ID]) {
      expect(document.getElementById(id)).toBeNull();
    }
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    await waitFor(() => expect(fetchPins).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(screen.queryByText('Pin drift')).not.toBeInTheDocument());
    expect(screen.queryByText(/changed since its pin/)).not.toBeInTheDocument();
    expect(screen.getByText('Pin for style')).toBeInTheDocument();
  });
});
