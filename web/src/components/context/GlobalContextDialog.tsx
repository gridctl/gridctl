import { useCallback, useEffect, useRef, useState } from 'react';
import {
  AlertCircle,
  Bold,
  Check,
  ChevronDown,
  ChevronRight,
  Code2,
  CloudUpload,
  Eye,
  EyeOff,
  File,
  FileDown,
  FileText,
  Heading,
  Layers,
  List,
  MonitorSmartphone,
  Plus,
  RefreshCw,
  Trash2,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { Modal } from '../ui/Modal';
import { IconButton } from '../ui/IconButton';
import { StatePill } from '../ui/StatePill';
import { PACK_CHIP_CLASS } from '../registry/PackChip';
import { showToast } from '../ui/Toast';
import { useSplitPane } from '../../hooks/useSplitPane';
import { SplitPaneHandle } from '../ui/SplitPane';
import { MarkdownPreview } from '../registry/MarkdownPreview';
import { applyMarkdownAction, type MarkdownAction } from '../../lib/markdownEdit';
import { useContextStore } from '../../stores/useContextStore';
import {
  adoptGlobalContext,
  deleteContextFragment,
  fetchContextFragments,
  fetchGlobalContextDiff,
  initGlobalContext,
  saveContextFragment,
  saveGlobalContext,
  scanGlobalContext,
  syncGlobalContext,
  unsyncGlobalContext,
  type ContextClientStatus,
  type ContextDoc,
  type ContextFragment,
  type ContextFragmentStatus,
  type ContextScanEntry,
  type ContextState,
  type ContextSyncResult,
} from '../../lib/api';

// Mirrors pkg/contexts/fragment_render.go: only Claude Code's rule-file
// dialect is a byte-identical projection of the source fragments, so only
// it can adopt a hand edit back. Every other multi-file dialect is lossy.
const IDENTITY_RENDER_SLUGS = new Set(['claude-code']);

// Strings the managed-block parser treats as boundaries; the backend
// rejects canonical content containing them, so the editor flags them
// live instead of surfacing a save error.
const RESERVED_MARKERS = [
  '<!-- BEGIN GRIDCTL MANAGED -->',
  '<!-- END GRIDCTL MANAGED -->',
  '<!-- Managed by gridctl.',
];

interface GlobalContextDialogProps {
  isOpen: boolean;
  /** Land on this client's drift review once the doc loads (the
   *  Connections hub's Review deep link). In fragments mode the client's
   *  row is spotlighted, and the review opens directly when the target
   *  is unambiguous (compiled, or exactly one drifted fragment). */
  initialDriftSlug?: string | null;
  onClose: () => void;
}

/**
 * Render-time deep-link seeding: run the seed exactly once per distinct
 * non-null initial value, during render, so the first paint already
 * reflects the deep link. Extracted into a hook because hand-copying the
 * adjust block between views has broken this file before.
 */
function useDriftSeed(initial: string | null | undefined, seed: (slug: string) => void) {
  const [seeded, setSeeded] = useState<string | null>(null);
  const value = initial ?? null;
  if (value !== seeded) {
    setSeeded(value);
    if (value) seed(value);
  }
}

/**
 * Global Context management surface: one canonical AGENTS.md, edited here
 * and projected into each linked client's global context file. When no
 * canonical file exists yet, an adoption-first setup view scans the known
 * client locations and offers import-or-template — nothing is written
 * until the user chooses. Per-project AGENTS.md files are out of scope
 * (they stay version-controlled in each repo).
 *
 * The editor mirrors SkillEditor's grammar: collapsible strip, formatting
 * toolbar, resizable markdown/preview split, and a status bar.
 */
export function GlobalContextDialog({ isOpen, onClose, initialDriftSlug = null }: GlobalContextDialogProps) {
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
      {/* Fragments mode replaces the canonical file entirely, so it must
          route before the exists check (the store is a directory now). */}
      {doc && doc.fragments_active && (
        <FragmentsView doc={doc} refreshError={error} initialDriftSlug={initialDriftSlug} />
      )}
      {doc && !doc.fragments_active && !doc.canonical.exists && <SetupView />}
      {doc && !doc.fragments_active && doc.canonical.exists && (
        <EditorView doc={doc} refreshError={error} initialDriftSlug={initialDriftSlug} />
      )}
    </Modal>
  );
}

/**
 * Scan every client's likely global context location. The scan itself
 * never writes. Returns null while the scan is in flight.
 */
function useContextScan(): ContextScanEntry[] | null {
  const [entries, setEntries] = useState<ContextScanEntry[] | null>(null);

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

  return entries;
}

/**
 * Radio list of canonical-content sources: each existing client file
 * (with its path and size), plus the starter template. Shared between
 * first-run setup and the editor's Import dialog.
 */
function SourceOptions({
  existing,
  choice,
  onChoice,
  templateLabel,
}: {
  existing: ContextScanEntry[];
  choice: string;
  onChoice: (value: string) => void;
  templateLabel: string;
}) {
  return (
    // min-w-0 overrides the fieldset default min-inline-size:min-content,
    // which would otherwise let long mono paths blow past the max width.
    <fieldset className="flex flex-col gap-2 min-w-0" aria-label="Choose a source">
      {[
        ...existing.map((e) => ({
          value: e.slug,
          label: `Import from ${e.name}`,
          hint: `${e.path} (${e.size} bytes)`,
          mono: true,
        })),
        {
          value: 'template',
          label: templateLabel,
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
            onChange={() => onChoice(opt.value)}
          />
          <span className="text-sm text-text-primary whitespace-nowrap">{opt.label}</span>
          <span className={cn('text-[11px] text-text-muted truncate min-w-0 flex-1', opt.mono && 'font-mono')}>
            {opt.hint}
          </span>
        </label>
      ))}
    </fieldset>
  );
}

/**
 * Adoption-first setup: scan every client's likely global context
 * location, then let the user import one file, or start from the short
 * starter template. Defaults to the first existing file so adoption is
 * the primary path, not the template.
 */
function SetupView() {
  const setDoc = useContextStore((s) => s.setDoc);
  const entries = useContextScan();
  const [choice, setChoice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const existing = (entries ?? []).filter((e) => e.exists);
  const selected = choice ?? existing[0]?.slug ?? 'template';

  const handleCreate = useCallback(async () => {
    setBusy(true);
    try {
      const doc =
        selected === 'template'
          ? await initGlobalContext({ source: 'template' })
          : await initGlobalContext({ source: 'client', client: selected });
      setDoc(doc);
      showToast('success', 'Canonical global context created');
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Setup failed');
    } finally {
      setBusy(false);
    }
  }, [selected, setDoc]);

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
            conventions, tone, tools) and syncs to every linked client. It can later be split
            into rule fragments with per-client assembly. Per-project AGENTS.md files stay in
            their repos and are never touched.
          </p>
        </div>
      </div>

      <SourceOptions
        existing={existing}
        choice={selected}
        onChoice={setChoice}
        templateLabel="Start from the starter template"
      />

      <div>
        <button
          onClick={() => void handleCreate()}
          disabled={busy}
          className="px-4 py-2 text-xs font-medium rounded-lg transition-all bg-primary text-background hover:bg-primary/90 disabled:opacity-50 disabled:cursor-not-allowed"
        >
          {busy ? 'Creating…' : 'Create canonical file'}
        </button>
      </div>
    </div>
  );
}

/**
 * The setup-time source picker, reachable again after the canonical file
 * exists: replace the canon with an existing client file (or the starter
 * template). Goes through init with force; a timestamped backup of the
 * previous canonical precedes the write.
 */
function ImportSourceDialog({
  dirty,
  onClose,
  onImported,
}: {
  dirty: boolean;
  onClose: () => void;
  onImported: (doc: ContextDoc) => void;
}) {
  const entries = useContextScan();
  const [choice, setChoice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const existing = (entries ?? []).filter((e) => e.exists);
  const selected = choice ?? existing[0]?.slug ?? 'template';

  const handleImport = useCallback(async () => {
    setBusy(true);
    try {
      const doc =
        selected === 'template'
          ? await initGlobalContext({ source: 'template', force: true })
          : await initGlobalContext({ source: 'client', client: selected, force: true });
      showToast('success', 'Canonical context replaced. Run a sync to propagate.');
      onImported(doc);
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Import failed');
      setBusy(false);
    }
  }, [selected, onImported]);

  return (
    <Modal isOpen onClose={onClose} title="Import global context" size="wide">
      <div className="flex flex-col gap-3">
        <p className="text-xs text-text-muted">
          Replace the canonical context with an existing client file or the starter template.
          A timestamped backup of the current canonical file precedes the write.
          {dirty && ' Your unsaved editor changes will be discarded.'}
        </p>
        {entries === null ? (
          <div className="h-24 flex items-center justify-center text-sm text-text-muted">
            Scanning client context files…
          </div>
        ) : (
          <SourceOptions
            existing={existing}
            choice={selected}
            onChoice={setChoice}
            templateLabel="Reset to the starter template"
          />
        )}
        <div className="flex items-center justify-end gap-2">
          <button
            onClick={onClose}
            disabled={busy}
            className="px-3 py-1.5 text-xs text-text-muted border border-border/40 rounded-lg hover:bg-surface-highlight transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={() => void handleImport()}
            disabled={busy || entries === null}
            className="px-3 py-1.5 text-xs font-medium text-primary bg-primary/10 border border-primary/25 rounded-lg hover:bg-primary/15 transition-colors disabled:opacity-50"
          >
            {busy ? 'Replacing…' : 'Replace canonical'}
          </button>
        </div>
      </div>
    </Modal>
  );
}

/**
 * SkillEditor-grade editing surface: action header, collapsible clients
 * strip, resizable markdown/preview split with a formatting toolbar, and
 * a status bar with live marker validation and line/char counts.
 */
function EditorView({
  doc,
  refreshError,
  initialDriftSlug = null,
}: {
  doc: ContextDoc;
  refreshError: string | null;
  initialDriftSlug?: string | null;
}) {
  const setDoc = useContextStore((s) => s.setDoc);
  const refresh = useContextStore((s) => s.refresh);
  // null draft = pristine (textarea mirrors the canonical content), so a
  // background refresh never clobbers in-progress typing.
  const [draft, setDraft] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [driftSlug, setDriftSlug] = useState<string | null>(null);
  // Seed the drift review once from a caller's deep link; only when that
  // client is actually drifted, so a stale link never opens an empty review.
  useDriftSeed(initialDriftSlug, (slug) => {
    if (doc.clients.some((c) => c.slug === slug && c.state === 'drifted')) {
      setDriftSlug(slug);
    }
  });
  const [showImport, setShowImport] = useState(false);
  const [showSplit, setShowSplit] = useState(false);
  const [showPreview, setShowPreview] = useState(true);

  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const previewRef = useRef<HTMLDivElement>(null);
  const { ratio, containerRef, handleMouseDown, isDragging } = useSplitPane(0.5);

  const content = draft ?? doc.canonical.content;
  const dirty = draft !== null && draft !== doc.canonical.content;
  const markerIssue = RESERVED_MARKERS.find((m) => content.includes(m)) ?? null;
  const lineCount = content.split('\n').length;
  const charCount = content.length;

  const handleSave = useCallback(async () => {
    if (!dirty || draft === null || markerIssue) return;
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
  }, [dirty, draft, markerIssue, setDoc]);

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

  // Formatting toolbar: pure transform at the textarea cursor (shared
  // with SkillEditor via lib/markdownEdit).
  const applyMarkdown = useCallback(
    (action: MarkdownAction) => {
      const ta = bodyRef.current;
      if (!ta) return;
      const next = applyMarkdownAction(content, ta.selectionStart, ta.selectionEnd, action);
      setDraft(next.value);
      requestAnimationFrame(() => {
        ta.focus();
        ta.setSelectionRange(next.selStart, next.selEnd);
      });
    },
    [content],
  );

  // Proportional scroll sync: the preview follows the editor closely
  // enough to feel tethered (same approach as SkillEditor).
  const handleEditorScroll = useCallback((e: React.UIEvent<HTMLTextAreaElement>) => {
    const ta = e.currentTarget;
    const preview = previewRef.current;
    if (!preview) return;
    const srcMax = ta.scrollHeight - ta.clientHeight;
    if (srcMax <= 0) return;
    const dstMax = preview.scrollHeight - preview.clientHeight;
    preview.scrollTop = (ta.scrollTop / srcMax) * dstMax;
  }, []);

  return (
    // Escape the Modal body padding so the panes run edge to edge, exactly
    // like SkillEditor.
    <div className="flex flex-col h-[calc(100%+2rem)] -mx-6 -my-4">
      {/* Action header */}
      <div className="flex items-center justify-between gap-3 px-5 py-3 border-b border-border/30 flex-shrink-0">
        <span
          className="text-[11px] text-text-muted font-mono truncate min-w-0"
          title={doc.canonical.path}
        >
          {doc.canonical.path}
        </span>
        <div className="flex items-center gap-2 flex-shrink-0">
          <button
            onClick={() => setShowPreview((p) => !p)}
            title={showPreview ? 'Hide preview' : 'Show preview'}
            className={cn(
              'p-1.5 rounded-lg transition-all duration-200',
              showPreview
                ? 'text-text-muted hover:text-primary hover:bg-primary/10'
                : 'text-primary bg-primary/10',
            )}
          >
            {showPreview ? <Eye size={14} /> : <EyeOff size={14} />}
          </button>
          <IconButton
            icon={RefreshCw}
            onClick={() => void refresh()}
            tooltip="Refresh status"
            size="sm"
            variant="ghost"
          />
          <button
            onClick={() => setShowSplit(true)}
            title="Split the canonical file into rule fragments"
            className="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-text-muted border border-border/40 hover:bg-surface-highlight rounded-lg transition-colors"
          >
            <Layers size={12} aria-hidden="true" />
            Fragments
          </button>
          <button
            onClick={() => setShowImport(true)}
            title="Replace the canonical context from an existing client file"
            className="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-text-muted border border-border/40 hover:bg-surface-highlight rounded-lg transition-colors"
          >
            <FileDown size={12} aria-hidden="true" />
            Import
          </button>
          <button
            onClick={() => void handleSyncAll()}
            disabled={syncing || dirty}
            title={dirty ? 'Save before syncing' : 'Sync every available client'}
            className="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-emerald-400 border border-emerald-400/25 hover:bg-emerald-400/10 rounded-lg transition-colors disabled:opacity-50"
          >
            <CloudUpload size={12} aria-hidden="true" className={syncing ? 'animate-pulse' : undefined} />
            {syncing ? 'Syncing…' : 'Sync all'}
          </button>
          <button
            onClick={() => void handleSave()}
            disabled={!dirty || saving || !!markerIssue}
            className={cn(
              'px-4 py-2 text-xs font-medium rounded-lg transition-all',
              'bg-primary text-background hover:bg-primary/90',
              (!dirty || saving || !!markerIssue) && 'opacity-50 cursor-not-allowed',
            )}
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>

      {refreshError && (
        <div role="alert" className="px-5 py-2 bg-status-error/10 border-b border-status-error/30 flex-shrink-0 text-xs text-status-error">
          Refresh failed: {refreshError}
        </div>
      )}

      <ClientsStrip clients={doc.clients} onReviewDrift={setDriftSlug} />

      {/* Editor area: split pane when preview on, full-width when off */}
      <div ref={containerRef} className="flex-1 flex min-h-0 group/split">
        <div
          className={cn('flex flex-col min-w-0 min-h-0', showPreview && 'border-r border-border/30')}
          style={showPreview ? { width: `${ratio * 100}%` } : { width: '100%' }}
        >
          <div className="flex items-center justify-between gap-2 px-4 py-1.5 border-b border-border/20 flex-shrink-0">
            <span className="text-xs text-text-muted uppercase tracking-wider">Markdown</span>
            <div className="flex items-center gap-0.5">
              <EditorToolbarButton icon={Bold} label="Bold" onClick={() => applyMarkdown('bold')} />
              <EditorToolbarButton icon={Heading} label="Heading" onClick={() => applyMarkdown('heading')} />
              <EditorToolbarButton icon={List} label="List item" onClick={() => applyMarkdown('list')} />
              <EditorToolbarButton icon={Code2} label="Code block" onClick={() => applyMarkdown('code')} />
            </div>
          </div>
          <textarea
            ref={bodyRef}
            value={content}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={handleKeyDown}
            onScroll={handleEditorScroll}
            aria-label="Canonical global context"
            placeholder={'# Global Agent Context\n\nDurable cross-project preferences only...\n\n## Coding Style\n\n- Prefer clarity over cleverness.'}
            className="flex-1 w-full bg-background/40 px-5 py-4 text-sm font-mono text-text-primary placeholder:text-text-muted/30 resize-none focus:outline-none leading-relaxed"
            spellCheck={false}
          />
        </div>

        {showPreview && (
          <>
            <SplitPaneHandle onMouseDown={handleMouseDown} isDragging={isDragging} />
            <div className="flex flex-col min-w-0 min-h-0" style={{ width: `${(1 - ratio) * 100}%` }}>
              <div className="px-4 py-2 border-b border-border/20 flex-shrink-0">
                <span className="text-xs text-text-muted uppercase tracking-wider">Preview</span>
              </div>
              <div ref={previewRef} className="flex-1 overflow-y-auto scrollbar-dark">
                <div className="px-5 py-4">
                  <MarkdownPreview content={content} emptyHint="Canonical context preview" />
                </div>
              </div>
            </div>
          </>
        )}
      </div>

      {/* Bottom status bar */}
      <div className="flex items-center justify-between px-5 py-2 border-t border-border/30 bg-surface/50 flex-shrink-0">
        <div className="flex items-center gap-3">
          {markerIssue ? (
            <span className="text-xs flex items-center gap-1 text-status-error">
              <AlertCircle size={12} />
              contains the reserved gridctl marker {markerIssue}
            </span>
          ) : (
            <span className="text-xs flex items-center gap-1 text-status-running">
              <Check size={12} />
              Valid
            </span>
          )}
          {dirty && !markerIssue && (
            <span className="text-xs text-text-muted">Unsaved changes</span>
          )}
        </div>
        <div className="flex items-center gap-4 text-xs text-text-muted font-mono">
          <span>{lineCount} lines</span>
          <span>{charCount} chars</span>
        </div>
      </div>

      {showImport && (
        <ImportSourceDialog
          dirty={dirty}
          onClose={() => setShowImport(false)}
          onImported={(next) => {
            setShowImport(false);
            setDoc(next);
            // The import replaced the canon; any draft is now stale.
            setDraft(null);
          }}
        />
      )}

      {showSplit && <SplitIntoFragmentsDialog dirty={dirty} onClose={() => setShowSplit(false)} />}

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

function EditorToolbarButton({
  icon: Icon,
  label,
  onClick,
}: {
  icon: typeof Bold;
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      title={label}
      aria-label={label}
      className="p-1.5 rounded-md text-text-muted hover:text-primary hover:bg-primary/10 transition-colors"
    >
      <Icon size={13} />
    </button>
  );
}

// States that make the clients strip open itself: the user must act.
const ATTENTION_STATES: ContextState[] = ['drifted', 'stale', 'target-missing'];

/**
 * Collapsible per-client strip, modeled on SkillEditor's frontmatter and
 * files strips: a one-line summary when collapsed, the actionable client
 * rows when expanded. Auto-expands when any client needs attention.
 */
function ClientsStrip({
  clients,
  onReviewDrift,
  spotlightSlug = null,
}: {
  clients: ContextClientStatus[];
  onReviewDrift: (slug: string, fragment?: string) => void;
  /** Deep-link target: this client's row starts expanded and scrolled into view. */
  spotlightSlug?: string | null;
}) {
  const needsAttention = clients.some((c) => ATTENTION_STATES.includes(c.state));
  const [expanded, setExpanded] = useState(needsAttention);
  // Re-open when attention appears after a refresh (e.g. drift detected);
  // never force-close a strip the user opened.
  const [prevAttention, setPrevAttention] = useState(needsAttention);
  if (needsAttention !== prevAttention) {
    setPrevAttention(needsAttention);
    if (needsAttention) setExpanded(true);
  }

  const counts = clients.reduce<Record<string, number>>((acc, c) => {
    acc[c.state] = (acc[c.state] ?? 0) + 1;
    return acc;
  }, {});
  const summary = (['in-sync', 'stale', 'drifted', 'target-missing', 'never-synced', 'unsupported'] as const)
    .filter((s) => counts[s])
    .map((s) => `${counts[s]} ${s}`)
    .join(' · ');

  return (
    <div className="border-b border-border/30 bg-surface/40 flex-shrink-0">
      <button
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
        className="w-full flex items-center gap-2 px-5 py-2 text-left hover:bg-surface-highlight/40 transition-colors"
      >
        {expanded ? (
          <ChevronDown size={13} className="text-text-muted flex-shrink-0" />
        ) : (
          <ChevronRight size={13} className="text-text-muted flex-shrink-0" />
        )}
        <MonitorSmartphone size={13} className="text-text-muted/70 flex-shrink-0" aria-hidden="true" />
        <span className="text-xs text-text-muted uppercase tracking-wider">Clients</span>
        <span className="text-[11px] text-text-muted/80 truncate">{summary}</span>
        {needsAttention && !expanded && (
          <span className="flex-shrink-0 text-[9px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded-full border border-status-pending/30 bg-status-pending/10 text-status-pending">
            Needs attention
          </span>
        )}
      </button>
      {expanded && (
        <ul className="divide-y divide-border/20 max-h-56 overflow-y-auto scrollbar-dark border-t border-border/20">
          {clients.map((c) => (
            <ClientRow
              key={c.slug}
              client={c}
              onReviewDrift={onReviewDrift}
              spotlight={c.slug === spotlightSlug}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

/**
 * One client's row with inline actions. In fragments mode a multi-file
 * row with non-synced fragments grows a chevron that expands one line per
 * fragment (StatePill, name, and Review on drifted lines), so a drifted
 * fragment can never hide a stale one behind the summary.
 */
function ClientRow({
  client: c,
  onReviewDrift,
  spotlight = false,
}: {
  client: ContextClientStatus;
  onReviewDrift: (slug: string, fragment?: string) => void;
  spotlight?: boolean;
}) {
  const refresh = useContextStore((s) => s.refresh);
  const [busy, setBusy] = useState(false);
  const fragments = c.fragments ?? [];
  const [expanded, setExpanded] = useState(spotlight && fragments.length > 0);
  // A spotlighted row whose fragments arrive after mount (the deep link
  // landed on a stale doc) still expands when the refresh delivers them.
  const [hadFragments, setHadFragments] = useState(fragments.length > 0);
  if ((fragments.length > 0) !== hadFragments) {
    setHadFragments(fragments.length > 0);
    if (spotlight && fragments.length > 0) setExpanded(true);
  }
  const rowRef = useRef<HTMLLIElement>(null);
  useEffect(() => {
    if (spotlight) rowRef.current?.scrollIntoView?.({ block: 'nearest' });
  }, [spotlight]);

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

  return (
    <li ref={rowRef} className="px-5 py-2">
      <div className="flex items-center gap-2.5">
      {fragments.length > 0 && (
        <button
          onClick={() => setExpanded((e) => !e)}
          aria-expanded={expanded}
          aria-label={`${expanded ? 'Collapse' : 'Expand'} ${c.name} fragments`}
          className="p-0.5 -ml-1 rounded text-text-muted hover:text-text-primary transition-colors flex-shrink-0"
        >
          {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        </button>
      )}
      <StatePill state={c.state} />
      {c.mode && (
        <span
          title="How this client receives the context in fragments mode"
          className="text-[10px] px-1.5 py-0.5 rounded border border-border/40 bg-background/40 text-text-muted font-mono whitespace-nowrap"
        >
          {c.mode}
        </span>
      )}
      <span className="text-xs text-text-primary whitespace-nowrap">
        {c.name}
        {c.experimental && c.supported && (
          <span className="ml-1 text-[10px] text-text-secondary">(experimental)</span>
        )}
      </span>
      <span className="text-[11px] text-text-muted font-mono truncate flex-1" title={c.target_path ?? c.detail}>
        {c.supported ? c.target_path : c.detail}
        {c.supported && !c.available && c.state === 'never-synced' && ' (client not detected)'}
      </span>
      <span className="flex items-center gap-1.5">
        {c.state === 'drifted' && (
          <ClientAction label="Review" onClick={() => onReviewDrift(c.slug)} disabled={busy} />
        )}
        {(c.state === 'stale' || c.state === 'target-missing' || (c.state === 'never-synced' && c.available)) && (
          <ClientAction
            label={busy ? 'Syncing…' : 'Sync'}
            disabled={busy}
            onClick={() => void act(() => syncGlobalContext({ clients: [c.slug] }), `${c.name} synced`)}
          />
        )}
        {(c.state === 'in-sync' || c.state === 'stale' || c.state === 'drifted') && (
          <ClientAction
            label="Unsync"
            subtle
            disabled={busy}
            onClick={() => void act(() => unsyncGlobalContext(c.slug), `${c.name} unsynced`)}
          />
        )}
      </span>
      </div>
      {expanded && fragments.length > 0 && (
        <ul className="mt-1.5 ml-6 flex flex-col gap-1" aria-label={`${c.name} fragments`}>
          {fragments.map((f) => (
            <FragmentStatusLine
              key={`${c.slug}:${f.name}`}
              clientSlug={c.slug}
              fragment={f}
              busy={busy}
              onReviewDrift={onReviewDrift}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

/** One non-synced fragment line under an expanded multi-file client row. */
function FragmentStatusLine({
  clientSlug,
  fragment: f,
  busy,
  onReviewDrift,
}: {
  clientSlug: string;
  fragment: ContextFragmentStatus;
  busy: boolean;
  onReviewDrift: (slug: string, fragment?: string) => void;
}) {
  return (
    <li className="flex items-center gap-2">
      <StatePill state={f.state} />
      <span className="text-[11px] text-text-secondary font-mono truncate flex-1">{f.name}</span>
      {f.pack && (
        // Static inside the modal: navigating under an open dialog would
        // change the route invisibly, so provenance here is display only.
        <span title={`Applied by pack ${f.pack}`} className={PACK_CHIP_CLASS}>
          pack: {f.pack}
        </span>
      )}
      {f.state === 'drifted' && (
        <ClientAction
          label="Review"
          disabled={busy}
          onClick={() => onReviewDrift(clientSlug, f.name)}
        />
      )}
    </li>
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

/**
 * Fragments-mode drift review, three honest cases keyed off the client's
 * mode and render class:
 *  - identity multi-file (Claude Code): per-fragment diff with a lossless
 *    Adopt; multiple drifted fragments select via a mini-list in the same
 *    modal, never stacked dialogs.
 *  - lossy multi-file: same rows and diff, no Adopt affordance at all;
 *    the reason and the real alternatives are stated as visible prose.
 *  - compiled: whole-document diff plus capture-into-fragment, the UI form
 *    of the engine's designated-capture-fragment adopt.
 * Force overwrite (canonical wins) and Cancel are always available.
 */
function FragmentDriftResolveDialog({
  client,
  fragmentNames,
  initialFragment,
  onClose,
  onResolved,
}: {
  client: ContextClientStatus;
  fragmentNames: string[];
  initialFragment: string | null;
  onClose: () => void;
  onResolved: () => void;
}) {
  const multiFile = client.mode === 'multi-file';
  const identity = multiFile && IDENTITY_RENDER_SLUGS.has(client.slug);
  const drifted = (client.fragments ?? []).filter((f) => f.state === 'drifted');
  const [selected, setSelected] = useState<string | null>(
    multiFile ? (initialFragment ?? drifted[0]?.name ?? null) : null,
  );
  // Diff text keyed by its request, so switching fragments derives back to
  // the loading state instead of flashing the previous fragment's diff.
  const diffKey = `${client.slug}:${multiFile ? (selected ?? '') : ''}`;
  const [diffState, setDiffState] = useState<{ key: string; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let cancelled = false;
    if (multiFile && !selected) return;
    fetchGlobalContextDiff(client.slug, multiFile ? (selected ?? undefined) : undefined)
      .then((d) => {
        if (!cancelled) setDiffState({ key: diffKey, text: d });
      })
      .catch((err) => {
        if (!cancelled) {
          setDiffState({ key: diffKey, text: err instanceof Error ? err.message : 'Diff unavailable' });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [client.slug, multiFile, selected, diffKey]);

  const diff = diffState?.key === diffKey ? diffState.text : null;

  const run = useCallback(
    async (fn: () => Promise<unknown>, okMessage: string) => {
      setBusy(true);
      try {
        await fn();
        showToast('success', okMessage);
        onResolved();
      } catch (err) {
        showToast('error', err instanceof Error ? err.message : 'Action failed');
        setBusy(false);
      }
    },
    [onResolved],
  );

  const handleOverwrite = () =>
    void run(
      () => syncGlobalContext({ clients: [client.slug], force: true }),
      `${client.name} restored from the canonical store`,
    );

  const title = multiFile
    ? `${selected ?? 'fragments'} in ${client.name} was edited`
    : `${client.name} was edited`;

  return (
    <Modal isOpen onClose={onClose} title={title} size="wide">
      <div className="flex gap-4">
        {multiFile && drifted.length > 1 && (
          <ul className="w-44 flex-shrink-0 flex flex-col gap-1" aria-label="Drifted fragments">
            {drifted.map((f) => (
              <li key={f.name}>
                <button
                  onClick={() => setSelected(f.name)}
                  aria-current={selected === f.name ? 'true' : undefined}
                  className={cn(
                    'w-full flex items-center gap-2 px-2 py-1.5 rounded-lg border text-left transition-colors',
                    selected === f.name
                      ? 'border-primary/40 bg-primary/10'
                      : 'border-border/40 hover:bg-surface-highlight',
                  )}
                >
                  <StatePill state={f.state} />
                  <span className="text-[11px] font-mono text-text-primary truncate">{f.name}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
        <div className="flex flex-col gap-3 min-w-0 flex-1">
          {multiFile && identity && (
            <p className="text-xs text-text-muted">
              The projected copy of this fragment in {client.name}'s rules directory differs
              from the canonical store. Adopt the edit to make it the fragment's new content,
              or overwrite the client to restore the canon. A timestamped backup precedes
              every write.
            </p>
          )}
          {multiFile && !identity && (
            <p className="text-xs text-text-muted" data-testid="lossy-reason">
              {client.name}'s rule files are a lossy render of the source fragments
              (frontmatter its dialect cannot express is dropped at write time), so this edit
              cannot flow back automatically. Re-apply the change to the source fragment in
              the editor, or force overwrite to restore the projected copy from the canon.
            </p>
          )}
          {!multiFile && (
            <p className="text-xs text-text-muted">
              {client.name} receives one assembled document compiled from{' '}
              {fragmentNames.length > 0
                ? `${fragmentNames.length} fragment${fragmentNames.length === 1 ? '' : 's'}`
                : 'your rule fragments'}
              , so the edit cannot be split back automatically. Capture the edited document
              into a fragment to keep it, or overwrite the client to restore the assembled
              canon. A timestamped backup precedes every write.
            </p>
          )}
          <pre className="text-[11px] font-mono bg-background/60 border border-border/30 rounded-lg p-3 overflow-x-auto max-h-72 overflow-y-auto scrollbar-dark whitespace-pre">
            {diff ?? 'Loading diff…'}
          </pre>
          {multiFile ? (
            <div className="flex items-center justify-end gap-2">
              <DialogButton label="Cancel" onClick={onClose} disabled={busy} variant="muted" />
              {identity && selected && (
                <DialogButton
                  label={`Adopt ${selected}`}
                  disabled={busy}
                  variant="primary"
                  onClick={() =>
                    void run(
                      () => adoptGlobalContext(client.slug, { fragment: selected }),
                      `Adopted "${selected}" from ${client.name} into the canonical store`,
                    )
                  }
                />
              )}
              <DialogButton
                label="Force overwrite"
                disabled={busy}
                variant="danger"
                onClick={handleOverwrite}
              />
            </div>
          ) : (
            <CompiledCaptureActions
              fragmentNames={fragmentNames}
              busy={busy}
              onCapture={(into) =>
                void run(
                  () => adoptGlobalContext(client.slug, { into }),
                  `Captured ${client.name}'s edit into fragment "${into}"`,
                )
              }
              onOverwrite={handleOverwrite}
              onClose={onClose}
            />
          )}
          {multiFile && (
            <p className="text-[11px] text-text-muted">
              Force overwrite rewrites every projected fragment from the canonical store;
              fragments you did not touch rewrite identically.
            </p>
          )}
        </div>
      </div>
    </Modal>
  );
}

/**
 * Capture picker for compiled targets: choose an existing fragment or name
 * a new one, then adopt the edited document into it.
 */
function CompiledCaptureActions({
  fragmentNames,
  busy,
  onCapture,
  onOverwrite,
  onClose,
}: {
  fragmentNames: string[];
  busy: boolean;
  onCapture: (into: string) => void;
  onOverwrite: () => void;
  onClose: () => void;
}) {
  const NEW_FRAGMENT = '__new__';
  const [choice, setChoice] = useState(fragmentNames[0] ?? NEW_FRAGMENT);
  const [newName, setNewName] = useState('');
  // The fragment list can arrive after mount (deep link opens the dialog
  // before the rail load resolves); re-default the untouched picker.
  const [hadNames, setHadNames] = useState(fragmentNames.length > 0);
  if ((fragmentNames.length > 0) !== hadNames) {
    setHadNames(fragmentNames.length > 0);
    if (choice === NEW_FRAGMENT && newName === '' && fragmentNames.length > 0) {
      setChoice(fragmentNames[0]);
    }
  }
  const isNew = choice === NEW_FRAGMENT;
  const target = isNew ? newName : choice;
  const valid = FRAGMENT_NAME_RE.test(target);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center gap-2 flex-wrap">
        <label htmlFor="capture-fragment" className="text-[11px] text-text-muted">
          Capture into
        </label>
        <select
          id="capture-fragment"
          value={choice}
          onChange={(e) => setChoice(e.target.value)}
          className="bg-background/60 border border-border/40 rounded-lg px-2 py-1.5 text-[11px] font-mono text-text-primary focus:outline-none focus:border-primary/50"
        >
          {fragmentNames.map((n) => (
            <option key={n} value={n}>
              {n}
            </option>
          ))}
          <option value={NEW_FRAGMENT}>New fragment…</option>
        </select>
        {isNew && (
          <input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="captured-from-client"
            aria-label="New capture fragment name"
            className="flex-1 min-w-32 bg-background/60 border border-border/40 rounded-lg px-2 py-1.5 text-[11px] font-mono text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50"
          />
        )}
      </div>
      {isNew && newName && !valid && (
        <p className="text-[11px] text-status-error">
          Lowercase letters, digits, and hyphens only.
        </p>
      )}
      <div className="flex items-center justify-end gap-2">
        <DialogButton label="Cancel" onClick={onClose} disabled={busy} variant="muted" />
        <DialogButton
          label={valid ? `Capture into ${target}` : 'Capture'}
          disabled={busy || !valid}
          variant="primary"
          onClick={() => onCapture(target)}
        />
        <DialogButton
          label="Force overwrite"
          disabled={busy}
          variant="danger"
          onClick={onOverwrite}
        />
      </div>
    </div>
  );
}

function DialogButton({
  label,
  onClick,
  disabled,
  variant,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  variant: 'muted' | 'primary' | 'danger';
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'px-3 py-1.5 text-xs rounded-lg border transition-colors disabled:opacity-50',
        variant === 'muted' && 'text-text-muted border-border/40 hover:bg-surface-highlight',
        variant === 'primary' &&
          'font-medium text-primary bg-primary/10 border-primary/25 hover:bg-primary/15',
        variant === 'danger' && 'font-medium text-red-400 border-red-400/25 hover:bg-red-400/10',
      )}
    >
      {label}
    </button>
  );
}

// Mirrors the backend's fragment-name rule so a bad name fails in the
// input, not as a server error.
const FRAGMENT_NAME_RE = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;

/**
 * The one deliberate entry into fragments mode from the single-file
 * editor: names the first fragment, states the migration plainly, and
 * only then writes. Mirrors `ctx add` (backup, AGENTS.md becomes
 * fragments/00-default.md, explicit message).
 */
function SplitIntoFragmentsDialog({ dirty, onClose }: { dirty: boolean; onClose: () => void }) {
  const refresh = useContextStore((s) => s.refresh);
  const [name, setName] = useState('');
  const [busy, setBusy] = useState(false);
  const valid = FRAGMENT_NAME_RE.test(name);

  const handleCreate = useCallback(async () => {
    if (!FRAGMENT_NAME_RE.test(name)) return;
    setBusy(true);
    try {
      const res = await saveContextFragment(name, '');
      showToast(
        'success',
        res.migrated
          ? 'Fragments mode activated: AGENTS.md migrated to fragments/00-default.md (backup saved)'
          : `Fragment "${name}" created`,
      );
      await refresh();
      onClose();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Failed to create fragment');
      setBusy(false);
    }
  }, [name, refresh, onClose]);

  return (
    <Modal isOpen onClose={onClose} title="Split into fragments" size="wide">
      <div className="flex flex-col gap-3">
        <p className="text-xs text-text-muted">
          Fragments turn the canonical file into a directory of rule files
          (~/.gridctl/context/fragments/) composed in filename order — numeric prefixes (00-,
          10-) control ordering. Your current AGENTS.md becomes fragments/00-default.md (a
          timestamped backup precedes the move), and this creates one new fragment alongside it.
          {dirty && ' Your unsaved editor changes will be discarded.'}
        </p>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && void handleCreate()}
          placeholder="10-style"
          aria-label="New fragment name"
          className="bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs font-mono text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50"
        />
        {name && !valid && (
          <p className="text-[11px] text-status-error">
            Lowercase letters, digits, and hyphens only.
          </p>
        )}
        <div className="flex items-center justify-end gap-2">
          <button
            onClick={onClose}
            disabled={busy}
            className="px-3 py-1.5 text-xs text-text-muted border border-border/40 rounded-lg hover:bg-surface-highlight transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={() => void handleCreate()}
            disabled={busy || !valid}
            className="px-3 py-1.5 text-xs font-medium text-primary bg-primary/10 border border-primary/25 rounded-lg hover:bg-primary/15 transition-colors disabled:opacity-50"
          >
            {busy ? 'Migrating…' : 'Activate fragments'}
          </button>
        </div>
      </div>
    </Modal>
  );
}

/**
 * Fragments-mode surface: a left rail of rule fragments in composition
 * order (adapting SkillFileTree's grammar) feeding the same editor and
 * preview split the single-file view uses, over the shared clients strip.
 */
function FragmentsView({
  doc,
  refreshError,
  initialDriftSlug = null,
}: {
  doc: ContextDoc;
  refreshError: string | null;
  initialDriftSlug?: string | null;
}) {
  const refresh = useContextStore((s) => s.refresh);
  const [fragments, setFragments] = useState<ContextFragment[] | null>(null);
  const [selected, setSelected] = useState<string | null>(null);
  const [draft, setDraft] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [driftTarget, setDriftTarget] = useState<{ slug: string; fragment: string | null } | null>(
    null,
  );
  const [spotlightSlug, setSpotlightSlug] = useState<string | null>(null);
  const [showPreview, setShowPreview] = useState(true);

  // Deep link from the Connections hub: spotlight the client's row, and
  // open the review directly only when the target is unambiguous (compiled
  // client, or exactly one drifted fragment). Otherwise the expanded
  // fragment lines are the picker.
  useDriftSeed(initialDriftSlug, (slug) => {
    const c = doc.clients.find((cl) => cl.slug === slug);
    if (!c) return;
    if (c.mode === 'multi-file') {
      setSpotlightSlug(slug);
      const drifted = (c.fragments ?? []).filter((f) => f.state === 'drifted');
      if (drifted.length === 1) setDriftTarget({ slug, fragment: drifted[0].name });
    } else if (c.state === 'drifted') {
      setDriftTarget({ slug, fragment: null });
    }
  });

  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const previewRef = useRef<HTMLDivElement>(null);
  const { ratio, containerRef, handleMouseDown, isDragging } = useSplitPane(0.5);

  const loadFragments = useCallback(async (selectName?: string) => {
    try {
      const res = await fetchContextFragments();
      setFragments(res.fragments);
      setSelected((prev) => {
        const want = selectName ?? prev;
        if (want && res.fragments.some((f) => f.name === want)) return want;
        return res.fragments[0]?.name ?? null;
      });
    } catch (err) {
      setFragments([]);
      showToast('error', err instanceof Error ? err.message : 'Failed to load fragments');
    }
  }, []);

  // Initial load uses a raw promise chain (the useContextScan pattern) so
  // no setState is reachable from the effect body itself.
  useEffect(() => {
    let cancelled = false;
    fetchContextFragments()
      .then((res) => {
        if (cancelled) return;
        setFragments(res.fragments);
        setSelected((prev) =>
          prev && res.fragments.some((f) => f.name === prev)
            ? prev
            : (res.fragments[0]?.name ?? null),
        );
      })
      .catch((err) => {
        if (cancelled) return;
        setFragments([]);
        showToast('error', err instanceof Error ? err.message : 'Failed to load fragments');
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const current = fragments?.find((f) => f.name === selected) ?? null;
  const content = draft ?? current?.content ?? '';
  const dirty = current !== null && draft !== null && draft !== current.content;
  const markerIssue = RESERVED_MARKERS.find((m) => content.includes(m)) ?? null;
  const lineCount = content.split('\n').length;
  const charCount = content.length;
  const fragmentPath = current
    ? doc.canonical.path.replace(/AGENTS\.md$/, `fragments/${current.name}.md`)
    : doc.canonical.path.replace(/AGENTS\.md$/, 'fragments/');

  const handleSelect = useCallback(
    (name: string) => {
      if (name === selected) return;
      if (dirty) {
        showToast('warning', 'Save or discard your changes before switching fragments');
        return;
      }
      setSelected(name);
      setDraft(null);
    },
    [selected, dirty],
  );

  const handleSave = useCallback(async () => {
    if (!dirty || draft === null || markerIssue || !current) return;
    const toSave = draft;
    setSaving(true);
    try {
      await saveContextFragment(current.name, toSave);
      setDraft((d) => (d === toSave ? null : d));
      showToast('success', 'Fragment saved. Run a sync to propagate.');
      await loadFragments(current.name);
      await refresh();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Save failed');
    } finally {
      setSaving(false);
    }
  }, [dirty, draft, markerIssue, current, loadFragments, refresh]);

  const handleSyncAll = useCallback(async () => {
    setSyncing(true);
    try {
      const resp = await syncGlobalContext();
      // Name the fragments a drift skip protected, not just a count. The
      // results themselves carry the fragment identity, so the names are
      // exact even when the doc in the store is stale.
      const driftedNames = new Set<string>();
      for (const r of resp.results) {
        if (r.action === 'skipped-drift' && r.fragment) driftedNames.add(r.fragment);
      }
      showToast(
        resp.has_failures ? 'warning' : 'success',
        summarizeSync(resp.results, [...driftedNames]),
      );
      await refresh();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Sync failed');
    } finally {
      setSyncing(false);
    }
  }, [refresh]);

  const handleAdd = useCallback(
    async (name: string) => {
      try {
        await saveContextFragment(name, '');
        showToast('success', `Fragment "${name}" created`);
        await loadFragments(name);
        await refresh();
      } catch (err) {
        showToast('error', err instanceof Error ? err.message : 'Failed to create fragment');
      }
    },
    [loadFragments, refresh],
  );

  const handleDelete = useCallback(
    async (name: string) => {
      try {
        await deleteContextFragment(name);
        showToast('success', `Fragment "${name}" removed (backup saved). Run a sync to drop its projections.`);
        if (name === selected) setDraft(null);
        await loadFragments();
        await refresh();
      } catch (err) {
        showToast('error', err instanceof Error ? err.message : 'Failed to remove fragment');
      }
    },
    [selected, loadFragments, refresh],
  );

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 's') {
        e.preventDefault();
        void handleSave();
      }
    },
    [handleSave],
  );

  const applyMarkdown = useCallback(
    (action: MarkdownAction) => {
      const ta = bodyRef.current;
      if (!ta) return;
      const next = applyMarkdownAction(content, ta.selectionStart, ta.selectionEnd, action);
      setDraft(next.value);
      requestAnimationFrame(() => {
        ta.focus();
        ta.setSelectionRange(next.selStart, next.selEnd);
      });
    },
    [content],
  );

  const handleEditorScroll = useCallback((e: React.UIEvent<HTMLTextAreaElement>) => {
    const ta = e.currentTarget;
    const preview = previewRef.current;
    if (!preview) return;
    const srcMax = ta.scrollHeight - ta.clientHeight;
    if (srcMax <= 0) return;
    const dstMax = preview.scrollHeight - preview.clientHeight;
    preview.scrollTop = (ta.scrollTop / srcMax) * dstMax;
  }, []);

  return (
    <div className="flex flex-col h-[calc(100%+2rem)] -mx-6 -my-4">
      {/* Action header */}
      <div className="flex items-center justify-between gap-3 px-5 py-3 border-b border-border/30 flex-shrink-0">
        <span className="text-[11px] text-text-muted font-mono truncate min-w-0" title={fragmentPath}>
          {fragmentPath}
        </span>
        <div className="flex items-center gap-2 flex-shrink-0">
          <button
            onClick={() => setShowPreview((p) => !p)}
            title={showPreview ? 'Hide preview' : 'Show preview'}
            className={cn(
              'p-1.5 rounded-lg transition-all duration-200',
              showPreview
                ? 'text-text-muted hover:text-primary hover:bg-primary/10'
                : 'text-primary bg-primary/10',
            )}
          >
            {showPreview ? <Eye size={14} /> : <EyeOff size={14} />}
          </button>
          <IconButton
            icon={RefreshCw}
            onClick={() => {
              void loadFragments();
              void refresh();
            }}
            tooltip="Refresh status"
            size="sm"
            variant="ghost"
          />
          <button
            onClick={() => void handleSyncAll()}
            disabled={syncing || dirty}
            title={dirty ? 'Save before syncing' : 'Sync every available client'}
            className="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-emerald-400 border border-emerald-400/25 hover:bg-emerald-400/10 rounded-lg transition-colors disabled:opacity-50"
          >
            <CloudUpload size={12} aria-hidden="true" className={syncing ? 'animate-pulse' : undefined} />
            {syncing ? 'Syncing…' : 'Sync all'}
          </button>
          <button
            onClick={() => void handleSave()}
            disabled={!dirty || saving || !!markerIssue}
            className={cn(
              'px-4 py-2 text-xs font-medium rounded-lg transition-all',
              'bg-primary text-background hover:bg-primary/90',
              (!dirty || saving || !!markerIssue) && 'opacity-50 cursor-not-allowed',
            )}
          >
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>

      {refreshError && (
        <div
          role="alert"
          className="px-5 py-2 bg-status-error/10 border-b border-status-error/30 flex-shrink-0 text-xs text-status-error"
        >
          Refresh failed: {refreshError}
        </div>
      )}

      <ClientsStrip
        clients={doc.clients}
        onReviewDrift={(slug, fragment) => setDriftTarget({ slug, fragment: fragment ?? null })}
        spotlightSlug={spotlightSlug}
      />

      <div className="flex-1 flex min-h-0">
        <FragmentRail
          fragments={fragments}
          selected={selected}
          onSelect={handleSelect}
          onAdd={handleAdd}
          onDelete={handleDelete}
          outOfSync={fragmentOutOfSync(doc.clients)}
        />

        {/* Editor area, identical grammar to the single-file view */}
        <div ref={containerRef} className="flex-1 flex min-w-0 min-h-0 group/split">
          {current === null ? (
            <div className="flex-1 flex items-center justify-center text-sm text-text-muted">
              {fragments === null ? 'Loading fragments…' : 'Add a fragment to start.'}
            </div>
          ) : (
            <>
              <div
                className={cn('flex flex-col min-w-0 min-h-0', showPreview && 'border-r border-border/30')}
                style={showPreview ? { width: `${ratio * 100}%` } : { width: '100%' }}
              >
                <div className="flex items-center justify-between gap-2 px-4 py-1.5 border-b border-border/20 flex-shrink-0">
                  <span className="text-xs text-text-muted uppercase tracking-wider">Markdown</span>
                  <div className="flex items-center gap-0.5">
                    <EditorToolbarButton icon={Bold} label="Bold" onClick={() => applyMarkdown('bold')} />
                    <EditorToolbarButton icon={Heading} label="Heading" onClick={() => applyMarkdown('heading')} />
                    <EditorToolbarButton icon={List} label="List item" onClick={() => applyMarkdown('list')} />
                    <EditorToolbarButton icon={Code2} label="Code block" onClick={() => applyMarkdown('code')} />
                  </div>
                </div>
                <textarea
                  ref={bodyRef}
                  value={content}
                  onChange={(e) => setDraft(e.target.value)}
                  onKeyDown={handleKeyDown}
                  onScroll={handleEditorScroll}
                  aria-label={`Fragment ${current.name}`}
                  placeholder={'---\ndescription: what this rule covers\npaths:\n  - "**/*.go"\n---\n\n# Rules\n'}
                  className="flex-1 w-full bg-background/40 px-5 py-4 text-sm font-mono text-text-primary placeholder:text-text-muted/30 resize-none focus:outline-none leading-relaxed"
                  spellCheck={false}
                />
              </div>

              {showPreview && (
                <>
                  <SplitPaneHandle onMouseDown={handleMouseDown} isDragging={isDragging} />
                  <div className="flex flex-col min-w-0 min-h-0" style={{ width: `${(1 - ratio) * 100}%` }}>
                    <div className="px-4 py-2 border-b border-border/20 flex-shrink-0">
                      <span className="text-xs text-text-muted uppercase tracking-wider">Preview</span>
                    </div>
                    <div ref={previewRef} className="flex-1 overflow-y-auto scrollbar-dark">
                      <div className="px-5 py-4">
                        <MarkdownPreview content={content} emptyHint="Fragment preview" />
                      </div>
                    </div>
                  </div>
                </>
              )}
            </>
          )}
        </div>
      </div>

      {/* Bottom status bar */}
      <div className="flex items-center justify-between px-5 py-2 border-t border-border/30 bg-surface/50 flex-shrink-0">
        <div className="flex items-center gap-3">
          {markerIssue ? (
            <span className="text-xs flex items-center gap-1 text-status-error">
              <AlertCircle size={12} />
              contains the reserved gridctl marker {markerIssue}
            </span>
          ) : (
            <span className="text-xs flex items-center gap-1 text-status-running">
              <Check size={12} />
              Valid
            </span>
          )}
          {dirty && !markerIssue && <span className="text-xs text-text-muted">Unsaved changes</span>}
          <span className="text-xs text-text-muted">
            Composed in filename order; numeric prefixes (00-, 10-) control ordering.
          </span>
        </div>
        <div className="flex items-center gap-4 text-xs text-text-muted font-mono">
          <span>{lineCount} lines</span>
          <span>{charCount} chars</span>
        </div>
      </div>

      {driftTarget &&
        (() => {
          const client = doc.clients.find((c) => c.slug === driftTarget.slug);
          if (!client) return null;
          return (
            <FragmentDriftResolveDialog
              client={client}
              fragmentNames={(fragments ?? []).map((f) => f.name)}
              initialFragment={driftTarget.fragment}
              onClose={() => setDriftTarget(null)}
              onResolved={() => {
                setDriftTarget(null);
                void loadFragments();
                void refresh();
              }}
            />
          );
        })()}
    </div>
  );
}

/**
 * Which clients hold a non-synced copy of each fragment, for the rail's
 * display-only drift dots.
 */
function fragmentOutOfSync(clients: ContextClientStatus[]): Map<string, string[]> {
  const byFragment = new Map<string, string[]>();
  for (const c of clients) {
    for (const f of c.fragments ?? []) {
      const names = byFragment.get(f.name) ?? [];
      names.push(c.name);
      byFragment.set(f.name, names);
    }
  }
  return byFragment;
}

/**
 * The fragment list rail, adapting SkillFileTree's grammar to a flat,
 * composition-ordered set: position, name, size, paths badge, add and
 * delete affordances.
 */
function FragmentRail({
  fragments,
  selected,
  onSelect,
  onAdd,
  onDelete,
  outOfSync,
}: {
  fragments: ContextFragment[] | null;
  selected: string | null;
  onSelect: (name: string) => void;
  onAdd: (name: string) => void;
  onDelete: (name: string) => void;
  /** Fragment name to client names holding a non-synced copy (drift dots). */
  outOfSync?: Map<string, string[]>;
}) {
  const [showNew, setShowNew] = useState(false);
  const [newName, setNewName] = useState('');
  const valid = FRAGMENT_NAME_RE.test(newName);

  const submitNew = () => {
    if (!valid) return;
    onAdd(newName);
    setNewName('');
    setShowNew(false);
  };

  return (
    <div className="w-60 flex-shrink-0 border-r border-border/30 flex flex-col min-h-0">
      <div className="flex items-center justify-between gap-2 px-3 py-2 border-b border-border/20 flex-shrink-0">
        <span className="text-[10px] text-text-muted uppercase tracking-wider">Fragments</span>
        <button
          onClick={() => setShowNew((s) => !s)}
          className="text-[10px] text-primary hover:text-primary/80 flex items-center gap-0.5 transition-colors"
        >
          <Plus size={10} /> Add
        </button>
      </div>
      {showNew && (
        <div className="px-3 py-2 flex items-center gap-1.5 border-b border-border/20 flex-shrink-0">
          <input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && submitNew()}
            placeholder="10-style"
            aria-label="New fragment name"
            className="flex-1 min-w-0 bg-background/60 border border-border/40 rounded px-2 py-1 text-[10px] font-mono text-text-primary placeholder:text-text-muted/50 focus:outline-none focus:border-primary/50"
          />
          <button
            onClick={submitNew}
            disabled={!valid}
            className="px-2 py-1 text-[10px] text-primary bg-primary/10 rounded hover:bg-primary/20 transition-colors disabled:opacity-50"
          >
            Create
          </button>
        </div>
      )}
      <div className="flex-1 overflow-y-auto scrollbar-dark py-1">
        {fragments === null ? (
          <p className="text-[10px] text-text-muted px-3 py-2">Loading fragments…</p>
        ) : fragments.length === 0 ? (
          <p className="text-[10px] text-text-muted px-3 py-2 italic">No fragments yet</p>
        ) : (
          fragments.map((f) => (
            <div
              key={f.name}
              className={cn(
                'flex items-center gap-1.5 px-3 py-1.5 group cursor-pointer transition-colors',
                selected === f.name ? 'bg-primary/10' : 'hover:bg-surface-highlight/50',
              )}
            >
              <File
                size={10}
                className={cn('flex-shrink-0', selected === f.name ? 'text-primary/70' : 'text-text-muted')}
              />
              <button
                onClick={() => onSelect(f.name)}
                title={f.description || f.name}
                className={cn(
                  'text-[11px] font-mono flex-1 text-left truncate transition-colors',
                  selected === f.name ? 'text-primary' : 'text-text-secondary hover:text-primary',
                )}
              >
                {f.name}
              </button>
              {(outOfSync?.get(f.name) ?? []).length > 0 && (
                <span
                  role="img"
                  aria-label={`Out of sync in ${(outOfSync?.get(f.name) ?? []).join(', ')}`}
                  title={`Out of sync in ${(outOfSync?.get(f.name) ?? []).join(', ')}`}
                  className="w-1.5 h-1.5 rounded-full bg-status-pending flex-shrink-0"
                />
              )}
              {(f.paths ?? []).length > 0 && (
                <span
                  title={`paths: ${(f.paths ?? []).join(', ')}`}
                  className="text-[9px] px-1 py-0.5 rounded border border-border/40 text-text-muted font-mono flex-shrink-0"
                >
                  globs
                </span>
              )}
              <span className="text-[10px] text-text-muted font-mono flex-shrink-0">{f.bytes}B</span>
              <button
                type="button"
                onClick={() => onDelete(f.name)}
                title={`Remove fragment ${f.name}`}
                className="opacity-0 group-hover:opacity-100 p-1 rounded text-text-muted hover:text-status-error hover:bg-status-error/10 focus:outline-none focus:ring-2 focus:ring-status-error/30 focus:opacity-100 transition-all"
              >
                <Trash2 size={10} />
              </button>
            </div>
          ))
        )}
      </div>
    </div>
  );
}

/**
 * One-line summary of a sync pass for the toast. In fragments mode the
 * caller can name the fragments a drift skip protected.
 */
function summarizeSync(results: ContextSyncResult[], driftedFragments?: string[]): string {
  const counts: Record<string, number> = {};
  for (const r of results) counts[r.action] = (counts[r.action] ?? 0) + 1;
  const parts: string[] = [];
  const written = (counts['created'] ?? 0) + (counts['updated'] ?? 0);
  if (written) parts.push(`${written} synced`);
  if (counts['unchanged']) parts.push(`${counts['unchanged']} unchanged`);
  if (counts['removed']) parts.push(`${counts['removed']} removed`);
  if (counts['skipped-drift']) {
    parts.push(
      driftedFragments?.length
        ? `${counts['skipped-drift']} skipped (drifted: ${driftedFragments.join(', ')})`
        : `${counts['skipped-drift']} skipped (drifted)`,
    );
  }
  if (counts['skipped-unavailable']) parts.push(`${counts['skipped-unavailable']} unavailable`);
  if (counts['error']) parts.push(`${counts['error']} failed`);
  return parts.length ? `Sync: ${parts.join(', ')}` : 'Nothing to sync';
}
