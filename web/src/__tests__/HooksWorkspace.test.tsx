import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { HooksWorkspace } from '../components/registry/hooks/HooksWorkspace';
import { usePinsStore } from '../stores/usePinsStore';
import type { PinFinding } from '../lib/api';
import type { HookPinDiff, HookProjectionStatus, RegistryHook, RegistryHookList } from '../types';

vi.mock('../components/ui/Toast', async () => {
  const actual = await vi.importActual<typeof import('../components/ui/Toast')>('../components/ui/Toast');
  return { ...actual, showToast: vi.fn() };
});

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchRegistryHooks: vi.fn(),
    fetchRegistryHook: vi.fn(),
    fetchHookProjectionStatus: vi.fn(),
    syncHookProjections: vi.fn(),
    unsyncHookProjections: vi.fn(),
    fetchHookPinDiff: vi.fn(),
    approveHookPin: vi.fn(),
    fetchHookPins: vi.fn(),
  };
});

import { showToast } from '../components/ui/Toast';
import {
  approveHookPin,
  fetchHookPinDiff,
  fetchHookPins,
  fetchHookProjectionStatus,
  fetchRegistryHook,
  fetchRegistryHooks,
  HTTPError,
  syncHookProjections,
  unsyncHookProjections,
} from '../lib/api';

const block: RegistryHook = {
  name: 'block-force-push',
  description: 'Deny git push --force before it runs',
  dialect: 'claude-code',
  events: ['PreToolUse'],
  scripts: 1,
  source: 'network-eng',
  pin_status: 'drift',
};

const detail: RegistryHook = {
  ...block,
  document: '{"name":"block-force-push"}',
  hooks: {
    PreToolUse: [
      {
        matcher: 'Bash',
        hooks: [{ type: 'command', command: './scripts/block-force-push.sh', if: 'Bash(git push *)' }],
      },
    ],
  },
};

const list: RegistryHookList = {
  hooks: [block],
  unsupported_clients: [
    { slug: 'gemini', name: 'Gemini CLI', location: '~/.gemini/settings.json hooks key', detected: false },
  ],
};

const inSync: HookProjectionStatus = {
  hook: 'block-force-push',
  client: 'claude-code',
  channel: 'merge-key',
  render: 'identity',
  state: 'in-sync',
  target: '/tmp/demo/.claude/settings.json',
  script_dir: '/tmp/demo/.gridctl/hooks/block-force-push',
};

const diff: HookPinDiff = {
  hook: 'block-force-push',
  status: 'drift',
  composite_hash: 'abc123',
  old_document: '{"name":"block-force-push"}',
  new_document: '{"name":"block-force-push","description":"edited"}',
  added_files: [],
  removed_files: [],
  modified_files: ['hook.json'],
  findings: [{
    code: 'hook-scan',
    severity: 'warn',
    confidence: 'high',
    field: 'scripts/block-force-push.sh:line-2',
    message: 'world-writable permissions',
    snippet: 'chmod\\s+777',
  }],
};

function LocationProbe() {
  const location = useLocation();
  return <div data-testid="location">{location.pathname + location.search}</div>;
}

function renderWorkspace() {
  return render(
    <MemoryRouter initialEntries={['/library?kind=hook']}>
      <CommandRegistryProvider>
        <HooksWorkspace onKindChange={() => {}} />
        <Routes>
          <Route path="*" element={<LocationProbe />} />
        </Routes>
      </CommandRegistryProvider>
    </MemoryRouter>,
  );
}

const pinnedDiff: HookPinDiff = {
  ...diff,
  status: 'pinned',
  new_document: diff.old_document,
  modified_files: [],
  findings: [],
};

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.mocked(fetchRegistryHooks).mockResolvedValue(list);
  vi.mocked(fetchRegistryHook).mockResolvedValue(detail);
  vi.mocked(fetchHookProjectionStatus).mockImplementation(async () => {
    return vi.mocked(syncHookProjections).mock.calls.length > 0 ? [inSync] : [];
  });
  vi.mocked(fetchHookPinDiff).mockResolvedValue(diff);
  vi.mocked(syncHookProjections).mockResolvedValue([
    { hook: 'block-force-push', client: 'claude-code', action: 'copied' },
  ]);
  vi.mocked(approveHookPin).mockResolvedValue(undefined);
  vi.mocked(fetchHookPins).mockResolvedValue({});
  usePinsStore.setState({ hookPins: null, hookPinsError: null });
});

describe('HooksWorkspace', () => {
  it('renders the example hook, a projection chip after sync, and a required approve reason', async () => {
    renderWorkspace();

    expect(await screen.findByText('block-force-push')).toBeInTheDocument();
    expect(screen.getByText('network-eng')).toBeInTheDocument();
    expect(screen.getByText('Not projected')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Inspect block-force-push' }));
    fireEvent.click(await screen.findByRole('tab', { name: 'Projection' }));
    fireEvent.click(screen.getByRole('button', { name: 'Sync' }));

    await waitFor(() => {
      expect(syncHookProjections).toHaveBeenCalledWith({ hooks: ['block-force-push'] });
      expect(vi.mocked(fetchHookProjectionStatus).mock.calls.length).toBeGreaterThan(1);
    });

    expect(await screen.findAllByText('Claude Code')).not.toHaveLength(0);
    expect(screen.getAllByText('in-sync').length).toBeGreaterThan(0);
    expect(screen.getByText('/tmp/demo/.gridctl/hooks/block-force-push')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('tab', { name: 'Pin' }));
    expect(await screen.findByText(/world-writable permissions/)).toBeInTheDocument();
    expect(screen.getByLabelText('Modified files')).toHaveTextContent('hook.json');
    expect(screen.getByLabelText('Hook pin diff')).toHaveTextContent('edited');
    const reason = screen.getByLabelText('Approve reason');
    expect(reason).toBeRequired();
    const approve = screen.getByRole('button', { name: 'Approve' });
    expect(approve).toBeDisabled();

    fireEvent.change(reason, { target: { value: 'reviewed the script' } });
    expect(approve).toBeEnabled();
    fireEvent.click(approve);
    await waitFor(() => {
      expect(approveHookPin).toHaveBeenCalledWith('block-force-push', 'abc123', 'reviewed the script');
    });
  });

  async function openPinTab() {
    fireEvent.click(await screen.findByRole('button', { name: 'Inspect block-force-push' }));
    fireEvent.click(await screen.findByRole('tab', { name: 'Pin' }));
    return screen.findByLabelText('Approve reason');
  }

  it('reloads the review and the store after approve without reselecting', async () => {
    vi.mocked(fetchHookPinDiff).mockResolvedValueOnce(diff).mockResolvedValue(pinnedDiff);
    const refreshed = {
      'block-force-push': {
        status: 'pinned' as const,
        definition_hash: 'h',
        pinned_at: '2026-01-01T00:00:00Z',
        last_verified_at: '2026-01-01T00:00:00Z',
      },
    };
    vi.mocked(fetchHookPins).mockResolvedValue(refreshed);
    renderWorkspace();
    fireEvent.change(await openPinTab(), { target: { value: 'reviewed' } });
    expect(screen.getByText('drift')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));

    expect(await screen.findByText('pinned')).toBeInTheDocument();
    expect(screen.getByText('hook.json unchanged')).toBeInTheDocument();
    expect(fetchHookPinDiff).toHaveBeenCalledTimes(2);
    expect(approveHookPin).toHaveBeenCalledWith('block-force-push', 'abc123', 'reviewed');
    expect(vi.mocked(approveHookPin).mock.calls[0]).toHaveLength(3);
    expect(vi.mocked(fetchHookPins).mock.invocationCallOrder[0]).toBeGreaterThan(
      vi.mocked(approveHookPin).mock.invocationCallOrder[0],
    );
    expect(usePinsStore.getState().hookPins).toEqual(refreshed);
    expect(screen.queryByLabelText('Evidence references, one per line')).not.toBeInTheDocument();
    expect(document.getElementById('pins-approve-button')).toBeNull();
    expect(showToast).toHaveBeenCalledWith('success', 'Hook pin approved');
  });

  it('keeps the approve a success when the store refresh fails', async () => {
    vi.mocked(fetchHookPins).mockRejectedValue(new Error('list down'));
    renderWorkspace();
    fireEvent.change(await openPinTab(), { target: { value: 'reviewed' } });
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    await waitFor(() => expect(fetchHookPins).toHaveBeenCalled());
    expect(showToast).toHaveBeenCalledWith('success', 'Hook pin approved');
    expect(showToast).not.toHaveBeenCalledWith('error', expect.anything(), expect.anything());
  });

  it('reloads the diff after a 409 conflict', async () => {
    vi.mocked(approveHookPin).mockRejectedValue(new HTTPError(409, 'hook content changed since review'));
    renderWorkspace();
    fireEvent.change(await openPinTab(), { target: { value: 'reviewed' } });
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    await waitFor(() => expect(fetchHookPinDiff).toHaveBeenCalledTimes(2));
    expect(vi.mocked(showToast).mock.calls.some(([kind, msg]) => kind === 'error' && /Reloading the diff/.test(String(msg)))).toBe(true);
  });

  it('shows the refusal text on a 400 and keeps the reason without reloading', async () => {
    vi.mocked(approveHookPin).mockRejectedValue(new HTTPError(400, 'reason contains a control character'));
    renderWorkspace();
    const reason = await openPinTab();
    fireEvent.change(reason, { target: { value: 'reviewed' } });
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    await waitFor(() =>
      expect(showToast).toHaveBeenCalledWith('error', 'reason contains a control character', {
        detail: 'reason contains a control character',
      }),
    );
    expect(fetchHookPinDiff).toHaveBeenCalledTimes(1);
    expect(reason).toHaveValue('reviewed');
  });

  it('shows a Pin drift chip that opens the Pins review without selecting the card', async () => {
    renderWorkspace();
    const chip = await screen.findByRole('button', { name: 'Review pin drift for block-force-push' });
    expect(chip).toHaveTextContent('Pin drift');
    expect(screen.getAllByRole('button', { name: 'Inspect block-force-push' })).toHaveLength(1);
    fireEvent.click(chip);
    expect(screen.getByTestId('location')).toHaveTextContent(
      '/pins?kind=hook&hook=block-force-push&view=drift',
    );
    expect(fetchRegistryHook).not.toHaveBeenCalled();
  });

  it('prefers the store pin status over the list row for the chip', async () => {
    usePinsStore.setState({
      hookPins: {
        'block-force-push': {
          status: 'pinned',
          definition_hash: 'h',
          pinned_at: '',
          last_verified_at: '',
        },
      },
    });
    renderWorkspace();
    expect(await screen.findByRole('button', { name: 'Inspect block-force-push' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Review pin drift/ })).not.toBeInTheDocument();
  });

  it('shows no chip for a pinned hook', async () => {
    vi.mocked(fetchRegistryHooks).mockResolvedValue({ ...list, hooks: [{ ...block, pin_status: 'pinned' }] });
    renderWorkspace();
    expect(await screen.findByRole('button', { name: 'Inspect block-force-push' })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Review pin drift/ })).not.toBeInTheDocument();
  });

  it('shows a target chip per projection and covered clients via their target', async () => {
    vi.mocked(fetchRegistryHooks).mockResolvedValue({
      ...list,
      unsupported_clients: [
        ...list.unsupported_clients,
        {
          slug: 'cursor',
          name: 'Cursor',
          location: 'reads ~/.claude/settings.json hooks',
          covered_by: 'claude-code',
          detected: true,
        },
      ],
    });
    vi.mocked(fetchHookProjectionStatus).mockResolvedValue([
      inSync,
      {
        ...inSync,
        client: 'opencode',
        channel: 'dedicated-file',
        render: 'lossy',
        target: '/tmp/demo/.config/opencode/plugins/gridctl-hook-block-force-push.ts',
        script_dir: '/tmp/demo/.gridctl/hooks/block-force-push@opencode',
        detail: 'dropped if "Bash(git push *)": the handler runs on every matched call',
      },
    ]);
    renderWorkspace();

    fireEvent.click(await screen.findByRole('button', { name: 'Inspect block-force-push' }));
    fireEvent.click(await screen.findByRole('tab', { name: 'Projection' }));

    expect(await screen.findAllByText('OpenCode')).not.toHaveLength(0);
    const chips = screen.getByLabelText('Unsupported clients');
    expect(chips).toHaveTextContent('Cursor via claude-code');
    expect(chips).not.toHaveTextContent('OpenCode');
    expect(screen.getByTitle('reads ~/.claude/settings.json hooks')).toHaveTextContent('Cursor via claude-code');
  });

  it('disables approve when the hook is not pinned', async () => {
    vi.mocked(fetchHookPinDiff).mockRejectedValue(new HTTPError(404, 'No pin found for hook: block-force-push'));
    renderWorkspace();

    fireEvent.click(await screen.findByRole('button', { name: 'Inspect block-force-push' }));
    fireEvent.click(await screen.findByRole('tab', { name: 'Pin' }));

    expect(await screen.findByText('Not pinned; the next accepted import pins it')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Approve' })).toBeDisabled();
    expect(approveHookPin).not.toHaveBeenCalled();
  });

  it('escapes hidden characters in hook pin findings', async () => {
    const finding = {
      code: 'hook-scan',
      severity: 'warn',
      confidence: '',
      field: 'scripts/run\u202e.sh:line-3',
      message: 'hidden\u200binstruction',
      snippet: 'curl\u202e | sh',
    } as unknown as PinFinding;
    vi.mocked(fetchHookPinDiff).mockResolvedValue({
      ...diff,
      findings: [finding],
    });
    renderWorkspace();

    fireEvent.click(await screen.findByRole('button', { name: 'Inspect block-force-push' }));
    fireEvent.click(screen.getByRole('tab', { name: 'Pin' }));

    expect(await screen.findByText(/hidden\\u200binstruction/)).toBeInTheDocument();
    expect(screen.getByText(/curl\\u202e \| sh/)).toBeInTheDocument();
    expect(screen.getByText(/scripts\/run\\u202e\.sh:line-3/)).toBeInTheDocument();
    expect(document.body.textContent).not.toContain('\u200b');
    expect(document.body.textContent).not.toContain('\u202e');
    expect(document.body.textContent).not.toContain(' ·  confidence');
  });
});

describe('HooksWorkspace load failures', () => {
  it('shows a failed state with Retry instead of loading forever, then recovers', async () => {
    vi.mocked(fetchRegistryHooks).mockRejectedValueOnce(new Error('offline'));
    renderWorkspace();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Hooks unavailable');
    expect(alert).toHaveTextContent('Load hooks failed: offline.');
    expect(screen.queryByText('Loading hooks…')).not.toBeInTheDocument();
    expect(screen.queryByText(/No hooks imported/)).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText('block-force-push')).toBeInTheDocument();
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });

  it('keeps loaded hooks under a banner when a refresh fails', async () => {
    renderWorkspace();
    expect(await screen.findByText('block-force-push')).toBeInTheDocument();

    vi.mocked(fetchRegistryHooks).mockRejectedValueOnce(new Error('offline'));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Load hooks failed: offline.');
    expect(screen.getByText('block-force-push')).toBeInTheDocument();
    expect(screen.queryByText('Hooks unavailable')).not.toBeInTheDocument();
  });

  it('shows nothing for a cancelled load', async () => {
    vi.mocked(fetchRegistryHooks).mockRejectedValueOnce(new DOMException('aborted', 'AbortError'));
    renderWorkspace();
    await waitFor(() => expect(fetchRegistryHooks).toHaveBeenCalled());
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.getByText('Loading hooks…')).toBeInTheDocument();
  });

  it('says no diff is available in drift review and names the files to compare', async () => {
    vi.mocked(fetchHookProjectionStatus).mockResolvedValue([{ ...inSync, state: 'drifted' }]);
    renderWorkspace();

    fireEvent.click(await screen.findByRole('button', { name: 'Inspect block-force-push' }));
    fireEvent.click(await screen.findByRole('tab', { name: 'Projection' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Review drift' }));

    const review = screen.getByRole('dialog', { name: 'Drift review' });
    const notice = screen.getByTestId('drift-no-diff');
    expect(review).toContainElement(notice);
    expect(notice).toHaveTextContent('cannot show this diff in the UI yet');
    expect(notice).toHaveTextContent('/tmp/demo/.claude/settings.json');
    expect(notice).toHaveTextContent('/tmp/demo/.gridctl/hooks/block-force-push');
    expect(screen.getByRole('button', { name: 'Force sync' })).toBeEnabled();
    expect(screen.getByRole('button', { name: 'Close' })).toBeEnabled();
  });
});

const opencodeRow: HookProjectionStatus = {
  ...inSync,
  client: 'opencode',
  channel: 'dedicated-file',
  target: '/tmp/demo/.config/opencode/plugins/gridctl-hook-block-force-push.ts',
  script_dir: '/tmp/demo/.gridctl/hooks/block-force-push@opencode',
};

const coveredClients: RegistryHookList['unsupported_clients'] = [
  { slug: 'cursor', name: 'Cursor', location: 'reads ~/.claude/settings.json hooks', covered_by: 'claude-code', detected: true },
  {
    slug: 'windsurf',
    name: 'Windsurf (Devin Local)',
    location: 'reads ~/.claude/settings.json hooks',
    covered_by: 'claude-code',
    detected: false,
  },
];

describe('HooksWorkspace unsync', () => {
  async function openProjection(rows: HookProjectionStatus[]) {
    vi.mocked(fetchHookProjectionStatus).mockResolvedValue(rows);
    renderWorkspace();
    fireEvent.click(await screen.findByRole('button', { name: 'Inspect block-force-push' }));
    fireEvent.click(await screen.findByRole('tab', { name: 'Projection' }));
    await screen.findAllByText(rows.length > 0 ? 'Claude Code' : 'Not projected yet.');
    return screen.getByRole('button', { name: 'Unsync' });
  }

  it('confirms before unsyncing and names each projected client', async () => {
    fireEvent.click(await openProjection([inSync, opencodeRow]));
    const dialog = screen.getByRole('alertdialog', { name: 'Unsync block-force-push?' });
    expect(unsyncHookProjections).not.toHaveBeenCalled();
    expect(dialog).toHaveTextContent(
      "Removes this hook's entries and script copies from Claude Code and OpenCode. Those clients stop running it.",
    );
    expect(dialog).toHaveTextContent('The registry copy and its pin stay. Sync restores the projection.');
    await waitFor(() => expect(within(dialog).getByRole('button', { name: 'Cancel' })).toHaveFocus());
    expect(within(dialog).getByRole('button', { name: 'Unsync from 2 clients' })).toBeEnabled();
  });

  it('closes on Cancel and Escape without calling the API', async () => {
    const unsync = await openProjection([inSync]);
    fireEvent.click(unsync);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();

    fireEvent.click(unsync);
    expect(screen.getByRole('alertdialog')).toBeInTheDocument();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(unsyncHookProjections).not.toHaveBeenCalled();
  });

  it('unsyncs once on confirm, refreshes, and toasts an unsync result', async () => {
    vi.mocked(unsyncHookProjections).mockResolvedValue([
      { hook: 'block-force-push', client: 'claude-code', target: inSync.target, action: 'removed' },
      { hook: 'block-force-push', client: 'opencode', target: opencodeRow.target, action: 'removed' },
    ]);
    fireEvent.click(await openProjection([inSync, opencodeRow]));
    const before = vi.mocked(fetchHookProjectionStatus).mock.calls.length;
    fireEvent.click(screen.getByRole('button', { name: 'Unsync from 2 clients' }));

    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Unsynced block-force-push from 2 clients'));
    expect(unsyncHookProjections).toHaveBeenCalledTimes(1);
    expect(unsyncHookProjections).toHaveBeenCalledWith({ hooks: ['block-force-push'] });
    await waitFor(() => expect(vi.mocked(fetchHookProjectionStatus).mock.calls.length).toBeGreaterThan(before));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    const messages = vi.mocked(showToast).mock.calls.map(([, msg]) => String(msg));
    expect(messages).not.toContain('Hook sync finished');
    expect(messages).not.toContain('Nothing to sync');
  });

  it('marks the confirm busy while the request is in flight', async () => {
    let settle: (rows: never[]) => void = () => {};
    vi.mocked(unsyncHookProjections).mockReturnValue(new Promise((resolve) => { settle = resolve; }));
    fireEvent.click(await openProjection([inSync]));
    const confirm = screen.getByRole('button', { name: 'Unsync from 1 client' });
    fireEvent.click(confirm);
    await waitFor(() => expect(confirm).toBeDisabled());
    settle([]);
    await waitFor(() => expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument());
    expect(showToast).toHaveBeenCalledWith('success', 'Nothing to unsync');
  });

  it('closes the dialog and reports a thrown unsync as a projection error', async () => {
    vi.mocked(unsyncHookProjections).mockRejectedValue(new Error('offline'));
    fireEvent.click(await openProjection([inSync]));
    fireEvent.click(screen.getByRole('button', { name: 'Unsync from 1 client' }));
    await waitFor(() =>
      expect(showToast).toHaveBeenCalledWith('error', expect.stringContaining('Hook projection'), expect.anything()),
    );
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Unsync' })).toBeEnabled();
  });

  it('names detected covered clients only', async () => {
    vi.mocked(fetchRegistryHooks).mockResolvedValue({ ...list, unsupported_clients: coveredClients });
    fireEvent.click(await openProjection([inSync, opencodeRow]));
    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent('Cursor reads the Claude Code projection and stops running it too.');
    expect(dialog).not.toHaveTextContent('Windsurf');
    expect(within(dialog).getByRole('button', { name: 'Unsync from 2 clients' })).toBeInTheDocument();
  });

  it('names no covered client without a removable Claude Code projection', async () => {
    vi.mocked(fetchRegistryHooks).mockResolvedValue({ ...list, unsupported_clients: coveredClients });
    fireEvent.click(await openProjection([{ ...inSync, state: 'drifted' }, opencodeRow]));
    expect(screen.getByRole('alertdialog')).not.toHaveTextContent('Cursor');
  });

  it('names no covered client when only another client is projected', async () => {
    vi.mocked(fetchRegistryHooks).mockResolvedValue({ ...list, unsupported_clients: coveredClients });
    vi.mocked(fetchHookProjectionStatus).mockResolvedValue([opencodeRow]);
    renderWorkspace();
    fireEvent.click(await screen.findByRole('button', { name: 'Inspect block-force-push' }));
    fireEvent.click(await screen.findByRole('tab', { name: 'Projection' }));
    await screen.findAllByText('OpenCode');
    fireEvent.click(screen.getByRole('button', { name: 'Unsync' }));
    expect(screen.getByRole('alertdialog')).not.toHaveTextContent('Cursor');
  });

  it('keeps a drifted client out of the removal and says it is kept', async () => {
    fireEvent.click(await openProjection([inSync, { ...opencodeRow, state: 'drifted' }]));
    const dialog = screen.getByRole('alertdialog');
    expect(dialog).toHaveTextContent(
      "Removes this hook's entries and script copies from Claude Code. That client stops running it.",
    );
    expect(dialog).toHaveTextContent(
      'OpenCode was edited after gridctl wrote it. Its projection is kept until the drift is reviewed.',
    );
    expect(within(dialog).getByRole('button', { name: 'Unsync from 1 client' })).toBeInTheDocument();
  });

  it('disables Unsync with no projections or with every projection drifted', async () => {
    const empty = await openProjection([]);
    expect(empty).toBeDisabled();
    expect(empty).toHaveAttribute('title', 'Not projected to any client');
    expect(screen.getByRole('button', { name: 'Sync' })).toBeEnabled();
    cleanup();

    const drifted = await openProjection([{ ...inSync, state: 'drifted' }, { ...opencodeRow, state: 'drifted' }]);
    expect(drifted).toBeDisabled();
    expect(drifted).toHaveAttribute('title', 'Every projection has drifted. Review the drift first.');
    expect(screen.getByRole('button', { name: 'Sync' })).toBeEnabled();
    expect(screen.getAllByRole('button', { name: 'Review drift' })).toHaveLength(2);
  });

  it('keeps sync toasts on the sync describer', async () => {
    renderWorkspace();
    fireEvent.click(await screen.findByRole('button', { name: 'Inspect block-force-push' }));
    fireEvent.click(await screen.findByRole('tab', { name: 'Projection' }));
    fireEvent.click(screen.getByRole('button', { name: 'Sync' }));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', 'Hook sync finished'));
  });
});

describe('HooksWorkspace pin review readability', () => {
  async function openPin() {
    renderWorkspace();
    fireEvent.click(await screen.findByRole('button', { name: 'Inspect block-force-push' }));
    fireEvent.click(await screen.findByRole('tab', { name: 'Pin' }));
    await screen.findByLabelText('Approve reason');
  }

  it('gives the hook.json diff a taller scroll box', async () => {
    await openPin();
    const box = await screen.findByLabelText('Hook pin diff');
    expect(box).toHaveClass('max-h-96', 'scrollbar-dark', 'overflow-x-auto', 'overflow-y-auto');
    expect(box).not.toHaveClass('max-h-40');
  });

  it('names the registry directory when scripts changed', async () => {
    vi.mocked(fetchRegistryHook).mockResolvedValue({ ...detail, dir: '/home/u/.gridctl/registry/hooks/block\u202eforce' });
    vi.mocked(fetchHookPinDiff).mockResolvedValue({ ...diff, modified_files: ['scripts/block-force-push.sh'] });
    await openPin();
    const note = await screen.findByTestId('hook-script-note');
    await waitFor(() =>
      expect(note).toHaveTextContent(
        'Script contents are not shown here. Read the changed files under /home/u/.gridctl/registry/hooks/block\\u202eforce before approving.',
      ),
    );
    expect(document.body.textContent).not.toContain('\u202e');
  });

  it('falls back to the registry directory wording without dir', async () => {
    vi.mocked(fetchHookPinDiff).mockResolvedValue({ ...diff, added_files: ['scripts/new.sh'], modified_files: [] });
    await openPin();
    expect(await screen.findByTestId('hook-script-note')).toHaveTextContent(
      "Script contents are not shown here. Read the changed files in the hook's registry directory before approving.",
    );
  });

  it('omits the note for removed-only changes, no changes, unpinned, and a failed load', async () => {
    vi.mocked(fetchHookPinDiff).mockResolvedValue({ ...diff, removed_files: ['scripts/old.sh'], modified_files: [] });
    await openPin();
    expect(await screen.findByLabelText('Removed files')).toBeInTheDocument();
    expect(screen.queryByTestId('hook-script-note')).not.toBeInTheDocument();
    cleanup();

    vi.mocked(fetchHookPinDiff).mockResolvedValue(pinnedDiff);
    await openPin();
    expect(await screen.findByText('hook.json unchanged')).toBeInTheDocument();
    expect(screen.queryByTestId('hook-script-note')).not.toBeInTheDocument();
    cleanup();

    vi.mocked(fetchHookPinDiff).mockRejectedValue(new HTTPError(404, 'No pin found'));
    await openPin();
    expect(await screen.findByText('Not pinned; the next accepted import pins it')).toBeInTheDocument();
    expect(screen.queryByTestId('hook-script-note')).not.toBeInTheDocument();
    cleanup();

    vi.mocked(fetchHookPinDiff).mockRejectedValue(new Error('offline'));
    await openPin();
    expect(await screen.findByText(/Load hook pin diff/)).toBeInTheDocument();
    expect(screen.queryByTestId('hook-script-note')).not.toBeInTheDocument();
  });
});
