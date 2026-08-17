import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import { AuthCard } from '../components/wizard/AuthCard';
import { useAuthCard, type AuthCardController } from '../components/wizard/useAuthCard';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchVariables: vi.fn().mockResolvedValue([]),
    createVariable: vi.fn(),
  };
});

/**
 * Renders the card with a real controller and exposes it, so a test can assert
 * on buildAuth output rather than reaching through a consumer wizard.
 */
function Harness({
  ssh = false,
  onController,
}: {
  ssh?: boolean;
  onController?: (c: AuthCardController) => void;
}) {
  const controller = useAuthCard();
  onController?.(controller);
  return <AuthCard controller={controller} ssh={ssh} />;
}

describe('AuthCard', () => {
  it('is collapsed and labelled optional by default', () => {
    render(<Harness />);
    const toggle = screen.getByRole('button', { name: /authentication/i });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    expect(toggle.textContent).toMatch(/optional/i);
  });

  it('expands to both modes with vault preselected', () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    expect(screen.getByText(/vault secret/i)).toBeInTheDocument();
    expect(screen.getByText(/paste token/i)).toBeInTheDocument();
    // Vault is the safer option and leads.
    expect(screen.getByLabelText(/vault secret/i)).toBeChecked();
  });

  it('points aria-controls at the panel it actually renders', () => {
    render(<Harness />);
    const toggle = screen.getByRole('button', { name: /authentication/i });
    fireEvent.click(toggle);
    const panelId = toggle.getAttribute('aria-controls');
    expect(panelId).toBeTruthy();
    expect(document.getElementById(panelId as string)).toBeInTheDocument();
  });

  it('gives two instances distinct panel ids and radio groups', () => {
    render(
      <>
        <Harness />
        <Harness />
      </>,
    );
    const [first, second] = screen.getAllByRole('button', { name: /authentication/i });
    expect(first.getAttribute('aria-controls')).not.toBe(second.getAttribute('aria-controls'));

    // Switching one card's mode must not move the other's radio.
    fireEvent.click(first);
    fireEvent.click(second);
    const pasteRadios = screen.getAllByLabelText(/paste token/i);
    fireEvent.click(pasteRadios[0]);
    expect(pasteRadios[0]).toBeChecked();
    expect(pasteRadios[1]).not.toBeChecked();
  });

  it('shows the ssh notice and collects nothing for an SSH URL', () => {
    render(<Harness ssh />);
    const toggle = screen.getByRole('button', { name: /authentication/i });
    expect(toggle.textContent).toMatch(/using ssh-agent/i);
    fireEvent.click(toggle);
    expect(screen.queryByPlaceholderText(/personal access token/i)).not.toBeInTheDocument();
    expect(screen.queryByLabelText(/vault secret/i)).not.toBeInTheDocument();
    expect(screen.getAllByText(/using ssh-agent/i).length).toBeGreaterThanOrEqual(1);
  });

  it('announces the banner and moves focus to it, not into a field', () => {
    let controller: AuthCardController | undefined;
    render(<Harness onController={(c) => (controller = c)} />);

    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    act(() => controller?.openWithBanner('This repository requires authentication.'));

    const banner = screen.getByRole('alert');
    expect(banner).toHaveTextContent(/requires authentication/i);
    // GOV.UK error-summary pattern: focus the explanation, not an input the
    // user never asked for.
    expect(banner).toHaveFocus();
    expect(banner).toHaveAttribute('tabindex', '-1');
  });

  it('labels the token input and hides it behind a reveal toggle', () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    fireEvent.click(screen.getByLabelText(/paste token/i));

    const input = screen.getByLabelText(/personal access token/i);
    expect(input).toHaveAttribute('type', 'password');
    expect(input).toHaveAttribute('autocomplete', 'off');
    expect(input).toHaveAttribute('spellcheck', 'false');
    // Password managers must not capture a git token.
    expect(input).toHaveAttribute('data-lpignore', 'true');

    const reveal = screen.getByRole('button', { name: /show token/i });
    expect(reveal).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(reveal);
    expect(screen.getByLabelText(/personal access token/i)).toHaveAttribute('type', 'text');
    expect(screen.getByRole('button', { name: /hide token/i })).toHaveAttribute(
      'aria-pressed',
      'true',
    );
  });

  it('keeps the mode selector as real radios in a labelled fieldset', () => {
    render(<Harness />);
    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    const radios = screen.getAllByRole('radio');
    expect(radios).toHaveLength(2);
    expect(screen.getByRole('group', { name: /authentication method/i })).toBeInTheDocument();
  });
});

describe('useAuthCard buildAuth', () => {
  it('returns undefined for an untouched card, which omits the field entirely', () => {
    let controller: AuthCardController | undefined;
    render(<Harness onController={(c) => (controller = c)} />);
    expect(controller?.buildAuth(false)).toBeUndefined();
  });

  it('returns undefined for SSH regardless of what was typed', () => {
    let controller: AuthCardController | undefined;
    render(<Harness onController={(c) => (controller = c)} />);
    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    fireEvent.click(screen.getByLabelText(/paste token/i));
    fireEvent.change(screen.getByPlaceholderText(/personal access token/i), {
      target: { value: 'ghp_x' },
    });
    expect(controller?.buildAuth(true)).toBeUndefined();
  });

  it('builds a credentialRef in vault mode', () => {
    let controller: AuthCardController | undefined;
    render(<Harness onController={(c) => (controller = c)} />);
    act(() => controller?.setVaultRef('${vault:GIT_TOKEN}'));
    // Re-read after the re-render: buildAuth closes over the state it was
    // created with.
    expect(controller?.buildAuth(false)).toEqual({
      method: 'token',
      credentialRef: '${vault:GIT_TOKEN}',
    });
  });

  it('trims a pasted token, since provider UIs copy a trailing newline', () => {
    let controller: AuthCardController | undefined;
    render(<Harness onController={(c) => (controller = c)} />);
    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    fireEvent.click(screen.getByLabelText(/paste token/i));
    fireEvent.change(screen.getByPlaceholderText(/personal access token/i), {
      target: { value: '  ghp_spaced\n' },
    });
    expect(controller?.buildAuth(false)).toEqual({ method: 'token', token: 'ghp_spaced' });
  });

  it('treats a whitespace-only token as nothing supplied', () => {
    let controller: AuthCardController | undefined;
    render(<Harness onController={(c) => (controller = c)} />);
    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    fireEvent.click(screen.getByLabelText(/paste token/i));
    fireEvent.change(screen.getByPlaceholderText(/personal access token/i), {
      target: { value: '   ' },
    });
    expect(controller?.buildAuth(false)).toBeUndefined();
  });

  it('openInVaultMode switches mode and opens with a banner', () => {
    let controller: AuthCardController | undefined;
    render(<Harness onController={(c) => (controller = c)} />);
    fireEvent.click(screen.getByRole('button', { name: /authentication/i }));
    fireEvent.click(screen.getByLabelText(/paste token/i));
    act(() => controller?.openInVaultMode('Now using HTTPS.'));
    expect(screen.getByLabelText(/vault secret/i)).toBeChecked();
    expect(screen.getByRole('alert')).toHaveTextContent(/now using https/i);
  });
});
