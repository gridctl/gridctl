import { useEffect, useState, useCallback, useRef, type ReactNode } from 'react';
import { flushSync } from 'react-dom';
import { useSearchParams } from 'react-router';
import { AlertCircle } from 'lucide-react';
import { SkillEditor } from '../components/registry/SkillEditor';
import { showToast } from '../components/ui/Toast';
import { ErrorBoundary } from '../components/ui/ErrorBoundary';
import { DetachedWindowFrame } from '../components/detached/DetachedWindowFrame';
import {
  fetchRegistrySkill,
  fetchRegistryStatus,
  fetchRegistrySkills,
  fetchSkillSources,
} from '../lib/api';
import { useRegistryStore } from '../stores/useRegistryStore';
import type { AgentSkill, SkillSourceStatus } from '../types';
import { useAuthStore } from '../stores/useAuthStore';

function DetachedEditorContent() {
  const generation = useAuthStore(s => s.generation);
  const authRequired = useAuthStore(s => s.authRequired);
  const [searchParams] = useSearchParams();
  const rawType = searchParams.get('type');
  // Always treat as skill editor (gracefully handle legacy "prompt" type)
  const editorType = (rawType === 'skill' || rawType === 'prompt' || !rawType) ? 'skill' : null;
  const itemName = rawType === 'prompt' ? null : searchParams.get('name');

  const [skill, setSkill] = useState<AgentSkill | undefined>();
  const [loading, setLoading] = useState(!!itemName);
  const [error, setError] = useState<string | null>(null);
  const [editorOpen, setEditorOpen] = useState(true);
  const [source, setSource] = useState<SkillSourceStatus | undefined>();
  const refreshPending = useRef(false);

  const refreshRegistry = useCallback(async () => {
    const started = useAuthStore.getState().generation;
    try {
      const [regStatus, regSkills] = await Promise.all([
        fetchRegistryStatus(),
        fetchRegistrySkills(),
      ]);
      if (useAuthStore.getState().generation !== started) return false;
      useRegistryStore.getState().setStatus(regStatus);
      useRegistryStore.getState().setSkills(regSkills);
      return true;
    } catch {
      return false;
    }
  }, []);

  // No item to load means nothing pending (state adjustment during render;
  // converges in one pass).
  if (!itemName && loading) setLoading(false);

  // Load the item being edited
  useEffect(() => {
    if (!itemName || authRequired || skill?.name === itemName) return;
    let cancelled = false;
    const current = () => !cancelled && useAuthStore.getState().generation === generation;

    const loadItem = async () => {
      try {
        if (editorType === 'skill') {
          const s = await fetchRegistrySkill(itemName);
          if (!current()) return;
          setSkill(s);
          setError(null);
        }
      } catch (err) {
        if (current()) setError(err instanceof Error ? err.message : 'Failed to load item');
      } finally {
        if (current()) setLoading(false);
      }
    };

    loadItem();
    return () => { cancelled = true; };
  }, [editorType, itemName, generation, authRequired, skill]);

  // Provenance for an imported skill. A failed fetch leaves the editor
  // without the strip.
  const loadedName = skill?.name;
  useEffect(() => {
    if (!loadedName || authRequired) return;
    let cancelled = false;
    fetchSkillSources()
      .then((sources) => {
        if (cancelled || useAuthStore.getState().generation !== generation) return;
        setSource(sources.find((src) => (src.skills ?? []).some((entry) => entry.name === loadedName)));
      })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [loadedName, generation, authRequired]);

  const handleClose = useCallback(() => {
    // window.close() fires beforeunload before it returns. isOpen has to be
    // false already, so the guard's effect cleanup removes the listener first.
    // A later render is too late: the browser asks during this call.
    flushSync(() => setEditorOpen(false));
    window.close();
  }, []);

  const handleSaved = useCallback(async () => {
    refreshPending.current = true;
    const refreshed = await refreshRegistry();
    refreshPending.current = !refreshed;
    if (!refreshed) showToast('warning', 'Saved successfully, but the Library refresh did not complete. The editor remains open.');
    return refreshed;
  }, [refreshRegistry]);

  useEffect(() => {
    if (authRequired || !refreshPending.current) return;
    void refreshRegistry().then(refreshed => { if (refreshed) refreshPending.current = false; });
  }, [generation, authRequired, refreshRegistry]);

  let content: ReactNode;
  if (!editorType) {
    content = (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center text-text-muted">
          <AlertCircle size={32} className="mx-auto mb-3 opacity-50" />
          <p className="text-sm">No editor type specified</p>
        </div>
      </div>
    );
  } else if (loading) {
    content = (
      <div className="flex-1 flex items-center justify-center">
        <div className="text-center space-y-4">
          <div className="w-10 h-10 mx-auto border-2 border-primary border-t-transparent rounded-full animate-spin" />
          <p className="text-sm text-text-muted">Loading skill...</p>
        </div>
      </div>
    );
  } else if (error) {
    content = (
      <div className="flex-1 flex items-center justify-center">
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
  } else {
    // Rendered flush: the editor fills the viewport above everything but toasts.
    content = (
      <SkillEditor
        isOpen={editorOpen}
        onClose={handleClose}
        onSaved={handleSaved}
        skill={skill}
        source={skill ? source : undefined}
        flush
      />
    );
  }

  // No header, banner, or footer: the flush editor's opaque modal layer
  // would cover them.
  return (
    <DetachedWindowFrame windowType="editor" titleDetail={itemName} footer={false}>
      {content}
    </DetachedWindowFrame>
  );
}

export function DetachedEditorPage() {
  return (
    <ErrorBoundary variant="window">
      <DetachedEditorContent />
    </ErrorBoundary>
  );
}
