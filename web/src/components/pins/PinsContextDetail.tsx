import { useEffect, useState } from 'react';
import { Trash2 } from 'lucide-react';
import type { ContextPin, ContextPinsDiff } from '../../lib/api';
import { approveContextPin, fetchContextPinDiff, HTTPError } from '../../lib/api';
import { escapeNonPrintable } from '../../lib/nonPrintable';
import { describeError } from '../../lib/describeError';
import { showToast } from '../ui/Toast';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { FindingsList } from './PinFindings';

interface PinsContextDetailProps {
  name: string;
  pin: ContextPin;
  onReset: (name: string) => Promise<void>;
}

export function PinsContextDetail({ name, pin, onReset }: PinsContextDetailProps) {
  const [resetOpen, setResetOpen] = useState(false);
  const [isResetting, setIsResetting] = useState(false);

  const handleResetConfirm = async () => {
    setIsResetting(true);
    try {
      await onReset(name);
      showToast('success', `Pin reset for ${name}`);
      setResetOpen(false);
    } catch (err) {
      showToast('error', describeError(err, 'Reset').text);
    } finally {
      setIsResetting(false);
    }
  };

  return (
    <div className="flex flex-col gap-6 p-6">
      <header className="flex items-start justify-between gap-3">
        <div>
          <h2 className="text-sm font-medium text-text-primary">{name}</h2>
          <p className="text-2xs text-text-muted mt-1">
            {pin.source === 'pack' && pin.pack ? `pack: ${pin.pack}` : pin.source === 'local' ? 'local' : 'context fragment'}
          </p>
        </div>
        <button
          type="button"
          onClick={() => setResetOpen(true)}
          className="h-7 px-2 text-2xs text-status-error border border-status-error/30 rounded hover:bg-status-error/10 flex items-center gap-1"
        >
          <Trash2 size={11} />
          Reset
        </button>
      </header>

      {pin.status === 'drift' ? (
        <>
          <h3 className="text-sm text-status-pending">{name} changed since its pin</h3>
          <ContextDriftSection name={name} />
        </>
      ) : (
        <p className="text-2xs text-text-muted">Content matches its pin.</p>
      )}

      <section className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <DocumentPane title="Pinned document" text={pin.document ?? ''} />
        <CurrentDocument name={name} fallback={pin.document ?? ''} drifted={pin.status === 'drift'} />
      </section>

      <FindingsList findings={pin.findings} />

      <ConfirmDialog
        isOpen={resetOpen}
        onClose={() => setResetOpen(false)}
        onConfirm={() => void handleResetConfirm()}
        title="Reset fragment pin"
        variant="danger"
        confirmLabel={isResetting ? 'Resetting…' : `Reset pin for ${name}`}
        message={
          <p>
            This deletes the pin record for <span className="font-mono">{name}</span>. It is re-pinned
            on the next sync.
          </p>
        }
      />
    </div>
  );
}

function DocumentPane({ title, text }: { title: string; text: string }) {
  return (
    <div className="min-w-0">
      <h4 className="text-3xs uppercase tracking-wider text-text-muted mb-1">{title}</h4>
      <pre className="text-2xs font-mono whitespace-pre-wrap break-words bg-background/40 border border-border/30 rounded p-3 max-h-64 overflow-auto">
        {escapeNonPrintable(text)}
      </pre>
    </div>
  );
}

function CurrentDocument({ name, fallback, drifted }: { name: string; fallback: string; drifted: boolean }) {
  const [fetched, setFetched] = useState<string | null>(null);
  useEffect(() => {
    if (!drifted) return;
    let cancelled = false;
    void fetchContextPinDiff(name)
      .then((diff) => {
        if (!cancelled) setFetched(diff.new_document ?? '');
      })
      .catch(() => {
        if (!cancelled) setFetched(null);
      });
    return () => {
      cancelled = true;
    };
  }, [name, drifted]);
  const text = drifted && fetched !== null ? fetched : fallback;
  return <DocumentPane title="Current document" text={text} />;
}

function ContextDriftSection({ name }: { name: string }) {
  const [diff, setDiff] = useState<ContextPinsDiff | null>(null);
  const [reason, setReason] = useState('');
  const [isApproving, setIsApproving] = useState(false);
  const [loadError, setLoadError] = useState(false);

  // No synchronous reset: the detail is keyed on the fragment name, so a
  // name change remounts this section. A failed load stays failed until the
  // next successful fetch.
  useEffect(() => {
    let cancelled = false;
    void fetchContextPinDiff(name)
      .then((next) => {
        if (!cancelled) {
          setDiff(next);
          setLoadError(false);
        }
      })
      .catch(() => {
        if (!cancelled) setLoadError(true);
      });
    return () => {
      cancelled = true;
    };
  }, [name]);

  const needsReason = (diff?.findings ?? []).some((f) => f.severity === 'warn' || f.severity === 'critical');

  const doApprove = async () => {
    if (!diff) return;
    setIsApproving(true);
    try {
      await approveContextPin(name, diff.composite_hash, reason.trim() || undefined);
      showToast('success', `Approved ${name}`);
      setReason('');
    } catch (err) {
      if (err instanceof HTTPError && err.status === 409) {
        showToast('error', 'Fragment content changed; fetch the diff again and re-review');
      } else {
        showToast('error', describeError(err, 'Approve').text);
      }
    } finally {
      setIsApproving(false);
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <FindingsList findings={diff?.findings} />
      {needsReason && (
        <p className="text-2xs text-status-pending">This fragment carries advisory findings; a reason is required</p>
      )}
      <label className="flex flex-col gap-1 text-2xs text-text-muted">
        Reason
        <input
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          aria-label="Approval reason"
          className="bg-background/60 border border-border/40 rounded px-2 py-1 text-xs text-text-primary"
        />
      </label>
      <button
        type="button"
        onClick={() => void doApprove()}
        disabled={diff === null || loadError || isApproving || (needsReason && reason.trim() === '')}
        className="self-start h-6 px-2.5 text-2xs font-medium rounded bg-status-running/15 text-status-running border border-status-running/30 hover:bg-status-running/25 transition-colors disabled:opacity-50"
      >
        {isApproving ? 'Approving…' : 'Approve'}
      </button>
    </div>
  );
}
