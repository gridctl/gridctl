// SidebarTelemetrySection: per-server persistence controls. Each signal is a
// group of Inherit, On, and Off buttons; choosing one sends a single PATCH
// that sets or clears that override on the server's telemetry block.
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
  fetchStackSpec,
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
  readTriState,
  triStateToPatch,
  type TriState,
} from '../../lib/telemetry-config';
import { useStackStore } from '../../stores/useStackStore';
import type { ResolvedTelemetry, TelemetrySignal } from '../../types';
import { describeError, isCancellation } from '../../lib/describeError';

const SIGNALS: TelemetrySignal[] = ['logs', 'metrics', 'traces'];

const OPTIONS: { value: TriState; label: string }[] = [
  { value: 'inherit', label: 'Inherit' },
  { value: 'on', label: 'On' },
  { value: 'off', label: 'Off' },
];

interface Props {
  serverName: string;
}

// Selections made since `base` was the current config. The store installs a
// new config object on every parse, so a refreshed spec retires the record.
interface OptimisticSelection {
  base: ResolvedTelemetry;
  states: Partial<Record<TelemetrySignal, TriState>>;
}

export function SidebarTelemetrySection({ serverName }: Props) {
  return <TelemetrySectionBody key={serverName} serverName={serverName} />;
}

function TelemetrySectionBody({ serverName }: Props) {
  const config = useTelemetryConfig();
  const inventory = useInventory();
  const stackName = useStackStore((s) => s.stackName);
  const [pending, setPending] = useState<TelemetrySignal | 'reset' | null>(null);
  const [optimistic, setOptimistic] = useState<OptimisticSelection | null>(null);
  const [wipeOpen, setWipeOpen] = useState(false);

  const serverInventory = useMemo(
    () => inventoryByServer(inventory, serverName),
    [inventory, serverName],
  );

  const current = optimistic && optimistic.base === config ? optimistic.states : {};
  const displayed = {} as Record<TelemetrySignal, TriState>;
  for (const sig of SIGNALS) {
    displayed[sig] = current[sig] ?? readTriState(config, serverName, sig);
  }
  const hasAnyOverride = SIGNALS.some((s) => displayed[s] !== 'inherit');

  const storagePath = stackName
    ? `~/.gridctl/telemetry/${stackName}/${serverName}/`
    : `~/.gridctl/telemetry/<stack>/${serverName}/`;

  async function setSignal(signal: TelemetrySignal, target: TriState) {
    if (pending !== null || target === displayed[signal]) return;
    const base = config;
    const previous = current[signal];
    setOptimistic((prev) =>
      prev && prev.base === base
        ? { base, states: { ...prev.states, [signal]: target } }
        : { base, states: { [signal]: target } },
    );
    setPending(signal);
    try {
      let resp;
      try {
        resp = await updateServerTelemetry(serverName, {
          persist: { [signal]: triStateToPatch(target) },
        });
      } catch (err) {
        setOptimistic((prev) => {
          if (!prev || prev.base !== base) return prev;
          const states = { ...prev.states };
          if (previous === undefined) delete states[signal];
          else states[signal] = previous;
          return { base, states };
        });
        handleError(err);
        return;
      }
      useTelemetryStore.getState().setInventory(resp.inventory);
      showToast('success', triStateToast(signal, target, serverName));
      await refreshSpec();
    } finally {
      setPending(null);
    }
  }

  async function resetOverrides() {
    if (pending !== null) return;
    setPending('reset');
    try {
      let resp;
      try {
        resp = await updateServerTelemetry(serverName, { persist: null });
      } catch (err) {
        handleError(err);
        return;
      }
      setOptimistic({
        base: useTelemetryStore.getState().config,
        states: { logs: 'inherit', metrics: 'inherit', traces: 'inherit' },
      });
      useTelemetryStore.getState().setInventory(resp.inventory);
      showToast('success', `Telemetry overrides cleared for ${serverName}`);
      await refreshSpec();
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
      <div className="text-3xs text-text-muted font-mono break-all bg-background/50 px-2 py-1 rounded-md border border-border-subtle">
        {storagePath}
      </div>

      <ul className="space-y-1">
        {SIGNALS.map((sig) => (
          <li key={sig}>
            <TriStateRow
              signal={sig}
              state={displayed[sig]}
              globalDefault={config.global[sig] === true}
              busy={pending === sig || pending === 'reset'}
              disabled={pending !== null}
              onSelect={(target) => setSignal(sig, target)}
            />
          </li>
        ))}
      </ul>

      <div className="flex items-center gap-2 pt-1">
        {hasAnyOverride && (
          <button
            type="button"
            onClick={resetOverrides}
            disabled={pending !== null}
            className={cn(
              'inline-flex items-center gap-1.5 px-2 py-1 rounded-md',
              'text-2xs font-medium text-text-secondary',
              'hover:bg-surface-highlight transition-colors',
              pending !== null && 'opacity-60 cursor-wait',
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
            'text-2xs font-medium transition-colors',
            serverInventory.length === 0
              ? 'text-text-muted cursor-not-allowed'
              : 'text-status-error hover:bg-status-error/10',
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

// A failed refresh is not reported; the slow poll catches up and the
// optimistic selection holds until then.
async function refreshSpec() {
  try {
    const spec = await fetchStackSpec();
    useTelemetryStore.getState().setRawSpec(spec.content);
  } catch {
    // ignored
  }
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
  if (!isCancellation(err)) {
    const described = describeError(err, 'Telemetry update');
    showToast('error', described.text, { detail: described.fullDetail });
  }
}

interface RowProps {
  signal: TelemetrySignal;
  state: TriState;
  globalDefault: boolean;
  busy: boolean;
  disabled: boolean;
  onSelect: (target: TriState) => void;
}

function TriStateRow({ signal, state, globalDefault, busy, disabled, onSelect }: RowProps) {
  const label = signal[0].toUpperCase() + signal.slice(1);
  const isOn = state === 'on';
  const isOff = state === 'off';
  const isInherit = state === 'inherit';

  return (
    <div
      role="group"
      aria-label={`${label} persistence`}
      aria-busy={busy || undefined}
      className="flex items-center justify-between gap-2 px-2 py-1.5 rounded-lg"
    >
      <span className="flex items-center gap-2 min-w-0">
        <span
          aria-hidden
          className={cn(
            'inline-block shrink-0 w-2.5 h-2.5 rounded-full border transition-all duration-200',
            isOn && 'bg-status-running border-status-running shadow-[0_0_6px_--alpha(var(--color-status-running)/50%)]',
            isOff && 'bg-transparent border-status-pending',
            isInherit && (globalDefault
              ? 'bg-status-running/30 border-status-running/40'
              : 'bg-text-muted/20 border-text-muted/40'),
          )}
        />
        <span
          className={cn(
            'text-sm capitalize truncate',
            isOn && 'text-text-primary',
            isOff && 'text-status-pending line-through',
            isInherit && 'text-text-secondary',
          )}
        >
          {label}
        </span>
        <span className="text-3xs font-mono text-text-muted truncate">
          global: {globalDefault ? 'on' : 'off'}
        </span>
      </span>
      <span className="inline-flex shrink-0 items-center rounded-lg border border-border/40 bg-background/60 p-0.5">
        {OPTIONS.map((opt) => {
          const selected = state === opt.value;
          return (
            <button
              key={opt.value}
              type="button"
              aria-pressed={selected}
              disabled={disabled}
              onClick={() => onSelect(opt.value)}
              className={cn(
                'px-1.5 py-0.5 rounded-md text-3xs font-medium transition-colors focus-visible:-outline-offset-2',
                selected
                  ? cn(
                      opt.value === 'on' && 'bg-status-running/15 text-status-running',
                      opt.value === 'off' && 'bg-status-pending/15 text-status-pending',
                      opt.value === 'inherit' && 'bg-primary/15 text-primary',
                    )
                  : 'text-text-muted enabled:hover:text-text-secondary enabled:hover:bg-surface-highlight/40',
                disabled && (busy ? 'cursor-wait' : 'cursor-not-allowed'),
                disabled && !selected && 'opacity-60',
              )}
            >
              {opt.label}
            </button>
          );
        })}
      </span>
    </div>
  );
}
