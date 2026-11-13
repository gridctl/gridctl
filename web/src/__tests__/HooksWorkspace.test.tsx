import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { HooksWorkspace } from '../components/registry/hooks/HooksWorkspace';
import type { PinFinding } from '../lib/api';
import type { HookPinDiff, HookProjectionStatus, RegistryHook, RegistryHookList } from '../types';

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
  };
});

import {
  approveHookPin,
  fetchHookPinDiff,
  fetchHookProjectionStatus,
  fetchRegistryHook,
  fetchRegistryHooks,
  HTTPError,
  syncHookProjections,
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

function renderWorkspace() {
  return render(
    <MemoryRouter initialEntries={['/library?kind=hook']}>
      <CommandRegistryProvider>
        <HooksWorkspace onKindChange={() => {}} />
      </CommandRegistryProvider>
    </MemoryRouter>,
  );
}

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
