import { useEffect, useState, useCallback, useRef, Component, type ReactNode } from 'react';
import {
  Library,
  BookOpen,
  ChevronDown,
  ChevronRight,
  Plus,
  Pencil,
  Trash2,
  Power,
  PowerOff,
  FolderOpen,
  RefreshCw,
  AlertCircle,
} from 'lucide-react';
import { cn } from '../lib/cn';
import { IconButton } from '../components/ui/IconButton';
import { ZoomControls } from '../components/log/ZoomControls';
import { SkillEditor } from '../components/registry/SkillEditor';
import { ToastContainer, showToast } from '../components/ui/Toast';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { useLogFontSize } from '../hooks/useLogFontSize';
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

function DetachedRegistryContent() {
  const [skills, setSkills] = useState<AgentSkill[] | null>(null);
  const [status, setStatus] = useState<RegistryStatus | null>(null);
  const [isLoading, setIsLoading] = useState(true);

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

  const handleToggleState = useCallback(async (skill: AgentSkill) => {
    try {
      if (skill.state === 'active') {
        await disableRegistrySkill(skill.name);
        showToast('success', `Skill "${skill.name}" disabled`);
      } else {
        await activateRegistrySkill(skill.name);
        showToast('success', `Skill "${skill.name}" activated`);
      }
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

      {/* Content */}
      <main
        ref={contentRef}
        className="flex-1 overflow-y-auto scrollbar-dark relative z-10"
        style={{ '--log-font-size': `${fontSize}px` } as React.CSSProperties}
      >
        {isLoading && (
          <div className="h-full flex items-center justify-center">
            <div className="w-6 h-6 border-2 border-primary border-t-transparent rounded-full animate-spin" />
          </div>
        )}

        {!isLoading && (skills ?? []).length === 0 && (
          <div className="h-full flex flex-col items-center justify-center text-text-muted gap-3 animate-fade-in-scale">
            <div className="p-4 rounded-xl bg-surface-elevated/50 border border-border/30">
              <BookOpen size={32} className="text-text-muted/50" />
            </div>
            <span className="text-sm">No skills registered</span>
            <span className="text-[10px] text-text-muted/60">Create a SKILL.md to get started</span>
          </div>
        )}

        {!isLoading && (skills ?? []).length > 0 && (
          <div className="p-3 space-y-2">
            {(skills ?? []).map((skill) => (
              <SkillItem
                key={skill.name}
                skill={skill}
                onEdit={(s) => { setEditingSkill(s); setShowEditor(true); }}
                onDelete={(name) => setConfirmDelete(name)}
                onToggleState={handleToggleState}
              />
            ))}
          </div>
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

// Skill item component
function SkillItem({
  skill,
  onEdit,
  onDelete,
  onToggleState,
}: {
  skill: AgentSkill;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (name: string) => void;
  onToggleState: (skill: AgentSkill) => void;
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
        <BookOpen size={12} className="text-primary/60 flex-shrink-0" />
        <span className="font-medium text-text-primary flex-1 text-left truncate log-text">
          {skill.name}
        </span>
        <StateBadge state={skill.state} />
        {skill.fileCount > 0 && (
          <span className="text-text-muted font-mono flex items-center gap-0.5 log-text-detail">
            <FolderOpen size={9} />
            {skill.fileCount}
          </span>
        )}
      </button>

      {expanded && (
        <div className="px-3 pb-3 border-t border-border-subtle">
          {skill.description && (
            <p className="text-text-secondary mt-2 mb-2 leading-relaxed log-text">
              {skill.description}
            </p>
          )}

          {skill.body && (
            <pre className="text-text-muted font-mono bg-background/60 p-2 rounded overflow-x-auto max-h-32 scrollbar-dark leading-relaxed whitespace-pre-wrap log-text-detail">
              {skill.body.split('\n').slice(0, 6).join('\n')}
              {skill.body.split('\n').length > 6 && '\n...'}
            </pre>
          )}

          <div className="flex gap-1 mt-2 flex-wrap">
            {skill.license && (
              <span className="text-[9px] px-1.5 py-0.5 rounded bg-surface-highlight text-text-muted">
                {skill.license}
              </span>
            )}
            {skill.compatibility && (
              <span className="text-[9px] px-1.5 py-0.5 rounded bg-surface-highlight text-text-muted">
                {skill.compatibility}
              </span>
            )}
          </div>

          <div className="flex items-center gap-1.5 mt-3 pt-2 border-t border-border-subtle/50">
            <button
              onClick={(e) => { e.stopPropagation(); onToggleState(skill); }}
              className={cn(
                'flex items-center gap-1 px-2.5 py-1 rounded-lg text-[10px] font-semibold transition-all duration-200',
                skill.state === 'active'
                  ? 'bg-status-pending text-background shadow-[0_1px_8px_rgba(234,179,8,0.2)] hover:shadow-[0_2px_12px_rgba(234,179,8,0.3)] hover:-translate-y-0.5 active:translate-y-0'
                  : 'bg-status-running text-background shadow-[0_1px_8px_rgba(16,185,129,0.2)] hover:shadow-[0_2px_12px_rgba(16,185,129,0.3)] hover:-translate-y-0.5 active:translate-y-0'
              )}
            >
              {skill.state === 'active' ? <PowerOff size={10} /> : <Power size={10} />}
              {skill.state === 'active' ? 'Disable' : 'Activate'}
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); onEdit(skill); }}
              className="flex items-center gap-1 px-2.5 py-1 rounded-lg text-[10px] font-semibold bg-gradient-to-r from-primary to-primary-dark text-background shadow-[0_1px_8px_rgba(245,158,11,0.2)] hover:shadow-[0_2px_12px_rgba(245,158,11,0.3)] hover:-translate-y-0.5 active:translate-y-0 transition-all duration-200"
            >
              <Pencil size={10} /> Edit
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); onDelete(skill.name); }}
              className="flex items-center gap-1 px-2.5 py-1 rounded-lg text-[10px] font-semibold bg-gradient-to-r from-status-error to-rose-600 text-white shadow-[0_1px_8px_rgba(244,63,94,0.2)] hover:shadow-[0_2px_12px_rgba(244,63,94,0.3)] hover:-translate-y-0.5 active:translate-y-0 transition-all duration-200"
            >
              <Trash2 size={10} /> Delete
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

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

export function DetachedRegistryPage() {
  return (
    <DetachedErrorBoundary>
      <DetachedRegistryContent />
    </DetachedErrorBoundary>
  );
}
