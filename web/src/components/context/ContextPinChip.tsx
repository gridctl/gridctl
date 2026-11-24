import { useEffect, useState } from 'react';
import { describeError, isCancellation } from '../../lib/describeError';
import { cn } from '../../lib/cn';
import { fetchContextPins, resetContextPin, type ContextPin } from '../../lib/api';
import { usePinsStore } from '../../stores/usePinsStore';
import { Modal } from '../ui/Modal';
import { FailedState } from '../ui/EmptyState';
import { PinsContextDetail } from '../pins/PinsContextDetail';

export interface ContextPinChipProps {
  status: 'pinned' | 'drift' | 'unpinned';
  onReview: () => void;
}

/** Per-fragment pin state. Pinned fragments render nothing. */
export function ContextPinChip({ status, onReview }: ContextPinChipProps) {
  if (status === 'pinned') return null;
  const drift = status === 'drift';
  return (
    <button
      type="button"
      onClick={onReview}
      className={cn(
        'text-3xs px-1.5 py-0.5 rounded border',
        drift
          ? 'text-status-pending border-status-pending/40 bg-status-pending/10'
          : 'text-text-muted border-border/40',
      )}
    >
      {drift ? 'Pin drift' : 'Unpinned'}
    </button>
  );
}

/** Loads one fragment pin and reviews it without leaving the context editor. */
export function ContextPinReviewModal({
  name,
  onClose,
}: {
  name: string;
  onClose: () => void;
}) {
  // undefined while loading; null when the load succeeded with no record.
  const [pin, setPin] = useState<ContextPin | null | undefined>(undefined);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let cancelled = false;
    void fetchContextPins()
      .then((all) => {
        if (cancelled) return;
        setPin(all[name] ?? null);
        setLoadError(null);
      })
      .catch((err) => {
        if (!cancelled && !isCancellation(err)) setLoadError(describeError(err, 'Load pin record').text);
      });
    return () => {
      cancelled = true;
    };
  }, [name, attempt]);
  return (
    <Modal isOpen onClose={onClose} title={`${name} changed since its pin`} size="wide">
      {pin ? (
        <PinsContextDetail
          name={name}
          pin={pin}
          onReset={async (fragment) => {
            await resetContextPin(fragment);
            usePinsStore.getState().setContextPins(await fetchContextPins());
            onClose();
          }}
        />
      ) : pin === null ? (
        <p className="text-xs text-text-muted p-4">No pin record for {name}.</p>
      ) : loadError ? (
        <FailedState
          feature="Pin record"
          message={loadError}
          onRetry={() => {
            setLoadError(null);
            setAttempt((n) => n + 1);
          }}
          inline
        />
      ) : (
        <p className="text-xs text-text-muted p-4">Loading pin record…</p>
      )}
    </Modal>
  );
}
