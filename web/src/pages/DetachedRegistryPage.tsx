import { useEffect, useState, useCallback, useRef, useMemo, Component, type ReactNode } from 'react';
import {
  Library,
  BookOpen,
  Plus,
  RefreshCw,
  AlertCircle,
  Search,
  X,
} from 'lucide-react';
import { cn } from '../lib/cn';
import { IconButton } from '../components/ui/IconButton';
import { ZoomControls } from '../components/log/ZoomControls';
import { SkillEditor } from '../components/registry/SkillEditor';
import { SkillCard } from '../components/registry/SkillCard';
import { SkillCardSkeleton } from '../components/registry/SkillCardSkeleton';
import { ToastContainer, showToast } from '../components/ui/Toast';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { useLogFontSize } from '../hooks/useLogFontSize';
import { useFuzzySearch } from '../hooks/useFuzzySearch';
import {
  fetchRegistryStatus,
  fetchRegistrySkills,
  deleteRegistrySkill,
  activateRegistrySkill,
  disableRegistrySkill,
} from '../lib/api';
import { POLLING } from '../lib/constants';
import type { AgentSkill, RegistryStatus, ItemState } from '../types';

// Error boundary for detached window
interface ErrorBoundaryState {
  hasError: boolean;
  error: Error | null;
}

class DetachedErrorBoundary extends Component<{ children: ReactNode }, ErrorBoundaryState> {
  constructor(props: { children: ReactNode }) {
    super(props);
    this.state = { hasError: false, error: null };
  }

  static getDerivedStateFromError(error: Error) {
    return { hasError: true, error };
  }

  render() {
    if (this.state.hasError) {
      return (
        <div className="h-screen w-screen bg-background flex items-center justify-center">
          <div className="text-center p-8 max-w-md">
            <div className="p-4 rounded-xl bg-status-error/10 border border-status-error/20 inline-block mb-4">
              <AlertCircle size={32} className="text-status-error" />
            </div>
            <h1 className="text-lg text-status-error mb-2">Something went wrong</h1>
            <pre className="text-xs text-text-muted bg-surface p-4 rounded-lg overflow-auto max-h-32 mb-4">
              {this.state.error?.message}
            </pre>
            <button
              onClick={() => window.location.reload()}
              className="px-4 py-2 bg-primary text-background rounded-lg font-medium hover:bg-primary/90 transition-colors"
            >
              Reload Window
            </button>
          </div>
        </div>
      );
    }
    return this.props.children;
  }
}

type FilterTab = 'all' | ItemState;

const TABS: { key: FilterTab; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'active', label: 'Active' },
  { key: 'draft', label: 'Draft' },
  { key: 'disabled', label: 'Disabled' },
];

function getGroupKey(dir?: string): string {
  if (!dir) return '';
  return dir.split('/')[0];
}

function groupSkills(skills: AgentSkill[]): Map<string, AgentSkill[]> {
  const groups = new Map<string, AgentSkill[]>();
  for (const skill of skills) {
    const key = getGroupKey(skill.dir);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key)!.push(skill);
  }
  return groups;
}

function toTitleCase(key: string): string {
  return key.replace(/-/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

interface GroupedSkillGridProps {
  skills: AgentSkill[];
  hasSearch: boolean;
  onEnable: (skill: AgentSkill) => void;
  onDisable: (skill: AgentSkill) => void;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (skill: AgentSkill) => void;
}

function GroupedSkillGrid({ skills, hasSearch, onEnable, onDisable, onEdit, onDelete }: GroupedSkillGridProps) {
  const groups = groupSkills(skills);
  const showHeaders = groups.size > 1;

  const gridStyle: React.CSSProperties = {
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
    gap: '12px',
  };

  return (
    <div className="p-4" style={gridStyle}>
      {Array.from(groups.entries()).map(([key, groupSkillList]) => (
        <GroupSection
          key={key || '__ungrouped__'}
          groupKey={key}
          skills={groupSkillList}
          showHeader={showHeaders}
          hasSearch={hasSearch}
          onEnable={onEnable}
          onDisable={onDisable}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      ))}
    </div>
  );
}

interface GroupSectionProps {
  groupKey: string;
  skills: AgentSkill[];
  showHeader: boolean;
  hasSearch: boolean;
  onEnable: (skill: AgentSkill) => void;
  onDisable: (skill: AgentSkill) => void;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (skill: AgentSkill) => void;
}

function GroupSection({ groupKey, skills, showHeader, hasSearch, onEnable, onDisable, onEdit, onDelete }: GroupSectionProps) {
  return (
    <>
      {showHeader && (
        <div style={{ gridColumn: '1 / -1' }} className="flex flex-col gap-1 mt-2 first:mt-0 animate-fade-in-scale">
          <div className="flex items-center justify-between">
            <span className="text-[10px] uppercase tracking-widest text-text-muted font-medium">
              {groupKey ? toTitleCase(groupKey) : 'Other'}
            </span>
            <span className="text-[10px] px-1.5 rounded-full bg-surface-highlight text-text-muted">
              {skills.length} {hasSearch ? 'matched' : 'skills'}
            </span>
          </div>
          <div className="border-b border-border/30" />
        </div>
      )}
      {skills.map((skill) => (
        <SkillCard
          key={skill.name}
          skill={skill}
          className={cn('animate-fade-in-scale', skill.metadata?.colspan === '2' ? 'col-span-2' : undefined)}
          onEnable={onEnable}
          onDisable={onDisable}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      ))}
    </>
  );
}

function DetachedRegistryContent() {
  const [skills, setSkills] = useState<AgentSkill[] | null>(null);
  const [status, setStatus] = useState<RegistryStatus | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [searchQuery, setSearchQuery] = useState('');
  const [activeTab, setActiveTab] = useState<FilterTab>('all');

  // Editor state
  const [showEditor, setShowEditor] = useState(false);
  const [editingSkill, setEditingSkill] = useState<AgentSkill | undefined>();

  // Delete confirmation
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  // Text zoom
  const contentRef = useRef<HTMLElement>(null);
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } =
    useLogFontSize(contentRef);

  // Register with main window
  useDetachedWindowSync('registry');

  const fetchData = useCallback(async () => {
    try {
      const [regStatus, regSkills] = await Promise.all([
        fetchRegistryStatus(),
        fetchRegistrySkills(),
      ]);
      setStatus(regStatus);
      setSkills(regSkills);
      setIsLoading(false);
    } catch {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    const initFetch = async () => {
      await fetchData();
    };
    initFetch();
    const interval = window.setInterval(fetchData, POLLING.STATUS);
    return () => clearInterval(interval);
  }, [fetchData]);

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
      showToast('error', err instanceof Error ? err.message : 'State change failed');
    }
  }, [fetchData]);

  const handleDisable = useCallback(async (skill: AgentSkill) => {
    try {
      await disableRegistrySkill(skill.name);
      showToast('success', `Skill "${skill.name}" disabled`);
      fetchData();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'State change failed');
    }
  }, [fetchData]);

  const handleDeleteConfirm = useCallback(async () => {
    if (!confirmDelete) return;
    try {
      await deleteRegistrySkill(confirmDelete);
      showToast('success', 'Skill deleted');
      fetchData();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Delete failed');
    } finally {
      setConfirmDelete(null);
    }
  }, [confirmDelete, fetchData]);

  const hasSkills = (skills ?? []).length > 0;

  return (
    <div className="h-screen w-screen bg-background flex flex-col overflow-hidden relative">
      {/* Background grain */}
      <div
        className="fixed inset-0 pointer-events-none z-0 opacity-[0.015]"
        style={{
          backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noise'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noise)'/%3E%3C/svg%3E")`,
        }}
      />

      {/* Header */}
      <header className="h-12 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-b border-border/50 flex items-center justify-between px-4 z-10 relative">
        <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/30 to-transparent" />

        <div className="flex items-center gap-3">
          <div className="p-1.5 rounded-lg bg-primary/10 border border-primary/20">
            <Library size={14} className="text-primary" />
          </div>
          <div>
            <span className="text-sm font-semibold text-text-primary tracking-tight">Registry</span>
            <span className="text-[10px] text-text-muted uppercase tracking-wider ml-2">Agent Skills</span>
          </div>
        </div>

        <div className="flex items-center gap-2">
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
            onClick={() => { setEditingSkill(undefined); setShowEditor(true); }}
            className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-primary hover:text-primary/80 bg-primary/10 hover:bg-primary/15 border border-primary/20 rounded-lg transition-colors"
          >
            <Plus size={12} /> New Skill
          </button>
          <IconButton icon={RefreshCw} onClick={fetchData} tooltip="Refresh" size="sm" variant="ghost" />
        </div>
      </header>

      {/* Search + Filter bar */}
      <div className="px-4 pt-3 pb-2.5 bg-surface/60 backdrop-blur-sm border-b border-border/40 flex flex-col gap-2 z-10 flex-shrink-0">
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
              onClick={() => setSearchQuery('')}
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
      </div>

      {/* Content */}
      <main
        ref={contentRef}
        className="flex-1 overflow-y-auto scrollbar-dark relative z-10"
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

        {/* No skills at all */}
        {!isLoading && !hasSkills && (
          <div className="h-full flex flex-col items-center justify-center text-text-muted gap-3 animate-fade-in-scale">
            <div className="p-4 rounded-xl bg-surface-elevated/50 border border-border/30">
              <BookOpen size={32} className="text-text-muted/50" />
            </div>
            <span className="text-sm">No skills registered</span>
            <span className="text-[10px] text-text-muted/60">Create a SKILL.md to get started</span>
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
          <GroupedSkillGrid
            skills={displayedSkills}
            hasSearch={searchQuery.length > 0}
            onEnable={handleEnable}
            onDisable={handleDisable}
            onEdit={(s) => { setEditingSkill(s); setShowEditor(true); }}
            onDelete={(s) => setConfirmDelete(s.name)}
          />
        )}
      </main>

      {/* Status footer */}
      <footer className="h-6 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-t border-border/50 flex items-center justify-between px-4 text-[10px] text-text-muted z-10">
        <span>
          {status ? `${status.totalSkills} total` : ''}
          {status ? ` \u00B7 ` : ''}
          <span className="text-status-running">{status?.activeSkills ?? 0} active</span>
        </span>
        <span className="flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse" />
          Detached Window
        </span>
      </footer>

      {/* Delete confirmation overlay */}
      {confirmDelete && (
        <div className="absolute inset-0 bg-background/80 backdrop-blur-sm flex items-center justify-center z-50">
          <div className="glass-panel-elevated rounded-xl p-5 max-w-xs mx-4 space-y-3">
            <p className="text-sm text-text-primary">
              Delete <span className="font-mono text-primary">{confirmDelete}</span>?
            </p>
            <p className="text-xs text-text-muted">This action cannot be undone.</p>
            <div className="flex justify-end gap-2 pt-2">
              <button
                onClick={() => setConfirmDelete(null)}
                className="px-3 py-1.5 text-xs text-text-secondary hover:text-text-primary bg-surface-elevated rounded-lg transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleDeleteConfirm}
                className="px-3 py-1.5 text-xs font-medium rounded-lg bg-status-error text-white hover:bg-status-error/90 transition-colors"
              >
                Delete
              </button>
            </div>
          </div>
        </div>
      )}

      {/* Editor modal */}
      <SkillEditor
        isOpen={showEditor}
        onClose={() => { setShowEditor(false); setEditingSkill(undefined); }}
        onSaved={fetchData}
        skill={editingSkill}
      />

      <ToastContainer />
    </div>
  );
}

export function DetachedRegistryPage() {
  return (
    <DetachedErrorBoundary>
      <DetachedRegistryContent />
    </DetachedErrorBoundary>
  );
}
