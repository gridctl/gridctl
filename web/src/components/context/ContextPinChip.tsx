import { useEffect, useState } from 'react';
import { Link } from 'react-router';
import { ExternalLink } from 'lucide-react';
import { describeError, isCancellation } from '../../lib/describeError';
import { cn } from '../../lib/cn';
import { escapeNonPrintable } from '../../lib/nonPrintable';
import { fetchContextPins, resetContextPin, type ContextPin } from '../../lib/api';
import { contextPinsUrl } from '../../lib/contextGovernance';
import { useGuardedNavigate } from '../../hooks/useGuardedNavigate';
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
  const navigate = useGuardedNavigate();
  // The pin renders from the store so an approve inside the detail (which
  // refreshes the store) updates the status, history, and title here.
  const storePin = usePinsStore((s) => s.contextPins?.[name]);
  const [loaded, setLoaded] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let cancelled = false;
    void fetchContextPins()
      .then((all) => {
        if (cancelled) return;
        usePinsStore.getState().setContextPins(all);
        setLoaded(true);
        setLoadError(null);
      })
      .catch((err) => {
        if (!cancelled && !isCancellation(err)) setLoadError(describeError(err, 'Load pin record').text);
      });
    return () => {
      cancelled = true;
    };
  }, [name, attempt]);
  // undefined while loading; null when the load succeeded with no record.
  const pin: ContextPin | null | undefined = loaded ? (storePin ?? null) : undefined;
  const shown = escapeNonPrintable(name);
  const title = pin?.status === 'drift' ? `${shown} changed since its pin` : `Pin for ${shown}`;
  const pinsHref = contextPinsUrl(name, pin?.status === 'drift' ? 'drift' : undefined);
  return (
    <Modal isOpen onClose={onClose} title={title} size="wide">
      <div className="flex justify-end pb-2">
        <Link
          to={pinsHref}
          onClick={(e) => {
            if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
            e.preventDefault();
            onClose();
            navigate(pinsHref);
          }}
          className="inline-flex items-center gap-1 text-2xs text-primary hover:text-primary-light underline underline-offset-2"
        >
          Open in Pins
          <ExternalLink size={11} aria-hidden="true" />
        </Link>
      </div>
      {pin ? (
        <PinsContextDetail
          name={name}
          pin={pin}
          anchors={false}
          onReset={async (fragment) => {
            await resetContextPin(fragment);
            usePinsStore.getState().setContextPins(await fetchContextPins());
            onClose();
          }}
        />
      ) : pin === null ? (
        <p className="text-xs text-text-muted p-4">No pin record for {shown}.</p>
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
