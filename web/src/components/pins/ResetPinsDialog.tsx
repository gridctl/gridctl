import { ConfirmDialog } from '../ui/ConfirmDialog';

// ResetPinsDialog wraps ConfirmDialog (variant="danger") with the blast
// radius spelled out: resetting discards the trust record, and the next
// verify re-pins whatever the server serves then, unseen. The actual DELETE
// lives in the caller so it can refresh the store and clear the URL in its
// own context.
interface Props {
  isOpen: boolean;
  onClose: () => void;
  onConfirm: () => void;
  serverName: string;
  toolCount: number;
}

export function ResetPinsDialog({ isOpen, onClose, onConfirm, serverName, toolCount }: Props) {
  return (
    <ConfirmDialog
      isOpen={isOpen}
      onClose={onClose}
      onConfirm={onConfirm}
      title="Reset pins"
      variant="danger"
      confirmLabel={`Reset pins for ${serverName}`}
      message={
        <div className="space-y-2">
          <p>
            This deletes all {toolCount} pinned tool {toolCount === 1 ? 'record' : 'records'} for{' '}
            <span className="font-mono text-text-secondary">{serverName}</span>. Drift status and
            findings history are discarded.
          </p>
          <p>
            On the next verify the server re-pins from scratch, trusting whatever definitions it
            serves at that moment without a diff to review. This cannot be undone.
          </p>
        </div>
      }
    />
  );
}
