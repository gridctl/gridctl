import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import '@testing-library/jest-dom';
import { VariablesPopover } from '../components/wizard/VariablesPopover';
import { useVaultStore } from '../stores/useVaultStore';
import * as api from '../lib/api';

const seeded = [
  { key: 'API_TOKEN', type: 'string' as const, is_secret: true },
  { key: 'DB_PASSWORD', type: 'string' as const, is_secret: true },
  { key: 'REDIS_URL', type: 'string' as const, is_secret: true },
];

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchVariables: vi.fn(),
    createVariable: vi.fn(),
    updateVariable: vi.fn(),
  };
});

type OnSelect = (reference: string) => void;

describe('VariablesPopover', () => {
  let onSelect: OnSelect;

  beforeEach(() => {
    onSelect = vi.fn<OnSelect>();
    useVaultStore.setState({ variables: null });
    vi.mocked(api.fetchVariables).mockReset();
    vi.mocked(api.fetchVariables).mockResolvedValue(seeded);
    vi.mocked(api.createVariable).mockReset();
    vi.mocked(api.createVariable).mockResolvedValue(undefined);
    vi.mocked(api.updateVariable).mockReset();
    vi.mocked(api.updateVariable).mockResolvedValue(undefined);
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

  describe('existing key in the create form', () => {
    const openCreateFormWithStore = async () => {
      useVaultStore.setState({ variables: seeded });
      render(<VariablesPopover onSelect={onSelect} />);
      fireEvent.click(screen.getByTitle('Insert variable'));
      await screen.findByText('API_TOKEN');
      fireEvent.click(screen.getByText('Create New Variable'));
    };

    const typeKeyAndValue = (key: string, value: string) => {
      fireEvent.change(screen.getByPlaceholderText('VARIABLE_KEY'), { target: { value: key } });
      fireEvent.change(screen.getByPlaceholderText('secret value'), { target: { value } });
    };

    it('shows the conflict, disables Create & Insert, and ignores Enter', async () => {
      await openCreateFormWithStore();
      typeKeyAndValue('API_TOKEN', 'new-value');

      expect(screen.getByRole('status')).toHaveTextContent('API_TOKEN already exists.');
      expect(screen.getByRole('button', { name: /Create & Insert/ })).toBeDisabled();
      fireEvent.keyDown(screen.getByPlaceholderText('secret value'), { key: 'Enter' });
      expect(api.createVariable).not.toHaveBeenCalled();
      expect(onSelect).not.toHaveBeenCalled();
    });

    it('inserts the existing reference without writing', async () => {
      await openCreateFormWithStore();
      typeKeyAndValue('API_TOKEN', 'new-value');

      fireEvent.click(screen.getByRole('button', { name: 'Insert existing' }));
      expect(onSelect).toHaveBeenCalledWith('${var:API_TOKEN}');
      expect(api.createVariable).not.toHaveBeenCalled();
      expect(api.updateVariable).not.toHaveBeenCalled();
      expect(screen.queryByPlaceholderText('Filter variables...')).not.toBeInTheDocument();
    });

    it('replaces through a partial update with no set after confirming', async () => {
      await openCreateFormWithStore();
      typeKeyAndValue('API_TOKEN', 'new-value');

      fireEvent.click(screen.getByRole('button', { name: 'Replace value…' }));
      expect(api.updateVariable).not.toHaveBeenCalled();
      const confirm = screen.getByRole('group', { name: 'Confirm replacing API_TOKEN' });
      expect(confirm).toHaveTextContent('The current value is replaced and cannot be recovered.');
      expect(confirm).not.toHaveTextContent('Set:');

      fireEvent.click(within(confirm).getByRole('button', { name: 'Replace & Insert' }));
      await waitFor(() => expect(onSelect).toHaveBeenCalledWith('${var:API_TOKEN}'));
      expect(api.updateVariable).toHaveBeenCalledTimes(1);
      const [key, input] = vi.mocked(api.updateVariable).mock.calls[0];
      expect(key).toBe('API_TOKEN');
      expect(input).toEqual({ value: 'new-value', type: 'string', isSecret: true });
      expect(input).not.toHaveProperty('set');
      expect(api.createVariable).not.toHaveBeenCalled();
    });

    it('disarms the confirmation when the value changes', async () => {
      await openCreateFormWithStore();
      typeKeyAndValue('API_TOKEN', 'new-value');
      fireEvent.click(screen.getByRole('button', { name: 'Replace value…' }));
      fireEvent.change(screen.getByPlaceholderText('secret value'), { target: { value: 'other' } });
      expect(screen.queryByRole('group', { name: /Confirm replacing/ })).toBeNull();
    });

    it('creates a new key with createOnly', async () => {
      await openCreateFormWithStore();
      typeKeyAndValue('NEW_TOKEN', 'v');

      fireEvent.click(screen.getByRole('button', { name: /Create & Insert/ }));
      await waitFor(() => expect(onSelect).toHaveBeenCalledWith('${var:NEW_TOKEN}'));
      expect(api.createVariable).toHaveBeenCalledWith({
        key: 'NEW_TOKEN',
        value: 'v',
        type: 'string',
        isSecret: true,
        createOnly: true,
      });
    });

    it('does not insert when the server reports the key exists', async () => {
      // The list never loaded, so only the server knows the key exists.
      vi.mocked(api.fetchVariables).mockRejectedValueOnce(new Error('offline'));
      render(<VariablesPopover onSelect={onSelect} />);
      fireEvent.click(screen.getByTitle('Insert variable'));
      fireEvent.click(await screen.findByText('Create New Variable'));

      vi.mocked(api.createVariable).mockRejectedValueOnce(
        new api.HTTPError(409, 'variable "API_TOKEN" already exists'),
      );
      typeKeyAndValue('API_TOKEN', 'new-value');
      fireEvent.click(screen.getByRole('button', { name: /Create & Insert/ }));

      await waitFor(() =>
        expect(screen.getByRole('status')).toHaveTextContent('API_TOKEN already exists.'),
      );
      expect(onSelect).not.toHaveBeenCalled();
      expect(screen.getByRole('button', { name: 'Insert existing' })).toBeInTheDocument();
      expect(screen.getByPlaceholderText('VARIABLE_KEY')).toBeInTheDocument();
    });

    it('shows the server message when the 409 refetch fails', async () => {
      render(<VariablesPopover onSelect={onSelect} />);
      fireEvent.click(screen.getByTitle('Insert variable'));
      fireEvent.click(await screen.findByText('Create New Variable'));

      vi.mocked(api.createVariable).mockRejectedValueOnce(
        new api.HTTPError(409, 'variable "BRAND_NEW" already exists'),
      );
      vi.mocked(api.fetchVariables).mockRejectedValueOnce(new Error('offline'));
      typeKeyAndValue('BRAND_NEW', 'new-value');
      fireEvent.click(screen.getByRole('button', { name: /Create & Insert/ }));

      expect(await screen.findByText('variable "BRAND_NEW" already exists')).toBeInTheDocument();
      expect(screen.queryByText('new-value')).toBeNull();
      expect(onSelect).not.toHaveBeenCalled();
      expect(screen.getByPlaceholderText('VARIABLE_KEY')).toBeInTheDocument();
    });

    it('disarms the confirmation when the popover closes', async () => {
      await openCreateFormWithStore();
      typeKeyAndValue('API_TOKEN', 'new-value');
      fireEvent.click(screen.getByRole('button', { name: 'Replace value…' }));
      expect(screen.getByRole('group', { name: 'Confirm replacing API_TOKEN' })).toBeInTheDocument();

      fireEvent.keyDown(screen.getByPlaceholderText('secret value'), { key: 'Escape' });
      expect(screen.queryByPlaceholderText('VARIABLE_KEY')).toBeNull();

      fireEvent.click(screen.getByTitle('Insert variable'));
      expect(await screen.findByPlaceholderText('VARIABLE_KEY')).toBeInTheDocument();
      expect(screen.queryByRole('group', { name: /Confirm replacing/ })).toBeNull();
      expect(screen.getByRole('button', { name: 'Replace value…' })).toBeInTheDocument();
    });
  });

  describe('load failures', () => {
    it('shows locked guidance with Retry on a 423 and never the server text', async () => {
      vi.mocked(api.fetchVariables).mockRejectedValue(new api.HTTPError(423, 'vault is locked'));
      render(<VariablesPopover onSelect={onSelect} />);
      fireEvent.click(screen.getByTitle('Insert variable'));

      const alert = await screen.findByRole('alert');
      expect(alert).toHaveTextContent('Variables locked. Unlock them in the Variables workspace, then retry.');
      expect(alert).not.toHaveTextContent(/vault/i);
      expect(screen.queryByText('Loading...')).not.toBeInTheDocument();
      expect(within(alert).getByRole('button', { name: 'Retry' })).toBeInTheDocument();
    });

    it('shows the failure message with Retry on another error and recovers', async () => {
      vi.mocked(api.fetchVariables).mockRejectedValueOnce(new Error('offline'));
      render(<VariablesPopover onSelect={onSelect} />);
      fireEvent.click(screen.getByTitle('Insert variable'));

      const alert = await screen.findByRole('alert');
      expect(alert).toHaveTextContent('Load variables failed: offline.');
      expect(screen.queryByText('Loading...')).not.toBeInTheDocument();
      expect(screen.queryByText('No variables found')).not.toBeInTheDocument();

      fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
      expect(await screen.findByText('API_TOKEN')).toBeInTheDocument();
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });

    it('keeps cached variables listed under the notice', async () => {
      useVaultStore.setState({ variables: seeded });
      vi.mocked(api.fetchVariables).mockRejectedValue(new Error('offline'));
      render(<VariablesPopover onSelect={onSelect} />);
      fireEvent.click(screen.getByTitle('Insert variable'));

      expect(await screen.findByRole('alert')).toHaveTextContent('Load variables failed: offline.');
      expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument();
    });

    it('shows the empty copy only for a loaded empty list', async () => {
      vi.mocked(api.fetchVariables).mockResolvedValue([]);
      render(<VariablesPopover onSelect={onSelect} />);
      fireEvent.click(screen.getByTitle('Insert variable'));
      expect(await screen.findByText('No variables found')).toBeInTheDocument();
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    });

    it('shows Loading... while the request is pending with no error', () => {
      vi.mocked(api.fetchVariables).mockReturnValue(new Promise(() => {}));
      render(<VariablesPopover onSelect={onSelect} />);
      fireEvent.click(screen.getByTitle('Insert variable'));
      expect(screen.getByText('Loading...')).toBeInTheDocument();
    });
  });
});
