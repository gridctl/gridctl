import { useCallback, useEffect, useState } from 'react';
import { CloudUpload, FileText, RefreshCw } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Modal } from '../ui/Modal';
import { IconButton } from '../ui/IconButton';
import { showToast } from '../ui/Toast';
import { MarkdownPreview } from '../registry/MarkdownPreview';
import { useContextStore } from '../../stores/useContextStore';
import {
  adoptGlobalContext,
  fetchGlobalContextDiff,
  initGlobalContext,
  saveGlobalContext,
  scanGlobalContext,
  syncGlobalContext,
  unsyncGlobalContext,
  type ContextClientStatus,
  type ContextDoc,
  type ContextScanEntry,
  type ContextState,
  type ContextSyncResult,
} from '../../lib/api';

interface GlobalContextDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

/**
 * Global Context management surface: one canonical AGENTS.md, edited here
 * and projected into each linked client's global context file. When no
 * canonical file exists yet, an adoption-first setup view scans the known
 * client locations and offers import-or-template — nothing is written
 * until the user chooses. Per-project AGENTS.md files are out of scope
 * (they stay version-controlled in each repo).
 */
export function GlobalContextDialog({ isOpen, onClose }: GlobalContextDialogProps) {
  const doc = useContextStore((s) => s.doc);
  const loading = useContextStore((s) => s.loading);
  const error = useContextStore((s) => s.error);
  const refresh = useContextStore((s) => s.refresh);

  useEffect(() => {
    if (isOpen) void refresh();
  }, [isOpen, refresh]);

  return (
    <Modal isOpen={isOpen} onClose={onClose} title="Global Context" size="full" expandable>
      {loading && !doc && (
        <div className="h-40 flex items-center justify-center text-sm text-text-muted">
          Loading global context…
        </div>
      )}
      {error && !doc && (
        <div className="h-40 flex items-center justify-center text-sm text-status-error">{error}</div>
      )}
      {error && doc && (
        <div role="alert" className="mb-2 text-xs text-status-error">
          Refresh failed: {error}
        </div>
      )}
      {doc && !doc.canonical.exists && <SetupView />}
      {doc && doc.canonical.exists && <EditorView doc={doc} />}
    </Modal>
  );
}

/**
 * Adoption-first setup: scan every client's likely global context
 * location, then let the user import one file, or start from the short
 * starter template. The scan itself never writes.
 */
function SetupView() {
  const setDoc = useContextStore((s) => s.setDoc);
  const [entries, setEntries] = useState<ContextScanEntry[] | null>(null);
  const [choice, setChoice] = useState<string>('template');
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    scanGlobalContext()
      .then((es) => {
        if (!cancelled) setEntries(es);
      })
      .catch((err) => {
        if (!cancelled) {
          setEntries([]);
          showToast('error', err instanceof Error ? err.message : 'Scan failed');
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const existing = (entries ?? []).filter((e) => e.exists);

  const handleCreate = useCallback(async () => {
    setBusy(true);
    try {
      const doc =
        choice === 'template'
          ? await initGlobalContext({ source: 'template' })
          : await initGlobalContext({ source: 'client', client: choice });
      setDoc(doc);
      showToast('success', 'Canonical global context created');
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Setup failed');
    } finally {
      setBusy(false);
    }
  }, [choice, setDoc]);

  if (entries === null) {
    return (
      <div className="h-40 flex items-center justify-center text-sm text-text-muted">
        Scanning client context files…
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-4 max-w-2xl">
      <div className="flex items-start gap-3">
        <div className="p-3 rounded-xl bg-surface-elevated/50 border border-border/30">
          <FileText size={22} className="text-text-muted/60" />
        </div>
        <div>
          <p className="text-sm text-text-primary font-medium">Set up your global context</p>
          <p className="text-xs text-text-muted mt-1">
            One canonical AGENTS.md holds your cross-project preferences (style, commit
            conventions, tone, tools) and syncs to every linked client. Per-project AGENTS.md
            files stay in their repos and are never touched.
          </p>
        </div>
      </div>

      <fieldset className="flex flex-col gap-2" aria-label="Choose a source">
        {[
          ...existing.map((e) => ({
            value: e.slug,
            label: `Import from ${e.name}`,
            hint: `${e.path} (${e.size} bytes)`,
            mono: true,
          })),
          {
            value: 'template',
            label: 'Start from the starter template',
            hint: 'a short draft to trim, not a finished file',
            mono: false,
          },
        ].map((opt) => (
          <label
            key={opt.value}
            className={cn(
              'flex items-center gap-2.5 px-3 py-2 rounded-lg border cursor-pointer transition-colors',
              choice === opt.value
                ? 'border-primary/40 bg-primary/10'
                : 'border-border/40 hover:bg-surface-highlight',
            )}
          >
            <input
              type="radio"
              name="context-source"
              value={opt.value}
              checked={choice === opt.value}
              onChange={() => setChoice(opt.value)}
            />
            <span className="text-sm text-text-primary">{opt.label}</span>
            <span className={cn('text-[11px] text-text-muted truncate', opt.mono && 'font-mono')}>
              {opt.hint}
            </span>
          </label>
        ))}
      </fieldset>

      <div>
        <button
          onClick={() => void handleCreate()}
          disabled={busy}
          className="px-4 py-2 text-xs font-medium text-primary bg-primary/10 hover:bg-primary/15 border border-primary/20 rounded-lg transition-colors disabled:opacity-60"
        >
          {busy ? 'Creating…' : 'Create canonical file'}
        </button>
      </div>
    </div>
  );
}

/** Split editor + per-client sync state for an existing canonical file. */
function EditorView({ doc }: { doc: ContextDoc }) {
  const setDoc = useContextStore((s) => s.setDoc);
  const refresh = useContextStore((s) => s.refresh);
  // null draft = pristine (textarea mirrors the canonical content), so a
  // background refresh never clobbers in-progress typing.
  const [draft, setDraft] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [driftSlug, setDriftSlug] = useState<string | null>(null);

  const content = draft ?? doc.canonical.content;
  const dirty = draft !== null && draft !== doc.canonical.content;

  const handleSave = useCallback(async () => {
    if (!dirty || draft === null) return;
    const toSave = draft;
    setSaving(true);
    try {
      const next = await saveGlobalContext(toSave);
      setDoc(next);
      // Only clear the draft if nothing was typed while the PUT was in
      // flight; otherwise those keystrokes would be discarded.
      setDraft((d) => (d === toSave ? null : d));
      showToast('success', 'Canonical context saved. Run a sync to propagate.');
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  }, [dirty, draft, setDoc]);

  const handleSyncAll = useCallback(async () => {
    setSyncing(true);
    try {
      const resp = await syncGlobalContext();
      showToast(resp.has_failures ? 'warning' : 'success', summarizeSync(resp.results));
      await refresh();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Sync failed');
    } finally {
      setSyncing(false);
    }
  }, [refresh]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 's') {
        e.preventDefault();
        void handleSave();
      }
    },
    [handleSave],
  );

  return (
    <div className="flex flex-col gap-4 h-full min-h-0">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <span className="text-[11px] text-text-muted font-mono truncate" title={doc.canonical.path}>
          {doc.canonical.path}
        </span>
        <div className="flex items-center gap-2">
          <button
            onClick={() => void handleSave()}
            disabled={!dirty || saving}
            className="px-3 py-1.5 text-xs font-medium text-primary bg-primary/10 hover:bg-primary/15 border border-primary/20 rounded-lg transition-colors disabled:opacity-50"
          >
            {saving ? 'Saving…' : dirty ? 'Save' : 'Saved'}
          </button>
          <button
            onClick={() => void handleSyncAll()}
            disabled={syncing || dirty}
            title={dirty ? 'Save before syncing' : 'Sync every available client'}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-emerald-400 border border-emerald-400/25 hover:bg-emerald-400/10 rounded-lg transition-colors disabled:opacity-50"
          >
            <CloudUpload size={12} aria-hidden="true" className={syncing ? 'animate-pulse' : undefined} />
            {syncing ? 'Syncing…' : 'Sync all'}
          </button>
          <IconButton
            icon={RefreshCw}
            onClick={() => void refresh()}
            tooltip="Refresh status"
            size="sm"
            variant="ghost"
          />
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-3 flex-1 min-h-0" style={{ minHeight: '260px' }}>
        <textarea
          value={content}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={handleKeyDown}
          aria-label="Canonical global context"
          spellCheck={false}
          className="w-full h-full min-h-[240px] resize-none bg-background/60 border border-border/40 rounded-lg p-3 text-xs font-mono text-text-primary focus:outline-none focus:border-primary/50 scrollbar-dark"
        />
        <div className="hidden lg:block overflow-y-auto border border-border/30 rounded-lg p-3 scrollbar-dark">
          <MarkdownPreview content={content} emptyHint="Canonical context preview" />
        </div>
      </div>

      <ClientList clients={doc.clients} onReviewDrift={setDriftSlug} />

      {driftSlug && (
        <DriftResolveDialog
          slug={driftSlug}
          name={doc.clients.find((c) => c.slug === driftSlug)?.name ?? driftSlug}
          onClose={() => setDriftSlug(null)}
          onResolved={(next) => {
            setDriftSlug(null);
            if (next) {
              setDoc(next);
              // Adopting changed the canon; keep any unsaved typing rather
              // than silently discarding it.
              if (!dirty) setDraft(null);
            } else {
              void refresh();
            }
          }}
        />
      )}
    </div>
  );
}

const STATE_STYLE: Record<ContextState, string> = {
  'in-sync': 'text-emerald-400 border-emerald-400/25 bg-emerald-400/10',
  stale: 'text-amber-300 border-amber-400/30 bg-amber-400/10',
  drifted: 'text-red-400 border-red-400/25 bg-red-400/10',
  'target-missing': 'text-red-400 border-red-400/25 bg-red-400/10',
  'never-synced': 'text-text-muted border-border/40 bg-background/40',
  unsupported: 'text-text-muted/60 border-border/30 bg-background/30',
};

/** Per-client sync state rows with inline actions. */
function ClientList({
  clients,
  onReviewDrift,
}: {
  clients: ContextClientStatus[];
  onReviewDrift: (slug: string) => void;
}) {
  const refresh = useContextStore((s) => s.refresh);
  const [busySlug, setBusySlug] = useState<string | null>(null);

  const act = useCallback(
    async (slug: string, fn: () => Promise<unknown>, okMessage: string) => {
      setBusySlug(slug);
      try {
        await fn();
        showToast('success', okMessage);
        await refresh();
      } catch (err) {
        showToast('error', err instanceof Error ? err.message : 'Action failed');
      } finally {
        setBusySlug(null);
      }
    },
    [refresh],
  );

  return (
    <div className="flex-shrink-0 border border-border/30 rounded-lg overflow-hidden">
      <div className="px-3 py-2 bg-surface/60 border-b border-border/30 text-[10px] uppercase tracking-wider text-text-muted">
        Clients
      </div>
      <ul className="divide-y divide-border/20 max-h-56 overflow-y-auto scrollbar-dark">
        {clients.map((c) => (
          <li key={c.slug} className="flex items-center gap-2.5 px-3 py-2">
            <span
              className={cn(
                'text-[10px] px-2 py-0.5 rounded-full border font-medium whitespace-nowrap',
                STATE_STYLE[c.state],
              )}
            >
              {c.state}
            </span>
            <span className="text-xs text-text-primary whitespace-nowrap">
              {c.name}
              {c.experimental && c.supported && (
                <span className="ml-1 text-[10px] text-amber-300/80">(experimental)</span>
              )}
            </span>
            <span className="text-[11px] text-text-muted font-mono truncate flex-1" title={c.target_path ?? c.detail}>
              {c.supported ? c.target_path : c.detail}
              {c.supported && !c.available && c.state === 'never-synced' && ' (client not detected)'}
            </span>
            <span className="flex items-center gap-1.5">
              {c.state === 'drifted' && (
                <ClientAction label="Review" onClick={() => onReviewDrift(c.slug)} disabled={busySlug !== null} />
              )}
              {(c.state === 'stale' || c.state === 'target-missing' || (c.state === 'never-synced' && c.available)) && (
                <ClientAction
                  label={busySlug === c.slug ? 'Syncing…' : 'Sync'}
                  disabled={busySlug !== null}
                  onClick={() =>
                    void act(c.slug, () => syncGlobalContext({ clients: [c.slug] }), `${c.name} synced`)
                  }
                />
              )}
              {(c.state === 'in-sync' || c.state === 'stale' || c.state === 'drifted') && (
                <ClientAction
                  label="Unsync"
                  subtle
                  disabled={busySlug !== null}
                  onClick={() =>
                    void act(c.slug, () => unsyncGlobalContext(c.slug), `${c.name} unsynced`)
                  }
                />
              )}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function ClientAction({
  label,
  onClick,
  disabled,
  subtle,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  subtle?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'px-2 py-0.5 rounded-md text-[11px] font-medium border transition-colors disabled:opacity-50',
        subtle
          ? 'text-text-muted border-border/40 hover:bg-surface-highlight'
          : 'text-primary border-primary/25 hover:bg-primary/10',
      )}
    >
      {label}
    </button>
  );
}

/**
 * Three-way drift resolution, mirroring the chezmoi model: adopt the hand
 * edit into the canon, overwrite the client from the canon, or cancel.
 * Never silently overwrites.
 */
function DriftResolveDialog({
  slug,
  name,
  onClose,
  onResolved,
}: {
  slug: string;
  name: string;
  onClose: () => void;
  onResolved: (doc: ContextDoc | null) => void;
}) {
  const [diff, setDiff] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    fetchGlobalContextDiff(slug)
      .then((d) => {
        if (!cancelled) setDiff(d);
      })
      .catch((err) => {
        if (!cancelled) setDiff(err instanceof Error ? err.message : 'Diff unavailable');
      });
    return () => {
      cancelled = true;
    };
  }, [slug]);

  const handleAdopt = useCallback(async () => {
    setBusy(true);
    try {
      const doc = await adoptGlobalContext(slug);
      showToast('success', `Adopted ${name}'s edit into the canonical context`);
      onResolved(doc);
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Adopt failed');
      setBusy(false);
    }
  }, [slug, name, onResolved]);

  const handleOverwrite = useCallback(async () => {
    setBusy(true);
    try {
      await syncGlobalContext({ clients: [slug], force: true });
      showToast('success', `${name} restored from the canonical context`);
      onResolved(null);
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Overwrite failed');
      setBusy(false);
    }
  }, [slug, name, onResolved]);

  return (
    <Modal isOpen onClose={onClose} title={`${name} was edited`} size="wide">
      <div className="flex flex-col gap-3">
        <p className="text-xs text-text-muted">
          The managed content in {name}'s file differs from the canonical context. Adopt the
          edit to make it the new canon, or overwrite the client to restore the canon. A
          timestamped backup precedes every write.
        </p>
        <pre className="text-[11px] font-mono bg-background/60 border border-border/30 rounded-lg p-3 overflow-x-auto max-h-72 overflow-y-auto scrollbar-dark whitespace-pre">
          {diff ?? 'Loading diff…'}
        </pre>
        <div className="flex items-center justify-end gap-2">
          <button
            onClick={onClose}
            disabled={busy}
            className="px-3 py-1.5 text-xs text-text-muted border border-border/40 rounded-lg hover:bg-surface-highlight transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={() => void handleAdopt()}
            disabled={busy}
            className="px-3 py-1.5 text-xs font-medium text-primary bg-primary/10 border border-primary/25 rounded-lg hover:bg-primary/15 transition-colors disabled:opacity-50"
          >
            Adopt into canon
          </button>
          <button
            onClick={() => void handleOverwrite()}
            disabled={busy}
            className="px-3 py-1.5 text-xs font-medium text-red-400 border border-red-400/25 rounded-lg hover:bg-red-400/10 transition-colors disabled:opacity-50"
          >
            Overwrite client
          </button>
        </div>
      </div>
    </Modal>
  );
}

/** One-line summary of a sync pass for the toast. */
function summarizeSync(results: ContextSyncResult[]): string {
  const counts: Record<string, number> = {};
  for (const r of results) counts[r.action] = (counts[r.action] ?? 0) + 1;
  const parts: string[] = [];
  const written = (counts['created'] ?? 0) + (counts['updated'] ?? 0);
  if (written) parts.push(`${written} synced`);
  if (counts['unchanged']) parts.push(`${counts['unchanged']} unchanged`);
  if (counts['skipped-drift']) parts.push(`${counts['skipped-drift']} skipped (drifted)`);
  if (counts['skipped-unavailable']) parts.push(`${counts['skipped-unavailable']} unavailable`);
  if (counts['error']) parts.push(`${counts['error']} failed`);
  return parts.length ? `Sync: ${parts.join(', ')}` : 'Nothing to sync';
}
