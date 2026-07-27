import { useMemo, useState } from 'react';
import { AlertCircle, AlertTriangle, ArrowLeft, Check, Loader2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';
import {
  AuthError,
  fetchStatus,
  fetchTools,
  fetchToolCatalog,
  setServerToolsBatch,
  SetServerToolsError,
} from '../../lib/api';
import { planBulkAction, type BulkAction, type BulkUsageContext } from '../../lib/toolBulk';
import { formatRelativeTime } from '../../lib/time';
import { showToast } from '../ui/Toast';
import { Modal } from '../ui/Modal';
import type { MCPServerStatus, ToolUsageResponse } from '../../types';

type Scope = 'fleet' | 'server' | 'custom';
type Phase = 'configure' | 'confirm';
type ApplyStatus = 'idle' | 'applying' | 'done' | 'error';

interface FleetActionsProps {
  isOpen: boolean;
  onClose: () => void;
  servers: MCPServerStatus[];
  // The currently-selected server, so the action can be scoped to one server
  // (the per-server counterpart to the fleet-wide default).
  activeServerName: string;
  // Usage snapshot backing the disable-unused action (null until the first
  // fetch lands; the workspace's usage hook polls while this panel is open).
  usage: ToolUsageResponse | null;
  // Audit lookback window the plan classifies against, plus its human label.
  windowMs: number;
  windowLabel: string;
  // Fetch time of the usage snapshot (the injected "now" for classification).
  fetchedAt: number | null;
}

// FleetActions is the bulk-action surface for the Tools workspace. It resolves
// an action (expose-all / hide-pattern) over a scope (all servers or the active
// one) into a concrete plan, echoes the resolved count, gates the commit behind
// a consequence-stating confirm step, applies the change through the batch
// endpoint (a single reload), and reports a per-server summary.
//
// The confirm step is an inline phase of this one focus-trapped Modal rather
// than a nested dialog — two stacked focus traps (Modal + ConfirmDialog) fight
// over Tab, so a single trap keeps the whole flow keyboard-operable.
export function FleetActions({
  isOpen,
  onClose,
  servers,
  activeServerName,
  usage,
  windowMs,
  windowLabel,
  fetchedAt,
}: FleetActionsProps) {
  const [scope, setScope] = useState<Scope>('fleet');
  const [action, setAction] = useState<BulkAction>('expose-all');
  const [pattern, setPattern] = useState('');
  // Custom scope: an explicit subset of servers (empty = nothing targeted).
  const [customSel, setCustomSel] = useState<Set<string>>(new Set());
  const [phase, setPhase] = useState<Phase>('configure');
  const [status, setStatus] = useState<ApplyStatus>('idle');
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [summary, setSummary] = useState<{ reloaded: boolean; servers: string[] } | null>(null);

  const scopedServers = useMemo(() => {
    if (scope === 'server') return servers.filter((s) => s.name === activeServerName);
    if (scope === 'custom') return servers.filter((s) => customSel.has(s.name));
    return servers;
  }, [scope, servers, activeServerName, customSel]);

  // The usage context exists only once a snapshot has loaded; without it the
  // disable-unused plan stays empty rather than treating "no data" as unused.
  const usageCtx: BulkUsageContext | undefined = useMemo(
    () =>
      fetchedAt != null
        ? { usage: usage?.servers, windowMs, now: fetchedAt }
        : undefined,
    [usage, windowMs, fetchedAt],
  );

  const plan = useMemo(
    () => planBulkAction(scopedServers, action, pattern, usageCtx),
    [scopedServers, action, pattern, usageCtx],
  );

  const canApply = plan.entries.length > 0 && status !== 'applying';
  const showResult = status === 'done' || status === 'error';

  function handleClose() {
    setPhase('configure');
    setStatus('idle');
    setErrorMsg(null);
    setSummary(null);
    setPattern('');
    setCustomSel(new Set());
    onClose();
  }

  function toggleCustom(name: string) {
    setCustomSel((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }

  async function apply() {
    setStatus('applying');
    setErrorMsg(null);
    try {
      const resp = await setServerToolsBatch(
        plan.entries.map((e) => ({ name: e.name, tools: e.tools })),
      );
      // Reflect the change immediately; polling would otherwise lag.
      try {
        const [statusResp, toolsList, catalogList] = await Promise.all([
          fetchStatus(),
          fetchTools(),
          fetchToolCatalog(),
        ]);
        useStackStore.getState().setGatewayStatus(statusResp);
        useStackStore.getState().setTools(toolsList.tools);
        useStackStore.getState().setToolCatalog(catalogList.tools);
      } catch {
        /* ignore — the next poll will refresh */
      }
      setSummary({ reloaded: resp.reloaded, servers: resp.servers.map((s) => s.server) });
      setStatus('done');
      showToast('success', `Updated ${resp.servers.length} server${resp.servers.length === 1 ? '' : 's'}`);
      if (!resp.reloaded) {
        showToast('warning', 'Stack updated. Run "gridctl reload" or restart with --watch to apply.');
      }
    } catch (err) {
      if (err instanceof AuthError) {
        setErrorMsg('Authentication required.');
      } else if (err instanceof SetServerToolsError) {
        setErrorMsg(err.hint ? `${err.message} — ${err.hint}` : err.message);
      } else {
        setErrorMsg(err instanceof Error ? err.message : 'Batch update failed');
      }
      setStatus('error');
    }
  }

  return (
    <Modal isOpen={isOpen} onClose={handleClose} title="Fleet tool actions">
      <div className="space-y-4 text-sm">
        {showResult ? (
          <ResultView status={status} summary={summary} errorMsg={errorMsg} onClose={handleClose} />
        ) : phase === 'confirm' ? (
          <ConfirmView
            action={action}
            plan={plan}
            windowLabel={windowLabel}
            applying={status === 'applying'}
            onBack={() => setPhase('configure')}
            onConfirm={() => void apply()}
          />
        ) : (
          <ConfigureView
            servers={servers}
            scopedCount={scopedServers.length}
            activeServerName={activeServerName}
            scope={scope}
            onScope={setScope}
            customSel={customSel}
            onToggleCustom={toggleCustom}
            action={action}
            onAction={setAction}
            pattern={pattern}
            onPattern={setPattern}
            plan={plan}
            usageReady={fetchedAt != null}
            observedSince={usage?.observedSince}
            windowLabel={windowLabel}
            canApply={canApply}
            onReview={() => setPhase('confirm')}
            onCancel={handleClose}
          />
        )}
      </div>
    </Modal>
  );
}

type Plan = ReturnType<typeof planBulkAction>;

interface ConfigureViewProps {
  servers: MCPServerStatus[];
  scopedCount: number;
  activeServerName: string;
  scope: Scope;
  onScope: (s: Scope) => void;
  customSel: Set<string>;
  onToggleCustom: (name: string) => void;
  action: BulkAction;
  onAction: (a: BulkAction) => void;
  pattern: string;
  onPattern: (p: string) => void;
  plan: Plan;
  // False until the first usage snapshot lands; disable-unused can't plan
  // without it, so the action card is disabled with an explanatory caption.
  usageReady: boolean;
  observedSince?: string;
  windowLabel: string;
  canApply: boolean;
  onReview: () => void;
  onCancel: () => void;
}

function ConfigureView({
  servers,
  scopedCount,
  activeServerName,
  scope,
  onScope,
  customSel,
  onToggleCustom,
  action,
  onAction,
  pattern,
  onPattern,
  plan,
  usageReady,
  observedSince,
  windowLabel,
  canApply,
  onReview,
  onCancel,
}: ConfigureViewProps) {
  const scopeLabel =
    scope === 'server'
      ? activeServerName || 'the selected server'
      : scope === 'custom'
        ? `${customSel.size} selected server${customSel.size === 1 ? '' : 's'}`
        : `all ${servers.length} servers`;
  return (
    <>
      <fieldset className="space-y-1.5">
        <legend className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">Scope</legend>
        <div className="flex gap-2">
          <SegButton active={scope === 'fleet'} onClick={() => onScope('fleet')}>
            All {servers.length} servers
          </SegButton>
          <SegButton active={scope === 'server'} disabled={!activeServerName} onClick={() => onScope('server')}>
            {activeServerName || 'Selected server'} only
          </SegButton>
          <SegButton active={scope === 'custom'} onClick={() => onScope('custom')}>
            Selected servers…
          </SegButton>
        </div>
        {scope === 'custom' && (
          <div
            className="max-h-32 overflow-y-auto scrollbar-dark rounded-md border border-border/30 bg-background/40 px-2 py-1.5 space-y-0.5"
            role="group"
            aria-label="Servers to target"
          >
            {servers.map((s) => (
              <label
                key={s.name}
                className="flex items-center gap-2 px-1 py-0.5 rounded text-[11px] text-text-secondary hover:bg-surface-highlight/40 cursor-pointer"
              >
                <input
                  type="checkbox"
                  checked={customSel.has(s.name)}
                  onChange={() => onToggleCustom(s.name)}
                  className="accent-[var(--color-primary)]"
                />
                <span className="font-mono truncate">{s.name}</span>
              </label>
            ))}
          </div>
        )}
      </fieldset>

      <fieldset className="space-y-1.5">
        <legend className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">Action</legend>
        <div className="flex gap-2">
          <SegButton active={action === 'expose-all'} onClick={() => onAction('expose-all')}>
            Expose all tools
          </SegButton>
          <SegButton active={action === 'hide-pattern'} onClick={() => onAction('hide-pattern')}>
            Hide matching pattern
          </SegButton>
          <SegButton
            active={action === 'disable-unused'}
            disabled={!usageReady}
            onClick={() => onAction('disable-unused')}
          >
            Disable unused
          </SegButton>
        </div>
        {!usageReady && (
          <p className="text-[10px] text-text-muted/70">
            Disable unused needs the usage snapshot; loading it now (or usage is unavailable on
            this gateway).
          </p>
        )}
      </fieldset>

      {action === 'hide-pattern' && (
        <div className="space-y-1.5">
          <label htmlFor="fleet-pattern" className="block text-[11px] text-text-secondary">
            Glob pattern (e.g. <span className="font-mono text-text-primary">delete_*</span>) — hides matching
            tools, keeps the rest
          </label>
          <input
            id="fleet-pattern"
            value={pattern}
            onChange={(e) => onPattern(e.target.value)}
            placeholder="delete_*"
            spellCheck={false}
            className="w-full bg-background/60 border border-border/40 rounded-md px-2.5 py-1.5 text-sm font-mono text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50"
          />
        </div>
      )}

      <div className="rounded-md border border-border/30 bg-background/40 px-3 py-2 text-[11px] text-text-secondary" role="status">
        {action === 'expose-all' ? (
          plan.entries.length > 0 ? (
            <span>
              Exposes all tools on <span className="font-medium text-text-primary">{plan.entries.length}</span>{' '}
              of {scopedCount} server{scopedCount === 1 ? '' : 's'} (the rest already expose everything).
            </span>
          ) : (
            <span>Every targeted server already exposes all of its tools — nothing to change.</span>
          )
        ) : action === 'disable-unused' ? (
          plan.entries.length > 0 ? (
            <span>
              Disables <span className="font-medium text-text-primary">{plan.matchedTools}</span> tool
              {plan.matchedTools === 1 ? '' : 's'} with no recorded calls in the last {windowLabel} across{' '}
              <span className="font-medium text-text-primary">{plan.entries.length}</span> server
              {plan.entries.length === 1 ? '' : 's'}.
            </span>
          ) : (
            <span>No exposed tools are unused in the last {windowLabel} across {scopeLabel}.</span>
          )
        ) : !pattern.trim() ? (
          <span>Enter a pattern to preview the matched tools.</span>
        ) : plan.entries.length > 0 ? (
          <span>
            Matches <span className="font-medium text-text-primary">{plan.matchedTools}</span> tool
            {plan.matchedTools === 1 ? '' : 's'} across{' '}
            <span className="font-medium text-text-primary">{plan.entries.length}</span> server
            {plan.entries.length === 1 ? '' : 's'}.
          </span>
        ) : (
          <span>No exposed tools match across {scopeLabel}.</span>
        )}
        {action === 'disable-unused' && observedSince && (
          <span className="block mt-1 text-text-muted">
            Tracking since {formatRelativeTime(new Date(observedSince))}; tools with no recorded
            calls may predate it.
          </span>
        )}
        {plan.blocked.length > 0 && (
          <span className="block mt-1 text-status-pending">
            {plan.blocked.length} server{plan.blocked.length === 1 ? '' : 's'} skipped:{' '}
            {action === 'disable-unused'
              ? 'every exposed tool is unused, and at least one must stay exposed.'
              : 'every exposed tool matches, and at least one must stay exposed.'}
          </span>
        )}
      </div>

      <div className="flex items-center justify-end gap-2 pt-1">
        <button
          type="button"
          onClick={onCancel}
          className="rounded-md px-3 py-1.5 text-[11px] text-text-secondary hover:text-text-primary transition-colors"
        >
          Cancel
        </button>
        <button
          type="button"
          onClick={onReview}
          disabled={!canApply}
          className={cn(
            'inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[11px] font-medium border transition-colors',
            canApply
              ? 'bg-primary/20 text-primary border-primary/30 hover:bg-primary/30'
              : 'bg-surface-highlight/50 text-text-muted border-border/30 cursor-not-allowed',
          )}
        >
          Review &amp; apply ({plan.entries.length})
        </button>
      </div>
    </>
  );
}

interface ConfirmViewProps {
  action: BulkAction;
  plan: Plan;
  windowLabel: string;
  applying: boolean;
  onBack: () => void;
  onConfirm: () => void;
}

function ConfirmView({ action, plan, windowLabel, applying, onBack, onConfirm }: ConfirmViewProps) {
  const n = plan.entries.length;
  return (
    <>
      <div className="flex items-start gap-2.5 rounded-md border border-status-pending/40 bg-status-pending/[0.06] px-3 py-3" role="status">
        <AlertTriangle size={14} className="text-status-pending flex-shrink-0 mt-0.5" aria-hidden="true" />
        <p className="text-[12px] text-text-secondary leading-relaxed">
          {action === 'expose-all' ? 'Expose all tools on ' : 'Update '}
          <span className="font-mono text-text-primary">{n}</span> server{n === 1 ? '' : 's'}
          {action === 'hide-pattern' && (
            <>
              {' '}
              (hiding <span className="font-mono text-text-primary">{plan.matchedTools}</span> tool
              {plan.matchedTools === 1 ? '' : 's'})
            </>
          )}
          {action === 'disable-unused' && (
            <>
              {' '}
              (disabling <span className="font-mono text-text-primary">{plan.matchedTools}</span> tool
              {plan.matchedTools === 1 ? '' : 's'} with no recorded calls in the last {windowLabel})
            </>
          )}
          ? This writes the stack file once and triggers a <span className="font-medium">single reload</span>{' '}
          of {n === 1 ? 'that server' : `${n} servers`}.
        </p>
      </div>

      {action === 'disable-unused' && (
        <div className="max-h-40 overflow-y-auto scrollbar-dark rounded-md border border-border/30 bg-background/40 px-3 py-2 space-y-1.5">
          {plan.entries.map((e) => (
            <div key={e.name} className="text-[10px] leading-relaxed">
              <span className="font-mono text-text-primary">{e.name}</span>{' '}
              <span className="text-text-muted">
                ({e.hidden.length} tool{e.hidden.length === 1 ? '' : 's'}):
              </span>{' '}
              <span className="font-mono text-text-muted break-words">{e.hidden.join(', ')}</span>
            </div>
          ))}
        </div>
      )}

      <div className="flex items-center justify-end gap-2 pt-1">
        <button
          type="button"
          onClick={onBack}
          disabled={applying}
          className="inline-flex items-center gap-1 rounded-md px-3 py-1.5 text-[11px] text-text-secondary hover:text-text-primary transition-colors disabled:opacity-50"
        >
          <ArrowLeft size={11} aria-hidden="true" />
          Back
        </button>
        <button
          type="button"
          onClick={onConfirm}
          disabled={applying}
          className="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[11px] font-medium border border-status-pending/40 bg-status-pending/10 text-status-pending hover:bg-status-pending/20 transition-colors disabled:opacity-50"
        >
          {applying ? (
            <>
              <Loader2 size={11} className="animate-spin" />
              Applying…
            </>
          ) : (
            'Apply & reload'
          )}
        </button>
      </div>
    </>
  );
}

interface ResultViewProps {
  status: ApplyStatus;
  summary: { reloaded: boolean; servers: string[] } | null;
  errorMsg: string | null;
  onClose: () => void;
}

function ResultView({ status, summary, errorMsg, onClose }: ResultViewProps) {
  return (
    <>
      {status === 'done' && summary && (
        <div className="rounded-md border border-status-running/30 bg-status-running/[0.06] px-3 py-2 space-y-1" role="status">
          <p className="flex items-center gap-1.5 text-[11px] font-medium text-status-running">
            <Check size={12} aria-hidden="true" />
            Updated {summary.servers.length} server{summary.servers.length === 1 ? '' : 's'}
            {summary.reloaded ? ' · reloaded once' : ' · reload pending'}
          </p>
          <ul className="text-[10px] text-text-muted font-mono space-y-0.5">
            {summary.servers.map((name) => (
              <li key={name}>✓ {name}</li>
            ))}
          </ul>
        </div>
      )}
      {status === 'error' && errorMsg && (
        <div className="flex items-start gap-2 rounded-md border border-status-error/40 bg-status-error/[0.06] px-3 py-2" role="alert">
          <AlertCircle size={12} className="text-status-error flex-shrink-0 mt-0.5" aria-hidden="true" />
          <p className="text-[11px] text-status-error">{errorMsg}</p>
        </div>
      )}
      <div className="flex items-center justify-end pt-1">
        <button
          type="button"
          onClick={onClose}
          className="rounded-md px-3 py-1.5 text-[11px] font-medium border border-border/40 bg-background/40 text-text-secondary hover:text-text-primary hover:border-border transition-colors"
        >
          Close
        </button>
      </div>
    </>
  );
}

interface SegButtonProps {
  active: boolean;
  disabled?: boolean;
  onClick: () => void;
  children: React.ReactNode;
}

function SegButton({ active, disabled, onClick, children }: SegButtonProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-pressed={active}
      className={cn(
        'flex-1 rounded-md px-2.5 py-1.5 text-[11px] font-medium border transition-colors',
        disabled && 'opacity-40 cursor-not-allowed',
        active
          ? 'bg-primary/15 text-primary border-primary/40'
          : 'bg-background/40 text-text-secondary border-border/40 hover:border-border hover:text-text-primary',
      )}
    >
      {children}
    </button>
  );
}

export default FleetActions;
