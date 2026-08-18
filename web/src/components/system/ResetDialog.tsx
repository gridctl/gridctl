import { useCallback, useEffect, useRef, useState } from 'react';
import {
  AlertTriangle,
  Archive,
  ChevronDown,
  ChevronRight,
  Loader2,
  Trash2,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { Modal } from '../ui/Modal';
import {
  executeReset,
  fetchResetPreview,
  type ResetDoc,
  type ResetPreviewResponse,
  type ResetRow,
} from '../../lib/api';

type Phase = 'preview' | 'confirm' | 'running' | 'result';
type Tier = 'default' | 'purge';

// Seam over the hard page reload so tests can observe the exit without
// jsdom navigation errors — the server side has the same seam for the
// same reason (SetResetExit observes self-termination without dying).
// eslint-disable-next-line react-refresh/only-export-components -- test seam; exported for unit tests alongside the dialog
export const pageReload = { current: () => window.location.reload() };

/**
 * ResetDialog is the web face of `gridctl reset`: a single focus-trapped
 * Modal hosting a phase machine (preview -> confirm -> running ->
 * result), never a nested ConfirmDialog (two stacked traps fight over
 * Tab; see FleetActions). The preview opens on the server dry run and
 * renders nothing actionable until it resolves, so the blast radius the
 * user confirms is the one the engine computed. The purge tier requires
 * typing the RESOLVED root path the preview returned; the server
 * enforces the same phrase, so the gate is real, not decorative.
 *
 * Both tiers end in a page reload: the serving daemon is inside the
 * blast radius either way, so eager store refreshes would race a dying
 * server. The poller owns the gateway-unavailable state after a purge.
 */
export function ResetDialog({ isOpen, onClose }: { isOpen: boolean; onClose: () => void }) {
  const [phase, setPhase] = useState<Phase>('preview');
  const [tier, setTier] = useState<Tier>('default');
  // Preview state is keyed by the tier it was fetched for: a stale tier's
  // response renders as "loading" rather than being cleared synchronously
  // inside the effect (which would cascade renders).
  const [previewState, setPreviewState] = useState<{ tier: Tier; resp: ResetPreviewResponse } | null>(null);
  const [previewErrorState, setPreviewErrorState] = useState<{ tier: Tier; msg: string } | null>(null);
  const [phrase, setPhrase] = useState('');
  const [result, setResult] = useState<ResetDoc | null>(null);
  const [execError, setExecError] = useState<string | null>(null);

  const preview = previewState && previewState.tier === tier ? previewState.resp : null;
  const previewError = previewErrorState && previewErrorState.tier === tier ? previewErrorState.msg : null;

  // The preview (and its single-use, tier-bound token) refetches on every
  // tier switch: a default-tier token must never authorize a purge.
  const fetchSeq = useRef(0);
  useEffect(() => {
    if (!isOpen) return;
    const seq = ++fetchSeq.current;
    const forTier = tier;
    fetchResetPreview({ purge: forTier === 'purge' })
      .then((resp) => {
        if (fetchSeq.current === seq) {
          setPreviewState({ tier: forTier, resp });
          setPreviewErrorState(null);
        }
      })
      .catch((err) => {
        if (fetchSeq.current === seq) {
          setPreviewErrorState({ tier: forTier, msg: err instanceof Error ? err.message : 'Preview failed' });
        }
      });
  }, [isOpen, tier]);

  // Reset transient state when the dialog closes (render-time adjustment,
  // matching Modal's own expanded-state reset).
  const [wasOpen, setWasOpen] = useState(isOpen);
  if (wasOpen !== isOpen) {
    setWasOpen(isOpen);
    if (!isOpen) {
      setPhase('preview');
      setTier('default');
      setPreviewState(null);
      setPreviewErrorState(null);
      setPhrase('');
      setResult(null);
      setExecError(null);
    }
  }

  // Escape and backdrop must not abandon a reset mid-flight; the cascade
  // is running server-side whether or not the dialog stays open. Once an
  // execute has succeeded, every exit reloads, not just Done: the stores
  // describe a world the reset dismantled, so X and Escape must not
  // strand the user on a stale page. A failed execute (stale token, 409)
  // changed nothing server-side and closes without reloading.
  const guardedClose = useCallback(() => {
    if (phase === 'running') return;
    if (phase === 'result' && result) {
      pageReload.current();
      return;
    }
    onClose();
  }, [phase, result, onClose]);

  const run = useCallback(async () => {
    if (!preview) return;
    setPhase('running');
    setExecError(null);
    try {
      const doc = await executeReset({
        purge: tier === 'purge',
        confirm_token: preview.confirm_token,
        confirm_phrase: tier === 'purge' ? phrase.trim() : '',
      });
      setResult(doc);
    } catch (err) {
      setExecError(err instanceof Error ? err.message : 'Reset failed');
    }
    setPhase('result');
  }, [preview, tier, phrase]);

  return (
    <Modal isOpen={isOpen} onClose={guardedClose} title="Reset gridctl" size="wide">
      {phase === 'preview' && (
        <PreviewView
          preview={preview}
          previewError={previewError}
          tier={tier}
          onTier={setTier}
          onContinue={() => setPhase('confirm')}
          onCancel={guardedClose}
        />
      )}
      {phase === 'confirm' && preview && (
        <ConfirmView
          preview={preview}
          tier={tier}
          phrase={phrase}
          onPhrase={setPhrase}
          onBack={() => setPhase('preview')}
          onCancel={guardedClose}
          onConfirm={run}
        />
      )}
      {phase === 'running' && <RunningView tier={tier} gridctlDir={preview?.confirm_phrase ?? ''} />}
      {phase === 'result' && <ResultView result={result} execError={execError} onClose={onClose} />}
    </Modal>
  );
}

// --- preview ---------------------------------------------------------------

const REMOVABLE_ACTIONS = new Set(['would-remove', 'would-stop', 'dropped-record']);
const KEPT_ACTIONS = new Set(['kept-drift', 'kept-foreign']);

function splitRows(doc: ResetDoc) {
  const rows = doc.rows ?? [];
  const recreatable: ResetRow[] = [];
  const wiring: ResetRow[] = [];
  const kept: ResetRow[] = [];
  for (const r of rows) {
    if (KEPT_ACTIONS.has(r.action)) kept.push(r);
    else if (!REMOVABLE_ACTIONS.has(r.action)) continue;
    else if (r.kind === 'wiring') wiring.push(r);
    else recreatable.push(r);
  }
  return { recreatable, wiring, kept };
}

interface PreviewViewProps {
  preview: ResetPreviewResponse | null;
  previewError: string | null;
  tier: Tier;
  onTier: (t: Tier) => void;
  onContinue: () => void;
  onCancel: () => void;
}

function PreviewView({ preview, previewError, tier, onTier, onContinue, onCancel }: PreviewViewProps) {
  if (previewError) {
    return (
      <div className="space-y-3">
        <div className="flex items-start gap-2 rounded-md border border-status-error/40 bg-status-error/[0.06] px-3 py-2" role="alert">
          <AlertTriangle size={12} className="text-status-error flex-shrink-0 mt-0.5" aria-hidden="true" />
          <p className="text-[11px] text-status-error">{previewError}</p>
        </div>
        <div className="flex justify-end">
          <FooterButton onClick={onCancel}>Close</FooterButton>
        </div>
      </div>
    );
  }
  if (!preview) {
    return (
      <p className="flex items-center gap-2 text-xs text-text-muted" role="status">
        <Loader2 size={12} className="animate-spin" aria-hidden="true" />
        Computing the blast radius…
      </p>
    );
  }

  const { recreatable, wiring, kept } = splitRows(preview.doc);
  const removableCount = recreatable.length + wiring.length;

  return (
    <div className="space-y-4 text-xs">
      <p className="font-mono text-[10px] text-text-muted">home: {preview.doc.home}</p>

      <TierCards tier={tier} onTier={onTier} stats={preview.doc.purge_stats} gridctlDir={preview.confirm_phrase} />

      <section aria-label="Will be removed">
        <SectionHeading>
          Will be removed
          <span className="normal-case tracking-normal font-normal text-text-muted"> · recreatable with gridctl apply / pack apply / link</span>
        </SectionHeading>
        {removableCount === 0 ? (
          <p className="text-text-muted">Nothing. This machine has no gridctl-created artifacts to remove.</p>
        ) : (
          <ClientGroups rows={[...recreatable, ...wiring]} />
        )}
      </section>

      <section aria-label="Kept because you edited them">
        {kept.length > 0 ? (
          <div className="rounded-md border border-status-pending/30 bg-status-pending/[0.06] px-3 py-2 space-y-1.5">
            <p className="flex items-center gap-1.5 font-medium text-status-pending">
              <AlertTriangle size={12} aria-hidden="true" />
              Kept: your edits are safe ({kept.length})
            </p>
            <ul className="space-y-0.5 text-[11px] text-text-secondary">
              {kept.map((r) => (
                <li key={r.kind + r.name + (r.client ?? '')} className="flex items-baseline gap-2">
                  <span className="text-[10px] px-2 py-0.5 rounded-full border font-medium whitespace-nowrap border-status-pending/30 bg-status-pending/10 text-status-pending">
                    kept (edited)
                  </span>
                  <span className="min-w-0">
                    <span className="font-mono">{r.kind} {r.name}</span>
                    {r.detail ? <span className="text-text-muted"> — {r.detail}</span> : null}
                  </span>
                </li>
              ))}
            </ul>
          </div>
        ) : null}
      </section>

      <BackupNote purge={tier === 'purge'} />

      <div className="flex items-center justify-end gap-2 pt-1">
        <FooterButton onClick={onCancel}>Cancel</FooterButton>
        <button
          type="button"
          onClick={onContinue}
          disabled={removableCount === 0 && tier === 'default'}
          className={cn(
            'rounded-md px-3 py-1.5 text-[11px] font-medium border transition-colors',
            'border-status-error/40 bg-status-error/10 text-status-error hover:bg-status-error/20',
            'disabled:opacity-40 disabled:cursor-not-allowed',
          )}
        >
          Continue to confirm
        </button>
      </div>
    </div>
  );
}

/**
 * Tier selection as two stacked radio cards. The asymmetry is the
 * docker/for-mac#6758 countermeasure: labels name outcomes and paths
 * (never bare words like "clean" or "factory"), and the purge card
 * carries its red border AT REST, not only when selected, so the
 * heavier option can never read as the milder one.
 */
function TierCards({
  tier,
  onTier,
  stats,
  gridctlDir,
}: {
  tier: Tier;
  onTier: (t: Tier) => void;
  stats?: ResetDoc['purge_stats'];
  gridctlDir: string;
}) {
  return (
    <fieldset className="space-y-2">
      <legend className="sr-only">Reset tier</legend>
      <TierCard
        selected={tier === 'default'}
        onSelect={() => onTier('default')}
        title="Reset projections and containers"
        danger={false}
      >
        Removes what gridctl placed elsewhere on this machine. Keeps {gridctlDir} (vault, oauth
        grants, pins, registry, telemetry).
      </TierCard>
      <TierCard
        selected={tier === 'purge'}
        onSelect={() => onTier('purge')}
        title={`Reset AND delete ${gridctlDir}`}
        danger
      >
        Everything above, plus permanent deletion of{' '}
        {stats ? (
          <>
            <Stat label="vault variables" value={stats.vault_variables < 0 ? 'unknown (locked)' : String(stats.vault_variables)} />
            {', '}
            <Stat label="oauth grants" value={String(stats.oauth_servers)} />
            {', '}
            <Stat label="pin files" value={String(stats.pin_files)} />
            {', and '}
            <Stat label="telemetry" value={formatBytes(stats.telemetry_bytes)} />
          </>
        ) : (
          'the vault, oauth grants, pins, and telemetry'
        )}
        . There is no restore command.
      </TierCard>
    </fieldset>
  );
}

function TierCard({
  selected,
  onSelect,
  title,
  danger,
  children,
}: {
  selected: boolean;
  onSelect: () => void;
  title: string;
  danger: boolean;
  children: React.ReactNode;
}) {
  return (
    <label
      className={cn(
        'block w-full cursor-pointer rounded-lg border px-3 py-2.5 transition-colors',
        danger
          ? 'border-status-error/30 bg-status-error/[0.05]'
          : 'border-border/40 bg-background/40',
        selected && (danger ? 'ring-1 ring-status-error/50' : 'ring-1 ring-primary/50'),
      )}
    >
      <span className="flex items-start gap-2.5">
        <input
          type="radio"
          name="reset-tier"
          checked={selected}
          onChange={onSelect}
          className="mt-0.5 accent-current"
        />
        <span className="space-y-0.5">
          <span className={cn('block text-[11px] font-semibold', danger ? 'text-status-error' : 'text-text-primary')}>
            {title}
          </span>
          <span className="block text-[11px] leading-relaxed text-text-secondary">{children}</span>
        </span>
      </span>
    </label>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <span className="whitespace-nowrap">
      <span className="font-mono text-text-primary">{value}</span> {label}
    </span>
  );
}

/** Rows grouped per client with expand-to-detail, so 100+ artifacts never
 *  render as a wall. Container/daemon/state rows group under "stacks". */
function ClientGroups({ rows }: { rows: ResetRow[] }) {
  const groups = new Map<string, ResetRow[]>();
  for (const r of rows) {
    const key = r.client || (r.kind === 'wiring' ? r.name : 'stacks');
    const list = groups.get(key) ?? [];
    list.push(r);
    groups.set(key, list);
  }
  const names = [...groups.keys()].sort();
  return (
    <ul className="space-y-1">
      {names.map((name) => (
        <ClientGroup key={name} name={name} rows={groups.get(name)!} />
      ))}
    </ul>
  );
}

function ClientGroup({ name, rows }: { name: string; rows: ResetRow[] }) {
  const [open, setOpen] = useState(false);
  const summary = summarizeKinds(rows);
  return (
    <li className="rounded-md border border-border/30 bg-background/30">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left hover:bg-surface-highlight/40 rounded-md transition-colors"
      >
        {open ? (
          <ChevronDown size={12} className="text-text-muted flex-shrink-0" aria-hidden="true" />
        ) : (
          <ChevronRight size={12} className="text-text-muted flex-shrink-0" aria-hidden="true" />
        )}
        <span className="font-medium text-text-primary">{name}</span>
        <span className="text-text-muted">{summary}</span>
      </button>
      {open && (
        <ul className="border-t border-border/20 px-3 py-1.5 space-y-0.5">
          {rows.map((r, i) => (
            <li key={i} className="flex items-baseline gap-2 text-[11px]">
              {/* Future tense: nothing has been deleted on the preview screen. */}
              <span className="inline-flex items-center gap-1 text-status-error">
                <Trash2 size={10} aria-hidden="true" />
                {r.action === 'dropped-record'
                  ? 'record'
                  : r.action === 'would-stop'
                    ? 'will stop'
                    : 'will remove'}
              </span>
              <span className="text-text-secondary">{r.kind}</span>
              <span className="font-mono text-[10px] text-text-muted truncate">{r.path || r.name}</span>
            </li>
          ))}
          <li className="pt-1 text-[10px] text-text-muted">
            Everything else here is not gridctl's and will not be touched.
          </li>
        </ul>
      )}
    </li>
  );
}

function summarizeKinds(rows: ResetRow[]): string {
  const counts = new Map<string, number>();
  for (const r of rows) counts.set(r.kind, (counts.get(r.kind) ?? 0) + 1);
  return [...counts.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([kind, n]) => `${n} ${kind}${n === 1 ? '' : 's'}`)
    .join(' · ');
}

function BackupNote({ purge }: { purge: boolean }) {
  return (
    <p className="flex items-start gap-1.5 text-[10px] text-text-muted">
      <Archive size={11} className="flex-shrink-0 mt-px" aria-hidden="true" />
      <span>
        A backup archive is written before anything is deleted
        {purge ? ' (beside the removed directory, outside the purged tree)' : ''}. It is a safety
        copy, not an undo: gridctl has no restore command.
      </span>
    </p>
  );
}

// --- confirm ---------------------------------------------------------------

interface ConfirmViewProps {
  preview: ResetPreviewResponse;
  tier: Tier;
  phrase: string;
  onPhrase: (v: string) => void;
  onBack: () => void;
  onCancel: () => void;
  onConfirm: () => void;
}

function ConfirmView({ preview, tier, phrase, onPhrase, onBack, onCancel, onConfirm }: ConfirmViewProps) {
  const { recreatable, wiring, kept } = splitRows(preview.doc);
  const count = recreatable.length + wiring.length;
  const purge = tier === 'purge';
  const phraseOk = !purge || phrase.trim() === preview.confirm_phrase;

  return (
    <div className="space-y-4 text-xs">
      <div role="status" className="rounded-md border border-border/40 bg-background/40 px-3 py-2 space-y-1">
        <p className="text-text-primary font-medium">
          {count} item{count === 1 ? '' : 's'} will be removed.
          {kept.length > 0 ? ` ${kept.length} hand-edited item${kept.length === 1 ? '' : 's'} kept.` : ''}
        </p>
        <p className="text-text-secondary">
          {purge ? (
            <>
              <span className="font-mono">{preview.confirm_phrase}</span> is deleted permanently:
              vault, oauth grants, pins, registry, and telemetry go with it.
            </>
          ) : (
            <>
              <span className="font-mono">{preview.confirm_phrase}</span> is preserved (vault, oauth
              grants, pins, registry, telemetry).
            </>
          )}
        </p>
      </div>

      {purge && (
        <div className="space-y-1.5">
          <label htmlFor="reset-confirm-phrase" className="block text-[11px] font-medium text-status-error">
            Type {preview.confirm_phrase} to confirm permanent deletion
          </label>
          <input
            id="reset-confirm-phrase"
            type="text"
            value={phrase}
            onChange={(e) => onPhrase(e.target.value)}
            autoComplete="off"
            spellCheck={false}
            aria-describedby="reset-purge-consequences"
            className={cn(
              'w-full rounded-md border bg-background/60 px-2.5 py-1.5 font-mono text-[11px]',
              'text-text-primary placeholder:text-text-muted/60 focus:outline-none focus:ring-1',
              phraseOk && phrase !== ''
                ? 'border-status-error/60 focus:ring-status-error/50'
                : 'border-border/40 focus:ring-primary/40',
            )}
          />
          <p id="reset-purge-consequences" className="text-[10px] text-text-muted">
            The backup archive deliberately excludes oauth tokens and daemon state; the vault is
            included as ciphertext. There is no restore command.
          </p>
        </div>
      )}

      <div className="flex items-center justify-between pt-1">
        <FooterButton onClick={onBack}>Back</FooterButton>
        <div className="flex items-center gap-2">
          {/* Cancel keeps initial focus: the destructive action is never the default. */}
          <FooterButton onClick={onCancel} autoFocus>
            Cancel
          </FooterButton>
          <button
            type="button"
            onClick={onConfirm}
            disabled={!phraseOk}
            className={cn(
              'rounded-md px-3 py-1.5 text-[11px] font-medium border transition-colors',
              'border-status-error/50 bg-status-error/15 text-status-error hover:bg-status-error/25',
              'disabled:opacity-40 disabled:cursor-not-allowed',
            )}
          >
            {/* Both labels name the resolved path, never an abstract noun
                (the docker/for-mac#6758 tier-confusion countermeasure). */}
            {purge ? `Reset and delete ${preview.confirm_phrase}` : `Reset (keep ${preview.confirm_phrase})`}
          </button>
        </div>
      </div>
    </div>
  );
}

// --- running ---------------------------------------------------------------

// Mirrors the engine's phase order (pkg/resetops/execute.go): backup,
// daemons, projections, contexts, wiring, containers, state files.
const RUN_STEPS = [
  'Writing backup',
  'Stopping daemons',
  'Removing projections',
  'Removing context rules',
  'Removing gateway entries from client configs',
  'Removing containers and networks',
  'Removing state files',
];

function RunningView({ tier, gridctlDir }: { tier: Tier; gridctlDir: string }) {
  return (
    <div className="space-y-3 text-xs" role="status" aria-live="polite">
      <p className="flex items-center gap-2 font-medium text-text-primary">
        <Loader2 size={13} className="animate-spin text-status-error" aria-hidden="true" />
        Resetting… this dialog stays open until the result arrives.
      </p>
      <ul className="space-y-1 text-[11px] text-text-secondary">
        {RUN_STEPS.map((step) => (
          <li key={step}>{step}</li>
        ))}
        {tier === 'purge' && <li className="text-status-error">Deleting {gridctlDir}</li>}
      </ul>
    </div>
  );
}

// --- result ----------------------------------------------------------------

// The actions the engine emits for completed work (execute.go); preview
// forms never appear in a result document.
const DONE_ACTIONS = new Set(['removed', 'stopped', 'dropped-record']);

function ResultView({
  result,
  execError,
  onClose,
}: {
  result: ResetDoc | null;
  execError: string | null;
  onClose: () => void;
}) {
  if (!result) {
    // A failed execute (stale token, 409, transport error) changed nothing
    // server-side: Close keeps the app as it was; reloading is for the
    // success path only.
    return (
      <div className="space-y-3 text-xs">
        <div className="flex items-start gap-2 rounded-md border border-status-error/40 bg-status-error/[0.06] px-3 py-2" role="alert">
          <AlertTriangle size={12} className="text-status-error flex-shrink-0 mt-0.5" aria-hidden="true" />
          <p className="text-[11px] text-status-error">{execError ?? 'Reset failed before it could report a result.'}</p>
        </div>
        <p className="text-[10px] text-text-muted">
          Nothing is removed until the backup succeeds, and reset is idempotent: fix the cause and
          run it again.
        </p>
        <div className="flex justify-end">
          <FooterButton onClick={onClose}>Close</FooterButton>
        </div>
      </div>
    );
  }

  const rows = result.rows ?? [];
  const removed = rows.filter((r) => DONE_ACTIONS.has(r.action)).length;
  const failedRows = rows.filter((r) => r.action === 'failed');

  return (
    <div className="space-y-3 text-xs">
      <div
        role="alert"
        className={cn(
          'rounded-md border px-3 py-2 space-y-1',
          result.failed > 0
            ? 'border-status-pending/40 bg-status-pending/[0.06]'
            : 'border-status-running/30 bg-status-running/[0.06]',
        )}
      >
        <p className={cn('font-medium', result.failed > 0 ? 'text-status-pending' : 'text-status-running')}>
          {removed} removed{result.failed > 0 ? ` · ${result.failed} failed` : ''}
          {result.kept?.length ? ` · ${result.kept.length} kept` : ''}
        </p>
        {result.failed > 0 && (
          <p className="text-[11px] text-text-secondary">
            Reset is idempotent; run it again to retry the failures.
          </p>
        )}
      </div>

      {failedRows.length > 0 && (
        <ul className="space-y-0.5 text-[11px]">
          {failedRows.map((r, i) => (
            <li key={i} className="text-status-error">
              <span className="font-mono">{r.kind} {r.name}</span>
              {r.error ? <span className="text-text-secondary">: {r.error}</span> : null}
            </li>
          ))}
        </ul>
      )}

      {result.backup_path && (
        <p className="flex items-start gap-1.5 text-[10px] text-text-muted">
          <Archive size={11} className="flex-shrink-0 mt-px" aria-hidden="true" />
          <span>
            Backup saved: <span className="font-mono">{result.backup_path}</span>. A safety copy,
            not an undo; recover forward with gridctl apply, pack add, and link.
          </span>
        </p>
      )}

      <p className="text-[10px] text-text-muted">The interface will reload into the empty state.</p>
      <div className="flex justify-end">
        <DoneButton />
      </div>
    </div>
  );
}

/** Both tiers reload on Done: the serving daemon was inside the blast
 *  radius (a purge exits it outright), so the stores describe a world
 *  that no longer exists. The poller already owns the dead-gateway
 *  state if the daemon is gone. */
function DoneButton() {
  return (
    <button
      type="button"
      onClick={() => pageReload.current()}
      className="rounded-md px-3 py-1.5 text-[11px] font-medium border border-primary/40 bg-primary/10 text-primary hover:bg-primary/20 transition-colors"
    >
      Done
    </button>
  );
}

// --- shared ----------------------------------------------------------------

function SectionHeading({ children }: { children: React.ReactNode }) {
  return (
    <h3 className="mb-1.5 text-[10px] font-semibold uppercase tracking-[0.2em] text-text-primary">
      {children}
    </h3>
  );
}

function FooterButton({
  onClick,
  autoFocus,
  children,
}: {
  onClick: () => void;
  autoFocus?: boolean;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      // Cancel-first focus inside an alert flow is the repo's safety convention.
      autoFocus={autoFocus}
      className="rounded-md px-3 py-1.5 text-[11px] font-medium border border-border/40 bg-background/40 text-text-secondary hover:text-text-primary hover:border-border transition-colors"
    >
      {children}
    </button>
  );
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ['KiB', 'MiB', 'GiB'];
  let value = n;
  let unit = 'B';
  for (const u of units) {
    if (value < 1024) break;
    value /= 1024;
    unit = u;
  }
  return `${value.toFixed(value >= 10 ? 0 : 1)} ${unit}`;
}
