import { useState } from 'react';
import { Lock } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';

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

  const reset = () => {
    setPassphrase('');
    setConfirm('');
    setError(null);
  };

  const handleSubmit = async () => {
    if (!passphrase.trim()) return;
    if (passphrase !== confirm) {
      setError('Passphrases do not match');
      return;
    }
    setIsLocking(true);
    setError(null);
    try {
      await onLock(passphrase);
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

  return (
    <div className={cn('space-y-2', className)}>
      <div className="text-xs text-text-secondary mb-2">
        Encrypt vault with a passphrase:
      </div>
      <input
        type="password"
        value={passphrase}
        onChange={(e) => {
          setPassphrase(e.target.value);
          setError(null);
        }}
        placeholder="New passphrase"
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
          disabled={!passphrase.trim() || !confirm.trim() || isLocking}
        >
          <Lock size={12} />
          {isLocking ? 'Encrypting...' : 'Encrypt'}
        </Button>
      </div>
    </div>
  );
}
