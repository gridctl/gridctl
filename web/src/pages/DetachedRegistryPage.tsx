import { useState, useCallback, useRef, useMemo } from 'react';
import {
  Library,
  BookOpen,
  Plus,
  RefreshCw,
  Search,
  X,
} from 'lucide-react';
import { cn } from '../lib/cn';
import { IconButton } from '../components/ui/IconButton';
import { ZoomControls } from '../components/ui/ZoomControls';
import { SkillEditor } from '../components/registry/SkillEditor';
import { SkillCardSkeleton } from '../components/registry/SkillCardSkeleton';
import { LibraryGrid } from '../components/registry/LibraryGrid';
import { ConfirmDialog } from '../components/ui/ConfirmDialog';
import { FailedState, FailureBanner } from '../components/ui/EmptyState';
import { showToast } from '../components/ui/Toast';
import { DetachedWindowFrame } from '../components/detached/DetachedWindowFrame';
import { useInterval } from '../hooks/useInterval';
import { useTextZoom } from '../hooks/useTextZoom';
import { useSharedStatus } from '../hooks/useSharedStatus';
import { useFuzzySearch } from '../hooks/useFuzzySearch';
import {
  fetchRegistryStatus,
  fetchRegistrySkills,
  deleteRegistrySkill,
  activateRegistrySkill,
  disableRegistrySkill,
  fetchSkillSources,
} from '../lib/api';
import { POLLING } from '../lib/constants';
import { LIBRARY_TEXT_ZOOM } from '../lib/textZoomDefaults';
import type { AgentSkill, RegistryStatus, ItemState, SkillSourceStatus } from '../types';
import { ErrorBoundary } from '../components/ui/ErrorBoundary';
import { useAuthStore } from '../stores/useAuthStore';
import { describeError, isCancellation } from '../lib/describeError';

type FilterTab = 'all' | ItemState;

const TABS: { key: FilterTab; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'active', label: 'Active' },
  { key: 'draft', label: 'Draft' },
  { key: 'disabled', label: 'Disabled' },
];

function DetachedRegistryContent() {
  const generation = useAuthStore(s => s.generation);
  const authRequired = useAuthStore(s => s.authRequired);
  const [skills, setSkills] = useState<AgentSkill[] | null>(null);
  const [status, setStatus] = useState<RegistryStatus | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [sources, setSources] = useState<SkillSourceStatus[] | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [activeTab, setActiveTab] = useState<FilterTab>('all');

  // Editor state
  const [showEditor, setShowEditor] = useState(false);
  const [editingSkill, setEditingSkill] = useState<AgentSkill | undefined>();

  // Delete confirmation
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  // Text zoom
  const contentRef = useRef<HTMLElement>(null);
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } = useTextZoom({
    storageKey: LIBRARY_TEXT_ZOOM.name,
    defaultSize: LIBRARY_TEXT_ZOOM.defaultSize,
    minSize: LIBRARY_TEXT_ZOOM.minSize,
    maxSize: LIBRARY_TEXT_ZOOM.maxSize,
    step: LIBRARY_TEXT_ZOOM.step,
    containerRef: contentRef,
  });

  const { status: gatewayStatus, settled, error: statusError, refresh: refreshStatus } =
    useSharedStatus({ enabled: !authRequired });

  const fetchData = useCallback(async () => {
    const current = () => useAuthStore.getState().generation === generation && !useAuthStore.getState().authRequired;
    if (!current()) return;
    // Sources only add provenance; a failure keeps the last list and never
    // fails the registry load.
    void fetchSkillSources()
      .then((next) => { if (current()) setSources(next); })
      .catch(() => {});
    try {
      const [regStatus, regSkills] = await Promise.all([
        fetchRegistryStatus(),
        fetchRegistrySkills(),
      ]);
      if (!current()) return;
      setStatus(regStatus);
      setSkills(regSkills);
      setLoadError(null);
      setIsLoading(false);
    } catch (err) {
      // A cancellation keeps the skeleton until the next poll rather than
      // leaving an unloaded page blank.
      if (!current() || isCancellation(err)) return;
      setLoadError(describeError(err, 'Refresh').text);
      setIsLoading(false);
    }
  }, [generation]);

  useInterval(fetchData, POLLING.INVENTORY, { enabled: !authRequired });

  // Fuzzy search runs on all skills; tab filter narrows the result
  const searchResults = useFuzzySearch(skills ?? [], searchQuery);

  // Tab counts from the searched set so they stay in sync with the search query
  const tabCounts = useMemo(() => ({
    all: searchResults.length,
    active: searchResults.filter((s) => s.state === 'active').length,
    draft: searchResults.filter((s) => s.state === 'draft').length,
    disabled: searchResults.filter((s) => s.state === 'disabled').length,
  }), [searchResults]);

  // Apply active tab filter on top of search results
  const displayedSkills = useMemo(() => {
    if (activeTab === 'all') return searchResults;
    return searchResults.filter((s) => s.state === activeTab);
  }, [searchResults, activeTab]);

  const handleEnable = useCallback(async (skill: AgentSkill) => {
    try {
      await activateRegistrySkill(skill.name);
      showToast('success', `Skill "${skill.name}" activated`);
      fetchData();
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'State change');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    }
  }, [fetchData]);

  const handleDisable = useCallback(async (skill: AgentSkill) => {
    try {
      await disableRegistrySkill(skill.name);
      showToast('success', `Skill "${skill.name}" disabled`);
      fetchData();
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'State change');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    }
  }, [fetchData]);

  const handleDeleteConfirm = useCallback(async () => {
    if (!confirmDelete) return;
    try {
      await deleteRegistrySkill(confirmDelete);
      showToast('success', 'Skill deleted');
      fetchData();
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'Delete');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    } finally {
      setConfirmDelete(null);
    }
  }, [confirmDelete, fetchData]);

  // Join skills to sources by name, as the Library workspace does.
  const sourceMap = useMemo(() => {
    const map = new Map<string, SkillSourceStatus>();
    for (const src of sources ?? []) {
      for (const entry of src.skills ?? []) {
        if (!map.has(entry.name)) map.set(entry.name, src);
      }
    }
    return map;
  }, [sources]);

  const hasSkills = (skills ?? []).length > 0;

  // The page's own failure row already reports the outage.
  const lost = (statusError != null || (settled && gatewayStatus == null)) && loadError == null;

  return (
    <DetachedWindowFrame
      windowType="registry"
      header={{
        icon: (
          <div className="p-1.5 rounded-lg bg-primary/10 border border-primary/20">
            <Library size={14} className="text-primary" />
          </div>
        ),
        start: (
          <div>
            <span className="text-sm font-semibold text-text-primary tracking-tight">Library</span>
            <span className="text-3xs text-text-muted uppercase tracking-wider ml-2">Skills & agents</span>
          </div>
        ),
        actions: (
          <>
            <ZoomControls
              fontSize={fontSize}
              onZoomIn={zoomIn}
              onZoomOut={zoomOut}
              onReset={resetZoom}
              isMin={isMin}
              isMax={isMax}
              isDefault={isDefault}
            />
            <button
              type="button"
              onClick={() => { setEditingSkill(undefined); setShowEditor(true); }}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-primary hover:text-primary/80 bg-primary/10 hover:bg-primary/15 border border-primary/20 rounded-lg transition-colors"
            >
              <Plus size={12} /> New Skill
            </button>
            <IconButton icon={RefreshCw} onClick={fetchData} tooltip="Refresh" size="sm" variant="ghost" />
          </>
        ),
      }}
      connection={{ lost, message: statusError ?? undefined, onRetry: () => void refreshStatus() }}
      footerStart={
        <span>
          {status ? `${status.totalSkills} total` : ''}
          {status ? ` · ` : ''}
          <span className="text-status-running">{status?.activeSkills ?? 0} active</span>
        </span>
      }
    >
      {/* Search + Filter bar */}
      <div className="px-4 pt-3 pb-2.5 bg-surface/60 backdrop-blur-sm border-b border-border/40 flex flex-col gap-2 z-raised flex-shrink-0">
        {/* Search input */}
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
              type="button"
              onClick={() => setSearchQuery('')}
              aria-label="Clear search"
              className="absolute right-2.5 top-1/2 -translate-y-1/2 p-0.5 rounded hover:bg-surface-highlight transition-colors"
            >
              <X size={13} className="text-text-muted" />
            </button>
          )}
        </div>

        {/* Filter tabs */}
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
                  'text-3xs px-1.5 py-0 rounded-full font-mono min-w-[18px] text-center transition-colors',
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
      </div>

      {/* Content */}
      <main
        ref={contentRef}
        className="flex-1 overflow-y-auto scrollbar-dark relative z-raised"
        style={{ '--log-font-size': `${fontSize}px` } as React.CSSProperties}
      >
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

        {!isLoading && loadError && skills === null && (
          <FailedState feature="Library" message={loadError} onRetry={fetchData} />
        )}

        {!isLoading && loadError && skills !== null && (
          <FailureBanner message={loadError} onRetry={fetchData} />
        )}

        {/* No skills at all */}
        {!isLoading && !loadError && skills !== null && !hasSkills && (
          <div className="h-full flex flex-col items-center justify-center text-text-muted gap-3 animate-fade-in-scale">
            <div className="p-4 rounded-xl bg-surface-elevated/50 border border-border/30">
              <BookOpen size={32} className="text-text-muted/50" />
            </div>
            <span className="text-sm">No skills registered</span>
            <span className="text-3xs text-text-muted">Create a SKILL.md to get started</span>
          </div>
        )}

        {/* Has skills but nothing matches current filter + search */}
        {!isLoading && hasSkills && displayedSkills.length === 0 && (
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

        {/* Card grid */}
        {!isLoading && displayedSkills.length > 0 && (
          <LibraryGrid
            skills={displayedSkills}
            hasSearch={searchQuery.length > 0}
            sourceMap={sourceMap}
            onEnable={handleEnable}
            onDisable={handleDisable}
            onEdit={(s) => { setEditingSkill(s); setShowEditor(true); }}
            onDelete={(s) => setConfirmDelete(s.name)}
          />
        )}
      </main>

      {/* Delete confirmation */}
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

      {/* Editor modal */}
      <SkillEditor
        isOpen={showEditor}
        onClose={() => { setShowEditor(false); setEditingSkill(undefined); }}
        onSaved={fetchData}
        skill={editingSkill}
        source={editingSkill ? sourceMap.get(editingSkill.name) : undefined}
      />
    </DetachedWindowFrame>
  );
}

export function DetachedRegistryPage() {
  return (
    <ErrorBoundary variant="window">
      <DetachedRegistryContent />
    </ErrorBoundary>
  );
}
