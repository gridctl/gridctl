import { useMemo, useState } from 'react';
import { AlertCircle, Check, Loader2, RefreshCw, Save, ShieldCheck, Users } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';
import { useClientScopeEditor } from '../../hooks/useClientScopeEditor';
import { fetchClients, fetchStatus } from '../../lib/api';
import { Modal } from '../ui/Modal';
import type { ClientStatus, MCPServerStatus } from '../../types';

interface ClientAccessEditorProps {
  isOpen: boolean;
  onClose: () => void;
  servers: MCPServerStatus[];
  // Optional client slug to focus when the editor opens (e.g. from the Stack
  // inspector's "Edit Scope"). When omitted, the first linked client is shown.
  initialSlug?: string | null;
}

// ClientAccessEditor is the per-client access surface for the Tools workspace.
// It lists linked clients, and for the selected client edits which MCP servers
// that client may reach, persisting a server-level profile to the stack
// `clients:` block. Tool-level allow-lists are enforced (PR3) and editable in
// stack.yaml directly; this editor manages the coarser server-level access that
// the Stack view reflects.
export function ClientAccessEditor({ isOpen, onClose, servers, initialSlug }: ClientAccessEditorProps) {
  const clients = useStackStore((s) => s.clients);
  const linked = useMemo(
    () => clients.filter((c) => c.linked).sort((a, b) => a.name.localeCompare(b.name)),
    [clients],
  );

  // Seeded from `initialSlug` at mount. Callers that open the editor focused on
  // a specific client (the Stack inspector) pass a `key` keyed to that slug so
  // a fresh seed remounts here; the Tools entry passes no seed and starts on the
  // first linked client.
  const [activeSlug, setActiveSlug] = useState<string | null>(initialSlug ?? null);
  const activeClient = useMemo(
    () => linked.find((c) => c.slug === activeSlug) ?? linked[0] ?? null,
    [linked, activeSlug],
  );

  return (
    <Modal isOpen={isOpen} onClose={onClose} title="Client access" size="wide">
      <div className="flex h-[60vh] min-h-[360px]">
        <ClientList
          clients={linked}
          activeSlug={activeClient?.slug ?? null}
          onSelect={setActiveSlug}
        />
        <div className="flex-1 min-w-0 overflow-y-auto scrollbar-dark">
          {activeClient ? (
            <ClientScopePane key={activeClient.slug} client={activeClient} servers={servers} />
          ) : (
            <EmptyClients />
          )}
        </div>
      </div>
    </Modal>
  );
}

function ClientList({
  clients,
  activeSlug,
  onSelect,
}: {
  clients: ClientStatus[];
  activeSlug: string | null;
  onSelect: (slug: string) => void;
}) {
  return (
    <aside className="w-56 flex-shrink-0 border-r border-border-subtle overflow-y-auto scrollbar-dark py-2 pr-2">
      <div className="px-3 pb-2 text-[10px] font-medium text-text-muted/60 uppercase tracking-[0.3em]">
        clients
      </div>
      {clients.map((c) => {
        const scoped = c.effectiveScope?.configured && !c.effectiveScope.unscoped;
        return (
          <button
            key={c.slug}
            onClick={() => onSelect(c.slug)}
            aria-current={c.slug === activeSlug}
            className={cn(
              'w-full flex items-center gap-2 px-3 py-1.5 rounded-md text-left transition-colors',
              c.slug === activeSlug
                ? 'bg-primary/10 text-primary'
                : 'text-text-secondary hover:bg-surface-highlight/50 hover:text-text-primary',
            )}
          >
            <span className="flex-1 min-w-0 text-xs truncate">{c.name}</span>
            <span
              className={cn(
                'flex-shrink-0 text-[9px] font-mono px-1.5 py-0.5 rounded',
                scoped ? 'bg-primary/15 text-primary' : 'bg-surface-elevated text-text-muted',
              )}
              title={scoped ? 'Scoped to specific servers' : 'Reaches all servers'}
            >
              {scoped ? `${c.effectiveScope?.servers.length ?? 0}` : 'all'}
            </span>
          </button>
        );
      })}
    </aside>
  );
}

function ClientScopePane({
  client,
  servers,
}: {
  client: ClientStatus;
  servers: MCPServerStatus[];
}) {
  const serverNames = useMemo(() => servers.map((s) => s.name), [servers]);
  const { selected, toggle, selectAll, clearAll, canSave, isSaving, conflict, createsBlock, save } =
    useClientScopeEditor(client, serverNames);

  const noneSelected = selected.size === 0;
  // This client has an operator-authored tool-level allow-list. The server-level
  // editor preserves it (it omits the tools axis on save); surface that so the
  // operator knows tool restrictions are managed in stack.yaml, not here.
  const hasToolScope = (client.effectiveScope?.tools.length ?? 0) > 0;

  async function handleReloadFromDisk() {
    try {
      const [clients, status] = await Promise.all([fetchClients(), fetchStatus()]);
      useStackStore.getState().setClients(clients);
      useStackStore.getState().setGatewayStatus(status);
    } catch {
      /* polling will catch up */
    }
  }

  return (
    <div className="px-5 py-4 space-y-3" aria-busy={isSaving}>
      <div className="flex items-center gap-2">
        <ShieldCheck size={14} className="text-primary/70" aria-hidden="true" />
        <h3 className="text-sm font-medium text-text-primary">{client.name}</h3>
        <span className="font-mono text-[10px] text-text-muted">{client.slug}</span>
      </div>
      <p className="text-[11px] text-text-muted leading-relaxed">
        Choose which MCP servers <span className="text-text-secondary">{client.name}</span> can
        reach. To deny a client entirely, leave it unlisted under a deny default
        rather than clearing every server here.
      </p>

      {hasToolScope && (
        <p className="text-[10px] text-text-muted/80 leading-relaxed">
          This client has a tool-level allow-list set in stack.yaml; it is preserved when you save
          server changes here.
        </p>
      )}

      {createsBlock && (
        <div className="flex items-start gap-2 rounded-md border border-status-pending/30 bg-status-pending/[0.06] px-3 py-2">
          <AlertCircle size={12} className="text-status-pending flex-shrink-0 mt-0.5" aria-hidden="true" />
          <p className="text-[11px] text-text-secondary leading-relaxed">
            No <span className="font-mono text-status-pending">clients</span> block exists yet.
            Saving creates one; clients you have not listed will then be{' '}
            <span className="font-medium">denied by default</span>.
          </p>
        </div>
      )}

      <div className="flex items-center gap-2 text-[11px] text-text-muted">
        <span>
          <span className="text-text-secondary font-medium">{selected.size}</span> of{' '}
          <span className="text-text-secondary font-medium">{serverNames.length}</span> servers
        </span>
        <div className="ml-auto flex items-center gap-2">
          <button
            type="button"
            onClick={selectAll}
            disabled={isSaving}
            className="text-[10px] text-secondary hover:text-secondary-light transition-colors disabled:opacity-50"
          >
            All
          </button>
          <span className="text-border" aria-hidden="true">·</span>
          <button
            type="button"
            onClick={clearAll}
            disabled={isSaving}
            className="text-[10px] text-secondary hover:text-secondary-light transition-colors disabled:opacity-50"
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
              onClick={() => toggle(name)}
              disabled={isSaving}
              className="w-full flex items-center gap-2.5 px-3 py-2 text-left hover:bg-surface-highlight/40 transition-colors disabled:opacity-60"
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
          Select at least one server to save.
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
              The stack file was modified outside the canvas.
            </p>
            <p className="text-[10px] text-text-muted">{conflict}</p>
            <button
              type="button"
              onClick={handleReloadFromDisk}
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
        onClick={save}
        disabled={!canSave || isSaving}
        aria-label={canSave ? 'Save client access and reload' : 'Saved'}
        className={cn(
          'w-full inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-[11px] font-medium transition-colors',
          canSave && !isSaving
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
            {canSave ? 'Save access & Reload' : 'Saved'}
          </>
        )}
      </button>
    </div>
  );
}

function EmptyClients() {
  return (
    <div className="h-full flex items-center justify-center px-6 py-12">
      <div className="max-w-xs text-center space-y-3">
        <div className="mx-auto w-12 h-12 rounded-2xl bg-primary/10 border border-primary/20 flex items-center justify-center">
          <Users size={22} className="text-primary/70" />
        </div>
        <p className="text-xs text-text-muted leading-relaxed">
          No linked clients yet. Run <span className="font-mono text-text-secondary">gridctl link</span>{' '}
          to connect a client, then set its access here.
        </p>
      </div>
    </div>
  );
}

export default ClientAccessEditor;
