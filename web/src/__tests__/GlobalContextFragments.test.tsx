import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { act, render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { ContextWorkspace } from '../components/context/ContextWorkspace';
import { useContextStore } from '../stores/useContextStore';
import type { ContextDoc, ContextFragment } from '../lib/api';
import type { RegistryKind } from '../lib/registryKind';

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
    fetchProposals: vi.fn().mockResolvedValue({ schema_version: 1, proposals: [], skipped: [], truncated: false }),
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
import * as toastMod from '../components/ui/Toast';

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

// The backend scaffolds a new fragment as a heading with its name.
const newFragment: ContextFragment = {
  name: '20-testing',
  content: '# 20-testing\n',
  bytes: 13,
  position: 3,
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (err: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function LocationProbe({ onNav }: { onNav?: (fn: (to: string) => void) => void }) {
  const location = useLocation();
  const navigate = useNavigate();
  onNav?.((to: string) => void navigate(to));
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function renderFragments(
  options: { url?: string; onKindChange?: (kind: RegistryKind) => void } = {},
) {
  const { url = '/library?kind=context', onKindChange = vi.fn() } = options;
  let navFn: (to: string) => void = () => {};
  const result = render(
    <CommandRegistryProvider>
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route path="/library" element={<ContextWorkspace onKindChange={onKindChange} />} />
          <Route path="*" element={<div data-testid="other-route" />} />
        </Routes>
        <LocationProbe onNav={(fn) => { navFn = fn; }} />
      </MemoryRouter>
    </CommandRegistryProvider>,
  );
  return { ...result, onKindChange, navigateTo: (to: string) => act(() => navFn(to)) };
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  useContextStore.setState({ doc: null, loading: false, error: null });
  vi.mocked(fetchGlobalContext).mockResolvedValue(fragmentsDoc);
  vi.mocked(fetchContextFragments).mockResolvedValue({ active: true, fragments });
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe('ContextWorkspace fragments mode', () => {
  it('routes to the fragments view with the rail in composition order', async () => {
    renderFragments();

    await waitFor(() => {
      expect(screen.getByText('00-default')).toBeInTheDocument();
    });
    expect(screen.getByText('10-style')).toBeInTheDocument();
    // The first fragment is selected and its content feeds the editor.
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Default rules\n');
    // Never the single-file setup view, even though canonical.exists=false.
    expect(screen.queryByText('Set up your global context')).not.toBeInTheDocument();
  });

  it('shows per-client mode chips in the inspector Clients tab', async () => {
    renderFragments();
    await waitFor(() => {
      expect(screen.getByText('00-default')).toBeInTheDocument();
    });
    const inspector = screen.getByRole('complementary', { name: 'Global context inspector' });
    expect(within(inspector).getByText('multi-file')).toBeInTheDocument();
    expect(within(inspector).getByText('compiled')).toBeInTheDocument();
  });

  it('reads ?fragment= on load, updates it on click, and toasts once on an unknown name', async () => {
    const spy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    renderFragments({ url: '/library?kind=context&fragment=10-style' });
    expect(await screen.findByLabelText('Fragment 10-style')).toHaveValue(fragments[1].content);

    fireEvent.click(screen.getByText('00-default'));
    expect(await screen.findByLabelText('Fragment 00-default')).toBeInTheDocument();
    expect(screen.getByTestId('location').textContent).toBe('/library?kind=context&fragment=00-default');
    cleanup();

    useContextStore.setState({ doc: null, loading: false, error: null });
    renderFragments({ url: '/library?kind=context&fragment=unknown-frag' });
    await screen.findByLabelText('Fragment 00-default');
    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/library?kind=context');
    });
    const notFoundCalls = spy.mock.calls.filter((c) => c[1] === 'Fragment "unknown-frag" not found');
    expect(notFoundCalls).toHaveLength(1);
    expect(notFoundCalls[0][0]).toBe('error');
  });

  it('snaps the URL back and preserves the draft when ?fragment= changes while dirty', async () => {
    const spy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    const { navigateTo } = renderFragments({ url: '/library?kind=context&fragment=00-default' });
    const editor = await screen.findByLabelText('Fragment 00-default');

    fireEvent.change(editor, { target: { value: '# Unsaved A\n' } });
    navigateTo('/library?kind=context&fragment=10-style');

    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/library?kind=context&fragment=00-default');
    });
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Unsaved A\n');
    expect(screen.queryByLabelText('Fragment 10-style')).not.toBeInTheDocument();
    expect(spy).toHaveBeenCalledWith(
      'warning',
      'Save or revert your changes before switching fragments',
    );
  });

  it('shows fragment metadata and Pins link in the inspector Details tab', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue({
      ...fragmentsDoc,
      pins: { drifted: ['10-style'], unpinned: [], missing: [] },
      clients: [
        {
          ...fragmentsDoc.clients[0],
          state: 'stale',
          fragments: [{ name: '10-style', state: 'stale' }],
        },
      ],
    });
    renderFragments({ url: '/library?kind=context&fragment=10-style&tab=details' });

    const inspector = await screen.findByRole('complementary', { name: 'Global context inspector' });
    const panel = within(inspector).getByRole('tabpanel', { name: 'Details' });
    await waitFor(() => {
      expect(panel).toHaveTextContent('10-style');
    });
    expect(panel).toHaveTextContent('/home/u/.gridctl/context/fragments/10-style.md');
    expect(panel).toHaveTextContent('style rules');
    expect(panel).toHaveTextContent('**/*.go');
    expect(panel).toHaveTextContent('42 bytes');
    expect(panel).toHaveTextContent('Claude Code');
    expect(panel).toHaveTextContent('Pin drift');
    expect(within(panel).getByRole('link', { name: 'Review in Pins' })).toHaveAttribute(
      'href',
      '/pins?kind=context&fragment=10-style&view=drift',
    );
  });

  it('shows a placeholder in Details when no fragment is selected', async () => {
    vi.mocked(fetchContextFragments).mockResolvedValue({ active: true, fragments: [] });
    renderFragments({ url: '/library?kind=context&tab=details' });

    const inspector = await screen.findByRole('complementary', { name: 'Global context inspector' });
    const panel = within(inspector).getByRole('tabpanel', { name: 'Details' });
    await waitFor(() => {
      expect(panel).toHaveTextContent('Select a fragment to see its details.');
    });
  });

  it('saves an edited fragment through the fragment endpoint', async () => {
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '00-default' });
    renderFragments();
    const editor = await screen.findByLabelText('Fragment 00-default');

    fireEvent.change(editor, { target: { value: '# Edited rules\n' } });
    fireEvent.click(screen.getByText('Save'));

    await waitFor(() => {
      expect(saveContextFragment).toHaveBeenCalledWith('00-default', '# Edited rules\n');
    });
  });

  it('renders a pin drift chip from GET /api/context pins', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue({
      ...fragmentsDoc,
      pins: { drifted: ['10-style'], missing: [], unpinned: ['00-default'] },
    });
    renderFragments();
    await screen.findByText('10-style');
    expect(screen.getByRole('button', { name: 'Pin drift' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Unpinned' })).toBeInTheDocument();
  });

  it('adds a Review in Pins action to the fragment save toast on pin drift', async () => {
    const spy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '00-default', pin_status: 'drift' });
    renderFragments();
    const editor = await screen.findByLabelText('Fragment 00-default');
    fireEvent.change(editor, { target: { value: '# Edited rules\n' } });
    fireEvent.click(screen.getByText('Save'));
    await waitFor(() => {
      expect(spy).toHaveBeenCalledWith(
        'success',
        'Fragment saved. Run a sync to propagate. Pin drift: review and approve in Pins.',
        expect.objectContaining({
          action: expect.objectContaining({ label: 'Review in Pins' }),
        }),
      );
    });
    const opts = spy.mock.calls.find((c) => c[0] === 'success')?.[2];
    act(() => {
      opts?.action?.onClick();
    });
    expect(screen.getByTestId('location').textContent).toBe('/pins?kind=context&fragment=00-default&view=drift');
  });

  it('switches fragments only when the draft is clean', async () => {
    renderFragments();
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

  it('creates a fragment, writes it to the URL, and raises no not-found toast', async () => {
    const spy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '20-testing' });
    renderFragments();
    await screen.findByText('00-default');

    fireEvent.click(screen.getByText('Add'));
    fireEvent.change(screen.getByLabelText('New fragment name'), {
      target: { value: '20-testing' },
    });
    vi.mocked(fetchContextFragments).mockResolvedValueOnce({
      active: true,
      fragments: [...fragments, newFragment],
    });
    fireEvent.click(screen.getByText('Create'));

    await waitFor(() => {
      expect(saveContextFragment).toHaveBeenCalledWith('20-testing', '');
    });
    expect(await screen.findByLabelText('Fragment 20-testing')).toHaveValue('# 20-testing\n');
    expect(screen.queryByLabelText('New fragment name')).not.toBeInTheDocument();
    expect(screen.getByTestId('location').textContent).toBe('/library?kind=context&fragment=20-testing');
    expect(spy.mock.calls.some((c) => String(c[1]).includes('not found'))).toBe(false);
  });

  it('refuses to add a fragment while the current one has unsaved edits', async () => {
    const spy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '00-default' });
    renderFragments();
    const editor = await screen.findByLabelText('Fragment 00-default');

    fireEvent.change(editor, { target: { value: '# Dirty\n' } });
    fireEvent.click(screen.getByText('Add'));
    fireEvent.change(screen.getByLabelText('New fragment name'), {
      target: { value: '20-testing' },
    });
    fireEvent.click(screen.getByText('Create'));

    await waitFor(() => {
      expect(spy).toHaveBeenCalledWith(
        'warning',
        'Save or revert your changes before adding a fragment',
      );
    });
    expect(saveContextFragment).not.toHaveBeenCalled();
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Dirty\n');
    expect(screen.getByLabelText('New fragment name')).toHaveValue('20-testing');

    fireEvent.click(screen.getByText('Save'));
    await waitFor(() => {
      expect(saveContextFragment).toHaveBeenCalledTimes(1);
    });
    expect(saveContextFragment).toHaveBeenCalledWith('00-default', '# Dirty\n');
  });

  it('never re-targets a draft when its fragment disappears on reload', async () => {
    const spy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    renderFragments();
    const editor = await screen.findByLabelText('Fragment 00-default');

    fireEvent.change(editor, { target: { value: '# Dirty\n' } });
    vi.mocked(fetchContextFragments).mockResolvedValueOnce({
      active: true,
      fragments: [fragments[1]],
    });
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));

    const other = await screen.findByLabelText('Fragment 10-style');
    expect(other).toHaveValue(fragments[1].content);
    expect(screen.queryByDisplayValue('# Dirty\n')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(saveContextFragment).not.toHaveBeenCalled();
    expect(spy).toHaveBeenCalledWith(
      'warning',
      expect.stringContaining('removed outside the editor'),
    );
  });

  it('shows the saved text as clean before the reload returns', async () => {
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '00-default' });
    renderFragments();
    const editor = await screen.findByLabelText('Fragment 00-default');

    const reload = deferred<{ active: boolean; fragments: ContextFragment[] }>();
    vi.mocked(fetchContextFragments).mockReturnValueOnce(reload.promise);
    fireEvent.change(editor, { target: { value: '# Saved\n' } });
    fireEvent.click(screen.getByText('Save'));

    await waitFor(() => {
      expect(saveContextFragment).toHaveBeenCalledWith('00-default', '# Saved\n');
    });
    await waitFor(() => {
      expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument();
    });
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Saved\n');
    expect(screen.getByRole('button', { name: 'Revert' })).toBeDisabled();

    reload.resolve({
      active: true,
      fragments: [{ ...fragments[0], content: '# Saved\n', bytes: 8 }, fragments[1]],
    });
    await waitFor(() => {
      expect(fetchContextFragments).toHaveBeenCalledTimes(2);
    });
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Saved\n');
    expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument();
  });

  it('keeps the saved text in the editor when the reload after a save fails', async () => {
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '00-default' });
    renderFragments();
    const editor = await screen.findByLabelText('Fragment 00-default');

    const reload = deferred<{ active: boolean; fragments: ContextFragment[] }>();
    vi.mocked(fetchContextFragments).mockReturnValueOnce(reload.promise);
    fireEvent.change(editor, { target: { value: '# Saved\n' } });
    fireEvent.click(screen.getByText('Save'));
    await waitFor(() => {
      expect(saveContextFragment).toHaveBeenCalled();
    });

    reload.reject(new Error('offline'));
    expect(await screen.findByRole('alert')).toHaveTextContent('Load fragments failed: offline.');
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Saved\n');
    expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument();
  });

  it('reverts a dirty fragment only after confirmation', async () => {
    const onKindChange = vi.fn();
    renderFragments({ onKindChange });
    const editor = await screen.findByLabelText('Fragment 00-default');

    fireEvent.change(editor, { target: { value: '# Dirty\n' } });
    fireEvent.click(screen.getByRole('button', { name: 'Revert' }));
    expect(screen.getByRole('alertdialog')).toHaveTextContent('00-default');
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Dirty\n');

    fireEvent.click(within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Dirty\n');

    // Escape closes only the Revert confirm.
    fireEvent.click(screen.getByRole('button', { name: 'Revert' }));
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Dirty\n');

    fireEvent.click(screen.getByRole('button', { name: 'Revert' }));
    fireEvent.click(
      within(screen.getByRole('alertdialog')).getByRole('button', { name: 'Revert "00-default"' }),
    );
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Default rules\n');
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();

    fireEvent.click(screen.getByText('10-style'));
    expect(await screen.findByLabelText('Fragment 10-style')).toBeInTheDocument();
    expect(onKindChange).not.toHaveBeenCalled();
  });

  it('disables Revert while the editor is clean', async () => {
    renderFragments();
    await screen.findByLabelText('Fragment 00-default');
    expect(screen.getByRole('button', { name: 'Revert' })).toBeDisabled();
  });

  it('names Revert in the switch-blocking toast', async () => {
    const spy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    renderFragments();
    const editor = await screen.findByLabelText('Fragment 00-default');

    fireEvent.change(editor, { target: { value: '# Dirty\n' } });
    fireEvent.click(screen.getByText('10-style'));
    expect(spy).toHaveBeenCalledWith(
      'warning',
      'Save or revert your changes before switching fragments',
    );
  });

  it('does not steal the selection when typing starts during a create', async () => {
    const spy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    const create = deferred<{ name: string }>();
    vi.mocked(saveContextFragment).mockReturnValueOnce(create.promise);
    renderFragments();
    const editor = await screen.findByLabelText('Fragment 00-default');

    fireEvent.click(screen.getByText('Add'));
    fireEvent.change(screen.getByLabelText('New fragment name'), {
      target: { value: '20-testing' },
    });
    fireEvent.click(screen.getByText('Create'));
    await waitFor(() => {
      expect(saveContextFragment).toHaveBeenCalledWith('20-testing', '');
    });

    fireEvent.change(editor, { target: { value: '# Dirty\n' } });
    vi.mocked(fetchContextFragments).mockResolvedValueOnce({
      active: true,
      fragments: [...fragments, newFragment],
    });
    create.resolve({ name: '20-testing' });

    expect(await screen.findByText('20-testing')).toBeInTheDocument();
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Dirty\n');
    expect(screen.queryByLabelText('Fragment 20-testing')).not.toBeInTheDocument();
    expect(spy).toHaveBeenCalledWith(
      'success',
      'Fragment "20-testing" created. Staying on "00-default" because it has unsaved changes.',
    );
  });

  it('does not steal the selection when typing starts during the reload after a create', async () => {
    const spy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '20-testing' });
    renderFragments();
    const editor = await screen.findByLabelText('Fragment 00-default');

    const reload = deferred<{ active: boolean; fragments: ContextFragment[] }>();
    vi.mocked(fetchContextFragments).mockReturnValueOnce(reload.promise);
    fireEvent.click(screen.getByText('Add'));
    fireEvent.change(screen.getByLabelText('New fragment name'), {
      target: { value: '20-testing' },
    });
    fireEvent.click(screen.getByText('Create'));
    await waitFor(() => {
      expect(fetchContextFragments).toHaveBeenCalledTimes(2);
    });

    fireEvent.change(editor, { target: { value: '# Dirty\n' } });
    reload.resolve({ active: true, fragments: [...fragments, newFragment] });

    expect(await screen.findByText('20-testing')).toBeInTheDocument();
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Dirty\n');
    expect(screen.queryByLabelText('Fragment 20-testing')).not.toBeInTheDocument();
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument();
    await waitFor(() => {
      expect(spy).toHaveBeenCalledWith(
        'success',
        'Fragment "20-testing" created. Staying on "00-default" because it has unsaved changes.',
      );
    });
  });

  it('keeps a fragment switched to and edited during the reload after a save', async () => {
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '00-default' });
    renderFragments();
    const editor = await screen.findByLabelText('Fragment 00-default');

    const reload = deferred<{ active: boolean; fragments: ContextFragment[] }>();
    vi.mocked(fetchContextFragments).mockReturnValueOnce(reload.promise);
    fireEvent.change(editor, { target: { value: '# Saved\n' } });
    fireEvent.click(screen.getByText('Save'));
    await waitFor(() => {
      expect(fetchContextFragments).toHaveBeenCalledTimes(2);
    });

    fireEvent.click(screen.getByText('10-style'));
    const other = await screen.findByLabelText('Fragment 10-style');
    fireEvent.change(other, { target: { value: 'Go style, edited.\n' } });
    reload.resolve({
      active: true,
      fragments: [{ ...fragments[0], content: '# Saved\n', bytes: 8 }, fragments[1]],
    });

    await waitFor(() => {
      expect(screen.getByText('Unsaved changes')).toBeInTheDocument();
    });
    expect(screen.getByLabelText('Fragment 10-style')).toHaveValue('Go style, edited.\n');
    expect(screen.queryByLabelText('Fragment 00-default')).not.toBeInTheDocument();
  });

  it('keeps text typed while a save is in flight as a draft of the same fragment', async () => {
    const save = deferred<{ name: string }>();
    vi.mocked(saveContextFragment)
      .mockReturnValueOnce(save.promise)
      .mockResolvedValue({ name: '00-default' });
    renderFragments();
    const editor = await screen.findByLabelText('Fragment 00-default');

    fireEvent.change(editor, { target: { value: '# First\n' } });
    fireEvent.click(screen.getByText('Save'));
    await waitFor(() => {
      expect(saveContextFragment).toHaveBeenCalledWith('00-default', '# First\n');
    });
    fireEvent.change(editor, { target: { value: '# Second\n' } });
    vi.mocked(fetchContextFragments).mockResolvedValueOnce({
      active: true,
      fragments: [{ ...fragments[0], content: '# First\n', bytes: 8 }, fragments[1]],
    });
    save.resolve({ name: '00-default' });

    await waitFor(() => {
      expect(fetchContextFragments).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Save' })).not.toBeDisabled();
    });
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Second\n');
    expect(screen.getByText('Unsaved changes')).toBeInTheDocument();

    fireEvent.click(screen.getByText('Save'));
    await waitFor(() => {
      expect(saveContextFragment).toHaveBeenCalledTimes(2);
    });
    expect(saveContextFragment).toHaveBeenLastCalledWith('00-default', '# Second\n');
  });

  it('does not report an outside removal when the dirty fragment is deleted here', async () => {
    const spy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    vi.mocked(deleteContextFragment).mockResolvedValue({ name: '00-default', backup: '/b' });
    renderFragments({ url: '/library?kind=context&fragment=00-default' });
    const editor = await screen.findByLabelText('Fragment 00-default');

    fireEvent.change(editor, { target: { value: '# Dirty\n' } });
    vi.mocked(fetchContextFragments).mockResolvedValueOnce({
      active: true,
      fragments: [fragments[1]],
    });
    fireEvent.click(screen.getByRole('button', { name: 'Remove fragment 00-default' }));
    fireEvent.click(screen.getByRole('button', { name: 'Remove "00-default"' }));

    await waitFor(() => {
      expect(deleteContextFragment).toHaveBeenCalledWith('00-default');
    });
    expect(await screen.findByLabelText('Fragment 10-style')).toHaveValue(fragments[1].content);
    await waitFor(() => {
      expect(fetchContextFragments).toHaveBeenCalledTimes(2);
    });
    expect(screen.getByTestId('location').textContent).toBe('/library?kind=context');
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled();
    const messages = spy.mock.calls.map((c) => String(c[1]));
    expect(messages.some((m) => m.includes('outside the editor'))).toBe(false);
    expect(messages.some((m) => m.includes('not found'))).toBe(false);
  });

  it('deletes the selected fragment and cleans the URL with no not-found toast', async () => {
    const spy = vi.spyOn(toastMod, 'showToast').mockImplementation(() => 'toast');
    vi.mocked(deleteContextFragment).mockResolvedValue({ name: '10-style', backup: '/b' });
    renderFragments({ url: '/library?kind=context&fragment=10-style' });
    await screen.findByLabelText('Fragment 10-style');

    vi.mocked(fetchContextFragments).mockResolvedValueOnce({
      active: true,
      fragments: [fragments[0]],
    });
    fireEvent.click(screen.getByRole('button', { name: 'Remove fragment 10-style' }));
    expect(deleteContextFragment).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/backup/i);

    fireEvent.click(screen.getByRole('button', { name: 'Remove "10-style"' }));
    await waitFor(() => {
      expect(deleteContextFragment).toHaveBeenCalledWith('10-style');
    });
    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/library?kind=context');
    });
    expect(spy.mock.calls.some((c) => String(c[1]).includes('not found'))).toBe(false);
  });

  it('confirms before discarding fragment edits on kind switch', async () => {
    const onKindChange = vi.fn();
    renderFragments({ onKindChange });
    const editor = await screen.findByLabelText('Fragment 00-default');

    fireEvent.change(editor, { target: { value: '# Edited rules\n' } });
    fireEvent.click(screen.getByRole('button', { name: 'Skills' }));

    expect(onKindChange).not.toHaveBeenCalled();
    expect(screen.getByRole('alertdialog')).toHaveTextContent(/unsaved edits/i);
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Edited rules\n');

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(onKindChange).not.toHaveBeenCalled();
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Edited rules\n');

    fireEvent.click(screen.getByRole('button', { name: 'Skills' }));
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }));
    expect(onKindChange).toHaveBeenCalledWith('skill');
  });

  it('offers the split-into-fragments affordance from the single-file editor', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(singleFileDoc);
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '10-style', migrated: true });
    renderFragments();
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
    { name: '00-default', state: 'drifted', pack: 'team-pack' },
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

  it('expands a multi-file row to every non-synced fragment with a navigating PackChip', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(identityClient));
    renderFragments();
    await screen.findByLabelText('Expand Claude Code fragments');

    fireEvent.click(screen.getByLabelText('Expand Claude Code fragments'));
    const list = screen.getByLabelText('Claude Code fragments');
    // The hidden-bucket case: the drifted fragment must not hide the stale one.
    expect(within(list).getByText('00-default')).toBeInTheDocument();
    expect(within(list).getByText('drifted')).toBeInTheDocument();
    expect(within(list).getByText('10-style')).toBeInTheDocument();
    expect(within(list).getByText('stale')).toBeInTheDocument();
    // Pack provenance renders a navigating PackChip.
    const packChip = within(list).getByRole('button', { name: /pack: team-pack/ });
    expect(within(list).getAllByRole('button', { name: /^pack:/ })).toHaveLength(1);
    fireEvent.click(packChip);
    expect(screen.getByTestId('location').textContent).toBe('/library?kind=pack&selected=team-pack');
  });

  it('adopts one fragment losslessly on the identity target', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(identityClient));
    renderFragments();
    await screen.findByLabelText('Expand Claude Code fragments');

    fireEvent.click(screen.getByLabelText('Expand Claude Code fragments'));
    const list = screen.getByLabelText('Claude Code fragments');
    fireEvent.click(within(list).getByText('Review'));

    // The diff request is fragment-scoped, not whole-client.
    await waitFor(() => {
      expect(fetchGlobalContextDiff).toHaveBeenCalledWith('claude-code', '00-default');
    });
    const adopt = await screen.findByText('Adopt 00-default');
    await waitFor(() => expect(adopt).toBeEnabled());
    fireEvent.click(adopt);
    await waitFor(() => {
      expect(adoptGlobalContext).toHaveBeenCalledWith('claude-code', { fragment: '00-default' });
    });
  });

  it('shows the lossy reason with no Adopt affordance on lossy renders', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(lossyClient));
    renderFragments();
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
    renderFragments();
    await screen.findByText('Review');

    fireEvent.click(screen.getByText('Review'));
    await screen.findByText(/receives one assembled document/);
    const capture = screen.getByText('Capture into 00-default');
    await waitFor(() => expect(capture).toBeEnabled());
    fireEvent.click(capture);

    await waitFor(() => {
      expect(adoptGlobalContext).toHaveBeenCalledWith('opencode', { into: '00-default' });
    });
  });

  it('captures a compiled edit into a new named fragment', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(compiledClient));
    renderFragments();
    await screen.findByText('Review');

    fireEvent.click(screen.getByText('Review'));
    const picker = await screen.findByLabelText('Capture into');
    fireEvent.change(picker, { target: { value: '__new__' } });
    fireEvent.change(screen.getByLabelText('New capture fragment name'), {
      target: { value: 'captured-notes' },
    });
    const capture = screen.getByText('Capture into captured-notes');
    await waitFor(() => expect(capture).toBeEnabled());
    fireEvent.click(capture);

    await waitFor(() => {
      expect(adoptGlobalContext).toHaveBeenCalledWith('opencode', { into: 'captured-notes' });
    });
  });

  it('disables Capture while the new fragment name is invalid', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(compiledClient));
    renderFragments();
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

  it('deep-links straight into the review when exactly one fragment is drifted and removes client param', async () => {
    const oneDrift = {
      ...identityClient,
      fragments: [{ name: '00-default', state: 'drifted' as const }],
    };
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(oneDrift));
    renderFragments({ url: '/library?kind=context&client=claude-code' });

    expect(await screen.findByText('00-default in Claude Code was edited')).toBeInTheDocument();
    await waitFor(() => {
      expect(fetchGlobalContextDiff).toHaveBeenCalledWith('claude-code', '00-default');
    });
    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/library?kind=context');
    });
  });

  it('leaves the expanded fragment lines as the picker when several are drifted and removes client param', async () => {
    const twoDrift = {
      ...identityClient,
      fragments: [
        { name: '00-default', state: 'drifted' as const },
        { name: '10-style', state: 'drifted' as const },
      ],
    };
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(twoDrift));
    renderFragments({ url: '/library?kind=context&client=claude-code' });

    // The row arrives pre-expanded (spotlight), but no dialog opens.
    const list = await screen.findByLabelText('Claude Code fragments');
    expect(within(list).getAllByText('Review')).toHaveLength(2);
    expect(screen.queryByText(/was edited/)).not.toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByTestId('location').textContent).toBe('/library?kind=context');
    });
  });

  it('marks fragments with non-synced copies in the rail', async () => {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(identityClient));
    renderFragments();
    await screen.findByText('10-style');

    // Both the drifted and the stale fragment carry a dot naming the client.
    expect(screen.getAllByLabelText('Out of sync in Claude Code')).toHaveLength(2);
  });
});

describe('fragment load failures', () => {
  it('shows a failure notice with Retry in the rail and disables Add until loaded', async () => {
    vi.mocked(fetchContextFragments).mockRejectedValueOnce(new Error('offline'));
    const toast = vi.spyOn(toastMod, 'showToast');
    renderFragments();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Load fragments failed: offline.');
    expect(screen.queryByText('No fragments yet')).not.toBeInTheDocument();
    expect(screen.queryByText('Add a fragment to start.')).not.toBeInTheDocument();
    expect(screen.getByText('Fragments unavailable')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add' })).toBeDisabled();
    expect(toast).not.toHaveBeenCalledWith('error', expect.anything(), expect.anything());

    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText('00-default')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Add' })).toBeEnabled();
  });

  it('keeps the list and the open editor when a later reload fails', async () => {
    vi.mocked(saveContextFragment).mockResolvedValue({ name: '00-default' });
    renderFragments();
    const editor = await screen.findByLabelText('Fragment 00-default');

    vi.mocked(fetchContextFragments).mockRejectedValueOnce(new Error('offline'));
    fireEvent.change(editor, { target: { value: '# Edited rules\n' } });
    fireEvent.click(screen.getByText('Save'));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Load fragments failed: offline.');
    expect(screen.getByText('00-default')).toBeInTheDocument();
    expect(screen.getByText('10-style')).toBeInTheDocument();
    expect(screen.getByLabelText('Fragment 00-default')).toHaveValue('# Edited rules\n');
    expect(screen.queryByText('Add a fragment to start.')).not.toBeInTheDocument();
  });

  it('drops a removed fragment from the rail when the reload fails', async () => {
    vi.mocked(deleteContextFragment).mockResolvedValue({ name: '10-style', backup: '/b' });
    renderFragments();
    await screen.findByText('10-style');

    vi.mocked(fetchContextFragments).mockRejectedValueOnce(new Error('offline'));
    fireEvent.click(screen.getByRole('button', { name: 'Remove fragment 10-style' }));
    fireEvent.click(screen.getByRole('button', { name: 'Remove "10-style"' }));

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Load fragments failed: offline.');
    expect(screen.queryByText('10-style')).not.toBeInTheDocument();
    expect(screen.getByText('00-default')).toBeInTheDocument();
  });
});

const GATE_TITLE = 'Review the diff before choosing';

describe('fragment drift review evidence gate', () => {
  async function openFragmentReview(client: ContextDoc['clients'][number]) {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(client));
    renderFragments();
    const expand = await screen.findByLabelText(`Expand ${client.name} fragments`);
    fireEvent.click(expand);
    const list = screen.getByLabelText(`${client.name} fragments`);
    fireEvent.click(within(list).getAllByText('Review')[0]);
    await screen.findByText(/was edited/);
  }

  async function openCompiledReview() {
    vi.mocked(fetchGlobalContext).mockResolvedValue(clientDoc(compiledClient));
    renderFragments();
    fireEvent.click(await screen.findByText('Review'));
    await screen.findByText(/receives one assembled document/);
  }

  it('gates Adopt and Force overwrite on the identity target until the diff loads', async () => {
    const d = deferred<string>();
    vi.mocked(fetchGlobalContextDiff).mockReturnValue(d.promise);
    await openFragmentReview(identityClient);

    expect(screen.getByText('Loading diff…')).toBeInTheDocument();
    const adopt = screen.getByRole('button', { name: 'Adopt 00-default' });
    const overwrite = screen.getByRole('button', { name: 'Force overwrite' });
    expect(adopt).toBeDisabled();
    expect(adopt).toHaveAttribute('title', GATE_TITLE);
    expect(overwrite).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeEnabled();

    await act(async () => d.resolve('@@ -1 +1 @@\n-a\n+b\n'));
    await waitFor(() => expect(adopt).toBeEnabled());
    expect(overwrite).toBeEnabled();
    expect(screen.getByLabelText('Diff for 00-default in Claude Code')).toBeInTheDocument();
  });

  it('gates Force overwrite on a lossy target until the diff loads', async () => {
    const d = deferred<string>();
    vi.mocked(fetchGlobalContextDiff).mockReturnValue(d.promise);
    await openFragmentReview(lossyClient);

    const overwrite = screen.getByRole('button', { name: 'Force overwrite' });
    expect(overwrite).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeEnabled();
    await act(async () => d.resolve('@@ -1 +1 @@\n-a\n+b\n'));
    await waitFor(() => expect(overwrite).toBeEnabled());
  });

  it('gates Capture and its overwrite on a compiled target without trapping Cancel', async () => {
    const d = deferred<string>();
    vi.mocked(fetchGlobalContextDiff).mockReturnValue(d.promise);
    await openCompiledReview();

    const capture = screen.getByRole('button', { name: 'Capture into 00-default' });
    const overwrite = screen.getByRole('button', { name: 'Force overwrite' });
    expect(capture).toBeDisabled();
    expect(capture).toHaveAttribute('title', GATE_TITLE);
    expect(overwrite).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeEnabled();

    await act(async () => d.resolve('@@ -1 +1 @@\n-a\n+b\n'));
    await waitFor(() => expect(capture).toBeEnabled());
    expect(overwrite).toBeEnabled();
  });

  it('shows an identity failure as an alert naming the fragment-scoped adopt, and Retry recovers', async () => {
    vi.mocked(fetchGlobalContextDiff)
      .mockRejectedValueOnce(new Error('rules dir unreadable'))
      .mockResolvedValueOnce('@@ -1 +1 @@\n-a\n+b\n');
    await openFragmentReview(identityClient);

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('rules dir unreadable');
    expect(alert).toHaveTextContent('gridctl ctx diff claude-code 00-default');
    expect(alert).toHaveTextContent('gridctl ctx adopt claude-code 00-default');
    expect(alert).toHaveTextContent('gridctl ctx sync claude-code --force');
    expect(screen.queryByLabelText('Diff for 00-default in Claude Code')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Adopt 00-default' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Force overwrite' })).toBeDisabled();

    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(screen.getByRole('button', { name: 'Adopt 00-default' })).toBeEnabled());
    expect(fetchGlobalContextDiff).toHaveBeenCalledTimes(2);
    expect(fetchGlobalContextDiff).toHaveBeenLastCalledWith('claude-code', '00-default');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('names no adopt command when a lossy render fails to load', async () => {
    vi.mocked(fetchGlobalContextDiff).mockRejectedValue(new Error('denied'));
    await openFragmentReview(lossyClient);

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('gridctl ctx diff vscode 00-default');
    expect(alert).toHaveTextContent('gridctl ctx sync vscode --force');
    expect(alert).not.toHaveTextContent(/ctx adopt/);
    expect(screen.getByRole('button', { name: 'Force overwrite' })).toBeDisabled();
  });

  it('names the --into capture when a compiled diff fails to load', async () => {
    vi.mocked(fetchGlobalContextDiff).mockRejectedValue(new Error('denied'));
    await openCompiledReview();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('gridctl ctx diff opencode');
    expect(alert).toHaveTextContent('gridctl ctx adopt opencode --into <fragment>');
    expect(alert).toHaveTextContent('gridctl ctx sync opencode --force');
    expect(screen.getByRole('button', { name: 'Capture into 00-default' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeEnabled();
  });

  it('returns to loading and gates the actions when another fragment is selected', async () => {
    const twoDrift = {
      ...identityClient,
      fragments: [
        { name: '00-default', state: 'drifted' as const },
        { name: '10-style', state: 'drifted' as const },
      ],
    };
    const second = deferred<string>();
    vi.mocked(fetchGlobalContextDiff).mockImplementation((_slug, fragment) =>
      fragment === '10-style' ? second.promise : Promise.resolve('@@ -1 +1 @@\n-a\n+b\n'),
    );
    await openFragmentReview(twoDrift);
    await waitFor(() => expect(screen.getByRole('button', { name: 'Adopt 00-default' })).toBeEnabled());

    const picker = screen.getByLabelText('Drifted fragments');
    fireEvent.click(within(picker).getByText('10-style'));
    const adopt = screen.getByRole('button', { name: 'Adopt 10-style' });
    expect(adopt).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Force overwrite' })).toBeDisabled();
    expect(screen.getByText('Loading diff…')).toBeInTheDocument();

    await act(async () => second.resolve('@@ -1 +1 @@\n-c\n+d\n'));
    await waitFor(() => expect(adopt).toBeEnabled());
    expect(fetchGlobalContextDiff).toHaveBeenLastCalledWith('claude-code', '10-style');
  });
});
