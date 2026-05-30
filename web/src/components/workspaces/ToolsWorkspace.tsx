import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Command } from 'cmdk';
import {
  Activity,
  AlertCircle,
  Check,
  ChevronRight,
  Layers,
  Loader2,
  RefreshCw,
  Save,
  Search,
  Users,
  Wrench,
  X,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { useToolsEditor } from '../../hooks/useToolsEditor';
import { useToolUsage } from '../../hooks/useToolUsage';
import { useFuzzySearch } from '../../hooks/useFuzzySearch';
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import { formatRelativeTime } from '../../lib/time';
import {
  AUDIT_WINDOWS,
  DEFAULT_AUDIT_WINDOW,
  auditWindowMs,
  classifyTool,
  formatLastUsed,
  unusedEnabledTools,
  type AuditState,
  type AuditWindow,
} from '../../lib/toolAudit';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { StatusDot } from '../ui/StatusDot';
import { FleetActions } from './FleetActions';
import { ClientAccessEditor } from './ClientAccessEditor';
import { ToolDetailPanel } from './ToolDetailPanel';
import type { MCPServerStatus, NodeStatus, Tool, ToolUsageStat } from '../../types';

// Per-state styling for Audit Mode dots/labels, keyed to the shared status
// color tokens: used→running (green), unused→pending (amber), disabled→muted.
const AUDIT_STYLES: Record<AuditState, { dot: string; text: string; label: string }> = {
  used: { dot: 'bg-status-running', text: 'text-status-running', label: 'used' },
  unused: { dot: 'bg-status-pending', text: 'text-status-pending', label: 'unused' },
  disabled: { dot: 'bg-text-muted/50', text: 'text-text-muted', label: 'disabled' },
};

// ToolsWorkspace is the fleet-wide tool-management surface, sibling to
// Topology, Library, and Variables. The left rail lists every MCP server with
// an enabled/total badge; the main pane edits the selected server's whitelist
// (reusing useToolsEditor) and previews each tool's input schema. A header
// search spans every server's tools at once.
//
// The editor controller is owned here (not inside a child) so the workspace
// can guard server switches against unsaved edits before the URL changes —
// the sidebar's topology-based discard flow doesn't map onto URL selection.
export function ToolsWorkspace() {
  const [searchParams, setSearchParams] = useSearchParams();
  const compact = useUIStore((s) => s.compactMode.tools);

  const mcpServers = useStackStore((s) => s.mcpServers);
  // The Tools workspace sources per-tool detail from the catalog (raw
  // descriptions + schemas, populated even in code mode), not the MCP-facing
  // `tools` list which is just the meta-tools when code mode is on.
  const toolCatalog = useStackStore((s) => s.toolCatalog);
  const isLoading = useStackStore((s) => s.isLoading);

  // Servers sorted by name for a stable rail order.
  const servers = useMemo(
    () => [...mcpServers].sort((a, b) => a.name.localeCompare(b.name)),
    [mcpServers],
  );

  // ---- URL state ----------------------------------------------------------
  const serverParam = searchParams.get('server') ?? '';
  const globalQuery = searchParams.get('q') ?? '';

  // The active server is the URL's ?server= when it names a real server,
  // otherwise the first server in the list.
  const activeServerName = useMemo(() => {
    if (servers.some((s) => s.name === serverParam)) return serverParam;
    return servers[0]?.name ?? '';
  }, [servers, serverParam]);

  const activeServer = useMemo(
    () => servers.find((s) => s.name === activeServerName) ?? null,
    [servers, activeServerName],
  );

  // Selecting a server also exits any active global search — done in one
  // params update so the two changes compose (two separate setSearchParams
  // calls in a handler both observe the same stale snapshot and the last wins).
  const applyServer = useCallback(
    (name: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set('server', name);
          next.delete('q');
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const setGlobalQuery = useCallback(
    (q: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (q.trim()) next.set('q', q);
          else next.delete('q');
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // ---- Editor controller (single instance, owned here) --------------------
  const editor = useToolsEditor(
    activeServerName,
    activeServer?.toolWhitelist ?? [],
    activeServer?.tools ?? [],
  );

  // ---- Audit Mode ---------------------------------------------------------
  // An overlay that classifies each tool as used / configured-but-unused /
  // disabled against a lookback window. Usage is fetched only while the mode
  // is on (the hook idles otherwise) so the editor pays nothing when it's off.
  const [auditMode, setAuditMode] = useState(false);
  const [auditWindow, setAuditWindow] = useState<AuditWindow>(DEFAULT_AUDIT_WINDOW);
  // Fleet bulk-action panel (expose-all / hide-pattern across servers).
  const [fleetOpen, setFleetOpen] = useState(false);
  // Per-client access editor (which servers each client may reach).
  const [accessOpen, setAccessOpen] = useState(false);
  const { usage, fetchedAt } = useToolUsage(auditMode);
  const windowMs = auditWindowMs(auditWindow);
  const usageByServer = usage?.servers;

  // Per-server count of exposed-but-unused tools, for the rail badge. "now" is
  // the fetch time from the hook (not a render-time clock read) so the memo
  // stays pure and only recomputes when usage/window changes.
  const unusedByServer = useMemo(() => {
    const out: Record<string, number> = {};
    if (!auditMode || !usageByServer || fetchedAt == null) return out;
    for (const s of servers) {
      out[s.name] = unusedEnabledTools(s, usageByServer[s.name], windowMs, fetchedAt).length;
    }
    return out;
  }, [auditMode, usageByServer, servers, windowMs, fetchedAt]);

  // ---- Server-switch guard ------------------------------------------------
  // A pending target set while the active editor has unsaved edits. The switch
  // commits (URL change) only after the user discards.
  const [pendingServer, setPendingServer] = useState<string | null>(null);
  // The tool whose detail is shown in the right rail. Distinct from the
  // whitelist selection (the checkboxes) — this is the "active" tool. Picking a
  // global-search result sets it so that tool is revealed on arrival.
  const [selectedTool, setSelectedTool] = useState<string | null>(null);

  // Switch to a server (or reveal a tool on the active one). Unsaved edits on
  // the current server route through a confirm dialog before the switch
  // commits. Plain functions — the React compiler memoizes them.
  function requestServer(name: string, tool?: string) {
    if (name !== activeServerName && editor.dirty) {
      setPendingServer(name);
      setSelectedTool(tool ?? null);
      return;
    }
    if (name !== activeServerName) applyServer(name);
    // Clear any active filter so a revealed tool is visible, then select it.
    editor.setQuery('');
    setSelectedTool(tool ?? null);
  }

  function confirmSwitch() {
    if (!pendingServer) return;
    // Reset the editor to the current server's saved state first so the
    // upcoming serverName change is a clean transition (no spurious prompt).
    editor.handleDiscard();
    applyServer(pendingServer);
    editor.setQuery('');
    setPendingServer(null);
  }

  // ---- Global search ------------------------------------------------------
  const searchActive = globalQuery.trim().length > 0;
  const globalMatches = useFuzzySearch(toolCatalog, globalQuery);
  const globalResults = useMemo(
    () => globalMatches.map(splitTool).filter((r): r is GlobalResult => r !== null),
    [globalMatches],
  );

  // ---- Selected-tool detail (right rail) ----------------------------------
  // The active tool's row (name + description), schema, whitelist state, and
  // audit classification — all derived here so the panel can live in the shell
  // right rail (a sibling of the center pane).
  const selectedRow = useMemo(
    () => (selectedTool ? editor.allTools.find((t) => t.name === selectedTool) ?? null : null),
    [selectedTool, editor.allTools],
  );
  const selectedSchema = useMemo(() => {
    if (!selectedTool || !activeServerName) return undefined;
    const prefixed = `${activeServerName}${TOOL_NAME_DELIMITER}${selectedTool}`;
    return toolCatalog.find((t) => t.name === prefixed)?.inputSchema;
  }, [selectedTool, activeServerName, toolCatalog]);
  const selectedEnabled = selectedTool ? editor.selected.has(selectedTool) : false;
  const selectedLastCalled = selectedTool
    ? usageByServer?.[activeServerName]?.[selectedTool]?.lastCalledAt
    : undefined;
  const selectedAuditState = useMemo(() => {
    if (!auditMode || !selectedTool || fetchedAt == null) return null;
    return classifyTool(selectedEnabled, selectedLastCalled, windowMs, fetchedAt);
  }, [auditMode, selectedTool, selectedEnabled, selectedLastCalled, windowMs, fetchedAt]);

  const leftRail = (
    <ServerRail
      compact={compact}
      servers={servers}
      activeServerName={activeServerName}
      onSelectServer={(name) => requestServer(name)}
      auditMode={auditMode}
      unusedByServer={unusedByServer}
    />
  );

  const rightRail = (
    <ToolDetailPanel
      serverName={activeServerName}
      tool={selectedRow}
      schema={selectedSchema}
      enabled={selectedEnabled}
      auditMode={auditMode}
      auditState={selectedAuditState}
      lastCalledAt={selectedLastCalled}
      onClose={() => setSelectedTool(null)}
    />
  );

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell
        workspace="tools"
        defaultLeftPct={20}
        defaultRightPct={30}
        left={leftRail}
        right={rightRail}
        minLeftPx={220}
        minRightPx={300}
      >
        <main className="flex flex-col h-full overflow-hidden">
          <ToolsHeader
            compact={compact}
            serverCount={servers.length}
            query={globalQuery}
            onQueryChange={setGlobalQuery}
            auditMode={auditMode}
            onToggleAudit={() => setAuditMode((v) => !v)}
            auditWindow={auditWindow}
            onWindowChange={setAuditWindow}
            observedSince={usage?.observedSince}
            onOpenFleet={() => setFleetOpen(true)}
            fleetDisabled={servers.length === 0}
            onOpenAccess={() => setAccessOpen(true)}
          />

          <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark">
            {servers.length === 0 ? (
              <ToolsEmptyState loading={isLoading} />
            ) : searchActive ? (
              <GlobalResults
                results={globalResults}
                query={globalQuery}
                serverCount={servers.length}
                onPick={(server, tool) => requestServer(server, tool)}
              />
            ) : activeServer ? (
              <ServerDetail
                key={activeServer.name}
                server={activeServer}
                editor={editor}
                selectedTool={selectedTool}
                onSelect={setSelectedTool}
                auditMode={auditMode}
                usage={usageByServer?.[activeServer.name]}
                windowMs={windowMs}
                now={fetchedAt}
              />
            ) : null}
          </div>
        </main>
      </WorkspaceShell>

      <ConfirmDialog
        isOpen={pendingServer !== null}
        onClose={() => setPendingServer(null)}
        onConfirm={confirmSwitch}
        title="Discard unsaved changes"
        message={
          <p>
            You have unsaved tool changes for{' '}
            <span className="font-mono text-primary">{activeServerName}</span>. Switch
            to{' '}
            <span className="font-mono text-primary">{pendingServer}</span> and discard
            them?
          </p>
        }
        confirmLabel="Discard & switch"
        variant="danger"
      />

      <FleetActions
        isOpen={fleetOpen}
        onClose={() => setFleetOpen(false)}
        servers={servers}
        activeServerName={activeServerName}
      />

      <ClientAccessEditor
        isOpen={accessOpen}
        onClose={() => setAccessOpen(false)}
        servers={servers}
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

interface ToolsHeaderProps {
  compact: boolean;
  serverCount: number;
  query: string;
  onQueryChange: (q: string) => void;
  auditMode: boolean;
  onToggleAudit: () => void;
  auditWindow: AuditWindow;
  onWindowChange: (w: AuditWindow) => void;
  observedSince?: string;
  onOpenFleet: () => void;
  fleetDisabled: boolean;
  onOpenAccess: () => void;
}

function ToolsHeader({
  compact,
  serverCount,
  query,
  onQueryChange,
  auditMode,
  onToggleAudit,
  auditWindow,
  onWindowChange,
  observedSince,
  onOpenFleet,
  fleetDisabled,
  onOpenAccess,
}: ToolsHeaderProps) {
  const searching = query.trim().length > 0;
  const windowLabel = AUDIT_WINDOWS.find((w) => w.id === auditWindow)?.label ?? '7 days';
  return (
    <header
      className={cn(
        'flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle px-6 flex flex-col gap-2',
        compact ? 'py-2' : 'py-3',
      )}
    >
      <div className="flex items-center gap-3">
        <div className="font-sans text-text-muted/60 text-[10px] uppercase tracking-[0.4em]">
          tools
        </div>
        <div className="font-mono text-[10px] text-text-muted">
          {serverCount} {serverCount === 1 ? 'server' : 'servers'}
        </div>

        <div className="ml-auto flex items-center gap-2">
          <button
            type="button"
            onClick={onOpenAccess}
            aria-label="Open per-client access editor"
            className={cn(
              'inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-[10px] font-medium border transition-colors',
              'bg-primary/10 text-primary border-primary/30 hover:bg-primary/20 hover:border-primary/50',
            )}
          >
            <Users size={11} aria-hidden="true" />
            Access
          </button>
          <button
            type="button"
            onClick={onOpenFleet}
            disabled={fleetDisabled}
            aria-label="Open fleet tool actions"
            className={cn(
              'inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-[10px] font-medium border transition-colors',
              'bg-background/40 text-text-muted border-border/40 hover:text-text-secondary hover:border-border',
              fleetDisabled && 'opacity-40 cursor-not-allowed',
            )}
          >
            <Layers size={11} aria-hidden="true" />
            Fleet
          </button>
          <button
            type="button"
            onClick={onToggleAudit}
            aria-pressed={auditMode}
            aria-label="Toggle audit mode"
            className={cn(
              'inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-[10px] font-medium border transition-colors',
              auditMode
                ? 'bg-primary/15 text-primary border-primary/40'
                : 'bg-background/40 text-text-muted border-border/40 hover:text-text-secondary hover:border-border',
            )}
          >
            <Activity size={11} aria-hidden="true" />
            Audit
          </button>
          {auditMode && (
            <label className="inline-flex items-center gap-1.5 text-[10px] text-text-muted">
              <span className="sr-only">Audit lookback window</span>
              <select
                value={auditWindow}
                onChange={(e) => onWindowChange(e.target.value as AuditWindow)}
                aria-label="Audit lookback window"
                className="bg-background/60 border border-border/40 rounded-md px-1.5 py-1 text-[10px] text-text-secondary focus:outline-none focus:border-primary/50"
              >
                {AUDIT_WINDOWS.map((w) => (
                  <option key={w.id} value={w.id}>
                    {w.label}
                  </option>
                ))}
              </select>
            </label>
          )}
        </div>
      </div>

      {auditMode && (
        <p className="text-[10px] text-text-muted/80 leading-relaxed" role="status">
          <span className="text-status-running">●</span> used ·{' '}
          <span className="text-status-pending">●</span> unused ·{' '}
          <span className="text-text-muted">●</span> disabled — &ldquo;unused&rdquo; means no
          recorded calls in the last {windowLabel}.
          {observedSince
            ? ` Tracking since ${formatRelativeTime(new Date(observedSince))}; tools with no recorded calls may predate it.`
            : ' Counts cover activity since the last gateway restart.'}
        </p>
      )}

      <div className="relative">
        <Search
          size={13}
          className="absolute left-3 top-1/2 -translate-y-1/2 text-text-muted/50 pointer-events-none"
        />
        <input
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          placeholder={
            serverCount === 1
              ? 'Search all tools…'
              : `Search tools across all ${serverCount} servers…`
          }
          aria-label="Search tools across all servers"
          className="w-full bg-background/60 border border-border/40 rounded-lg pl-9 pr-8 py-2 text-sm text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50 transition-colors"
        />
        {searching && (
          <button
            onClick={() => onQueryChange('')}
            aria-label="Clear search"
            className="absolute right-2.5 top-1/2 -translate-y-1/2 p-0.5 rounded hover:bg-surface-highlight transition-colors"
          >
            <X size={13} className="text-text-muted" />
          </button>
        )}
      </div>
    </header>
  );
}

// ---------------------------------------------------------------------------
// Left rail
// ---------------------------------------------------------------------------

interface ServerRailProps {
  compact: boolean;
  servers: MCPServerStatus[];
  activeServerName: string;
  onSelectServer: (name: string) => void;
  auditMode: boolean;
  unusedByServer: Record<string, number>;
}

function ServerRail({
  compact,
  servers,
  activeServerName,
  onSelectServer,
  auditMode,
  unusedByServer,
}: ServerRailProps) {
  return (
    <aside className="h-full flex flex-col bg-surface/40 backdrop-blur-sm border-r border-border-subtle">
      <div
        className={cn(
          'flex-shrink-0 px-3 border-b border-border-subtle/60',
          compact ? 'py-2' : 'py-3',
        )}
      >
        <div className="text-[10px] font-medium text-text-muted/60 uppercase tracking-[0.3em]">
          servers
        </div>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-2 py-2 space-y-0.5">
        {servers.map((server) => (
          <ServerPill
            key={server.name}
            server={server}
            active={server.name === activeServerName}
            onClick={() => onSelectServer(server.name)}
            auditMode={auditMode}
            unusedCount={unusedByServer[server.name] ?? 0}
          />
        ))}
        {servers.length === 0 && (
          <p className="px-3 py-2 text-[10px] text-text-muted/60 leading-relaxed">
            No MCP servers in the active stack.
          </p>
        )}
      </div>
    </aside>
  );
}

interface ServerPillProps {
  server: MCPServerStatus;
  active: boolean;
  onClick: () => void;
  auditMode: boolean;
  unusedCount: number;
}

function ServerPill({ server, active, onClick, auditMode, unusedCount }: ServerPillProps) {
  const { enabled, total } = toolCounts(server);
  const status = serverStatus(server);
  const showUnused = auditMode && unusedCount > 0;
  return (
    <button
      onClick={onClick}
      aria-current={active}
      className={cn(
        'group w-full flex items-center gap-2 px-3 py-1.5 rounded-md text-left transition-colors',
        active
          ? 'bg-primary/10 text-primary'
          : 'text-text-secondary hover:bg-surface-highlight/50 hover:text-text-primary',
      )}
    >
      <StatusDot status={status} size="sm" pulse={false} />
      <span className={cn('flex-1 min-w-0 text-xs font-mono truncate', active && 'text-primary')}>
        {server.name}
      </span>
      {showUnused && (
        <span
          className="flex-shrink-0 text-[10px] font-mono px-1.5 py-0.5 rounded tabular-nums bg-status-pending/15 text-status-pending"
          title={`${unusedCount} exposed ${unusedCount === 1 ? 'tool' : 'tools'} unused in the lookback window`}
        >
          {unusedCount} unused
        </span>
      )}
      <span
        className={cn(
          'flex-shrink-0 text-[10px] font-mono px-1.5 py-0.5 rounded tabular-nums',
          active ? 'bg-primary/15 text-primary' : 'bg-surface-elevated text-text-muted',
        )}
        title={`${enabled} of ${total} tools enabled`}
      >
        {enabled}/{total}
      </span>
    </button>
  );
}

// ---------------------------------------------------------------------------
// Detail pane — per-server editor + schema previews (driven by useToolsEditor)
// ---------------------------------------------------------------------------

interface ServerDetailProps {
  server: MCPServerStatus;
  editor: ReturnType<typeof useToolsEditor>;
  // The active tool whose detail shows in the right rail (controlled by the
  // workspace so a global-search reveal can target it). null = none selected.
  // Distinct from the editor's whitelist selection (the checkboxes).
  selectedTool: string | null;
  onSelect: (tool: string) => void;
  auditMode: boolean;
  // This server's per-tool usage map (from GET /api/tools/usage), or undefined
  // when usage hasn't loaded / Audit Mode is off.
  usage: Record<string, ToolUsageStat> | undefined;
  windowMs: number;
  // Fetch time used as "now" for audit classification (null until loaded).
  now: number | null;
}

function ServerDetail({
  server,
  editor,
  selectedTool,
  onSelect,
  auditMode,
  usage,
  windowMs,
  now,
}: ServerDetailProps) {
  const {
    allTools: rows,
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
    handleSave,
    disableTools,
    handleReloadFromDisk,
  } = editor;

  // Audit state per tool row, computed once per usage/selection/window change.
  // Captures `now` inside the memo so the row render stays pure (no clock read
  // during render). Empty when Audit Mode is off.
  const auditByTool = useMemo(() => {
    const map = new Map<string, AuditState>();
    if (!auditMode || now == null) return map;
    for (const row of rows) {
      map.set(row.name, classifyTool(selected.has(row.name), usage?.[row.name]?.lastCalledAt, windowMs, now));
    }
    return map;
  }, [auditMode, rows, selected, usage, windowMs, now]);

  // Tools currently exposed (selected) but with no activity in the window —
  // the "disable unused" remediation target. Derived from auditByTool so the
  // banner and the per-row dots never disagree.
  const unusedSelected = useMemo(() => {
    const out: string[] = [];
    for (const [name, state] of auditByTool) {
      if (state === 'unused') out.push(name);
    }
    return out;
  }, [auditByTool]);

  // Confirm gate for the remediation bulk action (consequence-stating).
  const [remediateOpen, setRemediateOpen] = useState(false);

  const rowRefs = useRef<Map<string, HTMLDivElement>>(new Map());

  // Scroll the selected tool into view (e.g. after a global-search reveal).
  // `block: 'nearest'` no-ops when the row is already visible, so clicking a
  // visible row to select it never jumps the list. DOM-only — no state writes.
  useEffect(() => {
    if (!selectedTool) return;
    // Optional-chain: scrollIntoView is absent in jsdom and older embeds.
    rowRefs.current.get(selectedTool)?.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' });
  }, [selectedTool]);

  const saveLabel = dirty
    ? `Save ${diffCount} change${diffCount === 1 ? '' : 's'} & Reload`
    : 'Saved';

  return (
    <div className="px-6 py-4 max-w-3xl space-y-3" aria-busy={isSaving}>
      {/* Count + quick actions */}
      <div className="flex items-center gap-2 text-[11px] text-text-muted">
        <span>
          <span className="text-text-secondary font-medium">{selected.size}</span> of{' '}
          <span className="text-text-secondary font-medium">{rows.length}</span> enabled —
          empty means all tools exposed
        </span>
        <div className="ml-auto flex items-center gap-2">
          <button
            type="button"
            onClick={selectAll}
            disabled={isSaving}
            aria-label="Select all tools"
            className="text-[10px] text-secondary hover:text-secondary-light transition-colors disabled:opacity-50"
          >
            Select all
          </button>
          <span className="text-border" aria-hidden="true">·</span>
          <button
            type="button"
            onClick={clearAll}
            disabled={isSaving}
            aria-label="Clear all tools"
            className="text-[10px] text-secondary hover:text-secondary-light transition-colors disabled:opacity-50"
          >
            Clear
          </button>
        </div>
      </div>

      {auditMode && unusedSelected.length > 0 && (
        <div className="flex items-center gap-2 rounded-md border border-status-pending/30 bg-status-pending/[0.06] px-3 py-2">
          <Activity size={12} className="text-status-pending flex-shrink-0" aria-hidden="true" />
          <p className="flex-1 text-[11px] text-text-secondary">
            <span className="font-medium text-status-pending">{unusedSelected.length}</span> exposed{' '}
            {unusedSelected.length === 1 ? 'tool has' : 'tools have'} no recorded calls in the
            lookback window.
          </p>
          <button
            type="button"
            onClick={() => setRemediateOpen(true)}
            disabled={isSaving}
            aria-label={`Disable ${unusedSelected.length} unused tools`}
            className="flex-shrink-0 inline-flex items-center gap-1 rounded-md border border-status-pending/40 bg-status-pending/10 px-2 py-1 text-[10px] font-medium text-status-pending hover:bg-status-pending/20 transition-colors disabled:opacity-50"
          >
            Disable {unusedSelected.length} unused
          </button>
        </div>
      )}

      <div className="rounded-lg border border-border/40 bg-background/60 overflow-hidden">
        <Command shouldFilter={false} label={`Tools for ${server.name}`} className="flex flex-col">
          <div className="flex items-center gap-2 px-3 py-2 border-b border-border/30">
            <Search size={12} className="text-text-muted flex-shrink-0" aria-hidden="true" />
            <Command.Input
              value={query}
              onValueChange={setQuery}
              placeholder={`Filter ${server.name} tools…`}
              aria-label="Filter tools"
              className="flex-1 bg-transparent outline-none text-xs text-text-primary placeholder:text-text-muted/60"
            />
          </div>

          <Command.List className="max-h-[60vh] overflow-y-auto" aria-label="Available tools">
            <Command.Empty>
              <p className="text-[11px] text-text-muted/60 italic py-4 px-3 text-center">
                {rows.length === 0
                  ? 'No tools discovered for this server yet.'
                  : `No tools match "${query}"`}
              </p>
            </Command.Empty>
            {visible.map((opt) => {
              const isEnabled = selected.has(opt.name);
              const isActive = selectedTool === opt.name;
              const auditState = auditByTool.get(opt.name) ?? null;
              return (
                <div
                  key={opt.name}
                  ref={(el) => {
                    if (el) rowRefs.current.set(opt.name, el);
                    else rowRefs.current.delete(opt.name);
                  }}
                  className={cn(
                    'border-b border-border/20 last:border-b-0 border-l-2',
                    isActive
                      ? 'border-l-primary bg-primary/[0.06]'
                      : 'border-l-transparent',
                    isEnabled && !isActive && 'bg-primary/[0.03]',
                  )}
                >
                  <Command.Item
                    value={opt.name}
                    onSelect={() => onSelect(opt.name)}
                    aria-current={isActive}
                    aria-label={`${opt.name} details`}
                    className={cn(
                      'flex items-start gap-2.5 px-3 py-2 cursor-pointer select-none outline-none transition-colors',
                      'hover:bg-surface-highlight/40',
                      '[&[data-selected=true]]:bg-surface-highlight/40',
                    )}
                  >
                    <button
                      type="button"
                      role="checkbox"
                      aria-checked={isEnabled}
                      aria-label={isEnabled ? `Disable ${opt.name}` : `Enable ${opt.name}`}
                      onClick={(e) => {
                        // Toggling exposure must not select the row for the panel.
                        e.stopPropagation();
                        toggle(opt.name);
                      }}
                      className={cn(
                        'mt-0.5 w-3.5 h-3.5 rounded border flex items-center justify-center flex-shrink-0 transition-colors',
                        isEnabled
                          ? 'bg-primary/20 border-primary/60'
                          : 'border-border/60 bg-background/50 hover:border-primary/40',
                      )}
                    >
                      {isEnabled && <Check size={10} className="text-primary" aria-hidden="true" />}
                    </button>
                    <div className="flex-1 min-w-0">
                      <div
                        className={cn(
                          'text-xs font-mono truncate',
                          isEnabled ? 'text-text-primary' : 'text-text-secondary',
                        )}
                      >
                        {opt.name}
                      </div>
                      {opt.description && (
                        <div className="text-[10px] text-text-muted truncate">{opt.description}</div>
                      )}
                      {auditState && (
                        <div
                          className={cn(
                            'flex items-center gap-1 text-[10px] mt-0.5',
                            AUDIT_STYLES[auditState].text,
                          )}
                        >
                          <span
                            className={cn(
                              'inline-block w-1.5 h-1.5 rounded-full flex-shrink-0',
                              AUDIT_STYLES[auditState].dot,
                            )}
                            aria-hidden="true"
                          />
                          <span>{AUDIT_STYLES[auditState].label}</span>
                          {auditState !== 'disabled' && (
                            <span className="text-text-muted/70 truncate">
                              · {formatLastUsed(usage?.[opt.name]?.lastCalledAt)}
                            </span>
                          )}
                        </div>
                      )}
                    </div>
                    <ChevronRight
                      size={13}
                      className={cn(
                        'flex-shrink-0 mt-0.5 transition-colors',
                        isActive ? 'text-primary' : 'text-text-muted/40',
                      )}
                      aria-hidden="true"
                    />
                  </Command.Item>
                </div>
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

      <ConfirmDialog
        isOpen={remediateOpen}
        onClose={() => setRemediateOpen(false)}
        onConfirm={() => {
          setRemediateOpen(false);
          void disableTools(unusedSelected);
        }}
        title="Disable unused tools"
        message={
          <p>
            Disable <span className="font-mono text-primary">{unusedSelected.length}</span> unused{' '}
            {unusedSelected.length === 1 ? 'tool' : 'tools'} on{' '}
            <span className="font-mono text-primary">{server.name}</span>? This updates the
            whitelist and reloads the server.
          </p>
        }
        confirmLabel="Disable & reload"
        variant="danger"
      />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Global search results
// ---------------------------------------------------------------------------

interface GlobalResult {
  server: string;
  tool: string;
  description?: string;
}

interface GlobalResultsProps {
  results: GlobalResult[];
  query: string;
  serverCount: number;
  onPick: (server: string, tool: string) => void;
}

function GlobalResults({ results, query, serverCount, onPick }: GlobalResultsProps) {
  return (
    <div className="px-6 py-4 max-w-3xl space-y-2">
      <p className="text-[10px] uppercase tracking-[0.18em] text-text-muted/70">
        Searching all {serverCount} {serverCount === 1 ? 'server' : 'servers'} ·{' '}
        {results.length} {results.length === 1 ? 'match' : 'matches'}
      </p>
      {results.length === 0 ? (
        <div className="py-10 text-center text-xs text-text-secondary">
          No tools match "{query}" across any server.
        </div>
      ) : (
        <div className="space-y-1">
          {results.map((r) => (
            <button
              key={`${r.server}${TOOL_NAME_DELIMITER}${r.tool}`}
              onClick={() => onPick(r.server, r.tool)}
              className="w-full flex items-start gap-2 px-3 py-2 rounded-md text-left bg-background/40 border border-border/30 hover:border-primary/40 hover:bg-surface-highlight/40 transition-colors"
            >
              <div className="flex-1 min-w-0">
                <div className="text-xs font-mono truncate">
                  <span className="text-primary/80">{r.server}</span>
                  <span className="text-text-muted/60 mx-1">›</span>
                  <span className="text-text-primary">{r.tool}</span>
                </div>
                {r.description && (
                  <div className="text-[10px] text-text-muted truncate mt-0.5">{r.description}</div>
                )}
              </div>
              <ChevronRight size={13} className="text-text-muted/50 flex-shrink-0 mt-0.5" />
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Empty state
// ---------------------------------------------------------------------------

function ToolsEmptyState({ loading }: { loading: boolean }) {
  return (
    <div className="h-full flex items-center justify-center px-6 py-12">
      <div className="max-w-md w-full text-center space-y-4">
        <div className="mx-auto w-14 h-14 rounded-2xl bg-primary/10 border border-primary/20 flex items-center justify-center">
          {loading ? (
            <Loader2 size={24} className="text-primary/70 animate-spin" />
          ) : (
            <Wrench size={24} className="text-primary/70" />
          )}
        </div>
        <div className="space-y-1.5">
          <h2 className="text-base font-semibold text-text-primary">
            {loading ? 'Loading servers…' : 'No MCP servers yet'}
          </h2>
          <p className="text-xs text-text-muted leading-relaxed">
            {loading
              ? 'Fetching the active stack.'
              : 'Add an MCP server to your stack to manage its tools here.'}
          </p>
        </div>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// toolCounts derives the "enabled/total" badge. An empty (or absent) whitelist
// means every advertised tool is exposed.
function toolCounts(server: MCPServerStatus): { enabled: number; total: number } {
  const total = server.tools.length;
  const wl = server.toolWhitelist;
  const enabled = wl && wl.length > 0 ? wl.length : total;
  return { enabled, total };
}

function serverStatus(server: MCPServerStatus): NodeStatus {
  if (server.healthy === false) return 'error';
  if (!server.initialized) return 'initializing';
  return 'running';
}

// splitTool turns a prefixed aggregated tool ("server__tool") into its server
// and tool parts. Returns null for names without the delimiter (defensive —
// every aggregated tool should carry it).
function splitTool(t: Tool): GlobalResult | null {
  const idx = t.name.indexOf(TOOL_NAME_DELIMITER);
  if (idx <= 0) return null;
  return {
    server: t.name.slice(0, idx),
    tool: t.name.slice(idx + TOOL_NAME_DELIMITER.length),
    description: t.description,
  };
}

export default ToolsWorkspace;
