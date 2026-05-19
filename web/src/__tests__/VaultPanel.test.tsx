import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import '@testing-library/jest-dom';
import { VaultPanel } from '../components/vault/VaultPanel';
import { useVaultStore } from '../stores/useVaultStore';

// Stub every api function VaultPanel imports. The placeholder assertions
// don't depend on data — they just need the form to render — so resolved
// no-op promises are sufficient.
vi.mock('../lib/api', () => ({
  fetchVariables: vi.fn().mockResolvedValue([]),
  fetchVariableSets: vi.fn().mockResolvedValue([]),
  createVariable: vi.fn().mockResolvedValue(undefined),
  getVariable: vi.fn().mockResolvedValue({ value: '' }),
  updateVariable: vi.fn().mockResolvedValue(undefined),
  deleteVariable: vi.fn().mockResolvedValue(undefined),
  createVariableSet: vi.fn().mockResolvedValue(undefined),
  deleteVariableSet: vi.fn().mockResolvedValue(undefined),
  assignVariableToSet: vi.fn().mockResolvedValue(undefined),
  fetchVariableStoreStatus: vi
    .fn()
    .mockResolvedValue({ locked: false, encrypted: false }),
  unlockVariableStore: vi.fn().mockResolvedValue(undefined),
  lockVariableStore: vi.fn().mockResolvedValue(undefined),
}));

const noop = () => {};

describe('VaultPanel — value placeholder adapts to type and visibility', () => {
  beforeEach(() => {
    useVaultStore.setState({
      variables: [],
      sets: [],
      loading: false,
      error: null,
      locked: false,
      encrypted: false,
    });
  });

  it('shows a secret-value hint by default (string + Secret)', () => {
    render(<VaultPanel onClose={noop} />);
    expect(screen.getByPlaceholderText('secret value')).toBeInTheDocument();
  });

  it('switches to a plaintext hint when Plaintext is selected', () => {
    render(<VaultPanel onClose={noop} />);
    fireEvent.click(screen.getByRole('button', { name: /plaintext/i }));
    expect(screen.queryByPlaceholderText('secret value')).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText('plaintext value')).toBeInTheDocument();
  });

  it('hints comma-separated input when list type is selected', () => {
    render(<VaultPanel onClose={noop} />);
    fireEvent.click(screen.getByRole('button', { name: 'list' }));
    expect(screen.getByPlaceholderText('item1, item2, item3')).toBeInTheDocument();
  });

  it('hints JSON object syntax when json type is selected', () => {
    render(<VaultPanel onClose={noop} />);
    fireEvent.click(screen.getByRole('button', { name: 'json' }));
    expect(screen.getByPlaceholderText('{"key": "value"}')).toBeInTheDocument();
  });

  it('hints a number example when number type is selected', () => {
    render(<VaultPanel onClose={noop} />);
    fireEvent.click(screen.getByRole('button', { name: 'number' }));
    expect(screen.getByPlaceholderText('42')).toBeInTheDocument();
  });

  it('hints true/false when bool type is selected', () => {
    render(<VaultPanel onClose={noop} />);
    fireEvent.click(screen.getByRole('button', { name: 'bool' }));
    expect(screen.getByPlaceholderText('true or false')).toBeInTheDocument();
  });

  it('leaves the key-input placeholder generic regardless of type', () => {
    render(<VaultPanel onClose={noop} />);
    expect(screen.getByPlaceholderText('KEY_NAME')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'json' }));
    expect(screen.getByPlaceholderText('KEY_NAME')).toBeInTheDocument();
  });
});
