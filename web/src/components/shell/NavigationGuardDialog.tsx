import { useEffect } from 'react';
import { createPortal } from 'react-dom';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { useNavigationGuardStore } from '../../stores/useNavigationGuardStore';

function joinLabels(labels: string[]): string {
  if (labels.length <= 1) return labels[0] ?? '';
  if (labels.length === 2) return `${labels[0]} and ${labels[1]}`;
  return `${labels.slice(0, -1).join(', ')}, and ${labels[labels.length - 1]}`;
}

// The one discard confirm for every guarded navigation. Portaled to the body
// and rendered only while a request is pending, so it lands after (and above)
// any modal that is already open.
export function NavigationGuardDialog() {
  const pending = useNavigationGuardStore((s) => s.pending);
  const confirm = useNavigationGuardStore((s) => s.confirm);
  const cancel = useNavigationGuardStore((s) => s.cancel);

  useEffect(() => useNavigationGuardStore.getState().hostMounted(), []);

  if (!pending) return null;

  return createPortal(
    <ConfirmDialog
      isOpen
      onClose={cancel}
      onConfirm={confirm}
      title="Discard unsaved changes"
      message={<p>You have unsaved changes in {joinLabels(pending.labels)}. Leave and discard them?</p>}
      confirmLabel="Discard & leave"
      variant="danger"
    />,
    document.body,
  );
}
