import { useEffect, useMemo, useState } from 'react';
import { Clock, FileText, Lock, LockOpen } from 'lucide-react';
import { cn } from '../../lib/cn';
import {
  approveContextPin,
  fetchContextPinDiff,
  fetchContextPins,
  HTTPError,
  type ContextPin,
  type ContextPinsDiff,
} from '../../lib/api';
import { usePinsStore } from '../../stores/usePinsStore';
import { escapeNonPrintable } from '../../lib/nonPrintable';
import { formatRelativeTime } from '../../lib/time';
import { describeError, isCancellation } from '../../lib/describeError';
import { pinStatusMeta } from './pinStatus';
import { FindingsList } from './PinFindings';
import {
  APPROVE_ARIA_KEYSHORTCUTS,
  APPROVE_BUTTON_ID,
  DRIFT_SECTION_ID,
} from './PinsDriftSection';
import { TOOLS_SECTION_ID } from './PinsServerDetail';
import { DocumentDiff } from './DocumentDiff';
import { PinDangerZone } from './PinDangerZone';
import { ShortcutKeys } from '../ui/ShortcutKeys';
import { PINS_APPROVE_KEYS } from '../../lib/shortcuts';
import { showToast } from '../ui/Toast';
import { ApprovalFields, PinApprovalHistory } from './PinApprovalHistory';
import { approvalRisk, credentialRefusalText, splitEvidence } from './pinApproval';
import { ApprovalRiskNotes } from './ApprovalRiskNotes';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { diffLines, MAX_DIFF_LINES } from '../../lib/diff';

// ---------------------------------------------------------------------------
// Context detail - fragment pin drift review (line diff) + pinned document
//
// Follows PinsSkillDetail and shares the server, skill, and context ids. The
// detail also mounts in the Library Context segment's pin review modal, so
// ids are opt-in (anchors) to keep them unique in the document.
// ---------------------------------------------------------------------------

interface PinsContextDetailProps {
  name: string;
  pin: ContextPin;
  // May reject; the detail reports the failure.
  onReset: (name: string) => Promise<void>;
  anchors?: boolean;
}

export function PinsContextDetail({ name, pin, onReset, anchors = true }: PinsContextDetailProps) {
  const { label, colorClass } = pinStatusMeta(pin.status);
  const hasDrift = pin.status === 'drift';
  // Fragment names come from the server; everything rendered uses this.
  const shown = escapeNonPrintable(name);
  const source =
    pin.source === 'pack' && pin.pack
      ? `pack: ${escapeNonPrintable(pin.pack)}`
      : pin.source === 'local'
        ? 'Local'
        : 'Context fragment';

  const handleReset = async () => {
    try {
      await onReset(name);
      showToast('success', `Pin reset for ${shown}; it re-pins on the next context sync`);
    } catch (err) {
      if (!isCancellation(err)) showToast('error', describeError(err, 'Reset').text);
    }
  };

  return (
    <div className={cn('px-6 py-4 space-y-4', hasDrift ? 'max-w-5xl' : 'max-w-3xl')}>
      <div className="flex items-center gap-3">
        <h2 className="text-sm font-mono text-text-primary">{shown}</h2>
        <span className={cn('flex items-center gap-1.5 text-xs', colorClass)}>
          {hasDrift ? <LockOpen size={11} /> : <Lock size={11} />}
          {hasDrift ? 'Pin drift' : label}
        </span>
        <span
          className="flex items-center gap-1 text-2xs text-text-muted ml-auto"
          title={pin.last_verified_at || undefined}
        >
          <Clock size={10} className="text-text-muted" />
          {pin.last_verified_at
            ? `verified ${formatRelativeTime(new Date(pin.last_verified_at))}`
            : 'never verified'}
        </span>
      </div>

      <div className="flex items-center gap-4 text-2xs text-text-muted flex-wrap">
        <span className="flex items-center gap-1">
          <FileText size={10} className="text-text-muted" />
          {source}
        </span>
        {pin.pinned_at && (
          <span title={pin.pinned_at}>
            first pinned {formatRelativeTime(new Date(pin.pinned_at))}
          </span>
        )}
        {pin.approved_reason && (
          <span title={escapeNonPrintable(pin.approved_reason)} className="truncate max-w-xs">
            approved with reason: {escapeNonPrintable(pin.approved_reason)}
          </span>
        )}
      </div>

      <PinApprovalHistory approvals={pin.approvals} />

      {hasDrift && <ContextDriftSection name={name} shown={shown} anchors={anchors} />}

      <div id={anchors ? TOOLS_SECTION_ID : undefined} className="space-y-4">
        {!hasDrift && (pin.findings?.length ?? 0) > 0 && (
          <section className="space-y-2 max-w-3xl">
            <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted">
              Advisory findings on the pinned content
            </h3>
            <FindingsList findings={pin.findings} />
          </section>
        )}

        <section className="space-y-2 max-w-3xl">
          {hasDrift ? (
            // The diff above already shows this text; keep it out of the way.
            <details>
              <summary className="cursor-pointer text-3xs uppercase tracking-[0.18em] text-text-muted">
                Pinned document
              </summary>
              <PinnedDocument text={pin.document ?? ''} />
            </details>
          ) : (
            <>
              <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted">
                Pinned document
              </h3>
              <PinnedDocument text={pin.document ?? ''} />
            </>
          )}
        </section>
      </div>

      <PinDangerZone
        buttonLabel={`Reset pin for ${shown}`}
        hint="Deletes the pin record; the fragment re-pins on the next context sync."
        dialogTitle="Reset fragment pin"
        message={
          <div className="space-y-2">
            <p>
              This deletes the pin record for{' '}
              <span className="font-mono text-text-secondary">{shown}</span>, including its
              pinned document, findings, and approval reason. The approval history log is kept.
            </p>
            <p>
              On the next context sync the fragment re-pins from scratch, trusting whatever
              content is on disk at that moment without a diff to review. This cannot be undone.
            </p>
          </div>
        }
        onReset={handleReset}
        anchor={anchors}
      />
    </div>
  );
}

function PinnedDocument({ text }: { text: string }) {
  return (
    <div className="mt-2 rounded border border-border/40 bg-background/60 max-h-96 overflow-auto scrollbar-dark">
      <pre className="text-2xs font-mono leading-relaxed p-2 whitespace-pre-wrap break-words text-text-secondary">
        {escapeNonPrintable(text)}
      </pre>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Drift section: line diff + findings + approve
// ---------------------------------------------------------------------------

function ContextDriftSection({
  name,
  shown,
  anchors,
}: {
  name: string;
  shown: string;
  anchors: boolean;
}) {
  const [diff, setDiff] = useState<ContextPinsDiff | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  const [degraded, setDegraded] = useState(false);
  const [isApproving, setIsApproving] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [reason, setReason] = useState('');
  const [evidence, setEvidence] = useState('');

  // The detail is keyed on the fragment name, so a name change remounts this
  // section; attempt bumps drive Retry and the post-approve reload.
  useEffect(() => {
    let cancelled = false;
    fetchContextPinDiff(name)
      .then((d) => {
        if (!cancelled) {
          setDiff(d);
          setLoadError(null);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setLoadError(err instanceof Error ? err.message : '');
      });
    return () => {
      cancelled = true;
    };
  }, [name, attempt]);

  // Unlike the skill section, a reload clears the old diff: the content moved
  // under it, and it must not stay on screen as if it were still current.
  const reloadDiff = () => {
    setDiff(null);
    setLoadError(null);
    setDegraded(false);
    setAttempt((n) => n + 1);
  };

  // The server requires a reason for any finding, including info.
  const needsReason = diff !== null && diff.findings.length > 0;
  const documentChanged =
    diff !== null && (diff.old_document ?? '') !== (diff.new_document ?? '');
  const currentDocument = diff?.new_document ?? '';

  const tokens = useMemo(
    () => (documentChanged && diff ? diffLines(diff.old_document ?? '', currentDocument) : []),
    [documentChanged, diff, currentDocument],
  );
  const risk = approvalRisk({
    findings: diff?.findings,
    truncated: tokens.length > MAX_DIFF_LINES,
    changedLines: tokens.filter((t) => t.kind !== 'same').length,
  });

  const doApprove = async () => {
    // The in-flight guard lives here rather than in `disabled`, so a repeated
    // click, Ctrl/Cmd+Enter, or palette command during an approve is a no-op
    // while the button keeps focus.
    if (!diff || isApproving) return;
    setIsApproving(true);
    try {
      const refs = splitEvidence(evidence);
      const reasonText = reason.trim() || undefined;
      if (refs.length > 0) {
        await approveContextPin(name, diff.composite_hash, reasonText, refs);
      } else {
        await approveContextPin(name, diff.composite_hash, reasonText);
      }
      const updated = await fetchContextPins();
      usePinsStore.getState().setContextPins(updated);
      showToast('success', `Approved content update for ${shown}`);
    } catch (err) {
      if (isCancellation(err)) return;
      if (err instanceof HTTPError && err.status === 409) {
        showToast('error', `${describeError(err, 'Approve').text} Reloading the diff for re-review.`);
        reloadDiff();
      } else if (err instanceof HTTPError && err.status === 400) {
        const refused = credentialRefusalText(err);
        if (refused) {
          showToast('error', refused, { detail: refused });
        } else {
          const described = describeError(err, 'Approve');
          showToast('error', described.text, { detail: described.fullDetail });
        }
      } else {
        const described = describeError(err, 'Approve');
        showToast('error', described.text, { detail: described.fullDetail });
        reloadDiff();
      }
    } finally {
      setIsApproving(false);
    }
  };

  const handleApproveClick = () => {
    if (!diff || isApproving) return;
    if (risk.needsConfirm) {
      setConfirmOpen(true);
    } else {
      void doApprove();
    }
  };

  return (
    <>
    <section
      id={anchors ? DRIFT_SECTION_ID : undefined}
      className="rounded-lg border border-status-pending/30 bg-status-pending/5 p-4 space-y-3"
    >
      <div className="flex items-center gap-3 flex-wrap">
        <h3 className="text-xs font-medium text-status-pending">Pin drift</h3>
        <div className="ml-auto flex items-start gap-2">
          <ApprovalFields
            reason={reason}
            onReason={setReason}
            evidence={evidence}
            onEvidence={setEvidence}
            reasonRequired={needsReason}
          />
          <button
            id={anchors ? APPROVE_BUTTON_ID : undefined}
            onClick={handleApproveClick}
            disabled={diff === null || (needsReason && reason.trim() === '')}
            aria-busy={isApproving}
            aria-keyshortcuts={anchors ? APPROVE_ARIA_KEYSHORTCUTS : undefined}
            title={
              needsReason
                ? 'Approving over unresolved advisory findings requires a reason'
                : 'Re-pin the current content'
            }
            className="h-6 px-2.5 text-2xs font-medium rounded bg-status-running/15 text-status-running border border-status-running/30 hover:bg-status-running/25 transition-colors disabled:opacity-50"
          >
            {isApproving ? 'Approving…' : 'Approve'}
          </button>
          {anchors && (
            // The chord dispatches through APPROVE_BUTTON_ID, so only an
            // anchored mount advertises it.
            <span aria-hidden="true" title="Ctrl or Cmd + Enter approves" className="h-6 flex items-center">
              <ShortcutKeys keys={PINS_APPROVE_KEYS} />
            </span>
          )}
        </div>
      </div>

      {needsReason && (
        <p className="text-2xs text-status-pending">
          This fragment carries advisory findings; a reason is required
        </p>
      )}

      {loadError !== null && (
        <div role="alert" className="flex items-center gap-3 text-2xs text-status-error">
          <span>
            Failed to load the pin diff for {shown}.{loadError ? ` ${escapeNonPrintable(loadError)}` : ''}
          </span>
          <button
            onClick={reloadDiff}
            className="h-6 px-2 font-medium rounded border border-status-error/30 bg-status-error/10 hover:bg-status-error/20 transition-colors"
          >
            Retry
          </button>
        </div>
      )}

      {diff === null && loadError === null && (
        <p className="text-2xs text-text-muted">Loading diff…</p>
      )}

      {diff && (
        <>
          {documentChanged ? (
            <DocumentDiff
              // DocumentDiff escapes its label; passing `shown` would escape twice.
              label={name}
              oldDocument={diff.old_document ?? ''}
              newDocument={currentDocument}
              tokens={tokens}
              fullDiffCommand={`gridctl ctx pins diff ${name}`}
              onDegraded={setDegraded}
            />
          ) : (
            <p className="text-2xs text-text-muted">
              The current content matches the pinned document. Approving records the review and
              clears the drift status.
            </p>
          )}

          {degraded && documentChanged && (
            <details>
              <summary className="cursor-pointer text-3xs uppercase tracking-[0.18em] text-text-muted">
                Full current document ({currentDocument.split('\n').length} lines)
              </summary>
              <PinnedDocument text={currentDocument} />
            </details>
          )}

          {diff.findings.length > 0 && (
            <div className="space-y-1.5">
              <h4 className="text-3xs uppercase tracking-[0.18em] text-text-muted">
                Advisory findings on the new content
              </h4>
              <FindingsList findings={diff.findings} />
            </div>
          )}
        </>
      )}
    </section>

    {/* display:contents so the parent's space-y margin cannot offset the
        fixed-position backdrop. */}
    <div className="contents">
      <ConfirmDialog
        isOpen={confirmOpen}
        onClose={() => setConfirmOpen(false)}
        onConfirm={() => {
          setConfirmOpen(false);
          void doApprove();
        }}
        title={`Approve ${shown}`}
        confirmLabel="Approve"
        autoFocus="cancel"
        message={
          <div className="space-y-2">
            <p>
              This re-pins the current content of{' '}
              <span className="font-mono text-text-secondary">{shown}</span>, which is injected
              into every linked client.
            </p>
            <ApprovalRiskNotes risk={risk} fullDiffCommand={`gridctl ctx pins diff ${name}`} />
          </div>
        }
      />
    </div>
    </>
  );
}
