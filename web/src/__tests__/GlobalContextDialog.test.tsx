import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { GlobalContextDialog } from '../components/context/GlobalContextDialog';
import { useContextStore } from '../stores/useContextStore';
import type { ContextDoc } from '../lib/api';

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
  };
});

import {
  fetchGlobalContext,
  initGlobalContext,
  scanGlobalContext,
  syncGlobalContext,
} from '../lib/api';

const emptyDoc: ContextDoc = {
  canonical: { path: '/home/u/.gridctl/context/AGENTS.md', exists: false, content: '' },
  needs_sync: false,
  clients: [],
};

const readyDoc: ContextDoc = {
  canonical: {
    path: '/home/u/.gridctl/context/AGENTS.md',
    exists: true,
    content: '# Rules\n',
  },
  needs_sync: true,
  clients: [
    {
      slug: 'claude-code',
      name: 'Claude Code',
      supported: true,
      available: true,
      strategy: 'dedicated-file',
      target_path: '/home/u/.claude/rules/gridctl.md',
      state: 'in-sync',
    },
    {
      slug: 'gemini',
      name: 'Gemini CLI',
      supported: true,
      available: true,
      strategy: 'import-shim',
      target_path: '/home/u/.gemini/GEMINI.md',
      state: 'never-synced',
    },
    {
      slug: 'opencode',
      name: 'OpenCode',
      supported: true,
      available: true,
      strategy: 'block',
      target_path: '/home/u/.config/opencode/AGENTS.md',
      state: 'drifted',
    },
    {
      slug: 'cursor',
      name: 'Cursor',
      supported: false,
      available: false,
      state: 'unsupported',
      detail: 'global User Rules are stored in app-internal storage; no supported file path',
    },
  ],
};

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  useContextStore.setState({ doc: null, loading: false, error: null });
});

describe('GlobalContextDialog', () => {
  it('shows the adoption-first setup view when no canonical file exists', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(emptyDoc);
    vi.mocked(scanGlobalContext).mockResolvedValue([
      { slug: 'claude-code', name: 'Claude Code', path: '/home/u/.claude/CLAUDE.md', exists: true, size: 42 },
      { slug: 'gemini', name: 'Gemini CLI', path: '/home/u/.gemini/GEMINI.md', exists: false, size: 0 },
    ]);

    render(<GlobalContextDialog isOpen onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('Set up your global context')).toBeInTheDocument();
    });
    // Existing file offered for import; non-existing one is not.
    expect(screen.getByText('Import from Claude Code')).toBeInTheDocument();
    expect(screen.queryByText('Import from Gemini CLI')).not.toBeInTheDocument();
    expect(screen.getByText('Start from the starter template')).toBeInTheDocument();
    // Nothing was written during the scan.
    expect(initGlobalContext).not.toHaveBeenCalled();
  });

  it('creates the canonical file from an imported client', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(emptyDoc);
    vi.mocked(scanGlobalContext).mockResolvedValue([
      { slug: 'claude-code', name: 'Claude Code', path: '/home/u/.claude/CLAUDE.md', exists: true, size: 42 },
    ]);
    vi.mocked(initGlobalContext).mockResolvedValue(readyDoc);

    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText('Import from Claude Code')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Import from Claude Code'));
    fireEvent.click(screen.getByText('Create canonical file'));

    await waitFor(() => {
      expect(initGlobalContext).toHaveBeenCalledWith({ source: 'client', client: 'claude-code' });
    });
    // The editor view replaces the setup view.
    await waitFor(() => {
      expect(screen.getByLabelText('Canonical global context')).toHaveValue('# Rules\n');
    });
  });

  it('renders per-client state chips and drift review action', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);

    render(<GlobalContextDialog isOpen onClose={() => {}} />);

    await waitFor(() => {
      expect(screen.getByText('Claude Code')).toBeInTheDocument();
    });
    expect(screen.getByText('in-sync')).toBeInTheDocument();
    expect(screen.getByText('never-synced')).toBeInTheDocument();
    expect(screen.getByText('drifted')).toBeInTheDocument();
    expect(screen.getByText('unsupported')).toBeInTheDocument();
    // Drifted client gets a Review action; unsupported client shows its reason.
    expect(screen.getByText('Review')).toBeInTheDocument();
    expect(
      screen.getByText(/app-internal storage/),
    ).toBeInTheDocument();
  });

  it('sync all calls the API and summarizes results', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(readyDoc);
    vi.mocked(syncGlobalContext).mockResolvedValue({
      dry_run: false,
      has_failures: false,
      results: [
        {
          slug: 'gemini',
          name: 'Gemini CLI',
          strategy: 'import-shim',
          target_path: '/home/u/.gemini/GEMINI.md',
          action: 'created',
        },
      ],
    });

    render(<GlobalContextDialog isOpen onClose={() => {}} />);
    await waitFor(() => {
      expect(screen.getByText('Sync all')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Sync all'));
    await waitFor(() => {
      expect(syncGlobalContext).toHaveBeenCalledWith();
    });
  });
});
