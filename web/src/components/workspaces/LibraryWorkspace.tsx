import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import {
  AlignJustify,
  BookOpen,
  LayoutGrid,
  List,
  Plus,
  Power,
  PowerOff,
  RefreshCw,
  Search,
  Trash2,
  X,
  type LucideIcon,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { PopoutButton } from '../ui/PopoutButton';
import { SkillEditor } from '../registry/SkillEditor';
import { SkillCardSkeleton } from '../registry/SkillCardSkeleton';
import { LibraryGrid, type GroupMode } from '../registry/LibraryGrid';
import { LibraryTable } from '../registry/LibraryTable';
import { SkillDetailPanel } from '../registry/SkillDetailPanel';
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
  setRegistrySkillsBatch,
} from '../../lib/api';
import { extractRepoInfo } from '../../lib/repo';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import type { AgentSkill, ItemState, SkillSourceStatus } from '../../types';

type FilterTab = 'all' | ItemState;
type SortMode = 'name' | 'state' | 'files';

function isGroupMode(value: string | null): value is GroupMode {
  return value === 'source' || value === 'category' || value === 'none';
}

function isFilterTab(value: string | null): value is FilterTab {
  return value === 'active' || value === 'draft' || value === 'disabled' || value === 'all';
}

function isSortMode(value: string | null): value is SortMode {
  return value === 'name' || value === 'state' || value === 'files';
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
  const sources = useRegistryStore((s) => s.sources);

  // null means "not loaded yet"; the deep-link error toast must wait for a
  // resolved fetch so a transient mid-fetch render doesn't false-positive.
  const isLoading = skills === null;
  const hasSkills = (skills ?? []).length > 0;

  const { openDetachedWindow } = useWindowManager();
  const compact = useUIStore((s) => s.compactMode.library);
  const toggleCompact = useUIStore((s) => s.toggleCompactMode);

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

  // Inspector selection — the skill shown in the right-rail SkillDetailPanel.
  // URL-synced as ?selected=<name> (replace history) so reload and deep-links
  // restore it; kept distinct from /library/:skillName, which opens the editor.
  const selectedName = searchParams.get('selected');
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

  // Sort axis — URL-synced as ?sort, with the default ('name') omitted.
  const sortParam = searchParams.get('sort');
  const sortMode: SortMode = isSortMode(sortParam) ? sortParam : 'name';
  const setSortMode = useCallback((mode: SortMode) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (mode === 'name') next.delete('sort');
        else next.set('sort', mode);
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  // View mode, URL-synced as ?view (cards default omitted), consistent with
  // the workspace's URL-first state model. Row density stays in useUIStore.
  const viewMode: 'cards' | 'table' = searchParams.get('view') === 'table' ? 'table' : 'cards';
  const setViewMode = useCallback((mode: 'cards' | 'table') => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (mode === 'cards') next.delete('view');
        else next.set('view', mode);
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  // Clear every removable facet in one update. Grouping (?group) is a view axis,
  // not a facet, so it is intentionally left untouched.
  const clearAllFacets = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete('q');
        next.delete('filter');
        next.delete('source');
        next.delete('sort');
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  // Editor + delete confirm state
  const [showEditor, setShowEditor] = useState(false);
  const [editingSkill, setEditingSkill] = useState<AgentSkill | undefined>();
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [confirmBulkDelete, setConfirmBulkDelete] = useState(false);

  // Multi-select state. Names only (not skill objects) so the selection
  // survives filter, sort, group, and refresh; stale names are pruned at the
  // point of use against the live skill set.
  const [selectedNames, setSelectedNames] = useState<Set<string>>(() => new Set());
  const toggleSelect = useCallback((skill: AgentSkill) => {
    setSelectedNames((prev) => {
      const next = new Set(prev);
      if (next.has(skill.name)) next.delete(skill.name);
      else next.add(skill.name);
      return next;
    });
  }, []);
  const clearSelection = useCallback(() => setSelectedNames(new Set()), []);

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

  // Esc clears an active multi-selection first, then (on a second press) closes
  // the inspector. Ignored while a modal is open or focus is in a text input
  // (the search box keeps its own clear affordance).
  useEffect(() => {
    if (!selectedName && selectedNames.size === 0) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || showEditor) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
      if (selectedNames.size > 0) {
        setSelectedNames(new Set());
        return;
      }
      setSelectedName(null);
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [selectedName, selectedNames.size, showEditor, setSelectedName]);

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
      // Drop the inspector selection if it pointed at the deleted skill, so the
      // URL doesn't carry a dead ?selected= param.
      if (confirmDelete === selectedName) setSelectedName(null);
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Delete failed');
    } finally {
      setConfirmDelete(null);
    }
  }, [confirmDelete, refreshRegistry, selectedName, setSelectedName]);

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

  // Sort a copy of the visible set (never mutate the source). LibraryGrid
  // buckets in array order, so sorting here also orders within each group;
  // group order itself is decided by the grid and unaffected.
  const sortedSkills = useMemo(() => {
    const byName = (a: AgentSkill, b: AgentSkill) => a.name.localeCompare(b.name);
    const copy = [...visibleSkills];
    if (sortMode === 'state') {
      const order: Record<ItemState, number> = { active: 0, draft: 1, disabled: 2 };
      return copy.sort((a, b) => order[a.state] - order[b.state] || byName(a, b));
    }
    if (sortMode === 'files') {
      return copy.sort((a, b) => b.fileCount - a.fileCount || byName(a, b));
    }
    return copy.sort(byName);
  }, [visibleSkills, sortMode]);

  // Reconcile the selection against the live skill set so a skill deleted out
  // from under the selection never lands in a bulk request (which would reject
  // the whole batch).
  const existingNames = useMemo(() => new Set((skills ?? []).map((s) => s.name)), [skills]);
  const liveSelected = useMemo(
    () => Array.from(selectedNames).filter((n) => existingNames.has(n)),
    [selectedNames, existingNames],
  );
  const allSelected = sortedSkills.length > 0 && sortedSkills.every((s) => selectedNames.has(s.name));
  const someSelected = sortedSkills.some((s) => selectedNames.has(s.name)) && !allSelected;
  const selectAllVisible = useCallback(
    () => setSelectedNames(new Set(sortedSkills.map((s) => s.name))),
    [sortedSkills],
  );

  const handleBulkState = useCallback(async (state: 'active' | 'disabled') => {
    if (liveSelected.length === 0) return;
    try {
      await setRegistrySkillsBatch(liveSelected.map((name) => ({ name, state })));
      const n = liveSelected.length;
      showToast('success', `${n} skill${n === 1 ? '' : 's'} ${state === 'active' ? 'enabled' : 'disabled'}`);
      clearSelection();
      refreshRegistry();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Bulk update failed');
    }
  }, [liveSelected, clearSelection, refreshRegistry]);

  const handleBulkDelete = useCallback(async () => {
    setConfirmBulkDelete(false);
    if (liveSelected.length === 0) return;
    const results = await Promise.allSettled(liveSelected.map((name) => deleteRegistrySkill(name)));
    const ok = results.filter((r) => r.status === 'fulfilled').length;
    showToast(ok === liveSelected.length ? 'success' : 'error', `Deleted ${ok} of ${liveSelected.length} skills`);
    clearSelection();
    refreshRegistry();
  }, [liveSelected, clearSelection, refreshRegistry]);

  // Human-readable label for the active source isolate, shown in its facet chip.
  const activeSourceLabel = useMemo(() => {
    if (!activeSource) return null;
    if (activeSource === 'local') return 'My Skills';
    const src = (sources ?? []).find((s) => s.name === activeSource);
    const info = src ? extractRepoInfo(src.repo) : null;
    return info ? `${info.owner}/${info.repo}` : activeSource;
  }, [activeSource, sources]);

  // Inspector state, derived from the in-store skills (no per-skill fetch). An
  // unknown ?selected= simply yields null → the panel's empty state.
  const selectedSkillObj = useMemo(
    () => (selectedName ? (skills ?? []).find((s) => s.name === selectedName) ?? null : null),
    [selectedName, skills],
  );

  // Other skills sharing the selected skill's top-level category, for the
  // inspector's "Related skills" list.
  const relatedSkills = useMemo(() => {
    if (!selectedSkillObj?.dir) return [];
    const key = selectedSkillObj.dir.split('/')[0];
    return (skills ?? []).filter(
      (s) => s.name !== selectedSkillObj.name && s.dir?.split('/')[0] === key,
    );
  }, [selectedSkillObj, skills]);

  const inspector = (
    <SkillDetailPanel
      skill={selectedSkillObj}
      source={selectedSkillObj ? sourceMap.get(selectedSkillObj.name) : undefined}
      relatedSkills={relatedSkills}
      onClose={() => setSelectedName(null)}
      onEdit={(s) => { setEditingSkill(s); setShowEditor(true); }}
      onToggle={(s) => (s.state === 'active' ? handleDisable(s) : handleEnable(s))}
      onDelete={(s) => setConfirmDelete(s.name)}
      onSelectRelated={(name) => setSelectedName(name)}
    />
  );

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell
        workspace="library"
        defaultRightPct={30}
        minRightPx={300}
        right={inspector}
      >
        <main className="flex flex-col h-full overflow-hidden">
          <LibraryHeader
            onNewSkill={handleNewSkill}
            onRefresh={refreshRegistry}
            onPopout={handlePopout}
            counts={tabCounts}
            activeTab={activeTab}
            onSelectFilter={setActiveTab}
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

            {/* Controls row: sort axis (always), then view controls, plus the
                grouping axis in cards view when sources exist. */}
            <div className="flex items-center justify-between gap-2 flex-wrap">
              <SortControl mode={sortMode} onChange={setSortMode} />
              <div className="flex items-center gap-2 flex-wrap">
                {hasSources && viewMode === 'cards' && (
                  <GroupByControl mode={groupMode} onChange={setGroupMode} />
                )}
                <ViewToggle mode={viewMode} onChange={setViewMode} />
                <IconButton
                  icon={AlignJustify}
                  onClick={() => toggleCompact('library')}
                  tooltip={compact ? 'Comfortable rows' : 'Compact rows'}
                  size="sm"
                  variant={compact ? 'default' : 'ghost'}
                />
              </div>
            </div>

            <FacetChips
              searchQuery={searchQuery}
              activeTab={activeTab}
              sortMode={sortMode}
              sourceLabel={groupMode === 'source' ? activeSourceLabel : null}
              onClearSearch={() => setSearchQuery('')}
              onClearState={() => setActiveTab('all')}
              onClearSource={() => setActiveSource(null)}
              onClearSort={() => setSortMode('name')}
              onClearAll={clearAllFacets}
            />
          </div>

          {liveSelected.length > 0 && (
            <BulkActionBar
              count={liveSelected.length}
              allSelected={allSelected}
              onSelectAll={selectAllVisible}
              onEnable={() => handleBulkState('active')}
              onDisable={() => handleBulkState('disabled')}
              onDelete={() => setConfirmBulkDelete(true)}
              onClear={clearSelection}
            />
          )}

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

            {!isLoading && visibleSkills.length > 0 && viewMode === 'cards' && (
              <LibraryGrid
                skills={sortedSkills}
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
                onSelect={(s) => setSelectedName(s.name)}
                activeSkillName={selectedName}
                selectedNames={selectedNames}
                onToggleSelect={toggleSelect}
              />
            )}

            {!isLoading && visibleSkills.length > 0 && viewMode === 'table' && (
              <LibraryTable
                skills={sortedSkills}
                sortMode={sortMode}
                onSort={setSortMode}
                selectedNames={selectedNames}
                onToggleSelect={toggleSelect}
                onSelectAll={selectAllVisible}
                onClearSelection={clearSelection}
                allSelected={allSelected}
                someSelected={someSelected}
                onSelect={(s) => setSelectedName(s.name)}
                activeSkillName={selectedName}
                sourceMap={sourceMap}
                onEnable={handleEnable}
                onDisable={handleDisable}
                onEdit={(s) => { setEditingSkill(s); setShowEditor(true); }}
                onDelete={(s) => setConfirmDelete(s.name)}
                compact={compact}
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

      <ConfirmDialog
        isOpen={confirmBulkDelete}
        onClose={() => setConfirmBulkDelete(false)}
        onConfirm={handleBulkDelete}
        title="Delete skills"
        message={
          <>
            <p>
              Delete <span className="font-mono text-primary">{liveSelected.length}</span> selected{' '}
              {liveSelected.length === 1 ? 'skill' : 'skills'}?
            </p>
            <p>This action cannot be undone.</p>
          </>
        }
        confirmLabel={
          <span>
            Delete {liveSelected.length} {liveSelected.length === 1 ? 'skill' : 'skills'}
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

const SORT_OPTIONS: { key: SortMode; label: string }[] = [
  { key: 'name', label: 'Name' },
  { key: 'state', label: 'State' },
  { key: 'files', label: 'Files' },
];

const SORT_LABEL: Record<SortMode, string> = { name: 'Name', state: 'State', files: 'Files' };

/** Segmented control to switch the Library's sort axis. */
function SortControl({ mode, onChange }: { mode: SortMode; onChange: (m: SortMode) => void }) {
  return (
    <div className="flex items-center gap-1" role="group" aria-label="Sort skills by">
      <span className="text-[10px] uppercase tracking-wider text-text-muted/60 mr-0.5">Sort</span>
      {SORT_OPTIONS.map((opt) => (
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

interface FacetChipsProps {
  searchQuery: string;
  activeTab: FilterTab;
  sortMode: SortMode;
  sourceLabel: string | null;
  onClearSearch: () => void;
  onClearState: () => void;
  onClearSource: () => void;
  onClearSort: () => void;
  onClearAll: () => void;
}

/**
 * One strip of removable chips, a chip per active non-default facet. Each chip
 * reads from and clears the shared URL state, so it stays in sync with the KPI
 * cards, search box, and grouping control. Renders nothing when no facet is
 * active. This subsumes the former standalone source-isolate chip.
 */
function FacetChips({
  searchQuery,
  activeTab,
  sortMode,
  sourceLabel,
  onClearSearch,
  onClearState,
  onClearSource,
  onClearSort,
  onClearAll,
}: FacetChipsProps) {
  const chips: { key: string; label: string; value: string; onClear: () => void }[] = [];
  if (searchQuery.trim()) chips.push({ key: 'search', label: 'Search', value: searchQuery.trim(), onClear: onClearSearch });
  if (activeTab !== 'all') chips.push({ key: 'state', label: 'State', value: activeTab, onClear: onClearState });
  if (sourceLabel) chips.push({ key: 'source', label: 'Source', value: sourceLabel, onClear: onClearSource });
  if (sortMode !== 'name') chips.push({ key: 'sort', label: 'Sort', value: SORT_LABEL[sortMode], onClear: onClearSort });

  if (chips.length === 0) return null;

  return (
    <div className="flex items-center gap-1.5 flex-wrap" role="group" aria-label="Active filters">
      {chips.map((chip) => (
        <button
          key={chip.key}
          onClick={chip.onClear}
          aria-label={`Clear ${chip.label} filter: ${chip.value}`}
          className="inline-flex items-center gap-1 text-[10px] px-2 py-0.5 rounded-full border border-primary/25 bg-primary/10 text-primary hover:bg-primary/15 transition-colors"
        >
          <span className="text-text-muted">{chip.label}</span>
          <span className="font-medium">{chip.value}</span>
          <X size={11} />
        </button>
      ))}
      {chips.length >= 2 && (
        <button
          onClick={onClearAll}
          aria-label="Clear all filters"
          className="text-[10px] px-2 py-0.5 rounded-full text-text-muted hover:text-text-secondary hover:bg-surface-highlight transition-colors"
        >
          Clear all
        </button>
      )}
    </div>
  );
}

const VIEW_OPTIONS: { key: 'cards' | 'table'; label: string; icon: LucideIcon }[] = [
  { key: 'cards', label: 'Cards', icon: LayoutGrid },
  { key: 'table', label: 'Table', icon: List },
];

/** Segmented toggle between the card grid and the table view. */
function ViewToggle({ mode, onChange }: { mode: 'cards' | 'table'; onChange: (m: 'cards' | 'table') => void }) {
  return (
    <div className="flex items-center gap-1" role="group" aria-label="View mode">
      {VIEW_OPTIONS.map(({ key, label, icon: Icon }) => (
        <button
          key={key}
          onClick={() => onChange(key)}
          aria-pressed={mode === key}
          aria-label={`${label} view`}
          className={cn(
            'p-1.5 rounded-md border transition-colors',
            mode === key
              ? 'bg-primary/10 text-primary border-primary/25'
              : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight border-transparent',
          )}
        >
          <Icon size={13} />
        </button>
      ))}
    </div>
  );
}

interface BulkActionBarProps {
  count: number;
  allSelected: boolean;
  onSelectAll: () => void;
  onEnable: () => void;
  onDisable: () => void;
  onDelete: () => void;
  onClear: () => void;
}

/**
 * Contextual bar shown while a multi-selection is active. The count is an
 * aria-live region so bulk results are announced. Destructive deletes are
 * confirmed upstream via ConfirmDialog.
 */
function BulkActionBar({ count, allSelected, onSelectAll, onEnable, onDisable, onDelete, onClear }: BulkActionBarProps) {
  return (
    <div
      role="region"
      aria-label="Bulk actions"
      className="flex items-center gap-2 flex-wrap px-4 py-2 bg-primary/[0.06] border-b border-primary/20 flex-shrink-0"
    >
      <span aria-live="polite" className="text-xs font-medium text-text-secondary">
        {count} selected
      </span>
      {!allSelected && (
        <button onClick={onSelectAll} className="text-[11px] text-text-muted hover:text-text-secondary transition-colors">
          Select all
        </button>
      )}
      <div className="ml-auto flex items-center gap-1.5">
        <button
          onClick={onEnable}
          className="inline-flex items-center gap-1 px-2 py-1 rounded-md text-[11px] font-medium text-emerald-400 border border-emerald-400/25 hover:bg-emerald-400/10 transition-colors"
        >
          <Power size={11} /> Enable
        </button>
        <button
          onClick={onDisable}
          className="inline-flex items-center gap-1 px-2 py-1 rounded-md text-[11px] font-medium text-text-muted border border-border/40 hover:bg-surface-highlight transition-colors"
        >
          <PowerOff size={11} /> Disable
        </button>
        <button
          onClick={onDelete}
          className="inline-flex items-center gap-1 px-2 py-1 rounded-md text-[11px] font-medium text-red-400 border border-red-400/25 hover:bg-red-400/10 transition-colors"
        >
          <Trash2 size={11} /> Delete
        </button>
        <button
          onClick={onClear}
          aria-label="Clear selection"
          className="inline-flex items-center gap-1 px-2 py-1 rounded-md text-[11px] text-text-muted border border-transparent hover:bg-surface-highlight transition-colors"
        >
          <X size={11} /> Clear
        </button>
      </div>
    </div>
  );
}

// Muted state-color dots, aligned with StateBadge. They are a secondary,
// aria-hidden cue — every KPI is conveyed by its text label + count, so the
// row never relies on color alone.
const STATE_DOT: Record<ItemState, string> = {
  active: 'bg-emerald-400',
  draft: 'bg-amber-400',
  disabled: 'bg-text-muted',
};

// KPI cards double as the state filter: clicking one applies that ?filter, and
// "Total" clears it. Counts are search-aware (they come from tabCounts),
// matching the tab strip these cards replaced. Appending a metric here (e.g.
// failing validation, once that data exists) extends the row without churn.
const KPI_METRICS: { key: FilterTab; label: string; dot: ItemState | null }[] = [
  { key: 'all', label: 'Total', dot: null },
  { key: 'active', label: 'Active', dot: 'active' },
  { key: 'draft', label: 'Draft', dot: 'draft' },
  { key: 'disabled', label: 'Disabled', dot: 'disabled' },
];

interface LibraryHeaderProps {
  onNewSkill: () => void;
  onRefresh: () => void;
  onPopout: () => void;
  counts: Record<FilterTab, number>;
  activeTab: FilterTab;
  onSelectFilter: (tab: FilterTab) => void;
  compact: boolean;
}

function LibraryHeader({ onNewSkill, onRefresh, onPopout, counts, activeTab, onSelectFilter, compact }: LibraryHeaderProps) {
  const registryDetached = useUIStore((s) => s.registryDetached);
  return (
    <header className={cn(
      'flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle px-6 flex flex-col gap-2',
      compact ? 'py-2' : 'py-3',
    )}>
      <div className="flex items-center justify-between gap-3">
        <div className="font-sans text-text-muted/60 text-[10px] uppercase tracking-[0.4em]">
          library
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
      </div>

      {/* KPI summary cards — the first-class dashboard signal, and the primary
          state filter. They replace the old tab strip, so the URL contract
          (?filter) and selected-state live in exactly one place. */}
      <div className="flex items-center gap-1.5 flex-wrap" role="group" aria-label="Filter skills by state">
        {KPI_METRICS.map((metric) => (
          <KpiCard
            key={metric.key}
            label={metric.label}
            count={counts[metric.key]}
            dot={metric.dot}
            active={activeTab === metric.key}
            compact={compact}
            onClick={() => onSelectFilter(metric.key)}
          />
        ))}
      </div>
    </header>
  );
}

interface KpiCardProps {
  label: string;
  count: number;
  dot: ItemState | null;
  active: boolean;
  compact: boolean;
  onClick: () => void;
}

function KpiCard({ label, count, dot, active, compact, onClick }: KpiCardProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      aria-label={`${label} (${count})`}
      className={cn(
        'flex flex-col items-start rounded-lg border transition-colors text-left min-w-[60px]',
        compact ? 'px-2.5 py-1' : 'px-3 py-1.5',
        active
          ? 'bg-primary/10 border-primary/30'
          : 'bg-background/40 border-border/40 hover:border-border hover:bg-surface-highlight',
      )}
    >
      <span className="flex items-center gap-1.5">
        {dot && (
          <span aria-hidden="true" className={cn('inline-block w-1.5 h-1.5 rounded-full flex-shrink-0', STATE_DOT[dot])} />
        )}
        <span className={cn(
          'text-[10px] uppercase tracking-wider font-medium',
          active ? 'text-primary' : 'text-text-muted',
        )}>
          {label}
        </span>
      </span>
      <span className={cn(
        'font-mono leading-none mt-0.5',
        compact ? 'text-xs' : 'text-sm',
        active ? 'text-primary' : 'text-text-secondary',
      )}>
        {count}
      </span>
    </button>
  );
}

export default LibraryWorkspace;
