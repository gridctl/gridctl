import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { SecretsPopover } from '../components/wizard/SecretsPopover';
import { useVaultStore } from '../stores/useVaultStore';

vi.mock('../lib/api', () => ({
  fetchVaultSecrets: vi.fn().mockResolvedValue([
    { key: 'API_TOKEN' },
    { key: 'DB_PASSWORD' },
    { key: 'REDIS_URL' },
  ]),
  createVaultSecret: vi.fn().mockResolvedValue(undefined),
}));

const selectNoop = (_reference: string) => {};

describe('SecretsPopover', () => {
  let onSelect: typeof selectNoop;

  beforeEach(() => {
    onSelect = vi.fn<typeof selectNoop>();
    useVaultStore.setState({ secrets: null });
  });

  it('renders the vault key icon button', () => {
    render(<SecretsPopover onSelect={onSelect} />);
    expect(screen.getByTitle('Insert vault secret')).toBeInTheDocument();
  });

  it('opens popover on click and loads secrets', async () => {
    render(<SecretsPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert vault secret'));

    await waitFor(() => {
      expect(screen.getByPlaceholderText('Filter secrets...')).toBeInTheDocument();
    });
  });

  it('filters secrets by search text', async () => {
    useVaultStore.setState({
      secrets: [{ key: 'API_TOKEN' }, { key: 'DB_PASSWORD' }, { key: 'REDIS_URL' }],
    });
    render(<SecretsPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert vault secret'));

    const search = screen.getByPlaceholderText('Filter secrets...');
    fireEvent.change(search, { target: { value: 'DB' } });

    expect(screen.getByText('DB_PASSWORD')).toBeInTheDocument();
    expect(screen.queryByText('REDIS_URL')).not.toBeInTheDocument();
  });

  it('calls onSelect with vault reference when secret is selected', async () => {
    useVaultStore.setState({
      secrets: [{ key: 'API_TOKEN' }],
    });
    render(<SecretsPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert vault secret'));
    fireEvent.click(screen.getByText('API_TOKEN'));
    expect(onSelect).toHaveBeenCalledWith('${vault:API_TOKEN}');
  });

  it('shows create new secret form', async () => {
    render(<SecretsPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert vault secret'));

    await waitFor(() => {
      expect(screen.getByText('Create New Secret')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Create New Secret'));
    expect(screen.getByPlaceholderText('SECRET_KEY')).toBeInTheDocument();
    expect(screen.getByPlaceholderText('Secret value')).toBeInTheDocument();
  });

  it('converts new key input to uppercase', async () => {
    render(<SecretsPopover onSelect={onSelect} />);
    fireEvent.click(screen.getByTitle('Insert vault secret'));

    await waitFor(() => {
      fireEvent.click(screen.getByText('Create New Secret'));
    });

    const keyInput = screen.getByPlaceholderText('SECRET_KEY');
    fireEvent.change(keyInput, { target: { value: 'my_api_key' } });
    expect(keyInput).toHaveValue('MY_API_KEY');
  });
});
