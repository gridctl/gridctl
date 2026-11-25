import { ConfirmDialog } from '../ui/ConfirmDialog';
import { useAuthStore } from '../../stores/useAuthStore';
import { useUIStore } from '../../stores/useUIStore';

// Mounted in AppShell, not in the header: the header's backdrop-blur creates a
// containing block that would trap this dialog's fixed positioning, and the
// command palette opens it too.
export function ForgetCredentialDialog() {
  const open = useUIStore((s) => s.forgetCredentialOpen);
  const setOpen = useUIStore((s) => s.setForgetCredentialOpen);

  return (
    <ConfirmDialog
      isOpen={open}
      onClose={() => setOpen(false)}
      onConfirm={() => {
        setOpen(false);
        useAuthStore.getState().forgetCredential();
      }}
      title="Forget gateway credential?"
      message={
        <>
          <p>
            The saved credential is removed from this browser. This window and any detached windows
            lock until it is entered again.
          </p>
          <p>The gateway keeps running and the credential stays valid.</p>
        </>
      }
      confirmLabel="Forget credential"
      variant="danger"
      autoFocus="cancel"
    />
  );
}
