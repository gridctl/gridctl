import { useState, useCallback } from 'react';
import { Lock, AlertCircle, Eye, EyeOff } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';

interface VaultLockPromptProps {
  onUnlock: (passphrase: string) => Promise<boolean>;
}

export function VaultLockPrompt({ onUnlock }: VaultLockPromptProps) {
  const [passphrase, setPassphrase] = useState('');
  const [showPassphrase, setShowPassphrase] = useState(false);
  const [isUnlocking, setIsUnlocking] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = useCallback(async (e: React.FormEvent) => {
    e.preventDefault();
    if (!passphrase.trim()) return;

    setIsUnlocking(true);
    setError(null);

    try {
      const success = await onUnlock(passphrase.trim());
      if (success) {
        setPassphrase('');
      } else {
        setError('Wrong passphrase — unable to decrypt vault');
      }
    } catch {
      setError('Wrong passphrase — unable to decrypt vault');
    } finally {
      setIsUnlocking(false);
    }
  }, [passphrase, onUnlock]);

  return (
    <div className="flex-1 flex items-center justify-center p-6">
      <div className="w-full max-w-xs animate-fade-in-scale">
        {/* Lock icon */}
        <div className="relative mx-auto w-14 h-14 mb-5">
          <div className="absolute inset-0 bg-primary/20 rounded-2xl blur-xl" />
          <div className="relative w-full h-full bg-primary/10 rounded-2xl border border-primary/20 flex items-center justify-center">
            <Lock size={24} className="text-primary" />
          </div>
        </div>

        <h3 className="text-sm font-semibold text-text-primary text-center mb-1.5">
          Vault Locked
        </h3>
        <p className="text-xs text-text-muted text-center mb-5">
          Enter your passphrase to access secrets.
        </p>

        <form onSubmit={handleSubmit} className="space-y-3">
          <div className="relative">
            <input
              type={showPassphrase ? 'text' : 'password'}
              value={passphrase}
              onChange={(e) => setPassphrase(e.target.value)}
              placeholder="Enter passphrase"
              autoFocus
              className={cn(
                'w-full bg-surface border rounded-lg px-3 py-2.5 pr-10',
                'text-sm font-mono text-text-primary',
                'placeholder:text-text-muted',
                'focus:border-primary/50 focus:ring-1 focus:ring-primary/30 outline-none',
                'transition-colors',
                error ? 'border-status-error/50' : 'border-border'
              )}
            />
            <button
              type="button"
              onClick={() => setShowPassphrase(!showPassphrase)}
              className="absolute right-2.5 top-1/2 -translate-y-1/2 p-1 rounded text-text-muted hover:text-text-primary transition-colors"
            >
              {showPassphrase ? <EyeOff size={14} /> : <Eye size={14} />}
            </button>
          </div>

          {error && (
            <div className="flex items-center gap-2 text-xs text-status-error">
              <AlertCircle size={12} className="flex-shrink-0" />
              <span>{error}</span>
            </div>
          )}

          <Button
            type="submit"
            variant="primary"
            disabled={!passphrase.trim() || isUnlocking}
            className="w-full"
          >
            {isUnlocking ? 'Unlocking...' : 'Unlock Vault'}
          </Button>
        </form>
      </div>
    </div>
  );
}
