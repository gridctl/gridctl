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
    fetchVariableUsage: vi.fn().mockResolvedValue({}),
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

describe('VaultWorkspace — server filter', () => {
  const testVariables = [
    { key: 'POSTGRES_URL', type: 'string' as const, is_secret: true },
    { key: 'POSTGRES_PASSWORD', type: 'string' as const, is_secret: true },
    { key: 'REDIS_URL', type: 'string' as const, is_secret: false },
  ];

  // Exact usage index: which server/resource references each variable. The
  // filter now matches this (not a key substring), so POSTGRES_PASSWORD counts
  // for `postgres` even though its key shares no substring with another server.
  const testUsage = {
    POSTGRES_URL: [
      { kind: 'mcp-server' as const, name: 'postgres', field: 'env.POSTGRES_URL' },
    ],
    POSTGRES_PASSWORD: [
      { kind: 'resource' as const, name: 'postgres', field: 'env.POSTGRES_PASSWORD' },
    ],
    REDIS_URL: [
      { kind: 'mcp-server' as const, name: 'redis', field: 'env.REDIS_URL' },
    ],
  };

  beforeEach(() => {
    vi.mocked(api.fetchVariableStoreStatus).mockResolvedValue({
      locked: false,
      encrypted: false,
    });
    vi.mocked(api.fetchVariables).mockResolvedValue(testVariables);
    vi.mocked(api.fetchVariableUsage).mockResolvedValue(testUsage);
    useVaultStore.setState({
      variables: testVariables,
      sets: [],
      usage: testUsage,
      loading: false,
      error: null,
      locked: false,
      encrypted: false,
    });
  });

  it('filters to the exact consumers of the deep-linked server', async () => {
    render(
      <MemoryRouter initialEntries={['/vault?filter=server:postgres']}>
        <VaultWorkspace />
      </MemoryRouter>,
    );
    expect(await screen.findByText('POSTGRES_URL')).toBeInTheDocument();
    expect(screen.getByText('POSTGRES_PASSWORD')).toBeInTheDocument();
    expect(screen.queryByText('REDIS_URL')).not.toBeInTheDocument();
    // Exact-match banner — no "approximate" disclaimer.
    expect(screen.getByText(/variables used by/i)).toBeInTheDocument();
    expect(screen.getByText('postgres')).toBeInTheDocument();
    expect(screen.queryByText(/approximate/i)).not.toBeInTheDocument();
  });

  it('excludes a variable whose key contains the server name but is not referenced by it', async () => {
    // REDIS_URL's key has no "postgres" substring, and POSTGRES_* are only kept
    // because the usage index — not the key text — links them to postgres.
    render(
      <MemoryRouter initialEntries={['/vault?filter=server:redis']}>
        <VaultWorkspace />
      </MemoryRouter>,
    );
    expect(await screen.findByText('REDIS_URL')).toBeInTheDocument();
    expect(screen.queryByText('POSTGRES_URL')).not.toBeInTheDocument();
  });

  it('clears the banner and removes the filter when Clear is clicked', async () => {
    render(
      <MemoryRouter initialEntries={['/vault?filter=server:postgres']}>
        <VaultWorkspace />
      </MemoryRouter>,
    );
    const clearBtn = await screen.findByRole('button', {
      name: /clear server filter/i,
    });
    fireEvent.click(clearBtn);
    expect(await screen.findByText('REDIS_URL')).toBeInTheDocument();
    expect(screen.queryByText(/variables used by/i)).not.toBeInTheDocument();
  });

  it('warns about consumers in the delete confirmation', async () => {
    render(
      <MemoryRouter initialEntries={['/vault']}>
        <VaultWorkspace />
      </MemoryRouter>,
    );
    // Expand the POSTGRES_URL row, then trigger its Delete action.
    const row = await screen.findByRole('button', { name: /POSTGRES_URL/i });
    fireEvent.click(row);
    fireEvent.click(screen.getByRole('button', { name: /^delete$/i }));

    expect(await screen.findByText(/used by 1 consumer/i)).toBeInTheDocument();
    expect(screen.getByText(/may break it/i)).toBeInTheDocument();
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

describe('VaultWorkspace — recently edited indicator', () => {
  const variables = [
    { key: 'API_KEY', type: 'string' as const, is_secret: true, set: 'dev' },
    { key: 'DB_URL', type: 'string' as const, is_secret: true, set: 'prod' },
  ];
  const sets = [
    { name: 'dev', count: 1 },
    { name: 'prod', count: 1 },
  ];

  beforeEach(() => {
    vi.mocked(api.fetchVariableStoreStatus).mockResolvedValue({
      locked: false,
      encrypted: false,
    });
    vi.mocked(api.fetchVariables).mockResolvedValue(variables);
    vi.mocked(api.fetchVariableSets).mockResolvedValue(sets);
    vi.mocked(api.fetchVariableUsage).mockResolvedValue({});
  });

  it('marks only the set whose member was edited this session', async () => {
    useVaultStore.setState({
      variables,
      sets,
      usage: {},
      recentlyEdited: { API_KEY: Date.now() },
      loading: false,
      error: null,
      locked: false,
      encrypted: false,
    });
    renderWorkspace();
    // API_KEY belongs to "dev", so exactly one set pill carries the dot.
    const dots = await screen.findAllByTitle('Recently edited');
    expect(dots).toHaveLength(1);
    expect(dots[0]).toHaveAttribute('aria-label', 'Recently edited');
  });

  it('shows no indicator when nothing was edited this session', async () => {
    useVaultStore.setState({
      variables,
      sets,
      usage: {},
      recentlyEdited: {},
      loading: false,
      error: null,
      locked: false,
      encrypted: false,
    });
    renderWorkspace();
    await screen.findByRole('button', { name: /all variables/i });
    expect(screen.queryByTitle('Recently edited')).not.toBeInTheDocument();
  });
});
