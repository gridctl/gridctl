import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import '@testing-library/jest-dom';
import { VaultWorkspace } from '../components/workspaces/VaultWorkspace';
import { useVaultStore } from '../stores/useVaultStore';
import * as api from '../lib/api';

// Resolve every vault API call to a benign no-op so the workspace renders
// without ever hitting the network.
vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
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
    importVariables: vi.fn().mockResolvedValue({ imported: 0 }),
  };
});

function renderWorkspace() {
  return render(
    <MemoryRouter initialEntries={['/vault']}>
      <VaultWorkspace />
    </MemoryRouter>,
  );
}

describe('VaultWorkspace — empty state', () => {
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

  it('renders the workspace header label', async () => {
    renderWorkspace();
    expect(await screen.findByText('variables')).toBeInTheDocument();
  });

  it('renders an Import .env button in the header', async () => {
    renderWorkspace();
    expect(
      await screen.findByRole('button', { name: /^import \.env$/i }),
    ).toBeInTheDocument();
  });

  it('shows Import from .env as the primary empty-state CTA', async () => {
    renderWorkspace();
    expect(
      await screen.findByRole('button', { name: /import from \.env/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /add one manually/i }),
    ).toBeInTheDocument();
  });

  it('opens the import modal when the header CTA is clicked', async () => {
    renderWorkspace();
    const cta = await screen.findByRole('button', { name: /^import \.env$/i });
    fireEvent.click(cta);
    expect(
      await screen.findByRole('dialog', { name: /import variables/i }),
    ).toBeInTheDocument();
  });

  it('renders an "All variables" pill in the left rail', async () => {
    renderWorkspace();
    expect(
      await screen.findByRole('button', { name: /all variables/i }),
    ).toBeInTheDocument();
  });
});

describe('VaultWorkspace — locked state', () => {
  beforeEach(() => {
    vi.mocked(api.fetchVariableStoreStatus).mockResolvedValue({
      locked: true,
      encrypted: true,
    });
    useVaultStore.setState({
      variables: null,
      sets: null,
      loading: false,
      error: null,
      locked: true,
      encrypted: true,
    });
  });

  it('renders the unlock prompt and no header actions', async () => {
    renderWorkspace();
    // Wait for the workspace shell to settle so the lock prompt has rendered.
    await screen.findByText('variables');
    await screen.findByText('Vault Locked');
    const passphraseInput = document.querySelector('input[type="password"]');
    expect(passphraseInput).not.toBeNull();
    // Header Import/Encrypt actions should not be present when locked.
    expect(
      screen.queryByRole('button', { name: /^import \.env$/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /^encrypt$/i }),
    ).not.toBeInTheDocument();
  });
});
