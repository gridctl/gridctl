import { useEffect, useState, useCallback, Component, type ReactNode } from 'react';
import { useSearchParams } from 'react-router-dom';
import { AlertCircle, FileText, Wrench } from 'lucide-react';
import { PromptEditor } from '../components/registry/PromptEditor';
import { SkillEditor } from '../components/registry/SkillEditor';
import { ToastContainer } from '../components/ui/Toast';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import {
  fetchRegistryPrompt,
  fetchRegistrySkill,
  fetchRegistryStatus,
  fetchRegistryPrompts,
  fetchRegistrySkills,
} from '../lib/api';
import { useRegistryStore } from '../stores/useRegistryStore';
import type { Prompt, Skill } from '../types';

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

const VALID_TYPES = new Set(['prompt', 'skill']);

function DetachedEditorContent() {
  const [searchParams] = useSearchParams();
  const rawType = searchParams.get('type');
  const editorType = VALID_TYPES.has(rawType ?? '') ? (rawType as 'prompt' | 'skill') : null;
  const itemName = searchParams.get('name');

  const [prompt, setPrompt] = useState<Prompt | undefined>();
  const [skill, setSkill] = useState<Skill | undefined>();
  const [loading, setLoading] = useState(!!itemName);
  const [error, setError] = useState<string | null>(null);

  // Register with main window
  useDetachedWindowSync('editor');

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
      // Ignore - store updates are best-effort from detached window
    }
  }, []);

  // Load the item being edited
  useEffect(() => {
    if (!itemName) {
      setLoading(false);
      return;
    }

    const loadItem = async () => {
      try {
        if (editorType === 'prompt') {
          const p = await fetchRegistryPrompt(itemName);
          setPrompt(p);
        } else if (editorType === 'skill') {
          const s = await fetchRegistrySkill(itemName);
          setSkill(s);
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load item');
      } finally {
        setLoading(false);
      }
    };

    loadItem();
  }, [editorType, itemName]);

  const handleClose = useCallback(() => {
    window.close();
  }, []);

  const handleSaved = useCallback(async () => {
    await refreshRegistry();
    // Close after a brief delay to show toast
    setTimeout(() => window.close(), 600);
  }, [refreshRegistry]);

  if (!editorType) {
    return (
      <div className="h-screen w-screen bg-background flex items-center justify-center">
        <div className="text-center text-text-muted">
          <AlertCircle size={32} className="mx-auto mb-3 opacity-50" />
          <p className="text-sm">No editor type specified</p>
        </div>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="h-screen w-screen bg-background flex items-center justify-center">
        <div className="text-center space-y-4">
          <div className="w-10 h-10 mx-auto border-2 border-primary border-t-transparent rounded-full animate-spin" />
          <p className="text-sm text-text-muted">Loading {editorType}...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="h-screen w-screen bg-background flex items-center justify-center">
        <div className="text-center p-8 max-w-md">
          <AlertCircle size={32} className="mx-auto mb-3 text-status-error" />
          <p className="text-sm text-status-error mb-4">{error}</p>
          <button
            onClick={() => window.location.reload()}
            className="px-4 py-2 bg-primary text-background rounded-lg font-medium hover:bg-primary/90 transition-colors"
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="h-screen w-screen bg-background overflow-hidden">
      {/* Background grain */}
      <div
        className="fixed inset-0 pointer-events-none z-0 opacity-[0.015]"
        style={{
          backgroundImage: `url("data:image/svg+xml,%3Csvg viewBox='0 0 256 256' xmlns='http://www.w3.org/2000/svg'%3E%3Cfilter id='noise'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='4' stitchTiles='stitch'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23noise)'/%3E%3C/svg%3E")`,
        }}
      />

      {/* Status bar at bottom */}
      <div className="fixed bottom-0 left-0 right-0 h-6 bg-surface/90 backdrop-blur-xl border-t border-border/50 flex items-center justify-between px-4 text-[10px] text-text-muted z-20">
        <span className="flex items-center gap-1.5">
          {editorType === 'prompt' ? (
            <FileText size={10} className="text-primary/60" />
          ) : (
            <Wrench size={10} className="text-primary/60" />
          )}
          {itemName ? `Editing: ${itemName}` : `New ${editorType}`}
        </span>
        <span className="flex items-center gap-1">
          <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse" />
          Detached Editor
        </span>
      </div>

      {/* Editor - rendered flush (fills viewport) in detached window */}
      {editorType === 'prompt' && (
        <PromptEditor
          isOpen={true}
          onClose={handleClose}
          onSaved={handleSaved}
          prompt={prompt}
          flush
        />
      )}
      {editorType === 'skill' && (
        <SkillEditor
          isOpen={true}
          onClose={handleClose}
          onSaved={handleSaved}
          skill={skill}
          flush
        />
      )}

      <ToastContainer />
    </div>
  );
}

export function DetachedEditorPage() {
  return (
    <DetachedErrorBoundary>
      <DetachedEditorContent />
    </DetachedErrorBoundary>
  );
}
