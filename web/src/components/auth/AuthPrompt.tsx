import { useState, useCallback, useRef, useEffect } from 'react';
import { Lock, AlertCircle, Eye, EyeOff } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { verifyCredential } from '../../lib/gatewayRequest';
import type { GatewayCredential } from '../../lib/credentials';
import { useAuthStore } from '../../stores/useAuthStore';

export function AuthPrompt() {
  const [token, setToken] = useState('');
  const [showToken, setShowToken] = useState(false);
  const [isVerifying, setIsVerifying] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [mode, setMode] = useState<GatewayCredential['mode']>('bearer');
  const [header, setHeader] = useState('Authorization');
  const inputRef = useRef<HTMLInputElement>(null);
  const requestRef = useRef<AbortController | null>(null);
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; requestRef.current?.abort(); };
  }, []);

  const beginAttempt = useAuthStore((s) => s.beginAttempt);
  const commitAttempt = useAuthStore((s) => s.commitAttempt);
  const notice = useAuthStore((s) => s.notice);

  const handleSubmit = useCallback(async (e: React.FormEvent) => {
    e.preventDefault();
    if (!token) return;

    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    const attempt = beginAttempt();
    const candidate = { mode, header, token };

    setIsVerifying(true);
    setError(null);

    try {
      await verifyCredential(candidate, controller.signal);
      if (!controller.signal.aborted) commitAttempt(attempt, candidate);
    } catch (cause) {
      if (!controller.signal.aborted && useAuthStore.getState().attempt === attempt) {
        setError(cause instanceof Error ? cause.message : 'Credential verification failed.');
        inputRef.current?.focus();
      }
    } finally {
      if (mountedRef.current && requestRef.current === controller) setIsVerifying(false);
    }
  }, [token, mode, header, beginAttempt, commitAttempt]);

  return (
    <div role="dialog" aria-modal="true" aria-labelledby="auth-title" onKeyDown={event => {
      if (event.key !== 'Tab') return;
      const fields = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('input, select, button:not(:disabled)'));
      const first = fields[0];
      const last = fields.at(-1);
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
      if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
    }} className="fixed inset-0 overflow-y-auto flex items-start justify-center bg-background/95 backdrop-blur-sm z-50 p-3">
      <div className="my-auto w-full max-w-sm p-4 sm:p-8 animate-fade-in-scale glass-panel-elevated">
        {/* Lock icon */}
        <div className="relative mx-auto w-16 h-16 mb-6">
          <div className="absolute inset-0 bg-primary/20 rounded-2xl blur-xl" />
          <div className="relative w-full h-full bg-primary/10 rounded-2xl border border-primary/20 flex items-center justify-center">
            <Lock size={28} className="text-primary" />
          </div>
        </div>

        <h2 id="auth-title" className="text-lg font-semibold text-text-primary text-center mb-2">
          Authentication Required
        </h2>
        <p className="text-sm text-text-muted text-center mb-6">
          This gateway requires an API token to access.
        </p>
        {notice && <p role="status" className="text-xs text-text-muted mb-4">{notice}</p>}

        <form onSubmit={handleSubmit} className="space-y-4">
          <label className="block text-sm text-text-primary">
            Authentication mode
            <select aria-label="Authentication mode" value={mode} onChange={e => { requestRef.current?.abort(); setMode(e.target.value as GatewayCredential['mode']); }} className="block w-full bg-surface p-2">
              <option value="bearer">Bearer</option>
              <option value="api_key">API key</option>
            </select>
          </label>
          {mode === 'api_key' && <label className="block text-sm text-text-primary">
            Credential header
            <input aria-label="Credential header" value={header} onChange={e => { requestRef.current?.abort(); setHeader(e.target.value); }} aria-describedby={error ? 'auth-error' : undefined} className="block w-full bg-surface p-2" />
          </label>}
          <label htmlFor="auth-secret" className="block text-sm text-text-primary">Credential</label>
          <div className="relative">
            <input
              id="auth-secret"
              ref={inputRef}
              aria-invalid={!!error}
              aria-describedby={error ? 'auth-error' : undefined}
              type={showToken ? 'text' : 'password'}
              value={token}
              onChange={(e) => { requestRef.current?.abort(); setToken(e.target.value); }}
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
              aria-label={showToken ? 'Hide credential' : 'Show credential'}
              aria-pressed={showToken}
              onClick={() => setShowToken(!showToken)}
              className="absolute right-2.5 top-1/2 -translate-y-1/2 p-1 rounded text-text-muted hover:text-text-primary transition-colors"
            >
              {showToken ? <EyeOff size={14} /> : <Eye size={14} />}
            </button>
          </div>

          {error && (
            <div id="auth-error" role="alert" className="flex items-center gap-2 text-xs text-status-error">
              <AlertCircle size={12} className="flex-shrink-0" />
              <span>{error}</span>
            </div>
          )}

          <Button
            type="submit"
            variant="primary"
            disabled={!token || isVerifying}
            className="w-full"
          >
            {isVerifying ? 'Verifying...' : 'Authenticate'}
          </Button>
          <p role="status" className="text-xs text-text-muted">{isVerifying ? 'Verifying credential with the gateway...' : ''}</p>
        </form>
      </div>
    </div>
  );
}
