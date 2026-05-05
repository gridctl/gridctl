// WipeTelemetryDialog wraps ConfirmDialog (variant="danger") with an
// enumerated body so the operator sees the size and date span being
// destroyed before confirming. The actual wipe call lives in callsites
// (header for global, sidebar for per-server) so each can update its
// store and toast in its own context.
import { useMemo } from 'react';
import type { InventoryRecord, TelemetrySignal } from '../../types';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { formatBytes, formatDateOnly } from '../../lib/format-bytes';
import { summarize } from '../../stores/useTelemetryStore';

interface Props {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  // Inventory subset that the wipe will affect. The caller is responsible
  // for filtering (header passes everything; sidebar passes
  // `inventoryByServer(name)`). Empty list disables the destructive
  // affordance so we never confirm a no-op.
  scope: InventoryRecord[];
  // Title and subject (e.g., "Wipe persisted data" / "for github") so the
  // copy reads correctly in both global and per-server callsites.
  title: string;
  subject?: string;
}

export function WipeTelemetryDialog({ isOpen, onClose, onConfirm, scope, title, subject }: Props) {
  const summary = useMemo(() => summarize(scope), [scope]);
  const empty = scope.length === 0;

  return (
    <ConfirmDialog
      isOpen={isOpen}
      onClose={onClose}
      onConfirm={onConfirm}
      title={title}
      variant="danger"
      confirmLabel={empty ? 'Nothing to wipe' : 'Wipe data'}
      message={
        <div className="space-y-2">
          <p>
            This permanently deletes telemetry files{subject ? ` ${subject}` : ''} from disk.
            The change cannot be undone, but it does not modify the stack YAML — persistence
            stays enabled and new files will be created as signals continue.
          </p>
          {empty ? (
            <p className="text-text-muted italic">No persisted telemetry exists in scope.</p>
          ) : (
            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 font-mono text-[11px] text-text-secondary">
              <dt className="text-text-muted">Total</dt>
              <dd>{formatBytes(summary.totalBytes)}</dd>
              <dt className="text-text-muted">Servers</dt>
              <dd>{summary.servers}</dd>
              <dt className="text-text-muted">Signals</dt>
              <dd>{summary.signals.length === 0 ? '—' : summary.signals.join(', ')}</dd>
              {summary.oldest && summary.newest && (
                <>
                  <dt className="text-text-muted">Span</dt>
                  <dd>
                    {formatDateOnly(summary.oldest)} → {formatDateOnly(summary.newest)}
                  </dd>
                </>
              )}
            </dl>
          )}
        </div>
      }
    />
  );
}

// Used by tests so the wipe modal renders deterministic text from a
// fixture without standing up the whole telemetry store.
export function _testFormatScope(scope: InventoryRecord[]): {
  totalBytes: string;
  servers: number;
  signals: TelemetrySignal[];
  span: string | null;
} {
  const s = summarize(scope);
  return {
    totalBytes: formatBytes(s.totalBytes),
    servers: s.servers,
    signals: s.signals,
    span:
      s.oldest && s.newest ? `${formatDateOnly(s.oldest)} → ${formatDateOnly(s.newest)}` : null,
  };
}
