import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { SecretGenerator } from '../components/vault/SecretGenerator';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));
import { showToast } from '../components/ui/Toast';

function open() {
  fireEvent.click(screen.getByRole('button', { name: 'Generate value' }));
}

describe('SecretGenerator', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders a collapsed trigger by default', () => {
    render(<SecretGenerator onGenerate={vi.fn()} />);
    const trigger = screen.getByRole('button', { name: 'Generate value' });
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('button', { name: 'Generate' })).toBeNull();
  });

  it('expands the panel on trigger click', () => {
    render(<SecretGenerator onGenerate={vi.fn()} />);
    open();
    expect(
      screen.getByRole('button', { name: 'Generate value' }),
    ).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('slider', { name: 'Length' })).toBeInTheDocument();
    expect(screen.getByRole('group', { name: 'Character classes' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Generate' })).toBeInTheDocument();
  });

  it('updates the live entropy readout as classes change', () => {
    render(<SecretGenerator onGenerate={vi.fn()} />);
    open();
    // Default: length 24 over the full 74-char alphabet → ~149 bits.
    expect(screen.getByText('~149 bits')).toBeInTheDocument();
    // Drop symbols (12 chars) → 62-char alphabet → ~143 bits.
    fireEvent.click(screen.getByRole('button', { name: '!@#' }));
    expect(screen.getByText('~143 bits')).toBeInTheDocument();
  });

  it('updates entropy as the length slider moves', () => {
    render(<SecretGenerator onGenerate={vi.fn()} />);
    open();
    fireEvent.change(screen.getByRole('slider', { name: 'Length' }), {
      target: { value: '8' },
    });
    // 8 × log2(74) ≈ 50 bits.
    expect(screen.getByText('~50 bits')).toBeInTheDocument();
  });

  it('never lets the last character class be disabled', () => {
    render(<SecretGenerator onGenerate={vi.fn()} />);
    open();
    fireEvent.click(screen.getByRole('button', { name: 'A-Z' }));
    fireEvent.click(screen.getByRole('button', { name: 'a-z' }));
    fireEvent.click(screen.getByRole('button', { name: '0-9' }));
    // Only symbols remain — its chip is now locked.
    expect(screen.getByRole('button', { name: '!@#' })).toBeDisabled();
  });

  it('fills the value and reveals it on Generate', () => {
    const onGenerate = vi.fn();
    const onReveal = vi.fn();
    render(<SecretGenerator onGenerate={onGenerate} onReveal={onReveal} />);
    open();
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }));

    expect(onGenerate).toHaveBeenCalledTimes(1);
    expect(onGenerate.mock.calls[0][0]).toHaveLength(24);
    expect(onReveal).toHaveBeenCalledTimes(1);
    // Button relabels and a copy affordance appears.
    expect(screen.getByRole('button', { name: 'Regenerate' })).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /copy generated value/i }),
    ).toBeInTheDocument();
  });

  it('produces a new value on regenerate', () => {
    const onGenerate = vi.fn();
    render(<SecretGenerator onGenerate={onGenerate} />);
    open();
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }));
    fireEvent.click(screen.getByRole('button', { name: 'Regenerate' }));
    expect(onGenerate).toHaveBeenCalledTimes(2);
    expect(onGenerate.mock.calls[0][0]).not.toBe(onGenerate.mock.calls[1][0]);
  });

  it('copies the generated value to the clipboard without leaking it to the toast', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText } });
    const onGenerate = vi.fn();
    render(<SecretGenerator onGenerate={onGenerate} />);
    open();
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }));
    const value = onGenerate.mock.calls[0][0];

    fireEvent.click(screen.getByRole('button', { name: /copy generated value/i }));
    await Promise.resolve();

    expect(writeText).toHaveBeenCalledWith(value);
    expect(showToast).toHaveBeenCalledWith('success', 'Copied');
    // The toast message must never contain the secret characters.
    expect(vi.mocked(showToast).mock.calls[0][1]).not.toContain(value);
  });

  it('announces only metadata — never the secret characters', () => {
    const onGenerate = vi.fn();
    render(<SecretGenerator onGenerate={onGenerate} />);
    open();
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }));
    const value = onGenerate.mock.calls[0][0];

    const live = screen.getByText(/Generated a 24-character value/);
    expect(live).toHaveTextContent('~149 bits of entropy');
    expect(live).not.toHaveTextContent(value);
  });

  it('collapses on Escape', () => {
    render(<SecretGenerator onGenerate={vi.fn()} />);
    open();
    fireEvent.keyDown(screen.getByRole('slider', { name: 'Length' }), {
      key: 'Escape',
    });
    expect(
      screen.getByRole('button', { name: 'Generate value' }),
    ).toHaveAttribute('aria-expanded', 'false');
    expect(screen.queryByRole('slider', { name: 'Length' })).toBeNull();
  });
});
