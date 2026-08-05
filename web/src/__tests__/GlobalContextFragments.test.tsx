import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { GlobalContextDialog } from '../components/context/GlobalContextDialog';
import { useContextStore } from '../stores/useContextStore';
import type { ContextDoc, ContextFragment } from '../lib/api';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchGlobalContext: vi.fn(),
    saveGlobalContext: vi.fn(),
    scanGlobalContext: vi.fn(),
    initGlobalContext: vi.fn(),
    syncGlobalContext: vi.fn(),
    adoptGlobalContext: vi.fn(),
    unsyncGlobalContext: vi.fn(),
    fetchGlobalContextDiff: vi.fn(),
    fetchContextFragments: vi.fn(),
    saveContextFragment: vi.fn(),
    deleteContextFragment: vi.fn(),
  };
});

import {
  adoptGlobalContext,
  deleteContextFragment,
  fetchContextFragments,
  fetchGlobalContext,
  fetchGlobalContextDiff,
  saveContextFragment,
} from '../lib/api';

const fragmentsDoc: ContextDoc = {
  canonical: { path: '/home/u/.gridctl/context/AGENTS.md', exists: false, content: '' },
  fragments_active: true,
  needs_sync: false,
  clients: [
    {
      slug: 'claude-code',
      name: 'Claude Code',
      supported: true,
      available: true,
      strategy: 'dedicated-file',
      mode: 'multi-file',
      target_path: '/home/u/.claude/rules',
      state: 'in-sync',
    },
    {
      slug: 'opencode',
      name: 'OpenCode',
      supported: true,
      available: true,
      strategy: 'block',
      mode: 'compiled',
      target_path: '/home/u/.config/opencode/AGENTS.md',
      state: 'in-sync',
    },
  ],
};

const singleFileDoc: ContextDoc = {
  canonical: { path: '/home/u/.gridctl/context/AGENTS.md', exists: true, content: '# Rules\n' },
  needs_sync: false,
  clients: [],
};

const fragments: ContextFragment[] = [
  {
    name: '00-default',
    content: '# Default rules\n',
    bytes: 16,
    position: 1,
  },
  {
    name: '10-style',
    description: 'style rules',
    paths: ['**/*.go'],
    content: '---\npaths:\n  - "**/*.go"\n---\n\nGo style.\n',
    bytes: 42,
    position: 2,
  },
];

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  useContextStore.setState({ doc: null, loading: false, error: null });
  vi.mocked(fetchGlobalContext).mockResolvedValue(fragmentsDoc);
  vi.mocked(fetchContextFragments).mockResolvedValue({ active: true, fragments });
});

describe('GlobalContextDialog fragments mode', () => {
  it('routes to the fragments view with the rail in composition order', async () => {
    render(<GlobalContextDialog isOpen onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('00-default')).toBeInTheDocument();
    });
    expect(screen.getByText('10-style')).toBeInTheDocument();
    // The first fragment is selected and its content feeds the editor.
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Default rules\n');
    // Never the single-file setup view, even though canonical.exists=false.
    expect(screen.queryByText('Set up your global context')).not.toBeInTheDocument();
  });

  it('shows per-client mode chips in the clients strip', async () => {
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText('00-default')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByText('Clients'));
    expect(screen.getByText('multi-file')).toBeInTheDocument();
    expect(screen.getByText('compiled')).toBeInTheDocument();
  });

  it('saves an edited fragment through the fragment endpoint', async () => {
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '00-default' });
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    const editor = await screen.findByLabelText('Fragment 00-default');

    fireEvent.change(editor, { target: { value: '# Edited rules\n' } });
    fireEvent.click(screen.getByText('Save'));

    await waitFor(() => {
      expect(saveContextFragment).toHaveBeenCalledWith('00-default', '# Edited rules\n');
    });
  });

  it('switches fragments only when the draft is clean', async () => {
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    const editor = await screen.findByLabelText('Fragment 00-default');

    fireEvent.change(editor, { target: { value: '# Dirty\n' } });
    fireEvent.click(screen.getByText('10-style'));
    // The dirty draft blocks the switch so typing is never discarded.
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Dirty\n');

    fireEvent.change(editor, { target: { value: '# Default rules\n' } });
    fireEvent.click(screen.getByText('10-style'));
    await waitFor(() => {
      expect(screen.getByLabelText('Fragment 10-style')).toBeInTheDocument();
    });
  });

  it('creates a fragment from the rail add affordance', async () => {
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '20-testing' });
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await screen.findByText('00-default');

    fireEvent.click(screen.getByText('Add'));
    fireEvent.change(screen.getByLabelText('New fragment name'), {
      target: { value: '20-testing' },
    });
    fireEvent.click(screen.getByText('Create'));

    await waitFor(() => {
      expect(saveContextFragment).toHaveBeenCalledWith('20-testing', '');
    });
  });

  it('deletes a fragment from the rail', async () => {
    vi.mocked(deleteContextFragment).mockResolvedValue({ name: '10-style', backup: '/b' });
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await screen.findByText('10-style');

    fireEvent.click(screen.getByTitle('Remove fragment 10-style'));

    await waitFor(() => {
      expect(deleteContextFragment).toHaveBeenCalledWith('10-style');
    });
  });

  it('offers the split-into-fragments affordance from the single-file editor', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(singleFileDoc);
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '10-style', migrated: true });
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await screen.findByLabelText('Canonical global context');

    fireEvent.click(screen.getByText('Fragments'));
    await screen.findByText('Split into fragments');
    fireEvent.change(screen.getByLabelText('New fragment name'), {
      target: { value: '10-style' },
    });
    fireEvent.click(screen.getByText('Activate fragments'));

    await waitFor(() => {
      expect(saveContextFragment).toHaveBeenCalledWith('10-style', '');
    });
  });
});

// One client per doc keeps Review buttons unambiguous per test.
function clientDoc(client: ContextDoc['clients'][number]): ContextDoc {
  return {
    canonical: { path: '/home/u/.gridctl/context/AGENTS.md', exists: false, content: '' },
    fragments_active: true,
    needs_sync: true,
    clients: [client],
  };
}

const identityClient: ContextDoc['clients'][number] = {
  slug: 'claude-code',
  name: 'Claude Code',
  supported: true,
  available: true,
  strategy: 'dedicated-file',
  mode: 'multi-file',
  target_path: '/home/u/.claude/rules',
  state: 'drifted',
  fragments: [
    { name: '00-default', state: 'drifted' },
    { name: '10-style', state: 'stale' },
  ],
};

const lossyClient: ContextDoc['clients'][number] = {
  slug: 'vscode',
  name: 'VS Code',
  supported: true,
  available: true,
  strategy: 'dedicated-file',
  mode: 'multi-file',
  target_path: '/home/u/.copilot/instructions',
  state: 'drifted',
  fragments: [{ name: '00-default', state: 'drifted' }],
};

const compiledClient: ContextDoc['clients'][number] = {
  slug: 'opencode',
  name: 'OpenCode',
  supported: true,
  available: true,
  strategy: 'block',
  mode: 'compiled',
  target_path: '/home/u/.config/opencode/AGENTS.md',
  state: 'drifted',
};

describe('fragment-level drift review', () => {
  beforeEach(() => {
    vi.mocked(fetchGlobalContextDiff).mockResolvedValue('--- canonical\n+++ target\n');
    vi.mocked(adoptGlobalContext).mockResolvedValue(clientDoc(identityClient));
  });

  it('expands a multi-file row to every non-synced fragment, drifted and stale', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(identityClient));
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await screen.findByLabelText('Expand Claude Code fragments');

    fireEvent.click(screen.getByLabelText('Expand Claude Code fragments'));
    const list = screen.getByLabelText('Claude Code fragments');
    // The hidden-bucket case: the drifted fragment must not hide the stale one.
    expect(within(list).getByText('00-default')).toBeInTheDocument();
    expect(within(list).getByText('drifted')).toBeInTheDocument();
    expect(within(list).getByText('10-style')).toBeInTheDocument();
    expect(within(list).getByText('stale')).toBeInTheDocument();
  });

  it('adopts one fragment losslessly on the identity target', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(identityClient));
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await screen.findByLabelText('Expand Claude Code fragments');

    fireEvent.click(screen.getByLabelText('Expand Claude Code fragments'));
    const list = screen.getByLabelText('Claude Code fragments');
    fireEvent.click(within(list).getByText('Review'));

    // The diff request is fragment-scoped, not whole-client.
    await waitFor(() => {
      expect(fetchGlobalContextDiff).toHaveBeenCalledWith('claude-code', '00-default');
    });
    fireEvent.click(await screen.findByText('Adopt 00-default'));
    await waitFor(() => {
      expect(adoptGlobalContext).toHaveBeenCalledWith('claude-code', { fragment: '00-default' });
    });
  });

  it('shows the lossy reason with no Adopt affordance on lossy renders', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(lossyClient));
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await screen.findByLabelText('Expand VS Code fragments');

    fireEvent.click(screen.getByLabelText('Expand VS Code fragments'));
    const list = screen.getByLabelText('VS Code fragments');
    fireEvent.click(within(list).getByText('Review'));

    expect(await screen.findByTestId('lossy-reason')).toHaveTextContent(/lossy render/);
    expect(screen.queryByText(/^Adopt /)).not.toBeInTheDocument();
    expect(screen.getByText('Force overwrite')).toBeInTheDocument();
  });

  it('captures a compiled edit into an existing fragment', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(compiledClient));
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await screen.findByText('Review');

    fireEvent.click(screen.getByText('Review'));
    await screen.findByText(/receives one assembled document/);
    fireEvent.click(screen.getByText('Capture into 00-default'));

    await waitFor(() => {
      expect(adoptGlobalContext).toHaveBeenCalledWith('opencode', { into: '00-default' });
    });
  });

  it('captures a compiled edit into a new named fragment', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(compiledClient));
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await screen.findByText('Review');

    fireEvent.click(screen.getByText('Review'));
    const picker = await screen.findByLabelText('Capture into');
    fireEvent.change(picker, { target: { value: '__new__' } });
    fireEvent.change(screen.getByLabelText('New capture fragment name'), {
      target: { value: 'captured-notes' },
    });
    fireEvent.click(screen.getByText('Capture into captured-notes'));

    await waitFor(() => {
      expect(adoptGlobalContext).toHaveBeenCalledWith('opencode', { into: 'captured-notes' });
    });
  });

  it('disables Capture while the new fragment name is invalid', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(compiledClient));
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await screen.findByText('Review');

    fireEvent.click(screen.getByText('Review'));
    const picker = await screen.findByLabelText('Capture into');
    fireEvent.change(picker, { target: { value: '__new__' } });
    fireEvent.change(screen.getByLabelText('New capture fragment name'), {
      target: { value: 'Bad_Name' },
    });

    expect(screen.getByText(/Lowercase letters, digits, and hyphens only/)).toBeInTheDocument();
    expect(screen.getByText('Capture')).toBeDisabled();
    expect(adoptGlobalContext).not.toHaveBeenCalled();
  });

  it('deep-links straight into the review when exactly one fragment is drifted', async () => {
    const oneDrift = {
      ...identityClient,
      fragments: [{ name: '00-default', state: 'drifted' as const }],
    };
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(oneDrift));
    render(<GlobalContextDialog isOpen onClose={() => {}} initialDriftSlug="claude-code" />);

    expect(await screen.findByText('00-default in Claude Code was edited')).toBeInTheDocument();
    await waitFor(() => {
      expect(fetchGlobalContextDiff).toHaveBeenCalledWith('claude-code', '00-default');
    });
  });

  it('leaves the expanded fragment lines as the picker when several are drifted', async () => {
    const twoDrift = {
      ...identityClient,
      fragments: [
        { name: '00-default', state: 'drifted' as const },
        { name: '10-style', state: 'drifted' as const },
      ],
    };
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(twoDrift));
    render(<GlobalContextDialog isOpen onClose={() => {}} initialDriftSlug="claude-code" />);

    // The row arrives pre-expanded (spotlight), but no dialog opens.
    const list = await screen.findByLabelText('Claude Code fragments');
    expect(within(list).getAllByText('Review')).toHaveLength(2);
    expect(screen.queryByText(/was edited/)).not.toBeInTheDocument();
  });

  it('marks fragments with non-synced copies in the rail', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(identityClient));
    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await screen.findByText('10-style');

    // Both the drifted and the stale fragment carry a dot naming the client.
    expect(screen.getAllByLabelText('Out of sync in Claude Code')).toHaveLength(2);
  });
});
