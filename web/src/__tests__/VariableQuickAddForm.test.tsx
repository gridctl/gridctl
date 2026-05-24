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

  it('hides the generator for non-string types', async () => {
    renderForm();
    fireEvent.click(screen.getByRole('button', { name: 'json' }));
    await screen.findByLabelText('JSON value');
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

describe('VariableQuickAddForm — type switching', () => {
  it('does not carry the bool default value into another type', async () => {
    renderForm();
    fireEvent.click(screen.getByRole('button', { name: 'bool' }));
    // bool seeds a concrete toggle (shows "false").
    expect(screen.getByRole('switch')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'list' }));
    // The list tag-input should be empty — no stray "false" chip.
    expect(screen.getByRole('textbox', { name: 'Add list item' })).toBeInTheDocument();
    expect(screen.queryByText('false')).toBeNull();
  });
});

describe('VariableQuickAddForm — cancel', () => {
  it('shows Cancel only when onCancel is provided and invokes it', () => {
    const onCancel = vi.fn();
    const { rerender } = render(
      <VariableQuickAddForm setNames={[]} onSubmit={vi.fn()} />,
    );
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull();

    rerender(
      <VariableQuickAddForm
        setNames={[]}
        onSubmit={vi.fn()}
        onCancel={onCancel}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});
