import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { PackUpdateDialog } from '../components/registry/packs/PackUpdateDialog';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    previewPack: vi.fn(),
    addPack: vi.fn(),
    fetchVariables: vi.fn().mockResolvedValue([{ key: 'GIT_TOKEN', secret: true }]),
    createVariable: vi.fn(),
  };
});

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

import { previewPack, addPack, HTTPError, PackFindingsError } from '../lib/api';
import { showToast } from '../components/ui/Toast';

const mockPreview = vi.mocked(previewPack);
const mockAdd = vi.mocked(addPack);

const resolved = {
  pack: 'team-pack',
  version: '1.0.0',
  wiring: false,
  skills: [],
  agents: [],
  rules: [],
};

const origin = {
  source: 'team-pack',
  repo: 'https://github.com/acme/team-pack',
  ref: 'main',
  commit_sha: 'abc123',
  fetched_at: '2026-08-17T00:00:00Z',
};

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function renderDialog(over: Partial<typeof origin> = {}) {
  return render(
    <MemoryRouter initialEntries={['/library?kind=pack']}>
      <PackUpdateDialog
        packName="team-pack"
        origin={{ ...origin, ...over }}
        onClose={vi.fn()}
        onUpdated={vi.fn()}
      />
      <Routes>
        <Route path="*" element={<LocationProbe />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('PackUpdateDialog', () => {
  beforeEach(() => {
    mockPreview.mockReset();
    mockAdd.mockReset();
  });

  /**
   * The whole point of the server-side stored-reference fallback: a pack
   * imported with a vault reference resolves here with no user input, because
   * the request deliberately omits `auth` and the server re-resolves what it
   * recorded at import.
   */
  it('previews a vault-backed private pack with no input, sending no auth field', async () => {
    mockPreview.mockResolvedValueOnce(resolved);
    renderDialog();

    await waitFor(() => expect(screen.getByText(/0 skills, 0 agents/)).toBeInTheDocument());
    expect(mockPreview).toHaveBeenCalledTimes(1);
    expect(mockPreview.mock.calls[0][0]).toMatchObject({
      repo: origin.repo,
      ref: 'main',
    });
    expect(mockPreview.mock.calls[0][0].auth).toBeUndefined();

    // Update is reachable, which it was not when a private pack dead-ended.
    expect(screen.getByRole('button', { name: /update from origin/i })).toBeEnabled();
  });

  it('offers a credentials recovery path when the stored reference cannot cover it', async () => {
    mockPreview.mockRejectedValueOnce(new HTTPError(401, 'Authentication required'));
    renderDialog();

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/authentication/i));
    // The dead end is gone: there is now something to do about it.
    expect(screen.getByRole('button', { name: /authentication/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /retry with credentials/i })).toBeInTheDocument();
  });

  it('retries the preview with the credential the user supplied', async () => {
    mockPreview
      .mockRejectedValueOnce(new HTTPError(401, 'Authentication required'))
      .mockResolvedValueOnce(resolved);
    renderDialog();

    await waitFor(() => screen.getByRole('button', { name: /retry with credentials/i }));

    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    fireEvent.click(screen.getByLabelText(/paste token/i));
    fireEvent.change(screen.getByPlaceholderText(/personal access token/i), {
      target: { value: 'ghp_dialog' },
    });
    fireEvent.click(screen.getByRole('button', { name: /retry with credentials/i }));

    await waitFor(() => expect(mockPreview).toHaveBeenCalledTimes(2));
    expect(mockPreview.mock.calls[1][0].auth).toEqual({
      method: 'token',
      token: 'ghp_dialog',
    });
    await waitFor(() => expect(screen.getByText(/0 skills, 0 agents/)).toBeInTheDocument());
    expect(screen.getByRole('button', { name: /update from origin/i })).toBeEnabled();
  });

  it('carries the supplied credential into the update request', async () => {
    mockPreview
      .mockRejectedValueOnce(new HTTPError(401, 'Authentication required'))
      .mockResolvedValueOnce(resolved);
    mockAdd.mockResolvedValueOnce({ doc: { pack: 'team-pack' }, notes: [] } as never);
    renderDialog();

    await waitFor(() => screen.getByRole('button', { name: /retry with credentials/i }));
    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    fireEvent.click(screen.getByLabelText(/paste token/i));
    fireEvent.change(screen.getByPlaceholderText(/personal access token/i), {
      target: { value: 'ghp_dialog' },
    });
    fireEvent.click(screen.getByRole('button', { name: /retry with credentials/i }));

    await waitFor(() => screen.getByText(/0 skills, 0 agents/));
    fireEvent.click(screen.getByRole('button', { name: /update from origin/i }));

    await waitFor(() => expect(mockAdd).toHaveBeenCalledTimes(1));
    expect(mockAdd.mock.calls[0][0].auth).toEqual({ method: 'token', token: 'ghp_dialog' });
  });

  /**
   * A missing agent is auth-shaped but no token can fix it, so the dialog must
   * not offer a credentials field for it.
   */
  it('does not offer credentials for an unreachable ssh-agent, but does offer a remedy', async () => {
    mockPreview.mockRejectedValueOnce(
      new HTTPError(422, 'Pack preview failed: ssh agent not available', {
        code: 'ssh_agent_unavailable',
        httpsEquivalent: 'https://github.com/acme/team-pack',
      }),
    );
    renderDialog({ repo: 'git@github.com:acme/team-pack.git' });

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/ssh agent/i));
    // A token cannot authenticate an SSH URL.
    expect(screen.queryByRole('button', { name: /retry with credentials/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^authentication/i })).not.toBeInTheDocument();

    // But the dialog must not dead-end: it names the cause and the HTTPS URL.
    expect(screen.getByText(/no ssh-agent reachable/i)).toBeInTheDocument();
    expect(screen.getByText('https://github.com/acme/team-pack')).toBeInTheDocument();
    // It must not claim it can switch protocol itself — that would repoint the
    // origin this pack tracks.
    expect(screen.getByText(/re-import the pack over https/i)).toBeInTheDocument();
  });

  it('falls back to the agent instructions when no HTTPS equivalent was derived', async () => {
    mockPreview.mockRejectedValueOnce(
      new HTTPError(422, 'Pack preview failed: ssh agent not available', {
        code: 'ssh_agent_unavailable',
      }),
    );
    renderDialog({ repo: 'ssh://git@internal/acme/pack.git' });

    await waitFor(() => expect(screen.getByText(/no ssh-agent reachable/i)).toBeInTheDocument());
    // No URL to offer, so no copy affordance and no misleading instruction.
    expect(screen.queryByRole('button', { name: /copy the https url/i })).not.toBeInTheDocument();
    expect(screen.getByText(/restart the daemon/i)).toBeInTheDocument();
  });

  it('keeps Update disabled while the manifest has not resolved', async () => {
    mockPreview.mockRejectedValueOnce(new HTTPError(500, 'Pack preview failed: boom'));
    renderDialog();

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/boom/i));
    expect(screen.getByRole('button', { name: /update from origin/i })).toBeDisabled();
  });
});

/**
 * The vault picker portals its dropdown to document.body, which puts it outside
 * the dialog's focus-trap container and outside the dialog's Escape handler.
 * Both of those defaults are actively wrong for a picker that belongs to the
 * dialog, and this is the recommended recovery path for a private pack imported
 * with a one-off token, so it has to be keyboard-reachable.
 */
describe('PackUpdateDialog — vault picker keyboard handling', () => {
  beforeEach(() => {
    mockPreview.mockReset();
    mockAdd.mockReset();
  });

  async function openVaultPicker(onClose = vi.fn()) {
    mockPreview.mockRejectedValueOnce(new HTTPError(401, 'Authentication required'));
    render(
      <MemoryRouter>
        <PackUpdateDialog
          packName="team-pack"
          origin={origin}
          onClose={onClose}
          onUpdated={vi.fn()}
        />
      </MemoryRouter>,
    );
    await waitFor(() => screen.getByRole('button', { name: /retry with credentials/i }));
    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    fireEvent.click(screen.getByTitle('Insert variable'));
    const filter = await waitFor(() => screen.getByPlaceholderText(/filter variables/i));
    return { filter, onClose };
  }

  it('Escape closes the picker, not the dialog', async () => {
    const { filter, onClose } = await openVaultPicker();

    fireEvent.keyDown(filter, { key: 'Escape' });

    expect(onClose).not.toHaveBeenCalled();
    // The picker itself closed, so Escape did something visible.
    await waitFor(() =>
      expect(screen.queryByPlaceholderText(/filter variables/i)).not.toBeInTheDocument(),
    );
  });

  // Asserted on the event rather than on resulting focus: the dialog's focus
  // trap only ever moves focus by calling preventDefault first, so an
  // uncancelled Tab means the trap never ran. Checking document.activeElement
  // instead would be order-dependent, because jsdom reports offsetParent as
  // null for every element, which changes which branch of the trap fires.
  it('Tab is left alone, so the trap cannot yank focus out of the picker', async () => {
    const { filter } = await openVaultPicker();

    const notCancelled = fireEvent.keyDown(filter, { key: 'Tab' });

    expect(notCancelled).toBe(true);
  });

  it('Shift+Tab is left alone too', async () => {
    const { filter } = await openVaultPicker();

    const notCancelled = fireEvent.keyDown(filter, { key: 'Tab', shiftKey: true });

    expect(notCancelled).toBe(true);
  });
});

describe('PackUpdateDialog held hooks', () => {
  beforeEach(() => {
    mockPreview.mockReset();
    mockAdd.mockReset();
    vi.mocked(showToast).mockReset();
  });

  it('warns rather than reporting a clean update when a hook is held', async () => {
    mockPreview.mockResolvedValueOnce(resolved);
    mockAdd.mockResolvedValueOnce({
      doc: {
        pack: 'team-pack',
        skills: [],
        agents: [],
        wiring: false,
        held_hooks: [{ name: 'block-force-push', composite_hash: 'sha256:abc' }],
      },
      notes: [],
    });
    renderDialog();

    await waitFor(() => screen.getByText(/0 skills, 0 agents/));
    fireEvent.click(screen.getByRole('button', { name: /update from origin/i }));

    await waitFor(() => expect(mockAdd).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(showToast).toHaveBeenCalledWith(
        'warning',
        'Updated team-pack; 1 hook held for pin approval',
        expect.objectContaining({ action: expect.objectContaining({ label: 'Review' }) }),
      ),
    );
    expect(showToast).not.toHaveBeenCalledWith('success', 'Updated team-pack from its origin');
  });

  it('links the Review action to the alphabetically first held hook', async () => {
    mockPreview.mockResolvedValueOnce(resolved);
    mockAdd.mockResolvedValueOnce({
      doc: {
        pack: 'team-pack',
        skills: [],
        agents: [],
        wiring: false,
        held_hooks: [
          { name: 'b', composite_hash: 'sha256:b' },
          { name: 'a', composite_hash: 'sha256:a' },
        ],
      },
      notes: [],
    });
    renderDialog();

    await waitFor(() => screen.getByText(/0 skills, 0 agents/));
    fireEvent.click(screen.getByRole('button', { name: /update from origin/i }));

    await waitFor(() =>
      expect(showToast).toHaveBeenCalledWith(
        'warning',
        'Updated team-pack; 2 hooks held for pin approval',
        expect.anything(),
      ),
    );
    const call = vi
      .mocked(showToast)
      .mock.calls.find(([, message]) => message === 'Updated team-pack; 2 hooks held for pin approval');
    act(() => call?.[2]?.action?.onClick());
    expect(screen.getByTestId('location')).toHaveTextContent('/pins?kind=hook&hook=a&view=drift');
  });

  it('calls the toast with two arguments when no hook is held', async () => {
    mockPreview.mockResolvedValueOnce(resolved);
    mockAdd.mockResolvedValueOnce({
      doc: { pack: 'team-pack', skills: [], agents: [], wiring: false },
      notes: [],
    });
    renderDialog();

    await waitFor(() => screen.getByText(/0 skills, 0 agents/));
    fireEvent.click(screen.getByRole('button', { name: /update from origin/i }));

    await waitFor(() => expect(showToast).toHaveBeenCalled());
    const call = vi
      .mocked(showToast)
      .mock.calls.find(([, message]) => message === 'Updated team-pack from its origin');
    expect(call).toHaveLength(2);
  });
});

describe('PackUpdateDialog security findings', () => {
  beforeEach(() => {
    mockPreview.mockReset();
    mockAdd.mockReset();
  });

  const flaggedPreview = {
    ...resolved,
    skills: [
      {
        kind: 'skill',
        name: 'deploy-helper',
        blocking: true,
        source: 'upstream',
        findings: [
          {
            stepId: 'SKILL.md:line-12',
            pattern: 'curl.*\\|\\s*sh',
            description: 'piped shell download',
            severity: 'danger',
          },
          { stepId: 'SKILL.md:line-30', pattern: 'sudo\\s', description: 'uses sudo', severity: 'warning' },
        ],
      },
    ],
  };

  it('shows each finding under its resource with severity classes before trust', async () => {
    mockPreview.mockResolvedValueOnce(flaggedPreview);
    renderDialog();

    const list = await screen.findByLabelText('Security findings on skill/deploy-helper');
    const items = within(list).getAllByRole('listitem');
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent('piped shell download');
    expect(items[0]).toHaveTextContent('SKILL.md:line-12');
    expect(items[0]).toHaveClass('text-status-error');
    expect(items[1]).toHaveTextContent('uses sudo');
    expect(items[1]).toHaveTextContent('SKILL.md:line-30');
    expect(items[1]).toHaveClass('text-status-pending');
    // The regex source is never rendered as if it were matched content.
    expect(list).not.toHaveTextContent('curl');
    expect(screen.getByText('skill/deploy-helper')).toBeInTheDocument();
    expect(screen.getByText('from upstream')).toBeInTheDocument();
    expect(screen.getByText('blocks import')).toBeInTheDocument();

    const update = screen.getByRole('button', { name: /update from origin/i });
    expect(update).toBeDisabled();
    fireEvent.click(screen.getByRole('checkbox'));
    expect(update).toBeEnabled();
  });

  it('shows the server findings in detail after a 409 and clears the trust checkbox', async () => {
    mockPreview.mockResolvedValueOnce(resolved);
    mockAdd.mockRejectedValueOnce(
      new PackFindingsError('Security findings', 'team-pack', [
        {
          kind: 'agent',
          name: 'reviewer',
          blocking: true,
          findings: [{ stepId: 'AGENT.md:line-3', description: 'hidden instruction', severity: 'danger' }],
        },
      ]),
    );
    renderDialog();

    const update = await screen.findByRole('button', { name: /update from origin/i });
    await waitFor(() => expect(update).toBeEnabled());
    fireEvent.click(update);

    const list = await screen.findByLabelText('Security findings on agent/reviewer');
    expect(list).toHaveTextContent('hidden instruction');
    expect(list).toHaveTextContent('AGENT.md:line-3');
    expect(within(list).getByRole('listitem')).toHaveClass('text-status-error');
    expect(screen.getByRole('checkbox')).not.toBeChecked();
    expect(update).toBeDisabled();
  });

  it('renders hidden characters in a finding description as visible escapes', async () => {
    mockPreview.mockResolvedValueOnce({
      ...resolved,
      rules: [
        {
          kind: 'rule',
          name: 'style',
          findings: [{ stepId: 'rule.md:line-1', description: 'hidden\u200bpayload\u202e', severity: 'warning' }],
        },
      ],
    });
    renderDialog();

    const list = await screen.findByLabelText('Security findings on rule/style');
    expect(list).toHaveTextContent('hidden\\u200bpayload\\u202e');
    expect(document.body.textContent).not.toContain('\u200b');
    expect(document.body.textContent).not.toContain('\u202e');
    // Non-blocking findings keep the explanatory sentence instead of a checkbox.
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument();
    expect(screen.getByText(/do not block the update/)).toBeInTheDocument();
  });
});
