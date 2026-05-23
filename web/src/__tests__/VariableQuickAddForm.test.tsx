import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { VariableQuickAddForm } from '../components/vault/VariableQuickAddForm';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

function renderForm() {
  return render(<VariableQuickAddForm setNames={[]} onSubmit={vi.fn()} />);
}

describe('VariableQuickAddForm — secret generator', () => {
  it('offers the generator for string variables (the default)', () => {
    renderForm();
    expect(
      screen.getByRole('button', { name: 'Generate value' }),
    ).toBeInTheDocument();
  });

  it('hides the generator for non-string types', () => {
    renderForm();
    fireEvent.click(screen.getByRole('button', { name: 'json' }));
    expect(screen.queryByRole('button', { name: 'Generate value' })).toBeNull();
  });

  it('fills and reveals the value input when Generate is clicked', () => {
    renderForm();
    const valueInput = screen.getByPlaceholderText('secret value') as HTMLInputElement;
    // Secret value starts masked.
    expect(valueInput).toHaveAttribute('type', 'password');

    fireEvent.click(screen.getByRole('button', { name: 'Generate value' }));
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }));

    expect(valueInput.value).toHaveLength(24);
    // Auto-revealed after generation.
    expect(valueInput).toHaveAttribute('type', 'text');
  });
});
