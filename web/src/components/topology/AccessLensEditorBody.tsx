import { useMemo } from 'react';
import { AlertCircle, Check, RefreshCw, ShieldCheck } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';
import { useAccessLensStore } from '../../stores/useAccessLensStore';
import { fetchClients, fetchStatus } from '../../lib/api';
import type { MCPServerStatus } from '../../types';

interface AccessLensEditorBodyProps {
  servers: MCPServerStatus[];
}

// AccessLensEditorBody is the keyboard-driven twin of the canvas node toggling:
// a checkbox list bound to the SAME Topology-scoped draft store the canvas reads
// and writes, so checking a box here lights the node out there and vice versa.
// It deliberately reuses the visual language of the Tools ClientScopePane but
// sources its state from the lifted draft store, not useClientScopeEditor (which
// stays the controller for the standalone Tools modal).
export function AccessLensEditorBody({ servers }: AccessLensEditorBodyProps) {
  const clientName = useAccessLensStore((s) => s.clientName);
  const clientSlug = useAccessLensStore((s) => s.clientSlug);
  const draft = useAccessLensStore((s) => s.draft);
  const savedTools = useAccessLensStore((s) => s.savedTools);
  const createsBlock = useAccessLensStore((s) => s.createsBlock);
  const conflict = useAccessLensStore((s) => s.conflict);
  const toggleServer = useAccessLensStore((s) => s.toggleServer);
  const selectAll = useAccessLensStore((s) => s.selectAll);
  const clearAll = useAccessLensStore((s) => s.clearAll);
  const setConflict = useAccessLensStore((s) => s.setConflict);

  const serverNames = useMemo(() => servers.map((s) => s.name), [servers]);
  const selected = useMemo(() => new Set(draft), [draft]);
  const noneSelected = draft.length === 0;
  const hasToolScope = savedTools.length > 0;

  async function handleReloadFromDisk() {
    try {
      const [clients, status] = await Promise.all([fetchClients(), fetchStatus()]);
      useStackStore.getState().setClients(clients);
      useStackStore.getState().setGatewayStatus(status);
      setConflict(null);
    } catch {
      /* polling will catch up */
    }
  }

  return (
    <div className="px-4 py-3 space-y-3">
      <div className="flex items-center gap-2">
        <ShieldCheck size={14} className="text-primary/70" aria-hidden="true" />
        <h3 className="text-sm font-medium text-text-primary">{clientName}</h3>
        <span className="font-mono text-[10px] text-text-muted">{clientSlug}</span>
      </div>
      <p className="text-[11px] text-text-muted leading-relaxed">
        Toggle which MCP servers <span className="text-text-secondary">{clientName}</span> can reach.
        Edits stage in a draft — the canvas re-lights live, but nothing is written until you save.
      </p>

      {hasToolScope && (
        <p className="text-[10px] text-text-muted/80 leading-relaxed">
          This client has a tool-level allow-list in stack.yaml; it is preserved on save and still
          gates which tools are visible.
        </p>
      )}

      {createsBlock && (
        <div className="flex items-start gap-2 rounded-md border border-status-pending/30 bg-status-pending/[0.06] px-3 py-2">
          <AlertCircle size={12} className="text-status-pending flex-shrink-0 mt-0.5" aria-hidden="true" />
          <p className="text-[11px] text-text-secondary leading-relaxed">
            No <span className="font-mono text-status-pending">clients</span> block exists yet. Saving
            creates one; unlisted clients become <span className="font-medium">deny by default</span>.
          </p>
        </div>
      )}

      <div className="flex items-center gap-2 text-[11px] text-text-muted">
        <span>
          <span className="text-text-secondary font-medium">{draft.length}</span> of{' '}
          <span className="text-text-secondary font-medium">{serverNames.length}</span> servers
        </span>
        <div className="ml-auto flex items-center gap-2">
          <button
            type="button"
            onClick={() => selectAll(serverNames)}
            className="text-[10px] text-secondary hover:text-secondary-light transition-colors"
          >
            All
          </button>
          <span className="text-border" aria-hidden="true">·</span>
          <button
            type="button"
            onClick={clearAll}
            className="text-[10px] text-secondary hover:text-secondary-light transition-colors"
          >
            None
          </button>
        </div>
      </div>

      <div className="rounded-lg border border-border/40 bg-background/60 divide-y divide-border/20">
        {serverNames.length === 0 && (
          <p className="px-3 py-4 text-[11px] text-text-muted/60 italic text-center">
            No MCP servers in the active stack.
          </p>
        )}
        {serverNames.map((name) => {
          const isOn = selected.has(name);
          return (
            <button
              key={name}
              type="button"
              role="checkbox"
              aria-checked={isOn}
              onClick={() => toggleServer(name)}
              className="w-full flex items-center gap-2.5 px-3 py-2 text-left hover:bg-surface-highlight/40 transition-colors"
            >
              <span
                className={cn(
                  'w-3.5 h-3.5 rounded border flex items-center justify-center flex-shrink-0 transition-colors',
                  isOn ? 'bg-primary/20 border-primary/60' : 'border-border/60 bg-background/50',
                )}
              >
                {isOn && <Check size={10} className="text-primary" aria-hidden="true" />}
              </span>
              <span
                className={cn(
                  'text-xs font-mono truncate',
                  isOn ? 'text-text-primary' : 'text-text-secondary',
                )}
              >
                {name}
              </span>
            </button>
          );
        })}
      </div>

      {noneSelected && (
        <p className="text-[10px] text-status-pending" role="status">
          Select at least one server to save. An empty list means &ldquo;all servers&rdquo;, not
          &ldquo;deny&rdquo;.
        </p>
      )}

      {conflict && (
        <div
          role="alert"
          className="flex items-start gap-2 rounded-md border border-status-pending/40 bg-status-pending/[0.05] px-3 py-2"
        >
          <AlertCircle size={12} className="text-status-pending flex-shrink-0 mt-0.5" />
          <div className="flex-1 min-w-0 space-y-1">
            <p className="text-[11px] text-status-pending font-medium">
              The stack file changed on disk.
            </p>
            <p className="text-[10px] text-text-muted">{conflict}</p>
            <button
              type="button"
              onClick={handleReloadFromDisk}
              className="inline-flex items-center gap-1 text-[10px] text-secondary hover:text-secondary-light transition-colors"
            >
              <RefreshCw size={10} />
              Reload file (keeps your draft)
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

export default AccessLensEditorBody;
