import { useEffect, useMemo, useRef, useState } from 'react';
import { Command } from 'cmdk';
import Fuse from 'fuse.js';
import { AlertCircle, Check, Loader2, RefreshCw, Save, Search } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import { showToast } from '../ui/Toast';
import {
  AuthError,
  fetchStatus,
  fetchTools,
  setServerTools,
  SetServerToolsError,
} from '../../lib/api';

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

interface ToolRow {
  name: string;
  description?: string;
}

// canonicalWhitelist normalizes a selection into the wire form: a sorted,
// deduplicated array. Dirty comparison uses it on both sides so selection
// order never triggers a spurious "unsaved changes" state.
function canonicalWhitelist(names: string[]): string[] {
  return Array.from(new Set(names)).sort();
}

function arraysEqual(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

export function ToolsEditor({ serverName, savedTools, serverTools }: ToolsEditorProps) {
  const tools = useStackStore((s) => s.tools);

  // Every tool the editor should render. The server's own advertised list
  // (from /api/status) is the authoritative source — it holds every tool
  // regardless of code mode. The global tools store provides descriptions
  // when available. A saved whitelist entry missing from both still renders
  // so the user sees what's actually persisted in the YAML.
  const allTools: ToolRow[] = useMemo(() => {
    const prefix = `${serverName}${TOOL_NAME_DELIMITER}`;
    const descriptions = new Map<string, string | undefined>();
    for (const t of tools ?? []) {
      if (t.name.startsWith(prefix)) {
        descriptions.set(t.name.slice(prefix.length), t.description);
      }
    }
    const rows = new Map<string, ToolRow>();
    for (const name of serverTools ?? []) {
      rows.set(name, { name, description: descriptions.get(name) });
    }
    for (const [name, description] of descriptions) {
      if (!rows.has(name)) rows.set(name, { name, description });
    }
    for (const name of savedTools) {
      if (!rows.has(name)) rows.set(name, { name });
    }
    return [...rows.values()];
  }, [tools, serverName, savedTools, serverTools]);

  // Saved selection in canonical form. When savedTools is empty, the YAML
  // has no whitelist, so the server is exposing every tool — we reflect that
  // by checking every row.
  const savedSelection = useMemo(() => {
    if (savedTools.length === 0) {
      return canonicalWhitelist(allTools.map((t) => t.name));
    }
    return canonicalWhitelist(savedTools);
  }, [savedTools, allTools]);

  const [selection, setSelection] = useState<string[]>(savedSelection);
  const [query, setQuery] = useState('');
  const [isSaving, setIsSaving] = useState(false);
  const [conflict, setConflict] = useState<string | null>(null);
  // When the user tries to switch nodes with unsaved edits, we stash the name
  // of the server we were editing so the "Keep editing" affordance can re-
  // select it in the graph store.
  const [discardPrompt, setDiscardPrompt] = useState<string | null>(null);

  // Reset local selection when the server switches or when the saved state
  // changes and the user has no pending edits. The ref captures the
  // most-recent committed serverName so polling-driven re-renders don't
  // clobber the user's in-progress edits.
  const committedServer = useRef(serverName);
  const savedRef = useRef(savedSelection);
  const selectionRef = useRef(selection);
  selectionRef.current = selection;

  useEffect(() => {
    const prevServer = committedServer.current;
    const prevSaved = savedRef.current;
    const currentCanonical = canonicalWhitelist(selectionRef.current);
    const isDirty = !arraysEqual(currentCanonical, prevSaved);

    if (prevServer !== serverName && isDirty) {
      // Node switched out from under a dirty editor. Freeze the incoming
      // render and surface a confirm dialog — until the user decides we keep
      // the previous selection in view.
      setDiscardPrompt(prevServer);
      return;
    }

    committedServer.current = serverName;
    savedRef.current = savedSelection;
    if (!isDirty || prevServer !== serverName) {
      setSelection(savedSelection);
    }
    // Intentionally depend on the tuple of (serverName, canonicalized
    // savedSelection) — polling that reshuffles the underlying tools array
    // without changing membership must not reset state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverName, savedSelection.join('\u0000')]);

  const fuse = useMemo(
    () => new Fuse(allTools, { keys: ['name', 'description'], threshold: 0.4 }),
    [allTools],
  );

  const visible = useMemo(() => {
    if (!query.trim()) return allTools;
    return fuse.search(query).map((r) => r.item);
  }, [fuse, query, allTools]);

  const selected = useMemo(() => new Set(selection), [selection]);
  const canonicalSelection = useMemo(() => canonicalWhitelist(selection), [selection]);
  const dirty = !arraysEqual(canonicalSelection, savedSelection);
  const diffCount = useMemo(() => {
    const saved = new Set(savedSelection);
    let count = 0;
    for (const name of canonicalSelection) if (!saved.has(name)) count++;
    for (const name of savedSelection) if (!selected.has(name)) count++;
    return count;
  }, [canonicalSelection, savedSelection, selected]);

  const toggle = (name: string) => {
    const next = new Set(selected);
    if (next.has(name)) next.delete(name);
    else next.add(name);
    setSelection([...next]);
  };

  const selectAll = () => setSelection(allTools.map((t) => t.name));
  const clearAll = () => setSelection([]);

  const handleSave = async () => {
    setIsSaving(true);
    setConflict(null);
    // Empty whitelist means "expose all tools" in stack YAML semantics. We
    // send an empty array when the user has selected every known tool so
    // the stack file stays clean of a redundant full-list whitelist.
    const wire =
      canonicalSelection.length === allTools.length && allTools.length > 0
        ? []
        : canonicalSelection;
    try {
      const resp = await setServerTools(serverName, wire);
      showToast('success', `Tools saved for ${serverName}`);
      if (resp.reloaded === false) {
        showToast(
          'warning',
          'Stack updated. Run "gridctl reload" or restart with --watch to apply.',
        );
      }
      // Refresh the global caches so the sidebar reflects the now-filtered
      // tool set. Best-effort; we've already persisted the write.
      try {
        const [status, toolsList] = await Promise.all([fetchStatus(), fetchTools()]);
        useStackStore.getState().setGatewayStatus(status);
        useStackStore.getState().setTools(toolsList.tools);
      } catch {
        /* ignore refresh failures — the page will re-poll shortly */
      }
    } catch (err) {
      if (err instanceof AuthError) {
        throw err;
      }
      if (err instanceof SetServerToolsError) {
        switch (err.code) {
          case 'stack_modified':
            setConflict(err.hint || err.message);
            return;
          case 'reload_failed':
            showToast(
              'error',
              `Tools saved for ${serverName}, but reload failed: ${err.message}. Check gridctl logs.`,
            );
            // The save persisted. Refetch so the editor shows the new
            // on-disk state as the clean baseline; the hot reload can be
            // re-attempted via the Reload button elsewhere in the UI.
            try {
              const [status, toolsList] = await Promise.all([fetchStatus(), fetchTools()]);
              useStackStore.getState().setGatewayStatus(status);
              useStackStore.getState().setTools(toolsList.tools);
            } catch {
              /* ignore refresh failures */
            }
            return;
          case 'unknown_tool':
            showToast('error', err.message);
            return;
          default:
            showToast('error', err.message);
            return;
        }
      }
      const msg = err instanceof Error ? err.message : 'Save failed';
      showToast('error', msg);
    } finally {
      setIsSaving(false);
    }
  };

  // handleReloadFromDisk refreshes the saved state after a 409. We refetch
  // gateway status (which carries the running whitelist) and let the parent
  // re-render the editor with the new savedTools prop; the user's in-flight
  // selection is kept on top of the refreshed state.
  const handleReloadFromDisk = async () => {
    setConflict(null);
    try {
      const [status, toolsList] = await Promise.all([fetchStatus(), fetchTools()]);
      useStackStore.getState().setGatewayStatus(status);
      useStackStore.getState().setTools(toolsList.tools);
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'Refresh failed';
      showToast('error', msg);
    }
  };

  const handleDiscard = () => {
    // User accepted the node switch. Adopt the incoming prop shape by
    // re-initialising selection to the new savedSelection.
    committedServer.current = serverName;
    savedRef.current = savedSelection;
    setSelection(savedSelection);
    setDiscardPrompt(null);
  };

  const handleKeepEditing = () => {
    if (!discardPrompt) return;
    // Revert the graph's node selection back to the server we were editing.
    // The sidebar re-renders with the previous node's data and our local
    // selection is still the in-flight edit.
    useStackStore.getState().selectNode(`mcp-${discardPrompt}`);
    setDiscardPrompt(null);
  };

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
