import { describe, it, expect, beforeEach, vi } from 'vitest';
import '@testing-library/jest-dom';
import { render, screen, cleanup, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { CommandRegistryProvider } from '../hooks/useCommandRegistry';
import { ProposalsWorkspace } from '../components/registry/proposals/ProposalsWorkspace';
import { LibraryKindSwitch } from '../components/registry/LibraryKindSwitch';
import { SEGMENT_GROUP_CLASS, segmentClass } from '../components/ui/segment';
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
  it('renders one visible Library title', async () => {
    renderWorkspace();
    expect(screen.getAllByRole('heading', { level: 1 })).toHaveLength(1);
    expect(screen.getByRole('heading', { level: 1, name: 'Library' }).className).not.toMatch(/sr-only|uppercase|tracking-/);
    await waitFor(() => expect(screen.getByTestId('workspace-header-summary')).toHaveTextContent(/\d+ proposals?/));
  });

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

    const approve = await screen.findByRole('button', { name: 'Approve and apply' });
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
    // A warn finding interposes the confirmation; nothing is sent until it is accepted.
    const dialog = screen.getByRole('alertdialog', { name: 'Approve and apply' });
    expect(dialog).toHaveTextContent('Approving applies the diff to the target');
    expect(dialog).toHaveTextContent('1 warn-or-critical scan finding');
    expect(approveProposal).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Approve and apply' }));
    await waitFor(() => expect(approveProposal).toHaveBeenCalledWith(pending.id, 'reviewed the wording', ['run:42']));
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('success', expect.stringMatching(/re-pinned/)));
  });

  it('disables approve with an explanation for a stale base and for pin drift', async () => {
    vi.mocked(fetchProposal).mockResolvedValueOnce({ ...pending, findings: [], base_current: false, pin_status: 'pinned' });
    renderWorkspace(`/library?kind=proposal&selected=${pending.id}`);
    expect(await screen.findByText(/Approve refuses a stale base/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Approve and apply' })).toBeDisabled();
    cleanup();

    vi.mocked(fetchProposal).mockResolvedValueOnce({ ...pending, findings: [], pin_status: 'drift' });
    renderWorkspace(`/library?kind=proposal&selected=${pending.id}`);
    expect(await screen.findByText(/in the Pins workspace before this proposal can land/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Approve and apply' })).toBeDisabled();
  });

  it('refreshes the record on a 409 and shows 422 text inline', async () => {
    vi.mocked(fetchProposal).mockResolvedValue({ ...pending, findings: [], pin_status: 'unpinned' });
    renderWorkspace(`/library?kind=proposal&selected=${pending.id}`);
    const approve = await screen.findByRole('button', { name: 'Approve and apply' });
    await waitFor(() => expect(approve).toBeEnabled());

    vi.mocked(approveProposal).mockRejectedValueOnce(new HTTPError(409, 'proposal p-3f9a1c2b7d4e is based on hash 4be1; skill incident-triage is now 9c02'));
    fireEvent.click(approve);
    await waitFor(() => expect(showToast).toHaveBeenCalledWith('error', expect.stringMatching(/is now 9c02/)));
    await waitFor(() => expect(fetchProposal).toHaveBeenCalledTimes(2));

    vi.mocked(approveProposal).mockRejectedValueOnce(new HTTPError(422, 'hunk 1 of SKILL.md does not match the current content. Nothing was written.'));
    fireEvent.click(screen.getByRole('button', { name: 'Approve and apply' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(/does not match the current content/);
  });

  it('approves a small clean proposal in one click', async () => {
    vi.mocked(fetchProposal).mockResolvedValue({ ...pending, findings: [], pin_status: 'pinned' });
    vi.mocked(approveProposal).mockResolvedValue({
      ...pending,
      status: 'approved',
      approved_by: 'local',
      approved_by_source: 'os',
      evidence_count: 0,
      applied: true,
    });
    renderWorkspace(`/library?kind=proposal&selected=${pending.id}`);
    const approve = await screen.findByRole('button', { name: 'Approve and apply' });
    await waitFor(() => expect(approve).toBeEnabled());
    fireEvent.click(approve);
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    await waitFor(() => expect(approveProposal).toHaveBeenCalledWith(pending.id, undefined, []));
  });

  it('confirms a large proposal before approving', async () => {
    const big = Array.from({ length: 50 }, (_, i) => `+added ${i}`).join('\n');
    vi.mocked(fetchProposal).mockResolvedValue({
      ...pending,
      findings: [],
      pin_status: 'pinned',
      diff: `--- a/SKILL.md\n+++ b/SKILL.md\n@@ -1,1 +1,51 @@\n keep\n${big}\n`,
    });
    renderWorkspace(`/library?kind=proposal&selected=${pending.id}`);
    const approve = await screen.findByRole('button', { name: 'Approve and apply' });
    await waitFor(() => expect(approve).toBeEnabled());
    fireEvent.click(approve);
    const dialog = screen.getByRole('alertdialog', { name: 'Approve and apply' });
    expect(dialog).toHaveTextContent('This is a large change.');
    expect(approveProposal).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument();
    expect(approveProposal).not.toHaveBeenCalled();
  });

  it('labels a record-path proposal Record approval', async () => {
    const record: Proposal = { ...pending, path: 'record', provenance: { kind: 'git' }, findings: [] };
    vi.mocked(fetchProposals).mockResolvedValue(list(record));
    vi.mocked(fetchProposal).mockResolvedValue({ ...record, pin_status: 'pinned' });
    vi.mocked(approveProposal).mockResolvedValue({
      ...record,
      status: 'approved',
      approved_by: 'local',
      approved_by_source: 'os',
      evidence_count: 0,
      applied: false,
    });
    renderWorkspace(`/library?kind=proposal&selected=${record.id}`);
    const approve = await screen.findByRole('button', { name: 'Record approval' });
    expect(screen.queryByRole('button', { name: 'Approve and apply' })).not.toBeInTheDocument();
    await waitFor(() => expect(approve).toBeEnabled());
    expect(approve).toHaveAttribute('title', 'Record the approval on this proposal; nothing is written');
    fireEvent.click(approve);
    await waitFor(() => expect(approveProposal).toHaveBeenCalledWith(record.id, undefined, []));
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

describe('Proposals load failures', () => {
  it('shows a failed list state with Retry and no toast, then recovers', async () => {
    vi.mocked(fetchProposals).mockRejectedValue(new Error('offline'));
    renderWorkspace();

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Proposals unavailable');
    expect(alert).toHaveTextContent('Load proposals failed: offline.');
    expect(screen.queryByText('Loading proposals…')).not.toBeInTheDocument();
    expect(showToast).not.toHaveBeenCalled();

    vi.mocked(fetchProposals).mockResolvedValue(list(approved, pending));
    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    const rows = await screen.findByRole('list', { name: 'Proposals' });
    await waitFor(() => expect(within(rows).getAllByRole('button')).toHaveLength(1));
    expect(screen.queryByText('Proposals unavailable')).not.toBeInTheDocument();
  });

  it('keeps the list under a banner when a refresh fails', async () => {
    renderWorkspace();
    const rows = await screen.findByRole('list', { name: 'Proposals' });
    await waitFor(() => expect(within(rows).getAllByRole('button')).toHaveLength(1));

    vi.mocked(fetchProposals).mockRejectedValueOnce(new Error('offline'));
    fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Load proposals failed: offline.');
    expect(within(rows).getAllByRole('button')).toHaveLength(1);
  });

  it('keeps approve disabled with a visible reason when the detail fetch fails', async () => {
    vi.mocked(fetchProposal).mockRejectedValueOnce(new Error('offline'));
    renderWorkspace(`/library?kind=proposal&selected=${pending.id}`);

    const alert = await screen.findByRole('alert');
    expect(alert).toHaveTextContent('Load proposal failed: offline.');
    const summary = screen.getByRole('region', { name: 'Summary' });
    expect(within(summary).getByText('unavailable')).toBeInTheDocument();
    expect(within(summary).queryByText('loading')).not.toBeInTheDocument();
    expect(
      screen.getByText('The current base and pin status could not be loaded. Retry to enable Approve.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Approve and apply' })).toBeDisabled();
    expect(showToast).not.toHaveBeenCalled();

    fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(within(summary).getByText('pinned')).toBeInTheDocument());
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    expect(screen.queryByText(/could not be loaded/)).not.toBeInTheDocument();
    expect(await screen.findByText(/requires a reason/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Approve and apply' })).toBeDisabled();
  });
});

describe('LibraryKindSwitch', () => {
  it('renders six aria-pressed segments with the shared segment chrome', async () => {
    render(<LibraryKindSwitch kind="agent" onChange={() => {}} />);
    const group = screen.getByRole('group', { name: 'Library catalog kind' });
    expect(group.className).toContain(SEGMENT_GROUP_CLASS);
    const buttons = within(group).getAllByRole('button');
    expect(buttons).toHaveLength(6);
    for (const button of buttons) {
      expect(button).toHaveAttribute('type', 'button');
      expect(button).toHaveAttribute('aria-pressed');
      expect(button.className).not.toMatch(/uppercase|tracking-\[0\.25em\]/);
    }
    const agents = within(group).getByRole('button', { name: 'Agents' });
    expect(agents).toHaveAttribute('aria-pressed', 'true');
    expect(agents.className).toContain(segmentClass(true));
    const skills = within(group).getByRole('button', { name: 'Skills' });
    expect(skills).toHaveAttribute('aria-pressed', 'false');
    expect(skills.className).toContain(segmentClass(false));
    const context = within(group).getByRole('button', { name: 'Global Context' });
    expect(context).toHaveTextContent('Context');
    expect(context).toHaveAttribute('title', 'Global Context');
    expect(context).toHaveAttribute('aria-pressed', 'false');
    expect(buttons.indexOf(context)).toBe(4);
    expect(await screen.findByLabelText('2 pending')).toBeInTheDocument();
  });

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
