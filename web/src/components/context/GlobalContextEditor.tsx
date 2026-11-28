import { useCallback, useDeferredValue, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import {
  AlertCircle,
  Bold,
  Check,
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
  Plus,
  Trash2,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { Modal } from '../ui/Modal';
import { FailureBanner } from '../ui/EmptyState';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { StatePill } from '../ui/StatePill';
import { showToast } from '../ui/Toast';
import { ContextFrame } from './ContextFrame';
import { ContextInspector, type ContextInspectorTab } from './ContextInspector';
import { ContextPinChip, ContextPinReviewModal } from './ContextPinChip';
import { contextPinsUrl, contextPinStatus } from '../../lib/contextGovernance';
import { useGuardedNavigate } from '../../hooks/useGuardedNavigate';
import { useSplitPane } from '../../hooks/useSplitPane';
import { SplitPaneHandle } from '../ui/SplitPane';
import { MarkdownPreview } from '../registry/MarkdownPreview';
import { applyMarkdownAction, type MarkdownAction } from '../../lib/markdownEdit';
import { useContextStore } from '../../stores/useContextStore';
import { describeError, isCancellation } from '../../lib/describeError';
import { escapeNonPrintable } from '../../lib/nonPrintable';
import { contextDriftCliHints } from '../../lib/contextDriftHints';
import { useLoadedEvidence, type LoadedEvidence } from '../../hooks/useLoadedEvidence';
import { UnifiedDiffView } from '../ui/UnifiedDiffView';
import type { RegistryKind } from '../../lib/registryKind';
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
  type ContextClientStatus,
  type ContextDoc,
  type ContextFragment,
  type ContextScanEntry,
  type ContextSyncResult,
} from '../../lib/api';

export interface ContextViewActions {
  refresh: () => void;
  syncAll: () => void;
}

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
          if (!isCancellation(err)) {
            const described = describeError(err, 'Scan');
            showToast('error', described.text, { detail: described.fullDetail });
          }
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
          <span className={cn('text-2xs text-text-muted truncate min-w-0 flex-1', opt.mono && 'font-mono')}>
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
export function SetupView() {
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
      if (!isCancellation(err)) {
        const described = describeError(err, 'Setup');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    } finally {
      setBusy(false);
    }
  }, [selected, setDoc]);

  if (entries === null) {
    return (
      <div className="flex-1 flex items-center justify-center text-sm text-text-muted">
        Scanning client context files…
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto scrollbar-dark p-6">
      <div className="flex flex-col gap-4 max-w-2xl">
      <div className="flex items-start gap-3">
        <div className="p-3 rounded-xl bg-surface-elevated/50 border border-border/30">
          <FileText size={22} className="text-text-muted" />
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
      if (!isCancellation(err)) {
        const described = describeError(err, 'Import');
        showToast('error', described.text, { detail: described.fullDetail });
      }
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
 * SkillEditor-grade editing surface: action header, resizable markdown and
 * preview split with a formatting toolbar, status bar with live marker
 * validation and line/char counts, and the right-rail ContextInspector.
 */
export function EditorView({
  doc,
  refreshError,
  initialDriftSlug = null,
  tab,
  onTabChange,
  onKindChange,
  onDirtyChange,
  onRegisterActions,
}: {
  doc: ContextDoc;
  refreshError: string | null;
  initialDriftSlug?: string | null;
  tab: ContextInspectorTab;
  onTabChange: (tab: ContextInspectorTab) => void;
  onKindChange: (kind: RegistryKind) => void;
  onDirtyChange?: (dirty: boolean) => void;
  onRegisterActions?: (actions: ContextViewActions | null) => void;
}) {
  const navigate = useGuardedNavigate();
  const setDoc = useContextStore((s) => s.setDoc);
  const refresh = useContextStore((s) => s.refresh);
  // null draft = pristine (textarea mirrors the canonical content), so a
  // background refresh never clobbers in-progress typing.
  const [draft, setDraft] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [driftSlug, setDriftSlug] = useState<string | null>(null);
  const [spotlightSlug, setSpotlightSlug] = useState<string | null>(null);
  // Seed the deep link once per non-null slug: spotlight the client row, and
  // open the drift review only when that client is actually drifted.
  useDriftSeed(initialDriftSlug, (slug) => {
    const c = doc.clients.find((cl) => cl.slug === slug);
    if (!c) return;
    setSpotlightSlug(slug);
    if (c.state === 'drifted') {
      setDriftSlug(slug);
    }
  });
  const [showImport, setShowImport] = useState(false);
  const [showSplit, setShowSplit] = useState(false);
  const [reviewPin, setReviewPin] = useState(false);
  const [showPreview, setShowPreview] = useState(true);

  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const previewRef = useRef<HTMLDivElement>(null);
  const { ratio, containerRef, handlePointerDown, isDragging } = useSplitPane(0.5);

  const content = draft ?? doc.canonical.content;
  // Preview trails the textarea so a long canonical file does not block keystrokes.
  const deferredContent = useDeferredValue(content);
  const dirty = draft !== null && draft !== doc.canonical.content;
  const markerIssue = RESERVED_MARKERS.find((m) => content.includes(m)) ?? null;

  useEffect(() => {
    onDirtyChange?.(dirty);
    return () => onDirtyChange?.(false);
  }, [dirty, onDirtyChange]);
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
      if (next.pin_status === 'drift') {
        showToast(
          'success',
          'Canonical context saved. Run a sync to propagate. Pin drift: review and approve in Pins.',
          {
            action: {
              label: 'Review in Pins',
              onClick: () => navigate(contextPinsUrl('AGENTS.md', 'drift')),
            },
          },
        );
      } else {
        showToast('success', 'Canonical context saved. Run a sync to propagate.');
      }
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'Save');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    } finally {
      setSaving(false);
    }
  }, [dirty, draft, markerIssue, setDoc, navigate]);

  const handleSyncAll = useCallback(async () => {
    setSyncing(true);
    try {
      const resp = await syncGlobalContext();
      showToast(resp.has_failures ? 'warning' : 'success', summarizeSync(resp.results));
      await refresh();
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'Sync');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    } finally {
      setSyncing(false);
    }
  }, [refresh]);

  useEffect(() => {
    onRegisterActions?.({
      refresh: () => void refresh(),
      syncAll: () => {
        if (dirty) {
          showToast('warning', 'Save or revert your changes before syncing');
          return;
        }
        void handleSyncAll();
      },
    });
    return () => onRegisterActions?.(null);
  }, [onRegisterActions, refresh, dirty, handleSyncAll]);

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

  const inspector = (
    <ContextInspector
      clients={doc.clients}
      tab={tab}
      onTabChange={onTabChange}
      onReviewDrift={(slug) => setDriftSlug(slug)}
      spotlightSlug={spotlightSlug}
      details={{
        mode: 'single-file',
        canonicalPath: doc.canonical.path,
        lineCount,
        charCount,
        pinStatus: contextPinStatus('AGENTS.md', doc.pins),
      }}
    />
  );

  return (
    <ContextFrame
      onKindChange={onKindChange}
      onRefresh={() => void refresh()}
      inspector={inspector}
      inspectorKey={spotlightSlug}
    >
      <div className="flex flex-col flex-1 min-h-0">
        {/* Action header */}
        <div className="flex items-center justify-between gap-3 px-5 py-3 border-b border-border/30 flex-shrink-0">
          <span
            className="text-2xs text-text-muted font-mono truncate min-w-0"
            title={doc.canonical.path}
          >
            {doc.canonical.path}
          </span>
          <div className="flex items-center gap-2 flex-shrink-0">
            <button
              type="button"
              onClick={() => setShowPreview((p) => !p)}
              title={showPreview ? 'Hide preview' : 'Show preview'}
              aria-label={showPreview ? 'Hide preview' : 'Show preview'}
              className={cn(
                'p-1.5 rounded-lg transition-all duration-200',
                showPreview
                  ? 'text-text-muted hover:text-primary hover:bg-primary/10'
                  : 'text-primary bg-primary/10',
              )}
            >
              {showPreview ? <Eye size={14} /> : <EyeOff size={14} />}
            </button>
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
              className="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-status-running border border-status-running/25 hover:bg-status-running/10 rounded-lg transition-colors disabled:opacity-50"
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
          <FailureBanner
            message={refreshError}
            onRetry={() => void refresh()}
            className="mx-5 mt-2 mb-2 flex-shrink-0"
          />
        )}

        {/* Editor area: split pane when preview on, full-width when off */}
        <div ref={containerRef} className="flex-1 flex min-h-0 group/split">
          <div
            className={cn('flex flex-col min-w-0 min-h-0', showPreview && 'border-r border-border/30')}
            style={showPreview ? { width: `${ratio * 100}%` } : { width: '100%' }}
          >
            <div className="flex items-center justify-between gap-2 px-4 py-1.5 border-b border-border/20 flex-shrink-0">
              <span className="text-xs text-text-muted uppercase tracking-wider flex items-center gap-2">
                Markdown
                <ContextPinChip
                  status={contextPinStatus('AGENTS.md', doc.pins)}
                  onReview={() => setReviewPin(true)}
                />
              </span>
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
              className="flex-1 w-full bg-background/40 px-5 py-4 text-sm font-mono text-text-primary placeholder:text-text-muted resize-none leading-relaxed"
              spellCheck={false}
            />
          </div>

          {showPreview && (
            <>
              <SplitPaneHandle onPointerDown={handlePointerDown} isDragging={isDragging} />
              <div className="flex flex-col min-w-0 min-h-0" style={{ width: `${(1 - ratio) * 100}%` }}>
                <div className="px-4 py-2 border-b border-border/20 flex-shrink-0">
                  <span className="text-xs text-text-muted uppercase tracking-wider">Preview</span>
                </div>
                <div ref={previewRef} className="flex-1 overflow-y-auto scrollbar-dark">
                  <div className="px-5 py-4">
                    <MarkdownPreview content={deferredContent} emptyHint="Canonical context preview" />
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
        {reviewPin && <ContextPinReviewModal name="AGENTS.md" onClose={() => setReviewPin(false)} />}

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
    </ContextFrame>
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
  const fetchDiff = useCallback(() => fetchGlobalContextDiff(slug), [slug]);
  const evidence = useLoadedEvidence(slug, fetchDiff);
  const ready = evidence.status === 'ready';
  const [busy, setBusy] = useState(false);

  const handleAdopt = useCallback(async () => {
    setBusy(true);
    try {
      const doc = await adoptGlobalContext(slug);
      showToast('success', `Adopted ${name}'s edit into the canonical context`);
      onResolved(doc);
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'Adopt');
        showToast('error', described.text, { detail: described.fullDetail });
      }
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
      if (!isCancellation(err)) {
        const described = describeError(err, 'Overwrite');
        showToast('error', described.text, { detail: described.fullDetail });
      }
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
        <DriftDiffEvidence
          evidence={evidence}
          ariaLabel={`Diff for ${name}`}
          cliHints={contextDriftCliHints(slug, 'single-file')}
        />
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
            disabled={busy || !ready}
            title={ready ? undefined : EVIDENCE_GATE_TITLE}
            className="px-3 py-1.5 text-xs font-medium text-primary bg-primary/10 border border-primary/25 rounded-lg hover:bg-primary/15 transition-colors disabled:opacity-50"
          >
            Adopt into canon
          </button>
          <Button
            variant="outline-danger"
            size="sm"
            onClick={() => void handleOverwrite()}
            disabled={busy || !ready}
            title={ready ? undefined : EVIDENCE_GATE_TITLE}
          >
            Overwrite client
          </Button>
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
  // The diff is keyed by its request, so switching fragments derives back to
  // loading (actions gated) instead of showing the previous fragment's diff.
  // A multi-file client with nothing selected has no request: idle, not loading.
  const diffKey = multiFile && !selected ? null : `${client.slug}:${multiFile ? (selected ?? '') : ''}`;
  const fetchDiff = useCallback(
    () => fetchGlobalContextDiff(client.slug, multiFile ? (selected ?? undefined) : undefined),
    [client.slug, multiFile, selected],
  );
  const evidence = useLoadedEvidence(diffKey, fetchDiff);
  const ready = evidence.status === 'ready';
  const gateTitle = evidence.status === 'idle' ? IDLE_GATE_TITLE : EVIDENCE_GATE_TITLE;
  const [busy, setBusy] = useState(false);
  const driftCase = !multiFile ? 'compiled' : identity ? 'multi-file-identity' : 'multi-file-lossy';

  const run = useCallback(
    async (fn: () => Promise<unknown>, okMessage: string) => {
      setBusy(true);
      try {
        await fn();
        showToast('success', okMessage);
        onResolved();
      } catch (err) {
        if (!isCancellation(err)) {
          const described = describeError(err, 'Action');
          showToast('error', described.text, { detail: described.fullDetail });
        }
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
                  <span className="text-2xs font-mono text-text-primary truncate">{f.name}</span>
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
          <DriftDiffEvidence
            evidence={evidence}
            ariaLabel={multiFile ? `Diff for ${selected ?? 'fragment'} in ${client.name}` : `Diff for ${client.name}`}
            cliHints={contextDriftCliHints(client.slug, driftCase, selected)}
          />
          {multiFile ? (
            <div className="flex items-center justify-end gap-2">
              <Button type="button" size="sm" variant="ghost" onClick={onClose} disabled={busy}>
                Cancel
              </Button>
              {identity && selected && (
                <Button
                  type="button"
                  size="sm"
                  variant="primary"
                  disabled={busy || !ready}
                  title={ready ? undefined : gateTitle}
                  onClick={() =>
                    void run(
                      () => adoptGlobalContext(client.slug, { fragment: selected }),
                      `Adopted "${selected}" from ${client.name} into the canonical store`,
                    )
                  }
                >
                  {`Adopt ${selected}`}
                </Button>
              )}
              <Button
                type="button"
                size="sm"
                variant="outline-danger"
                disabled={busy || !ready}
                title={ready ? undefined : gateTitle}
                onClick={handleOverwrite}
              >
                Force overwrite
              </Button>
            </div>
          ) : (
            <CompiledCaptureActions
              fragmentNames={fragmentNames}
              busy={busy}
              evidenceReady={ready}
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
            <p className="text-2xs text-text-muted">
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
  evidenceReady,
  onCapture,
  onOverwrite,
  onClose,
}: {
  fragmentNames: string[];
  busy: boolean;
  evidenceReady: boolean;
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
        <label htmlFor="capture-fragment" className="text-2xs text-text-muted">
          Capture into
        </label>
        <select
          id="capture-fragment"
          value={choice}
          onChange={(e) => setChoice(e.target.value)}
          className="bg-background/60 border border-border/40 rounded-lg px-2 py-1.5 text-2xs font-mono text-text-primary"
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
            className="flex-1 min-w-32 bg-background/60 border border-border/40 rounded-lg px-2 py-1.5 text-2xs font-mono text-text-primary placeholder:text-text-muted"
          />
        )}
      </div>
      {isNew && newName && !valid && (
        <p className="text-2xs text-status-error">
          Lowercase letters, digits, and hyphens only.
        </p>
      )}
      <div className="flex items-center justify-end gap-2">
        <Button type="button" size="sm" variant="ghost" onClick={onClose} disabled={busy}>
          Cancel
        </Button>
        <Button
          type="button"
          size="sm"
          variant="primary"
          disabled={busy || !valid || !evidenceReady}
          title={evidenceReady ? undefined : EVIDENCE_GATE_TITLE}
          onClick={() => onCapture(target)}
        >
          {valid ? `Capture into ${target}` : 'Capture'}
        </Button>
        <Button
          type="button"
          size="sm"
          variant="outline-danger"
          disabled={busy || !evidenceReady}
          title={evidenceReady ? undefined : EVIDENCE_GATE_TITLE}
          onClick={onOverwrite}
        >
          Force overwrite
        </Button>
      </div>
    </div>
  );
}

const EVIDENCE_GATE_TITLE = 'Review the diff before choosing';
const IDLE_GATE_TITLE = 'Select a drifted fragment first';

/**
 * The evidence block of a drift dialog. A failed load is an alert outside
 * the diff with Retry and the CLI route for this case; its message is never
 * rendered as diff text.
 */
function DriftDiffEvidence({
  evidence,
  ariaLabel,
  cliHints,
}: {
  evidence: LoadedEvidence & { retry: () => void };
  ariaLabel: string;
  cliHints: string[];
}) {
  switch (evidence.status) {
    case 'idle':
      return <p className="text-2xs text-text-muted">Select a drifted fragment to review its diff.</p>;
    case 'loading':
      return <p className="text-2xs text-text-muted">Loading diff…</p>;
    case 'error':
      return (
        <div
          role="alert"
          className="rounded-lg border border-status-error/30 bg-status-error/10 p-3 text-2xs text-status-error space-y-2"
        >
          <p>Diff failed to load: {escapeNonPrintable(evidence.message)}</p>
          <p className="text-text-secondary">
            Or resolve it from a terminal:{' '}
            {cliHints.map((cmd, i) => (
              <span key={cmd}>
                {i > 0 && (i === cliHints.length - 1 ? (cliHints.length > 2 ? ', or ' : ' or ') : ', ')}
                <code className="font-mono">{cmd}</code>
              </span>
            ))}
            .
          </p>
          <Button type="button" size="sm" variant="secondary" onClick={evidence.retry}>
            Retry
          </Button>
        </div>
      );
    case 'ready':
      return evidence.text === '' ? (
        <p className="text-2xs text-text-muted">
          No line-level differences between the canonical content and this file.
        </p>
      ) : (
        <UnifiedDiffView diff={evidence.text} ariaLabel={ariaLabel} maxHeightClass="max-h-72" />
      );
  }
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
      if (!isCancellation(err)) {
        const described = describeError(err, 'Create fragment');
        showToast('error', described.text, { detail: described.fullDetail });
      }
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
          className="bg-background/60 border border-border/40 rounded-lg px-3 py-2 text-xs font-mono text-text-primary placeholder:text-text-muted"
        />
        {name && !valid && (
          <p className="text-2xs text-status-error">
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

interface FragmentDraft {
  name: string;
  value: string;
}

/**
 * Fragments-mode surface: a left rail of rule fragments in composition
 * order (adapting SkillFileTree's grammar) feeding the same editor and
 * preview split the single-file view uses, beside the right-rail inspector.
 */
export function FragmentsView({
  doc,
  refreshError,
  initialDriftSlug = null,
  fragmentParam,
  onFragmentParamChange,
  tab,
  onTabChange,
  onKindChange,
  onDirtyChange,
  onRegisterActions,
}: {
  doc: ContextDoc;
  refreshError: string | null;
  initialDriftSlug?: string | null;
  fragmentParam: string | null;
  onFragmentParamChange: (name: string | null) => void;
  tab: ContextInspectorTab;
  onTabChange: (tab: ContextInspectorTab) => void;
  onKindChange: (kind: RegistryKind) => void;
  onDirtyChange?: (dirty: boolean) => void;
  onRegisterActions?: (actions: ContextViewActions | null) => void;
}) {
  const navigate = useGuardedNavigate();
  const refresh = useContextStore((s) => s.refresh);
  const [fragments, setFragments] = useState<ContextFragment[] | null>(null);
  const [fragmentsError, setFragmentsError] = useState<string | null>(null);
  // The draft carries the name of the fragment it was typed in, so a change
  // of selection or URL can never re-target unsaved text at another fragment.
  // The refs mirror the latest values for decisions made after an await.
  const [draft, setDraft] = useState<FragmentDraft | null>(null);
  const draftRef = useRef<FragmentDraft | null>(null);
  const fragmentsRef = useRef<ContextFragment[] | null>(null);
  const fragmentParamRef = useRef<string | null>(fragmentParam);
  useLayoutEffect(() => {
    fragmentParamRef.current = fragmentParam;
  });
  const [revertOpen, setRevertOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [driftTarget, setDriftTarget] = useState<{ slug: string; fragment: string | null } | null>(
    null,
  );
  const [spotlightSlug, setSpotlightSlug] = useState<string | null>(null);
  const [showPreview, setShowPreview] = useState(true);
  const [pendingDelete, setPendingDelete] = useState<string | null>(null);
  const [reviewFragment, setReviewFragment] = useState<string | null>(null);

  // Deep link from the Connections hub: spotlight the client's row, and
  // open the review directly only when the target is unambiguous (compiled
  // client, or exactly one drifted fragment). Otherwise the expanded
  // fragment lines are the picker.
  useDriftSeed(initialDriftSlug, (slug) => {
    const c = doc.clients.find((cl) => cl.slug === slug);
    if (!c) return;
    setSpotlightSlug(slug);
    if (c.mode === 'multi-file') {
      const drifted = (c.fragments ?? []).filter((f) => f.state === 'drifted');
      if (drifted.length === 1) setDriftTarget({ slug, fragment: drifted[0].name });
    } else if (c.state === 'drifted') {
      setDriftTarget({ slug, fragment: null });
    }
  });

  const bodyRef = useRef<HTMLTextAreaElement>(null);
  const previewRef = useRef<HTMLDivElement>(null);
  const { ratio, containerRef, handlePointerDown, isDragging } = useSplitPane(0.5);

  const commitDraft = useCallback((next: FragmentDraft | null) => {
    draftRef.current = next;
    setDraft(next);
  }, []);

  useEffect(() => {
    fragmentsRef.current = fragments;
  }, [fragments]);

  // Resolves to the name of a fragment whose unsaved draft kept the
  // selection in place of selectName, or null.
  const loadFragments = useCallback(
    async (selectName?: string): Promise<string | null> => {
      try {
        const res = await fetchContextFragments();
        setFragments(res.fragments);
        setFragmentsError(null);
        // A draft whose fragment is gone is dropped rather than applied to
        // whichever fragment the fallback below selects.
        const d = draftRef.current;
        if (d && !res.fragments.some((f) => f.name === d.name)) {
          const wasDirty = d.value !== fragmentsRef.current?.find((f) => f.name === d.name)?.content;
          draftRef.current = null;
          setDraft(null);
          if (wasDirty) {
            showToast(
              'warning',
              `Fragment "${d.name}" was removed outside the editor. Its unsaved edits were discarded.`,
            );
          }
        }
        // Edits typed while this fetch was pending own the selection, so a
        // requested switch cannot hide them from the dirty guards.
        const live = draftRef.current;
        const liveFragment = live ? res.fragments.find((f) => f.name === live.name) : undefined;
        const held = live && liveFragment && live.value !== liveFragment.content ? live.name : null;
        const currentParam = fragmentParamRef.current;
        if (held) {
          if (currentParam !== held && (currentParam !== null || res.fragments[0]?.name !== held)) {
            onFragmentParamChange(held);
          }
        } else if (selectName && res.fragments.some((f) => f.name === selectName)) {
          onFragmentParamChange(selectName);
        } else if (currentParam && !res.fragments.some((f) => f.name === currentParam)) {
          onFragmentParamChange(null);
        }
        return held !== null && selectName !== undefined && held !== selectName ? held : null;
      } catch (err) {
        if (!isCancellation(err)) setFragmentsError(describeError(err, 'Load fragments').text);
        return null;
      }
    },
    [onFragmentParamChange],
  );

  // Initial load uses a raw promise chain (the useContextScan pattern) so
  // no setState is reachable from the effect body itself.
  useEffect(() => {
    let cancelled = false;
    fetchContextFragments()
      .then((res) => {
        if (cancelled) return;
        setFragments(res.fragments);
        setFragmentsError(null);
      })
      .catch((err) => {
        if (cancelled) return;
        if (!isCancellation(err)) setFragmentsError(describeError(err, 'Load fragments').text);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // A live dirty draft pins selection to its own fragment so no URL change
  // can ever display or save unsaved text under another fragment.
  const liveDirtyName = useMemo(() => {
    if (!draft || !fragments) return null;
    const match = fragments.find((f) => f.name === draft.name);
    if (!match || draft.value === match.content) return null;
    return draft.name;
  }, [draft, fragments]);

  const selected = useMemo(() => {
    if (!fragments) return null;
    if (liveDirtyName) return liveDirtyName;
    if (fragmentParam && fragments.some((f) => f.name === fragmentParam)) return fragmentParam;
    return fragments[0]?.name ?? null;
  }, [fragments, liveDirtyName, fragmentParam]);

  // Snap the URL back to the dirty fragment if an external URL edit or
  // history move tries to point ?fragment= somewhere else.
  const dirtySnapMismatch =
    liveDirtyName !== null &&
    (fragmentParam !== null
      ? fragmentParam !== liveDirtyName
      : (fragments?.[0]?.name ?? null) !== liveDirtyName);
  const lastSnappedRef = useRef<string | null>(null);
  useEffect(() => {
    if (!dirtySnapMismatch || !liveDirtyName) {
      lastSnappedRef.current = null;
      return;
    }
    const key = `${liveDirtyName}->${fragmentParam ?? ''}`;
    if (lastSnappedRef.current === key) return;
    lastSnappedRef.current = key;
    onFragmentParamChange(liveDirtyName);
    showToast('warning', 'Save or revert your changes before switching fragments');
  }, [dirtySnapMismatch, liveDirtyName, fragmentParam, onFragmentParamChange]);

  // Deep-link contract for ?fragment=: wait for the list load, toast once
  // and clear on a miss (mirrors PacksWorkspace).
  const lastResolvedFragmentRef = useRef<string | null>(null);
  useEffect(() => {
    if (!fragmentParam) {
      lastResolvedFragmentRef.current = null;
      return;
    }
    if (fragments === null || dirtySnapMismatch) return;
    if (lastResolvedFragmentRef.current === fragmentParam) return;
    lastResolvedFragmentRef.current = fragmentParam;
    if (!fragments.some((f) => f.name === fragmentParam)) {
      showToast('error', `Fragment "${fragmentParam}" not found`);
      onFragmentParamChange(null);
    }
  }, [fragmentParam, fragments, dirtySnapMismatch, onFragmentParamChange]);

  const current = fragments?.find((f) => f.name === selected) ?? null;
  const ownDraft = current !== null && draft !== null && draft.name === current.name ? draft : null;
  const content = ownDraft?.value ?? current?.content ?? '';
  // Own deferred value: this editor is a separate component from the canonical view.
  const deferredContent = useDeferredValue(content);
  const dirty = current !== null && ownDraft !== null && ownDraft.value !== current.content;
  const markerIssue = RESERVED_MARKERS.find((m) => content.includes(m)) ?? null;

  useEffect(() => {
    onDirtyChange?.(dirty);
    return () => onDirtyChange?.(false);
  }, [dirty, onDirtyChange]);

  const lineCount = content.split('\n').length;
  const charCount = content.length;
  const fragmentPath = current
    ? doc.canonical.path.replace(/AGENTS\.md$/, `fragments/${current.name}.md`)
    : doc.canonical.path.replace(/AGENTS\.md$/, 'fragments/');

  const handleSelect = useCallback(
    (name: string) => {
      if (name === selected) return;
      if (dirty) {
        showToast('warning', 'Save or revert your changes before switching fragments');
        return;
      }
      commitDraft(null);
      onFragmentParamChange(name);
    },
    [selected, dirty, commitDraft, onFragmentParamChange],
  );

  const handleSave = useCallback(async () => {
    if (!dirty || ownDraft === null || markerIssue) return;
    const savedName = ownDraft.name;
    const toSave = ownDraft.value;
    setSaving(true);
    try {
      const saved = await saveContextFragment(savedName, toSave);
      // Patch the local list first so a failed reload below cannot show
      // the pre-save text as clean content.
      setFragments((prev) =>
        prev?.map((f) =>
          f.name === savedName
            ? { ...f, content: toSave, bytes: new TextEncoder().encode(toSave).length }
            : f,
        ) ?? prev,
      );
      // Text typed while the PUT was in flight stays as the draft.
      const latest = draftRef.current;
      if (latest && latest.name === savedName && latest.value === toSave) commitDraft(null);
      if (saved.pin_status === 'drift') {
        showToast(
          'success',
          'Fragment saved. Run a sync to propagate. Pin drift: review and approve in Pins.',
          {
            action: {
              label: 'Review in Pins',
              onClick: () => navigate(contextPinsUrl(savedName, 'drift')),
            },
          },
        );
      } else {
        showToast('success', 'Fragment saved. Run a sync to propagate.');
      }
      // No name: the user may have switched to another fragment meanwhile.
      await loadFragments();
      await refresh();
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'Save');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    } finally {
      setSaving(false);
    }
  }, [dirty, ownDraft, markerIssue, commitDraft, loadFragments, refresh, navigate]);

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
      if (!isCancellation(err)) {
        const described = describeError(err, 'Sync');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    } finally {
      setSyncing(false);
    }
  }, [refresh]);

  const handleRefreshAll = useCallback(() => {
    void loadFragments();
    void refresh();
  }, [loadFragments, refresh]);

  useEffect(() => {
    onRegisterActions?.({
      refresh: handleRefreshAll,
      syncAll: () => {
        if (dirty) {
          showToast('warning', 'Save or revert your changes before syncing');
          return;
        }
        void handleSyncAll();
      },
    });
    return () => onRegisterActions?.(null);
  }, [onRegisterActions, handleRefreshAll, dirty, handleSyncAll]);

  const handleAdd = useCallback(
    async (name: string): Promise<boolean> => {
      if (dirty) {
        showToast('warning', 'Save or revert your changes before adding a fragment');
        return false;
      }
      let created = false;
      try {
        await saveContextFragment(name, '');
        created = true;
        // Typing that started during the create or the reload keeps the
        // selection on its fragment rather than switching to the new one.
        const held = await loadFragments(name);
        showToast(
          'success',
          held !== null
            ? `Fragment "${name}" created. Staying on "${held}" because it has unsaved changes.`
            : `Fragment "${name}" created`,
        );
        await refresh();
      } catch (err) {
        if (!isCancellation(err)) {
          const described = describeError(err, created ? 'Refresh' : 'Create fragment');
          showToast('error', described.text, { detail: described.fullDetail });
        }
      }
      return created;
    },
    [dirty, loadFragments, refresh],
  );

  const handleDelete = useCallback(
    async (name: string) => {
      try {
        await deleteContextFragment(name);
        showToast('success', `Fragment "${name}" removed (backup saved). Run a sync to drop its projections.`);
        // Drop the draft before the reload so it is not reported as removed
        // outside the editor.
        if (draftRef.current?.name === name) commitDraft(null);
        // Remove the URL parameter before the reload so the not-found
        // resolver never toasts for a fragment the user just removed.
        if (name === selected || name === fragmentParamRef.current) {
          onFragmentParamChange(null);
        }
        // Drop the entry locally so a failed reload below cannot leave the
        // removed fragment listed and editable.
        setFragments((prev) => prev?.filter((f) => f.name !== name) ?? prev);
        await loadFragments();
        await refresh();
      } catch (err) {
        if (!isCancellation(err)) {
          const described = describeError(err, 'Remove fragment');
          showToast('error', described.text, { detail: described.fullDetail });
        }
      }
    },
    [selected, commitDraft, onFragmentParamChange, loadFragments, refresh],
  );

  const handleDriftResolved = useCallback(() => {
    setDriftTarget(null);
    void loadFragments();
    void refresh();
  }, [loadFragments, refresh]);

  const editDraft = useCallback(
    (value: string) => {
      if (current) commitDraft({ name: current.name, value });
    },
    [current, commitDraft],
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
      editDraft(next.value);
      requestAnimationFrame(() => {
        ta.focus();
        ta.setSelectionRange(next.selStart, next.selEnd);
      });
    },
    [content, editDraft],
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

  const outOfSync = useMemo(() => fragmentOutOfSync(doc.clients), [doc.clients]);

  const inspector = (
    <ContextInspector
      clients={doc.clients}
      tab={tab}
      onTabChange={onTabChange}
      onReviewDrift={(slug, fragment) => setDriftTarget({ slug, fragment: fragment ?? null })}
      spotlightSlug={spotlightSlug}
      details={{
        mode: 'fragments',
        fragment: current,
        fragmentPath: current ? fragmentPath : null,
        outOfSyncClients: current ? (outOfSync.get(current.name) ?? []) : [],
        pinStatus: current ? contextPinStatus(current.name, doc.pins) : null,
      }}
    />
  );

  return (
    <ContextFrame
      onKindChange={onKindChange}
      onRefresh={handleRefreshAll}
      inspector={inspector}
      inspectorKey={spotlightSlug}
    >
      <div className="flex flex-col flex-1 min-h-0">
        {/* Action header */}
        <div className="flex items-center justify-between gap-3 px-5 py-3 border-b border-border/30 flex-shrink-0">
          <span className="text-2xs text-text-muted font-mono truncate min-w-0" title={fragmentPath}>
            {fragmentPath}
          </span>
          <div className="flex items-center gap-2 flex-shrink-0">
            <button
              type="button"
              onClick={() => setShowPreview((p) => !p)}
              title={showPreview ? 'Hide preview' : 'Show preview'}
              aria-label={showPreview ? 'Hide preview' : 'Show preview'}
              className={cn(
                'p-1.5 rounded-lg transition-all duration-200',
                showPreview
                  ? 'text-text-muted hover:text-primary hover:bg-primary/10'
                  : 'text-primary bg-primary/10',
              )}
            >
              {showPreview ? <Eye size={14} /> : <EyeOff size={14} />}
            </button>
            <button
              onClick={() => void handleSyncAll()}
              disabled={syncing || dirty}
              title={dirty ? 'Save before syncing' : 'Sync every available client'}
              className="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-status-running border border-status-running/25 hover:bg-status-running/10 rounded-lg transition-colors disabled:opacity-50"
            >
              <CloudUpload size={12} aria-hidden="true" className={syncing ? 'animate-pulse' : undefined} />
              {syncing ? 'Syncing…' : 'Sync all'}
            </button>
            <button
              type="button"
              onClick={() => setRevertOpen(true)}
              disabled={!dirty || saving}
              title="Discard unsaved changes to this fragment"
              className="inline-flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-text-secondary border border-border/40 hover:bg-surface-highlight/50 rounded-lg transition-colors disabled:opacity-50"
            >
              Revert
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
          <FailureBanner
            message={refreshError}
            onRetry={handleRefreshAll}
            className="mx-5 mt-2 mb-2 flex-shrink-0"
          />
        )}

        <div className="flex-1 flex min-h-0">
          <FragmentRail
            fragments={fragments}
            error={fragmentsError}
            onRetry={() => {
              if (fragments === null) setFragmentsError(null);
              void loadFragments();
            }}
            selected={selected}
            onSelect={handleSelect}
            onAdd={handleAdd}
            onDelete={setPendingDelete}
            outOfSync={outOfSync}
            pins={doc.pins}
            onReviewPin={setReviewFragment}
          />
          {reviewFragment && (
            <ContextPinReviewModal name={reviewFragment} onClose={() => setReviewFragment(null)} />
          )}

          {/* Editor area, identical grammar to the single-file view */}
          <div ref={containerRef} className="flex-1 flex min-w-0 min-h-0 group/split">
            {current === null ? (
              <div className="flex-1 flex items-center justify-center text-sm text-text-muted">
                {fragments === null
                  ? fragmentsError
                    ? 'Fragments unavailable'
                    : 'Loading fragments…'
                  : 'Add a fragment to start.'}
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
                    onChange={(e) => editDraft(e.target.value)}
                    onKeyDown={handleKeyDown}
                    onScroll={handleEditorScroll}
                    aria-label={`Fragment ${current.name}`}
                    placeholder={'---\ndescription: what this rule covers\npaths:\n  - "**/*.go"\n---\n\n# Rules\n'}
                    className="flex-1 w-full bg-background/40 px-5 py-4 text-sm font-mono text-text-primary placeholder:text-text-muted resize-none leading-relaxed"
                    spellCheck={false}
                  />
                </div>

                {showPreview && (
                  <>
                    <SplitPaneHandle onPointerDown={handlePointerDown} isDragging={isDragging} />
                    <div className="flex flex-col min-w-0 min-h-0" style={{ width: `${(1 - ratio) * 100}%` }}>
                      <div className="px-4 py-2 border-b border-border/20 flex-shrink-0">
                        <span className="text-xs text-text-muted uppercase tracking-wider">Preview</span>
                      </div>
                      <div ref={previewRef} className="flex-1 overflow-y-auto scrollbar-dark">
                        <div className="px-5 py-4">
                          <MarkdownPreview content={deferredContent} emptyHint="Fragment preview" />
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

        <ConfirmDialog
          isOpen={pendingDelete !== null}
          onClose={() => setPendingDelete(null)}
          onConfirm={() => {
            const name = pendingDelete;
            setPendingDelete(null);
            if (name) void handleDelete(name);
          }}
          title="Remove fragment"
          message={
            <>
              <p>
                Remove <span className="font-mono text-primary">{pendingDelete}</span>?
              </p>
              <p>
                A backup copy is saved outside the fragments directory. A sync afterward drops its projections.
              </p>
            </>
          }
          confirmLabel={`Remove "${pendingDelete ?? ''}"`}
          variant="danger"
        />

        <ConfirmDialog
          isOpen={revertOpen}
          onClose={() => setRevertOpen(false)}
          onConfirm={() => {
            setRevertOpen(false);
            commitDraft(null);
          }}
          title="Revert fragment"
          message={
            <p>
              Discard the unsaved changes to <span className="font-mono text-primary">{current?.name}</span> and
              restore its saved content?
            </p>
          }
          confirmLabel={`Revert "${current?.name ?? ''}"`}
          variant="danger"
        />

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
                onResolved={handleDriftResolved}
              />
            );
          })()}
      </div>
    </ContextFrame>
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
  error,
  onRetry,
  selected,
  onSelect,
  onAdd,
  onDelete,
  outOfSync,
  pins,
  onReviewPin,
}: {
  fragments: ContextFragment[] | null;
  error: string | null;
  onRetry: () => void;
  selected: string | null;
  onSelect: (name: string) => void;
  /** Resolves true when the fragment was created. */
  onAdd: (name: string) => Promise<boolean>;
  onDelete: (name: string) => void;
  /** Fragment name to client names holding a non-synced copy (drift dots). */
  outOfSync?: Map<string, string[]>;
  pins?: ContextDoc['pins'];
  onReviewPin?: (name: string) => void;
}) {
  const [showNew, setShowNew] = useState(false);
  const [newName, setNewName] = useState('');
  const valid = FRAGMENT_NAME_RE.test(newName);
  // Adding before the list has loaded would select a fragment the rail
  // cannot show, so Add waits for a successful load.
  const canAdd = fragments !== null;

  const [creating, setCreating] = useState(false);

  // A refused or failed create keeps the typed name so it can be retried.
  const submitNew = async () => {
    if (!valid || !canAdd || creating) return;
    setCreating(true);
    try {
      if (await onAdd(newName)) {
        setNewName('');
        setShowNew(false);
      }
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="w-60 flex-shrink-0 border-r border-border/30 flex flex-col min-h-0">
      <div className="flex items-center justify-between gap-2 px-3 py-2 border-b border-border/20 flex-shrink-0">
        <span className="text-3xs text-text-muted uppercase tracking-wider">Fragments</span>
        <button
          onClick={() => setShowNew((s) => !s)}
          disabled={!canAdd}
          className="text-3xs text-primary hover:text-primary-light flex items-center gap-0.5 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
        >
          <Plus size={10} /> Add
        </button>
      </div>
      {showNew && canAdd && (
        <div className="px-3 py-2 flex items-center gap-1.5 border-b border-border/20 flex-shrink-0">
          <input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') void submitNew();
            }}
            placeholder="10-style"
            aria-label="New fragment name"
            className="flex-1 min-w-0 bg-background/60 border border-border/40 rounded px-2 py-1 text-3xs font-mono text-text-primary placeholder:text-text-muted"
          />
          <button
            onClick={() => void submitNew()}
            disabled={!valid || !canAdd || creating}
            className="px-2 py-1 text-3xs text-primary bg-primary/10 rounded hover:bg-primary/20 transition-colors disabled:opacity-50"
          >
            Create
          </button>
        </div>
      )}
      <div className="flex-1 overflow-y-auto scrollbar-dark py-1">
        {error && <FailureBanner message={error} onRetry={onRetry} className="mx-2 mt-1 mb-1" />}
        {fragments === null ? (
          !error && <p className="text-3xs text-text-muted px-3 py-2">Loading fragments…</p>
        ) : fragments.length === 0 ? (
          <p className="text-3xs text-text-muted px-3 py-2 italic">No fragments yet</p>
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
                className={cn('flex-shrink-0', selected === f.name ? 'text-primary' : 'text-text-muted')}
              />
              <button
                onClick={() => onSelect(f.name)}
                title={f.description || f.name}
                className={cn(
                  'text-2xs font-mono flex-1 text-left truncate transition-colors',
                  selected === f.name ? 'text-primary' : 'text-text-secondary hover:text-primary',
                )}
              >
                {f.name}
              </button>
              {onReviewPin && (
                <ContextPinChip
                  status={contextPinStatus(f.name, pins)}
                  onReview={() => onReviewPin(f.name)}
                />
              )}
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
                  className="text-3xs px-1 py-0.5 rounded border border-border/40 text-text-muted font-mono flex-shrink-0"
                >
                  globs
                </span>
              )}
              <span className="text-3xs text-text-muted font-mono flex-shrink-0">{f.bytes}B</span>
              <button
                type="button"
                onClick={() => onDelete(f.name)}
                title={`Remove fragment ${f.name}`}
                aria-label={`Remove fragment ${f.name}`}
                className="opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 hover-none:opacity-100 p-1 rounded text-text-muted hover:text-status-error hover:bg-status-error/10 focus:opacity-100 transition-all"
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
