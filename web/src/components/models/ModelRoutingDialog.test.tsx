import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor } from '@testing-library/react';
import { ModelRoutingDialog } from './ModelRoutingDialog';
import type { ModelsStatusDoc, ModelsValidateDoc } from '../../types';

vi.mock('../ui/Toast', async () => {
  const actual = await vi.importActual<typeof import('../ui/Toast')>('../ui/Toast');
  return { ...actual, showToast: vi.fn() };
});

vi.mock('../../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../../lib/api')>('../../lib/api');
  return {
    ...actual,
    fetchModelsStatus: vi.fn(),
    fetchModelsValidation: vi.fn(),
    syncModels: vi.fn(),
    adoptModels: vi.fn(),
    ackModelsRestart: vi.fn(),
  };
});

import {
  ackModelsRestart,
  fetchModelsStatus,
  fetchModelsValidation,
  syncModels,
} from '../../lib/api';

const syncedDoc: ModelsStatusDoc = {
  policy_path: '/home/u/.gridctl/models/policy.yaml',
  policy_exists: true,
  needs_attention: false,
  routing: {
    entry_model: 'smart-router',
    default_tier: 'MEDIUM',
    backends: ['qwen-local', 'claude-sonnet'],
    tiers: {
      SIMPLE: 'qwen-local',
      MEDIUM: 'qwen-local',
      COMPLEX: 'claude-sonnet',
      REASONING: 'claude-sonnet',
    },
  },
  targets: [
    {
      target: 'litellm-fragment',
      client: 'litellm',
      state: 'in-sync',
      restart_pending: true,
      path: '/home/u/litellm/gridctl-models.yaml',
    },
    { target: 'opencode', client: 'opencode', state: 'in-sync', path: '/home/u/.config/opencode/opencode.json' },
  ],
};

const cleanValidation: ModelsValidateDoc = {
  policy_path: '/home/u/.gridctl/models/policy.yaml',
  valid: true,
  issues: [],
};

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.mocked(fetchModelsStatus).mockResolvedValue(syncedDoc);
  vi.mocked(fetchModelsValidation).mockResolvedValue(cleanValidation);
  vi.mocked(syncModels).mockResolvedValue([]);
  vi.mocked(ackModelsRestart).mockResolvedValue({ acknowledged: true });
});

function renderDialog(onClose = vi.fn()) {
  render(<ModelRoutingDialog isOpen onClose={onClose} />);
  return onClose;
}

describe('ModelRoutingDialog', () => {
  it('renders the routing summary, targets, and disambiguation', async () => {
    renderDialog();
    expect(await screen.findByText('LiteLLM router fragment')).toBeInTheDocument();
    expect(screen.getByText('OpenCode provider')).toBeInTheDocument();
    expect(screen.getByText('smart-router')).toBeInTheDocument();
    expect(screen.getByText('Experimental')).toBeInTheDocument();
    // The default tier is marked once, on MEDIUM.
    expect(screen.getByText('default')).toBeInTheDocument();
    expect(screen.getByText(/Not skill or agent/)).toBeInTheDocument();
  });

  it('shows the restart-pending chip and confirms Mark restarted', async () => {
    renderDialog();
    expect(await screen.findByText('restart pending')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Mark restarted' }));
    // The confirm carries the honesty copy; nothing fires until confirmed.
    expect(screen.getByText(/gridctl never probes the LiteLLM process/)).toBeInTheDocument();
    expect(ackModelsRestart).not.toHaveBeenCalled();

    const confirm = screen.getAllByRole('button', { name: 'Mark restarted' });
    fireEvent.click(confirm[confirm.length - 1]);
    await waitFor(() => expect(ackModelsRestart).toHaveBeenCalledTimes(1));
  });

  it('offers whole-policy sync only and previews via dry-run diff', async () => {
    renderDialog();
    const preview = await screen.findByRole('button', { name: 'Preview changes' });
    fireEvent.click(preview);
    await waitFor(() =>
      expect(syncModels).toHaveBeenCalledWith({ dry_run: true, diff: true }),
    );

    fireEvent.click(screen.getByRole('button', { name: 'Sync all targets' }));
    await waitFor(() => expect(syncModels).toHaveBeenCalledWith({}));
  });

  it('disables Preview and Sync while validation has errors', async () => {
    vi.mocked(fetchModelsValidation).mockResolvedValue({
      policy_path: '/home/u/.gridctl/models/policy.yaml',
      valid: false,
      issues: [{ severity: 'error', field: 'router.entry_model', message: 'entry_model is required' }],
    });
    renderDialog();
    const sync = await screen.findByRole('button', { name: 'Sync all targets' });
    expect(sync).toBeDisabled();
    expect(sync).toHaveAttribute('title', expect.stringContaining('validation errors'));
    expect(screen.getByRole('button', { name: 'Preview changes' })).toBeDisabled();
    // The findings render as rows.
    expect(screen.getByText('entry_model is required')).toBeInTheDocument();
  });

  it('review offers Overwrite but not Accept on include-only drift', async () => {
    vi.mocked(fetchModelsStatus).mockResolvedValue({
      ...syncedDoc,
      targets: [
        { target: 'litellm-fragment', client: 'litellm', state: 'in-sync' },
        { target: 'litellm-include', client: 'litellm', state: 'drifted', path: '/home/u/litellm/config.yaml' },
      ],
    });
    renderDialog();
    fireEvent.click(await screen.findByRole('button', { name: 'Review drift' }));
    expect(await screen.findByText(/overwriting is the one resolution/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Accept on-disk as owned' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Overwrite with policy' })).toBeInTheDocument();
    // The review previews the forced pass: what Overwrite would write.
    await waitFor(() =>
      expect(syncModels).toHaveBeenCalledWith({ dry_run: true, diff: true, force: true }),
    );
  });

  it('skipped results offer Overwrite, so force is reachable without drift', async () => {
    // Foreign files never show as drifted: status stays never-synced and
    // the sync pass reports skipped-foreign. Without this affordance the
    // engine copy points at a --force no button can set.
    vi.mocked(syncModels).mockImplementation(async (body) => {
      if (body?.dry_run) {
        return [{ target: 'litellm-fragment', client: 'litellm', path: '/x', action: 'would-update', diff: '--- a\n+++ b' }];
      }
      if (body?.force) {
        return [{ target: 'litellm-fragment', client: 'litellm', path: '/x', action: 'updated' }];
      }
      return [
        { target: 'litellm-fragment', client: 'litellm', path: '/x', action: 'skipped-foreign', detail: 'a file already exists at the fragment path' },
      ];
    });
    renderDialog();
    fireEvent.click(await screen.findByRole('button', { name: 'Sync all targets' }));
    expect(await screen.findByText('skipped-foreign')).toBeInTheDocument();

    const overwrite = screen.getByRole('button', { name: 'Overwrite with policy' });
    fireEvent.click(overwrite);
    // The confirm names the forced pass's real write set once the
    // preview resolves.
    expect(await screen.findByText(/Rewrite LiteLLM router fragment from the/)).toBeInTheDocument();
    expect(screen.getByText(/latches restart-pending/)).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Overwrite' }));
    await waitFor(() =>
      expect(syncModels).toHaveBeenCalledWith({ force: true }),
    );
  });

  it('Overwrite confirm names the whole forced write set, not only drift', async () => {
    // Force is whole-policy: a stale fragment is rewritten alongside the
    // drifted OpenCode row the review was opened for.
    vi.mocked(fetchModelsStatus).mockResolvedValue({
      ...syncedDoc,
      targets: [
        { target: 'litellm-fragment', client: 'litellm', state: 'stale' },
        { target: 'opencode', client: 'opencode', state: 'drifted' },
      ],
    });
    vi.mocked(syncModels).mockResolvedValue([
      { target: 'litellm-fragment', client: 'litellm', path: '/x', action: 'would-update', diff: 'd1' },
      { target: 'opencode', client: 'opencode', path: '/y', action: 'would-update', diff: 'd2' },
    ]);
    renderDialog();
    fireEvent.click(await screen.findByRole('button', { name: 'Review drift' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Overwrite with policy' }));
    const confirm = await screen.findByText(/Rewrite LiteLLM router fragment, OpenCode provider/);
    expect(confirm).toBeInTheDocument();
    expect(screen.getByText(/latches restart-pending/)).toBeInTheDocument();
  });

  it('gates Review on validation errors when Overwrite is the only resolution', async () => {
    vi.mocked(fetchModelsStatus).mockResolvedValue({
      ...syncedDoc,
      targets: [
        { target: 'litellm-fragment', client: 'litellm', state: 'in-sync' },
        { target: 'litellm-include', client: 'litellm', state: 'drifted' },
      ],
    });
    vi.mocked(fetchModelsValidation).mockResolvedValue({
      policy_path: '/home/u/.gridctl/models/policy.yaml',
      valid: false,
      issues: [{ severity: 'error', field: 'router.entry_model', message: 'entry_model is required' }],
    });
    renderDialog();
    const review = await screen.findByRole('button', { name: 'Review drift' });
    expect(review).toBeDisabled();
    expect(review).toHaveAttribute('title', expect.stringContaining('valid policy'));
  });

  it('Escape closes exactly one stacked layer per press', async () => {
    vi.mocked(fetchModelsStatus).mockResolvedValue({
      ...syncedDoc,
      targets: [
        { target: 'litellm-fragment', client: 'litellm', state: 'in-sync' },
        { target: 'opencode', client: 'opencode', state: 'drifted', path: '/home/u/.config/opencode/opencode.json' },
      ],
    });
    const onClose = renderDialog();
    fireEvent.click(await screen.findByRole('button', { name: 'Review drift' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Accept on-disk as owned' }));
    expect(screen.getByText(/No file is rewritten/)).toBeInTheDocument();

    // Three document-level keydown listeners are live (dialog, review,
    // confirm); one press must close only the confirm.
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByText(/No file is rewritten/)).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Review drift' })).toBeInTheDocument();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('heading', { name: 'Review drift' })).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Model routing' })).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('shows the init empty state when no policy exists', async () => {
    vi.mocked(fetchModelsStatus).mockResolvedValue({
      policy_path: '/home/u/.gridctl/models/policy.yaml',
      policy_exists: false,
      needs_attention: false,
      targets: [
        { target: 'litellm-fragment', client: 'litellm', state: 'never-synced', detail: "no policy; run 'gridctl models init'" },
      ],
    });
    renderDialog();
    expect(await screen.findByText('No model routing policy yet')).toBeInTheDocument();
    expect(screen.getByText('gridctl models init')).toBeInTheDocument();
    // No action bar without a policy.
    expect(screen.queryByRole('button', { name: 'Sync all targets' })).not.toBeInTheDocument();
    expect(fetchModelsValidation).not.toHaveBeenCalled();
  });

  it('carries a parse failure as a document banner, not a crash', async () => {
    vi.mocked(fetchModelsStatus).mockResolvedValue({
      policy_path: '/home/u/.gridctl/models/policy.yaml',
      policy_exists: true,
      needs_attention: false,
      policy_error: 'parsing models policy: yaml: line 2',
      targets: [],
    });
    renderDialog();
    expect(await screen.findByText(/does not parse/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Sync all targets' })).not.toBeInTheDocument();
  });
});
