import { useCallback, useEffect, useMemo, useRef, useState, createElement } from 'react';
import { useSearchParams } from 'react-router';
import { Bot, Download, List, Package, RefreshCw, Search, X } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { IconButton } from '../../ui/IconButton';
import { ConfirmDialog } from '../../ui/ConfirmDialog';
import { showToast } from '../../ui/Toast';
import { WorkspaceShell } from '../../layout/WorkspaceShell';
import { useCommandRegistry } from '../../../hooks/useCommandRegistry';
import { useRegistryStore } from '../../../stores/useRegistryStore';
import { useWizardStore } from '../../../stores/useWizardStore';
import { LibraryKindSwitch } from '../LibraryKindSwitch';
import type { RegistryKind } from '../../../lib/registryKind';
import { AgentCard } from './AgentCard';
import { AgentDetailPanel } from './AgentDetailPanel';
import { AgentEditor } from './AgentEditor';
import { describeSyncResults, needsSync, statusesByAgent } from './agentModel';
import {
  deleteRegistryAgent,
  fetchAgentProjectionStatus,
  fetchRegistryAgents,
  syncAgentProjections,
} from '../../../lib/api';
import type { PaletteCommand } from '../../../types/palette';
import type { RegistryAgent } from '../../../types';

interface AgentsWorkspaceProps {
  /** Switch the Library back to the Skills segment (updates ?kind). */
  onKindChange: (kind: RegistryKind) => void;
}

/**
 * The Agents segment of the Library workspace: a catalog of imported agent
 * definitions with per-client projection state. Agents are definitions
 * gridctl copies or renders into client directories — never executes — so
 * this surface is catalog plus projection, with no lifecycle states, no
 * usage KPIs, and no run chrome.
 */
export function AgentsWorkspace({ onKindChange }: AgentsWorkspaceProps) {
  const [searchParams, setSearchParams] = useSearchParams();
  const agents = useRegistryStore((s) => s.agents);
  const agentStatuses = useRegistryStore((s) => s.agentStatuses);
  const openWizard = useWizardStore((s) => s.open);

  const isLoading = agents === null;
  const hasAgents = (agents ?? []).length > 0;

  // URL state, following the workspace's grammar: defaults omitted,
  // replace-history updates.
  const searchQuery = searchParams.get('q') ?? '';
  const selectedName = searchParams.get('selected');

  const setSearchQuery = useCallback((value: string) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (value.trim()) next.set('q', value);
        else next.delete('q');
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  const setSelectedName = useCallback((name: string | null) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (name) next.set('selected', name);
        else next.delete('selected');
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  const refresh = useCallback(async () => {
    try {
      const [list, statuses] = await Promise.all([
        fetchRegistryAgents(),
        fetchAgentProjectionStatus(),
      ]);
      useRegistryStore.getState().setAgents(list);
      useRegistryStore.getState().setAgentStatuses(statuses);
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Failed to load agents');
    }
  }, []);

  // Segment-entry fetch: agents are not on the global polling cycle, so an
  // agent imported via the CLI appears as soon as the segment mounts (or on
  // Refresh / after wizard import).
  useEffect(() => {
    void refresh();
  }, [refresh]);

  // Deep-link contract for ?selected=: wait for the load to resolve, then
  // toast-and-clear on a miss (mirrors /library/:skillName).
  const lastResolvedRef = useRef<string | null>(null);
  useEffect(() => {
    if (!selectedName) {
      lastResolvedRef.current = null;
      return;
    }
    if (lastResolvedRef.current === selectedName || isLoading) return;
    lastResolvedRef.current = selectedName;
    if (!(agents ?? []).some((a) => a.name === selectedName)) {
      showToast('error', `Agent "${selectedName}" not found`);
      setSelectedName(null);
    }
  }, [selectedName, isLoading, agents, setSelectedName]);

  const handleImport = useCallback(() => openWizard('skill'), [openWizard]);

  // Workspace commands for the Agents segment. Registered under a distinct
  // scope so the skills-segment commands (registered by LibraryWorkspace)
  // never collide.
  const { registerCommands, unregisterCommands } = useCommandRegistry();
  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'library:agents-refresh',
        label: 'Library: Refresh Agents',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(RefreshCw, { size: 14 }),
        keywords: ['reload', 'refresh', 'agents'],
        onSelect: () => void refresh(),
      },
      {
        id: 'library:agents-import',
        label: 'Library: Import from Git',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Download, { size: 14 }),
        keywords: ['import', 'git', 'source', 'agent'],
        onSelect: handleImport,
      },
      {
        id: 'library:view-skills',
        label: 'Library: View Skills',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(List, { size: 14 }),
        keywords: ['skills', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('skill'),
      },
      {
        id: 'library:view-packs',
        label: 'Library: View Packs',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Package, { size: 14 }),
        keywords: ['packs', 'pack', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('pack'),
      },
    ];
    registerCommands('library-agents', commands);
    return () => unregisterCommands('library-agents');
  }, [registerCommands, unregisterCommands, refresh, handleImport, onKindChange]);

  const statusMap = useMemo(() => statusesByAgent(agentStatuses), [agentStatuses]);

  // KPI counts. Always rendered, zero included: the strip itself is the
  // signal that agents support exists on a fresh install. Projected means
  // "has any projection rows" (coverage), not "all in sync" (health —
  // that is what Drifted and the per-row states are for).
  const kpis = useMemo(() => {
    const total = (agents ?? []).length;
    let projected = 0;
    let drifted = 0;
    for (const a of agents ?? []) {
      const rows = statusMap.get(a.name) ?? [];
      if (rows.length > 0) projected++;
      if (rows.some((r) => r.state === 'drifted')) drifted++;
    }
    return { total, projected, drifted };
  }, [agents, statusMap]);

  // Agents with at least one stale or target-missing projection, for the
  // "Sync N stale agents" pill (SyncSourcesButton's proportionate sibling —
  // at 0-10 agents a checkbox bulk bar would be overkill). Restricted to
  // agents still in the catalog: an orphaned lock row (agent deleted out
  // from under its projections) would otherwise 404 the whole named sync.
  const staleAgents = useMemo(() => {
    const known = new Set((agents ?? []).map((a) => a.name));
    const names = new Set<string>();
    for (const s of agentStatuses ?? []) {
      if (needsSync(s) && known.has(s.agent)) names.add(s.agent);
    }
    return [...names].sort();
  }, [agents, agentStatuses]);

  const [syncingStale, setSyncingStale] = useState(false);
  const handleSyncStale = useCallback(async () => {
    if (syncingStale || staleAgents.length === 0) return;
    setSyncingStale(true);
    try {
      const results = await syncAgentProjections({ agents: staleAgents });
      const { kind, message } = describeSyncResults(results);
      showToast(kind, message);
      await refresh();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Sync failed');
    } finally {
      setSyncingStale(false);
    }
  }, [syncingStale, staleAgents, refresh]);

  // Editor + delete state.
  const [editorAgent, setEditorAgent] = useState<RegistryAgent | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  // Esc closes the inspector — but never while any dialog is open (the
  // editor, the delete confirm, or the drift dialog nested inside the
  // projection rows; the role probe covers them all without prop
  // drilling) and never while focus is in a text input.
  useEffect(() => {
    if (!selectedName) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (document.querySelector('[role="dialog"]')) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
      setSelectedName(null);
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [selectedName, setSelectedName]);

  const handleDeleteConfirm = useCallback(async () => {
    if (!confirmDelete) return;
    try {
      await deleteRegistryAgent(confirmDelete);
      showToast('success', 'Agent deleted');
      if (confirmDelete === selectedName) setSelectedName(null);
      await refresh();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Delete failed');
    } finally {
      setConfirmDelete(null);
    }
  }, [confirmDelete, selectedName, setSelectedName, refresh]);

  // Search + source grouping.
  const visibleAgents = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    const list = agents ?? [];
    if (!q) return list;
    return list.filter(
      (a) => a.name.toLowerCase().includes(q) || a.description.toLowerCase().includes(q),
    );
  }, [agents, searchQuery]);

  const groups = useMemo(() => {
    const bySource = new Map<string, RegistryAgent[]>();
    for (const a of visibleAgents) {
      const key = a.source ?? 'My Agents';
      const group = bySource.get(key);
      if (group) group.push(a);
      else bySource.set(key, [a]);
    }
    return [...bySource.entries()]
      .map(([source, members]) => ({
        source,
        members: [...members].sort((a, b) => a.name.localeCompare(b.name)),
      }))
      .sort((a, b) => a.source.localeCompare(b.source));
  }, [visibleAgents]);

  const selectedAgent = useMemo(
    () => (selectedName ? (agents ?? []).find((a) => a.name === selectedName) ?? null : null),
    [selectedName, agents],
  );

  const inspector = (
    <AgentDetailPanel
      agent={selectedAgent}
      statuses={selectedAgent ? statusMap.get(selectedAgent.name) ?? [] : []}
      onClose={() => setSelectedName(null)}
      onEdit={setEditorAgent}
      onDelete={(a) => setConfirmDelete(a.name)}
      onRefresh={refresh}
    />
  );

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell workspace="library" defaultRightPct={30} minRightPx={300} right={inspector}>
        <main className="flex flex-col h-full overflow-hidden">
          <header className="flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle px-6 py-3 flex flex-col gap-2">
            <div className="flex items-center justify-between gap-3">
              <LibraryKindSwitch kind="agent" onChange={onKindChange} />
              <div className="flex items-center gap-2">
                <button
                  onClick={handleImport}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium whitespace-nowrap text-secondary hover:text-secondary/80 border border-secondary/25 hover:bg-secondary/10 rounded-lg transition-colors"
                  title="Import agents from a git repository (discovered from agents/*.md)"
                >
                  <Download size={12} /> Import
                </button>
                {staleAgents.length > 0 && (
                  <button
                    type="button"
                    onClick={() => void handleSyncStale()}
                    disabled={syncingStale}
                    aria-busy={syncingStale}
                    className="inline-flex items-center gap-1.5 text-xs px-3 py-1.5 rounded-lg border border-primary/30 bg-primary/10 text-primary hover:bg-primary/20 transition-colors disabled:opacity-60"
                  >
                    <RefreshCw size={12} aria-hidden="true" className={syncingStale ? 'animate-spin' : undefined} />
                    <span aria-live="polite">
                      {syncingStale
                        ? 'Syncing…'
                        : `Sync ${staleAgents.length} stale agent${staleAgents.length === 1 ? '' : 's'}`}
                    </span>
                  </button>
                )}
                <IconButton icon={RefreshCw} onClick={() => void refresh()} tooltip="Refresh" size="sm" variant="ghost" />
              </div>
            </div>

            {/* KPI strip: rendered unconditionally, zero included, matching
                the Skills segment's contract. No lifecycle or usage KPIs —
                agents have neither. */}
            <div className="flex items-center gap-1.5 flex-wrap" role="group" aria-label="Agent projection summary">
              <AgentKpi label="Total" value={isLoading ? null : kpis.total} />
              <AgentKpi
                label="Projected"
                value={isLoading ? null : kpis.projected}
                detail={isLoading ? undefined : `of ${kpis.total}`}
              />
              <AgentKpi label="Drifted" value={isLoading ? null : kpis.drifted} />
            </div>
          </header>

          {/* Search bar, mirroring the Skills segment's treatment. */}
          <div className="px-4 pt-3 pb-2.5 bg-surface/60 backdrop-blur-sm border-b border-border/40 flex-shrink-0">
            <div className="relative">
              <Search size={13} className="absolute left-3 top-1/2 -translate-y-1/2 text-text-muted/50 pointer-events-none" />
              <input
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search agents…"
                aria-label="Filter agents"
                className="w-full bg-background/60 border border-border/40 rounded-lg pl-9 pr-8 py-2 text-sm text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50 transition-colors"
              />
              {searchQuery && (
                <button
                  onClick={() => setSearchQuery('')}
                  aria-label="Clear search"
                  className="absolute right-2.5 top-1/2 -translate-y-1/2 p-0.5 rounded hover:bg-surface-highlight transition-colors"
                >
                  <X size={13} className="text-text-muted" />
                </button>
              )}
            </div>
          </div>

          <div className="flex-1 overflow-y-auto scrollbar-dark">
            {!isLoading && !hasAgents && (
              <div className="h-full flex flex-col items-center justify-center text-text-muted gap-3 animate-fade-in-scale p-8 text-center">
                <div className="p-4 rounded-xl bg-surface-elevated/50 border border-border/30">
                  <Bot size={32} className="text-text-muted/50" />
                </div>
                <span className="text-sm text-text-secondary">No agents imported</span>
                <span className="text-[11px] text-text-muted max-w-sm">
                  Agents are discovered from <span className="font-mono">agents/*.md</span> in an
                  imported repository, then projected into each client's agents directory.
                </span>
                <button
                  onClick={handleImport}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-primary hover:text-primary/80 bg-primary/10 hover:bg-primary/15 border border-primary/20 rounded-lg transition-colors"
                >
                  <Download size={12} /> Import from git
                </button>
              </div>
            )}

            {!isLoading && hasAgents && visibleAgents.length === 0 && (
              <div className="h-full flex flex-col items-center justify-center text-text-muted gap-3 animate-fade-in-scale p-8">
                <div className="p-4 rounded-xl bg-surface-elevated/50 border border-border/30">
                  <Search size={28} className="text-text-muted/50" />
                </div>
                <span className="text-sm text-text-secondary">No agents match "{searchQuery}"</span>
                <button
                  onClick={() => setSearchQuery('')}
                  className="text-xs text-primary hover:text-primary/80 transition-colors underline underline-offset-2"
                >
                  Clear search
                </button>
              </div>
            )}

            {!isLoading && visibleAgents.length > 0 && (
              <div className="p-4 flex flex-col gap-5">
                {groups.map((group) => (
                  <section key={group.source} aria-label={`Agents from ${group.source}`}>
                    <div className="flex items-center gap-2 mb-2">
                      <span className="text-[10px] uppercase tracking-wider font-medium text-text-muted">
                        {group.source}
                      </span>
                      <span className="text-[10px] text-text-muted/60 font-mono">{group.members.length}</span>
                    </div>
                    <div
                      style={{
                        display: 'grid',
                        gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
                        gap: '12px',
                      }}
                    >
                      {group.members.map((agent) => (
                        <AgentCard
                          key={agent.name}
                          agent={agent}
                          statuses={statusMap.get(agent.name) ?? []}
                          isActive={agent.name === selectedName}
                          onSelect={(a) => setSelectedName(a.name)}
                          onEdit={setEditorAgent}
                          onDelete={(a) => setConfirmDelete(a.name)}
                        />
                      ))}
                    </div>
                  </section>
                ))}
              </div>
            )}
          </div>
        </main>
      </WorkspaceShell>

      <ConfirmDialog
        isOpen={confirmDelete !== null}
        onClose={() => setConfirmDelete(null)}
        onConfirm={() => void handleDeleteConfirm()}
        title="Delete agent"
        message={
          <>
            <p>
              Delete <span className="font-mono text-primary">{confirmDelete}</span> from the
              canonical store?
            </p>
            <p>
              Its projected copies in client directories are removed with it. This action cannot
              be undone.
            </p>
          </>
        }
        confirmLabel={
          <span>
            Delete <span className="font-mono">"{confirmDelete}"</span>
          </span>
        }
        variant="danger"
      />

      <AgentEditor
        isOpen={editorAgent !== null}
        agent={editorAgent}
        onClose={() => setEditorAgent(null)}
        onSaved={refresh}
      />
    </div>
  );
}

/**
 * One agent KPI card. Read-only (agents have no state filter axis), styled
 * to match the Skills segment's KpiCard so the strip reads as the same
 * dashboard. A null value renders a dash while the list loads.
 */
function AgentKpi({ label, value, detail }: { label: string; value: number | null; detail?: string }) {
  return (
    <div className="flex flex-col items-start rounded-lg border bg-background/40 border-border/40 px-3 py-1.5 min-w-[60px]">
      <span className="text-[10px] uppercase tracking-wider font-medium text-text-muted">{label}</span>
      <span className={cn('font-mono leading-none mt-0.5 text-sm text-text-secondary')}>
        {value === null ? '–' : detail ? `${value} ${detail}` : value}
      </span>
    </div>
  );
}
