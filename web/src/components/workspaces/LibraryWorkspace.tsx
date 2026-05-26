import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import {
  BookOpen,
  Plus,
  RefreshCw,
  Search,
  X,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { PopoutButton } from '../ui/PopoutButton';
import { SkillEditor } from '../registry/SkillEditor';
import { SkillCardSkeleton } from '../registry/SkillCardSkeleton';
import { LibraryGrid, type GroupMode } from '../registry/LibraryGrid';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { showToast } from '../ui/Toast';
import { useFuzzySearch } from '../../hooks/useFuzzySearch';
import { useWindowManager } from '../../hooks/useWindowManager';
import { useRegistryStore } from '../../stores/useRegistryStore';
import { useUIStore } from '../../stores/useUIStore';
import { useLibraryCommands, type LibraryFilter } from '../library/useLibraryCommands';
import {
  activateRegistrySkill,
  deleteRegistrySkill,
  disableRegistrySkill,
  fetchRegistrySkills,
  fetchRegistryStatus,
  fetchSkillSources,
} from '../../lib/api';
import { extractRepoInfo } from '../../lib/repo';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import type { AgentSkill, ItemState, SkillSourceStatus } from '../../types';

type FilterTab = 'all' | ItemState;

function isGroupMode(value: string | null): value is GroupMode {
  return value === 'source' || value === 'category' || value === 'none';
}

const TABS: { key: FilterTab; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'active', label: 'Active' },
  { key: 'draft', label: 'Draft' },
  { key: 'disabled', label: 'Disabled' },
];

function isFilterTab(value: string | null): value is FilterTab {
  return value === 'active' || value === 'draft' || value === 'disabled' || value === 'all';
}

/**
 * LibraryWorkspace is the catalog view of the skill registry, hosted inside
 * the unified AppShell. Search and filter state mirror the URL so reload,
 * back/forward, and deep-links all preserve the user's view. The
 * `/library/:skillName` path mounts the editor for that skill if it exists,
 * else surfaces a toast and falls back to `/library`.
 */
export function LibraryWorkspace() {
  const [searchParams, setSearchParams] = useSearchParams();
  const { skillName } = useParams<{ skillName?: string }>();
  const navigate = useNavigate();

  // Registry is fetched by the global usePolling cycle in AppShell — read from
  // the store instead of starting a second polling loop here.
  const skills = useRegistryStore((s) => s.skills);
  const status = useRegistryStore((s) => s.status);
  const sources = useRegistryStore((s) => s.sources);

  // null means "not loaded yet"; the deep-link error toast must wait for a
  // resolved fetch so a transient mid-fetch render doesn't false-positive.
  const isLoading = skills === null;
  const hasSkills = (skills ?? []).length > 0;

  const { openDetachedWindow } = useWindowManager();
  const compact = useUIStore((s) => s.compactMode.library);

  // URL → local state for the search input. We round-trip through state so the
  // input keeps caret behavior; the URL is the source of truth on reload.
  const searchQuery = searchParams.get('q') ?? '';
  const filterParam = searchParams.get('filter');
  const activeTab: FilterTab = isFilterTab(filterParam) ? filterParam : 'all';

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

  const setActiveTab = useCallback((tab: FilterTab) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (tab === 'all') next.delete('filter');
        else next.set('filter', tab);
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  // Provenance grouping state. The default grouping is Source when any source
  // exists, else Category (today's behavior) — so the default value is omitted
  // from the URL and a no-source registry is untouched.
  const hasSources = (sources ?? []).length > 0;
  const defaultGroup: GroupMode = hasSources ? 'source' : 'category';
  const groupParam = searchParams.get('group');
  const groupMode: GroupMode = isGroupMode(groupParam) ? groupParam : defaultGroup;
  const sourceParam = searchParams.get('source');
  const activeSource = groupMode === 'source' ? sourceParam : null;

  // Join skills to sources by skill name. Verified against internal/api/skills.go:
  // source entries are built from the same registry store, so SkillSourceEntry.name
  // === AgentSkill.name. Caveat (deferred): a skill kept after its source is removed,
  // or a name shared across sources, maps to the first owning source or "My Skills".
  const sourceMap = useMemo(() => {
    const map = new Map<string, SkillSourceStatus>();
    for (const src of sources ?? []) {
      for (const entry of src.skills ?? []) {
        if (!map.has(entry.name)) map.set(entry.name, src);
      }
    }
    return map;
  }, [sources]);

  const setGroupMode = useCallback((mode: GroupMode) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (mode === defaultGroup) next.delete('group');
        else next.set('group', mode);
        if (mode !== 'source') next.delete('source'); // isolate only applies in source mode
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams, defaultGroup]);

  const setActiveSource = useCallback((key: string | null) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (!key) next.delete('source');
        else next.set('source', key);
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  // Editor + delete confirm state
  const [showEditor, setShowEditor] = useState(false);
  const [editingSkill, setEditingSkill] = useState<AgentSkill | undefined>();
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  // Track the last skillName we resolved so we don't refire the toast on every
  // re-render. Reset whenever the param actually changes.
  const lastResolvedSkillRef = useRef<string | null>(null);

  // Deep-link resolution: /library/:skillName mounts the editor for that
  // skill once the registry has loaded. Unknown name → toast + replace URL.
  useEffect(() => {
    if (!skillName) {
      lastResolvedSkillRef.current = null;
      return;
    }
    if (lastResolvedSkillRef.current === skillName) return;
    if (isLoading) return;

    const found = (skills ?? []).find((s) => s.name === skillName);
    lastResolvedSkillRef.current = skillName;
    if (found) {
      setEditingSkill(found);
      setShowEditor(true);
    } else {
      showToast('error', `Skill "${skillName}" not found`);
      navigate('/library', { replace: true });
    }
  }, [skillName, isLoading, skills, navigate]);

  // When the editor closes, drop the :skillName segment so the URL reflects
  // the catalog view again. Search/filter query params survive.
  const handleEditorClose = useCallback(() => {
    setShowEditor(false);
    setEditingSkill(undefined);
    if (skillName) {
      navigate({ pathname: '/library', search: searchParams.toString() }, { replace: true });
    }
  }, [navigate, searchParams, skillName]);

  const refreshRegistry = useCallback(async () => {
    try {
      const [regStatus, regSkills] = await Promise.all([
        fetchRegistryStatus(),
        fetchRegistrySkills(),
      ]);
      useRegistryStore.getState().setStatus(regStatus);
      useRegistryStore.getState().setSkills(regSkills);
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Refresh failed');
    }
  }, []);

  // Refresh registry + sources together — used after an inline source update so
  // the "update available" badge clears and any new/removed skills appear.
  const refreshAll = useCallback(async () => {
    await refreshRegistry();
    try {
      const srcs = await fetchSkillSources();
      useRegistryStore.getState().setSources(srcs);
    } catch {
      // Sources unavailable — progressive disclosure.
    }
  }, [refreshRegistry]);

  const handleEnable = useCallback(async (skill: AgentSkill) => {
    try {
      await activateRegistrySkill(skill.name);
      showToast('success', `Skill "${skill.name}" activated`);
      refreshRegistry();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'State change failed');
    }
  }, [refreshRegistry]);

  const handleDisable = useCallback(async (skill: AgentSkill) => {
    try {
      await disableRegistrySkill(skill.name);
      showToast('success', `Skill "${skill.name}" disabled`);
      refreshRegistry();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'State change failed');
    }
  }, [refreshRegistry]);

  const handleDeleteConfirm = useCallback(async () => {
    if (!confirmDelete) return;
    try {
      await deleteRegistrySkill(confirmDelete);
      showToast('success', 'Skill deleted');
      refreshRegistry();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Delete failed');
    } finally {
      setConfirmDelete(null);
    }
  }, [confirmDelete, refreshRegistry]);

  const handleNewSkill = useCallback(() => {
    setEditingSkill(undefined);
    setShowEditor(true);
  }, []);

  const handlePopout = useCallback(() => {
    openDetachedWindow('registry');
  }, [openDetachedWindow]);

  const handleShowAll = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete('q');
        next.delete('filter');
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  const handleFilter = useCallback((value: LibraryFilter) => {
    if (value === 'all') {
      setActiveTab('all');
    } else {
      setActiveTab(value);
    }
  }, [setActiveTab]);

  useLibraryCommands({
    onNewSkill: handleNewSkill,
    onRefresh: refreshRegistry,
    onShowAll: handleShowAll,
    onFilter: handleFilter,
    onOpenInNewWindow: handlePopout,
    onSetGroup: setGroupMode,
  });

  const searchResults = useFuzzySearch(skills ?? [], searchQuery);

  const tabCounts = useMemo(() => ({
    all: searchResults.length,
    active: searchResults.filter((s) => s.state === 'active').length,
    draft: searchResults.filter((s) => s.state === 'draft').length,
    disabled: searchResults.filter((s) => s.state === 'disabled').length,
  }), [searchResults]);

  const displayedSkills = useMemo(() => {
    if (activeTab === 'all') return searchResults;
    return searchResults.filter((s) => s.state === activeTab);
  }, [searchResults, activeTab]);

  // Apply the provenance isolate on top of search + tab filtering, so empty
  // states and the grid see a coherent set.
  const visibleSkills = useMemo(() => {
    if (groupMode !== 'source' || !activeSource) return displayedSkills;
    return displayedSkills.filter((s) => {
      const src = sourceMap.get(s.name);
      return activeSource === 'local' ? !src : src?.name === activeSource;
    });
  }, [displayedSkills, groupMode, activeSource, sourceMap]);

  // Human-readable label for the active isolate, shown in the "Show all" chip.
  const activeSourceLabel = useMemo(() => {
    if (!activeSource) return null;
    if (activeSource === 'local') return 'My Skills';
    const src = (sources ?? []).find((s) => s.name === activeSource);
    const info = src ? extractRepoInfo(src.repo) : null;
    return info ? `${info.owner}/${info.repo}` : activeSource;
  }, [activeSource, sources]);

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell workspace="library" defaultLeftPct={0} defaultRightPct={0}>
        <main className="flex flex-col h-full overflow-hidden">
          <LibraryHeader
            onNewSkill={handleNewSkill}
            onRefresh={refreshRegistry}
            onPopout={handlePopout}
            totalSkills={status?.totalSkills ?? skills?.length ?? 0}
            activeSkills={status?.activeSkills ?? 0}
            compact={compact}
          />

          {/* Search + Filter bar */}
          <div className="px-4 pt-3 pb-2.5 bg-surface/60 backdrop-blur-sm border-b border-border/40 flex flex-col gap-2 flex-shrink-0">
            <div className="relative">
              <Search size={13} className="absolute left-3 top-1/2 -translate-y-1/2 text-text-muted/50 pointer-events-none" />
              <input
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search skills…"
                aria-label="Filter skills"
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

            <div className="flex items-center justify-between gap-2 flex-wrap">
              <div className="flex gap-1 flex-wrap">
                {TABS.map((tab) => (
                  <button
                    key={tab.key}
                    onClick={() => setActiveTab(tab.key)}
                    className={cn(
                      'flex items-center gap-1.5 px-3 py-1 rounded-lg text-xs font-medium transition-colors',
                      activeTab === tab.key
                        ? 'bg-primary/10 text-primary border border-primary/25'
                        : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight border border-transparent',
                    )}
                  >
                    {tab.label}
                    <span
                      className={cn(
                        'text-[10px] px-1.5 py-0 rounded-full font-mono min-w-[18px] text-center transition-colors',
                        activeTab === tab.key
                          ? 'bg-primary/15 text-primary'
                          : 'bg-surface-highlight text-text-muted',
                      )}
                    >
                      {tabCounts[tab.key]}
                    </span>
                  </button>
                ))}
              </div>
              {hasSources && (
                <GroupByControl mode={groupMode} onChange={setGroupMode} />
              )}
            </div>

            {groupMode === 'source' && activeSource && activeSourceLabel && (
              <div className="flex">
                <button
                  onClick={() => setActiveSource(null)}
                  aria-label={`Showing ${activeSourceLabel} only — show all groups`}
                  className="inline-flex items-center gap-1 text-[10px] px-2 py-0.5 rounded-full border border-primary/25 bg-primary/10 text-primary hover:bg-primary/15 transition-colors"
                >
                  <span className="text-text-muted">Showing</span>
                  <span className="font-medium">{activeSourceLabel}</span>
                  <X size={11} />
                </button>
              </div>
            )}
          </div>

          <div className="flex-1 overflow-y-auto scrollbar-dark">
            {isLoading && (
              <div
                className="p-4"
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
                  gap: '12px',
                }}
              >
                {Array.from({ length: 8 }).map((_, i) => (
                  <SkillCardSkeleton key={i} />
                ))}
              </div>
            )}

            {!isLoading && !hasSkills && (
              <div className="h-full flex flex-col items-center justify-center text-text-muted gap-3 animate-fade-in-scale">
                <div className="p-4 rounded-xl bg-surface-elevated/50 border border-border/30">
                  <BookOpen size={32} className="text-text-muted/50" />
                </div>
                <span className="text-sm">No skills registered</span>
                <span className="text-[10px] text-text-muted">Create a SKILL.md to get started</span>
              </div>
            )}

            {!isLoading && hasSkills && visibleSkills.length === 0 && (
              <div className="h-full flex flex-col items-center justify-center text-text-muted gap-3 animate-fade-in-scale p-8">
                <div className="p-4 rounded-xl bg-surface-elevated/50 border border-border/30">
                  <Search size={28} className="text-text-muted/50" />
                </div>
                <span className="text-sm text-text-secondary">
                  No skills match{searchQuery ? ` "${searchQuery}"` : ' this filter'}
                </span>
                {searchQuery && (
                  <button
                    onClick={() => setSearchQuery('')}
                    className="text-xs text-primary hover:text-primary/80 transition-colors underline underline-offset-2"
                  >
                    Clear search
                  </button>
                )}
              </div>
            )}

            {!isLoading && visibleSkills.length > 0 && (
              <LibraryGrid
                skills={visibleSkills}
                hasSearch={searchQuery.length > 0}
                groupMode={groupMode}
                sourceMap={sourceMap}
                activeSource={activeSource}
                onIsolateSource={setActiveSource}
                onRefresh={refreshAll}
                onEnable={handleEnable}
                onDisable={handleDisable}
                onEdit={(s) => { setEditingSkill(s); setShowEditor(true); }}
                onDelete={(s) => setConfirmDelete(s.name)}
              />
            )}
          </div>
        </main>
      </WorkspaceShell>

      <ConfirmDialog
        isOpen={confirmDelete !== null}
        onClose={() => setConfirmDelete(null)}
        onConfirm={handleDeleteConfirm}
        title="Delete skill"
        message={
          <>
            <p>
              Delete <span className="font-mono text-primary">{confirmDelete}</span>?
            </p>
            <p>This action cannot be undone.</p>
          </>
        }
        confirmLabel={
          <span>
            Delete <span className="font-mono">"{confirmDelete}"</span>
          </span>
        }
        variant="danger"
      />

      <SkillEditor
        isOpen={showEditor}
        onClose={handleEditorClose}
        onSaved={refreshRegistry}
        skill={editingSkill}
      />
    </div>
  );
}

const GROUP_OPTIONS: { key: GroupMode; label: string }[] = [
  { key: 'source', label: 'Source' },
  { key: 'category', label: 'Category' },
  { key: 'none', label: 'None' },
];

/** Segmented control to switch the Library's grouping axis. */
function GroupByControl({ mode, onChange }: { mode: GroupMode; onChange: (m: GroupMode) => void }) {
  return (
    <div className="flex items-center gap-1" role="group" aria-label="Group skills by">
      <span className="text-[10px] uppercase tracking-wider text-text-muted/60 mr-0.5">Group</span>
      {GROUP_OPTIONS.map((opt) => (
        <button
          key={opt.key}
          onClick={() => onChange(opt.key)}
          aria-pressed={mode === opt.key}
          className={cn(
            'px-2 py-1 rounded-md text-[11px] font-medium transition-colors',
            mode === opt.key
              ? 'bg-primary/10 text-primary border border-primary/25'
              : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight border border-transparent',
          )}
        >
          {opt.label}
        </button>
      ))}
    </div>
  );
}

interface LibraryHeaderProps {
  onNewSkill: () => void;
  onRefresh: () => void;
  onPopout: () => void;
  totalSkills: number;
  activeSkills: number;
  compact: boolean;
}

function LibraryHeader({ onNewSkill, onRefresh, onPopout, totalSkills, activeSkills, compact }: LibraryHeaderProps) {
  const registryDetached = useUIStore((s) => s.registryDetached);
  return (
    <header className={cn(
      'flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle flex items-center justify-between px-6',
      compact ? 'py-2' : 'py-3',
    )}>
      <div className="flex items-center gap-3">
        <div className="font-sans text-text-muted/60 text-[10px] uppercase tracking-[0.4em]">
          library
        </div>
        <div className="font-mono text-[10px] text-text-muted">
          {totalSkills} {totalSkills === 1 ? 'skill' : 'skills'}
        </div>
        {activeSkills > 0 && (
          <div className="font-mono text-[10px] text-status-running">
            {activeSkills} active
          </div>
        )}
      </div>
      <div className="flex items-center gap-2">
        <button
          onClick={onNewSkill}
          className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-primary hover:text-primary/80 bg-primary/10 hover:bg-primary/15 border border-primary/20 rounded-lg transition-colors"
        >
          <Plus size={12} /> New Skill
        </button>
        <IconButton icon={RefreshCw} onClick={onRefresh} tooltip="Refresh" size="sm" variant="ghost" />
        <PopoutButton onClick={onPopout} disabled={registryDetached} tooltip="Open in new window" />
      </div>
    </header>
  );
}

export default LibraryWorkspace;
