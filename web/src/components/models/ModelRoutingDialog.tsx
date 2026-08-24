import { useCallback, useEffect, useState } from 'react';
import { ArrowRight } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Modal } from '../ui/Modal';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { StatePill } from '../ui/StatePill';
import { showToast } from '../ui/Toast';
import {
  HTTPError,
  ackModelsRestart,
  adoptModels,
  fetchModelsStatus,
  fetchModelsValidation,
  syncModels,
} from '../../lib/api';
import { formatStampOrUnknown } from '../../lib/time';
import type {
  ModelsStatusDoc,
  ModelsSyncResult,
  ModelsTargetStatus,
  ModelsValidateDoc,
} from '../../types';
import {
  TIER_ORDER,
  describeModelsSyncResults,
  driftedTargets,
  hasAdoptableDrift,
  modelsTargetLabel,
} from './modelsModel';

interface ModelRoutingDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

/**
 * Model routing: the read-and-reconcile surface over `gridctl models`.
 * Status rows, the policy's routing summary, validation findings, and
 * the whole-policy verbs (preview, sync, adopt, ack-restart), never an
 * editor. The policy document is edited via `gridctl models edit`; every
 * documented failure in this space is a UI becoming a second writer
 * against a file source of truth.
 *
 * Engine contracts this surface must not soften: Sync and Adopt are
 * whole-policy (no per-target buttons), Adopt records bytes without
 * touching files and never covers the include line, and restart-pending
 * is an annotation, not attention.
 */
export function ModelRoutingDialog({ isOpen, onClose }: ModelRoutingDialogProps) {
  const [doc, setDoc] = useState<ModelsStatusDoc | null>(null);
  const [validation, setValidation] = useState<ModelsValidateDoc | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  // Results of the last mutation or preview, rendered inline: the
  // restart instruction and backup paths must outlive a toast.
  const [results, setResults] = useState<ModelsSyncResult[] | null>(null);
  const [resultsLabel, setResultsLabel] = useState('');
  const [reviewing, setReviewing] = useState(false);
  const [confirmMarkRestarted, setConfirmMarkRestarted] = useState(false);
  const [confirmAdopt, setConfirmAdopt] = useState(false);
  const [confirmOverwrite, setConfirmOverwrite] = useState(false);
  // The forced dry-run: what Overwrite with policy would write. Shared
  // by the Review diffs and the Overwrite confirm, so the confirm names
  // the real blast radius (force is whole-policy and also rewrites
  // stale and never-synced targets, not only the drifted rows that
  // opened the review).
  const [forcedPreview, setForcedPreview] = useState<ModelsSyncResult[] | null>(null);
  const [forcedPreviewError, setForcedPreviewError] = useState<string | null>(null);

  const loadForcedPreview = useCallback(() => {
    setForcedPreview(null);
    setForcedPreviewError(null);
    syncModels({ dry_run: true, diff: true, force: true })
      .then(setForcedPreview)
      .catch((err) =>
        setForcedPreviewError(err instanceof Error ? err.message : 'Preview failed'),
      );
  }, []);

  const refresh = useCallback(async () => {
    try {
      const status = await fetchModelsStatus();
      setDoc(status);
      setLoadError(null);
      if (status.policy_exists && !status.policy_error) {
        try {
          setValidation(await fetchModelsValidation());
        } catch {
          // Validation is advisory; status is the load-bearing fetch.
          setValidation(null);
        }
      } else {
        setValidation(null);
      }
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : 'Failed to load model routing status');
    }
  }, []);

  // Reset transient state on open during render (the Modal pattern), so
  // the reset commits with the open instead of one render later; the
  // effect only starts the fetch.
  const [wasOpen, setWasOpen] = useState(isOpen);
  if (wasOpen !== isOpen) {
    setWasOpen(isOpen);
    if (isOpen) {
      setResults(null);
      setReviewing(false);
    }
  }

  useEffect(() => {
    if (!isOpen) return;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- all state writes happen after awaited fetches resolve; nothing sets state synchronously in this effect
    void refresh();
  }, [isOpen, refresh]);

  const act = useCallback(
    async (fn: () => Promise<unknown>, okMessage: string) => {
      setBusy(true);
      try {
        await fn();
        showToast('success', okMessage);
        await refresh();
      } catch (err) {
        showToast('error', err instanceof Error ? err.message : 'Action failed');
      } finally {
        setBusy(false);
      }
    },
    [refresh],
  );

  const runSync = useCallback(
    async (body: { dry_run?: boolean; diff?: boolean; force?: boolean }, label: string) => {
      setBusy(true);
      try {
        const res = await syncModels(body);
        const { kind, message } = describeModelsSyncResults(res, !!body.dry_run);
        showToast(kind, message);
        setResults(res);
        setResultsLabel(label);
        await refresh();
      } catch (err) {
        // The 409 for an invalid policy carries the findings; the message
        // is the engine's own text either way.
        const message =
          err instanceof HTTPError
            ? err.message
            : err instanceof Error
              ? err.message
              : 'Sync failed';
        showToast('error', message);
      } finally {
        setBusy(false);
      }
    },
    [refresh],
  );

  const targets = doc?.targets ?? [];
  const drifted = driftedTargets(targets);
  const adoptableDrift = hasAdoptableDrift(targets);
  const validationBlocks = validation !== null && !validation.valid;
  const syncDisabledTitle = validationBlocks
    ? 'The policy has validation errors; fix them before syncing'
    : undefined;
  const canMutate = !!doc?.policy_exists && !doc?.policy_error && !busy && !validationBlocks;
  // A skipped result names force as the only way through (a foreign file
  // at the target path, or recorded drift); without this the engine copy
  // points at a flag no button can set.
  const skippedResults = (results ?? []).filter(
    (r) => r.action === 'skipped-foreign' || r.action === 'skipped-drift',
  );
  // Review's only verb on inadoptable drift is Overwrite, which an
  // invalid policy cannot render; gate the entry rather than walking the
  // user into a danger confirm that can only 409.
  const reviewDisabledTitle =
    validationBlocks && !adoptableDrift
      ? 'Overwrite is the only resolution here, and it needs a valid policy'
      : undefined;

  const openReview = () => {
    loadForcedPreview();
    setReviewing(true);
  };
  const openOverwriteConfirm = () => {
    // Reaching this from the results strip means no review is open and
    // any earlier preview may predate the last mutation; reload. From
    // inside Review the preview was fetched on open and stays current.
    if (!reviewing) loadForcedPreview();
    setConfirmOverwrite(true);
  };

  // Escape and backdrop close the topmost layer only. Every stacked
  // layer (this modal, the drift review's modal, each confirm) registers
  // its own document keydown listener, and stopPropagation cannot
  // silence sibling listeners on the same node, so every layer routes
  // its close through this one closure. All listeners fire on one
  // Escape, but within a single dispatch they share the same render's
  // state, so each performs the identical idempotent front-layer close.
  const closeTop = confirmMarkRestarted
    ? () => setConfirmMarkRestarted(false)
    : confirmAdopt
      ? () => setConfirmAdopt(false)
      : confirmOverwrite
        ? () => setConfirmOverwrite(false)
        : reviewing
          ? () => setReviewing(false)
          : onClose;

  return (
    <Modal isOpen={isOpen} onClose={closeTop} title="Model routing" size="wide">
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-6 py-4 flex flex-col gap-4">
        {/* Identity: what this surface is, and what it is not. */}
        <div className="flex flex-col gap-1">
          <div className="flex items-center gap-2 flex-wrap">
            <span
              className="text-[9px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded-full border border-status-pending/30 bg-status-pending/10 text-status-pending"
              title="The models projection kind is Experimental: LiteLLM's auto-router schema is still evolving upstream"
            >
              Experimental
            </span>
            <span className="text-[11px] text-text-muted">
              LiteLLM auto-router and OpenCode provider projection. Not skill or agent{' '}
              <span className="font-mono">model:</span> preferences.
            </span>
          </div>
          {doc && (
            <span className="text-[11px] text-text-muted font-mono truncate" title={doc.policy_path}>
              {doc.policy_path}
              <span className="font-sans text-text-muted/70"> · edit: </span>
              gridctl models edit
            </span>
          )}
        </div>

        {loadError && (
          <p role="alert" className="text-xs text-status-error border border-status-error/30 bg-status-error/10 rounded-lg px-3 py-2">
            {loadError}
          </p>
        )}

        {doc && !doc.policy_exists && (
          <div className="flex flex-col items-center gap-2 py-10 text-center">
            <p className="text-sm text-text-secondary">No model routing policy yet</p>
            <p className="text-xs text-text-muted max-w-sm">
              Create one with <span className="font-mono text-text-secondary">gridctl models init</span>{' '}
              in a terminal. It will live at{' '}
              <span className="font-mono break-all">{doc.policy_path}</span>.
            </p>
          </div>
        )}

        {doc?.policy_error && (
          <p role="alert" className="text-xs text-status-error border border-status-error/30 bg-status-error/10 rounded-lg px-3 py-2">
            The policy file does not parse: {doc.policy_error}
          </p>
        )}

        {doc?.routing && <RoutingSummary routing={doc.routing} />}

        {validation && <ValidationFindings validation={validation} />}

        {doc?.policy_exists && !doc.policy_error && (
          <section aria-label="Projection targets" className="flex flex-col gap-1">
            <h3 className="text-[10px] uppercase tracking-[0.2em] text-text-muted/70">Targets</h3>
            <ul className="divide-y divide-border/20 border border-border/30 rounded-lg px-3">
              {targets.map((t) => (
                <TargetRow
                  key={t.target}
                  status={t}
                  busy={busy}
                  onMarkRestarted={() => setConfirmMarkRestarted(true)}
                />
              ))}
            </ul>
          </section>
        )}

        {results && (
          <section aria-label={resultsLabel} className="flex flex-col gap-1">
            <h3 className="text-[10px] uppercase tracking-[0.2em] text-text-muted/70">
              {resultsLabel}
            </h3>
            <ul className="flex flex-col gap-2">
              {results.map((r) => (
                <SyncResultRow key={r.target} result={r} />
              ))}
            </ul>
            {skippedResults.length > 0 && (
              <div className="flex items-center gap-2 flex-wrap pt-1">
                <span className="text-[11px] text-text-muted flex-1">
                  Skipped targets are written only by overwriting: the engine never touches a
                  drifted or foreign file on a plain sync.
                </span>
                <button
                  onClick={openOverwriteConfirm}
                  disabled={busy || validationBlocks}
                  title={syncDisabledTitle}
                  className="px-3 py-1.5 text-xs font-medium text-red-400 border border-red-400/25 rounded-lg hover:bg-red-400/10 transition-colors disabled:opacity-50 whitespace-nowrap"
                >
                  Overwrite with policy
                </button>
              </div>
            )}
          </section>
        )}
      </div>

      {/* Action bar: whole-policy verbs only. The engine has no
          per-target selection, so no row-level sync exists anywhere. */}
      {doc?.policy_exists && !doc.policy_error && (
        <div className="flex items-center gap-2 px-6 py-3 border-t border-border/30 flex-shrink-0">
          <ActionButton
            label={busy ? 'Working…' : 'Preview changes'}
            subtle
            disabled={!canMutate}
            title={syncDisabledTitle}
            onClick={() => void runSync({ dry_run: true, diff: true }, 'Preview')}
          />
          {drifted.length > 0 && (
            <ActionButton
              label="Review drift"
              subtle
              disabled={busy || !!reviewDisabledTitle}
              title={reviewDisabledTitle}
              onClick={openReview}
            />
          )}
          <span className="ml-auto" />
          <ActionButton
            label={busy ? 'Working…' : 'Sync all targets'}
            disabled={!canMutate}
            title={syncDisabledTitle}
            onClick={() => void runSync({}, 'Sync results')}
          />
        </div>
      )}

      {reviewing && (
        <DriftReviewDialog
          drifted={drifted}
          busy={busy}
          adoptable={adoptableDrift}
          diffs={forcedPreview === null ? null : forcedPreview.filter((r) => r.diff)}
          diffError={forcedPreviewError}
          overwriteDisabledTitle={syncDisabledTitle}
          onClose={closeTop}
          onAdopt={() => setConfirmAdopt(true)}
          onOverwrite={openOverwriteConfirm}
        />
      )}

      <ConfirmDialog
        isOpen={confirmMarkRestarted}
        onClose={() => setConfirmMarkRestarted(false)}
        onConfirm={() => {
          setConfirmMarkRestarted(false);
          void act(() => ackModelsRestart(), 'Recorded the LiteLLM restart');
        }}
        title="Mark restarted"
        message={
          <>
            <p>
              gridctl never probes the LiteLLM process. Confirming records that you restarted it
              yourself since the last fragment write.
            </p>
            <p>If you have not restarted it, status will claim the policy is live when it is not.</p>
          </>
        }
        confirmLabel="Mark restarted"
      />

      <ConfirmDialog
        isOpen={confirmAdopt}
        onClose={() => setConfirmAdopt(false)}
        onConfirm={() => {
          setConfirmAdopt(false);
          setReviewing(false);
          void act(async () => {
            const res = await adoptModels();
            setResults(null);
            return res;
          }, 'Recorded the on-disk state as owned');
        }}
        title="Accept on-disk as owned"
        message={
          <>
            <p>
              gridctl will record the current fragment and OpenCode provider bytes as owned. No file
              is rewritten, and the policy document is not updated.
            </p>
            <p>
              A later sync from this policy will overwrite those bytes. The include line cannot be
              adopted; a removed include is restored only by Sync with overwrite.
            </p>
          </>
        }
        confirmLabel="Accept"
      />

      <ConfirmDialog
        isOpen={confirmOverwrite}
        onClose={() => setConfirmOverwrite(false)}
        onConfirm={() => {
          setConfirmOverwrite(false);
          setReviewing(false);
          void runSync({ force: true }, 'Sync results');
        }}
        title="Overwrite with policy"
        message={<OverwriteMessage preview={forcedPreview} />}
        confirmLabel="Overwrite"
        variant="danger"
      />
    </Modal>
  );
}

/**
 * The Overwrite confirm's body, naming the real blast radius: force is
 * whole-policy, so the forced dry-run's would-update rows are the list,
 * not the drifted rows that opened the review. Until the preview
 * resolves the copy stays honest about scope rather than guessing.
 */
function OverwriteMessage({ preview }: { preview: ModelsSyncResult[] | null }) {
  const wouldWrite = (preview ?? []).filter((r) => r.action === 'would-update');
  const latches = wouldWrite.some((r) => r.target === 'litellm-fragment');
  return (
    <>
      <p>
        {wouldWrite.length > 0 ? (
          <>
            Rewrite {wouldWrite.map((r) => modelsTargetLabel(r.target)).join(', ')} from the
            policy, discarding what is on disk?
          </>
        ) : (
          <>
            Rewrite every target that no longer matches the policy, discarding what is on disk?
            Overwrite is whole-policy: stale and never-synced targets are written too, not only
            the drifted ones.
          </>
        )}
      </p>
      <p>
        The engine backs each file up before overwriting.
        {latches && ' Rewriting the fragment latches restart-pending until you restart LiteLLM.'}
      </p>
    </>
  );
}

/**
 * Static projection of what the policy says: tier to backend, default
 * marked. The only routing visualization the control plane can honestly
 * provide (gridctl never observes LiteLLM's routing decisions).
 */
function RoutingSummary({ routing }: { routing: NonNullable<ModelsStatusDoc['routing']> }) {
  return (
    <section aria-label="Routing summary" className="flex flex-col gap-1">
      <h3 className="text-[10px] uppercase tracking-[0.2em] text-text-muted/70">Routing</h3>
      <div className="border border-border/30 rounded-lg px-3 py-2 flex flex-col gap-1">
        <span className="text-[11px] text-text-muted">
          Clients select <span className="font-mono text-text-secondary">{routing.entry_model}</span>;
          LiteLLM classifies each request into a tier.
        </span>
        <ul className="flex flex-col gap-0.5">
          {TIER_ORDER.map((tier) => (
            <li key={tier} className="flex items-center gap-2 text-xs">
              <span className="font-mono text-[11px] text-text-muted w-24 flex-shrink-0">{tier}</span>
              <ArrowRight size={10} className="text-text-muted/50 flex-shrink-0" aria-hidden="true" />
              <span className="font-mono text-text-secondary truncate">
                {routing.tiers[tier] ?? '—'}
              </span>
              {routing.default_tier === tier && (
                <span className="text-[10px] px-1.5 py-0.5 rounded border border-border/40 bg-background/40 text-text-muted whitespace-nowrap">
                  default
                </span>
              )}
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

function ValidationFindings({ validation }: { validation: ModelsValidateDoc }) {
  const errors = validation.issues.filter((i) => i.severity === 'error').length;
  const warnings = validation.issues.length - errors;
  const [expanded, setExpanded] = useState(errors > 0);
  if (validation.issues.length === 0) return null;
  const summary = [
    errors > 0 ? `${errors} error${errors === 1 ? '' : 's'}` : null,
    warnings > 0 ? `${warnings} warning${warnings === 1 ? '' : 's'}` : null,
  ]
    .filter(Boolean)
    .join(', ');
  return (
    <section aria-label="Validation findings" className="flex flex-col gap-1">
      <button
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
        className="flex items-center gap-2 text-left"
      >
        <h3 className="text-[10px] uppercase tracking-[0.2em] text-text-muted/70">Validation</h3>
        <span
          role={errors > 0 ? 'alert' : undefined}
          className={cn('text-[11px]', errors > 0 ? 'text-status-error' : 'text-status-pending')}
        >
          {summary}
        </span>
      </button>
      {expanded && (
        <ul className="flex flex-col gap-1 border border-border/30 rounded-lg px-3 py-2">
          {validation.issues.map((issue, i) => (
            <li key={`${issue.field}-${i}`} className="flex items-start gap-2 text-xs">
              <span
                className={cn(
                  'text-[10px] px-1.5 py-0.5 rounded-full border font-medium flex-shrink-0',
                  issue.severity === 'error'
                    ? 'text-status-error border-status-error/30 bg-status-error/10'
                    : 'text-status-pending border-status-pending/30 bg-status-pending/10',
                )}
              >
                {issue.severity}
              </span>
              <span className="font-mono text-[11px] text-text-muted flex-shrink-0">{issue.field}</span>
              <span className="text-text-secondary">{issue.message}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function TargetRow({
  status: t,
  busy,
  onMarkRestarted,
}: {
  status: ModelsTargetStatus;
  busy: boolean;
  onMarkRestarted: () => void;
}) {
  return (
    <li className="flex flex-col gap-1 py-2">
      <div className="flex items-center gap-2 flex-wrap">
        <StatePill state={t.state} />
        {t.restart_pending && (
          <span
            className="text-[10px] px-1.5 py-0.5 rounded border border-status-pending/30 bg-status-pending/10 text-status-pending font-mono whitespace-nowrap"
            title="LiteLLM reads config only at startup; the file on disk is newer than what the proxy is serving"
          >
            restart pending
          </span>
        )}
        <span className="text-xs text-text-primary whitespace-nowrap">
          {modelsTargetLabel(t.target)}
        </span>
        <span className="ml-auto flex items-center gap-1.5">
          {t.restart_pending && (
            <ActionButton label="Mark restarted" disabled={busy} onClick={onMarkRestarted} />
          )}
        </span>
      </div>
      {t.path && (
        <span className="text-[11px] text-text-muted font-mono truncate" title={t.path}>
          {t.path}
        </span>
      )}
      {t.synced_at && (
        <span className="text-[11px] text-text-muted/70">
          synced {formatStampOrUnknown(t.synced_at)}
        </span>
      )}
      {t.detail && <span className="text-[11px] text-text-muted/80">{t.detail}</span>}
    </li>
  );
}

function SyncResultRow({ result: r }: { result: ModelsSyncResult }) {
  const failed = r.action === 'error';
  const skipped = r.action === 'skipped-drift' || r.action === 'skipped-foreign';
  return (
    <li className="flex flex-col gap-1 border border-border/30 rounded-lg px-3 py-2">
      <div className="flex items-center gap-2 flex-wrap">
        <span
          className={cn(
            'text-[10px] px-1.5 py-0.5 rounded-full border font-medium whitespace-nowrap',
            failed && 'text-status-error border-status-error/30 bg-status-error/10',
            skipped && 'text-status-pending border-status-pending/30 bg-status-pending/10',
            !failed && !skipped && 'text-emerald-400 border-emerald-400/25 bg-emerald-400/10',
          )}
        >
          {r.action}
        </span>
        <span className="text-xs text-text-primary">{modelsTargetLabel(r.target)}</span>
        <span className="text-[11px] text-text-muted font-mono truncate ml-auto" title={r.path}>
          {r.path}
        </span>
      </div>
      {r.error && <span className="text-[11px] text-status-error">{r.error}</span>}
      {r.detail && <span className="text-[11px] text-text-muted/80">{r.detail}</span>}
      {r.backup_path && (
        <span className="text-[11px] text-text-muted/60 font-mono truncate" title={r.backup_path}>
          backup: {r.backup_path}
        </span>
      )}
      {r.diff && (
        <pre
          aria-label={`Diff for ${modelsTargetLabel(r.target)}`}
          className="text-[11px] font-mono bg-background/60 border border-border/30 rounded-lg p-3 overflow-x-auto max-h-72 overflow-y-auto scrollbar-dark whitespace-pre"
        >
          {r.diff}
        </pre>
      )}
    </li>
  );
}

/**
 * The whole-policy drift review: every drifted row with the two real
 * resolutions. Adopt is offered only when a drifted row is one Adopt can
 * record (fragment or OpenCode); include-line drift resolves only via
 * Overwrite, and the dialog says so in prose rather than hiding the
 * reason in a disabled button. The diffs come from the parent's forced
 * dry-run (drifted targets skip a plain dry-run), which the Overwrite
 * confirm also reads to name its blast radius.
 */
function DriftReviewDialog({
  drifted,
  busy,
  adoptable,
  diffs,
  diffError,
  overwriteDisabledTitle,
  onClose,
  onAdopt,
  onOverwrite,
}: {
  drifted: ModelsTargetStatus[];
  busy: boolean;
  adoptable: boolean;
  /** Forced dry-run rows that carry a diff; null while loading. */
  diffs: ModelsSyncResult[] | null;
  diffError: string | null;
  /** Set while validation errors block a forced sync from rendering. */
  overwriteDisabledTitle?: string;
  onClose: () => void;
  onAdopt: () => void;
  onOverwrite: () => void;
}) {
  const includeOnly = !adoptable;
  return (
    <Modal isOpen onClose={onClose} title="Review drift" size="wide">
      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-6 py-4 flex flex-col gap-3">
        <p className="text-xs text-text-muted">
          {drifted.length === 1
            ? `${modelsTargetLabel(drifted[0].target)} was edited since gridctl wrote it.`
            : `${drifted.length} targets were edited since gridctl wrote them.`}{' '}
          The diffs below show what Overwrite with policy would write.
          {includeOnly &&
            ' Only the include line drifted: nothing exists to accept, so overwriting is the one resolution.'}
        </p>
        {diffError && (
          <p role="alert" className="text-xs text-status-error">
            {diffError}
          </p>
        )}
        {diffs === null && !diffError && (
          <p className="text-xs text-text-muted">Loading diffs…</p>
        )}
        {diffs !== null && diffs.length === 0 && (
          <p className="text-xs text-text-muted">
            No content diff to show; the drift is recorded state (for example a removed include
            line).
          </p>
        )}
        {(diffs ?? []).map((r) => (
          <div key={r.target} className="flex flex-col gap-1">
            <span className="text-xs text-text-primary">{modelsTargetLabel(r.target)}</span>
            <pre
              aria-label={`Diff for ${modelsTargetLabel(r.target)}`}
              className="text-[11px] font-mono bg-background/60 border border-border/30 rounded-lg p-3 overflow-x-auto max-h-72 overflow-y-auto scrollbar-dark whitespace-pre"
            >
              {r.diff}
            </pre>
          </div>
        ))}
      </div>
      <div className="flex items-center justify-end gap-2 px-6 py-3 border-t border-border/30 flex-shrink-0 flex-wrap">
        <ActionButton label="Cancel" subtle disabled={busy} onClick={onClose} />
        {adoptable && (
          <ActionButton label="Accept on-disk as owned" disabled={busy} onClick={onAdopt} />
        )}
        <button
          onClick={onOverwrite}
          disabled={busy || !!overwriteDisabledTitle}
          title={overwriteDisabledTitle}
          className="px-3 py-1.5 text-xs font-medium text-red-400 border border-red-400/25 rounded-lg hover:bg-red-400/10 transition-colors disabled:opacity-50"
        >
          Overwrite with policy
        </button>
      </div>
    </Modal>
  );
}

function ActionButton({
  label,
  onClick,
  disabled,
  subtle,
  title,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  subtle?: boolean;
  title?: string;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      title={title}
      className={cn(
        'px-3 py-1.5 rounded-lg text-xs font-medium border transition-colors disabled:opacity-50 whitespace-nowrap',
        subtle
          ? 'text-text-muted border-border/40 hover:bg-surface-highlight'
          : 'text-primary bg-primary/10 border-primary/25 hover:bg-primary/15',
      )}
    >
      {label}
    </button>
  );
}
