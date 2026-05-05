// HeaderTelemetryPill — the "Persistence: …" affordance in the header
// status row. Mirrors the visual rhythm of the neighbouring stack-name
// and server-count pills: glass surface, primary-color icon, compact
// label. Click opens a popover with the three signal toggles and a
// destructive "Wipe all" button; mutations call the API helpers and
// hand the refreshed inventory back to the store.
import { useEffect, useRef, useState } from 'react';
import { Database, Power, PowerOff, Trash2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { showToast } from '../ui/Toast';
import { WipeTelemetryDialog } from './WipeTelemetryDialog';
import {
  StackModifiedError,
  updateStackTelemetry,
  wipeTelemetry,
} from '../../lib/api';
import {
  useInventory,
  useTelemetryConfig,
  useTelemetryStore,
} from '../../stores/useTelemetryStore';
import type { TelemetrySignal } from '../../types';

const SIGNALS: TelemetrySignal[] = ['logs', 'metrics', 'traces'];

// Uppercase three-letter abbreviations keep the pill compact when all
// three signals are on (`Persistence: Logs+Metrics+Traces` would be
// fine in English but starts to overflow when other status pills get
// busy). Keep them title-case to match other pill copy in the header.
const ABBREV: Record<TelemetrySignal, string> = {
  logs: 'Logs',
  metrics: 'Metrics',
  traces: 'Traces',
};

export function buildPillLabel(global: Partial<Record<TelemetrySignal, boolean>>): string {
  const on = SIGNALS.filter((s) => global[s] === true);
  if (on.length === 0) return 'Persistence: Off';
  return `Persistence: ${on.map((s) => ABBREV[s]).join('+')}`;
}

export function HeaderTelemetryPill() {
  const config = useTelemetryConfig();
  const inventory = useInventory();
  const [open, setOpen] = useState(false);
  const [wipeOpen, setWipeOpen] = useState(false);
  const [pending, setPending] = useState<TelemetrySignal | 'wipe' | null>(null);
  const popoverRef = useRef<HTMLDivElement | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);

  const anyOn = SIGNALS.some((s) => config.global[s] === true);
  const label = buildPillLabel(config.global);

  // Close the popover on outside click or Escape so it composes with the
  // rest of the header's overlapping affordances (vault panel, wizard).
  useEffect(() => {
    if (!open) return;
    const onPointer = (e: MouseEvent) => {
      const t = e.target as Node | null;
      if (!t) return;
      if (popoverRef.current?.contains(t) || triggerRef.current?.contains(t)) return;
      setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    document.addEventListener('mousedown', onPointer);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onPointer);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  async function toggleSignal(signal: TelemetrySignal) {
    const current = config.global[signal] === true;
    setPending(signal);
    try {
      const resp = await updateStackTelemetry({ persist: { [signal]: !current } });
      useTelemetryStore.getState().setInventory(resp.inventory);
      showToast('success', `${ABBREV[signal]} persistence ${!current ? 'enabled' : 'disabled'}`);
    } catch (err) {
      handleError(err);
    } finally {
      setPending(null);
    }
  }

  async function confirmWipe() {
    setWipeOpen(false);
    setPending('wipe');
    try {
      const resp = await wipeTelemetry();
      useTelemetryStore.getState().setInventory(resp.inventory);
      showToast('success', 'Persisted telemetry wiped');
    } catch (err) {
      handleError(err);
    } finally {
      setPending(null);
    }
  }

  return (
    <div className="relative">
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="dialog"
        aria-expanded={open}
        title={label}
        className={cn(
          'flex items-center gap-2 px-3 py-1.5 rounded-full',
          'bg-surface-elevated/60 backdrop-blur-sm border transition-all duration-200',
          anyOn
            ? 'border-status-running/30 hover:border-status-running/50'
            : 'border-border/50 hover:border-text-muted/40',
          'focus:outline-none focus:ring-2 focus:ring-primary/30',
        )}
      >
        <Database size={12} className={anyOn ? 'text-status-running' : 'text-text-muted'} />
        <span
          className={cn(
            'text-xs font-medium',
            anyOn ? 'text-status-running' : 'text-text-secondary',
          )}
        >
          {label}
        </span>
      </button>

      {open && (
        <div
          ref={popoverRef}
          role="dialog"
          aria-label="Stack telemetry persistence"
          className={cn(
            'absolute top-full mt-2 right-0 z-40 w-64',
            'glass-panel-elevated rounded-xl p-3 shadow-lg',
          )}
        >
          <div className="text-[10px] uppercase tracking-widest font-medium text-text-muted px-1 pb-2">
            Stack-global signals
          </div>
          <ul className="space-y-1">
            {SIGNALS.map((sig) => {
              const isOn = config.global[sig] === true;
              const isBusy = pending === sig;
              return (
                <li key={sig}>
                  <button
                    type="button"
                    onClick={() => toggleSignal(sig)}
                    disabled={isBusy}
                    aria-pressed={isOn}
                    className={cn(
                      'w-full flex items-center justify-between gap-3 px-2 py-2 rounded-lg',
                      'transition-all duration-200 group',
                      'hover:bg-surface-highlight/60',
                      'focus:outline-none focus:ring-2 focus:ring-primary/30',
                      isBusy && 'opacity-60 cursor-wait',
                    )}
                  >
                    <span className="flex items-center gap-2">
                      {isOn ? (
                        <Power size={12} className="text-status-running" />
                      ) : (
                        <PowerOff size={12} className="text-text-muted group-hover:text-status-running transition-colors" />
                      )}
                      <span className="text-sm text-text-primary capitalize">{sig}</span>
                    </span>
                    <span
                      className={cn(
                        'text-[10px] font-mono uppercase tracking-wider px-1.5 py-0.5 rounded',
                        isOn
                          ? 'bg-status-running/10 text-status-running border border-status-running/20'
                          : 'bg-surface-elevated text-text-muted border border-border/40',
                      )}
                    >
                      {isOn ? 'On' : 'Off'}
                    </span>
                  </button>
                </li>
              );
            })}
          </ul>
          <div className="mt-2 pt-2 border-t border-border-subtle">
            <button
              type="button"
              onClick={() => setWipeOpen(true)}
              disabled={pending === 'wipe' || inventory.length === 0}
              className={cn(
                'w-full flex items-center justify-center gap-2 px-2 py-2 rounded-lg',
                'text-xs font-medium transition-all duration-200',
                inventory.length === 0
                  ? 'text-text-muted/60 cursor-not-allowed'
                  : 'text-status-error hover:bg-status-error/10',
                'focus:outline-none focus:ring-2 focus:ring-status-error/30',
              )}
            >
              <Trash2 size={12} />
              Wipe all persisted data
            </button>
          </div>
        </div>
      )}

      <WipeTelemetryDialog
        isOpen={wipeOpen}
        onClose={() => setWipeOpen(false)}
        onConfirm={confirmWipe}
        scope={inventory}
        title="Wipe all persisted telemetry"
      />
    </div>
  );
}

function handleError(err: unknown) {
  if (err instanceof StackModifiedError) {
    showToast('warning', err.message, {
      duration: 6000,
      // The hint guides the user to the Reload affordance which already
      // exists in the header; we surface it as the action label so the
      // toast doesn't disappear before the operator reads it.
      action: err.hint
        ? { label: 'Got it', onClick: () => undefined }
        : undefined,
    });
    return;
  }
  showToast('error', err instanceof Error ? err.message : 'Telemetry update failed');
}
