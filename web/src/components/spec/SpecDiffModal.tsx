import { useEffect, useCallback, useId, useState } from 'react';
import { createPortal } from 'react-dom';
import { AlertCircle, Loader2, Minus, Pencil, Plus, X } from 'lucide-react';
import { cn } from '../../lib/cn';
import { fetchStackHealth, fetchStackPlan } from '../../lib/api';
import { describeError } from '../../lib/describeError';
import { useFocusTrap } from '../../hooks/useFocusTrap';
import { useSpecStore } from '../../stores/useSpecStore';
import { Button } from '../ui/Button';
import type { DiffAction, DiffItem, PlanDiff } from '../../types';

const KIND_ORDER = ['mcp-server', 'resource', 'gateway', 'network'];

const KIND_LABELS: Record<string, string> = {
  'mcp-server': 'MCP servers',
  resource: 'Resources',
  gateway: 'Gateway',
  network: 'Networks',
};

const ACTION_LABELS: Record<DiffAction, string> = {
  add: 'Add',
  remove: 'Remove',
  change: 'Change',
};

const ACTION_ICONS = { add: Plus, remove: Minus, change: Pencil } as const;

const ACTION_CLASSES: Record<DiffAction, string> = {
  add: 'text-status-running',
  remove: 'text-status-error',
  change: 'text-status-pending',
};

const PLAN_LIMITS_NOTE =
  'This compares the stack file with the configuration the running gateway loaded. ' +
  'It does not resolve ${secret:} references or run the startup security check, ' +
  'so a reload can still be refused or require a restart. ' +
  'Changes to clients, limits, policy, groups, skills, model_preferences, runs, and experimental ' +
  'are applied by a reload and are not listed.';

function groupByKind(items: DiffItem[]): Array<[string, DiffItem[]]> {
  const groups = new Map<string, DiffItem[]>();
  for (const item of items) {
    const list = groups.get(item.kind) ?? [];
    list.push(item);
    groups.set(item.kind, list);
  }
  const rank = (kind: string) => {
    const i = KIND_ORDER.indexOf(kind);
    return i === -1 ? KIND_ORDER.length : i;
  };
  return [...groups.entries()].sort((a, b) => rank(a[0]) - rank(b[0]));
}

function countAction(plan: PlanDiff | null, action: DiffAction): number {
  return plan?.items?.filter((i) => i.action === action).length ?? 0;
}

function PlanList({ plan }: { plan: PlanDiff }) {
  return (
    <div className="space-y-5">
      {groupByKind(plan.items).map(([kind, items]) => (
        <section key={kind} aria-label={KIND_LABELS[kind] ?? kind}>
          <h3 className="text-xs font-semibold uppercase tracking-wide text-text-muted mb-2">
            {KIND_LABELS[kind] ?? kind}
          </h3>
          <ul className="space-y-2">
            {items.map((item) => {
              const Icon = ACTION_ICONS[item.action] ?? Pencil;
              return (
                <li
                  key={`${item.action}:${item.kind}:${item.name}`}
                  className="rounded-lg border border-border/40 bg-surface-elevated px-3 py-2"
                >
                  <div className="flex items-center gap-2 text-sm">
                    <span className={cn('flex items-center gap-1 text-xs font-medium', ACTION_CLASSES[item.action])}>
                      <Icon size={12} aria-hidden="true" />
                      {ACTION_LABELS[item.action] ?? item.action}
                    </span>
                    <span className="font-mono text-text-primary">{item.name}</span>
                    {item.override && (
                      <span className="px-1.5 py-0.5 rounded-full bg-surface-highlight text-3xs text-text-muted">
                        from override file
                      </span>
                    )}
                  </div>
                  {item.details && item.details.length > 0 && (
                    <ul className="mt-1.5 ml-5 space-y-0.5 font-mono text-xs text-text-secondary">
                      {item.details.map((detail, i) => (
                        <li key={i}>{detail}</li>
                      ))}
                    </ul>
                  )}
                </li>
              );
            })}
          </ul>
        </section>
      ))}
    </div>
  );
}

interface SpecDiffModalProps {
  /** Runs the reload. Called once, from the confirm or "Reload without preview" button. */
  onApply: () => void;
  /** Called when an apply-mode preview is dismissed without reloading. */
  onDismiss?: () => void;
}

export function SpecDiffModal({ onApply, onDismiss }: SpecDiffModalProps) {
  const diffModalOpen = useSpecStore((s) => s.diffModalOpen);
  const titleId = useId();
  const panelRef = useFocusTrap<HTMLDivElement>({ active: diffModalOpen });
  const diffModalMode = useSpecStore((s) => s.diffModalMode);
  const plan = useSpecStore((s) => s.plan);
  const planLoading = useSpecStore((s) => s.planLoading);
  const planError = useSpecStore((s) => s.planError);
  const [compareAttempt, setCompareAttempt] = useState(0);

  const isCompareMode = diffModalMode === 'compare';

  const dismiss = useCallback(() => {
    const wasApply = useSpecStore.getState().diffModalMode === 'apply';
    useSpecStore.getState().closeDiffModal();
    if (wasApply) onDismiss?.();
  }, [onDismiss]);

  const apply = useCallback(() => {
    useSpecStore.getState().closeDiffModal();
    onApply();
  }, [onApply]);

  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape') dismiss();
    },
    [dismiss],
  );

  useEffect(() => {
    if (diffModalOpen) {
      document.addEventListener('keydown', handleKeyDown);
      return () => document.removeEventListener('keydown', handleKeyDown);
    }
  }, [diffModalOpen, handleKeyDown]);

  // Compare mode asks the server on every open and retry. The cleanup marks
  // the request stale, so a response that lands after close, a mode change,
  // or a newer request is dropped.
  useEffect(() => {
    if (!diffModalOpen || !isCompareMode) return;
    let stale = false;
    useSpecStore.getState().setPlanLoading();
    fetchStackPlan().then(
      (result) => {
        if (stale) return;
        useSpecStore.getState().setPlan(result);
        fetchStackHealth().then(
          (health) => {
            if (!stale) useSpecStore.getState().setHealth(health);
          },
          () => undefined,
        );
      },
      (err: unknown) => {
        if (!stale) useSpecStore.getState().setPlanError(err);
      },
    );
    return () => {
      stale = true;
    };
  }, [diffModalOpen, isCompareMode, compareAttempt]);

  if (!diffModalOpen) return null;

  const described = planError
    ? describeError(planError, isCompareMode ? 'Compare' : 'Reload preview')
    : null;
  const showPlan = !planLoading && !described && plan !== null;
  const hasItems = showPlan && (plan.items?.length ?? 0) > 0;
  const addedCount = countAction(plan, 'add');
  const changedCount = countAction(plan, 'change');
  const removedCount = countAction(plan, 'remove');

  return createPortal(
    <div
      ref={panelRef}
      role="dialog"
      aria-modal="true"
      aria-labelledby={titleId}
      className="fixed inset-0 z-modal flex flex-col bg-background"
    >
      {/* Header (pinned) */}
      <div className="flex-shrink-0 flex items-center justify-between px-6 py-3 border-b border-border/50 bg-surface-elevated">
        <div className="flex items-center gap-4">
          <h2 id={titleId} className="text-sm font-semibold text-text-primary">
            {isCompareMode ? 'Compare to running' : 'Reload Config'}
          </h2>
          {hasItems && (
            <div className="flex items-center gap-3 text-xs">
              <span className="flex items-center gap-1 px-2 py-0.5 rounded-full bg-status-running/10 text-status-running">
                <Plus size={11} aria-hidden="true" />
                {addedCount} added
              </span>
              <span className="flex items-center gap-1 px-2 py-0.5 rounded-full bg-status-pending/10 text-status-pending">
                <Pencil size={11} aria-hidden="true" />
                {changedCount} changed
              </span>
              <span className="flex items-center gap-1 px-2 py-0.5 rounded-full bg-status-error/10 text-status-error">
                <Minus size={11} aria-hidden="true" />
                {removedCount} removed
              </span>
            </div>
          )}
        </div>
        <button
          type="button"
          onClick={dismiss}
          aria-label="Close"
          className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors text-text-muted hover:text-text-primary"
        >
          <X size={16} />
        </button>
      </div>

      {/* Body (scrollable) */}
      <div className="flex-1 min-h-0 overflow-auto scrollbar-dark px-6 py-4">
        {planLoading ? (
          <div role="status" className="flex items-center justify-center gap-2 h-full text-text-muted text-sm">
            <Loader2 size={14} className="animate-spin" aria-hidden="true" />
            Comparing the stack file with the running gateway...
          </div>
        ) : described ? (
          <div
            role="alert"
            title={described.fullDetail}
            className="rounded-lg border border-status-error/30 bg-status-error/[0.06] px-4 py-3 flex items-start gap-2 text-sm text-status-error"
          >
            <AlertCircle size={14} className="mt-0.5 flex-shrink-0" aria-hidden="true" />
            <span className="break-words">{described.text}</span>
          </div>
        ) : showPlan ? (
          <div className="space-y-5">
            {plan.override && (
              <p className="text-xs text-text-muted">
                Override file merged: <span className="font-mono text-text-secondary">{plan.override}</span>
              </p>
            )}
            {hasItems ? (
              <PlanList plan={plan} />
            ) : (
              <p className="text-sm text-text-secondary">
                Servers, resources, gateway settings, and networks in the stack file match the running gateway.
              </p>
            )}
            <p className="text-xs text-text-muted">{PLAN_LIMITS_NOTE}</p>
          </div>
        ) : null}
      </div>

      {/* Footer (pinned) */}
      <div className="flex-shrink-0 border-t border-border/50 bg-surface-elevated px-6 py-3">
        <div className="flex items-center justify-end gap-3">
          {isCompareMode ? (
            <>
              {described && (
                <Button variant="secondary" onClick={() => setCompareAttempt((n) => n + 1)}>
                  Retry
                </Button>
              )}
              <Button variant="secondary" onClick={dismiss}>
                Close
              </Button>
            </>
          ) : described ? (
            <>
              <Button variant="secondary" onClick={dismiss}>
                Cancel
              </Button>
              <Button variant="primary" onClick={apply}>
                Reload without preview
              </Button>
            </>
          ) : (
            <>
              <Button variant="secondary" onClick={dismiss}>
                Cancel
              </Button>
              <Button variant="primary" onClick={apply} disabled={!showPlan}>
                Reload
              </Button>
            </>
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}
