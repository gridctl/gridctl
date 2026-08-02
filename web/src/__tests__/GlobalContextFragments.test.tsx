import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
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
  deleteContextFragment,
  fetchContextFragments,
  fetchGlobalContext,
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
