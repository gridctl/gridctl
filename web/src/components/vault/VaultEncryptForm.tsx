import { useState } from 'react';
import { Lock } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';

// First-time encryption enforces a minimum passphrase length and nudges
// toward a longer one. An already encrypted store is locked again through
// relock, which takes no passphrase, so this form only ever encrypts.
const MIN_PASSPHRASE_LENGTH = 8;
const RECOMMENDED_PASSPHRASE_LENGTH = 12;

export interface VaultEncryptFormProps {
  onLock: (passphrase: string) => Promise<void>;
  onCancel: () => void;
  className?: string;
}

// Inline passphrase + confirm form used by both the sidebar and the
// detached page when the user clicks "Encrypt." Owns its own passphrase
// state so the parent doesn't have to thread it through.
export function VaultEncryptForm({
  onLock,
  onCancel,
  className,
}: VaultEncryptFormProps) {
  const [passphrase, setPassphrase] = useState('');
  const [confirm, setConfirm] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [isLocking, setIsLocking] = useState(false);

  const tooShort = passphrase.trim().length < MIN_PASSPHRASE_LENGTH;

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
      setError(err instanceof Error ? err.message : 'Failed to encrypt variables');
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
    passphrase.length > 0
      ? tooShort
        ? `Passphrase must be at least ${MIN_PASSPHRASE_LENGTH} characters.`
        : passphrase.trim().length < RECOMMENDED_PASSPHRASE_LENGTH
          ? 'Short passphrases are easy to brute-force; 12+ characters recommended.'
          : null
      : null;

  return (
    <div className={cn('space-y-2', className)}>
      <div className="text-xs text-text-secondary mb-2">
        Encrypt the variable store with a passphrase:
      </div>
      <input
        type="password"
        value={passphrase}
        onChange={(e) => {
          setPassphrase(e.target.value);
          setError(null);
        }}
        placeholder="New passphrase"
        aria-label="New passphrase"
        autoFocus
        className="w-full bg-surface border border-border rounded-lg px-3 py-2 text-xs font-mono text-text-primary placeholder:text-text-muted transition-colors"
      />
      <input
        type="password"
        value={confirm}
        onChange={(e) => {
          setConfirm(e.target.value);
          setError(null);
        }}
        placeholder="Confirm passphrase"
        aria-label="Confirm passphrase"
        className="w-full bg-surface border border-border rounded-lg px-3 py-2 text-xs font-mono text-text-primary placeholder:text-text-muted transition-colors"
        onKeyDown={(e) => {
          if (e.key === 'Enter') handleSubmit();
        }}
      />
      {guidance && (
        <p
          className={cn(
            'text-3xs',
            tooShort ? 'text-status-error' : 'text-status-pending',
          )}
        >
          {guidance}
        </p>
      )}
      {error && <p className="text-3xs text-status-error">{error}</p>}
      <div className="flex justify-end gap-2">
        <button
          onClick={handleCancel}
          className="px-2 py-1 text-3xs text-text-secondary hover:text-text-primary rounded transition-colors"
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
          {isLocking ? 'Encrypting...' : 'Encrypt'}
        </Button>
      </div>
    </div>
  );
}
