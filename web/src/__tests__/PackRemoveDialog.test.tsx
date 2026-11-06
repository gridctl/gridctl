import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import '@testing-library/jest-dom';
import { PackRemoveDialog } from '../components/registry/packs/PackRemoveDialog';
import { removePack, type PackRemoveDoc } from '../lib/api';
import { showToast } from '../components/ui/Toast';

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    removePack: vi.fn(),
  };
});

vi.mock('../components/ui/Toast', () => ({ showToast: vi.fn() }));

const mockRemove = vi.mocked(removePack);
const mockToast = vi.mocked(showToast);

const ruleRemediation =
  "keep the edit by renaming the fragment, or discard it with 'gridctl ctx rm team-style' before removing, or force removal with --force";

function dryRun(rows: PackRemoveDoc['rows']): PackRemoveDoc {
  return { pack: 'team-pack', dry_run: true, rows };
}

describe('PackRemoveDialog', () => {
  beforeEach(() => {
    mockRemove.mockReset();
    mockToast.mockReset();
  });

  it('uses a kept rule remediation in the aftermath toast', async () => {
    mockRemove
      .mockResolvedValueOnce(
        dryRun([{ kind: 'skill', name: 'alpha', action: 'would-remove' }]),
      )
      .mockResolvedValueOnce({
        pack: 'team-pack',
        rows: [
          {
            kind: 'rule',
            name: 'other-style',
            action: 'removed',
            remediation: "run 'gridctl ctx sync' to drop projected client files for this fragment",
          },
          {
            kind: 'rule',
            name: 'team-style',
            action: 'skipped-drift',
            remediation: ruleRemediation,
          },
        ],
        kept: ['rule/team-style'],
      });

    render(<PackRemoveDialog packName="team-pack" onClose={vi.fn()} onRemoved={vi.fn()} />);
    const remove = await screen.findByRole('button', { name: 'Remove (keep local edits)' });
    await waitFor(() => expect(remove).toBeEnabled());
    fireEvent.click(remove);

    await waitFor(() => expect(mockToast).toHaveBeenCalledWith('warning', expect.stringContaining(ruleRemediation)));
    expect(mockToast).not.toHaveBeenCalledWith('warning', expect.stringContaining('Adopt the edits'));
    expect(mockToast).not.toHaveBeenCalledWith('warning', expect.stringContaining('gridctl ctx sync'));
  });

  it('keeps the adopt sentence when no kept row carries a remediation', async () => {
    mockRemove
      .mockResolvedValueOnce(
        dryRun([{ kind: 'skill', name: 'alpha', action: 'would-remove' }]),
      )
      .mockResolvedValueOnce({
        pack: 'team-pack',
        rows: [{ kind: 'skill', name: 'alpha', action: 'skipped-drift' }],
        kept: ['skill/alpha'],
      });

    render(<PackRemoveDialog packName="team-pack" onClose={vi.fn()} onRemoved={vi.fn()} />);
    const remove = await screen.findByRole('button', { name: 'Remove (keep local edits)' });
    await waitFor(() => expect(remove).toBeEnabled());
    fireEvent.click(remove);

    await waitFor(() =>
      expect(mockToast).toHaveBeenCalledWith(
        'warning',
        expect.stringContaining('Adopt the edits or remove with overwrite to finish.'),
      ),
    );
  });
});
