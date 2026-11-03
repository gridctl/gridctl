import { useEffect, useState } from 'react';
import { cn } from '../../lib/cn';
import { fetchContextPins, resetContextPin, type ContextPin } from '../../lib/api';
import { usePinsStore } from '../../stores/usePinsStore';
import { Modal } from '../ui/Modal';
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
  const [pin, setPin] = useState<ContextPin | null>(null);
  useEffect(() => {
    let cancelled = false;
    void fetchContextPins()
      .then((all) => {
        if (!cancelled) setPin(all[name] ?? null);
      })
      .catch(() => {
        if (!cancelled) setPin(null);
      });
    return () => {
      cancelled = true;
    };
  }, [name]);
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
      ) : (
        <p className="text-xs text-text-muted p-4">No pin record for {name}.</p>
      )}
    </Modal>
  );
}
