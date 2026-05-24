import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
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

function renderPanel() {
  return render(
    <MemoryRouter>
      <VaultPanel onClose={noop} />
    </MemoryRouter>,
  );
}

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
    renderPanel();
    expect(screen.getByPlaceholderText('secret value')).toBeInTheDocument();
  });

  it('switches to a plaintext hint when Plaintext is selected', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: /plaintext/i }));
    expect(screen.queryByPlaceholderText('secret value')).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText('plaintext value')).toBeInTheDocument();
  });

  it('hints comma-separated input when list type is selected', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'list' }));
    expect(screen.getByPlaceholderText('item1, item2, item3')).toBeInTheDocument();
  });

  it('shows the JSON editor when json type is selected', async () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'json' }));
    expect(await screen.findByLabelText('JSON value')).toBeInTheDocument();
  });

  it('hints a number example when number type is selected', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'number' }));
    expect(screen.getByPlaceholderText('42')).toBeInTheDocument();
  });

  it('renders a toggle switch when bool type is selected', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: 'bool' }));
    expect(screen.getByRole('switch')).toBeInTheDocument();
  });

  it('leaves the key-input placeholder generic regardless of type', async () => {
    renderPanel();
    expect(screen.getByPlaceholderText('KEY_NAME')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'json' }));
    await screen.findByLabelText('JSON value');
    expect(screen.getByPlaceholderText('KEY_NAME')).toBeInTheDocument();
  });
});

describe('VaultPanel — value input masking follows Secret/Plaintext', () => {
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

  it('masks the value input by default (Secret)', () => {
    renderPanel();
    expect(screen.getByPlaceholderText('secret value')).toHaveAttribute(
      'type',
      'password',
    );
  });

  it('reveals the value input when Plaintext is selected', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: /plaintext/i }));
    expect(screen.getByPlaceholderText('plaintext value')).toHaveAttribute(
      'type',
      'text',
    );
  });

  it('re-masks the value input when switching back to Secret', () => {
    renderPanel();
    fireEvent.click(screen.getByRole('button', { name: /plaintext/i }));
    fireEvent.click(screen.getByRole('button', { name: /secret/i }));
    expect(screen.getByPlaceholderText('secret value')).toHaveAttribute(
      'type',
      'password',
    );
  });
});

describe('VaultPanel — sidebar scoped down to quick-lookup', () => {
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

  it('does not render an Encrypt action button', () => {
    renderPanel();
    expect(
      screen.queryByRole('button', { name: /^encrypt$/i }),
    ).not.toBeInTheDocument();
  });

  it('does not render a "+ New set" button', () => {
    renderPanel();
    expect(
      screen.queryByRole('button', { name: /new set/i }),
    ).not.toBeInTheDocument();
  });

  it('does not render a "Create a variable set" CTA when there are no sets', () => {
    renderPanel();
    expect(
      screen.queryByRole('button', { name: /create a variable set/i }),
    ).not.toBeInTheDocument();
  });

  it('renders the footer hint linking to /vault when unlocked', () => {
    renderPanel();
    const link = screen.getByRole('link', {
      name: /manage sets and bulk-import in the variables workspace/i,
    });
    expect(link).toBeInTheDocument();
    expect(link).toHaveAttribute('href', '/vault');
  });

  it('hides the footer hint when the vault is locked', () => {
    useVaultStore.setState({
      variables: null,
      sets: null,
      loading: false,
      error: null,
      locked: true,
      encrypted: true,
    });
    renderPanel();
    expect(
      screen.queryByRole('link', {
        name: /manage sets and bulk-import in the variables workspace/i,
      }),
    ).not.toBeInTheDocument();
  });
});
