import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { ProposalsWorkspace } from '../components/registry/proposals/ProposalsWorkspace';
import { LibraryKindSwitch } from '../components/registry/LibraryKindSwitch';
import { showToast } from '../components/ui/Toast';
import type { Proposal, ProposalList } from '../types';

vi.mock('../components/ui/Toast', () => ({
  showToast: vi.fn(),
  ToastContainer: () => null,
}));

vi.mock('../lib/api', async () => {
  const actual = await vi.importActual<typeof import('../lib/api')>('../lib/api');
  return {
    ...actual,
    fetchProposals: vi.fn(),
    fetchProposal: vi.fn(),
    approveProposal: vi.fn(),
    rejectProposal: vi.fn(),
  };
});

import { approveProposal, fetchProposal, fetchProposals, HTTPError, rejectProposal } from '../lib/api';

const pending: Proposal = {
  id: 'p-3f9a1c2b7d4e',
  version: '1',
  kind: 'skill',
  target: 'incident-triage',
  base_hash: 'h2:4be1aaaaaaaaaaaaaaaa',
  base_current: true,
  diff: '--- a/SKILL.md\n+++ b/SKILL.md\n@@ -1,2 +1,2 @@\n keep\n-old line\n+new line\u202e\n',
  classification: 'knowledge',
  prevalence: { sessions: 14, runs: 6 },
  evidence: ['https://example.test/pr/1'],
  reason: '',
  source: 'dream',
  created_at: '2026-11-09T14:02:00Z',
  status: 'pending',
  path: 'apply',
  provenance: { kind: 'local' },
  findings: [{ code: 'imperative', severity: 'warn', confidence: 'high', field: 'body', message: 'ignore previous instructions' }],
  verdict: null,
  files: ['SKILL.md'],
};

const approved: Proposal = {
  ...pending,
  id: 'p-91ab00000000',
  target: 'commit-conventions',
  kind: 'context',
  status: 'approved',
  path: 'record',
  provenance: { kind: 'pack', pack: 'portable-pack' },
  findings: [],
  created_at: '2026-11-08T09:11:00Z',
  verdict: { at: '2026-11-08T10:00:00Z', by: 'local', applied: false, reason: 'ok' },
};

function list(...proposals: Proposal[]): ProposalList {
  return { schema_version: 1, proposals, skipped: [], truncated: false };
}

function renderWorkspace(entry = '/library?kind=proposal') {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <CommandRegistryProvider>
        <ProposalsWorkspace onKindChange={() => {}} />
      </CommandRegistryProvider>
    </MemoryRouter>,
  );
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  vi.mocked(fetchProposals).mockResolvedValue(list(approved, pending));
  vi.mocked(fetchProposal).mockImplementation(async (id: string) => ({
    ...(id === pending.id ? pending : approved),
    pin_status: 'pinned',
  }));
});

describe('ProposalsWorkspace', () => {
  it('lists pending proposals by default with a badge, and filters by status', async () => {
    renderWorkspace();
    const rows = await screen.findByRole('list', { name: 'Proposals' });
    expect(within(rows).getAllByRole('button')).toHaveLength(1);
    expect(within(rows).getByRole('button', { name: /p-3f9a1c2b7d4e/ })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Proposals/, pressed: true })).toHaveTextContent('1');

    fireEvent.click(screen.getByRole('button', { name: 'All' }));
    await waitFor(() => expect(within(rows).getAllByRole('button')).toHaveLength(2));
    expect(within(rows).getAllByRole('button')[0]).toHaveAccessibleName(/p-3f9a1c2b7d4e/);
  });

  it('names gridctl propose in the empty state', async () => {
    vi.mocked(fetchProposals).mockResolvedValue(list());
    renderWorkspace();
    expect(await screen.findByText(/Stage one with/)).toBeInTheDocument();
    expect(screen.getByText('gridctl propose')).toBeInTheDocument();
  });

  it('renders the diff escaped and gates approve on a reason when findings exist', async () => {
    renderWorkspace(`/library?kind=proposal&selected=${pending.id}`);
    await waitFor(() => expect(fetchProposal).toHaveBeenCalledWith(pending.id));
    const diff = await screen.findByLabelText('Proposal diff');
    expect(within(diff).getByText('+new line\\u202e')).toHaveAttribute('data-kind', 'added');
    expect(within(diff).getByText('-old line')).toHaveAttribute('data-kind', 'removed');
    expect(screen.getByText('ignore previous instructions')).toBeInTheDocument();

    const approve = await screen.findByRole('button', { name: 'Approve' });
    await waitFor(() => expect(screen.getByText(/requires a reason/)).toBeInTheDocument());
    expect(approve).toBeDisabled();

    vi.mocked(approveProposal).mockResolvedValue({
      ...pending,
      status: 'approved',
      approved_by: 'local',
      approved_by_source: 'os',
      evidence_count: 2,
      applied: true,
    });
    fireEvent.change(screen.getByLabelText(/^Reason/), { target: { value: 'reviewed the wording' } });
    fireEvent.change(screen.getByLabelText(/Additional evidence/), { target: { value: 'run:42\n\nrun:42' } });
    expect(approve).toBeEnabled();
    fireEvent.click(approve);
    await waitFor(() => expect(approveProposal).toHaveBeenCalledWith(pending.id, 'reviewed the wording', ['run:42']));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', expect.stringMatching(/re-pinned/)));
  });

  it('disables approve with an explanation for a stale base and for pin drift', async () => {
    vi.mocked(fetchProposal).mockResolvedValueOnce({ ...pending, findings: [], base_current: false, pin_status: 'pinned' });
    renderWorkspace(`/library?kind=proposal&selected=${pending.id}`);
    expect(await screen.findByText(/Approve refuses a stale base/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Approve' })).toBeDisabled();
    cleanup();

    vi.mocked(fetchProposal).mockResolvedValueOnce({ ...pending, findings: [], pin_status: 'drift' });
    renderWorkspace(`/library?kind=proposal&selected=${pending.id}`);
    expect(await screen.findByText(/in the Pins workspace before this proposal can land/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Approve' })).toBeDisabled();
  });

  it('refreshes the record on a 409 and shows 422 text inline', async () => {
    vi.mocked(fetchProposal).mockResolvedValue({ ...pending, findings: [], pin_status: 'unpinned' });
    renderWorkspace(`/library?kind=proposal&selected=${pending.id}`);
    const approve = await screen.findByRole('button', { name: 'Approve' });
    await waitFor(() => expect(approve).toBeEnabled());

    vi.mocked(approveProposal).mockRejectedValueOnce(new HTTPError(409, 'proposal p-3f9a1c2b7d4e is based on hash 4be1; skill incident-triage is now 9c02'));
    fireEvent.click(approve);
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('error', expect.stringMatching(/is now 9c02/)));
    await waitFor(() => expect(fetchProposal).toHaveBeenCalledTimes(2));

    vi.mocked(approveProposal).mockRejectedValueOnce(new HTTPError(422, 'hunk 1 of SKILL.md does not match the current content. Nothing was written.'));
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/does not match the current content/);
  });

  it('requires a reason to reject', async () => {
    vi.mocked(rejectProposal).mockResolvedValue({
      ...pending,
      status: 'rejected',
      approved_by: 'local',
      approved_by_source: 'os',
      evidence_count: 0,
    });
    renderWorkspace(`/library?kind=proposal&selected=${pending.id}`);
    fireEvent.click(await screen.findByRole('button', { name: 'Reject' }));
    const dialog = screen.getByRole('dialog', { name: 'Reject proposal' });
    const confirm = within(dialog).getByRole('button', { name: 'Confirm reject' });
    expect(confirm).toBeDisabled();
    fireEvent.change(within(dialog).getByLabelText(/Rejection reason/), { target: { value: 'duplicate' } });
    fireEvent.click(confirm);
    await waitFor(() => expect(rejectProposal).toHaveBeenCalledWith(pending.id, 'duplicate'));
  });
});

describe('LibraryKindSwitch', () => {
  it('loads the pending count when no count is passed', async () => {
    render(<LibraryKindSwitch kind="skill" onChange={() => {}} />);
    await waitFor(() => expect(fetchProposals).toHaveBeenCalledWith({ status: 'pending' }));
    expect(await screen.findByLabelText('2 pending')).toBeInTheDocument();
  });

  it('hides the badge when the count fails to load', async () => {
    vi.mocked(fetchProposals).mockRejectedValue(new Error('offline'));
    render(<LibraryKindSwitch kind="skill" onChange={() => {}} />);
    await waitFor(() => expect(fetchProposals).toHaveBeenCalled());
    expect(screen.queryByLabelText(/pending/)).not.toBeInTheDocument();
  });
});
