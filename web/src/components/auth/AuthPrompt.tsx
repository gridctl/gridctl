import { useState, useCallback } from 'react';
import { Lock, AlertCircle, Eye, EyeOff } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { storeToken, clearToken, fetchStatus } from '../../lib/api';
import { useAuthStore } from '../../stores/useAuthStore';

export function AuthPrompt() {
  const [token, setToken] = useState('');
  const [showToken, setShowToken] = useState(false);
  const [isVerifying, setIsVerifying] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const setAuthenticated = useAuthStore((s) => s.setAuthenticated);

  const handleSubmit = useCallback(async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token.trim()) return;

    setIsVerifying(true);
    setError(null);

    // Store token temporarily and test it
    storeToken(token.trim());

    try {
      await fetchStatus();
      setAuthenticated(true);
    } catch {
      setError('Invalid token — authentication failed');
      clearToken();
    } finally {
      setIsVerifying(false);
    }
  }, [token, setAuthenticated]);

  return (
    <div className="absolute inset-0 flex items-center justify-center bg-background/95 backdrop-blur-sm z-50">
      <div className="w-full max-w-sm p-8 animate-fade-in-scale glass-panel-elevated">
        {/* Lock icon */}
        <div className="relative mx-auto w-16 h-16 mb-6">
          <div className="absolute inset-0 bg-primary/20 rounded-2xl blur-xl" />
          <div className="relative w-full h-full bg-primary/10 rounded-2xl border border-primary/20 flex items-center justify-center">
            <Lock size={28} className="text-primary" />
          </div>
        </div>

        <h2 className="text-lg font-semibold text-text-primary text-center mb-2">
          Authentication Required
        </h2>
        <p className="text-sm text-text-muted text-center mb-6">
          This gateway requires an API token to access.
        </p>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="relative">
            <input
              type={showToken ? 'text' : 'password'}
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder="Enter your API token"
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
              onClick={() => setShowToken(!showToken)}
              className="absolute right-2.5 top-1/2 -translate-y-1/2 p-1 rounded text-text-muted hover:text-text-primary transition-colors"
            >
              {showToken ? <EyeOff size={14} /> : <Eye size={14} />}
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
            disabled={!token.trim() || isVerifying}
            className="w-full"
          >
            {isVerifying ? 'Verifying...' : 'Authenticate'}
          </Button>
        </form>
      </div>
    </div>
  );
}
