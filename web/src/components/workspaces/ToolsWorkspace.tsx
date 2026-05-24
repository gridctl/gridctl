import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { Command } from 'cmdk';
import {
  AlertCircle,
  Check,
  ChevronRight,
  Loader2,
  RefreshCw,
  Save,
  Search,
  Wrench,
  X,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { useToolsEditor } from '../../hooks/useToolsEditor';
import { useFuzzySearch } from '../../hooks/useFuzzySearch';
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { StatusDot } from '../ui/StatusDot';
import { CodeViewer } from '../ui/CodeViewer';
import type { MCPServerStatus, NodeStatus, Tool } from '../../types';

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
  const tools = useStackStore((s) => s.tools);
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

  // ---- Server-switch guard ------------------------------------------------
  // A pending target set while the active editor has unsaved edits. The switch
  // commits (URL change) only after the user discards.
  const [pendingServer, setPendingServer] = useState<string | null>(null);
  // The tool whose input schema is expanded in the detail pane. Picking a
  // global-search result sets it so that tool is revealed on arrival.
  const [expandedTool, setExpandedTool] = useState<string | null>(null);

  // Switch to a server (or reveal a tool on the active one). Unsaved edits on
  // the current server route through a confirm dialog before the switch
  // commits. Plain functions — the React compiler memoizes them.
  function requestServer(name: string, tool?: string) {
    if (name !== activeServerName && editor.dirty) {
      setPendingServer(name);
      setExpandedTool(tool ?? null);
      return;
    }
    if (name !== activeServerName) applyServer(name);
    // Clear any active filter so a revealed tool is visible, then expand it.
    editor.setQuery('');
    setExpandedTool(tool ?? null);
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

  const toggleExpanded = (tool: string) =>
    setExpandedTool((prev) => (prev === tool ? null : tool));

  // ---- Global search ------------------------------------------------------
  const searchActive = globalQuery.trim().length > 0;
  const globalMatches = useFuzzySearch(tools, globalQuery);
  const globalResults = useMemo(
    () => globalMatches.map(splitTool).filter((r): r is GlobalResult => r !== null),
    [globalMatches],
  );

  const leftRail = (
    <ServerRail
      compact={compact}
      servers={servers}
      activeServerName={activeServerName}
      onSelectServer={(name) => requestServer(name)}
    />
  );

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell
        workspace="tools"
        defaultLeftPct={22}
        defaultRightPct={0}
        left={leftRail}
        minLeftPx={220}
      >
        <main className="flex flex-col h-full overflow-hidden">
          <ToolsHeader
            compact={compact}
            serverCount={servers.length}
            query={globalQuery}
            onQueryChange={setGlobalQuery}
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
                allTools={tools}
                expanded={expandedTool}
                onToggleExpand={toggleExpanded}
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
}

function ToolsHeader({ compact, serverCount, query, onQueryChange }: ToolsHeaderProps) {
  const searching = query.trim().length > 0;
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
      </div>

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
}

function ServerRail({ compact, servers, activeServerName, onSelectServer }: ServerRailProps) {
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
}

function ServerPill({ server, active, onClick }: ServerPillProps) {
  const { enabled, total } = toolCounts(server);
  const status = serverStatus(server);
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
  allTools: Tool[];
  // The tool whose schema is expanded (controlled by the workspace so a
  // global-search reveal can target it). null = none expanded.
  expanded: string | null;
  onToggleExpand: (tool: string) => void;
}

function ServerDetail({ server, editor, allTools, expanded, onToggleExpand }: ServerDetailProps) {
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
    handleReloadFromDisk,
  } = editor;

  // Input schemas for this server's tools, keyed by unprefixed tool name.
  const schemaByTool = useMemo(() => {
    const prefix = `${server.name}${TOOL_NAME_DELIMITER}`;
    const map = new Map<string, Record<string, unknown>>();
    for (const t of allTools) {
      if (t.name.startsWith(prefix)) map.set(t.name.slice(prefix.length), t.inputSchema);
    }
    return map;
  }, [allTools, server.name]);

  const rowRefs = useRef<Map<string, HTMLDivElement>>(new Map());

  // Scroll the expanded tool into view (e.g. after a global-search reveal).
  // DOM-only side effect — no state updates.
  useEffect(() => {
    if (!expanded) return;
    // Optional-chain: scrollIntoView is absent in jsdom and older embeds.
    rowRefs.current.get(expanded)?.scrollIntoView?.({ block: 'center', behavior: 'smooth' });
  }, [expanded]);

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
              const isSelected = selected.has(opt.name);
              const schema = schemaByTool.get(opt.name);
              const isExpanded = expanded === opt.name;
              return (
                <div
                  key={opt.name}
                  ref={(el) => {
                    if (el) rowRefs.current.set(opt.name, el);
                    else rowRefs.current.delete(opt.name);
                  }}
                  className={cn(
                    'border-b border-border/20 last:border-b-0',
                    isSelected && 'bg-primary/[0.03]',
                  )}
                >
                  <Command.Item
                    value={opt.name}
                    onSelect={() => toggle(opt.name)}
                    aria-checked={isSelected}
                    aria-label={opt.name}
                    className={cn(
                      'flex items-start gap-2.5 px-3 py-2 cursor-pointer select-none outline-none transition-colors',
                      'hover:bg-surface-highlight/50',
                      '[&[data-selected=true]]:bg-primary/[0.06]',
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
                    {schema && (
                      <button
                        type="button"
                        onClick={(e) => {
                          // Don't toggle selection when peeking the schema.
                          e.stopPropagation();
                          onToggleExpand(opt.name);
                        }}
                        aria-label={isExpanded ? `Hide ${opt.name} schema` : `Show ${opt.name} schema`}
                        aria-expanded={isExpanded}
                        className="flex-shrink-0 p-0.5 rounded text-text-muted hover:text-text-primary hover:bg-surface-highlight transition-colors"
                      >
                        <ChevronRight
                          size={13}
                          className={cn('transition-transform', isExpanded && 'rotate-90')}
                        />
                      </button>
                    )}
                  </Command.Item>
                  {isExpanded && schema && (
                    <div className="px-3 pb-2">
                      <CodeViewer
                        language="json"
                        content={JSON.stringify(schema, null, 2)}
                        ariaLabel={`${opt.name} input schema`}
                        className="rounded-md border border-border/30 bg-background/80 max-h-64"
                      />
                    </div>
                  )}
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
