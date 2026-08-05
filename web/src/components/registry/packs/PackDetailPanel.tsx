import { useCallback, useEffect, useRef, useState } from 'react';
import { useNavigate } from 'react-router';
import { AlertTriangle, Package, RefreshCw, Trash2, Upload, X, GitBranch } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { IconButton } from '../../ui/IconButton';
import { StatePill } from '../../ui/StatePill';
import { showToast } from '../../ui/Toast';
import {
  applyPack,
  fetchPackDetail,
  HTTPError,
  type PackDetail,
  type PackRow,
} from '../../../lib/api';
import { describeApplyDoc, groupPackRows, rowPillState } from './packModel';
import { PackApplyForceDialog } from './PackApplyForceDialog';
import { PackRemoveDialog } from './PackRemoveDialog';
import { PackUpdateDialog } from './PackUpdateDialog';

interface PackDetailPanelProps {
  name: string | null;
  onClose: () => void;
  /** Refetch the list after any mutation (packs are off the global poll). */
  onChanged: () => Promise<void> | void;
}

/**
 * One pack's detail: manifest summary, header verbs (Apply, Update from
 * origin, Refresh, Remove), and the child-resource table grouped by kind
 * in manifest order, attention-first within each group.
 */
export function PackDetailPanel({ name, onClose, onChanged }: PackDetailPanelProps) {
  const navigate = useNavigate();
  const [detail, setDetail] = useState<PackDetail | null>(null);
  // A 409 collision is a page-level banner naming both repos, never a
  // generic toast.
  const [collisionMessage, setCollisionMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [lastApply, setLastApply] = useState<{ applied: number; total: number } | null>(null);
  const [forceDialog, setForceDialog] = useState<{ drifted: PackRow[] } | null>(null);
  const [showRemove, setShowRemove] = useState(false);
  const [showUpdate, setShowUpdate] = useState(false);

  // nameRef guards imperative loads: a slow response resolving after a
  // pack switch must never overwrite the newly selected pack's detail.
  const nameRef = useRef(name);
  useEffect(() => {
    nameRef.current = name;
  }, [name]);
  const load = useCallback(async () => {
    if (!name) return;
    try {
      const d = await fetchPackDetail(name);
      if (nameRef.current !== name) return;
      setDetail(d);
      setCollisionMessage(null);
    } catch (err) {
      if (nameRef.current !== name) return;
      if (err instanceof HTTPError && err.status === 409) {
        setDetail(null);
        setCollisionMessage(err.message);
        return;
      }
      showToast('error', err instanceof Error ? err.message : 'Failed to load pack');
    }
  }, [name]);

  // Detail rows stay null until the first fetch for this pack resolves;
  // switching packs derives back to loading instead of showing the
  // previous pack's rows.
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  if (name !== loadedFor) {
    setLoadedFor(name);
    setDetail(null);
    setCollisionMessage(null);
    setLastApply(null);
  }
  useEffect(() => {
    if (!name) return;
    let cancelled = false;
    fetchPackDetail(name)
      .then((d) => {
        if (cancelled) return;
        setDetail(d);
        setCollisionMessage(null);
      })
      .catch((err) => {
        if (cancelled) return;
        if (err instanceof HTTPError && err.status === 409) {
          setDetail(null);
          setCollisionMessage(err.message);
          return;
        }
        showToast('error', err instanceof Error ? err.message : 'Failed to load pack');
      });
    return () => {
      cancelled = true;
    };
  }, [name]);

  const refreshAll = useCallback(async () => {
    await load();
    await onChanged();
  }, [load, onChanged]);

  const handleRefresh = useCallback(async () => {
    if (busy) return;
    setBusy('refresh');
    try {
      await refreshAll();
    } finally {
      setBusy(null);
    }
  }, [busy, refreshAll]);

  const runApply = useCallback(
    async (force: boolean) => {
      if (!name || busy) return;
      setBusy('apply');
      try {
        const doc = await applyPack(name, force ? { force: true } : undefined);
        setLastApply({ applied: doc.applied, total: doc.total });
        const outcome = describeApplyDoc(doc);
        showToast(outcome.kind, outcome.message);
        await refreshAll();
        // The force follow-up: when a plain apply skipped drifted rows,
        // offer the overwrite path instead of leaving CLI prose as the
        // only remediation. Foreign skips are excepted: force does not
        // apply to them, and their rows name the owning pack.
        if (!force && outcome.driftedSkips.length > 0) {
          setForceDialog({ drifted: outcome.driftedSkips });
        }
      } catch (err) {
        showToast('error', err instanceof Error ? err.message : 'Apply failed');
      } finally {
        setBusy(null);
      }
    },
    [name, busy, refreshAll],
  );

  if (!name) {
    return (
      <div className="h-full flex flex-col items-center justify-center text-text-muted gap-2 p-6 text-center">
        <Package size={24} className="text-text-muted/40" />
        <span className="text-xs">Select a pack to inspect its resources.</span>
      </div>
    );
  }

  if (collisionMessage) {
    return (
      <div className="h-full flex flex-col">
        <PanelHeader name={name} onClose={onClose} />
        <div
          role="alert"
          className="m-4 rounded-xl border border-red-400/30 bg-red-400/10 p-4 text-xs text-text-primary space-y-2"
        >
          <div className="flex items-center gap-2 font-medium text-red-400">
            <AlertTriangle size={14} /> Pack name collision
          </div>
          <p className="text-text-secondary">{collisionMessage}</p>
          <p className="text-text-muted">
            Two imported sources claim this pack name, so gridctl refuses to pick one.
            Remove one of the sources, or re-add one origin so a single source owns the
            name.
          </p>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full flex flex-col overflow-hidden">
      <PanelHeader name={name} onClose={onClose} />

      {detail === null ? (
        <div className="flex-1 flex items-center justify-center text-xs text-text-muted">
          Loading pack…
        </div>
      ) : (
        <div className="flex-1 overflow-y-auto scrollbar-dark">
          {/* Manifest summary. */}
          <div className="px-4 pt-3 pb-2 border-b border-border/30">
            {detail.info.description && (
              <p className="text-xs text-text-secondary">{detail.info.description}</p>
            )}
            <div className="flex items-center gap-2 mt-1.5 text-[10px] text-text-muted font-mono flex-wrap">
              {detail.info.version && <span>v{detail.info.version}</span>}
              {detail.info.author && <span>by {detail.info.author}</span>}
              <span className="inline-flex items-center gap-1 truncate max-w-full" title={detail.info.origin.repo}>
                <GitBranch size={10} aria-hidden="true" />
                {detail.info.origin.repo}
                {detail.info.origin.ref ? `@${detail.info.origin.ref}` : ''}
              </span>
              {detail.info.origin.commit_sha && (
                <span title={detail.info.origin.commit_sha}>
                  {detail.info.origin.commit_sha.slice(0, 7)}
                </span>
              )}
            </div>
            {lastApply && (
              <p className="text-[11px] text-text-muted mt-1.5" aria-live="polite">
                Applied {lastApply.applied}/{lastApply.total} resources.
              </p>
            )}
            {!detail.info.applied && (
              <p className="text-[11px] text-text-muted mt-1.5">
                Imported into the registry. Apply projects skills, agents, rules, and wiring
                to your clients.
              </p>
            )}
            {/* Header verbs; busy state disables all four. */}
            <div className="flex items-center gap-2 mt-2.5 pb-1 flex-wrap">
              <button
                onClick={() => void runApply(false)}
                disabled={busy !== null}
                aria-busy={busy === 'apply'}
                className={cn(
                  'px-3 py-1.5 text-xs font-medium rounded-lg transition-colors disabled:opacity-50',
                  detail.needs_attention || !detail.info.applied
                    ? 'bg-primary text-background hover:bg-primary/90'
                    : 'text-primary bg-primary/10 border border-primary/25 hover:bg-primary/15',
                )}
              >
                {busy === 'apply' ? 'Applying…' : 'Apply'}
              </button>
              <button
                onClick={() => setShowUpdate(true)}
                disabled={busy !== null}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs text-text-muted border border-border/40 hover:bg-surface-highlight rounded-lg transition-colors disabled:opacity-50"
                title="Re-import from the stored origin: refreshes changed upstream rules and the selection"
              >
                <Upload size={11} aria-hidden="true" /> Update from origin
              </button>
              <IconButton icon={RefreshCw} onClick={() => void handleRefresh()} tooltip="Refresh status" size="sm" variant="ghost" />
              <button
                onClick={() => setShowRemove(true)}
                disabled={busy !== null}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs text-red-400 border border-red-400/25 hover:bg-red-400/10 rounded-lg transition-colors disabled:opacity-50"
              >
                <Trash2 size={11} aria-hidden="true" /> Remove
              </button>
            </div>
          </div>

          {/* Child resources, grouped by kind in manifest order. */}
          <div className="px-4 py-3 flex flex-col gap-4">
            {detail.rows.length === 0 && (
              <p className="text-[11px] text-text-muted">
                Nothing projected yet. Apply to see per-resource state here.
              </p>
            )}
            {groupPackRows(detail.rows).map((group) => (
              <section key={group.kind} aria-label={`${group.label} rows`}>
                <div className="text-[10px] uppercase tracking-wider font-medium text-text-muted mb-1.5">
                  {group.label}
                </div>
                <ul className="flex flex-col divide-y divide-border/20 rounded-lg border border-border/30 bg-background/30">
                  {group.rows.map((row, i) => (
                    <PackRowLine
                      key={`${row.kind}:${row.name}:${row.client ?? ''}:${i}`}
                      row={row}
                      onNavigate={navigate}
                    />
                  ))}
                </ul>
              </section>
            ))}
          </div>
        </div>
      )}

      {forceDialog && name && (
        <PackApplyForceDialog
          packName={name}
          driftedRows={forceDialog.drifted}
          busy={busy === 'apply'}
          onCancel={() => setForceDialog(null)}
          onOverwrite={() => {
            setForceDialog(null);
            void runApply(true);
          }}
        />
      )}
      {showRemove && name && (
        <PackRemoveDialog
          packName={name}
          onClose={() => setShowRemove(false)}
          onRemoved={async (stillExists) => {
            setShowRemove(false);
            await onChanged();
            if (stillExists) {
              await load();
            } else {
              onClose();
            }
          }}
        />
      )}
      {showUpdate && name && detail && (
        <PackUpdateDialog
          packName={name}
          origin={detail.info.origin}
          onClose={() => setShowUpdate(false)}
          onUpdated={() => {
            setShowUpdate(false);
            void refreshAll();
          }}
        />
      )}
    </div>
  );
}

function PanelHeader({ name, onClose }: { name: string; onClose: () => void }) {
  return (
    <div className="flex items-center gap-2 px-4 py-2.5 border-b border-border/30 flex-shrink-0">
      <Package size={14} className="text-text-muted/70" aria-hidden="true" />
      <span className="text-sm font-medium text-text-primary truncate flex-1">{name}</span>
      <IconButton icon={X} onClick={onClose} tooltip="Close" size="sm" variant="ghost" />
    </div>
  );
}

/**
 * One child-resource line: pill or action, name, client, deep link where
 * a real surface exists, and the engine's detail and remediation prose
 * whenever the wire carries them.
 */
function PackRowLine({ row, onNavigate }: { row: PackRow; onNavigate: (to: string) => void }) {
  const pill = rowPillState(row);
  const link = rowLink(row);
  return (
    <li className="px-3 py-2">
      <div className="flex items-center gap-2">
        {pill ? (
          <StatePill state={pill} />
        ) : (
          <span className="text-[10px] px-2 py-0.5 rounded-full border border-border/40 bg-background/40 text-text-muted font-medium whitespace-nowrap">
            {row.action || 'pending'}
          </span>
        )}
        {link ? (
          <button
            onClick={() => onNavigate(link)}
            className="text-xs font-mono text-text-primary hover:text-primary underline-offset-2 hover:underline truncate text-left"
            title={`Open ${row.name}`}
          >
            {row.name}
          </button>
        ) : (
          <span className="text-xs font-mono text-text-primary truncate">{row.name}</span>
        )}
        {row.client && (
          <span className="text-[10px] text-text-muted font-mono whitespace-nowrap">{row.client}</span>
        )}
      </div>
      {row.detail && <p className="text-[11px] text-text-muted mt-1">{row.detail}</p>}
      {row.remediation && (
        <p className="text-[11px] text-text-secondary mt-0.5">{remediationProse(row)}</p>
      )}
      {isNoGatewayRow(row) && (
        <p className="text-[11px] text-text-secondary mt-0.5">
          Start a gateway from the Stack workspace, then apply again.
        </p>
      )}
    </li>
  );
}

/** Deep links only where a real surface exists; rule rows stay text. */
function rowLink(row: PackRow): string | null {
  switch (row.kind) {
    case 'skill':
      return `/library?kind=skill&selected=${encodeURIComponent(row.name)}`;
    case 'agent':
      return `/library?kind=agent&selected=${encodeURIComponent(row.name)}`;
    case 'wiring':
      return row.client ? `/connections?client=${encodeURIComponent(row.client)}` : null;
    default:
      return null;
  }
}

function isNoGatewayRow(row: PackRow): boolean {
  return row.kind === 'wiring' && (row.detail ?? '').includes('no running gateway');
}

/** The engine's remediation, rendered verbatim; every row that carries
 *  one shows it (the Connections lesson: remediation on the wire but
 *  invisible is a bug). */
function remediationProse(row: PackRow): string {
  const r = row.remediation ?? '';
  return r.charAt(0).toUpperCase() + r.slice(1);
}
