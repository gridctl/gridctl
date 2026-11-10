import { useCallback, useEffect, useId, useState } from 'react';
import { FileDiff } from 'lucide-react';
import { InspectorHeader } from '../../inspector';
import { Button } from '../../ui/Button';
import { showToast } from '../../ui/Toast';
import { approveProposal, fetchProposal, HTTPError, rejectProposal } from '../../../lib/api';
import { cn } from '../../../lib/cn';
import { describeError, isCancellation } from '../../../lib/describeError';
import { escapeNonPrintable, shortPinHash } from '../../../lib/nonPrintable';
import { formatRelativeTime } from '../../../lib/time';
import { FindingsList } from '../../pins/PinFindings';
import { credentialRefusalText, splitEvidence } from '../../pins/pinApproval';
import { approveBlock, classifyUnifiedDiff, pathExplanation, provenanceLabel, type UnifiedLine } from './proposalModel';
import type { Proposal } from '../../../types';

interface ProposalDetailPanelProps {
  proposal: Proposal | null;
  onClose: () => void;
  onChanged: () => Promise<void> | void;
}

export function ProposalDetailPanel({ proposal, onClose, onChanged }: ProposalDetailPanelProps) {
  const id = proposal?.id ?? null;
  const [detail, setDetail] = useState<Proposal | null>(null);
  const [attempt, setAttempt] = useState(0);

  // The list rows carry the stored base_current and no pin_status; the
  // single GET refreshes both against the live stores, so gating waits for it.
  useEffect(() => {
    if (!id) return;
    let cancelled = false;
    fetchProposal(id)
      .then((full) => {
        if (!cancelled) setDetail(full);
      })
      .catch((err) => {
        if (!cancelled && !isCancellation(err)) {
          const described = describeError(err, 'Load proposal');
          showToast('error', described.text, { detail: described.fullDetail });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [id, attempt]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  if (!proposal) {
    return (
      <div className="h-full flex items-center justify-center text-xs text-text-muted p-6">
        Select a proposal to review it.
      </div>
    );
  }

  const shown = detail ?? proposal;
  return (
    <div className="h-full flex flex-col min-h-0">
      <InspectorHeader
        title={`${shown.kind}/${escapeNonPrintable(shown.target)}`}
        subtitle={`Proposal ${shown.id}`}
        icon={FileDiff}
        accent="secondary"
        onClose={onClose}
      />
      <div className="flex-1 overflow-auto p-4 flex flex-col gap-4">
        <ProposalSummary proposal={shown} refreshed={detail !== null} />
        <section aria-label="Diff" className="space-y-1">
          <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted/70">
            Diff{shown.files && shown.files.length > 0 ? ` (${shown.files.map(escapeNonPrintable).join(', ')})` : ''}
          </h3>
          <UnifiedDiffView diff={shown.diff} />
        </section>
        {(shown.findings?.length ?? 0) > 0 && (
          <section className="space-y-1.5">
            <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted/70">Findings (advisory)</h3>
            <FindingsList findings={shown.findings ?? []} />
          </section>
        )}
        <EvidenceList label="Evidence" items={shown.evidence ?? []} />
        <TextField label="Reason" value={shown.reason} />
        <TextField label="Source" value={shown.source} />
        {shown.verdict && <VerdictSection proposal={shown} />}
      </div>
      {shown.status === 'pending' && (
        <ProposalActions
          key={shown.id}
          proposal={shown}
          ready={detail !== null}
          onReload={reload}
          onChanged={onChanged}
        />
      )}
    </div>
  );
}

function ProposalSummary({ proposal, refreshed }: { proposal: Proposal; refreshed: boolean }) {
  const pinStatus = refreshed ? proposal.pin_status ?? 'unknown' : 'loading';
  return (
    <section aria-label="Summary" className="flex flex-col gap-2 text-xs">
      <p className="text-text-secondary">{escapeNonPrintable(pathExplanation(proposal))}</p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-2xs">
        <dt className="text-text-muted">Status</dt>
        <dd className="font-mono">{proposal.status}</dd>
        <dt className="text-text-muted">Classification</dt>
        <dd className="font-mono">{proposal.classification}</dd>
        <dt className="text-text-muted">Path</dt>
        <dd className="font-mono">{proposal.path}</dd>
        <dt className="text-text-muted">Provenance</dt>
        <dd className="font-mono break-all">{escapeNonPrintable(provenanceLabel(proposal))}</dd>
        <dt className="text-text-muted">Prevalence</dt>
        <dd>
          {proposal.prevalence.sessions} {proposal.prevalence.sessions === 1 ? 'session' : 'sessions'},{' '}
          {proposal.prevalence.runs} {proposal.prevalence.runs === 1 ? 'run' : 'runs'}
        </dd>
        <dt className="text-text-muted">Base</dt>
        <dd className={cn('font-mono', !proposal.base_current && 'text-status-pending')} title={proposal.base_hash}>
          {proposal.base_current ? 'current' : 'stale'} ({shortPinHash(proposal.base_hash)})
        </dd>
        <dt className="text-text-muted">Pin</dt>
        <dd className={cn('font-mono', pinStatus === 'drift' && 'text-status-pending')}>{pinStatus}</dd>
        <dt className="text-text-muted">Created</dt>
        <dd title={proposal.created_at}>
          {formatRelativeTime(new Date(proposal.created_at))}
          {proposal.created_by ? ` by ${escapeNonPrintable(proposal.created_by)}` : ''}
        </dd>
      </dl>
      {!proposal.base_current && (
        <p role="note" className="text-2xs text-status-pending">
          The target changed since the proposer read it. Approve refuses until the proposal is re-based.
        </p>
      )}
      {pinStatus === 'drift' && (
        <p role="note" className="text-2xs text-status-pending">
          The target has unreviewed pin drift. Review it in the Pins workspace
          {proposal.path === 'apply' ? ' before this proposal can be applied.' : '.'}
        </p>
      )}
    </section>
  );
}

// UnifiedDiffView renders the stored diff verbatim, one classified line at a
// time. Each line is escaped after splitting so hidden characters show.
function UnifiedDiffView({ diff }: { diff: string }) {
  const lines = classifyUnifiedDiff(diff);
  return (
    <div className="rounded border border-border/40 bg-background/60 overflow-x-auto max-h-96 overflow-y-auto scrollbar-dark">
      <pre className="text-2xs font-mono leading-relaxed p-2 whitespace-pre-wrap break-words" aria-label="Proposal diff">
        {lines.map((line, i) => (
          <UnifiedDiffLine key={i} line={line} />
        ))}
      </pre>
    </div>
  );
}

function UnifiedDiffLine({ line }: { line: UnifiedLine }) {
  return (
    <span
      data-kind={line.kind}
      className={cn(
        'block px-1',
        line.kind === 'added' && 'bg-status-running/10 text-status-running',
        line.kind === 'removed' && 'bg-status-error/10 text-status-error',
        line.kind === 'context' && 'text-text-secondary',
        line.kind === 'hunk' && 'text-primary/80',
        (line.kind === 'header' || line.kind === 'note') && 'text-text-muted',
      )}
    >
      {escapeNonPrintable(line.text) || ' '}
    </span>
  );
}

function EvidenceList({ label, items }: { label: string; items: string[] }) {
  if (items.length === 0) return null;
  return (
    <section className="space-y-1">
      <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted/70">{label}</h3>
      <ol className="list-decimal pl-5 text-2xs font-mono space-y-0.5">
        {items.map((ref, i) => (
          <li key={`${i}-${ref}`} className="break-all">{escapeNonPrintable(ref)}</li>
        ))}
      </ol>
    </section>
  );
}

function TextField({ label, value }: { label: string; value?: string }) {
  if (!value) return null;
  return (
    <section className="space-y-1">
      <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted/70">{label}</h3>
      <p className="text-2xs whitespace-pre-wrap break-words">{escapeNonPrintable(value)}</p>
    </section>
  );
}

function VerdictSection({ proposal }: { proposal: Proposal }) {
  const v = proposal.verdict;
  if (!v) return null;
  const outcome = proposal.status === 'rejected'
    ? 'Rejected'
    : v.applied
      ? 'Approved and applied'
      : 'Approved (verdict recorded; nothing was written)';
  return (
    <section aria-label="Verdict" className="space-y-1 rounded-lg border border-border-subtle p-3">
      <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted/70">Verdict</h3>
      <p className="text-xs">{outcome}</p>
      <p className="text-2xs text-text-muted" title={v.at}>
        {formatRelativeTime(new Date(v.at))}
        {v.by ? ` by ${escapeNonPrintable(v.by)}` : ''}
        {v.by_source ? ` (${escapeNonPrintable(v.by_source)})` : ''}
        {v.stack ? ` · stack ${escapeNonPrintable(v.stack)}` : ''}
      </p>
      {v.new_hash && (
        <p className="text-2xs font-mono text-text-muted" title={v.new_hash}>new hash {shortPinHash(v.new_hash)}</p>
      )}
      {v.reason && <p className="text-2xs whitespace-pre-wrap break-words">{escapeNonPrintable(v.reason)}</p>}
      {v.pin_error && (
        <p role="alert" className="text-2xs text-status-error">
          Applied, but re-pinning failed: {escapeNonPrintable(v.pin_error)}. Approve the drift in the Pins workspace.
        </p>
      )}
      <EvidenceList label="Verdict evidence" items={v.evidence ?? []} />
    </section>
  );
}

function ProposalActions({
  proposal,
  ready,
  onReload,
  onChanged,
}: {
  proposal: Proposal;
  ready: boolean;
  onReload: () => void;
  onChanged: () => Promise<void> | void;
}) {
  const reasonId = useId();
  const evidenceId = useId();
  const blockId = useId();
  const rejectId = useId();
  const [reason, setReason] = useState(proposal.reason ?? '');
  const [evidence, setEvidence] = useState('');
  const [inlineError, setInlineError] = useState('');
  const [busy, setBusy] = useState(false);
  const [rejecting, setRejecting] = useState(false);
  const [rejectReason, setRejectReason] = useState('');

  const findingsExist = (proposal.findings?.length ?? 0) > 0;
  const block = ready ? approveBlock(proposal, reason) : 'Loading the current base and pin status.';
  const approveDisabled = busy || block !== null;

  const approve = async () => {
    if (approveDisabled) return;
    setBusy(true);
    setInlineError('');
    try {
      const refs = splitEvidence(evidence);
      const res = await approveProposal(proposal.id, reason.trim() || undefined, refs);
      showToast(
        'success',
        res.applied
          ? `Approved ${proposal.id}: applied to ${proposal.kind} ${escapeNonPrintable(proposal.target)} and re-pinned`
          : `Approved ${proposal.id}: verdict recorded; nothing was written`,
      );
      onReload();
      await onChanged();
    } catch (err) {
      if (isCancellation(err)) return;
      if (err instanceof HTTPError && err.status === 409) {
        showToast('error', escapeNonPrintable(err.message));
        onReload();
        await onChanged();
      } else if (err instanceof HTTPError && err.status === 400) {
        setInlineError(credentialRefusalText(err) ?? describeError(err, 'Approve').text);
      } else if (err instanceof HTTPError && err.status === 422) {
        setInlineError(err.message);
      } else {
        const described = describeError(err, 'Approve');
        showToast('error', described.text, { detail: described.fullDetail });
        onReload();
        await onChanged();
      }
    } finally {
      setBusy(false);
    }
  };

  const reject = async () => {
    if (busy || rejectReason.trim() === '') return;
    setBusy(true);
    setInlineError('');
    try {
      await rejectProposal(proposal.id, rejectReason.trim());
      showToast('success', `Rejected ${proposal.id} (reason recorded). Nothing was written.`);
      setRejecting(false);
      onReload();
      await onChanged();
    } catch (err) {
      if (isCancellation(err)) return;
      if (err instanceof HTTPError && err.status === 409) {
        showToast('error', escapeNonPrintable(err.message));
        onReload();
        await onChanged();
      } else if (err instanceof HTTPError && err.status === 400) {
        setInlineError(credentialRefusalText(err) ?? describeError(err, 'Reject').text);
      } else {
        const described = describeError(err, 'Reject');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <footer className="flex-shrink-0 border-t border-border-subtle p-4 flex flex-col gap-2" aria-label="Review proposal">
      <label htmlFor={reasonId} className="text-2xs text-text-muted">
        Reason{findingsExist ? ' (required: advisory findings present)' : ' (optional)'}
      </label>
      <textarea
        id={reasonId}
        value={reason}
        onChange={(e) => setReason(e.target.value)}
        required={findingsExist}
        rows={2}
        className="text-xs rounded border border-border bg-background p-2"
      />
      <label htmlFor={evidenceId} className="text-2xs text-text-muted">
        Additional evidence references, one per line
      </label>
      <textarea
        id={evidenceId}
        value={evidence}
        onChange={(e) => setEvidence(e.target.value)}
        placeholder="https://example.test/pull/1"
        rows={2}
        className="text-xs rounded border border-border bg-background p-2"
      />
      {inlineError && (
        <p role="alert" className="text-2xs text-status-error break-words">{escapeNonPrintable(inlineError)}</p>
      )}
      {block && ready && (
        <p id={blockId} className="text-2xs text-text-muted">{block}</p>
      )}
      <div className="flex items-center gap-2">
        <Button
          size="sm"
          disabled={approveDisabled}
          aria-busy={busy}
          aria-describedby={block && ready ? blockId : undefined}
          title={block ?? 'Approve this proposal'}
          onClick={() => void approve()}
        >
          {busy && !rejecting ? 'Approving…' : 'Approve'}
        </Button>
        <Button size="sm" variant="secondary" disabled={busy} onClick={() => setRejecting(true)}>
          Reject
        </Button>
      </div>
      {rejecting && (
        <div role="dialog" aria-label="Reject proposal" className="rounded-lg border border-status-error/30 p-3 flex flex-col gap-2">
          <label htmlFor={rejectId} className="text-2xs text-text-muted">Rejection reason (required)</label>
          <input
            id={rejectId}
            autoFocus
            value={rejectReason}
            onChange={(e) => setRejectReason(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void reject();
              if (e.key === 'Escape') setRejecting(false);
            }}
            className="text-xs rounded border border-border bg-background px-2 py-1"
          />
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline-danger"
              disabled={busy || rejectReason.trim() === ''}
              onClick={() => void reject()}
            >
              Confirm reject
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setRejecting(false)}>Cancel</Button>
          </div>
        </div>
      )}
    </footer>
  );
}
