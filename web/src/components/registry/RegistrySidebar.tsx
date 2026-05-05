import { useState, useCallback, useEffect, useRef } from 'react';
import {
  Library,
  BookOpen,
  ChevronDown,
  ChevronRight,
  Plus,
  Power,
  PowerOff,
  X,
  Search,
  FolderOpen,
  GitBranch,
  Download,
  ArrowUpCircle,
  CheckCircle2,
  XCircle,
  MinusCircle,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { useRegistryStore } from '../../stores/useRegistryStore';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { useFuzzySearch } from '../../hooks/useFuzzySearch';
import { PopoutButton } from '../ui/PopoutButton';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { SkillEditor } from './SkillEditor';
import { StateBadge } from './StateBadge';
import { TestStatusBadge } from './TestStatusBadge';
import { SkillActions } from './SkillActions';
import { useListNav } from '../../hooks/useListNav';
import { showToast } from '../ui/Toast';
import {
  fetchRegistryStatus,
  fetchRegistrySkills,
  deleteRegistrySkill,
  activateRegistrySkill,
  disableRegistrySkill,
  fetchSkillUpdates,
  updateSkillSource,
  getSkillTestResult,
} from '../../lib/api';
import { hasWorkflowBlock } from '../../lib/workflowSync';
import { useWizardStore } from '../../stores/useWizardStore';
import type { AgentSkill, UpdateSummary, SkillTestResult } from '../../types';

export function RegistrySidebar({ embedded = false }: { embedded?: boolean } = {}) {
  const skills = useRegistryStore((s) => s.skills);
  const status = useRegistryStore((s) => s.status);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const registryDetached = useUIStore((s) => s.registryDetached);
  const editorDetached = useUIStore((s) => s.editorDetached);
  const selectNode = useStackStore((s) => s.selectNode);
  const { openDetachedWindow } = useWindowManager();

  // Editor state
  const [showEditor, setShowEditor] = useState(false);
  const [editingSkill, setEditingSkill] = useState<AgentSkill | undefined>();

  // Search state
  const [searchQuery, setSearchQuery] = useState('');

  // Update badge state
  const [updateSummary, setUpdateSummary] = useState<UpdateSummary | null>(null);

  // Update-all loading state
  const [updatingAll, setUpdatingAll] = useState(false);

  // Check for updates periodically
  const checkUpdates = useCallback(async () => {
    try {
      const summary = await fetchSkillUpdates();
      setUpdateSummary(summary);
    } catch {
      // Silent
    }
  }, []);

  // Check on mount (non-blocking)
  useState(() => { checkUpdates(); });

  const openWizard = useWizardStore((s) => s.open);

  const filteredSkills = useFuzzySearch(skills ?? [], searchQuery);

  // Delete confirmation
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  // Keyboard nav state
  const [selectedIndex, setSelectedIndex] = useState(0);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const skillRowRefs = useRef<Array<HTMLElement | null>>([]);

  const handleClose = () => {
    setSidebarOpen(false);
    selectNode(null);
  };

  const handlePopout = () => {
    openDetachedWindow('registry');
  };

  const refreshRegistry = useCallback(async () => {
    try {
      const [regStatus, regSkills] = await Promise.all([
        fetchRegistryStatus(),
        fetchRegistrySkills(),
      ]);
      useRegistryStore.getState().setStatus(regStatus);
      useRegistryStore.getState().setSkills(regSkills);
    } catch {
      // Next polling cycle will pick up changes
    }
  }, []);

  // Update all sources with available updates
  const handleUpdateAll = useCallback(async () => {
    if (!updateSummary?.sources) return;
    setUpdatingAll(true);
    try {
      const toUpdate = updateSummary.sources.filter((s) => s.hasUpdate);
      for (const src of toUpdate) {
        await updateSkillSource(src.name);
      }
      showToast('success', `Updated ${toUpdate.length} source${toUpdate.length !== 1 ? 's' : ''}`);
      await Promise.all([refreshRegistry(), checkUpdates()]);
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Update failed');
    } finally {
      setUpdatingAll(false);
    }
  }, [updateSummary, refreshRegistry, checkUpdates]);

  const handleToggleState = useCallback(async (skill: AgentSkill) => {
    try {
      if (skill.state === 'active') {
        await disableRegistrySkill(skill.name);
        showToast('success', `Skill "${skill.name}" disabled`);
      } else {
        await activateRegistrySkill(skill.name);
        showToast('success', `Skill "${skill.name}" activated`);
      }
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

  // Clamp selected index when the filtered list shrinks
  useEffect(() => {
    if (filteredSkills.length === 0) return;
    if (selectedIndex >= filteredSkills.length) {
      setSelectedIndex(filteredSkills.length - 1);
    }
  }, [filteredSkills.length, selectedIndex]);

  // Scroll the selected row into view on change
  useEffect(() => {
    skillRowRefs.current[selectedIndex]?.scrollIntoView({ block: 'nearest' });
  }, [selectedIndex]);

  const keyboardEnabled = !showEditor && confirmDelete === null;
  const selectedSkill = filteredSkills[selectedIndex];

  useListNav({
    itemCount: filteredSkills.length,
    selectedIndex: Math.max(0, selectedIndex),
    setSelectedIndex,
    onEnter: () => {
      const el = skillRowRefs.current[selectedIndex];
      el?.querySelector<HTMLElement>('[data-skill-header]')?.click();
    },
    onEdit: () => {
      if (selectedSkill) {
        setEditingSkill(selectedSkill);
        setShowEditor(true);
      }
    },
    onToggle: () => {
      if (selectedSkill) handleToggleState(selectedSkill);
    },
    enabled: keyboardEnabled,
  });

  // Global '/' to focus search, 'n' to open new-skill editor
  useEffect(() => {
    if (!keyboardEnabled) return;
    const handler = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      const target = e.target as HTMLElement | null;
      if (!target) return;
      const tag = target.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable) return;
      if (target.closest('[role="dialog"], [role="alertdialog"]')) return;

      if (e.key === '/') {
        e.preventDefault();
        searchInputRef.current?.focus();
        searchInputRef.current?.select();
      } else if (e.key === 'n') {
        e.preventDefault();
        setEditingSkill(undefined);
        setShowEditor(true);
      }
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [keyboardEnabled]);

  return (
    <div className={cn('flex flex-col overflow-hidden', !embedded && 'h-full w-full')}>
      {!embedded && (
        <>
          {/* Accent line */}
          <div className="absolute top-0 left-0 bottom-0 w-px bg-gradient-to-b from-primary/40 via-primary/20 to-transparent" />

          {/* Header */}
          <div className="flex items-center justify-between p-4 border-b border-border/50 bg-surface-elevated/30">
            <div className="flex items-center gap-3 min-w-0">
              <div className="p-2 rounded-xl flex-shrink-0 border bg-primary/10 border-primary/20">
                <Library size={16} className="text-primary" />
              </div>
              <div className="min-w-0">
                <h2 className="font-semibold text-text-primary truncate tracking-tight">Registry</h2>
                <div className="flex items-center gap-1.5">
                  <p className="text-[10px] text-text-muted uppercase tracking-wider">Agent Skills</p>
                </div>
              </div>
            </div>
            <div className="flex items-center gap-1">
              <PopoutButton
                onClick={handlePopout}
                disabled={registryDetached}
              />
              <button onClick={handleClose} className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors group">
                <X size={16} className="text-text-muted group-hover:text-text-primary transition-colors" />
              </button>
            </div>
          </div>
        </>
      )}

      {/* Item count + New Skill + Import buttons */}
      <div className="flex items-center justify-between px-4 py-2 border-b border-border/20">
        <div className="flex items-center gap-2">
          <span className="text-[10px] text-text-muted">
            {searchQuery
              ? `${filteredSkills.length} of ${(skills ?? []).length} skills`
              : `${(skills ?? []).length} skills`}
          </span>
          {(updateSummary?.available ?? 0) > 0 && (
            <>
              <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-primary/10 text-primary font-medium flex items-center gap-1 animate-fade-in-scale">
                <ArrowUpCircle size={10} />
                {updateSummary?.available} update{(updateSummary?.available ?? 0) !== 1 ? 's' : ''}
              </span>
              <button
                onClick={handleUpdateAll}
                disabled={updatingAll}
                className={cn(
                  'text-[10px] px-1.5 py-0.5 rounded-full font-medium flex items-center gap-1 transition-colors',
                  updatingAll
                    ? 'bg-text-muted/10 text-text-muted cursor-wait'
                    : 'bg-secondary/10 text-secondary hover:bg-secondary/20',
                )}
              >
                <ArrowUpCircle size={10} />
                {updatingAll ? 'Updating...' : 'Update All'}
              </button>
            </>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => openWizard('skill')}
            className="flex items-center gap-1 text-[10px] text-secondary hover:text-secondary/80 transition-colors"
          >
            <Download size={10} /> Import
          </button>
          <button
            onClick={() => { setEditingSkill(undefined); setShowEditor(true); }}
            className="flex items-center gap-1 text-[10px] text-primary hover:text-primary/80 transition-colors"
          >
            <Plus size={10} /> New
          </button>
        </div>
      </div>

      {/* Search */}
      <div className="px-2 py-1.5 border-b border-border/20 flex-shrink-0" role="search">
        <div className="relative">
          <Search size={12} className="absolute left-2 top-1/2 -translate-y-1/2 text-text-muted/50" />
          <input
            ref={searchInputRef}
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Search skills..."
            aria-label="Filter skills"
            className="w-full bg-background/40 border border-border/30 rounded-lg pl-7 pr-7 py-1 text-xs text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/40"
          />
          {searchQuery && (
            <button
              type="button"
              onClick={() => setSearchQuery('')}
              title="Clear search"
              className="absolute right-1.5 top-1/2 -translate-y-1/2 p-1.5 rounded hover:bg-surface-highlight transition-colors focus:outline-none focus:ring-2 focus:ring-primary/30"
            >
              <X size={12} className="text-text-muted" />
            </button>
          )}
        </div>
      </div>

      {/* Skills list */}
      <div className="flex-1 overflow-y-auto scrollbar-dark">
        <SkillsList
          skills={filteredSkills}
          isFiltered={!!searchQuery}
          selectedIndex={selectedIndex}
          onSelectIndex={setSelectedIndex}
          rowRefs={skillRowRefs}
          onEdit={(skill) => { setEditingSkill(skill); setShowEditor(true); }}
          onDelete={(name) => setConfirmDelete(name)}
          onToggleState={handleToggleState}
          onOpenWorkflow={(name) => openDetachedWindow('workflow', `skill=${encodeURIComponent(name)}`)}
        />
      </div>

      {/* Status footer */}
      {status && (
        <div className="px-4 py-2 border-t border-border/30 bg-surface/30">
          <div className="flex items-center justify-between text-[10px] text-text-muted">
            <span>{status.totalSkills} total</span>
            <span className="text-status-running">{status.activeSkills} active</span>
          </div>
        </div>
      )}

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
        onSaved={refreshRegistry}
        skill={editingSkill}
        popoutDisabled={editorDetached}
        onPopout={() => {
          const params = editingSkill
            ? `type=skill&name=${encodeURIComponent(editingSkill.name)}`
            : 'type=skill';
          openDetachedWindow('editor', params);
          setShowEditor(false);
          setEditingSkill(undefined);
        }}
      />
    </div>
  );
}

// --- SkillsList ---

function SkillsList({
  skills,
  isFiltered,
  selectedIndex,
  onSelectIndex,
  rowRefs,
  onEdit,
  onDelete,
  onToggleState,
  onOpenWorkflow,
}: {
  skills: AgentSkill[];
  isFiltered: boolean;
  selectedIndex: number;
  onSelectIndex: (i: number) => void;
  rowRefs: React.RefObject<Array<HTMLElement | null>>;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (name: string) => void;
  onToggleState: (skill: AgentSkill) => void;
  onOpenWorkflow: (name: string) => void;
}) {
  if ((skills ?? []).length === 0) {
    return (
      <div className="p-6 text-center">
        <BookOpen size={24} className="text-text-muted/30 mx-auto mb-2" />
        <p className="text-text-muted text-xs">
          {isFiltered ? 'No matching skills' : 'No skills registered'}
        </p>
        {!isFiltered && (
          <p className="text-text-muted text-[10px] mt-1">
            Create a SKILL.md to get started
          </p>
        )}
      </div>
    );
  }

  return (
    <div className="p-2 space-y-1">
      {(skills ?? []).map((skill, index) => (
        <SkillItem
          key={skill.name}
          skill={skill}
          index={index}
          isSelected={index === selectedIndex}
          onSelect={() => onSelectIndex(index)}
          rowRef={(el) => {
            if (rowRefs.current) rowRefs.current[index] = el;
          }}
          onEdit={onEdit}
          onDelete={onDelete}
          onToggleState={onToggleState}
          onOpenWorkflow={onOpenWorkflow}
        />
      ))}
    </div>
  );
}

// --- SkillItem ---

function SkillItem({
  skill,
  index: _index,
  isSelected,
  onSelect,
  rowRef,
  onEdit,
  onDelete,
  onToggleState,
  onOpenWorkflow,
}: {
  skill: AgentSkill;
  index: number;
  isSelected: boolean;
  onSelect: () => void;
  rowRef: (el: HTMLElement | null) => void;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (name: string) => void;
  onToggleState: (skill: AgentSkill) => void;
  onOpenWorkflow: (name: string) => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const [testResult, setTestResult] = useState<SkillTestResult | null>(null);
  const [showTestDetails, setShowTestDetails] = useState(false);
  const isExecutable = hasWorkflowBlock(skill.body ?? '');

  useEffect(() => {
    if (!expanded) return;
    getSkillTestResult(skill.name)
      .then(setTestResult)
      .catch(() => setTestResult(null));
  }, [expanded, skill.name]);

  const isActive = skill.state === 'active';

  return (
    <div
      ref={rowRef}
      className={cn(
        'group rounded-lg bg-surface-elevated/50 border overflow-hidden transition-colors',
        isSelected ? 'border-primary/40 shadow-[0_0_0_1px_rgba(245,158,11,0.25)]' : 'border-border-subtle',
      )}
    >
      {/* Header row */}
      <div
        data-skill-header
        className={cn(
          'w-full flex items-center gap-2 p-3 hover:bg-surface-highlight/50 transition-colors',
          // Keep the full row feeling clickable while hosting nested buttons.
          'cursor-pointer',
        )}
        onClick={() => {
          onSelect();
          setExpanded(!expanded);
        }}
      >
        <div className="p-0.5 text-text-muted">
          {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        </div>
        <BookOpen size={12} className="text-primary/60 flex-shrink-0" />
        <span className="text-xs font-medium text-text-primary flex-1 text-left truncate">
          {skill.name}
        </span>
        {isExecutable && (
          <span className="text-[10px] px-1.5 py-0.5 rounded-full bg-primary/10 text-primary border border-primary/20 font-mono">
            workflow
          </span>
        )}
        <StateBadge state={skill.state} />
        {isExecutable && (
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              onOpenWorkflow(skill.name);
            }}
            title="Open workflow designer"
            className="p-2 rounded hover:bg-primary/10 transition-all duration-200 group focus:outline-none focus:ring-2 focus:ring-primary/30"
          >
            <GitBranch size={12} className="text-text-muted group-hover:text-primary transition-colors" />
          </button>
        )}
        {skill.fileCount > 0 && (
          <span className="text-[10px] text-text-muted font-mono flex items-center gap-0.5">
            <FolderOpen size={9} />
            {skill.fileCount}
          </span>
        )}
        {/* Power toggle — most common action, promoted to collapsed row */}
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onToggleState(skill);
          }}
          title={isActive ? 'Disable skill' : 'Activate skill'}
          className={cn(
            'p-2 rounded transition-all duration-200 group focus:outline-none focus:ring-2 focus:ring-primary/30',
            isActive ? 'hover:bg-amber-400/10' : 'hover:bg-emerald-400/10',
          )}
        >
          {isActive ? (
            <PowerOff size={12} className="text-text-muted group-hover:text-amber-400 transition-colors" />
          ) : (
            <Power size={12} className="text-text-muted group-hover:text-emerald-400 transition-colors" />
          )}
        </button>
      </div>

      {/* Expanded content */}
      {expanded && (
        <div className="px-3 pb-3 border-t border-border-subtle">
          {/* Description */}
          {skill.description && (
            <p className="text-[11px] text-text-secondary mt-2 mb-2 leading-relaxed">
              {skill.description}
            </p>
          )}

          {/* Body preview (first 6 lines of markdown) */}
          {skill.body && (
            <pre className="text-[10px] text-text-muted font-mono bg-background/60 p-2 rounded overflow-x-auto max-h-32 scrollbar-dark leading-relaxed whitespace-pre-wrap">
              {skill.body.split('\n').slice(0, 6).join('\n')}
              {skill.body.split('\n').length > 6 && '\n...'}
            </pre>
          )}

          {/* Metadata badges */}
          <div className="flex gap-1 mt-2 flex-wrap">
            {skill.license && (
              <span className="text-[10px] px-1.5 py-0.5 rounded bg-surface-highlight text-text-muted">
                {skill.license}
              </span>
            )}
            {skill.compatibility && (
              <span className="text-[10px] px-1.5 py-0.5 rounded bg-surface-highlight text-text-muted">
                {skill.compatibility}
              </span>
            )}
          </div>

          {/* Test status badge */}
          <div className="mt-2">
            <TestStatusBadge
              testResult={testResult}
              density="compact"
              onClick={() => setShowTestDetails(!showTestDetails)}
            />
          </div>

          {/* Per-criterion details */}
          {showTestDetails && testResult && testResult.results.length > 0 && (
            <div className="mt-2 space-y-1.5 rounded-lg border border-border/30 bg-background/40 p-2">
              {testResult.results.map((r, i) => (
                <div key={i} className="flex items-start gap-1.5">
                  {r.skipped ? (
                    <MinusCircle size={10} className="text-text-muted/50 flex-shrink-0 mt-0.5" />
                  ) : r.passed ? (
                    <CheckCircle2 size={10} className="text-status-running flex-shrink-0 mt-0.5" />
                  ) : (
                    <XCircle size={10} className="text-status-error flex-shrink-0 mt-0.5" />
                  )}
                  <div className="min-w-0">
                    <p className="text-[10px] text-text-muted font-mono leading-relaxed truncate">
                      {r.criterion}
                    </p>
                    {!r.passed && !r.skipped && r.actual && (
                      <p className="text-[10px] text-status-error mt-0.5 font-mono truncate">
                        actual: {r.actual}
                      </p>
                    )}
                    {r.skipped && r.skipReason && (
                      <p className="text-[10px] text-text-muted mt-0.5 italic">
                        {r.skipReason}
                      </p>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* Actions — toggle lives on the collapsed row, so only edit + delete here */}
          <div className="flex items-center justify-end gap-0.5 mt-3 pt-2 border-t border-border-subtle/50">
            <SkillActions
              skill={skill}
              showToggle={false}
              onToggle={onToggleState}
              onEdit={onEdit}
              onDelete={(s) => onDelete(s.name)}
            />
          </div>
        </div>
      )}
    </div>
  );
}

