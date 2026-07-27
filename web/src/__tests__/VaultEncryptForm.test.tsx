import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import '@testing-library/jest-dom';
import { VaultEncryptForm } from '../components/vault/VaultEncryptForm';
import { VaultLockPrompt } from '../components/vault/VaultLockPrompt';

function fillPassphrases(
  passphrase: string,
  confirm = passphrase,
  placeholder = 'New passphrase',
) {
  fireEvent.change(screen.getByPlaceholderText(placeholder), {
    target: { value: passphrase },
  });
  fireEvent.change(screen.getByPlaceholderText('Confirm passphrase'), {
    target: { value: confirm },
  });
}

describe('VaultEncryptForm — encrypt mode', () => {
  it('shows first-encrypt copy by default', () => {
    render(<VaultEncryptForm onLock={vi.fn()} onCancel={vi.fn()} />);
    expect(
      screen.getByText(/encrypt vault with a passphrase/i),
    ).toBeInTheDocument();
    expect(screen.getByPlaceholderText('New passphrase')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^encrypt$/i })).toBeInTheDocument();
  });

  it('blocks passphrases under 8 characters', () => {
    const onLock = vi.fn();
    render(<VaultEncryptForm onLock={onLock} onCancel={vi.fn()} />);
    fillPassphrases('short');
    expect(
      screen.getByText(/must be at least 8 characters/i),
    ).toBeInTheDocument();
    const submit = screen.getByRole('button', { name: /^encrypt$/i });
    expect(submit).toBeDisabled();
    fireEvent.click(submit);
    expect(onLock).not.toHaveBeenCalled();
  });

  it('nudges toward 12+ characters without blocking', async () => {
    const onLock = vi.fn().mockResolvedValue(undefined);
    render(<VaultEncryptForm onLock={onLock} onCancel={vi.fn()} />);
    fillPassphrases('ninechars');
    expect(screen.getByText(/12\+ characters recommended/i)).toBeInTheDocument();
    const submit = screen.getByRole('button', { name: /^encrypt$/i });
    expect(submit).toBeEnabled();
    await act(async () => {
      fireEvent.click(submit);
    });
    expect(onLock).toHaveBeenCalledWith('ninechars');
  });

  it('trims the passphrase before locking, matching what unlock submits', async () => {
    const onLock = vi.fn().mockResolvedValue(undefined);
    render(<VaultEncryptForm onLock={onLock} onCancel={vi.fn()} />);
    fillPassphrases('padded-passphrase \n');
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /^encrypt$/i }));
    });
    expect(onLock).toHaveBeenCalledWith('padded-passphrase');
  });

  it('rejects mismatched passphrases', () => {
    const onLock = vi.fn();
    render(<VaultEncryptForm onLock={onLock} onCancel={vi.fn()} />);
    fillPassphrases('long-enough-pass', 'different-pass');
    fireEvent.click(screen.getByRole('button', { name: /^encrypt$/i }));
    expect(screen.getByText(/do not match/i)).toBeInTheDocument();
    expect(onLock).not.toHaveBeenCalled();
  });
});

describe('VaultEncryptForm — lock mode', () => {
  it('shows re-lock copy and accepts a short existing passphrase', async () => {
    const onLock = vi.fn().mockResolvedValue(undefined);
    render(
      <VaultEncryptForm mode="lock" onLock={onLock} onCancel={vi.fn()} />,
    );
    expect(
      screen.getByText(/lock vault with your passphrase/i),
    ).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Passphrase')).toBeInTheDocument();
    expect(
      screen.queryByPlaceholderText('New passphrase'),
    ).not.toBeInTheDocument();

    // The existing passphrase may predate the length floor — lock mode must
    // not reject it.
    fillPassphrases('old', 'old', 'Passphrase');
    const submit = screen.getByRole('button', { name: /lock vault/i });
    expect(submit).toBeEnabled();
    await act(async () => {
      fireEvent.click(submit);
    });
    expect(onLock).toHaveBeenCalledWith('old');
  });
});

describe('VaultLockPrompt — unlock errors', () => {
  it('shows the specific error returned by the unlock action', async () => {
    const onUnlock = vi
      .fn()
      .mockResolvedValue({ ok: false, error: 'Unlock failed: gateway is down' });
    render(<VaultLockPrompt onUnlock={onUnlock} />);
    fireEvent.change(screen.getByPlaceholderText('Enter passphrase'), {
      target: { value: 'anything' },
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /unlock vault/i }));
    });
    expect(
      screen.getByText('Unlock failed: gateway is down'),
    ).toBeInTheDocument();
  });

  it('clears the field on success', async () => {
    const onUnlock = vi.fn().mockResolvedValue({ ok: true });
    render(<VaultLockPrompt onUnlock={onUnlock} />);
    const input = screen.getByPlaceholderText<HTMLInputElement>(
      'Enter passphrase',
    );
    fireEvent.change(input, { target: { value: 'correct-pass' } });
    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /unlock vault/i }));
    });
    expect(onUnlock).toHaveBeenCalledWith('correct-pass');
    expect(input.value).toBe('');
  });
});
