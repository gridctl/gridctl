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
  FileDown,
  FileText,
  Heading,
  List,
  MonitorSmartphone,
  RefreshCw,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { Modal } from '../ui/Modal';
import { IconButton } from '../ui/IconButton';
import { showToast } from '../ui/Toast';
import { useSplitPane } from '../../hooks/useSplitPane';
import { SplitPaneHandle } from '../ui/SplitPane';
import { MarkdownPreview } from '../registry/MarkdownPreview';
import { applyMarkdownAction, type MarkdownAction } from '../../lib/markdownEdit';
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
  onClose: () => void;
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
      {doc && !doc.canonical.exists && <SetupView />}
      {doc && doc.canonical.exists && <EditorView doc={doc} refreshError={error} />}
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
            conventions, tone, tools) and syncs to every linked client. Per-project AGENTS.md
            files stay in their repos and are never touched.
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
function EditorView({ doc, refreshError }: { doc: ContextDoc; refreshError: string | null }) {
  const setDoc = useContextStore((s) => s.setDoc);
  const refresh = useContextStore((s) => s.refresh);
  // null draft = pristine (textarea mirrors the canonical content), so a
  // background refresh never clobbers in-progress typing.
  const [draft, setDraft] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [driftSlug, setDriftSlug] = useState<string | null>(null);
  const [showImport, setShowImport] = useState(false);
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

const STATE_STYLE: Record<ContextState, string> = {
  'in-sync': 'text-emerald-400 border-emerald-400/25 bg-emerald-400/10',
  stale: 'text-amber-300 border-amber-400/30 bg-amber-400/10',
  drifted: 'text-red-400 border-red-400/25 bg-red-400/10',
  'target-missing': 'text-red-400 border-red-400/25 bg-red-400/10',
  'never-synced': 'text-text-muted border-border/40 bg-background/40',
  unsupported: 'text-text-muted/60 border-border/30 bg-background/30',
};

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
}: {
  clients: ContextClientStatus[];
  onReviewDrift: (slug: string) => void;
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
          <span className="flex-shrink-0 text-[9px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded-full border border-amber-400/30 bg-amber-400/10 text-amber-300">
            Needs attention
          </span>
        )}
      </button>
      {expanded && (
        <ul className="divide-y divide-border/20 max-h-56 overflow-y-auto scrollbar-dark border-t border-border/20">
          {clients.map((c) => (
            <ClientRow key={c.slug} client={c} onReviewDrift={onReviewDrift} />
          ))}
        </ul>
      )}
    </div>
  );
}

/** One client's row with inline actions. */
function ClientRow({
  client: c,
  onReviewDrift,
}: {
  client: ContextClientStatus;
  onReviewDrift: (slug: string) => void;
}) {
  const refresh = useContextStore((s) => s.refresh);
  const [busy, setBusy] = useState(false);

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
    <li className="flex items-center gap-2.5 px-5 py-2">
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
