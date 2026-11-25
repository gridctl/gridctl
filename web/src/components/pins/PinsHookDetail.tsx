import { Link } from 'react-router';
import { Clock, GitBranch, Lock, LockOpen } from 'lucide-react';
import { cn } from '../../lib/cn';
import { escapeNonPrintable, shortPinHash } from '../../lib/nonPrintable';
import { formatRelativeTime } from '../../lib/time';
import { pinStatusMeta } from './pinStatus';
import { PinApprovalHistory } from './PinApprovalHistory';
import { TOOLS_SECTION_ID } from './PinsServerDetail';
import { HookPinReview } from '../registry/hooks/HookPinReview';
import type { HookPin } from '../../types';

// ---------------------------------------------------------------------------
// Hook detail: header, provenance, approval history, the shared hook review,
// and the pinned file digests. The header reads the stored list status; the
// review verifies live and its status line is authoritative.
// ---------------------------------------------------------------------------

interface PinsHookDetailProps {
  name: string;
  pin: HookPin;
}

// The review refreshes the hook pins store itself after an approve; the
// Pins workspace has no other list to reload.
const noRefresh = () => undefined;

export function PinsHookDetail({ name, pin }: PinsHookDetailProps) {
  const { label, colorClass } = pinStatusMeta(pin.status);
  const hasDrift = pin.status === 'drift';
  const files = pin.files ?? [];
  const origin =
    pin.source === 'git' && pin.origin?.repo
      ? `Imported: ${pin.origin.repo}${pin.origin.ref ? `@${pin.origin.ref}` : ''}`
      : 'Local';

  return (
    <div className="px-6 py-4 space-y-4 max-w-3xl">
      <div className="flex items-center gap-3">
        <h2 className="text-sm font-mono text-text-primary">{escapeNonPrintable(name)}</h2>
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
          <GitBranch size={10} className="text-text-muted" />
          {escapeNonPrintable(origin)}
        </span>
        <span>
          <span className="text-text-secondary font-medium">{files.length}</span>{' '}
          {files.length === 1 ? 'file' : 'files'} pinned
        </span>
        {pin.pinned_at && (
          <span title={pin.pinned_at}>first pinned {formatRelativeTime(new Date(pin.pinned_at))}</span>
        )}
        {pin.approved_reason && (
          <span className="truncate max-w-xs">
            approved with reason: {escapeNonPrintable(pin.approved_reason)}
          </span>
        )}
        <Link
          to={`/library?kind=hook&selected=${encodeURIComponent(name)}`}
          className="text-primary hover:underline"
        >
          Open in Library
        </Link>
      </div>

      <PinApprovalHistory approvals={pin.approvals} />

      {hasDrift && (
        <p className="text-xs text-text-secondary">
          Clients keep the approved version of this hook; projection of the new content is skipped
          until it is approved.
        </p>
      )}

      <HookPinReview name={name} inPinsWorkspace onRefresh={noRefresh} />

      <section id={TOOLS_SECTION_ID} className="space-y-2">
        <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted">Pinned file digests</h3>
        {files.length === 0 ? (
          <p className="text-2xs text-text-muted">No files recorded in the pin.</p>
        ) : (
          <div className="rounded-lg border border-border/40 bg-background/60 overflow-hidden">
            <table className="w-full text-xs border-collapse">
              <thead>
                <tr className="border-b border-border/30">
                  <th className="text-left px-3 py-2 text-text-muted font-medium">File</th>
                  <th className="text-left px-3 py-2 text-text-muted font-medium">Digest</th>
                </tr>
              </thead>
              <tbody>
                {files.map((f) => (
                  <tr key={f.path} className="border-b border-border/20 last:border-b-0">
                    <td className="px-3 py-2 font-mono text-text-primary">{escapeNonPrintable(f.path)}</td>
                    <td className="px-3 py-2 font-mono text-text-muted" title={f.digest}>
                      {shortPinHash(f.digest)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  );
}
