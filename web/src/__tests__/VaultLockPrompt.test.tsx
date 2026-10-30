import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { VaultLockPrompt } from '../components/vault/VaultLockPrompt';

describe('VaultLockPrompt', () => {
  it('names the passphrase field and toggles its visibility', async () => {
    const onUnlock = vi.fn().mockResolvedValue({ ok: false, error: 'Wrong passphrase' });
    render(<VaultLockPrompt onUnlock={onUnlock} />);

    const input = screen.getByLabelText('Passphrase');
    expect(input).toHaveAttribute('type', 'password');

    fireEvent.click(screen.getByRole('button', { name: 'Show passphrase' }));
    expect(input).toHaveAttribute('type', 'text');
    expect(screen.getByRole('button', { name: 'Hide passphrase' })).toBeInTheDocument();

    fireEvent.change(input, { target: { value: 'secret' } });
    fireEvent.submit(input.closest('form')!);

    expect(await screen.findByText('Wrong passphrase')).toBeInTheDocument();
    expect(input).toHaveAttribute('aria-invalid', 'true');
    const described = document.getElementById(input.getAttribute('aria-describedby') ?? '');
    expect(described).toHaveTextContent('Wrong passphrase');
  });
});
