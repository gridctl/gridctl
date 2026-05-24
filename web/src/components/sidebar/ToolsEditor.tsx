import { Command } from 'cmdk';
import { AlertCircle, Check, Loader2, RefreshCw, Save, Search } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useToolsEditor } from '../../hooks/useToolsEditor';

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
}

// ToolsEditor is the sidebar's per-server whitelist editor. All of its state
// and the save-and-reload flow live in useToolsEditor; this component is the
// presentational shell. The fleet Tools workspace reuses the same hook so the
// two surfaces stay in lock-step.
export function ToolsEditor({ serverName, savedTools, serverTools }: ToolsEditorProps) {
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
    diffCount,
    isSaving,
    conflict,
    discardPrompt,
    handleSave,
    handleReloadFromDisk,
    handleDiscard,
    handleKeepEditing,
  } = useToolsEditor(serverName, savedTools, serverTools);

  const saveLabel = dirty
    ? `Save ${diffCount} change${diffCount === 1 ? '' : 's'} & Reload`
    : 'Saved';

  return (
    <div aria-label="Tools Editor" aria-busy={isSaving} className="space-y-2">
      {discardPrompt && (
        <div
          role="alertdialog"
          aria-label="Discard unsaved tool changes"
          className="rounded-md border border-status-pending/40 bg-status-pending/[0.05] px-3 py-2 space-y-2"
        >
          <p className="text-[11px] text-status-pending font-medium">
            Discard unsaved changes to {discardPrompt}?
          </p>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={handleDiscard}
              className="text-[10px] px-2 py-1 rounded-md bg-status-pending/20 text-status-pending hover:bg-status-pending/30 transition-colors"
            >
              Discard
            </button>
            <button
              type="button"
              onClick={handleKeepEditing}
              className="text-[10px] px-2 py-1 rounded-md border border-border/40 text-text-secondary hover:bg-surface-highlight/50 transition-colors"
            >
              Keep editing
            </button>
          </div>
        </div>
      )}

      <div className="rounded-lg border border-border/40 bg-background/60 overflow-hidden">
        <div className="flex items-center gap-2 px-3 py-2 text-[10px] text-text-muted border-b border-border/30">
          <span>
            <span className="text-text-secondary font-medium">{selected.size}</span> of{' '}
            <span className="text-text-secondary font-medium">{allTools.length}</span>{' '}
            enabled — empty means all tools exposed
          </span>
          <div className="ml-auto flex items-center gap-2">
            <button
              type="button"
              onClick={selectAll}
              aria-label="Select all tools"
              disabled={isSaving}
              className="text-[10px] text-secondary hover:text-secondary-light transition-colors disabled:opacity-50"
            >
              Select all
            </button>
            <span className="text-border" aria-hidden="true">·</span>
            <button
              type="button"
              onClick={clearAll}
              aria-label="Clear all tools"
              disabled={isSaving}
              className="text-[10px] text-secondary hover:text-secondary-light transition-colors disabled:opacity-50"
            >
              Clear
            </button>
          </div>
        </div>

        <Command shouldFilter={false} label="Tools Editor" className="flex flex-col">
          <div className="flex items-center gap-2 px-3 py-2 border-b border-border/30">
            <Search size={12} className="text-text-muted flex-shrink-0" aria-hidden="true" />
            <Command.Input
              value={query}
              onValueChange={setQuery}
              placeholder="Search tools..."
              aria-label="Search tools"
              className="flex-1 bg-transparent outline-none text-xs text-text-primary placeholder:text-text-muted/60"
            />
          </div>

          <Command.List className="max-h-60 overflow-y-auto" aria-label="Available tools">
            <Command.Empty>
              <p className="text-[11px] text-text-muted/60 italic py-4 px-3 text-center">
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
                  onSelect={() => toggle(opt.name)}
                  aria-checked={isSelected}
                  aria-label={opt.name}
                  className={cn(
                    'flex items-start gap-2.5 px-3 py-2 cursor-pointer select-none outline-none transition-colors',
                    'hover:bg-surface-highlight/50',
                    '[&[data-selected=true]]:bg-primary/[0.06]',
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
                      <div className="text-[10px] text-text-muted truncate">{opt.description}</div>
                    )}
                  </div>
                </Command.Item>
              );
            })}
          </Command.List>
        </Command>
      </div>

      {conflict && (
        <div
          role="alert"
          className="flex items-start gap-2 rounded-md border border-status-pending/40 bg-status-pending/[0.05] px-3 py-2"
        >
          <AlertCircle size={12} className="text-status-pending flex-shrink-0 mt-0.5" />
          <div className="flex-1 min-w-0 space-y-1">
            <p className="text-[11px] text-status-pending font-medium">
              The stack file was modified outside the canvas.
            </p>
            <p className="text-[10px] text-text-muted">{conflict}</p>
            <button
              type="button"
              onClick={handleReloadFromDisk}
              aria-label="Reload stack file from disk"
              className="inline-flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
            >
              <RefreshCw size={10} />
              Reload file
            </button>
          </div>
        </div>
      )}

      <button
        type="button"
        onClick={handleSave}
        disabled={!dirty || isSaving}
        aria-label={saveLabel}
        className={cn(
          'w-full inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-[11px] font-medium transition-colors',
          dirty && !isSaving
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
  );
}
