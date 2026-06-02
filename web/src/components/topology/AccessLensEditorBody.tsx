import { useMemo, useState } from 'react';
import { AlertCircle, Check, Copy, RefreshCw, ShieldCheck } from 'lucide-react';
import { cn } from '../../lib/cn';
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import { useStackStore } from '../../stores/useStackStore';
import { useAccessLensStore, flattenTools } from '../../stores/useAccessLensStore';
import { fetchClients, fetchStatus } from '../../lib/api';
import type { MCPServerStatus } from '../../types';
import { ServerToolScopeGroup, type ScopeTool } from './ServerToolScopeGroup';

interface AccessLensEditorBodyProps {
  servers: MCPServerStatus[];
}

// AccessLensEditorBody is the keyboard-driven twin of the canvas node toggling:
// a checkbox list bound to the SAME Topology-scoped draft store the canvas reads
// and writes, so checking a box here lights the node out there and vice versa.
// Each granted server also carries a per-client tool-exposure control (All vs a
// custom subset); the canvas stays server-level, so this slide-over is where
// tool narrowing happens.
export function AccessLensEditorBody({ servers }: AccessLensEditorBodyProps) {
  const clientName = useAccessLensStore((s) => s.clientName);
  const clientSlug = useAccessLensStore((s) => s.clientSlug);
  const draft = useAccessLensStore((s) => s.draft);
  const createsBlock = useAccessLensStore((s) => s.createsBlock);
  const conflict = useAccessLensStore((s) => s.conflict);
  const toggleServer = useAccessLensStore((s) => s.toggleServer);
  const selectAll = useAccessLensStore((s) => s.selectAll);
  const clearAll = useAccessLensStore((s) => s.clearAll);
  const setConflict = useAccessLensStore((s) => s.setConflict);
  const toolMode = useAccessLensStore((s) => s.toolMode);
  const customSel = useAccessLensStore((s) => s.customSel);
  const setServerToolMode = useAccessLensStore((s) => s.setServerToolMode);
  const toggleTool = useAccessLensStore((s) => s.toggleTool);
  const selectAllTools = useAccessLensStore((s) => s.selectAllTools);
  const clearTools = useAccessLensStore((s) => s.clearTools);
  const toolCatalog = useStackStore((s) => s.toolCatalog);

  const serverNames = useMemo(() => servers.map((s) => s.name), [servers]);
  const serverToolMap = useMemo(
    () => Object.fromEntries(servers.map((s) => [s.name, s.tools ?? []])),
    [servers],
  );
  const selected = useMemo(() => new Set(draft), [draft]);
  const noneSelected = draft.length === 0;

  // Descriptions are resolved from the globally-polled tool catalog by prefixed
  // name (the catalog stays full even in code mode, unlike /api/tools).
  const descriptionOf = useMemo(() => {
    const m = new Map<string, string | undefined>();
    for (const t of toolCatalog) m.set(t.name, t.description);
    return m;
  }, [toolCatalog]);

  const availableToolsFor = (server: string): ScopeTool[] =>
    (serverToolMap[server] ?? []).map((name) => ({
      name,
      description: descriptionOf.get(`${server}${TOOL_NAME_DELIMITER}${name}`),
    }));

  // The exact prefixed allow-list a commit would write under the current draft —
  // shown so operators migrating from hand-edited stack.yaml can read and copy it.
  const rawAllowList = useMemo(
    () => flattenTools(draft, serverToolMap, toolMode, customSel),
    [draft, serverToolMap, toolMode, customSel],
  );

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
        Toggle which MCP servers <span className="text-text-secondary">{clientName}</span> can reach,
        and narrow a granted server to specific tools. Edits stage in a draft — the canvas re-lights
        live, but nothing is written until you save.
      </p>

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
          const mode = toolMode[name] ?? 'all';
          const scoped = isOn && mode === 'custom';
          return (
            <div key={name}>
              <button
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
                {scoped && (
                  <span className="ml-auto flex-shrink-0 text-[9px] font-mono px-1.5 py-0.5 rounded bg-primary/15 text-primary">
                    scoped
                  </span>
                )}
              </button>

              {isOn && (
                <ServerToolScopeGroup
                  serverName={name}
                  availableTools={availableToolsFor(name)}
                  mode={mode}
                  selected={new Set(customSel[name] ?? [])}
                  onModeChange={(m) => setServerToolMode(name, m)}
                  onToggleTool={(tool) => toggleTool(name, tool)}
                  onSelectAll={(visible) => selectAllTools(name, visible)}
                  onClear={(visible) => clearTools(name, visible)}
                />
              )}
            </div>
          );
        })}
      </div>

      {noneSelected && (
        <p className="text-[10px] text-status-pending" role="status">
          Select at least one server to save. An empty list means &ldquo;all servers&rdquo;, not
          &ldquo;deny&rdquo;.
        </p>
      )}

      {rawAllowList.length > 0 && <RawAllowList tools={rawAllowList} />}

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

// RawAllowList surfaces the exact prefixed tools: entries a commit would write,
// for operators who manage stack.yaml directly. Collapsed by default.
function RawAllowList({ tools }: { tools: string[] }) {
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState(false);

  async function copy() {
    try {
      await navigator.clipboard.writeText(tools.join('\n'));
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard may be unavailable; ignore */
    }
  }

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          aria-expanded={open}
          className="text-[10px] text-secondary hover:text-secondary-light transition-colors"
        >
          {open ? 'Hide' : 'View'} raw tools allow-list ({tools.length})
        </button>
        {open && (
          <button
            type="button"
            onClick={copy}
            className="inline-flex items-center gap-1 text-[10px] text-text-muted hover:text-text-secondary transition-colors"
          >
            {copied ? <Check size={10} className="text-status-running" /> : <Copy size={10} />}
            {copied ? 'Copied' : 'Copy'}
          </button>
        )}
      </div>
      {open && (
        <pre className="max-h-40 overflow-auto scrollbar-dark rounded-md border border-border/40 bg-background/70 px-3 py-2 text-[10px] font-mono leading-relaxed text-text-secondary">
          {tools.join('\n')}
        </pre>
      )}
    </div>
  );
}

export default AccessLensEditorBody;
