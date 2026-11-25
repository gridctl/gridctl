import { useState, type ReactNode } from 'react';
import { Trash2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { RESET_BUTTON_ID } from './PinsServerDetail';
import { ConfirmDialog } from '../ui/ConfirmDialog';

// PinDangerZone is the destructive reset affordance shared by the skill and
// context details. onReset must not reject: there is no catch here, so each
// caller reports its own errors. The button is guarded, not disabled, while a
// reset runs so a palette click still lands on it (and does nothing).
export function PinDangerZone({
  buttonLabel,
  hint,
  dialogTitle,
  message,
  onReset,
  anchor = true,
}: {
  buttonLabel: string;
  hint: string;
  dialogTitle: string;
  message: ReactNode;
  onReset: () => Promise<void>;
  anchor?: boolean;
}) {
  const [resetOpen, setResetOpen] = useState(false);
  const [isResetting, setIsResetting] = useState(false);

  const handleResetConfirm = async () => {
    setResetOpen(false);
    setIsResetting(true);
    try {
      await onReset();
    } finally {
      setIsResetting(false);
    }
  };

  return (
    <>
      <section className="pt-4 mt-2 border-t border-border/30 space-y-2 max-w-3xl">
        <h3 className="text-3xs uppercase tracking-[0.18em] text-status-error/70">Danger</h3>
        <div className="flex items-center gap-3">
          <button
            id={anchor ? RESET_BUTTON_ID : undefined}
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
            {isResetting ? 'Resetting…' : buttonLabel}
          </button>
          <p className="text-2xs text-text-muted">{hint}</p>
        </div>
      </section>

      <div className="contents">
        <ConfirmDialog
          isOpen={resetOpen}
          onClose={() => setResetOpen(false)}
          onConfirm={() => void handleResetConfirm()}
          title={dialogTitle}
          variant="danger"
          confirmLabel={buttonLabel}
          message={message}
        />
      </div>
    </>
  );
}
