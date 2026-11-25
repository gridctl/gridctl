import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, useLocation } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { PinsWorkspace } from '../components/workspaces/PinsWorkspace';
import { requestGatewayPoll } from '../hooks/usePolling';
import { usePinsStore } from '../stores/usePinsStore';
import { useUIStore } from '../stores/useUIStore';
import * as api from '../lib/api';
import { HTTPError, type ServerPins } from '../lib/api';
import type { HookPin, HookPinDiff } from '../types';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn(), copyWithToast: vi.fn() }));
vi.mock('../hooks/usePolling', () => ({
  requestGatewayPoll: vi.fn(),
  usePolling: () => ({ refresh: vi.fn() }),
}));

import { showToast } from '../components/ui/Toast';

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

function hookPin(status: HookPin['status'], overrides: Partial<HookPin> = {}): HookPin {
  return {
    definition_hash: 'sha256:def',
    files: [{ path: 'scripts/guard.sh', digest: 'sha256:0123456789abcdef' }],
    source: 'git',
    origin: { repo: 'https://github.com/acme/guards', ref: 'main' },
    pinned_at: '2026-07-01T00:00:00Z',
    last_verified_at: '2026-07-15T00:00:00Z',
    status,
    ...overrides,
  };
}

const driftDiff: HookPinDiff = {
  hook: 'block-force-push',
  status: 'drift',
  composite_hash: 'composite-1',
  old_document: '{"name":"block-force-push"}',
  new_document: '{"name":"block-force-push","description":"edited"}',
  added_files: [],
  removed_files: [],
  modified_files: ['hook.json'],
  findings: [],
};

const pinnedDiff: HookPinDiff = {
  ...driftDiff,
  status: 'pinned',
  new_document: driftDiff.old_document,
  modified_files: [],
};

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function lastLocation(): string {
  return screen.getByTestId('location').textContent ?? '';
}

function renderWorkspace(initialEntry = '/pins?kind=hook') {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <CommandRegistryProvider>
        <PinsWorkspace />
        <LocationProbe />
      </CommandRegistryProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  usePinsStore.setState({
    pins: { github: serverPins('pinned') },
    hookPins: {
      'audit-log': hookPin('pinned'),
      'block-force-push': hookPin('drift'),
    },
  });
  vi.spyOn(api, 'fetchHookPinDiff').mockResolvedValue(driftDiff);
  vi.spyOn(api, 'fetchHookPins').mockResolvedValue({});
});

afterEach(() => {
  usePinsStore.setState({ pins: null, skillPins: null, hookPins: null, hookPinsError: null });
  useUIStore.setState({ pinsPrefs: { attentionOnly: null, findingsOnly: false } });
  vi.mocked(showToast).mockClear();
  vi.restoreAllMocks();
});

describe('PinsWorkspace hooks kind', () => {
  it('shows a pressed Hooks toggle, lists drifted hooks first, and mounts the hook detail', async () => {
    renderWorkspace('/pins?kind=hook&attention=0');

    expect(screen.getByRole('button', { name: 'Hooks' })).toHaveAttribute('aria-pressed', 'true');
    const rail = screen.getByRole('complementary', { name: 'Pins rail' });
    const rows = within(rail).getAllByRole('button', { name: /^(audit-log|block-force-push)/ });
    expect(within(rows[0]).getByText('block-force-push')).toBeInTheDocument();
    expect(within(rows[1]).getByText('audit-log')).toBeInTheDocument();
    expect(within(rows[0]).getByText(/https:\/\/github\.com\/acme\/guards · verified/)).toBeInTheDocument();
    expect(screen.getByText('2 hooks pinned · 1 drifted')).toBeInTheDocument();
    expect(await screen.findByText('Pin status:')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'block-force-push' })).toBeInTheDocument();
    expect(screen.getByText(/Imported: https:\/\/github\.com\/acme\/guards@main/)).toBeInTheDocument();
    expect(screen.getByText(/projection of the new content is skipped/)).toBeInTheDocument();
    expect(screen.getByText('Pinned file digests')).toBeInTheDocument();
    expect(screen.getByText('scripts/guard.sh')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Open in Library' })).toHaveAttribute(
      'href',
      '/library?kind=hook&selected=block-force-push',
    );
    await waitFor(() => expect(lastLocation()).toContain('hook=block-force-push'));
    expect(lastLocation()).not.toContain('server=');
  });

  it('switches to the hook kind from the toggle', async () => {
    renderWorkspace('/pins');
    fireEvent.click(screen.getByRole('button', { name: 'Hooks' }));
    await waitFor(() => expect(lastLocation()).toContain('kind=hook'));
    await waitFor(() => expect(lastLocation()).toContain('hook=block-force-push'));
  });

  it('treats an unknown kind as servers', () => {
    renderWorkspace('/pins?kind=bogus');
    expect(screen.getByRole('button', { name: 'Servers' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'Hooks' })).toHaveAttribute('aria-pressed', 'false');
  });

  it('filters to drift and findings under Attention and keeps a named clean hook visible', async () => {
    usePinsStore.setState({
      hookPins: {
        clean: hookPin('pinned'),
        drifted: hookPin('drift'),
        flagged: hookPin('pinned', {
          findings: [{ code: 'hook-scan', severity: 'warn', confidence: 'high', field: 'f', message: 'm' }],
        }),
        quiet: hookPin('pinned'),
      },
    });
    renderWorkspace('/pins?kind=hook&hook=clean');

    const rail = screen.getByRole('complementary', { name: 'Pins rail' });
    expect(screen.getByRole('button', { name: 'Attention' })).toHaveAttribute('aria-pressed', 'true');
    expect(within(rail).getByRole('button', { name: /^drifted/ })).toBeInTheDocument();
    expect(within(rail).getByRole('button', { name: /^flagged/ })).toBeInTheDocument();
    expect(within(rail).queryByRole('button', { name: /^quiet/ })).not.toBeInTheDocument();
    expect(within(rail).getByRole('button', { name: /^clean/ })).toHaveAttribute(
      'title',
      'Not in the attention filter',
    );
    expect(screen.getByText('4 hooks pinned · 1 drifted · 1 with findings')).toBeInTheDocument();
  });

  it('shows the empty state for an empty map', () => {
    usePinsStore.setState({ hookPins: {} });
    renderWorkspace();
    expect(screen.getByText('No hooks pinned yet')).toBeInTheDocument();
  });

  it('shows the loading state before the first hook pins poll', () => {
    usePinsStore.setState({ hookPins: null });
    renderWorkspace();
    expect(screen.getByText(/Loading pins/)).toBeInTheDocument();
  });

  it('shows a failed state with Retry when the first fetch failed', () => {
    usePinsStore.setState({ hookPins: null, hookPinsError: 'hook pins down' });
    renderWorkspace();
    expect(screen.getByText('Pins unavailable')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(requestGatewayPoll).toHaveBeenCalled();
  });

  it('approves with evidence, carries the approve id only while drifted, and refreshes', async () => {
    const approve = vi.spyOn(api, 'approveHookPin').mockResolvedValue(undefined);
    vi.mocked(api.fetchHookPinDiff).mockReset();
    vi.mocked(api.fetchHookPinDiff).mockResolvedValueOnce(driftDiff).mockResolvedValue(pinnedDiff);
    const refreshed = { 'audit-log': hookPin('pinned'), 'block-force-push': hookPin('pinned') };
    vi.mocked(api.fetchHookPins).mockResolvedValue(refreshed);
    renderWorkspace('/pins?kind=hook&hook=block-force-push&attention=0');

    const button = await screen.findByRole('button', { name: 'Approve' });
    await waitFor(() => expect(button).toHaveAttribute('id', 'pins-approve-button'));
    expect(document.getElementById('pins-drift-section')).not.toBeNull();

    fireEvent.change(screen.getByLabelText('Evidence references, one per line'), {
      target: { value: 'https://example.test/pull/1\n\nhttps://example.test/pull/1\nTICKET-2' },
    });
    fireEvent.click(button);

    await waitFor(() =>
      expect(approve).toHaveBeenCalledWith('block-force-push', 'composite-1', '', [
        'https://example.test/pull/1',
        'TICKET-2',
      ]),
    );
    await waitFor(() => expect(api.fetchHookPinDiff).toHaveBeenCalledTimes(2));
    expect(await screen.findByText('pinned')).toBeInTheDocument();
    expect(screen.getByText('hook.json unchanged')).toBeInTheDocument();
    await waitFor(() => expect(usePinsStore.getState().hookPins).toEqual(refreshed));
    expect(screen.getByRole('button', { name: 'Approve' })).not.toHaveAttribute('id');
  });

  it('does not put the approve id on a pinned hook', async () => {
    vi.mocked(api.fetchHookPinDiff).mockResolvedValue(pinnedDiff);
    renderWorkspace('/pins?kind=hook&hook=audit-log&attention=0');
    expect(await screen.findByText('pinned')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Approve' })).not.toHaveAttribute('id');
  });

  it('reloads the diff on a 409 and keeps typed input on a 400', async () => {
    const approve = vi
      .spyOn(api, 'approveHookPin')
      .mockRejectedValueOnce(new HTTPError(400, 'evidence reference is too long'))
      .mockRejectedValueOnce(new HTTPError(409, 'hook changed since review'));
    renderWorkspace('/pins?kind=hook&hook=block-force-push');

    const button = await screen.findByRole('button', { name: 'Approve' });
    await waitFor(() => expect(button).toBeEnabled());
    const evidence = screen.getByLabelText('Evidence references, one per line');
    fireEvent.change(evidence, { target: { value: 'ref-1' } });
    fireEvent.click(button);
    await waitFor(() =>
      expect(showToast).toHaveBeenCalledWith('error', 'evidence reference is too long', {
        detail: 'evidence reference is too long',
      }),
    );
    expect(api.fetchHookPinDiff).toHaveBeenCalledTimes(1);
    expect(evidence).toHaveValue('ref-1');

    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.click(button);
    await waitFor(() => expect(approve).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(api.fetchHookPinDiff).toHaveBeenCalledTimes(2));
  });

  it('escapes hidden characters from the server in the hook detail', async () => {
    const name = 'guard\u202ehook';
    usePinsStore.setState({
      hookPins: {
        [name]: hookPin('drift', {
          origin: { repo: 'https://ex\u200bample.test/repo', ref: 'ma\u202ein' },
          approved_reason: 'ok\u200b',
          files: [{ path: 'scripts/\u202erun.sh', digest: 'sha256:abc' }],
        }),
      },
    });
    vi.mocked(api.fetchHookPinDiff).mockResolvedValue({
      ...driftDiff,
      hook: name,
      new_document: '{"name":"x\u200b"}',
      modified_files: ['scripts/\u202erun.sh'],
    });
    renderWorkspace(`/pins?kind=hook&hook=${encodeURIComponent(name)}`);

    expect(await screen.findByLabelText('Hook pin diff')).toBeInTheDocument();
    const text = document.body.textContent ?? '';
    expect(text).not.toContain('\u202e');
    expect(text).not.toContain('\u200b');
    expect(text).toContain('\\u202e');
  });
});
