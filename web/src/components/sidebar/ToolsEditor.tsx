import { useEffect } from 'react';
import { createPortal } from 'react-dom';
import { Command } from 'cmdk';
import { AlertCircle, Check, Loader2, RefreshCw, Save, Search, Undo2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useToolsEditor } from '../../hooks/useToolsEditor';
import { useBeforeUnloadGuard } from '../../hooks/useBeforeUnloadGuard';
import { useInspectorToolsGuardStore } from '../../stores/useInspectorToolsGuardStore';
import { ConfirmDialog } from '../ui/ConfirmDialog';

interface ToolsEditorProps {
  serverName: string;
  // Current whitelist applied to the server in the live stack. An empty array
  // means "no whitelist" — every tool the gateway knows about is exposed.
  //
  // The gateway only ever loads the post-filter set of tools, so the editor
  // shows exactly what is currently exposed: to expand beyond the saved
  // whitelist the user must clear it, save, and then re-select. Mirrors how
  // the ToolsPicker wizard step operates.
  savedTools: string[];
  // Unprefixed tool names advertised by this specific server (from the
  // status endpoint's per-server `tools` field). This is the authoritative
  // list of every tool the server exposes, unaffected by code mode — which
  // hides downstream tools behind meta-tools in the global aggregated list.
  serverTools?: string[];
  // Moves the host's selection back to the draft's owner on "Keep editing".
  // Defaults to selecting the owner's node in the Stack store, which only the
  // main-window inspector uses.
  onRestoreServer?: (name: string) => void;
}

// ToolsEditor is the sidebar's per-server whitelist editor. All of its state
// and the save-and-reload flow live in useToolsEditor; this component is the
// presentational shell. The fleet Tools workspace reuses the same hook so the
// two surfaces stay in lock-step.
export function ToolsEditor({ serverName, savedTools, serverTools, onRestoreServer }: ToolsEditorProps) {
  const {
    allTools,
    visible,
    query,
    setQuery,
    selected,
    toggle,
    selectAll,
    clearAll,
    dirty,
    saveBlocked,
    diffCount,
    isSaving,
    conflict,
    hiddenReference,
    owner,
    locked,
    discardPrompt,
    handleSave,
    handleReloadFromDisk,
    handleDiscard,
    handleKeepEditing,
  } = useToolsEditor(serverName, savedTools, serverTools, { onRestoreServer });

  // Publish the draft's owner while it has unsaved edits so leave actions
  // outside this tree can ask first. Cleared on unmount.
  const guarding = locked || dirty;
  useEffect(() => {
    useInspectorToolsGuardStore.getState().setDirtyServer(guarding ? owner : null);
  }, [guarding, owner]);
  useEffect(
    () => () => {
      useInspectorToolsGuardStore.setState({ dirtyServer: null, pendingLeave: null });
    },
    [],
  );
  useBeforeUnloadGuard(guarding);

  const pendingLeave = useInspectorToolsGuardStore((s) => s.pendingLeave);
  const promptOpen = discardPrompt !== null || pendingLeave !== null;
  // dirty and diffCount compare against serverName's baseline, which is the
  // wrong server while locked, so the lock gates them first.
  const inert = locked || pendingLeave !== null;
  const showDirty = dirty && !locked;

  const saveLabel = showDirty
    ? `Save ${diffCount} change${diffCount === 1 ? '' : 's'} & Reload`
    : 'Saved';

  const confirmPrompt = () => {
    handleDiscard();
    if (useInspectorToolsGuardStore.getState().pendingLeave) {
      useInspectorToolsGuardStore.getState().confirmLeave();
    }
  };
  const cancelPrompt = () => {
    // A pending leave is dropped first; a locked prompt behind it stays up.
    if (useInspectorToolsGuardStore.getState().pendingLeave) {
      useInspectorToolsGuardStore.getState().cancelLeave();
      return;
    }
    handleKeepEditing();
  };

  return (
    <div aria-label="Tools Editor" aria-busy={isSaving} className="space-y-2">
      {/* Portaled: a collapsed InspectorSection hides its panel, which would
          hide an in-place fixed dialog while its focus trap stayed active.
          The wrapper keeps clicks inside the dialog from reaching React
          ancestors of the editor. */}
      {promptOpen &&
        createPortal(
          <div onClick={(e) => e.stopPropagation()}>
            <ConfirmDialog
              isOpen
              onClose={cancelPrompt}
              onConfirm={confirmPrompt}
              title="Unsaved tool changes"
              message={<p>Discard unsaved changes to {owner}?</p>}
              confirmLabel="Discard"
              cancelLabel="Keep editing"
              variant="danger"
              busy={isSaving}
            />
          </div>,
          document.body,
        )}

      <div className="rounded-lg border border-border/40 bg-background/60 overflow-hidden">
        <div className="flex items-center gap-2 px-3 py-2 text-3xs text-text-muted border-b border-border/30">
          {saveBlocked ? (
            <span role="alert" className="text-status-error">
              <span className="font-medium">0</span> of{' '}
              <span className="font-medium">{allTools.length}</span> selected — cannot save
              an empty selection (an empty whitelist would expose all tools)
            </span>
          ) : (
            <span>
              <span className="text-text-secondary font-medium">{selected.size}</span> of{' '}
              <span className="text-text-secondary font-medium">{allTools.length}</span>{' '}
              enabled — empty means all tools exposed
            </span>
          )}
          <div className="ml-auto flex items-center gap-2">
            <button
              type="button"
              onClick={selectAll}
              aria-label="Select all tools"
              disabled={isSaving || inert}
              className="text-3xs text-secondary hover:text-secondary-light transition-colors disabled:opacity-50"
            >
              Select all
            </button>
            <span className="text-border" aria-hidden="true">·</span>
            <button
              type="button"
              onClick={clearAll}
              aria-label="Clear all tools"
              disabled={isSaving || inert}
              className="text-3xs text-secondary hover:text-secondary-light transition-colors disabled:opacity-50"
            >
              Clear
            </button>
          </div>
        </div>

        <Command shouldFilter={false} label="Tools Editor" className="group/cmd flex flex-col">
          <div className="flex items-center gap-2 px-3 py-2 border-b border-border/30">
            <Search size={12} className="text-text-muted flex-shrink-0" aria-hidden="true" />
            <Command.Input
              value={query}
              onValueChange={setQuery}
              placeholder="Search tools..."
              aria-label="Search tools"
              className="flex-1 bg-transparent rounded text-xs text-text-primary placeholder:text-text-muted"
            />
          </div>

          <Command.List className="max-h-60 overflow-y-auto" aria-label="Available tools">
            <Command.Empty>
              <p className="text-2xs text-text-muted italic py-4 px-3 text-center">
                {allTools.length === 0
                  ? 'No tools discovered for this server yet.'
                  : `No tools match "${query}"`}
              </p>
            </Command.Empty>
            {visible.map((opt) => {
              const isSelected = selected.has(opt.name);
              return (
                <Command.Item
                  key={opt.name}
                  value={opt.name}
                  disabled={inert}
                  onSelect={() => toggle(opt.name)}
                  aria-checked={isSelected}
                  aria-label={opt.name}
                  className={cn(
                    'flex items-start gap-2.5 px-3 py-2 cursor-pointer select-none transition-colors',
                    'hover:bg-surface-highlight/50',
                    '[&[data-selected=true]]:bg-primary/[0.06]',
                    'group-focus-within/cmd:data-[selected=true]:outline-2 group-focus-within/cmd:data-[selected=true]:-outline-offset-2 group-focus-within/cmd:data-[selected=true]:outline-primary',
                    isSelected && 'bg-primary/[0.03]',
                  )}
                >
                  <div
                    className={cn(
                      'mt-0.5 w-3.5 h-3.5 rounded border flex items-center justify-center flex-shrink-0 transition-colors',
                      isSelected
                        ? 'bg-primary/20 border-primary/60'
                        : 'border-border/60 bg-background/50',
                    )}
                    aria-hidden="true"
                  >
                    {isSelected && <Check size={10} className="text-primary" />}
                  </div>
                  <div className="flex-1 min-w-0">
                    <div
                      className={cn(
                        'text-xs font-mono truncate',
                        isSelected ? 'text-text-primary' : 'text-text-secondary',
                      )}
                    >
                      {opt.name}
                    </div>
                    {opt.description && (
                      <div className="text-3xs text-text-muted truncate">{opt.description}</div>
                    )}
                  </div>
                </Command.Item>
              );
            })}
          </Command.List>
        </Command>
      </div>

      {hiddenReference && (
        <div role="alert" className="rounded-md border border-status-error/40 bg-status-error/[0.05] px-3 py-2">
          <p className="text-2xs text-status-error">{hiddenReference}</p>
        </div>
      )}

      {conflict && (
        <div
          role="alert"
          className="flex items-start gap-2 rounded-md border border-status-pending/40 bg-status-pending/[0.05] px-3 py-2"
        >
          <AlertCircle size={12} className="text-status-pending flex-shrink-0 mt-0.5" />
          <div className="flex-1 min-w-0 space-y-1">
            <p className="text-2xs text-status-pending font-medium">
              The stack file was modified outside the canvas.
            </p>
            <p className="text-3xs text-text-muted">{conflict}</p>
            <button
              type="button"
              onClick={handleReloadFromDisk}
              aria-label="Reload stack file from disk"
              className="inline-flex items-center gap-1 text-3xs text-secondary hover:text-secondary-light transition-colors"
            >
              <RefreshCw size={10} />
              Reload file
            </button>
          </div>
        </div>
      )}

      <div className="flex items-center gap-2">
        {showDirty && (
          <button
            type="button"
            onClick={handleDiscard}
            disabled={isSaving || inert}
            aria-label="Discard unsaved tool changes"
            className="flex-shrink-0 inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-2xs font-medium border border-border/40 text-text-secondary hover:text-text-primary hover:bg-surface-highlight/50 transition-colors disabled:opacity-50"
          >
            <Undo2 size={11} />
            Discard
          </button>
        )}
        <button
          type="button"
          onClick={handleSave}
          disabled={!showDirty || isSaving || saveBlocked || inert}
          aria-label={saveLabel}
          className={cn(
            'flex-1 inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-2xs font-medium transition-colors',
            showDirty && !isSaving && !saveBlocked && !inert
              ? 'bg-primary/20 text-primary border border-primary/30 hover:bg-primary/30'
              : 'bg-surface-highlight/50 text-text-muted border border-border/30 cursor-not-allowed',
          )}
        >
          {isSaving ? (
            <>
              <Loader2 size={11} className="animate-spin" />
              Saving…
            </>
          ) : (
            <>
              <Save size={11} />
              {saveLabel}
            </>
          )}
        </button>
      </div>
    </div>
  );
}
