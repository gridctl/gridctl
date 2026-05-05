// SidebarTelemetrySection — per-server tri-state controls. Each row
// cycles inherit → on → off → inherit; the explicit states map to PATCH
// bodies that set/clear the override on the server's telemetry block.
// "Reset to global" wipes all overrides via the persist:null idiom and
// the per-server "Wipe data" button reuses WipeTelemetryDialog with a
// scope filtered to this server.
import { useMemo, useState } from 'react';
import { RotateCcw, Trash2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { showToast } from '../ui/Toast';
import { WipeTelemetryDialog } from './WipeTelemetryDialog';
import {
  StackModifiedError,
  updateServerTelemetry,
  wipeTelemetry,
} from '../../lib/api';
import {
  inventoryByServer,
  useInventory,
  useTelemetryConfig,
  useTelemetryStore,
} from '../../stores/useTelemetryStore';
import {
  nextTriState,
  readTriState,
  triStateToPatch,
  type TriState,
} from '../../lib/telemetry-config';
import { useStackStore } from '../../stores/useStackStore';
import type { TelemetrySignal } from '../../types';

const SIGNALS: TelemetrySignal[] = ['logs', 'metrics', 'traces'];

interface Props {
  serverName: string;
}

export function SidebarTelemetrySection({ serverName }: Props) {
  const config = useTelemetryConfig();
  const inventory = useInventory();
  const stackName = useStackStore((s) => s.stackName);
  const [pending, setPending] = useState<TelemetrySignal | 'reset' | null>(null);
  const [wipeOpen, setWipeOpen] = useState(false);

  const serverInventory = useMemo(
    () => inventoryByServer(inventory, serverName),
    [inventory, serverName],
  );
  const override = config.servers[serverName] ?? {};
  const hasAnyOverride = SIGNALS.some((s) => override[s] === true || override[s] === false);

  const storagePath = stackName
    ? `~/.gridctl/telemetry/${stackName}/${serverName}/`
    : `~/.gridctl/telemetry/<stack>/${serverName}/`;

  async function cycleSignal(signal: TelemetrySignal) {
    const current = readTriState(config, serverName, signal);
    const next = nextTriState(current);
    setPending(signal);
    try {
      const resp = await updateServerTelemetry(serverName, {
        persist: { [signal]: triStateToPatch(next) },
      });
      useTelemetryStore.getState().setInventory(resp.inventory);
      showToast('success', triStateToast(signal, next, serverName));
    } catch (err) {
      handleError(err);
    } finally {
      setPending(null);
    }
  }

  async function resetOverrides() {
    setPending('reset');
    try {
      const resp = await updateServerTelemetry(serverName, { persist: null });
      useTelemetryStore.getState().setInventory(resp.inventory);
      showToast('success', `Telemetry overrides cleared for ${serverName}`);
    } catch (err) {
      handleError(err);
    } finally {
      setPending(null);
    }
  }

  async function confirmWipe() {
    setWipeOpen(false);
    try {
      const resp = await wipeTelemetry({ server: serverName });
      useTelemetryStore.getState().setInventory(resp.inventory);
      showToast('success', `Persisted telemetry wiped for ${serverName}`);
    } catch (err) {
      handleError(err);
    }
  }

  return (
    <div className="space-y-3">
      <div className="text-[10px] text-text-muted font-mono break-all bg-background/50 px-2 py-1 rounded-md border border-border-subtle">
        {storagePath}
      </div>

      <ul className="space-y-1">
        {SIGNALS.map((sig) => {
          const state = readTriState(config, serverName, sig);
          const globalDefault = config.global[sig] === true;
          const isBusy = pending === sig;
          return (
            <li key={sig}>
              <TriStateRow
                signal={sig}
                state={state}
                globalDefault={globalDefault}
                busy={isBusy}
                onClick={() => cycleSignal(sig)}
              />
            </li>
          );
        })}
      </ul>

      <div className="flex items-center gap-2 pt-1">
        {hasAnyOverride && (
          <button
            type="button"
            onClick={resetOverrides}
            disabled={pending === 'reset'}
            className={cn(
              'inline-flex items-center gap-1.5 px-2 py-1 rounded-md',
              'text-[11px] font-medium text-text-secondary',
              'hover:bg-surface-highlight transition-colors',
              'focus:outline-none focus:ring-2 focus:ring-primary/30',
            )}
          >
            <RotateCcw size={11} />
            Reset to global
          </button>
        )}
        <button
          type="button"
          onClick={() => setWipeOpen(true)}
          disabled={serverInventory.length === 0}
          className={cn(
            'inline-flex items-center gap-1.5 ml-auto px-2 py-1 rounded-md',
            'text-[11px] font-medium transition-colors',
            serverInventory.length === 0
              ? 'text-text-muted/60 cursor-not-allowed'
              : 'text-status-error hover:bg-status-error/10',
            'focus:outline-none focus:ring-2 focus:ring-status-error/30',
          )}
        >
          <Trash2 size={11} />
          Wipe data
        </button>
      </div>

      <WipeTelemetryDialog
        isOpen={wipeOpen}
        onClose={() => setWipeOpen(false)}
        onConfirm={confirmWipe}
        scope={serverInventory}
        title={`Wipe persisted data for ${serverName}`}
        subject={`for ${serverName}`}
      />
    </div>
  );
}

function triStateToast(signal: TelemetrySignal, state: TriState, server: string): string {
  const label = signal[0].toUpperCase() + signal.slice(1);
  switch (state) {
    case 'on':
      return `${label} persistence enabled for ${server}`;
    case 'off':
      return `${label} persistence disabled for ${server}`;
    case 'inherit':
      return `${label} persistence reverts to global for ${server}`;
  }
}

function handleError(err: unknown) {
  if (err instanceof StackModifiedError) {
    showToast('warning', err.message, { duration: 6000 });
    return;
  }
  showToast('error', err instanceof Error ? err.message : 'Telemetry update failed');
}

interface RowProps {
  signal: TelemetrySignal;
  state: TriState;
  globalDefault: boolean;
  busy: boolean;
  onClick: () => void;
}

function TriStateRow({ signal, state, globalDefault, busy, onClick }: RowProps) {
  const label = signal[0].toUpperCase() + signal.slice(1);
  const isOn = state === 'on';
  const isOff = state === 'off';
  const isInherit = state === 'inherit';

  // ariaPressed reflects the *effective* state — accessibility tools should
  // see "pressed" whenever persistence is on, regardless of whether that's
  // due to inheritance or an explicit override.
  const effectivePressed = state === 'on' || (state === 'inherit' && globalDefault);

  return (
    <button
      type="button"
      onClick={onClick}
      disabled={busy}
      aria-pressed={effectivePressed}
      title={triStateTitle(state, globalDefault)}
      className={cn(
        'w-full flex items-center justify-between gap-3 px-2 py-1.5 rounded-lg',
        'transition-all duration-200 group',
        'hover:bg-surface-highlight/60',
        'focus:outline-none focus:ring-2 focus:ring-primary/30',
        busy && 'opacity-60 cursor-wait',
      )}
    >
      <span className="flex items-center gap-2">
        <span
          aria-hidden
          className={cn(
            'inline-block w-2.5 h-2.5 rounded-full border transition-all duration-200',
            isOn && 'bg-status-running border-status-running shadow-[0_0_6px_rgba(16,185,129,0.5)]',
            isOff && 'bg-transparent border-amber-400',
            isInherit && (globalDefault
              ? 'bg-status-running/30 border-status-running/40'
              : 'bg-text-muted/20 border-text-muted/40'),
          )}
        />
        <span
          className={cn(
            'text-sm capitalize',
            isOn && 'text-text-primary',
            isOff && 'text-amber-400 line-through',
            isInherit && 'text-text-secondary',
          )}
        >
          {label}
        </span>
      </span>
      <span
        className={cn(
          'text-[10px] font-mono uppercase tracking-wider px-1.5 py-0.5 rounded',
          isOn && 'bg-status-running/10 text-status-running border border-status-running/20',
          isOff && 'bg-amber-400/10 text-amber-400 border border-amber-400/20',
          isInherit && 'bg-surface-elevated text-text-muted border border-border/40',
        )}
      >
        {triStateBadge(state, globalDefault)}
      </span>
    </button>
  );
}

function triStateBadge(state: TriState, globalDefault: boolean): string {
  switch (state) {
    case 'on':
      return 'On';
    case 'off':
      return 'Off';
    case 'inherit':
      return globalDefault ? 'Inherit · On' : 'Inherit · Off';
  }
}

function triStateTitle(state: TriState, globalDefault: boolean): string {
  const next = nextTriState(state);
  const inheritWord = globalDefault ? 'on' : 'off';
  const stateWord =
    state === 'inherit' ? `inheriting global (${inheritWord})` : `explicitly ${state}`;
  return `${stateWord} — click to set ${next}`;
}
