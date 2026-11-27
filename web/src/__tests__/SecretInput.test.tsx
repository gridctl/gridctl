import { describe, it, expect, vi, beforeEach } from 'vitest';
import { useState } from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { SecretInput } from '../components/wizard/SecretInput';
import { createVariable, fetchVariables, HTTPError } from '../lib/api';
import { useVaultStore } from '../stores/useVaultStore';

vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  fetchVariables: vi.fn(),
  createVariable: vi.fn(),
}));

function Harness({ initial, onValue, suggestedKey }: { initial: string; onValue?: (v: string) => void; suggestedKey?: string }) {
  const [value, setValue] = useState(initial);
  return (
    <>
      <label htmlFor="tok">Token</label>
      <SecretInput
        id="tok"
        value={value}
        onChange={(next) => {
          setValue(next);
          onValue?.(next);
        }}
        placeholder="${var:MY_TOKEN}"
        suggestedKey={suggestedKey}
      />
    </>
  );
}

const WARNING = 'This literal will be written to the stack file in clear text.';

describe('SecretInput', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useVaultStore.setState({ variables: null });
  });

  it('masks a literal and shows the warning', () => {
    render(<Harness initial="sk-literal-123" />);
    const input = screen.getByLabelText('Token');
    expect(input).toHaveAttribute('type', 'password');
    expect(input).toHaveAttribute('autocomplete', 'off');
    expect(input).toHaveAttribute('data-lpignore', 'true');
    expect(screen.getByText(WARNING)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /store as variable/i })).toBeInTheDocument();
  });

  it('renders a reference as text with no warning or toggle', () => {
    render(<Harness initial="${var:MY_TOKEN}" />);
    const input = screen.getByLabelText('Token');
    expect(input).toHaveAttribute('type', 'text');
    expect(input).toHaveClass('text-tertiary');
    expect(screen.queryByText(WARNING)).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Show value' })).not.toBeInTheDocument();
  });

  it('renders an empty value as text', () => {
    render(<Harness initial="" />);
    expect(screen.getByLabelText('Token')).toHaveAttribute('type', 'text');
  });

  it('toggles reveal', () => {
    render(<Harness initial="sk-literal-123" />);
    const show = screen.getByRole('button', { name: 'Show value' });
    expect(show).toHaveAttribute('aria-pressed', 'false');
    fireEvent.click(show);
    expect(screen.getByLabelText('Token')).toHaveAttribute('type', 'text');
    const hide = screen.getByRole('button', { name: 'Hide value' });
    expect(hide).toHaveAttribute('aria-pressed', 'true');
    fireEvent.click(hide);
    expect(screen.getByLabelText('Token')).toHaveAttribute('type', 'password');
  });

  it('stores a new variable and replaces the field with its reference', async () => {
    vi.mocked(fetchVariables)
      .mockResolvedValueOnce([])
      .mockResolvedValueOnce([{ key: 'API_TOKEN' } as Awaited<ReturnType<typeof fetchVariables>>[number]]);
    vi.mocked(createVariable).mockResolvedValue(undefined);
    const onValue = vi.fn();
    render(<Harness initial="sk-literal-123" onValue={onValue} suggestedKey="api-token" />);

    fireEvent.click(screen.getByRole('button', { name: /store as variable/i }));
    expect(screen.getByLabelText('Variable name')).toHaveValue('API_TOKEN');
    fireEvent.click(screen.getByRole('button', { name: 'Save variable' }));

    await waitFor(() => expect(onValue).toHaveBeenCalledWith('${var:API_TOKEN}'));
    expect(createVariable).toHaveBeenCalledWith(
      expect.objectContaining({ key: 'API_TOKEN', value: 'sk-literal-123', type: 'string', isSecret: true }),
    );
    expect(useVaultStore.getState().variables).toEqual([{ key: 'API_TOKEN' }]);
    expect(screen.getByLabelText('Token')).toHaveValue('${var:API_TOKEN}');
  });

  it('refuses an existing key without creating or replacing', async () => {
    vi.mocked(fetchVariables).mockResolvedValue([{ key: 'MY_TOKEN' } as Awaited<ReturnType<typeof fetchVariables>>[number]]);
    const onValue = vi.fn();
    render(<Harness initial="sk-literal-123" onValue={onValue} suggestedKey="MY_TOKEN" />);

    fireEvent.click(screen.getByRole('button', { name: /store as variable/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Save variable' }));

    expect(await screen.findByRole('alert')).toHaveTextContent(
      'A variable named MY_TOKEN already exists. Pick another name or insert it with the key icon.',
    );
    expect(createVariable).not.toHaveBeenCalled();
    expect(onValue).not.toHaveBeenCalled();
    expect(screen.getByLabelText('Token')).toHaveValue('sk-literal-123');
  });

  it('maps a 409 from the server to the already-exists message', async () => {
    vi.mocked(fetchVariables).mockResolvedValue([]);
    vi.mocked(createVariable).mockRejectedValue(new HTTPError(409, 'conflict'));
    const onValue = vi.fn();
    render(<Harness initial="sk-literal-123" onValue={onValue} suggestedKey="MY_TOKEN" />);
    fireEvent.click(screen.getByRole('button', { name: /store as variable/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Save variable' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('already exists');
    expect(onValue).not.toHaveBeenCalled();
  });

  it('shows a locked store error and keeps the literal', async () => {
    vi.mocked(fetchVariables).mockRejectedValue(new HTTPError(423, 'locked'));
    const onValue = vi.fn();
    render(<Harness initial="sk-literal-123" onValue={onValue} suggestedKey="MY_TOKEN" />);
    fireEvent.click(screen.getByRole('button', { name: /store as variable/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Save variable' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/locked/i);
    expect(screen.getByRole('alert')).not.toHaveTextContent('sk-literal-123');
    expect(onValue).not.toHaveBeenCalled();
    expect(screen.getByLabelText('Token')).toHaveValue('sk-literal-123');
  });

  it('rejects an invalid key name', async () => {
    render(<Harness initial="sk-literal-123" />);
    fireEvent.click(screen.getByRole('button', { name: /store as variable/i }));
    fireEvent.change(screen.getByLabelText('Variable name'), { target: { value: '1abc' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save variable' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/uppercase name/);
    expect(fetchVariables).not.toHaveBeenCalled();
  });
});
