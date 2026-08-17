import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { MemoryRouter } from 'react-router';
import { PackImportWizard } from '../components/wizard/steps/PackImportWizard';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    previewPack: vi.fn(),
    addPack: vi.fn(),
    applyPack: vi.fn(),
    fetchVariables: vi.fn().mockResolvedValue([]),
    createVariable: vi.fn(),
  };
});

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

import { previewPack, addPack, AuthError, HTTPError } from '../lib/api';
import { showToast } from '../components/ui/Toast';

const mockPreview = vi.mocked(previewPack);
const mockAdd = vi.mocked(addPack);
const mockToast = vi.mocked(showToast);

const emptyPreview = {
  pack: 'team-pack',
  version: '1.0.0',
  wiring: false,
  skills: [],
  agents: [],
  rules: [],
};

function renderWizard() {
  return render(
    <MemoryRouter>
      <PackImportWizard />
    </MemoryRouter>,
  );
}

function enterRepo(url: string) {
  fireEvent.change(screen.getByLabelText(/pack repository url/i), { target: { value: url } });
}

const clickPreview = () =>
  fireEvent.click(screen.getByRole('button', { name: /preview pack/i }));

describe('PackImportWizard — credentials', () => {
  beforeEach(() => {
    mockPreview.mockReset();
    mockAdd.mockReset();
    mockToast.mockReset();
  });

  it('offers credentials collapsed, so a public pack costs no extra step', () => {
    renderWizard();
    const toggle = screen.getByRole('button', { name: /authentication/i });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
  });

  it('omits auth entirely for a public pack, which also lets the server use a stored ref', async () => {
    mockPreview.mockResolvedValueOnce(emptyPreview);
    renderWizard();
    enterRepo('https://github.com/acme/team-pack');
    clickPreview();

    await waitFor(() => expect(mockPreview).toHaveBeenCalledTimes(1));
    expect(mockPreview.mock.calls[0][0].auth).toBeUndefined();
  });

  /**
   * The fetch layer turns every 401 into an AuthError, which carries no status.
   * That is the real shape a private pack produces, and asserting it here is
   * what keeps the discovery path from silently dying: a status-only check
   * would leave the card shut on the one case it exists for.
   */
  it('auto-opens the card on a 401, which arrives as an AuthError with no status', async () => {
    mockPreview.mockRejectedValueOnce(new AuthError('Authentication required'));
    renderWizard();
    enterRepo('https://github.com/acme/private-pack');
    clickPreview();

    await waitFor(() => {
      expect(screen.getByRole('button', { name: /authentication/i })).toHaveAttribute(
        'aria-expanded',
        'true',
      );
    });
    expect(screen.getByText(/requires authentication/i)).toBeInTheDocument();
  });

  it('auto-opens with the not-found wording on a 404', async () => {
    mockPreview.mockRejectedValueOnce(
      new HTTPError(404, 'Pack preview failed: repository not found'),
    );
    renderWizard();
    enterRepo('https://github.com/acme/private-pack');
    clickPreview();

    await waitFor(() => {
      expect(screen.getByText(/if this is a private repository/i)).toBeInTheDocument();
    });
  });

  it('sends the credential on retry through the same submit button', async () => {
    mockPreview
      .mockRejectedValueOnce(new AuthError('Authentication required'))
      .mockResolvedValueOnce(emptyPreview);

    renderWizard();
    enterRepo('https://github.com/acme/private-pack');
    clickPreview();

    await waitFor(() =>
      expect(screen.getByRole('button', { name: /authentication/i })).toHaveAttribute(
        'aria-expanded',
        'true',
      ),
    );

    // Switch to a pasted token and retry with the same submit button.
    fireEvent.click(screen.getByLabelText(/paste token/i));
    fireEvent.change(screen.getByPlaceholderText(/personal access token/i), {
      target: { value: 'ghp_secret' },
    });
    clickPreview();

    await waitFor(() => expect(mockPreview).toHaveBeenCalledTimes(2));
    expect(mockPreview.mock.calls[1][0]).toMatchObject({
      repo: 'https://github.com/acme/private-pack',
      auth: { method: 'token', token: 'ghp_secret' },
    });
    // The retry succeeded, so the wizard advances rather than re-asking.
    await waitFor(() => expect(screen.getByText(/team-pack/)).toBeInTheDocument());
  });

  it('announces a source failure once, through the alert region and not a toast', async () => {
    mockPreview.mockRejectedValueOnce(new HTTPError(500, 'Pack preview failed: boom'));
    renderWizard();
    enterRepo('https://github.com/acme/team-pack');
    clickPreview();

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/boom/i));
    expect(mockToast).not.toHaveBeenCalled();
  });
});

describe('PackImportWizard — unreachable ssh-agent', () => {
  beforeEach(() => {
    mockPreview.mockReset();
    mockAdd.mockReset();
    mockToast.mockReset();
  });

  const agentError = () =>
    new HTTPError(422, 'Pack preview failed: ssh agent not available: SSH_AUTH_SOCK is unset', {
      code: 'ssh_agent_unavailable',
      httpsEquivalent: 'https://github.com/acme/private-pack',
    });

  it('shows its own banner and does NOT open the credentials card', async () => {
    mockPreview.mockRejectedValueOnce(agentError());
    renderWizard();
    enterRepo('git@github.com:acme/private-pack.git');
    clickPreview();

    await waitFor(() => {
      expect(screen.getByText(/no ssh-agent reachable/i)).toBeInTheDocument();
    });
    // A token cannot authenticate an SSH URL, so opening the card would imply
    // a remedy that does not exist.
    expect(screen.getByRole('button', { name: /authentication/i })).toHaveAttribute(
      'aria-expanded',
      'false',
    );
    expect(screen.getByText(/restart the daemon/i)).toBeInTheDocument();
  });

  it('"Try HTTPS instead" rewrites the URL and opens the card in vault mode', async () => {
    mockPreview.mockRejectedValueOnce(agentError());
    renderWizard();
    enterRepo('git@github.com:acme/private-pack.git');
    clickPreview();

    await waitFor(() => screen.getByRole('button', { name: /try https instead/i }));
    fireEvent.click(screen.getByRole('button', { name: /try https instead/i }));

    expect(screen.getByLabelText(/pack repository url/i)).toHaveValue(
      'https://github.com/acme/private-pack',
    );
    expect(screen.getByRole('button', { name: /authentication/i })).toHaveAttribute(
      'aria-expanded',
      'true',
    );
    expect(screen.getByLabelText(/vault secret/i)).toBeChecked();
    // The banner is gone once the user has acted on it.
    expect(screen.queryByText(/no ssh-agent reachable/i)).not.toBeInTheDocument();
  });

  it('offers no HTTPS switch when the server could not derive one', async () => {
    mockPreview.mockRejectedValueOnce(
      new HTTPError(422, 'Pack preview failed: ssh agent not available', {
        code: 'ssh_agent_unavailable',
      }),
    );
    renderWizard();
    enterRepo('ssh://git@internal/acme/pack.git');
    clickPreview();

    await waitFor(() => expect(screen.getByText(/no ssh-agent reachable/i)).toBeInTheDocument());
    expect(screen.queryByRole('button', { name: /try https instead/i })).not.toBeInTheDocument();
  });

  it('returns to the source step when the install itself hits an auth failure', async () => {
    mockPreview.mockResolvedValueOnce(emptyPreview);
    mockAdd.mockRejectedValueOnce(new AuthError('Authentication required'));

    renderWizard();
    enterRepo('https://github.com/acme/private-pack');
    clickPreview();

    await waitFor(() => screen.getByRole('button', { name: /import pack/i }));
    fireEvent.click(screen.getByRole('button', { name: /import pack/i }));

    // Back on the source step, with the URL intact and the card open: the fix
    // lives here, and this is not a wizard restart.
    await waitFor(() => {
      expect(screen.getByLabelText(/pack repository url/i)).toHaveValue(
        'https://github.com/acme/private-pack',
      );
    });
    expect(screen.getByRole('button', { name: /authentication/i })).toHaveAttribute(
      'aria-expanded',
      'true',
    );
  });
});
