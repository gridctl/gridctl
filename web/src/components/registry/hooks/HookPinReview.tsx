import { useEffect, useState } from 'react';
import { Button } from '../../ui/Button';
import { showToast } from '../../ui/Toast';
import { approveHookPin, fetchHookPinDiff, fetchHookPins, HTTPError } from '../../../lib/api';
import { cn } from '../../../lib/cn';
import { describeError, isCancellation } from '../../../lib/describeError';
import { diffLines, MAX_DIFF_LINES, type DiffToken } from '../../../lib/diff';
import { escapeNonPrintable } from '../../../lib/nonPrintable';
import { usePinsStore } from '../../../stores/usePinsStore';
import { FindingsList } from '../../pins/PinFindings';
import { APPROVE_BUTTON_ID, DRIFT_SECTION_ID } from '../../pins/PinsDriftSection';
import { credentialRefusalText, splitEvidence } from '../../pins/pinApproval';
import type { HookPinDiff } from '../../../types';

interface HookPinReviewProps {
  name: string;
  onRefresh: () => Promise<void> | void;
  // The Pins workspace mount carries the shared drift anchors and the
  // evidence input. The Library Pin tab leaves them out so the ids stay
  // unique to the Pins route.
  inPinsWorkspace?: boolean;
  // The registry directory, named in the note that script contents are
  // not shown. The Library Pin tab passes it from the hook detail.
  hookDir?: string;
}

// HookPinReview is the single hook pin review, mounted by the Library Pin
// tab and the Pins workspace hook detail. The diff endpoint verifies live,
// so its status is authoritative over the stored list status.
export function HookPinReview({ name, onRefresh, inPinsWorkspace = false, hookDir }: HookPinReviewProps) {
  const [diff, setDiff] = useState<HookPinDiff | null>(null);
  const [notPinned, setNotPinned] = useState(false);
  const [loadError, setLoadError] = useState('');
  const [reason, setReason] = useState('');
  const [evidence, setEvidence] = useState('');
  const [busy, setBusy] = useState(false);
  const [attempt, setAttempt] = useState(0);

  // A reload (attempt bump) keeps the previous diff visible until the fresh
  // one lands rather than flashing the loading state.
  useEffect(() => {
    let cancelled = false;
    fetchHookPinDiff(name)
      .then((next) => {
        if (cancelled) return;
        setNotPinned(false);
        setLoadError('');
        setDiff(next);
      })
      .catch((err) => {
        if (cancelled || isCancellation(err)) return;
        setDiff(null);
        if (err instanceof HTTPError && err.status === 404) {
          setNotPinned(true);
          setLoadError('');
          return;
        }
        setNotPinned(false);
        setLoadError(describeError(err, 'Load hook pin diff').text);
      });
    return () => {
      cancelled = true;
    };
  }, [name, attempt]);

  const reloadDiff = () => setAttempt((n) => n + 1);

  const findings = diff?.findings ?? [];
  const findingsExist = findings.length > 0;
  const expectHash = diff?.composite_hash ?? '';
  const approveDisabled = busy || notPinned || loadError !== '' || expectHash === '' || (findingsExist && reason.trim() === '');
  const oldDocument = diff?.old_document ?? '';
  const newDocument = diff?.new_document ?? '';
  const documentChanged = diff !== null && oldDocument !== newDocument;

  const approve = async () => {
    if (approveDisabled) return;
    setBusy(true);
    try {
      const refs = inPinsWorkspace ? splitEvidence(evidence) : [];
      if (refs.length > 0) {
        await approveHookPin(name, expectHash, reason.trim(), refs);
      } else {
        await approveHookPin(name, expectHash, reason.trim());
      }
    } catch (err) {
      setBusy(false);
      if (isCancellation(err)) return;
      if (err instanceof HTTPError && err.status === 409) {
        showToast('error', `${describeError(err, 'Approve hook pin').text} Reloading the diff for re-review.`);
        reloadDiff();
      } else if (err instanceof HTTPError && err.status === 400) {
        // An input refusal: keep the typed reason and evidence for correction.
        const refused = credentialRefusalText(err);
        if (refused) {
          showToast('error', refused, { detail: refused });
        } else {
          const described = describeError(err, 'Approve hook pin');
          showToast('error', described.text, { detail: described.fullDetail });
        }
      } else {
        const described = describeError(err, 'Approve hook pin');
        showToast('error', described.text, { detail: described.fullDetail });
        reloadDiff();
      }
      return;
    }

    showToast('success', 'Hook pin approved');
    setReason('');
    setEvidence('');
    reloadDiff();
    // The approve already landed; a failed list refresh waits for the next
    // poll instead of reporting the approve as failed.
    try {
      usePinsStore.getState().setHookPins(await fetchHookPins());
    } catch {
      // The next inventory poll retries.
    }
    try {
      await onRefresh();
    } catch {
      // onRefresh owns its own failure reporting.
    }
    setBusy(false);
  };

  const statusLabel = notPinned ? 'not pinned' : diff?.status || (loadError ? 'unavailable' : 'loading');

  return (
    <div id={inPinsWorkspace ? DRIFT_SECTION_ID : undefined} className="flex flex-col gap-2">
      <p className="text-xs">Pin status: <span className="font-mono">{statusLabel}</span></p>
      {notPinned && (
        <p className="text-xs text-text-muted">Not pinned; the next accepted import pins it</p>
      )}
      {loadError && <p className="text-xs text-status-error">{loadError}</p>}
      {findingsExist && (
        <section className="space-y-2 max-w-3xl">
          <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted">
            Advisory findings on the pinned content
          </h3>
          <FindingsList findings={findings} />
        </section>
      )}
      {diff && (
        <>
          <HookFileChangeList label="Added" paths={diff.added_files} glyph="+" tone="text-status-running" />
          <HookFileChangeList label="Removed" paths={diff.removed_files} glyph="-" tone="text-status-error" />
          <HookFileChangeList label="Modified" paths={diff.modified_files} glyph="~" tone="text-status-pending" />
          {(diff.added_files.length > 0 || diff.modified_files.length > 0) && (
            <p className="text-2xs text-text-muted" data-testid="hook-script-note">
              Script contents are not shown here.{' '}
              {hookDir ? (
                <>
                  Read the changed files under{' '}
                  <span className="font-mono break-all">{escapeNonPrintable(hookDir)}</span> before approving.
                </>
              ) : (
                "Read the changed files in the hook's registry directory before approving."
              )}
            </p>
          )}
          {documentChanged ? (
            <HookDocumentDiff oldDocument={oldDocument} newDocument={newDocument} />
          ) : (
            <p className="text-2xs text-text-muted">hook.json unchanged</p>
          )}
        </>
      )}
      <label className="text-2xs text-text-muted" htmlFor="hook-pin-reason">
        Reason{findingsExist ? ' (required when findings exist)' : ''}
      </label>
      <textarea
        id="hook-pin-reason"
        aria-label="Approve reason"
        required={findingsExist}
        disabled={notPinned || loadError !== ''}
        value={reason}
        onChange={(e) => setReason(e.target.value)}
        className="text-xs rounded border border-border bg-background p-2 min-h-16"
      />
      {inPinsWorkspace && (
        <label className="flex flex-col gap-1 text-2xs text-text-muted">
          Evidence references, one per line
          <textarea
            aria-label="Evidence references, one per line"
            disabled={notPinned || loadError !== ''}
            value={evidence}
            onChange={(e) => setEvidence(e.target.value)}
            placeholder="https://example.test/pull/1"
            rows={2}
            className="text-xs rounded border border-border bg-background p-2"
          />
        </label>
      )}
      <Button
        id={inPinsWorkspace && diff?.status === 'drift' ? APPROVE_BUTTON_ID : undefined}
        size="sm"
        disabled={approveDisabled}
        onClick={() => void approve()}
      >
        Approve
      </Button>
    </div>
  );
}

function HookFileChangeList({
  label,
  paths,
  glyph,
  tone,
}: {
  label: string;
  paths: string[];
  glyph: string;
  tone: string;
}) {
  if (paths.length === 0) return null;
  return (
    <div className="text-2xs font-mono space-y-0.5" aria-label={`${label} files`}>
      {paths.map((p) => (
        <div key={p} className={cn('flex gap-1.5', tone)}>
          <span>{glyph}</span>
          <span className="break-all">{escapeNonPrintable(p)}</span>
        </div>
      ))}
    </div>
  );
}

function HookDocumentDiff({ oldDocument, newDocument }: { oldDocument: string; newDocument: string }) {
  const tokens = diffLines(oldDocument, newDocument);
  const changed = tokens.filter((t) => t.kind !== 'same').length;
  const shown = tokens.slice(0, MAX_DIFF_LINES);
  return (
    <div className="space-y-1">
      <h4 className="text-3xs uppercase tracking-[0.18em] text-text-muted">
        hook.json ({changed} {changed === 1 ? 'line' : 'lines'} changed)
      </h4>
      <div className="rounded border border-border/40 bg-background/60 overflow-x-auto max-h-96 overflow-y-auto scrollbar-dark" aria-label="Hook pin diff">
        <pre className="text-2xs font-mono leading-relaxed p-2 whitespace-pre-wrap break-words">
          {shown.map((token, i) => (
            <HookDiffLine key={i} token={token} />
          ))}
        </pre>
      </div>
      {tokens.length > MAX_DIFF_LINES && (
        <p className="text-3xs text-text-muted">Diff truncated at {MAX_DIFF_LINES} lines.</p>
      )}
    </div>
  );
}

function HookDiffLine({ token }: { token: DiffToken }) {
  const prefix = token.kind === 'added' ? '+ ' : token.kind === 'removed' ? '- ' : '  ';
  return (
    <span
      className={cn(
        'block px-1',
        token.kind === 'added' && 'bg-status-running/10 text-status-running',
        token.kind === 'removed' && 'bg-status-error/10 text-status-error',
        token.kind === 'same' && 'text-text-secondary',
      )}
    >
      {prefix}
      {escapeNonPrintable(token.text)}
    </span>
  );
}
