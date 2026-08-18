import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ResetDialog } from '../components/system/ResetDialog';
import { fetchResetPreview, executeReset } from '../lib/api';
import type { ResetDoc, ResetPreviewResponse } from '../lib/api';

vi.mock('../lib/api', () => ({
  fetchResetPreview: vi.fn(),
  executeReset: vi.fn(),
}));

const GRIDCTL_DIR = '/Users/demo/.gridctl';

function previewResponse(overrides?: Partial<ResetDoc>): ResetPreviewResponse {
  return {
    confirm_token: 'tok-1',
    confirm_phrase: GRIDCTL_DIR,
    doc: {
      schema_version: 1,
      home: '/Users/demo',
      purge: false,
      dry_run: true,
      failed: 0,
      rows: [
        { kind: 'skill', name: 'review-pr', client: 'claude-code', path: '/Users/demo/.claude/skills/review-pr', action: 'would-remove' },
        { kind: 'skill', name: 'edited-one', action: 'kept-drift', detail: 'hand-edited; kept' },
        { kind: 'wiring', name: 'gridctl', client: 'cursor', path: '/Users/demo/.cursor/mcp.json', action: 'would-remove' },
        { kind: 'daemon', name: 'devstack', action: 'would-stop', detail: 'pid 1, port 8180' },
      ],
      ...overrides,
    },
  };
}

describe('ResetDialog', () => {
  beforeEach(() => {
    vi.mocked(fetchResetPreview).mockReset();
    vi.mocked(executeReset).mockReset();
  });

  it('opens on the server preview and groups rows by consequence class', async () => {
    vi.mocked(fetchResetPreview).mockResolvedValue(previewResponse());
    render(<ResetDialog isOpen onClose={() => {}} />);

    // Nothing actionable until the dry run resolves.
    expect(screen.getByRole('status')).toHaveTextContent(/blast radius/i);

    await waitFor(() => expect(screen.getByText(/will be removed/i)).toBeInTheDocument());
    // Drift-kept items render as a reassurance, never in the removal list.
    expect(screen.getByText(/your edits are safe/i)).toBeInTheDocument();
    expect(screen.getByText(/edited-one/)).toBeInTheDocument();
    // Per-client groups, not a flat list.
    expect(screen.getByRole('button', { name: /claude-code/i })).toBeInTheDocument();
    // The honest no-restore wording is present from the first screen.
    expect(screen.getAllByText(/no restore command/i).length).toBeGreaterThan(0);
  });

  it('refetches the preview when the tier changes (tokens are tier-bound)', async () => {
    vi.mocked(fetchResetPreview).mockResolvedValue(previewResponse());
    render(<ResetDialog isOpen onClose={() => {}} />);
    await waitFor(() => expect(fetchResetPreview).toHaveBeenCalledWith({ purge: false }));

    fireEvent.click(screen.getByRole('radio', { name: /reset and delete/i }));
    await waitFor(() => expect(fetchResetPreview).toHaveBeenCalledWith({ purge: true }));
  });

  it('disables the purge confirm until the RESOLVED path is typed exactly', async () => {
    vi.mocked(fetchResetPreview).mockResolvedValue(previewResponse());
    render(<ResetDialog isOpen onClose={() => {}} />);
    await waitFor(() => expect(screen.getByText(/will be removed/i)).toBeInTheDocument());

    fireEvent.click(screen.getByRole('radio', { name: /reset and delete/i }));
    await waitFor(() => expect(screen.getByText(/will be removed/i)).toBeInTheDocument());
    fireEvent.click(screen.getByRole('button', { name: /continue to confirm/i }));

    const confirm = screen.getByRole('button', { name: /reset and delete/i });
    expect(confirm).toBeDisabled();

    const input = screen.getByLabelText(/type .* to confirm/i);
    // The literal tilde form must not enable the button.
    fireEvent.change(input, { target: { value: '~/.gridctl' } });
    expect(confirm).toBeDisabled();

    fireEvent.change(input, { target: { value: GRIDCTL_DIR } });
    expect(confirm).toBeEnabled();
  });

  it('executes with the preview token and announces the result via role=alert', async () => {
    vi.mocked(fetchResetPreview).mockResolvedValue(previewResponse());
    vi.mocked(executeReset).mockResolvedValue({
      schema_version: 1,
      home: '/Users/demo',
      purge: false,
      dry_run: false,
      backup_path: '/Users/demo/.gridctl/backups/reset-x.tar.gz',
      failed: 1,
      rows: [
        { kind: 'skill', name: 'review-pr', action: 'removed' },
        { kind: 'containers', name: 'devstack', action: 'failed', error: 'docker unavailable' },
      ],
    });
    render(<ResetDialog isOpen onClose={() => {}} />);
    await waitFor(() => expect(screen.getByText(/will be removed/i)).toBeInTheDocument());

    fireEvent.click(screen.getByRole('button', { name: /continue to confirm/i }));
    fireEvent.click(screen.getByRole('button', { name: /reset \(keep state directory\)/i }));

    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
    expect(executeReset).toHaveBeenCalledWith({
      purge: false,
      confirm_token: 'tok-1',
      confirm_phrase: '',
    });
    // Partial failure is stated with the idempotent-retry remediation and
    // the failed row's server error verbatim.
    expect(screen.getByRole('alert')).toHaveTextContent(/1 removed · 1 failed/);
    expect(screen.getByText(/run it again to retry/i)).toBeInTheDocument();
    expect(screen.getByText(/docker unavailable/)).toBeInTheDocument();
    expect(screen.getByText(/backup saved/i)).toBeInTheDocument();
  });

  it('reports a failed execute without pretending anything was removed', async () => {
    vi.mocked(fetchResetPreview).mockResolvedValue(previewResponse());
    vi.mocked(executeReset).mockRejectedValue(new Error('a reset is already running'));
    render(<ResetDialog isOpen onClose={() => {}} />);
    await waitFor(() => expect(screen.getByText(/will be removed/i)).toBeInTheDocument());

    fireEvent.click(screen.getByRole('button', { name: /continue to confirm/i }));
    fireEvent.click(screen.getByRole('button', { name: /reset \(keep state directory\)/i }));

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/already running/));
  });

  it('surfaces a preview failure instead of a permanently empty dialog', async () => {
    vi.mocked(fetchResetPreview).mockRejectedValue(new Error('reset is only accepted from loopback connections'));
    render(<ResetDialog isOpen onClose={() => {}} />);
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent(/loopback/));
  });
});
