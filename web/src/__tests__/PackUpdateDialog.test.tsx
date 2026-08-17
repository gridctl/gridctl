import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
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

import { previewPack, addPack, AuthError, HTTPError } from '../lib/api';

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

function renderDialog(over: Partial<typeof origin> = {}) {
  return render(
    <PackUpdateDialog
      packName="team-pack"
      origin={{ ...origin, ...over }}
      onClose={vi.fn()}
      onUpdated={vi.fn()}
    />,
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
    mockPreview.mockRejectedValueOnce(new AuthError('Authentication required'));
    renderDialog();

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/authentication/i));
    // The dead end is gone: there is now something to do about it.
    expect(screen.getByRole('button', { name: /authentication/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /retry with credentials/i })).toBeInTheDocument();
  });

  it('retries the preview with the credential the user supplied', async () => {
    mockPreview
      .mockRejectedValueOnce(new AuthError('Authentication required'))
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
      .mockRejectedValueOnce(new AuthError('Authentication required'))
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
  it('does not offer credentials for an unreachable ssh-agent', async () => {
    mockPreview.mockRejectedValueOnce(
      new HTTPError(422, 'Pack preview failed: ssh agent not available', {
        code: 'ssh_agent_unavailable',
        httpsEquivalent: 'https://github.com/acme/team-pack',
      }),
    );
    renderDialog({ repo: 'git@github.com:acme/team-pack.git' });

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/ssh agent/i));
    expect(screen.queryByRole('button', { name: /retry with credentials/i })).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^authentication/i })).not.toBeInTheDocument();
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
    mockPreview.mockRejectedValueOnce(new AuthError('Authentication required'));
    render(
      <PackUpdateDialog
        packName="team-pack"
        origin={origin}
        onClose={onClose}
        onUpdated={vi.fn()}
      />,
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
