import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { VariableQuickAddForm } from '../components/vault/VariableQuickAddForm';
import { HTTPError, type Variable } from '../lib/api';

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

function renderForm() {
  return render(
    <VariableQuickAddForm setNames={[]} existingVariables={[]} onSubmit={vi.fn()} />,
  );
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
      <VariableQuickAddForm setNames={[]} existingVariables={[]} onSubmit={vi.fn()} />,
    );
    expect(screen.queryByRole('button', { name: 'Cancel' })).toBeNull();

    rerender(
      <VariableQuickAddForm
        setNames={[]}
        existingVariables={[]}
        onSubmit={vi.fn()}
        onCancel={onCancel}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});

describe('VariableQuickAddForm — existing key', () => {
  const existing: Variable[] = [
    { key: 'API_KEY', type: 'json', is_secret: true, set: 'prod' },
  ];

  function renderWithExisting(
    onSubmit = vi.fn().mockResolvedValue(undefined),
    existingVariables: Variable[] = existing,
  ) {
    const utils = render(
      <VariableQuickAddForm
        setNames={['prod', 'staging']}
        existingVariables={existingVariables}
        onSubmit={onSubmit}
      />,
    );
    return { onSubmit, ...utils };
  }

  function type(key: string, value: string) {
    fireEvent.change(screen.getByLabelText('Variable key'), { target: { value: key } });
    fireEvent.change(screen.getByPlaceholderText('secret value'), { target: { value } });
  }

  it('shows the conflict before submit and disables Add', () => {
    renderWithExisting();
    type('API_KEY', 'new-value');
    expect(screen.getByRole('status')).toHaveTextContent('API_KEY already exists.');
    expect(screen.getByLabelText('Variable key')).toHaveAttribute(
      'aria-describedby',
      screen.getByRole('status').id,
    );
    expect(screen.getByRole('button', { name: 'Add' })).toBeDisabled();
  });

  it('does not submit on Enter in the value input', () => {
    const { onSubmit } = renderWithExisting();
    type('API_KEY', 'new-value');
    fireEvent.keyDown(screen.getByPlaceholderText('secret value'), { key: 'Enter' });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('arms a confirmation on Replace… and submits replace only on confirm', async () => {
    const { onSubmit } = renderWithExisting();
    type('API_KEY', 'new-value');

    fireEvent.click(screen.getByRole('button', { name: 'Replace…' }));
    expect(onSubmit).not.toHaveBeenCalled();

    const confirm = screen.getByRole('group', { name: 'Confirm replacing API_KEY' });
    expect(confirm).toHaveTextContent('The current value is replaced and cannot be recovered.');
    expect(confirm).toHaveTextContent('Type: json → string');
    expect(confirm).toHaveTextContent('The description and other metadata are kept.');
    // Visibility is unchanged and no set was chosen, so neither is listed.
    expect(confirm).not.toHaveTextContent('Visibility');
    expect(confirm).not.toHaveTextContent('Set:');

    fireEvent.click(within(confirm).getByRole('button', { name: 'Replace API_KEY' }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit).toHaveBeenCalledWith({
      key: 'API_KEY',
      value: 'new-value',
      type: 'string',
      isSecret: true,
      set: undefined,
      replace: true,
    });
  });

  it('lists a set change only when a different set is chosen', () => {
    renderWithExisting();
    type('API_KEY', 'new-value');
    fireEvent.change(screen.getByLabelText('Variable set'), { target: { value: 'staging' } });
    fireEvent.click(screen.getByRole('button', { name: 'Replace…' }));
    expect(screen.getByRole('group', { name: 'Confirm replacing API_KEY' })).toHaveTextContent(
      'Set: prod → staging',
    );
  });

  it('disarms the confirmation when the key is edited', () => {
    renderWithExisting();
    type('API_KEY', 'new-value');
    fireEvent.click(screen.getByRole('button', { name: 'Replace…' }));
    expect(screen.getByRole('group', { name: 'Confirm replacing API_KEY' })).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText('Variable key'), { target: { value: 'API_KE' } });
    expect(screen.queryByRole('group', { name: /Confirm replacing/ })).toBeNull();
  });

  it('keeps the form contents when the confirmation is cancelled', () => {
    const { onSubmit } = renderWithExisting();
    type('API_KEY', 'new-value');
    fireEvent.click(screen.getByRole('button', { name: 'Replace…' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel replace' }));
    expect(screen.queryByRole('group', { name: /Confirm replacing/ })).toBeNull();
    expect(screen.getByLabelText('Variable key')).toHaveValue('API_KEY');
    expect(screen.getByPlaceholderText('secret value')).toHaveValue('new-value');
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('submits a new key without replace', async () => {
    const { onSubmit } = renderWithExisting();
    type('OTHER_KEY', 'v');
    expect(screen.queryByRole('status')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Replace…' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit.mock.calls[0][0]).not.toHaveProperty('replace');
    expect(onSubmit.mock.calls[0][0]).toMatchObject({ key: 'OTHER_KEY', value: 'v' });
  });

  it('treats a 409 from a stale list as a conflict and offers Replace… once the list has the key', async () => {
    const onSubmit = vi
      .fn()
      .mockRejectedValue(new HTTPError(409, 'variable "API_KEY" already exists'));
    const { rerender } = renderWithExisting(onSubmit, []);
    type('API_KEY', 'new-value');
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));

    await waitFor(() =>
      expect(screen.getByRole('status')).toHaveTextContent(
        'API_KEY already exists. Refresh the list to replace it.',
      ),
    );
    expect(screen.getByRole('button', { name: 'Add' })).toBeDisabled();
    expect(screen.queryByRole('button', { name: 'Replace…' })).toBeNull();
    // The raw server error line is replaced by the notice.
    expect(screen.queryByText('variable "API_KEY" already exists')).toBeNull();

    rerender(
      <VariableQuickAddForm
        setNames={['prod', 'staging']}
        existingVariables={existing}
        onSubmit={onSubmit}
      />,
    );
    expect(screen.getByRole('status')).toHaveTextContent('API_KEY already exists.');
    expect(screen.getByRole('button', { name: 'Replace…' })).toBeEnabled();
  });
});
