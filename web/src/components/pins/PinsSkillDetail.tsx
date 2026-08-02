import { useEffect, useState } from 'react';
import { Clock, GitBranch, Lock, LockOpen, Trash2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import {
  approveSkillPin,
  fetchSkillPinDiff,
  fetchSkillPins,
  HTTPError,
  type SkillPin,
  type SkillPinsDiff,
} from '../../lib/api';
import { usePinsStore } from '../../stores/usePinsStore';
import { diffLines, MAX_DIFF_LINES, type DiffToken } from '../../lib/diff';
import { escapeNonPrintable, shortPinHash } from '../../lib/nonPrintable';
import { formatRelativeTime } from '../../lib/time';
import { pinStatusMeta } from './pinStatus';
import { FindingsList } from './PinFindings';
import { APPROVE_BUTTON_ID, DRIFT_SECTION_ID } from './PinsDriftSection';
import { RESET_BUTTON_ID, TOOLS_SECTION_ID } from './PinsServerDetail';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { showToast } from '../ui/Toast';

// ---------------------------------------------------------------------------
// Skill detail - pin drift review (prose diff + file changes) + pinned files
//
// Mirrors PinsServerDetail/DriftSection for the skill kind. The button and
// section ids are shared with the server components on purpose: only one
// kind's pane is mounted at a time, so ?view= scrolling and the command
// palette's clickById dispatch keep working across the kind toggle.
// ---------------------------------------------------------------------------

interface PinsSkillDetailProps {
  name: string;
  pin: SkillPin;
  onReset: (skillName: string) => Promise<void>;
}

export function PinsSkillDetail({ name, pin, onReset }: PinsSkillDetailProps) {
  const { label, colorClass } = pinStatusMeta(pin.status);
  const hasDrift = pin.status === 'drift';
  const [resetOpen, setResetOpen] = useState(false);
  const [isResetting, setIsResetting] = useState(false);
  const files = pin.files ?? [];

  const handleResetConfirm = async () => {
    setResetOpen(false);
    setIsResetting(true);
    try {
      await onReset(name);
    } finally {
      setIsResetting(false);
    }
  };

  return (
    <div className={cn('px-6 py-4 space-y-4', hasDrift ? 'max-w-5xl' : 'max-w-3xl')}>
      <div className="flex items-center gap-3">
        <h2 className="text-sm font-mono text-text-primary">{name}</h2>
        <span className={cn('flex items-center gap-1.5 text-xs', colorClass)}>
          {hasDrift ? <LockOpen size={11} /> : <Lock size={11} />}
          {hasDrift ? 'Pin drift' : label}
        </span>
        <span
          className="flex items-center gap-1 text-[11px] text-text-muted ml-auto"
          title={pin.last_verified_at || undefined}
        >
          <Clock size={10} className="text-text-muted/60" />
          {pin.last_verified_at
            ? `verified ${formatRelativeTime(new Date(pin.last_verified_at))}`
            : 'never verified'}
        </span>
      </div>

      <div className="flex items-center gap-4 text-[11px] text-text-muted flex-wrap">
        {/* Origin is factual provenance, never a trust judgment. */}
        <span className="flex items-center gap-1" title={pin.origin?.commitSha || undefined}>
          <GitBranch size={10} className="text-text-muted/60" />
          {pin.source === 'git' && pin.origin?.repo
            ? `Imported: ${pin.origin.repo}${pin.origin.ref ? `@${pin.origin.ref}` : ''}`
            : 'Local'}
        </span>
        <span>
          <span className="text-text-secondary font-medium">{files.length}</span> supporting{' '}
          {files.length === 1 ? 'file' : 'files'} pinned
        </span>
        {pin.pinned_at && (
          <span title={pin.pinned_at}>
            first pinned {formatRelativeTime(new Date(pin.pinned_at))}
          </span>
        )}
        {pin.approved_reason && (
          <span title={pin.approved_reason} className="truncate max-w-xs">
            approved with reason: {escapeNonPrintable(pin.approved_reason)}
          </span>
        )}
      </div>

      {hasDrift && <SkillDriftSection skillName={name} />}

      {!hasDrift && (pin.findings?.length ?? 0) > 0 && (
        <section className="space-y-2 max-w-3xl">
          <h3 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">
            Advisory findings on the pinned content
          </h3>
          <FindingsList findings={pin.findings} />
        </section>
      )}

      <section id={TOOLS_SECTION_ID} className="space-y-2 max-w-3xl">
        <h3 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">
          Pinned file digests
        </h3>
        <div className="rounded-lg border border-border/40 bg-background/60 overflow-hidden">
          <table className="w-full text-xs border-collapse">
            <thead>
              <tr className="border-b border-border/30">
                <th className="text-left px-3 py-2 text-text-muted font-medium">File</th>
                <th className="text-left px-3 py-2 text-text-muted font-medium">Digest</th>
              </tr>
            </thead>
            <tbody>
              <tr className="border-b border-border/20">
                <td className="px-3 py-2 font-mono text-text-primary">SKILL.md</td>
                <td className="px-3 py-2 font-mono text-text-muted" title={pin.skill_hash}>
                  {shortPinHash(pin.skill_hash)}
                </td>
              </tr>
              {files.map((f) => (
                <tr key={f.path} className="border-b border-border/20 last:border-b-0">
                  <td className="px-3 py-2 font-mono text-text-primary">
                    {escapeNonPrintable(f.path)}
                  </td>
                  <td className="px-3 py-2 font-mono text-text-muted" title={f.digest}>
                    {shortPinHash(f.digest)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="pt-4 mt-2 border-t border-border/30 space-y-2 max-w-3xl">
        <h3 className="text-[10px] uppercase tracking-[0.18em] text-status-error/70">Danger</h3>
        <div className="flex items-center gap-3">
          <button
            id={RESET_BUTTON_ID}
            onClick={() => {
              if (!isResetting) setResetOpen(true);
            }}
            aria-busy={isResetting}
            className={cn(
              'flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-medium text-status-error bg-status-error/10 border border-status-error/20 hover:bg-status-error/20 transition-colors',
              isResetting && 'opacity-50',
            )}
          >
            <Trash2 size={11} />
            {isResetting ? 'Resetting…' : `Reset pin for ${name}`}
          </button>
          <p className="text-[11px] text-text-muted">
            Deletes the pin record; the skill re-pins on the next registry refresh.
          </p>
        </div>
      </section>

      <div className="contents">
        <ConfirmDialog
          isOpen={resetOpen}
          onClose={() => setResetOpen(false)}
          onConfirm={() => void handleResetConfirm()}
          title="Reset skill pin"
          variant="danger"
          confirmLabel={`Reset pin for ${name}`}
          message={
            <div className="space-y-2">
              <p>
                This deletes the pin record for{' '}
                <span className="font-mono text-text-secondary">{name}</span>, including its
                digest set, findings history, and any approval reason.
              </p>
              <p>
                On the next registry refresh the skill re-pins from scratch, trusting whatever
                content is on disk at that moment without a diff to review. This cannot be undone.
              </p>
            </div>
          }
        />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Drift section: summary + prose diff + file changes + findings + approve
// ---------------------------------------------------------------------------

function SkillDriftSection({ skillName }: { skillName: string }) {
  const [diff, setDiff] = useState<SkillPinsDiff | null>(null);
  const [loadError, setLoadError] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [isApproving, setIsApproving] = useState(false);
  const [reason, setReason] = useState('');

  // No synchronous reset here: the section is remounted per skill (the
  // workspace keys the detail on the active name), so diff starts null; a
  // reload (attempt bump) keeps the stale diff visible until the fresh one
  // lands rather than flashing the loading state.
  useEffect(() => {
    let cancelled = false;
    fetchSkillPinDiff(skillName)
      .then((d) => {
        if (!cancelled) {
          setDiff(d);
          setLoadError(false);
        }
      })
      .catch(() => {
        if (!cancelled) setLoadError(true);
      });
    return () => {
      cancelled = true;
    };
  }, [skillName, attempt]);

  const reloadDiff = () => setAttempt((n) => n + 1);

  // The backend requires a reason exactly when the current content carries
  // unresolved advisory findings; surface the input up front instead of
  // bouncing the user off a 400. The diff endpoint always sends findings
  // (non-nil on the wire), so no optional chaining past the diff itself.
  const needsReason = diff !== null && diff.findings.length > 0;
  const documentChanged =
    diff !== null && (diff.old_document ?? '') !== (diff.new_document ?? '');

  const doApprove = async () => {
    // The in-flight guard lives here rather than in `disabled`: the
    // workspace's Enter binding clicks this button programmatically, and a
    // disabled button would swallow the programmatic click silently.
    if (!diff || isApproving) return;
    setIsApproving(true);
    try {
      await approveSkillPin(skillName, diff.composite_hash, reason.trim() || undefined);
      const updated = await fetchSkillPins();
      usePinsStore.getState().setSkillPins(updated);
      showToast('success', `Approved content update for ${skillName}`);
    } catch (err) {
      if (err instanceof HTTPError && err.status === 409) {
        showToast('error', `${err.message} — reloading the diff for re-review`);
        reloadDiff();
      } else if (err instanceof HTTPError && err.status === 400) {
        showToast('error', err.message);
      } else {
        showToast(
          'error',
          `Failed to approve: ${err instanceof Error ? err.message : 'Unknown error'}`,
        );
        reloadDiff();
      }
    } finally {
      setIsApproving(false);
    }
  };

  return (
    <section
      id={DRIFT_SECTION_ID}
      className="rounded-lg border border-status-pending/30 bg-status-pending/5 p-4 space-y-3"
    >
      <div className="flex items-center gap-3 flex-wrap">
        <h3 className="text-xs font-medium text-status-pending">Pin drift</h3>
        {diff && (
          <span className="text-[11px] text-text-muted">{driftSummary(diff, documentChanged)}</span>
        )}
        <div className="ml-auto flex items-center gap-2">
          {needsReason && (
            <input
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="Reason (required: advisory findings present)"
              aria-label="Approval reason"
              className="h-6 w-64 px-2 text-[11px] rounded border border-border/40 bg-background/60 text-text-primary placeholder:text-text-muted/60"
            />
          )}
          <button
            id={APPROVE_BUTTON_ID}
            onClick={() => void doApprove()}
            disabled={diff === null || loadError || (needsReason && reason.trim() === '')}
            aria-busy={isApproving}
            title={
              needsReason
                ? 'Approving over unresolved advisory findings requires a reason'
                : 'Re-pin the current content'
            }
            className="h-6 px-2.5 text-[11px] font-medium rounded bg-status-running/15 text-status-running border border-status-running/30 hover:bg-status-running/25 transition-colors disabled:opacity-50"
          >
            {isApproving ? 'Approving…' : 'Approve'}
          </button>
        </div>
      </div>

      {loadError && (
        <div role="alert" className="flex items-center gap-3 text-[11px] text-status-error">
          <span>Failed to load the pin diff for {skillName}.</span>
          <button
            onClick={reloadDiff}
            className="h-6 px-2 font-medium rounded border border-status-error/30 bg-status-error/10 hover:bg-status-error/20 transition-colors"
          >
            Retry
          </button>
        </div>
      )}

      {diff === null && !loadError && (
        <p className="text-[11px] text-text-muted">Loading diff…</p>
      )}

      {diff && (
        <>
          <FileChangeList label="Added" paths={diff.added_files} glyph="+" tone="text-status-running" />
          <FileChangeList label="Removed" paths={diff.removed_files} glyph="-" tone="text-status-error" />
          <FileChangeList label="Modified" paths={diff.modified_files} glyph="~" tone="text-status-pending" />

          {documentChanged && (
            <SkillDocumentDiff
              oldDocument={diff.old_document ?? ''}
              newDocument={diff.new_document ?? ''}
            />
          )}

          {diff.findings.length > 0 && (
            <div className="space-y-1.5">
              <h4 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">
                Advisory findings on the new content
              </h4>
              <FindingsList findings={diff.findings} />
            </div>
          )}
        </>
      )}
    </section>
  );
}

// driftSummary is the semantic one-liner above the raw diff: what moved, at
// a glance, before the reader commits to the full document.
function driftSummary(diff: SkillPinsDiff, documentChanged: boolean): string {
  const parts: string[] = [documentChanged ? 'SKILL.md changed' : 'SKILL.md unchanged'];
  const file = (n: number) => `${n} supporting ${n === 1 ? 'file' : 'files'}`;
  if (diff.added_files.length > 0) parts.push(`${file(diff.added_files.length)} added`);
  if (diff.removed_files.length > 0) parts.push(`${file(diff.removed_files.length)} removed`);
  if (diff.modified_files.length > 0) parts.push(`${file(diff.modified_files.length)} modified`);
  return parts.join(' · ');
}

function FileChangeList({
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
    <div className="text-[11px] font-mono space-y-0.5" aria-label={`${label} files`}>
      {paths.map((p) => (
        <div key={p} className={cn('flex gap-1.5', tone)}>
          <span>{glyph}</span>
          <span className="break-all">{escapeNonPrintable(p)}</span>
        </div>
      ))}
    </div>
  );
}

// SkillDocumentDiff renders the canonical SKILL.md delta as a unified
// line-level view (markdown prose, not JSON schemas — diffLines, not the
// schema panels). Each line is escaped individually AFTER splitting:
// escaping first would turn real newlines into literal \n and collapse the
// document onto one line.
function SkillDocumentDiff({
  oldDocument,
  newDocument,
}: {
  oldDocument: string;
  newDocument: string;
}) {
  const tokens = diffLines(oldDocument, newDocument);
  const changed = tokens.filter((t) => t.kind !== 'same').length;
  const shown = tokens.slice(0, MAX_DIFF_LINES);
  return (
    <div className="space-y-1">
      <h4 className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">
        SKILL.md ({changed} {changed === 1 ? 'line' : 'lines'} changed)
      </h4>
      <div className="rounded border border-border/40 bg-background/60 overflow-x-auto max-h-96 overflow-y-auto scrollbar-dark">
        <pre className="text-[11px] font-mono leading-relaxed p-2 whitespace-pre-wrap break-words">
          {shown.map((t, i) => (
            <DiffLine key={i} token={t} />
          ))}
        </pre>
      </div>
      {tokens.length > MAX_DIFF_LINES && (
        <p className="text-[10px] text-text-muted">
          Diff truncated at {MAX_DIFF_LINES} lines.
        </p>
      )}
    </div>
  );
}

function DiffLine({ token }: { token: DiffToken }) {
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
