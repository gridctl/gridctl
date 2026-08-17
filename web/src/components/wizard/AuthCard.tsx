import { useEffect, useId, useRef, useState } from 'react';
import { AlertCircle, ChevronDown, ChevronRight, Eye, EyeOff, KeyRound, ShieldCheck, X } from 'lucide-react';
import { cn } from '../../lib/cn';
import { VariablesPopover } from './VariablesPopover';
import type { AuthCardController } from './useAuthCard';

interface AuthCardProps {
  controller: AuthCardController;
  /** SSH URLs authenticate through the agent; the card says so and collects nothing. */
  ssh: boolean;
}

/**
 * Collapsible credentials card shared by the skill and pack import wizards.
 *
 * Collapsed and optional by default: most repositories are public, so leading
 * with a credentials question would tax every import for the minority case. It
 * opens on its own when a scan fails in an auth-shaped way.
 */
export function AuthCard({ controller, ssh }: AuthCardProps) {
  const {
    open,
    setOpen,
    mode,
    setMode,
    vaultRef,
    setVaultRef,
    pasteToken,
    setPasteToken,
    banner,
  } = controller;

  // The card renders in more than one place, so the panel id has to be unique
  // per instance or aria-controls would point at whichever mounted first.
  const panelId = useId();
  const tokenId = useId();
  const bannerRef = useRef<HTMLDivElement>(null);
  const [revealed, setRevealed] = useState(false);

  // Move focus to the banner, not into a field the user did not ask for
  // (GOV.UK error-summary pattern): it announces the reason and leaves the
  // next move to them.
  useEffect(() => {
    if (banner && open) bannerRef.current?.focus();
  }, [banner, open]);

  return (
    <div
      data-testid="auth-card"
      className={cn(
        'rounded-lg border border-border/30 bg-white/[0.02] transition-colors',
        open && 'border-border/50 bg-white/[0.03]',
      )}
    >
      <button
        type="button"
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-3 py-2 text-[11px] text-text-secondary hover:text-text-primary transition-colors"
      >
        <ShieldCheck size={12} className="text-primary/70" />
        <span className="font-medium">Authentication</span>
        <span className="text-text-muted text-[10px]">
          {ssh ? '— using ssh-agent' : open ? '' : '(optional)'}
        </span>
        <span className="ml-auto text-text-muted">
          {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        </span>
      </button>

      {open && (
        <div id={panelId} className="px-3 pb-3 pt-1 space-y-2.5 border-t border-border/20">
          {banner && (
            <div
              ref={bannerRef}
              role="alert"
              tabIndex={-1}
              className="flex items-start gap-1.5 text-[10px] text-status-pending bg-status-pending/5 border border-status-pending/20 rounded-md px-2 py-1.5 focus:outline-none focus:ring-1 focus:ring-status-pending/40"
            >
              <AlertCircle size={10} className="mt-0.5 flex-shrink-0" />
              <span>{banner}</span>
            </div>
          )}

          {ssh ? (
            <div className="flex items-center gap-1.5 text-[10px] text-text-muted">
              <KeyRound size={10} />
              <span>Using ssh-agent — no token needed.</span>
            </div>
          ) : (
            <>
              {/* Mode selector — real radios for accessibility */}
              <fieldset className="flex gap-1 text-[10px]">
                <legend className="sr-only">Authentication method</legend>
                {(
                  [
                    { v: 'vault', label: 'Vault secret', sub: 'recommended' },
                    { v: 'token', label: 'Paste token', sub: 'not saved' },
                  ] as const
                ).map((opt) => (
                  <label
                    key={opt.v}
                    className={cn(
                      'flex-1 cursor-pointer rounded-md px-2 py-1.5 border text-center transition-colors',
                      mode === opt.v
                        ? 'bg-primary/10 border-primary/30 text-primary'
                        : 'bg-white/[0.02] border-white/[0.06] text-text-muted hover:text-text-secondary',
                    )}
                  >
                    <input
                      type="radio"
                      name={`auth-mode-${panelId}`}
                      value={opt.v}
                      checked={mode === opt.v}
                      onChange={() => setMode(opt.v)}
                      className="sr-only"
                    />
                    <span className="block font-medium">{opt.label}</span>
                    <span className="block text-[9px] opacity-70">{opt.sub}</span>
                  </label>
                ))}
              </fieldset>

              {mode === 'vault' ? (
                <div className="flex items-center gap-2">
                  {vaultRef ? (
                    <div className="flex-1 flex items-center justify-between gap-2 bg-background/60 border border-border/40 rounded-md px-2 py-1.5 text-[10px] font-mono text-text-primary">
                      <span className="truncate">{vaultRef}</span>
                      <button
                        type="button"
                        onClick={() => setVaultRef('')}
                        className="text-text-muted hover:text-status-error transition-colors"
                        aria-label="Clear vault selection"
                      >
                        <X size={11} />
                      </button>
                    </div>
                  ) : (
                    <div className="flex-1 text-[10px] text-text-muted italic px-1">
                      Choose a vault key →
                    </div>
                  )}
                  <VariablesPopover onSelect={setVaultRef} />
                </div>
              ) : (
                <>
                  <label htmlFor={tokenId} className="sr-only">
                    Personal access token
                  </label>
                  <div className="relative">
                    <input
                      id={tokenId}
                      type={revealed ? 'text' : 'password'}
                      value={pasteToken}
                      onChange={(e) => setPasteToken(e.target.value)}
                      placeholder="Personal Access Token"
                      autoComplete="off"
                      spellCheck={false}
                      data-1p-ignore
                      data-lpignore="true"
                      data-bwignore
                      className="w-full bg-background/60 border border-border/40 rounded-md pl-2 pr-8 py-1.5 text-[11px] font-mono focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50 transition-colors"
                    />
                    <button
                      type="button"
                      onClick={() => setRevealed((v) => !v)}
                      aria-pressed={revealed}
                      aria-label={revealed ? 'Hide token' : 'Show token'}
                      className="absolute right-1.5 top-1/2 -translate-y-1/2 p-1 text-text-muted hover:text-text-secondary transition-colors"
                    >
                      {revealed ? <EyeOff size={11} /> : <Eye size={11} />}
                    </button>
                  </div>
                  <p className="text-[10px] text-text-muted">
                    Used once for this request — not saved anywhere.
                  </p>
                </>
              )}
            </>
          )}
        </div>
      )}
    </div>
  );
}
