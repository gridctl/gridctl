import { Copy } from 'lucide-react';
import type { PinApproval } from '../../lib/api';
import { escapeNonPrintable } from '../../lib/nonPrintable';
import { formatRelativeTime } from '../../lib/time';
import { copyWithToast } from '../ui/Toast';

// Approval history is operator-supplied text. References are never anchors
// and are never fetched. escapeNonPrintable keeps hidden characters visible.

export function PinApprovalHistory({ approvals }: { approvals?: PinApproval[] }) {
  if (!approvals || approvals.length === 0) return null;
  const [latest, ...older] = approvals;
  return (
    <section className="space-y-2" aria-label="Approval history">
      <h3 className="text-3xs uppercase tracking-[0.18em] text-text-muted/70">Last approval</h3>
      <PinApprovalEntry entry={latest} />
      {older.length > 0 && (
        <details className="text-2xs">
          <summary className="cursor-pointer text-text-muted">History</summary>
          <div className="mt-2 space-y-2">
            {older.map((entry, index) => (
              <PinApprovalEntry key={`${entry.approved_at}-${index}`} entry={entry} />
            ))}
          </div>
        </details>
      )}
    </section>
  );
}

function PinApprovalEntry({ entry }: { entry: PinApproval }) {
  const when = entry.approved_at ? new Date(entry.approved_at) : null;
  const relative = when && !Number.isNaN(when.getTime()) ? formatRelativeTime(when) : 'unknown time';
  const who = entry.approved_by
    ? `${escapeNonPrintable(entry.approved_by)}${entry.approved_by_source ? ` (${escapeNonPrintable(entry.approved_by_source)})` : ''}`
    : '';
  return (
    <div className="rounded-md border border-border/40 bg-background/60 px-3 py-2 space-y-1 text-2xs">
      <p className="text-text-secondary" title={entry.approved_at || undefined}>
        <span>{relative}</span>
        {who ? <span> by {who}</span> : null}
      </p>
      {entry.reason && (
        <p className="text-text-primary whitespace-pre-wrap">{escapeNonPrintable(entry.reason)}</p>
      )}
      {(entry.evidence ?? []).map((ref) => (
        <p key={ref} className="flex items-start gap-2 text-text-muted">
          <span className="font-mono break-all">{escapeNonPrintable(ref)}</span>
          <button
            type="button"
            className="p-0.5 rounded text-text-muted hover:text-primary"
            aria-label={`Copy reference ${escapeNonPrintable(ref)}`}
            onClick={() => void copyWithToast(ref, 'Reference')}
          >
            <Copy size={11} />
          </button>
        </p>
      ))}
    </div>
  );
}

export function ApprovalFields({
  reason,
  onReason,
  evidence,
  onEvidence,
  reasonRequired,
}: {
  reason: string;
  onReason: (value: string) => void;
  evidence: string;
  onEvidence: (value: string) => void;
  reasonRequired: boolean;
}) {
  return (
    <div className="flex flex-col gap-1.5 min-w-0">
      <input
        value={reason}
        onChange={(e) => onReason(e.target.value)}
        placeholder={reasonRequired ? 'Reason (required: advisory findings present)' : 'Reason (optional)'}
        aria-label="Approval reason"
        className="h-6 w-64 px-2 text-2xs rounded border border-border/40 bg-background/60 text-text-primary placeholder:text-text-muted/60"
      />
      <label className="flex flex-col gap-1 text-3xs text-text-muted">
        Evidence references, one per line
        <textarea
          value={evidence}
          onChange={(e) => onEvidence(e.target.value)}
          placeholder="https://example.test/pull/1"
          aria-label="Evidence references, one per line"
          rows={2}
          className="w-64 px-2 py-1 text-2xs rounded border border-border/40 bg-background/60 text-text-primary placeholder:text-text-muted/60"
        />
      </label>
    </div>
  );
}

