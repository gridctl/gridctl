import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { VariablesPopover } from '../components/wizard/VariablesPopover';
import { useVaultStore } from '../stores/useVaultStore';

vi.mock('../lib/api', () => ({
  fetchVariables: vi.fn().mockResolvedValue([
    { key: 'API_TOKEN', type: 'string', is_secret: true },
    { key: 'DB_PASSWORD', type: 'string', is_secret: true },
    { key: 'REDIS_URL', type: 'string', is_secret: true },
  ]),
  createVariable: vi.fn().mockResolvedValue(undefined),
}));

const selectNoop = (_reference: string) => {};

describe('VariablesPopover', () => {
  let onSelect: typeof selectNoop;

  beforeEach(() => {
    onSelect = vi.fn<typeof selectNoop>();
    useVaultStore.setState({ variables: null });
  });

  it('renders the insert-variable trigger button', () => {
    render(<VariablesPopover onSelect={onSelect} />);
    expect(screen.getByTitle('Insert variable')).toBeInTheDocument();
  });

  it('opens popover on click and loads variables', async () => {
    render(<VariablesPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert variable'));

    await waitFor(() => {
      expect(screen.getByPlaceholderText('Filter variables...')).toBeInTheDocument();
    });
  });

  it('filters variables by search text', async () => {
    useVaultStore.setState({
      variables: [
        { key: 'API_TOKEN', type: 'string', is_secret: true },
        { key: 'DB_PASSWORD', type: 'string', is_secret: true },
        { key: 'REDIS_URL', type: 'string', is_secret: true },
      ],
    });
    render(<VariablesPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert variable'));

    const search = screen.getByPlaceholderText('Filter variables...');
    fireEvent.change(search, { target: { value: 'DB' } });

    expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument();
    expect(screen.queryByText('REDIS_URL')).not.toBeInTheDocument();
  });

  it('emits ${var:KEY} canonical syntax on select', async () => {
    useVaultStore.setState({
      variables: [{ key: 'API_TOKEN', type: 'string', is_secret: true }],
    });
    render(<VariablesPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert variable'));
    fireEvent.click(screen.getByText('API_TOKEN'));
    expect(onSelect).toHaveBeenCalledWith('${var:API_TOKEN}');
  });

  it('surfaces both secret and plaintext variables with distinct visibility icons', async () => {
    useVaultStore.setState({
      variables: [
        { key: 'SECRET_TOKEN', type: 'string', is_secret: true },
        { key: 'REGION', type: 'string', is_secret: false },
      ],
    });
    render(<VariablesPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert variable'));

    // Both rows render in the picker.
    expect(screen.getByText('SECRET_TOKEN')).toBeInTheDocument();
    expect(screen.getByText('REGION')).toBeInTheDocument();

    // Visibility icons differ — one lock (secret), one eye (plaintext).
    const lockIcons = document.querySelectorAll('[aria-label="secret"]');
    const eyeIcons = document.querySelectorAll('[aria-label="plaintext"]');
    expect(lockIcons.length).toBeGreaterThanOrEqual(1);
    expect(eyeIcons.length).toBeGreaterThanOrEqual(1);
  });

  it('shows create new variable form', async () => {
    render(<VariablesPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert variable'));

    await waitFor(() => {
      expect(screen.getByText('Create New Variable')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Create New Variable'));
    expect(screen.getByPlaceholderText('VARIABLE_KEY')).toBeInTheDocument();
    // Default form state is string + Secret, so the value hint mirrors that.
    expect(screen.getByPlaceholderText('secret value')).toBeInTheDocument();
  });

  it('converts new key input to uppercase', async () => {
    render(<VariablesPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert variable'));

    await waitFor(() => {
      fireEvent.click(screen.getByText('Create New Variable'));
    });

    const keyInput = screen.getByPlaceholderText('VARIABLE_KEY');
    fireEvent.change(keyInput, { target: { value: 'my_api_key' } });
    expect(keyInput).toHaveValue('MY_API_KEY');
  });

  it('renders dropdown via portal at document.body', async () => {
    const { container } = render(<VariablesPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert variable'));

    await waitFor(() => {
      expect(screen.getByPlaceholderText('Filter variables...')).toBeInTheDocument();
    });

    // Dropdown is not inside the component container — it's portalled to body
    expect(container.querySelector('[placeholder="Filter variables..."]')).toBeNull();
    expect(within(document.body).getByPlaceholderText('Filter variables...')).toBeInTheDocument();
  });

  it('renders all variables in the list when many exist', async () => {
    const many = Array.from({ length: 20 }, (_, i) => ({
      key: `VAR_${i}`,
      type: 'string' as const,
      is_secret: true,
    }));
    useVaultStore.setState({ variables: many });

    render(<VariablesPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert variable'));

    for (let i = 0; i < 20; i++) {
      expect(screen.getByText(`VAR_${i}`)).toBeInTheDocument();
    }
  });

  describe('placeholder adaptation', () => {
    const openCreateForm = async () => {
      render(<VariablesPopover onSelect={onSelect} />);
      fireEvent.click(screen.getByTitle('Insert variable'));
      await waitFor(() => {
        fireEvent.click(screen.getByText('Create New Variable'));
      });
    };

    it('shows a secret-value hint by default (string + Secret)', async () => {
      await openCreateForm();
      expect(screen.getByPlaceholderText('secret value')).toBeInTheDocument();
    });

    it('drops the word "secret" when Plaintext is selected', async () => {
      await openCreateForm();
      fireEvent.click(screen.getByRole('button', { name: /plaintext/i }));
      expect(screen.queryByPlaceholderText('secret value')).not.toBeInTheDocument();
      expect(screen.getByPlaceholderText('plaintext value')).toBeInTheDocument();
    });

    it('hints comma-separated input when list type is selected', async () => {
      await openCreateForm();
      fireEvent.click(screen.getByRole('button', { name: 'list' }));
      expect(screen.getByPlaceholderText('item1, item2, item3')).toBeInTheDocument();
    });

    it('shows the JSON editor when json type is selected', async () => {
      await openCreateForm();
      fireEvent.click(screen.getByRole('button', { name: 'json' }));
      expect(await screen.findByLabelText('JSON value')).toBeInTheDocument();
    });

    it('hints a number example when number type is selected', async () => {
      await openCreateForm();
      fireEvent.click(screen.getByRole('button', { name: 'number' }));
      expect(screen.getByPlaceholderText('42')).toBeInTheDocument();
    });

    it('renders a toggle switch when bool type is selected', async () => {
      await openCreateForm();
      fireEvent.click(screen.getByRole('button', { name: 'bool' }));
      expect(screen.getByRole('switch')).toBeInTheDocument();
    });
  });

  it('closes popover on outside mousedown', async () => {
    render(<VariablesPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert variable'));

    await waitFor(() => {
      expect(screen.getByPlaceholderText('Filter variables...')).toBeInTheDocument();
    });

    fireEvent.mouseDown(document.body);

    await waitFor(() => {
      expect(screen.queryByPlaceholderText('Filter variables...')).not.toBeInTheDocument();
    });
  });

  describe('secret generator', () => {
    const openCreateForm = async () => {
      render(<VariablesPopover onSelect={onSelect} />);
      fireEvent.click(screen.getByTitle('Insert variable'));
      await waitFor(() => {
        fireEvent.click(screen.getByText('Create New Variable'));
      });
    };

    it('offers the generator in the create-new form for string variables', async () => {
      await openCreateForm();
      expect(
        screen.getByRole('button', { name: 'Generate value' }),
      ).toBeInTheDocument();
    });

    it('generates into the value input without closing the popover', async () => {
      await openCreateForm();
      const valueInput = screen.getByPlaceholderText('secret value') as HTMLInputElement;

      fireEvent.click(screen.getByRole('button', { name: 'Generate value' }));
      // Interacting inside the inline panel must not close the host popover.
      fireEvent.mouseDown(screen.getByRole('slider', { name: 'Length' }));
      fireEvent.click(screen.getByRole('button', { name: 'Generate' }));

      expect(valueInput.value).toHaveLength(24);
      // The create form (and popover) are still open.
      expect(screen.getByPlaceholderText('VARIABLE_KEY')).toBeInTheDocument();
    });
  });
});
