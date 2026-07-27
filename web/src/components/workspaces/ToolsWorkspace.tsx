import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router';
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
  Undo2,
  Users,
  Boxes,
  Wrench,
  X,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { useStackStore } from '../../stores/useStackStore';
import { useToolsDirtyStore } from '../../stores/useToolsDirtyStore';
import { useUIStore } from '../../stores/useUIStore';
import { useToolsEditor } from '../../hooks/useToolsEditor';
import { useToolUsage } from '../../hooks/useToolUsage';
import { useOptimize } from '../../hooks/useOptimize';
import { useFuzzySearch } from '../../hooks/useFuzzySearch';
import { TOOL_NAME_DELIMITER } from '../../lib/constants';
import { formatRelativeTime } from '../../lib/time';
import {
  AUDIT_WINDOWS,
  DEFAULT_AUDIT_WINDOW,
  auditWindowMs,
  classifyTool,
  effectiveEnabledTools,
  formatLastUsed,
  unusedEnabledTools,
  type AuditState,
  type AuditWindow,
} from '../../lib/toolAudit';
import {
  AUDIT_FILTERS,
  TOOL_SORT_MODES,
  filterToolRows,
  isAuditFilter,
  isToolSortMode,
  sortNeedsUsage,
  sortToolRows,
  type AuditFilter,
  type ToolSortMode,
} from '../../lib/toolSort';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { StatusDot } from '../ui/StatusDot';
import { FleetActions } from './FleetActions';
import { ClientAccessEditor } from './ClientAccessEditor';
import { GroupsPanel } from './GroupsPanel';
import { useGroups } from '../../hooks/useGroups';
import { CHIP_TONE_CLASS, annotationChips, groupsForTool } from '../../lib/groups';
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
// Stack, Library, and Variables. The left rail lists every MCP server with
// an enabled/total badge; the main pane edits the selected server's whitelist
// (reusing useToolsEditor) and previews each tool's input schema. A header
// search spans every server's tools at once.
//
// The editor controller is owned here (not inside a child) so the workspace
// can guard server switches against unsaved edits before the URL changes —
// the sidebar's stack-based discard flow doesn't map onto URL selection.
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
  // Everything an operator would share in a support link lives in the URL:
  // server, global query, selected tool, audit mode, and lookback window.
  // Defaults are omitted so bare links stay canonical (Metrics idiom).
  const serverParam = searchParams.get('server') ?? '';
  const globalQuery = searchParams.get('q') ?? '';
  // The tool whose detail is shown in the right rail. Distinct from the
  // whitelist selection (the checkboxes) — this is the "active" tool. Picking
  // a global-search result sets it so that tool is revealed on arrival.
  const selectedTool = searchParams.get('tool');
  const auditMode = searchParams.get('audit') === '1';
  const windowParam = searchParams.get('window');
  const auditWindow: AuditWindow = AUDIT_WINDOWS.some((w) => w.id === windowParam)
    ? (windowParam as AuditWindow)
    : DEFAULT_AUDIT_WINDOW;

  // List facets: URL param wins, then the persisted preference, then the
  // default (the Pins idiom): a shared ?filter= link restores the exact view
  // while a returning operator keeps their last-used facets.
  const toolsPrefs = useUIStore((s) => s.toolsPrefs);
  const filterParam = searchParams.get('filter');
  const auditFilter: AuditFilter = isAuditFilter(filterParam) ? filterParam : toolsPrefs.filter;
  const sortParam = searchParams.get('sort');
  const sortMode: ToolSortMode = isToolSortMode(sortParam) ? sortParam : toolsPrefs.sort;
  const destructiveOnly = searchParams.has('risk')
    ? searchParams.get('risk') === 'destructive'
    : toolsPrefs.destructiveOnly;

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

  // Selecting a server also exits any active global search and re-targets the
  // detail tool — done in one params update so the changes compose (two
  // separate setSearchParams calls in a handler both observe the same stale
  // snapshot and the last wins).
  const applyServer = useCallback(
    (name: string, tool: string | null = null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          next.set('server', name);
          next.delete('q');
          if (tool) next.set('tool', tool);
          else next.delete('tool');
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const applyTool = useCallback(
    (tool: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (tool) next.set('tool', tool);
          else next.delete('tool');
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const toggleAudit = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (next.get('audit') === '1') {
          // The window is audit-local; a bare link must not carry it alone.
          next.delete('audit');
          next.delete('window');
        } else {
          next.set('audit', '1');
        }
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  const applyAuditWindow = useCallback(
    (w: AuditWindow) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (w === DEFAULT_AUDIT_WINDOW) next.delete('window');
          else next.set('window', w);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // Facet writers persist the preference and mirror it into the URL in one
  // gesture; default values are deleted so bare links stay canonical.
  const applyFilter = useCallback(
    (f: AuditFilter) => {
      useUIStore.getState().setToolsPrefs({ filter: f });
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (f === 'all') next.delete('filter');
          else next.set('filter', f);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const applySort = useCallback(
    (m: ToolSortMode) => {
      useUIStore.getState().setToolsPrefs({ sort: m });
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (m === 'default') next.delete('sort');
          else next.set('sort', m);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const applyRisk = useCallback(
    (on: boolean) => {
      useUIStore.getState().setToolsPrefs({ destructiveOnly: on });
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (on) next.set('risk', 'destructive');
          else next.delete('risk');
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // Clearing both facets must be one setSearchParams call: two functional
  // setters in one handler both close over the same render's snapshot, so
  // the second navigation would resurrect the param the first deleted (the
  // applyServer comment documents the same footgun).
  const clearFacets = useCallback(() => {
    useUIStore.getState().setToolsPrefs({ filter: 'all', destructiveOnly: false });
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete('filter');
        next.delete('risk');
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

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

  // Fleet bulk-action panel (expose-all / hide-pattern across servers).
  const [fleetOpen, setFleetOpen] = useState(false);
  // Per-client access editor (which servers each client may reach).
  const [accessOpen, setAccessOpen] = useState(false);
  // Tool groups panel (the curation axis: bundles served at /groups/{name}/mcp).
  const [groupsOpen, setGroupsOpen] = useState(false);
  const { report: groupsReport } = useGroups(true);

  // ---- Audit Mode ---------------------------------------------------------
  // An overlay that classifies each tool as used / configured-but-unused /
  // disabled against a lookback window (mode and window are URL state above).
  // Usage polls while something consumes it: Audit Mode's classification, the
  // detail panel's Usage section for the selected tool (shown outside Audit
  // Mode too), a usage-keyed sort, or the Fleet panel's disable-unused plan.
  // Otherwise the hook idles so the editor pays nothing.
  const { usage, error: usageError, fetchedAt } = useToolUsage(
    auditMode || selectedTool != null || sortNeedsUsage(sortMode) || fleetOpen,
  );
  // Usage is loading (not failed) until the first snapshot lands — without
  // this, Audit Mode renders no dots and no badges, indistinguishable from
  // "everything is clean".
  const auditLoading = auditMode && fetchedAt == null && usageError == null;
  // A failed fetch over a retained snapshot is "stale", not "unavailable" —
  // the dots keep rendering from the last snapshot, so the copy must not
  // contradict them.
  const usageNotice = usageError
    ? fetchedAt == null
      ? `Tool usage unavailable: ${usageError}`
      : `Usage refresh failed: ${usageError} — showing the last loaded snapshot`
    : null;
  const windowMs = auditWindowMs(auditWindow);
  const windowLabel = AUDIT_WINDOWS.find((w) => w.id === auditWindow)?.label ?? '7 days';
  const usageByServer = usage?.servers;

  // ---- Optimize convergence ------------------------------------------------
  // The optimize `unused_tool` heuristic shares Audit's 7d idea of "unused"
  // (fixed server-side; the audit window picker doesn't move it). Surface its
  // count as one line with a jump into Audit so the two tell one story.
  // Honesty rules: a young gateway returns only a need_more_data info finding
  // and a missing accumulator 503s; both render as nothing, never "0 unused".
  const { report: optimizeReport } = useOptimize(servers.length > 0);
  const optimizeUnusedCount = useMemo(() => {
    if (!optimizeReport) return null;
    const n = optimizeReport.findings.filter((f) => f.heuristic === 'unused_tool').length;
    return n > 0 ? n : null;
  }, [optimizeReport]);

  // Jump target for the optimize line: Audit Mode at the 7d window (the
  // window optimize classified against; 7d is the default so ?window= drops).
  const openAudit7d = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.set('audit', '1');
        next.delete('window');
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

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
  // (and any tool reveal that rides along) commits only after the user
  // discards.
  const [pendingServer, setPendingServer] = useState<string | null>(null);
  const [pendingTool, setPendingTool] = useState<string | null>(null);

  // Switch to a server (or reveal a tool on the active one). Unsaved edits on
  // the current server route through a confirm dialog before the switch
  // commits. Plain functions — the React compiler memoizes them.
  function requestServer(name: string, tool?: string) {
    if (name !== activeServerName && editor.dirty) {
      setPendingServer(name);
      setPendingTool(tool ?? null);
      return;
    }
    // Clear any active filter so a revealed tool is visible, then select it.
    editor.setQuery('');
    applyServer(name, tool ?? null);
  }

  function confirmSwitch() {
    if (!pendingServer) return;
    // Reset the editor to the current server's saved state first so the
    // upcoming serverName change is a clean transition (no spurious prompt).
    editor.handleDiscard();
    editor.setQuery('');
    applyServer(pendingServer, pendingTool);
    setPendingServer(null);
    setPendingTool(null);
  }

  // ---- Leave-workspace guard ----------------------------------------------
  // Mirror the editor's dirty flag into the shared guard store so the
  // WorkspaceSwitcher (outside this tree) can read it synchronously and
  // intercept the NavLink; the intercepted target lands in exitNavTarget and
  // the confirm renders below. Cleared on unmount so a stale flag can never
  // block navigation after the workspace is gone.
  const navigate = useNavigate();
  const exitNavTarget = useToolsDirtyStore((s) => s.exitNavTarget);
  useEffect(() => {
    useToolsDirtyStore.getState().setDirty(editor.dirty);
  }, [editor.dirty]);
  useEffect(
    () => () => {
      const s = useToolsDirtyStore.getState();
      s.setDirty(false);
      s.clearExitNav();
    },
    [],
  );

  // Hard page unload (reload/close): warn while the editor is dirty.
  useEffect(() => {
    if (!editor.dirty) return;
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = '';
    };
    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, [editor.dirty]);

  // ---- Global search ------------------------------------------------------
  const searchActive = globalQuery.trim().length > 0;
  const globalMatches = useFuzzySearch(toolCatalog, globalQuery);
  const globalResults = useMemo(
    () => globalMatches.map(splitTool).filter((r): r is GlobalResult => r !== null),
    [globalMatches],
  );

  // Audit classification for search results, which span every server. The
  // active server classifies against the draft selection so search rows agree
  // with the editor rows; other servers use their live whitelist.
  const auditForResult = useCallback(
    (serverName: string, toolName: string): AuditState | null => {
      if (!auditMode || fetchedAt == null) return null;
      const srv = servers.find((s) => s.name === serverName);
      if (!srv) return null;
      const enabled =
        serverName === activeServerName
          ? editor.selected.has(toolName)
          : effectiveEnabledTools(srv).has(toolName);
      return classifyTool(
        enabled,
        usageByServer?.[serverName]?.[toolName]?.lastCalledAt,
        windowMs,
        fetchedAt,
      );
    },
    [auditMode, fetchedAt, servers, activeServerName, editor.selected, usageByServer, windowMs],
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
  // Self-heal a ?tool= that doesn't resolve on the active server (typo or a
  // cross-server link): drop it so the detail rail isn't stuck on its empty
  // prompt and usage polling doesn't run for a phantom selection. Skipped
  // while the server has no discovered tools yet — the name may resolve once
  // discovery lands.
  useEffect(() => {
    if (selectedTool && !selectedRow && editor.allTools.length > 0) applyTool(null);
  }, [selectedTool, selectedRow, editor.allTools.length, applyTool]);

  const selectedEnabled = selectedTool ? editor.selected.has(selectedTool) : false;
  const selectedUsage = selectedTool ? usageByServer?.[activeServerName]?.[selectedTool] : undefined;
  const selectedLastCalled = selectedUsage?.lastCalledAt;
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
      dirtyServer={editor.dirty ? activeServerName : null}
      draftEnabledCount={editor.dirty ? editor.selected.size : null}
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
      usage={selectedUsage}
      lastCalledAt={selectedLastCalled}
      onClose={() => applyTool(null)}
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
            onToggleAudit={toggleAudit}
            auditWindow={auditWindow}
            windowLabel={windowLabel}
            onWindowChange={applyAuditWindow}
            observedSince={usage?.observedSince}
            usageNotice={usageNotice}
            usageLoading={auditLoading}
            onOpenFleet={() => setFleetOpen(true)}
            fleetDisabled={servers.length === 0}
            onOpenAccess={() => setAccessOpen(true)}
            onOpenGroups={() => setGroupsOpen(true)}
            optimizeUnusedCount={optimizeUnusedCount}
            onReviewAudit={openAudit7d}
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
                auditFor={auditForResult}
              />
            ) : activeServer ? (
              <ServerDetail
                key={activeServer.name}
                server={activeServer}
                editor={editor}
                selectedTool={selectedTool}
                onSelect={applyTool}
                auditMode={auditMode}
                usage={usageByServer?.[activeServer.name]}
                windowMs={windowMs}
                now={fetchedAt}
                auditFilter={auditFilter}
                onFilter={applyFilter}
                sortMode={sortMode}
                onSort={applySort}
                destructiveOnly={destructiveOnly}
                onRisk={applyRisk}
                onClearFacets={clearFacets}
                groupsFor={(tool) =>
                  groupsForTool(groupsReport, `${activeServer.name}${TOOL_NAME_DELIMITER}${tool}`)
                }
              />
            ) : null}
          </div>
        </main>
      </WorkspaceShell>

      <ConfirmDialog
        isOpen={pendingServer !== null}
        onClose={() => {
          setPendingServer(null);
          setPendingTool(null);
        }}
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

      {/* Gated on the server-switch dialog being closed so two focus traps
          never stack. */}
      <ConfirmDialog
        isOpen={exitNavTarget !== null && pendingServer === null}
        onClose={() => useToolsDirtyStore.getState().clearExitNav()}
        onConfirm={() => {
          const target = exitNavTarget;
          editor.handleDiscard();
          useToolsDirtyStore.getState().clearExitNav();
          if (target) navigate(target);
        }}
        title="Discard unsaved changes"
        message={<p>Leave Tools and discard unsaved tool changes?</p>}
        confirmLabel="Discard & leave"
        variant="danger"
      />

      <FleetActions
        isOpen={fleetOpen}
        onClose={() => setFleetOpen(false)}
        servers={servers}
        activeServerName={activeServerName}
        usage={usage}
        windowMs={windowMs}
        windowLabel={windowLabel}
        fetchedAt={fetchedAt}
      />

      <ClientAccessEditor
        isOpen={accessOpen}
        onClose={() => setAccessOpen(false)}
        servers={servers}
      />

      <GroupsPanel
        isOpen={groupsOpen}
        onClose={() => setGroupsOpen(false)}
        report={groupsReport}
        toolCatalog={toolCatalog}
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
  windowLabel: string;
  onWindowChange: (w: AuditWindow) => void;
  observedSince?: string;
  // Non-null when the usage fetch is failing — Audit Mode must say so instead
  // of rendering an empty overlay (preformatted: unavailable vs stale).
  usageNotice: string | null;
  // True while the first usage snapshot is still in flight.
  usageLoading: boolean;
  onOpenFleet: () => void;
  fleetDisabled: boolean;
  onOpenAccess: () => void;
  onOpenGroups: () => void;
  // Count of optimize `unused_tool` findings; null when there are none, the
  // gateway is too young (need_more_data), or the report is unavailable;
  // the line renders only for a real, positive count.
  optimizeUnusedCount: number | null;
  onReviewAudit: () => void;
}

function ToolsHeader({
  compact,
  serverCount,
  query,
  onQueryChange,
  auditMode,
  onToggleAudit,
  auditWindow,
  windowLabel,
  onWindowChange,
  observedSince,
  usageNotice,
  usageLoading,
  onOpenFleet,
  fleetDisabled,
  onOpenAccess,
  onOpenGroups,
  optimizeUnusedCount,
  onReviewAudit,
}: ToolsHeaderProps) {
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

        <div className="ml-auto flex items-center gap-2">
          {/* Always visible — the unconfigured case opens the panel's empty
              state, which teaches the stack.yaml `groups:` block (same
              discovery pattern as Access with zero linked clients). */}
          <button
            type="button"
            onClick={onOpenGroups}
            aria-label="Open tool groups"
            className={cn(
              'inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-[10px] font-medium border transition-colors',
              'bg-secondary/10 text-secondary border-secondary/30 hover:bg-secondary/20 hover:border-secondary/50',
            )}
          >
            <Boxes size={11} aria-hidden="true" />
            Groups
          </button>
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

      {optimizeUnusedCount !== null && (
        <p className="text-[10px] text-text-muted/80 leading-relaxed" role="status">
          <span className="text-status-pending font-medium">Optimize</span> suggests{' '}
          <span className="text-text-secondary font-medium">{optimizeUnusedCount}</span> unused{' '}
          {optimizeUnusedCount === 1 ? 'tool' : 'tools'} (7d)
          {!auditMode && (
            <>
              {' · '}
              <button
                type="button"
                onClick={onReviewAudit}
                className="text-secondary hover:text-secondary-light transition-colors"
              >
                Review in Audit
              </button>
            </>
          )}
        </p>
      )}

      {auditMode && (
        <p className="text-[10px] text-text-muted/80 leading-relaxed" role="status">
          <span className="text-status-running">●</span> used ·{' '}
          <span className="text-status-pending">●</span> unused ·{' '}
          <span className="text-text-muted">●</span> disabled — &ldquo;unused&rdquo; means no
          recorded calls in the last {windowLabel}.
          {observedSince
            ? ` Tracking since ${formatRelativeTime(new Date(observedSince))}; tools with no recorded calls may predate it.`
            : ' Counts cover activity since the last gateway restart.'}
          {usageLoading && (
            <span className="inline-flex items-center gap-1 ml-2 text-text-secondary">
              <Loader2 size={10} className="animate-spin" aria-hidden="true" />
              Loading usage…
            </span>
          )}
        </p>
      )}
      {auditMode && usageNotice && (
        <p role="alert" className="text-[10px] text-status-error leading-relaxed">
          {usageNotice}
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
  // The server whose editor holds unsaved edits (only ever the active one).
  dirtyServer: string | null;
  // The dirty editor's draft selection size; the dirty server's badge shows
  // this (draft) count instead of the live one so the rail and the center
  // editor agree while an edit is in flight.
  draftEnabledCount: number | null;
}

function ServerRail({
  compact,
  servers,
  activeServerName,
  onSelectServer,
  auditMode,
  unusedByServer,
  dirtyServer,
  draftEnabledCount,
}: ServerRailProps) {
  return (
    <aside className="h-full flex flex-col bg-surface border-r border-border-subtle">
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
            dirty={server.name === dirtyServer}
            draftEnabled={server.name === dirtyServer ? draftEnabledCount : null}
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
  dirty: boolean;
  // Draft selection size while this server's editor is dirty (null otherwise).
  draftEnabled: number | null;
}

function ServerPill({
  server,
  active,
  onClick,
  auditMode,
  unusedCount,
  dirty,
  draftEnabled,
}: ServerPillProps) {
  const { enabled, total } = toolCounts(server);
  const showDraft = dirty && draftEnabled !== null;
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
      {dirty && (
        <span
          className="flex-shrink-0 text-[9px] font-medium px-1.5 py-0.5 rounded bg-status-pending/15 text-status-pending"
          title="This server has unsaved tool changes; the count badge shows the draft selection"
        >
          unsaved
        </span>
      )}
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
          showDraft
            ? 'bg-status-pending/15 text-status-pending'
            : active
              ? 'bg-primary/15 text-primary'
              : 'bg-surface-elevated text-text-muted',
        )}
        title={
          showDraft
            ? `Unsaved draft: ${draftEnabled} of ${total} tools selected (saved: ${enabled}/${total})`
            : `${enabled} of ${total} tools enabled`
        }
      >
        {showDraft ? draftEnabled : enabled}/{total}
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
  // List facets (URL/pref state owned by the workspace).
  auditFilter: AuditFilter;
  onFilter: (f: AuditFilter) => void;
  sortMode: ToolSortMode;
  onSort: (m: ToolSortMode) => void;
  destructiveOnly: boolean;
  onRisk: (on: boolean) => void;
  // Atomic both-facets reset (one URL write; see clearFacets).
  onClearFacets: () => void;
  // Names of tool groups whose surface includes this (canonical) tool, for
  // the curation-axis badges on each row.
  groupsFor: (tool: string) => string[];
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
  auditFilter,
  onFilter,
  sortMode,
  onSort,
  destructiveOnly,
  onRisk,
  onClearFacets,
  groupsFor,
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
    saveBlocked,
    diffCount,
    isSaving,
    conflict,
    handleSave,
    disableTools,
    handleReloadFromDisk,
    handleDiscard,
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

  // Audit filters only bite while classification data exists; the risk facet
  // is annotation-driven and applies regardless. Sort applies after facets,
  // except during a text query, where Fuse relevance order wins so the best
  // match stays on top.
  const auditActive = auditMode && now != null;
  const shown = useMemo(() => {
    const filtered = filterToolRows(
      visible,
      auditActive ? auditFilter : 'all',
      destructiveOnly,
      (name) => auditByTool.get(name) ?? null,
    );
    if (query.trim()) return filtered;
    return sortToolRows(filtered, sortMode, usage);
  }, [visible, auditActive, auditFilter, destructiveOnly, auditByTool, query, sortMode, usage]);

  // Chip counts cover the whole server (not the query-narrowed list) so the
  // chips read as a per-server summary, mirroring the rail badge math.
  const filterCounts = useMemo(() => {
    const counts: Record<AuditFilter, number> = {
      all: rows.length,
      used: 0,
      unused: 0,
      disabled: 0,
    };
    for (const state of auditByTool.values()) counts[state]++;
    return counts;
  }, [rows, auditByTool]);
  const destructiveCount = useMemo(
    () => rows.filter((r) => r.annotations?.destructiveHint === true).length,
    [rows],
  );
  const facetsActive =
    (auditActive && auditFilter !== 'all' ? 1 : 0) + (destructiveOnly ? 1 : 0);

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
    <div className="px-6 py-4 max-w-4xl space-y-3" aria-busy={isSaving}>
      {/* Count + quick actions. The neutral "empty means all" help text must
          never sit next to a zero draft count — there it would describe the
          re-expose-all footgun as expected behavior. */}
      <div className="flex items-center gap-2 text-[11px] text-text-muted">
        {saveBlocked ? (
          <span role="alert" className="text-status-error">
            <span className="font-medium">0</span> of{' '}
            <span className="font-medium">{rows.length}</span> selected — cannot save an
            empty selection (an empty whitelist would expose all tools)
          </span>
        ) : (
          <span>
            <span className="text-text-secondary font-medium">{selected.size}</span> of{' '}
            <span className="text-text-secondary font-medium">{rows.length}</span> enabled —
            empty means all tools exposed
          </span>
        )}
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

      <div
        className="flex items-center gap-1.5 flex-wrap text-[10px]"
        role="group"
        aria-label="Tool list filters and sort"
      >
        {auditMode &&
          AUDIT_FILTERS.map((f) => (
            <FacetChip
              key={f.id}
              active={auditFilter === f.id}
              onClick={() => onFilter(f.id)}
              disabled={!auditActive}
            >
              {f.label}{' '}
              <span className="ml-1 tabular-nums opacity-70">{filterCounts[f.id]}</span>
            </FacetChip>
          ))}
        <FacetChip
          active={destructiveOnly}
          onClick={() => onRisk(!destructiveOnly)}
          tone="danger"
          title="Only tools whose server reports destructiveHint: true (a claim, not verified)"
        >
          Destructive{' '}
          <span className="ml-1 tabular-nums opacity-70">{destructiveCount}</span>
        </FacetChip>
        {facetsActive >= 2 && (
          <button
            type="button"
            onClick={onClearFacets}
            className="text-secondary hover:text-secondary-light transition-colors"
          >
            Clear all
          </button>
        )}
        <span className="ml-auto inline-flex items-center gap-1.5">
          {facetsActive > 0 && (
            <span role="status" className="text-text-muted">
              {shown.length} of {rows.length} shown
            </span>
          )}
          <label className="inline-flex items-center gap-1 text-text-muted">
            <span className="sr-only">Sort tools</span>
            <select
              value={sortMode}
              onChange={(e) => onSort(e.target.value as ToolSortMode)}
              aria-label="Sort tools"
              className="bg-background/60 border border-border/40 rounded-md px-1.5 py-1 text-[10px] text-text-secondary focus:outline-none focus:border-primary/50"
            >
              {TOOL_SORT_MODES.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.label}
                </option>
              ))}
            </select>
          </label>
        </span>
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
                  : visible.length === 0
                    ? `No tools match "${query}"`
                    : 'No tools match the active filters.'}
              </p>
            </Command.Empty>
            {shown.map((opt) => {
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
                      <div className="flex items-center gap-1.5 min-w-0">
                        <span
                          className={cn(
                            'text-xs font-mono truncate',
                            isEnabled ? 'text-text-primary' : 'text-text-secondary',
                          )}
                        >
                          {opt.name}
                        </span>
                        {groupsFor(opt.name).map((g) => (
                          <span
                            key={g}
                            title={`In tool group "${g}"`}
                            className="flex-shrink-0 px-1.5 py-px rounded text-[9px] font-medium bg-secondary/10 text-secondary"
                          >
                            {g}
                          </span>
                        ))}
                        {/* Server-reported annotation chips. Undeclared hints
                            render nothing (spec: absent = worst-case, which
                            the detail panel's Hints section spells out). */}
                        {annotationChips(opt.annotations).map((chip) => (
                          <span
                            key={chip.label}
                            title={`${chip.title} (reported by the server, not verified)`}
                            className={cn(
                              'flex-shrink-0 px-1.5 py-px rounded text-[9px] font-medium',
                              CHIP_TONE_CLASS[chip.tone],
                            )}
                          >
                            {chip.label}
                          </span>
                        ))}
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

      <div className="flex items-center gap-2">
        {dirty && (
          <button
            type="button"
            onClick={handleDiscard}
            disabled={isSaving}
            aria-label="Discard unsaved tool changes"
            className="flex-shrink-0 inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-[11px] font-medium border border-border/40 text-text-secondary hover:text-text-primary hover:bg-surface-highlight/50 transition-colors disabled:opacity-50"
          >
            <Undo2 size={11} />
            Discard
          </button>
        )}
        <button
          type="button"
          onClick={handleSave}
          disabled={!dirty || isSaving || saveBlocked}
          aria-label={saveLabel}
          className={cn(
            'flex-1 inline-flex items-center justify-center gap-1.5 rounded-md px-3 py-2 text-[11px] font-medium transition-colors',
            dirty && !isSaving && !saveBlocked
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

// FacetChip is an aria-pressed filter pill (the Metrics ToolsFilterBar idiom).
function FacetChip({
  active,
  onClick,
  disabled,
  tone = 'default',
  title,
  children,
}: {
  active: boolean;
  onClick: () => void;
  disabled?: boolean;
  tone?: 'default' | 'danger';
  title?: string;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-pressed={active}
      title={title}
      className={cn(
        'inline-flex items-center rounded-full border px-2 py-0.5 font-medium transition-colors',
        disabled && 'opacity-40 cursor-not-allowed',
        active
          ? tone === 'danger'
            ? 'bg-status-error/10 text-status-error border-status-error/40'
            : 'bg-primary/15 text-primary border-primary/40'
          : 'bg-background/40 text-text-muted border-border/40 hover:text-text-secondary hover:border-border',
      )}
    >
      {children}
    </button>
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
  // Audit classification for a result row (null when Audit Mode is off or
  // usage hasn't loaded), so cross-server unused hunting works from search.
  auditFor: (server: string, tool: string) => AuditState | null;
}

function GlobalResults({ results, query, serverCount, onPick, auditFor }: GlobalResultsProps) {
  return (
    <div className="px-6 py-4 max-w-4xl space-y-2">
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
          {results.map((r) => {
            const auditState = auditFor(r.server, r.tool);
            return (
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
                    </div>
                  )}
                </div>
                <ChevronRight size={13} className="text-text-muted/50 flex-shrink-0 mt-0.5" />
              </button>
            );
          })}
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
