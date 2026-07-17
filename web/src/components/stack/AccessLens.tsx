import { useCallback, useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { ScanEye, PanelRightOpen, Save, Undo2, AlertTriangle } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';
import {
  useAccessLensStore,
  isDirty,
  buildDraftScope,
  flattenTools,
  hasEmptyCustomGrant,
  canonical,
  type SeedParams,
} from '../../stores/useAccessLensStore';
import { baselineServers } from '../../hooks/useClientScopeEditor';
import { SlideOver } from '../ui/SlideOver';
import { Modal } from '../ui/Modal';
import { AccessLensEditorBody } from './AccessLensEditorBody';
import { AccessLensCommitGate } from './AccessLensCommitGate';
import type { ClientStatus, MCPServerStatus } from '../../types';

interface AccessLensProps {
  servers: MCPServerStatus[];
}

interface ExitRequest {
  message: string;
  proceed: () => void;
  cancel?: () => void;
}

// AccessLens is the Stack workspace's authoring surface: a header toggle that turns the
// canvas into a draft editor for the selected client's server access, a floating
// action bar reporting live impact, the right-anchored slide-over editor, the
// commit gate (the only write boundary), and the dirty-draft discard-with-confirm
// guard on every exit path (mode-off, client-change, navigate-away). The draft
// itself lives in the lifted store so the canvas can preview it without this
// component or the slide-over being the owner.
export function AccessLens({ servers }: AccessLensProps) {
  const clients = useStackStore((s) => s.clients);
  const selectedNodeId = useStackStore((s) => s.selectedNodeId);
  const selectNode = useStackStore((s) => s.selectNode);

  const enabled = useAccessLensStore((s) => s.enabled);
  const slideOverOpen = useAccessLensStore((s) => s.slideOverOpen);
  const clientSlug = useAccessLensStore((s) => s.clientSlug);
  const clientName = useAccessLensStore((s) => s.clientName);
  const draft = useAccessLensStore((s) => s.draft);
  const baseline = useAccessLensStore((s) => s.baseline);
  const toolMode = useAccessLensStore((s) => s.toolMode);
  const customSel = useAccessLensStore((s) => s.customSel);
  const baselineTools = useAccessLensStore((s) => s.baselineTools);
  const toolsTouched = useAccessLensStore((s) => s.toolsTouched);
  const savedTools = useAccessLensStore((s) => s.savedTools);
  const setEnabled = useAccessLensStore((s) => s.setEnabled);
  const seed = useAccessLensStore((s) => s.seed);
  const openSlideOver = useAccessLensStore((s) => s.openSlideOver);
  const closeSlideOver = useAccessLensStore((s) => s.closeSlideOver);
  const discardDraft = useAccessLensStore((s) => s.discardDraft);
  const clearDraft = useAccessLensStore((s) => s.clearDraft);
  const exitNavTarget = useAccessLensStore((s) => s.exitNavTarget);
  const clearExitNav = useAccessLensStore((s) => s.clearExitNav);
  const pendingSwitchSlug = useAccessLensStore((s) => s.pendingSwitchSlug);
  const requestSwitch = useAccessLensStore((s) => s.requestSwitch);
  const clearSwitch = useAccessLensStore((s) => s.clearSwitch);
  const navigate = useNavigate();

  const [gateOpen, setGateOpen] = useState(false);
  // Local exit confirm for mode-off (set from an event handler, never an effect).
  // Client-switch and navigate-away confirms are store-driven (see below).
  const [exit, setExit] = useState<ExitRequest | null>(null);

  const serverToolMap = useMemo(
    () => Object.fromEntries(servers.map((s) => [s.name, s.tools ?? []])),
    [servers],
  );
  // The flattened tool allow-list the current draft would write (see
  // flattenTools), and whether it is a deliberate, effective change: only a
  // touched-and-different tool axis counts, so server-only edits preserve a
  // client's hand-authored tool list rather than clobbering it.
  const flatTools = useMemo(
    () => flattenTools(draft, serverToolMap, toolMode, customSel),
    [draft, serverToolMap, toolMode, customSel],
  );
  const toolsDirty = toolsTouched && isDirty(flatTools, baselineTools);
  const emptyCustom = hasEmptyCustomGrant(draft, toolMode, customSel);
  // The tool list the draft would actually result in: when the operator has not
  // touched a tool group, the commit OMITS the axis and the backend keeps the
  // saved list, so the live preview must reflect savedTools — not the freshly
  // flattened intent — or the canvas would show more reach than a save grants.
  const previewToolList = toolsDirty ? flatTools : savedTools;
  // A granted server that is still initializing has an unknown tool universe, so
  // a restrictive flat list would silently drop it (and keep its tools hidden
  // once it comes up). Block the save until it reports its tools. Servers that
  // failed registration outright are exempt: they will never report tools, so
  // blocking on them would wedge saves forever.
  const pendingInit =
    toolsDirty &&
    draft.some((n) => {
      const server = servers.find((s) => s.name === n);
      return server?.initialized === false && !server.registrationFailed;
    });

  const serversDirty = clientSlug != null && isDirty(draft, baseline);
  const dirty = clientSlug != null && (serversDirty || toolsDirty);
  const canSave =
    clientSlug != null &&
    draft.length > 0 &&
    (serversDirty || toolsDirty) &&
    !emptyCustom &&
    !pendingInit;

  const selectedClient = useMemo<ClientStatus | null>(() => {
    if (!selectedNodeId?.startsWith('client-')) return null;
    const slug = selectedNodeId.slice('client-'.length);
    return clients.find((c) => c.slug === slug && c.linked) ?? null;
  }, [selectedNodeId, clients]);

  const allServerNames = useMemo(() => servers.map((s) => s.name), [servers]);

  const makeSeed = useCallback(
    (client: ClientStatus): SeedParams => ({
      slug: client.slug,
      name: client.name,
      baseline: baselineServers(client, allServerNames),
      savedTools: client.effectiveScope?.tools ?? [],
      createsBlock: !client.effectiveScope?.configured,
      serverTools: serverToolMap,
    }),
    [allServerNames, serverToolMap],
  );

  // Seed / re-seed the draft as the selected client changes, guarding a dirty
  // draft on a client switch (discard-with-confirm). Never reseeds an unchanged
  // client's in-progress edit; reseeds a same-client baseline only when a refresh
  // moved it and the draft is clean.
  useEffect(() => {
    if (!enabled || !selectedClient || pendingSwitchSlug) return;

    if (selectedClient.slug === clientSlug) {
      const fresh = canonical(baselineServers(selectedClient, allServerNames));
      // Reseed a same-client baseline only when a refresh moved it AND there is
      // no in-progress edit (server OR tool) to clobber.
      if (!isDirty(draft, baseline) && !toolsDirty && isDirty(fresh, baseline)) {
        seed(makeSeed(selectedClient));
      }
      return;
    }

    // A different client became selected. A dirty draft must be confirmed before
    // we retarget — stash the intent in the store (a store update is allowed in
    // an effect; React setState is not).
    if (clientSlug != null && (isDirty(draft, baseline) || toolsDirty)) {
      requestSwitch(selectedClient.slug);
      return;
    }
    seed(makeSeed(selectedClient));
  }, [
    enabled,
    selectedClient,
    clientSlug,
    draft,
    baseline,
    toolsDirty,
    allServerNames,
    pendingSwitchSlug,
    makeSeed,
    seed,
    requestSwitch,
  ]);

  // Hard page unload (reload/close): warn while a draft is unsaved.
  useEffect(() => {
    if (!dirty) return;
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = '';
    };
    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, [dirty]);

  const requestDisable = useCallback(() => {
    if (dirty) {
      setExit({
        message: 'Turn off Access Lens and discard unsaved access changes?',
        proceed: () => {
          clearDraft();
          setEnabled(false);
        },
      });
      return;
    }
    clearDraft();
    setEnabled(false);
  }, [dirty, clearDraft, setEnabled]);

  const hasPendingExit = exit != null || pendingSwitchSlug != null || exitNavTarget != null;

  // Escape exits the mode (honoring the dirty confirm) when no overlay of its own
  // is handling the key. The gate (Modal) and slide-over own their Escape.
  useEffect(() => {
    if (!enabled) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (gateOpen || slideOverOpen || hasPendingExit) return;
      const tag = (e.target as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
      e.stopPropagation();
      requestDisable();
    };
    // Capture so the lens claims Escape before the global deselect handler.
    document.addEventListener('keydown', onKey, true);
    return () => document.removeEventListener('keydown', onKey, true);
  }, [enabled, gateOpen, slideOverOpen, hasPendingExit, requestDisable]);

  const toolsVisible = useMemo(
    () => (clientSlug ? buildDraftScope(draft, servers, previewToolList).tools.length : 0),
    [clientSlug, draft, servers, previewToolList],
  );

  // Unify the three exit-confirm sources into one descriptor. Mode-off is local
  // React state; client-switch and navigate-away are store-driven so they never
  // require setState inside an effect.
  const switchTarget = pendingSwitchSlug
    ? clients.find((c) => c.slug === pendingSwitchSlug && c.linked) ?? null
    : null;
  const activeExit: ExitRequest | null =
    exit ??
    (pendingSwitchSlug
      ? {
          message: `Discard unsaved access changes to ${clientName}?`,
          proceed: () => {
            if (switchTarget) seed(makeSeed(switchTarget));
            clearSwitch();
          },
          cancel: () => {
            if (clientSlug) selectNode(`client-${clientSlug}`);
            clearSwitch();
          },
        }
      : exitNavTarget
        ? {
            message: 'Leave Access Lens and discard unsaved access changes?',
            proceed: () => {
              clearDraft();
              setEnabled(false);
              clearExitNav();
              navigate(exitNavTarget);
            },
            cancel: () => clearExitNav(),
          }
        : null);

  function resolveExit(run?: () => void) {
    run?.();
    setExit(null);
  }

  return (
    <>
      {/* Header toggle — net-new to the Stack workspace. Amber when on, with aria-pressed. */}
      <div className="absolute top-3 left-3 z-20 flex items-center gap-2">
        <button
          type="button"
          onClick={() => (enabled ? requestDisable() : setEnabled(true))}
          aria-pressed={enabled}
          aria-label="Toggle Access Lens"
          className={cn(
            'inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[11px] font-medium border transition-colors glass-panel',
            enabled
              ? 'bg-primary/15 text-primary border-primary/40 shadow-glow-primary'
              : 'text-text-muted border-border/40 hover:text-text-secondary hover:border-border',
          )}
        >
          <ScanEye size={12} aria-hidden="true" />
          Access Lens
        </button>

        {enabled && (
          <div className="glass-panel rounded-md px-2.5 py-1.5 text-[10px] text-text-muted flex items-center gap-2" role="status">
            {selectedClient ? (
              <>
                <span>
                  Editing <span className="text-text-secondary font-mono">{selectedClient.name}</span>{' '}
                  — click servers to grant or revoke
                </span>
                <button
                  type="button"
                  onClick={openSlideOver}
                  className="inline-flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
                >
                  <PanelRightOpen size={11} aria-hidden="true" />
                  Editor
                </button>
              </>
            ) : (
              <span>Select a client to shape its access. Multi-select is not editable here.</span>
            )}
          </div>
        )}
      </div>

      {/* Slide-over editor — beside the canvas, which stays interactive. */}
      <SlideOver
        isOpen={enabled && slideOverOpen && selectedClient != null}
        onClose={closeSlideOver}
        title="Access editor"
      >
        <AccessLensEditorBody servers={servers} />
      </SlideOver>

      {/* Floating action bar — visible whenever the draft is dirty. */}
      {dirty && (
        <div className="absolute bottom-6 left-1/2 -translate-x-1/2 z-30 animate-fade-in-up">
          <div className="glass-panel-elevated shadow-bevel rounded-xl px-4 py-2.5 flex items-center gap-4">
            <span className="text-[11px] text-text-secondary" role="status">
              <span className="font-mono text-primary">{draft.length}</span> server
              {draft.length === 1 ? '' : 's'} granted ·{' '}
              <span className="font-mono text-primary">{toolsVisible}</span> tool
              {toolsVisible === 1 ? '' : 's'} visible
            </span>
            {pendingInit && (
              <span className="text-[10px] text-status-pending" role="status">
                A granted server is still initializing — wait before narrowing tools.
              </span>
            )}
            <div className="h-4 w-px bg-border/50" aria-hidden="true" />
            <button
              type="button"
              onClick={discardDraft}
              className="inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-[11px] text-text-secondary hover:text-text-primary transition-colors"
            >
              <Undo2 size={12} aria-hidden="true" />
              Discard
            </button>
            <button
              type="button"
              onClick={() => setGateOpen(true)}
              disabled={!canSave}
              aria-label="Save access scope"
              className={cn(
                'inline-flex items-center gap-1.5 rounded-md px-3 py-1 text-[11px] font-medium border transition-colors',
                canSave
                  ? 'bg-primary/20 text-primary border-primary/30 hover:bg-primary/30'
                  : 'bg-surface-highlight/50 text-text-muted border-border/30 cursor-not-allowed',
              )}
            >
              <Save size={12} aria-hidden="true" />
              Save Scope
            </button>
          </div>
        </div>
      )}

      <AccessLensCommitGate
        servers={servers}
        isOpen={gateOpen}
        onClose={() => setGateOpen(false)}
        onCommitted={() => {
          setGateOpen(false);
          closeSlideOver();
        }}
      />

      {/* Discard-with-confirm — shared by mode-off, client-change, navigate-away. */}
      <Modal
        isOpen={activeExit != null}
        onClose={() => resolveExit(activeExit?.cancel)}
        title="Unsaved access changes"
      >
        <div className="space-y-4 text-sm">
          <div className="flex items-start gap-2.5 rounded-md border border-status-pending/40 bg-status-pending/[0.06] px-3 py-3">
            <AlertTriangle size={14} className="text-status-pending flex-shrink-0 mt-0.5" aria-hidden="true" />
            <p className="text-[12px] text-text-secondary leading-relaxed">{activeExit?.message}</p>
          </div>
          <div className="flex items-center justify-end gap-2">
            <button
              type="button"
              onClick={() => resolveExit(activeExit?.cancel)}
              className="rounded-md px-3 py-1.5 text-[11px] text-text-secondary hover:text-text-primary transition-colors"
            >
              Keep editing
            </button>
            <button
              type="button"
              onClick={() => resolveExit(activeExit?.proceed)}
              className="inline-flex items-center gap-1.5 rounded-md px-3 py-1.5 text-[11px] font-medium border border-status-pending/40 bg-status-pending/10 text-status-pending hover:bg-status-pending/20 transition-colors"
            >
              Discard changes
            </button>
          </div>
        </div>
      </Modal>
    </>
  );
}

export default AccessLens;
