import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import '@testing-library/jest-dom';
import { VaultWorkspace } from '../components/workspaces/VaultWorkspace';
import { ToastContainer } from '../components/ui/Toast';
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

// Renders the workspace alongside a ToastContainer so drop-validation toasts
// (normally mounted by AppShell) are assertable in isolation.
function renderWithToasts() {
  return render(
    <MemoryRouter initialEntries={['/vault']}>
      <VaultWorkspace />
      <ToastContainer />
    </MemoryRouter>,
  );
}

// Duck-typed File — the drop path only reads name/type/text().
function fakeFile(name: string, content: string, type = ''): File {
  return { name, type, text: () => Promise.resolve(content) } as unknown as File;
}

function dispatchDrag(type: string, files: unknown[] = []) {
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.assign(event, { dataTransfer: { types: ['Files'], files } });
  act(() => {
    window.dispatchEvent(event);
  });
}

const OVERLAY_COPY = /drop a \.env or \.json file to import/i;

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
    // Select the POSTGRES_URL row, then delete from the inspector header.
    const row = await screen.findByRole('option', { name: /POSTGRES_URL/i });
    fireEvent.click(row);
    fireEvent.click(
      await screen.findByRole('button', { name: /delete variable/i }),
    );

    expect(await screen.findByText(/used by 1 consumer/i)).toBeInTheDocument();
    expect(screen.getByText(/may break it/i)).toBeInTheDocument();
  });

  it('warns about set injection when deleting a set-injected variable', async () => {
    const injectedUsage = {
      POSTGRES_URL: [
        {
          kind: 'secrets-set' as const,
          name: 'dev',
          field: 'secrets.sets',
        },
      ],
    };
    vi.mocked(api.fetchVariableUsage).mockResolvedValue(injectedUsage);
    useVaultStore.setState({ usage: injectedUsage, usageLoaded: true });

    render(
      <MemoryRouter initialEntries={['/vault']}>
        <VaultWorkspace />
      </MemoryRouter>,
    );
    const row = await screen.findByRole('option', { name: /POSTGRES_URL/i });
    fireEvent.click(row);
    fireEvent.click(
      await screen.findByRole('button', { name: /delete variable/i }),
    );

    // The generic consumer warning fires (count > 0) plus the injection note.
    expect(await screen.findByText(/used by 1 consumer/i)).toBeInTheDocument();
    expect(
      screen.getByText(/injected into every server env/i),
    ).toBeInTheDocument();
  });

  it('finds variables by consumer server name in search', async () => {
    // "postgres" appears in POSTGRES_URL's consumers, not only its key; the
    // decisive case is REDIS_URL staying hidden while a usage-only match
    // (consumer name) keeps a variable visible.
    render(
      <MemoryRouter initialEntries={['/vault?q=postgres']}>
        <VaultWorkspace />
      </MemoryRouter>,
    );
    expect(await screen.findByText('POSTGRES_URL')).toBeInTheDocument();
    expect(screen.getByText('POSTGRES_PASSWORD')).toBeInTheDocument();
    expect(screen.queryByText('REDIS_URL')).not.toBeInTheDocument();
  });

  it('matches a variable whose key shares nothing with the query via usage', async () => {
    const vars = [
      { key: 'DB_TOKEN', type: 'string' as const, is_secret: true },
      { key: 'OTHER', type: 'string' as const, is_secret: true },
    ];
    const usageByName = {
      DB_TOKEN: [
        { kind: 'mcp-server' as const, name: 'zapier', field: 'command[4]' },
      ],
    };
    vi.mocked(api.fetchVariables).mockResolvedValue(vars);
    vi.mocked(api.fetchVariableUsage).mockResolvedValue(usageByName);
    useVaultStore.setState({ variables: vars, usage: usageByName });

    render(
      <MemoryRouter initialEntries={['/vault?q=zapier']}>
        <VaultWorkspace />
      </MemoryRouter>,
    );
    expect(await screen.findByText('DB_TOKEN')).toBeInTheDocument();
    expect(screen.queryByText('OTHER')).not.toBeInTheDocument();
  });

  it('jumps to Stack when a consumer is clicked', async () => {
    render(
      <MemoryRouter initialEntries={['/vault']}>
        <Routes>
          <Route path="/vault" element={<VaultWorkspace />} />
          <Route path="/stack" element={<div>STACK PAGE</div>} />
        </Routes>
      </MemoryRouter>,
    );
    const row = await screen.findByRole('option', { name: /POSTGRES_URL/i });
    fireEvent.click(row);
    fireEvent.click(
      await screen.findByRole('button', { name: /go to postgres/i }),
    );
    expect(await screen.findByText('STACK PAGE')).toBeInTheDocument();
  });
});

describe('VaultWorkspace — inspector selection', () => {
  const testVariables = [
    { key: 'POSTGRES_URL', type: 'string' as const, is_secret: true },
    { key: 'REDIS_URL', type: 'string' as const, is_secret: true },
  ];
  const testUsage = {
    POSTGRES_URL: [
      { kind: 'mcp-server' as const, name: 'postgres', field: 'env.POSTGRES_URL' },
    ],
  };

  beforeEach(() => {
    vi.mocked(api.fetchVariableStoreStatus).mockResolvedValue({
      locked: false,
      encrypted: false,
    });
    vi.mocked(api.fetchVariables).mockResolvedValue(testVariables);
    vi.mocked(api.fetchVariableSets).mockResolvedValue([]);
    vi.mocked(api.fetchVariableUsage).mockResolvedValue(testUsage);
    vi.mocked(api.deleteVariable).mockResolvedValue(undefined);
    useVaultStore.setState({
      variables: testVariables,
      sets: [],
      usage: testUsage,
      recentlyEdited: {},
      loading: false,
      error: null,
      locked: false,
      encrypted: false,
    });
  });

  it('shows the overview pane while nothing is selected', async () => {
    renderWorkspace();
    expect(await screen.findByText('Variables overview')).toBeInTheDocument();
    expect(
      screen.queryByRole('heading', { name: 'POSTGRES_URL' }),
    ).not.toBeInTheDocument();
  });

  it('selects a row on click and shows its detail with usage first', async () => {
    renderWorkspace();
    const row = await screen.findByRole('option', { name: /POSTGRES_URL/i });
    fireEvent.click(row);

    expect(
      await screen.findByRole('option', { name: /POSTGRES_URL/i, selected: true }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('heading', { name: 'POSTGRES_URL' }),
    ).toBeInTheDocument();
    expect(screen.getByText('Referenced in stack')).toBeInTheDocument();
    expect(screen.getByText(/used by 1 site/i)).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: /go to postgres/i }),
    ).toBeInTheDocument();
  });

  it('restores the selection from a ?selected= deep link', async () => {
    render(
      <MemoryRouter initialEntries={['/vault?selected=POSTGRES_URL']}>
        <VaultWorkspace />
      </MemoryRouter>,
    );
    expect(
      await screen.findByRole('option', { name: /POSTGRES_URL/i, selected: true }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole('heading', { name: 'POSTGRES_URL' }),
    ).toBeInTheDocument();
  });

  it('shows the orphan callout for a variable with no consumers', async () => {
    renderWorkspace();
    const row = await screen.findByRole('option', { name: /REDIS_URL/i });
    fireEvent.click(row);
    expect(
      await screen.findByText(/not referenced by/i),
    ).toBeInTheDocument();
  });

  it('falls back to the overview when the selection is filtered out', async () => {
    // The search query keeps REDIS_URL only, so the selected POSTGRES_URL is
    // filtered out of the list and the pane shows the overview instead.
    render(
      <MemoryRouter initialEntries={['/vault?selected=POSTGRES_URL&q=REDIS']}>
        <VaultWorkspace />
      </MemoryRouter>,
    );
    expect(await screen.findByText('Variables overview')).toBeInTheDocument();
    expect(
      screen.queryByRole('option', { name: /POSTGRES_URL/i }),
    ).not.toBeInTheDocument();
  });

  it('clears the selection after deleting the inspected variable', async () => {
    renderWorkspace();
    const row = await screen.findByRole('option', { name: /POSTGRES_URL/i });
    fireEvent.click(row);
    fireEvent.click(
      await screen.findByRole('button', { name: /delete variable/i }),
    );
    fireEvent.click(
      await screen.findByRole('button', { name: /delete "POSTGRES_URL"/i }),
    );

    expect(await screen.findByText('Variables overview')).toBeInTheDocument();
    expect(vi.mocked(api.deleteVariable)).toHaveBeenCalledWith('POSTGRES_URL');
  });

  it('closes the inspector via its close button', async () => {
    renderWorkspace();
    fireEvent.click(
      await screen.findByRole('option', { name: /POSTGRES_URL/i }),
    );
    await screen.findByRole('heading', { name: 'POSTGRES_URL' });
    fireEvent.click(screen.getByRole('button', { name: /close inspector/i }));
    expect(await screen.findByText('Variables overview')).toBeInTheDocument();
  });

  it('moves the selection with arrow keys', async () => {
    renderWorkspace();
    await screen.findByRole('option', { name: /POSTGRES_URL/i });

    fireEvent.keyDown(document.body, { key: 'ArrowDown' });
    expect(
      await screen.findByRole('option', { name: /POSTGRES_URL/i, selected: true }),
    ).toBeInTheDocument();

    fireEvent.keyDown(document.body, { key: 'ArrowDown' });
    expect(
      await screen.findByRole('option', { name: /REDIS_URL/i, selected: true }),
    ).toBeInTheDocument();
  });
});

describe('VaultWorkspace — unassigned filter', () => {
  const variables = [
    { key: 'API_KEY', type: 'string' as const, is_secret: true, set: 'dev' },
    { key: 'LOOSE_VAR', type: 'string' as const, is_secret: false },
  ];
  const sets = [{ name: 'dev', count: 1 }];

  beforeEach(() => {
    vi.mocked(api.fetchVariableStoreStatus).mockResolvedValue({
      locked: false,
      encrypted: true,
    });
    vi.mocked(api.fetchVariables).mockResolvedValue(variables);
    vi.mocked(api.fetchVariableSets).mockResolvedValue(sets);
    vi.mocked(api.fetchVariableUsage).mockResolvedValue({});
    useVaultStore.setState({
      variables,
      sets,
      usage: {},
      usageLoaded: true,
      recentlyEdited: {},
      loading: false,
      error: null,
      locked: false,
      encrypted: true,
    });
  });

  it('renders an Unassigned pill and filters to variables without a set', async () => {
    renderWorkspace();
    const pill = await screen.findByRole('button', { name: /unassigned/i });
    fireEvent.click(pill);

    expect(await screen.findByText('LOOSE_VAR')).toBeInTheDocument();
    expect(screen.queryByText('API_KEY')).not.toBeInTheDocument();
  });

  it('restores the unassigned filter from ?set=__none__', async () => {
    render(
      <MemoryRouter initialEntries={['/vault?set=__none__']}>
        <VaultWorkspace />
      </MemoryRouter>,
    );
    expect(await screen.findByText('LOOSE_VAR')).toBeInTheDocument();
    expect(screen.queryByText('API_KEY')).not.toBeInTheDocument();
  });
});

describe('VaultWorkspace — unencrypted secrets banner', () => {
  const variables = [
    { key: 'API_KEY', type: 'string' as const, is_secret: true },
  ];

  beforeEach(() => {
    sessionStorage.clear();
    vi.mocked(api.fetchVariableStoreStatus).mockResolvedValue({
      locked: false,
      encrypted: false,
    });
    vi.mocked(api.fetchVariables).mockResolvedValue(variables);
    vi.mocked(api.fetchVariableSets).mockResolvedValue([]);
    vi.mocked(api.fetchVariableUsage).mockResolvedValue({});
    useVaultStore.setState({
      variables,
      sets: [],
      usage: {},
      usageLoaded: true,
      recentlyEdited: {},
      loading: false,
      error: null,
      locked: false,
      encrypted: false,
    });
  });

  it('nudges to encrypt and opens the encrypt drawer', async () => {
    renderWorkspace();
    expect(
      await screen.findByText(/secrets are stored unencrypted/i),
    ).toBeInTheDocument();

    // The banner's Encrypt action opens the passphrase drawer in encrypt mode.
    const banner = screen
      .getByText(/secrets are stored unencrypted/i)
      .closest('div')?.parentElement;
    const encryptBtn = Array.from(
      banner?.querySelectorAll('button') ?? [],
    ).find((b) => b.textContent?.match(/encrypt/i));
    expect(encryptBtn).toBeTruthy();
    fireEvent.click(encryptBtn!);
    expect(
      await screen.findByPlaceholderText('New passphrase'),
    ).toBeInTheDocument();
  });

  it('dismisses for the session', async () => {
    renderWorkspace();
    fireEvent.click(
      await screen.findByRole('button', {
        name: /dismiss unencrypted secrets warning/i,
      }),
    );
    expect(
      screen.queryByText(/secrets are stored unencrypted/i),
    ).not.toBeInTheDocument();
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

describe('VaultWorkspace — drag-and-drop import', () => {
  beforeEach(() => {
    // Reset fetch mocks so a prior block's variables don't leak in and create
    // duplicate-key matches between the list and the modal preview.
    vi.mocked(api.fetchVariables).mockResolvedValue([]);
    vi.mocked(api.fetchVariableSets).mockResolvedValue([]);
    vi.mocked(api.fetchVariableUsage).mockResolvedValue({});
    vi.mocked(api.fetchVariableStoreStatus).mockResolvedValue({
      locked: false,
      encrypted: false,
    });
    useVaultStore.setState({
      variables: [],
      sets: [],
      usage: {},
      recentlyEdited: {},
      loading: false,
      error: null,
      locked: false,
      encrypted: false,
    });
  });

  it('shows the dropzone overlay while a file is dragged over the page', async () => {
    renderWithToasts();
    await screen.findByText('variables');
    dispatchDrag('dragenter');
    expect(await screen.findByText(OVERLAY_COPY)).toBeInTheDocument();
  });

  it('opens the import modal pre-populated when a .env file is dropped', async () => {
    renderWithToasts();
    await screen.findByText('variables');
    dispatchDrag('drop', [fakeFile('app.env', 'FOO=bar')]);
    expect(
      await screen.findByRole('dialog', { name: /import variables/i }),
    ).toBeInTheDocument();
    expect(await screen.findByText('FOO')).toBeInTheDocument();
  });

  it('parses a dropped .json file into the preview', async () => {
    renderWithToasts();
    await screen.findByText('variables');
    dispatchDrag('drop', [
      fakeFile('vars.json', '{"variables":[{"key":"API_KEY","value":"sk"}]}'),
    ]);
    expect(
      await screen.findByRole('dialog', { name: /import variables/i }),
    ).toBeInTheDocument();
    expect(await screen.findByText('API_KEY')).toBeInTheDocument();
  });

  it('rejects an unsupported file type with a toast and no modal', async () => {
    renderWithToasts();
    await screen.findByText('variables');
    dispatchDrag('drop', [fakeFile('photo.png', 'binary', 'image/png')]);
    expect(
      await screen.findByText(/only \.env and \.json files/i),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole('dialog', { name: /import variables/i }),
    ).not.toBeInTheDocument();
  });

  it('warns and skips an empty file', async () => {
    renderWithToasts();
    await screen.findByText('variables');
    dispatchDrag('drop', [fakeFile('empty.env', '   ')]);
    expect(await screen.findByText(/file looks empty/i)).toBeInTheDocument();
    expect(
      screen.queryByRole('dialog', { name: /import variables/i }),
    ).not.toBeInTheDocument();
  });

  it('imports the first of multiple dropped files and warns', async () => {
    renderWithToasts();
    await screen.findByText('variables');
    dispatchDrag('drop', [
      fakeFile('a.env', 'FOO=bar'),
      fakeFile('b.env', 'BAZ=qux'),
    ]);
    expect(await screen.findByText(/multiple files/i)).toBeInTheDocument();
    expect(
      await screen.findByRole('dialog', { name: /import variables/i }),
    ).toBeInTheDocument();
    expect(await screen.findByText('FOO')).toBeInTheDocument();
    expect(screen.queryByText('BAZ')).not.toBeInTheDocument();
  });

  it('suppresses the overlay while the import modal is already open', async () => {
    renderWithToasts();
    const cta = await screen.findByRole('button', { name: /^import \.env$/i });
    fireEvent.click(cta);
    await screen.findByRole('dialog', { name: /import variables/i });
    dispatchDrag('dragenter');
    expect(screen.queryByText(OVERLAY_COPY)).not.toBeInTheDocument();
  });
});

describe('VaultWorkspace — drag-and-drop while locked', () => {
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

  it('does not show the overlay or open the modal when locked', async () => {
    renderWithToasts();
    await screen.findByText('Vault Locked');
    dispatchDrag('dragenter');
    expect(screen.queryByText(OVERLAY_COPY)).not.toBeInTheDocument();
    dispatchDrag('drop', [fakeFile('a.env', 'FOO=bar')]);
    expect(
      screen.queryByRole('dialog', { name: /import variables/i }),
    ).not.toBeInTheDocument();
  });
});
