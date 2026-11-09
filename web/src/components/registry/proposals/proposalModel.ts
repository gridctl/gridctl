import type { Proposal, ProposalStatus } from '../../../types';

export type ProposalStatusFilter = ProposalStatus | 'all';

export const PROPOSAL_STATUS_FILTERS: { key: ProposalStatusFilter; label: string }[] = [
  { key: 'pending', label: 'Pending' },
  { key: 'approved', label: 'Approved' },
  { key: 'rejected', label: 'Rejected' },
  { key: 'all', label: 'All' },
];

export function isProposalStatusFilter(value: string | null): value is ProposalStatusFilter {
  return value === 'pending' || value === 'approved' || value === 'rejected' || value === 'all';
}

// sortProposals matches the store's order: pending first, then newest
// first, with the id as a stable tie-break.
export function sortProposals(list: Proposal[]): Proposal[] {
  return [...list].sort((a, b) => {
    const ap = a.status === 'pending' ? 0 : 1;
    const bp = b.status === 'pending' ? 0 : 1;
    if (ap !== bp) return ap - bp;
    const at = Date.parse(a.created_at) || 0;
    const bt = Date.parse(b.created_at) || 0;
    if (at !== bt) return bt - at;
    return a.id.localeCompare(b.id);
  });
}

export function filterProposals(list: Proposal[], status: ProposalStatusFilter): Proposal[] {
  if (status === 'all') return list;
  return list.filter((p) => p.status === status);
}

export function countPending(list: Proposal[] | null): number {
  return (list ?? []).filter((p) => p.status === 'pending').length;
}

// approveBlock returns why Approve is disabled, or null when it is enabled.
// The server enforces every one of these; the UI gates up front so the
// reviewer sees the reason instead of bouncing off a 409 or 400.
export function approveBlock(p: Proposal, reason: string): string | null {
  if (p.status !== 'pending') {
    return `This proposal is already ${p.status}.`;
  }
  if (!p.base_current) {
    return 'The target changed since the proposer read it. Approve refuses a stale base; the proposer must re-base against the current content.';
  }
  if (p.path === 'apply' && p.pin_status === 'drift') {
    return 'The target has unreviewed pin drift. Approve or reset the drift in the Pins workspace before this proposal can land.';
  }
  if ((p.findings?.length ?? 0) > 0 && reason.trim() === '') {
    return 'Approving over advisory findings requires a reason.';
  }
  return null;
}

// arrival says how a record-path change reaches the target. Hooks import
// only through packs. Mirrors the CLI's wording.
function arrival(p: Proposal): string {
  if (p.kind === 'hook' || p.provenance.kind === 'pack') {
    return "the change arrives through 'gridctl pack add'";
  }
  if (p.provenance.kind === 'local-import') {
    const dir = p.provenance.source_dir || 'the source directory';
    return `edit ${dir} and run 'gridctl skill update'`;
  }
  return "the change arrives through 'gridctl skill update'";
}

// pathExplanation is the one-sentence summary of what Approve will do.
export function pathExplanation(p: Proposal): string {
  if (p.path === 'apply') {
    return 'Approve applies the diff to the target, re-pins it, and records the approval with this proposal as evidence.';
  }
  return `Approve records the verdict on the proposal only; nothing is written and no pin changes. ${capitalize(arrival(p))}, and the approval of the resulting pin drift should cite proposal:${p.id}.`;
}

export function provenanceLabel(p: Proposal): string {
  switch (p.provenance.kind) {
    case 'pack':
      return p.provenance.pack ? `pack ${p.provenance.pack}` : 'pack';
    case 'local-import':
      return p.provenance.source_dir ? `imported from ${p.provenance.source_dir}` : 'local import';
    case 'git':
      return 'git import';
    case 'local':
      return 'local';
    default:
      return p.provenance.kind || 'unknown';
  }
}

function capitalize(s: string): string {
  return s.length === 0 ? s : s[0].toUpperCase() + s.slice(1);
}

export type UnifiedLineKind = 'header' | 'hunk' | 'added' | 'removed' | 'context' | 'note';

export interface UnifiedLine {
  kind: UnifiedLineKind;
  text: string;
}

const HUNK_HEADER = /^@@ -\d+(?:,(\d+))? \+\d+(?:,(\d+))? @@/;

// classifyUnifiedDiff labels each line of a stored unified diff by its
// prefix. It tracks hunk line counts so a removed line whose content starts
// with "-- " is not mistaken for a "--- " file header.
export function classifyUnifiedDiff(diff: string): UnifiedLine[] {
  const lines = diff.replace(/\r\n/g, '\n').split('\n');
  if (lines.length > 0 && lines[lines.length - 1] === '') lines.pop();
  const out: UnifiedLine[] = [];
  let oldLeft = 0;
  let newLeft = 0;
  for (const text of lines) {
    if (oldLeft > 0 || newLeft > 0) {
      if (text.startsWith('+')) {
        newLeft--;
        out.push({ kind: 'added', text });
        continue;
      }
      if (text.startsWith('-')) {
        oldLeft--;
        out.push({ kind: 'removed', text });
        continue;
      }
      if (text.startsWith(' ') || text === '') {
        oldLeft--;
        newLeft--;
        out.push({ kind: 'context', text });
        continue;
      }
      if (text.startsWith('\\')) {
        out.push({ kind: 'note', text });
        continue;
      }
      oldLeft = 0;
      newLeft = 0;
    }
    const hunk = HUNK_HEADER.exec(text);
    if (hunk) {
      oldLeft = hunk[1] === undefined ? 1 : Number(hunk[1]);
      newLeft = hunk[2] === undefined ? 1 : Number(hunk[2]);
      out.push({ kind: 'hunk', text });
      continue;
    }
    if (text.startsWith('\\')) {
      out.push({ kind: 'note', text });
      continue;
    }
    out.push({ kind: 'header', text });
  }
  return out;
}
