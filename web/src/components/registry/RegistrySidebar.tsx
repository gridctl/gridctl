import { useState, useCallback } from 'react';
import {
  Library,
  FileText,
  Wrench,
  BarChart3,
  ChevronDown,
  ChevronRight,
  Plus,
  Pencil,
  Trash2,
  Play,
  Power,
  PowerOff,
  X,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { useRegistryStore } from '../../stores/useRegistryStore';
import { useStackStore } from '../../stores/useStackStore';
import { useUIStore } from '../../stores/useUIStore';
import { useWindowManager } from '../../hooks/useWindowManager';
import { PopoutButton } from '../ui/PopoutButton';
import { PromptEditor } from './PromptEditor';
import { SkillEditor } from './SkillEditor';
import { SkillTestRunner } from './SkillTestRunner';
import { showToast } from '../ui/Toast';
import {
  fetchRegistryStatus,
  fetchRegistryPrompts,
  fetchRegistrySkills,
  deleteRegistryPrompt,
  deleteRegistrySkill,
  activateRegistryPrompt,
  disableRegistryPrompt,
  activateRegistrySkill,
  disableRegistrySkill,
} from '../../lib/api';
import type { Prompt, Skill, ItemState, RegistryStatus } from '../../types';

type Tab = 'prompts' | 'skills' | 'status';

const tabConfig: { key: Tab; label: string; icon: typeof FileText }[] = [
  { key: 'prompts', label: 'Prompts', icon: FileText },
  { key: 'skills', label: 'Skills', icon: Wrench },
  { key: 'status', label: 'Status', icon: BarChart3 },
];

export function RegistrySidebar() {
  const [activeTab, setActiveTab] = useState<Tab>('prompts');
  const prompts = useRegistryStore((s) => s.prompts);
  const skills = useRegistryStore((s) => s.skills);
  const status = useRegistryStore((s) => s.status);
  const setSidebarOpen = useUIStore((s) => s.setSidebarOpen);
  const sidebarDetached = useUIStore((s) => s.sidebarDetached);
  const editorDetached = useUIStore((s) => s.editorDetached);
  const selectNode = useStackStore((s) => s.selectNode);
  const { openDetachedWindow } = useWindowManager();

  // Modal state
  const [showPromptEditor, setShowPromptEditor] = useState(false);
  const [editingPrompt, setEditingPrompt] = useState<Prompt | undefined>();
  const [showSkillEditor, setShowSkillEditor] = useState(false);
  const [editingSkill, setEditingSkill] = useState<Skill | undefined>();
  const [testingSkill, setTestingSkill] = useState<Skill | undefined>();

  // Delete confirmation state
  const [confirmDelete, setConfirmDelete] = useState<{ type: 'prompt' | 'skill'; name: string } | null>(null);

  const handleClose = () => {
    setSidebarOpen(false);
    selectNode(null);
  };

  const handlePopout = () => {
    openDetachedWindow('sidebar', `node=${encodeURIComponent('Registry')}`);
  };

  const refreshRegistry = useCallback(async () => {
    try {
      const [regStatus, regPrompts, regSkills] = await Promise.all([
        fetchRegistryStatus(),
        fetchRegistryPrompts(),
        fetchRegistrySkills(),
      ]);
      useRegistryStore.getState().setStatus(regStatus);
      useRegistryStore.getState().setPrompts(regPrompts);
      useRegistryStore.getState().setSkills(regSkills);
    } catch {
      // Next polling cycle will pick up changes
    }
  }, []);

  const handleEditPrompt = useCallback((prompt: Prompt) => {
    setEditingPrompt(prompt);
    setShowPromptEditor(true);
  }, []);

  const handleEditSkill = useCallback((skill: Skill) => {
    setEditingSkill(skill);
    setShowSkillEditor(true);
  }, []);

  const handleDeleteConfirm = useCallback(async () => {
    if (!confirmDelete) return;
    try {
      if (confirmDelete.type === 'prompt') {
        await deleteRegistryPrompt(confirmDelete.name);
        showToast('success', 'Prompt deleted');
      } else {
        await deleteRegistrySkill(confirmDelete.name);
        showToast('success', 'Skill deleted');
      }
      refreshRegistry();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Delete failed');
    } finally {
      setConfirmDelete(null);
    }
  }, [confirmDelete, refreshRegistry]);

  const handleTogglePromptState = useCallback(async (prompt: Prompt) => {
    try {
      if (prompt.state === 'active') {
        await disableRegistryPrompt(prompt.name);
        showToast('success', `Prompt "${prompt.name}" disabled`);
      } else {
        await activateRegistryPrompt(prompt.name);
        showToast('success', `Prompt "${prompt.name}" activated`);
      }
      refreshRegistry();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'State change failed');
    }
  }, [refreshRegistry]);

  const handleToggleSkillState = useCallback(async (skill: Skill) => {
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

  return (
    <div className="h-full w-full flex flex-col overflow-hidden">
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
              <p className="text-[10px] text-text-muted uppercase tracking-wider">Internal</p>
            </div>
          </div>
        </div>
        <div className="flex items-center gap-1">
          <PopoutButton
            onClick={handlePopout}
            tooltip="Open in new window"
            disabled={sidebarDetached}
          />
          <button onClick={handleClose} className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors group">
            <X size={16} className="text-text-muted group-hover:text-text-primary transition-colors" />
          </button>
        </div>
      </div>

      {/* Tab bar */}
      <div className="flex border-b border-border/30">
        {tabConfig.map(({ key, label, icon: TabIcon }) => (
          <button
            key={key}
            onClick={() => setActiveTab(key)}
            className={cn(
              'flex-1 flex items-center justify-center gap-1.5 px-3 py-2.5 text-xs font-medium transition-colors',
              activeTab === key
                ? 'text-primary border-b-2 border-primary'
                : 'text-text-muted hover:text-text-secondary'
            )}
          >
            <TabIcon size={12} />
            {label}
          </button>
        ))}
      </div>

      {/* Item count + New button */}
      {activeTab !== 'status' && (
        <div className="flex items-center justify-between px-4 py-2 border-b border-border/20">
          <span className="text-[10px] text-text-muted">
            {activeTab === 'prompts'
              ? `${(prompts ?? []).length} items`
              : `${(skills ?? []).length} items`}
          </span>
          <button
            onClick={() =>
              activeTab === 'prompts'
                ? setShowPromptEditor(true)
                : setShowSkillEditor(true)
            }
            className="flex items-center gap-1 text-[10px] text-primary hover:text-primary/80 transition-colors"
          >
            <Plus size={10} /> New
          </button>
        </div>
      )}

      {/* Tab content */}
      <div className="flex-1 overflow-y-auto scrollbar-dark">
        {activeTab === 'prompts' && (
          <PromptsTab
            prompts={prompts ?? []}
            onEdit={handleEditPrompt}
            onDelete={(name) => setConfirmDelete({ type: 'prompt', name })}
            onToggleState={handleTogglePromptState}
          />
        )}
        {activeTab === 'skills' && (
          <SkillsTab
            skills={skills ?? []}
            onEdit={handleEditSkill}
            onDelete={(name) => setConfirmDelete({ type: 'skill', name })}
            onToggleState={handleToggleSkillState}
            onTest={setTestingSkill}
          />
        )}
        {activeTab === 'status' && <StatusTab status={status} />}
      </div>

      {/* Delete confirmation */}
      {confirmDelete && (
        <div className="absolute inset-0 bg-background/80 backdrop-blur-sm flex items-center justify-center z-50">
          <div className="glass-panel-elevated rounded-xl p-5 max-w-xs mx-4 space-y-3">
            <p className="text-sm text-text-primary">
              Delete <span className="font-mono text-primary">{confirmDelete.name}</span>?
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

      {/* Modals */}
      <PromptEditor
        isOpen={showPromptEditor}
        onClose={() => {
          setShowPromptEditor(false);
          setEditingPrompt(undefined);
        }}
        onSaved={refreshRegistry}
        prompt={editingPrompt}
        popoutDisabled={editorDetached}
        onPopout={() => {
          const params = editingPrompt
            ? `type=prompt&name=${encodeURIComponent(editingPrompt.name)}`
            : 'type=prompt';
          openDetachedWindow('editor', params);
          setShowPromptEditor(false);
          setEditingPrompt(undefined);
        }}
      />
      <SkillEditor
        isOpen={showSkillEditor}
        onClose={() => {
          setShowSkillEditor(false);
          setEditingSkill(undefined);
        }}
        onSaved={refreshRegistry}
        skill={editingSkill}
        popoutDisabled={editorDetached}
        onPopout={() => {
          const params = editingSkill
            ? `type=skill&name=${encodeURIComponent(editingSkill.name)}`
            : 'type=skill';
          openDetachedWindow('editor', params);
          setShowSkillEditor(false);
          setEditingSkill(undefined);
        }}
      />
      {testingSkill && (
        <SkillTestRunner
          isOpen={!!testingSkill}
          onClose={() => setTestingSkill(undefined)}
          skill={testingSkill}
        />
      )}
    </div>
  );
}

// --- Prompts Tab ---

function PromptsTab({
  prompts,
  onEdit,
  onDelete,
  onToggleState,
}: {
  prompts: Prompt[];
  onEdit: (prompt: Prompt) => void;
  onDelete: (name: string) => void;
  onToggleState: (prompt: Prompt) => void;
}) {
  if ((prompts ?? []).length === 0) {
    return (
      <div className="p-6 text-center">
        <FileText size={24} className="text-text-muted/30 mx-auto mb-2" />
        <p className="text-text-muted text-xs">No prompts registered</p>
      </div>
    );
  }

  return (
    <div className="p-2 space-y-1">
      {(prompts ?? []).map((prompt) => (
        <PromptItem
          key={prompt.name}
          prompt={prompt}
          onEdit={onEdit}
          onDelete={onDelete}
          onToggleState={onToggleState}
        />
      ))}
    </div>
  );
}

function PromptItem({
  prompt,
  onEdit,
  onDelete,
  onToggleState,
}: {
  prompt: Prompt;
  onEdit: (prompt: Prompt) => void;
  onDelete: (name: string) => void;
  onToggleState: (prompt: Prompt) => void;
}) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="rounded-lg bg-surface-elevated/50 border border-border-subtle overflow-hidden">
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-2 p-3 hover:bg-surface-highlight/50 transition-colors"
      >
        <div className="p-0.5 text-text-muted">
          {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        </div>
        <FileText size={12} className="text-primary/60 flex-shrink-0" />
        <span className="text-xs font-medium text-text-primary flex-1 text-left truncate">
          {prompt.name}
        </span>
        <StateBadge state={prompt.state} />
        {(prompt.arguments ?? []).length > 0 && (
          <span className="text-[10px] text-text-muted font-mono">
            {(prompt.arguments ?? []).length} args
          </span>
        )}
      </button>

      {expanded && (
        <div className="px-3 pb-3 border-t border-border-subtle">
          {prompt.description && (
            <p className="text-[11px] text-text-secondary mt-2 mb-2 leading-relaxed">
              {prompt.description}
            </p>
          )}
          {prompt.content && (
            <pre className="text-[10px] text-text-muted font-mono bg-background/60 p-2 rounded overflow-x-auto max-h-32 scrollbar-dark leading-relaxed">
              {prompt.content}
            </pre>
          )}
          {(prompt.arguments ?? []).length > 0 && (
            <div className="mt-2 space-y-1">
              <span className="text-[10px] text-text-muted uppercase tracking-wider">Arguments</span>
              {(prompt.arguments ?? []).map((arg) => (
                <div key={arg.name} className="flex items-center gap-2 text-[10px]">
                  <span className="font-mono text-primary">{arg.name}</span>
                  {arg.required && (
                    <span className="text-status-error/70">*</span>
                  )}
                  {arg.description && (
                    <span className="text-text-muted truncate">{arg.description}</span>
                  )}
                </div>
              ))}
            </div>
          )}
          {(prompt.tags ?? []).length > 0 && (
            <div className="flex gap-1 mt-2 flex-wrap">
              {(prompt.tags ?? []).map((tag) => (
                <span key={tag} className="text-[9px] px-1.5 py-0.5 rounded bg-surface-highlight text-text-muted">
                  {tag}
                </span>
              ))}
            </div>
          )}
          {/* Actions */}
          <div className="flex items-center gap-2 mt-3 pt-2 border-t border-border-subtle/50">
            <button
              onClick={(e) => {
                e.stopPropagation();
                onToggleState(prompt);
              }}
              className={cn(
                'flex items-center gap-1 text-[10px] transition-colors',
                prompt.state === 'active'
                  ? 'text-text-muted hover:text-status-pending'
                  : 'text-text-muted hover:text-status-running',
              )}
            >
              {prompt.state === 'active' ? <PowerOff size={10} /> : <Power size={10} />}
              {prompt.state === 'active' ? 'Disable' : 'Activate'}
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation();
                onEdit(prompt);
              }}
              className="flex items-center gap-1 text-[10px] text-text-muted hover:text-primary transition-colors"
            >
              <Pencil size={10} /> Edit
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation();
                onDelete(prompt.name);
              }}
              className="flex items-center gap-1 text-[10px] text-text-muted hover:text-status-error transition-colors"
            >
              <Trash2 size={10} /> Delete
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// --- Skills Tab ---

function SkillsTab({
  skills,
  onEdit,
  onDelete,
  onToggleState,
  onTest,
}: {
  skills: Skill[];
  onEdit: (skill: Skill) => void;
  onDelete: (name: string) => void;
  onToggleState: (skill: Skill) => void;
  onTest: (skill: Skill) => void;
}) {
  if ((skills ?? []).length === 0) {
    return (
      <div className="p-6 text-center">
        <Wrench size={24} className="text-text-muted/30 mx-auto mb-2" />
        <p className="text-text-muted text-xs">No skills registered</p>
      </div>
    );
  }

  return (
    <div className="p-2 space-y-1">
      {(skills ?? []).map((skill) => (
        <SkillItem
          key={skill.name}
          skill={skill}
          onEdit={onEdit}
          onDelete={onDelete}
          onToggleState={onToggleState}
          onTest={onTest}
        />
      ))}
    </div>
  );
}

function SkillItem({
  skill,
  onEdit,
  onDelete,
  onToggleState,
  onTest,
}: {
  skill: Skill;
  onEdit: (skill: Skill) => void;
  onDelete: (name: string) => void;
  onToggleState: (skill: Skill) => void;
  onTest: (skill: Skill) => void;
}) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div className="rounded-lg bg-surface-elevated/50 border border-border-subtle overflow-hidden">
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center gap-2 p-3 hover:bg-surface-highlight/50 transition-colors"
      >
        <div className="p-0.5 text-text-muted">
          {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        </div>
        <Wrench size={12} className="text-primary/60 flex-shrink-0" />
        <span className="text-xs font-medium text-text-primary flex-1 text-left truncate">
          {skill.name}
        </span>
        <StateBadge state={skill.state} />
        {(skill.steps ?? []).length > 0 && (
          <span className="text-[10px] text-text-muted font-mono">
            {(skill.steps ?? []).length} steps
          </span>
        )}
      </button>

      {expanded && (
        <div className="px-3 pb-3 border-t border-border-subtle">
          {skill.description && (
            <p className="text-[11px] text-text-secondary mt-2 mb-2 leading-relaxed">
              {skill.description}
            </p>
          )}

          {/* Tool chain */}
          {(skill.steps ?? []).length > 0 && (
            <div className="mt-2">
              <span className="text-[10px] text-text-muted uppercase tracking-wider block mb-1.5">Tool Chain</span>
              <div className="space-y-1">
                {(skill.steps ?? []).map((step, i) => (
                  <div key={i} className="flex items-center gap-2 py-1 px-2 rounded bg-background/40">
                    <span className="text-[9px] text-text-muted font-mono w-4 text-right flex-shrink-0">{i + 1}.</span>
                    <Wrench size={9} className="text-primary/50 flex-shrink-0" />
                    <span className="text-[10px] font-mono text-primary truncate">{step.tool}</span>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Input parameters */}
          {(skill.input ?? []).length > 0 && (
            <div className="mt-2 space-y-1">
              <span className="text-[10px] text-text-muted uppercase tracking-wider">Inputs</span>
              {(skill.input ?? []).map((param) => (
                <div key={param.name} className="flex items-center gap-2 text-[10px]">
                  <span className="font-mono text-primary">{param.name}</span>
                  {param.required && (
                    <span className="text-status-error/70">*</span>
                  )}
                  {param.description && (
                    <span className="text-text-muted truncate">{param.description}</span>
                  )}
                </div>
              ))}
            </div>
          )}

          {(skill.tags ?? []).length > 0 && (
            <div className="flex gap-1 mt-2 flex-wrap">
              {(skill.tags ?? []).map((tag) => (
                <span key={tag} className="text-[9px] px-1.5 py-0.5 rounded bg-surface-highlight text-text-muted">
                  {tag}
                </span>
              ))}
            </div>
          )}

          {/* Actions */}
          <div className="flex items-center gap-2 mt-3 pt-2 border-t border-border-subtle/50">
            {skill.state === 'active' && (
              <button
                onClick={(e) => {
                  e.stopPropagation();
                  onTest(skill);
                }}
                className="flex items-center gap-1 text-[10px] text-text-muted hover:text-status-running transition-colors"
              >
                <Play size={10} /> Test
              </button>
            )}
            <button
              onClick={(e) => {
                e.stopPropagation();
                onToggleState(skill);
              }}
              className={cn(
                'flex items-center gap-1 text-[10px] transition-colors',
                skill.state === 'active'
                  ? 'text-text-muted hover:text-status-pending'
                  : 'text-text-muted hover:text-status-running',
              )}
            >
              {skill.state === 'active' ? <PowerOff size={10} /> : <Power size={10} />}
              {skill.state === 'active' ? 'Disable' : 'Activate'}
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation();
                onEdit(skill);
              }}
              className="flex items-center gap-1 text-[10px] text-text-muted hover:text-primary transition-colors"
            >
              <Pencil size={10} /> Edit
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation();
                onDelete(skill.name);
              }}
              className="flex items-center gap-1 text-[10px] text-text-muted hover:text-status-error transition-colors"
            >
              <Trash2 size={10} /> Delete
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

// --- Status Tab ---

function StatusTab({ status }: { status: RegistryStatus | null }) {
  if (!status) {
    return (
      <div className="p-6 text-center">
        <BarChart3 size={24} className="text-text-muted/30 mx-auto mb-2" />
        <p className="text-text-muted text-xs">Loading registry status...</p>
      </div>
    );
  }

  return (
    <div className="p-4 space-y-3">
      {/* Prompts */}
      <div className="glass-panel p-3 rounded-lg">
        <div className="flex items-center gap-2 mb-3">
          <FileText size={12} className="text-primary/60" />
          <span className="text-[10px] text-text-muted uppercase tracking-wider font-medium">Prompts</span>
        </div>
        <div className="space-y-2">
          <div className="flex justify-between items-center">
            <span className="text-xs text-text-secondary">Total</span>
            <span className="text-xs font-mono text-text-primary font-bold tabular-nums">{status.totalPrompts}</span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-xs text-text-secondary">Active</span>
            <span className="text-xs font-mono text-status-running font-bold tabular-nums">{status.activePrompts}</span>
          </div>
        </div>
      </div>

      {/* Skills */}
      <div className="glass-panel p-3 rounded-lg">
        <div className="flex items-center gap-2 mb-3">
          <Wrench size={12} className="text-primary/60" />
          <span className="text-[10px] text-text-muted uppercase tracking-wider font-medium">Skills</span>
        </div>
        <div className="space-y-2">
          <div className="flex justify-between items-center">
            <span className="text-xs text-text-secondary">Total</span>
            <span className="text-xs font-mono text-text-primary font-bold tabular-nums">{status.totalSkills}</span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-xs text-text-secondary">Active</span>
            <span className="text-xs font-mono text-status-running font-bold tabular-nums">{status.activeSkills}</span>
          </div>
        </div>
      </div>
    </div>
  );
}

// --- Shared Components ---

function StateBadge({ state }: { state: ItemState }) {
  const styles: Record<ItemState, string> = {
    active: 'bg-status-running/10 text-status-running',
    draft: 'bg-status-pending/10 text-status-pending',
    disabled: 'bg-surface-highlight text-text-muted',
  };

  return (
    <span className={cn('text-[9px] px-1.5 py-0.5 rounded font-mono', styles[state] ?? styles.draft)}>
      {state}
    </span>
  );
}
