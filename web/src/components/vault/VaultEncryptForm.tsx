import { useState } from 'react';
import { Lock } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';

// First-time encryption enforces a minimum passphrase length; re-locking an
// already-encrypted vault must accept the existing passphrase whatever its
// length, so the floor applies to 'encrypt' mode only.
const MIN_PASSPHRASE_LENGTH = 8;
const RECOMMENDED_PASSPHRASE_LENGTH = 12;

export interface VaultEncryptFormProps {
  // 'encrypt' sets a passphrase for the first time; 'lock' re-locks an
  // already-encrypted vault with its existing passphrase. Copy and validation
  // follow the mode.
  mode?: 'encrypt' | 'lock';
  onLock: (passphrase: string) => Promise<void>;
  onCancel: () => void;
  className?: string;
}

// Inline passphrase + confirm form used by both the sidebar and the
// detached page when the user clicks "Encrypt." Owns its own passphrase
// state so the parent doesn't have to thread it through.
export function VaultEncryptForm({
  mode = 'encrypt',
  onLock,
  onCancel,
  className,
}: VaultEncryptFormProps) {
  const [passphrase, setPassphrase] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [isLocking, setIsLocking] = useState(false);

  const encrypting = mode === 'encrypt';
  const tooShort =
    encrypting && passphrase.trim().length < MIN_PASSPHRASE_LENGTH;

  const reset = () => {
    setPassphrase('');
    setConfirm('');
    setError(null);
  };

  const handleSubmit = async () => {
    if (!passphrase.trim() || tooShort) return;
    if (passphrase !== confirm) {
      setError('Passphrases do not match');
      return;
    }
    setIsLocking(true);
    setError(null);
    try {
      // Trimmed to match what VaultLockPrompt submits on unlock — an
      // untrimmed pasted passphrase would otherwise encrypt a vault the web
      // UI can never unlock.
      await onLock(passphrase.trim());
      reset();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to lock vault');
    } finally {
      setIsLocking(false);
    }
  };

  const handleCancel = () => {
    reset();
    onCancel();
  };

  // Non-blocking strength guidance for first-time encryption: a hard floor at
  // 8 characters, a nudge up to 12.
  const guidance =
    encrypting && passphrase.length > 0
      ? tooShort
        ? `Passphrase must be at least ${MIN_PASSPHRASE_LENGTH} characters.`
        : passphrase.trim().length < RECOMMENDED_PASSPHRASE_LENGTH
          ? 'Short passphrases are easy to brute-force; 12+ characters recommended.'
          : null
      : null;

  return (
    <div className={cn('space-y-2', className)}>
      <div className="text-xs text-text-secondary mb-2">
        {encrypting
          ? 'Encrypt vault with a passphrase:'
          : 'Lock vault with your passphrase:'}
      </div>
      <input
        type="password"
        value={passphrase}
        onChange={(e) => {
          setPassphrase(e.target.value);
          setError(null);
        }}
        placeholder={encrypting ? 'New passphrase' : 'Passphrase'}
        autoFocus
        className="w-full bg-surface border border-border rounded-lg px-3 py-2 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:ring-1 focus:ring-primary/30 outline-none transition-colors"
      />
      <input
        type="password"
        value={confirm}
        onChange={(e) => {
          setConfirm(e.target.value);
          setError(null);
        }}
        placeholder="Confirm passphrase"
        className="w-full bg-surface border border-border rounded-lg px-3 py-2 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:ring-1 focus:ring-primary/30 outline-none transition-colors"
        onKeyDown={(e) => {
          if (e.key === 'Enter') handleSubmit();
        }}
      />
      {guidance && (
        <p
          className={cn(
            'text-[10px]',
            tooShort ? 'text-status-error' : 'text-status-pending',
          )}
        >
          {guidance}
        </p>
      )}
      {error && <p className="text-[10px] text-status-error">{error}</p>}
      <div className="flex justify-end gap-2">
        <button
          onClick={handleCancel}
          className="px-2 py-1 text-[10px] text-text-secondary hover:text-text-primary rounded transition-colors"
        >
          Cancel
        </button>
        <Button
          variant="primary"
          size="sm"
          onClick={handleSubmit}
          disabled={
            !passphrase.trim() || !confirm.trim() || tooShort || isLocking
          }
        >
          <Lock size={12} />
          {encrypting
            ? isLocking
              ? 'Encrypting...'
              : 'Encrypt'
            : isLocking
              ? 'Locking...'
              : 'Lock vault'}
        </Button>
      </div>
    </div>
  );
}
