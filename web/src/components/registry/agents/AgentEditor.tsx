import { useCallback, useEffect, useState } from 'react';
import { AlertTriangle, Eye, Loader2, Pencil } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { Modal } from '../../ui/Modal';
import { showToast } from '../../ui/Toast';
import { MarkdownPreview } from '../MarkdownPreview';
import { bodyFromRaw } from './agentModel';
import { AgentScanError, fetchRegistryAgent, updateRegistryAgent } from '../../../lib/api';
import type { RegistryAgent, SecurityFinding } from '../../../types';

interface AgentEditorProps {
  isOpen: boolean;
  agent: RegistryAgent | null;
  onClose: () => void;
  onSaved: () => Promise<void> | void;
}

/**
 * Single-file editor for an AGENT.md. The whole raw file (frontmatter
 * included) is the editing surface: the server re-parses, refuses renames,
 * runs the blocking security scan, and writes the bytes verbatim, so
 * unknown frontmatter keys round-trip untouched simply because the editor
 * never restructures them. Follows SkillEditor's edit/preview grammar
 * without its file tree — agents are one file by design.
 */
export function AgentEditor({ isOpen, agent, onClose, onSaved }: AgentEditorProps) {
  const [raw, setRaw] = useState('');
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [mode, setMode] = useState<'edit' | 'preview'>('edit');
  const [error, setError] = useState<string | null>(null);
  const [findings, setFindings] = useState<SecurityFinding[] | null>(null);

  const name = agent?.name ?? null;
  const loadKey = isOpen && name ? name : null;

  // Render-time reset when the editor opens on a (new) agent: the fetch
  // itself stays in the effect below, but the synchronous state resets
  // happen during render (the adjust-during-render pattern) so the effect
  // never calls setState synchronously.
  const [prevKey, setPrevKey] = useState<string | null>(null);
  if (loadKey !== prevKey) {
    setPrevKey(loadKey);
    setRaw('');
    setError(null);
    setFindings(null);
    setDirty(false);
    setMode('edit');
    setLoading(loadKey !== null);
  }

  // Load the verbatim file when the editor opens. The list payload omits
  // raw, so this always fetches the single agent.
  useEffect(() => {
    if (!loadKey) return;
    let cancelled = false;
    fetchRegistryAgent(loadKey)
      .then((full) => {
        if (!cancelled) setRaw(full.raw ?? '');
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load agent');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [loadKey]);

  const handleSave = useCallback(async () => {
    if (!name || saving) return;
    setSaving(true);
    setError(null);
    setFindings(null);
    try {
      await updateRegistryAgent(name, raw);
      showToast('success', `Agent "${name}" saved`);
      setDirty(false);
      await onSaved();
      onClose();
    } catch (err) {
      if (err instanceof AgentScanError) {
        setFindings(err.findings);
        setError(err.message);
      } else {
        setError(err instanceof Error ? err.message : 'Save failed');
      }
    } finally {
      setSaving(false);
    }
  }, [name, raw, saving, onSaved, onClose]);

  if (!isOpen || !agent) return null;

  return (
    <Modal isOpen onClose={onClose} title={`Edit ${agent.name}`} size="wide">
      <div className="flex flex-col gap-3">
        {/* Mode toggle, matching the workspace's aria-pressed segment treatment. */}
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-1" role="group" aria-label="Editor mode">
            {([
              { key: 'edit' as const, label: 'Edit', icon: Pencil },
              { key: 'preview' as const, label: 'Preview', icon: Eye },
            ]).map(({ key, label, icon: Icon }) => (
              <button
                key={key}
                onClick={() => setMode(key)}
                aria-pressed={mode === key}
                className={cn(
                  'inline-flex items-center gap-1.5 px-2 py-1 rounded-md text-[11px] font-medium transition-colors border',
                  mode === key
                    ? 'bg-primary/10 text-primary border-primary/25'
                    : 'text-text-muted hover:text-text-secondary hover:bg-surface-highlight border-transparent',
                )}
              >
                <Icon size={12} /> {label}
              </button>
            ))}
          </div>
          <span className="text-[10px] text-text-muted">
            The whole file is saved verbatim; unknown frontmatter keys round-trip untouched.
          </span>
        </div>

        {mode === 'edit' ? (
          <textarea
            value={raw}
            onChange={(e) => {
              setRaw(e.target.value);
              setDirty(true);
            }}
            disabled={loading}
            spellCheck={false}
            aria-label={`AGENT.md content for ${agent.name}`}
            className="w-full h-80 bg-background/60 border border-border/40 rounded-lg p-3 text-xs font-mono text-text-primary leading-relaxed resize-y focus:outline-none focus:border-primary/50 transition-colors scrollbar-dark"
          />
        ) : (
          <div className="h-80 overflow-y-auto scrollbar-dark border border-border/30 rounded-lg p-3 skill-md">
            <MarkdownPreview content={bodyFromRaw(raw)} emptyHint="Nothing to preview yet." />
          </div>
        )}

        {error && (
          <div className="text-xs text-status-error whitespace-pre-wrap" role="alert">
            {error}
          </div>
        )}
        {findings && findings.length > 0 && (
          <div className="space-y-1" role="alert">
            <span className="text-[10px] text-text-muted uppercase tracking-wider">
              Security findings blocked the save
            </span>
            {findings.map((f, i) => (
              <div
                key={i}
                className="text-[11px] px-2 py-1.5 rounded-lg flex items-start gap-1.5 bg-status-error/10 text-status-error"
              >
                <AlertTriangle size={11} className="flex-shrink-0 mt-0.5" />
                <span>{f.description}</span>
              </div>
            ))}
          </div>
        )}

        <div className="flex items-center justify-end gap-2 pt-1 border-t border-border/20">
          <button
            onClick={onClose}
            disabled={saving}
            className="px-3 py-1.5 text-xs text-text-muted border border-border/40 rounded-lg hover:bg-surface-highlight transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={() => void handleSave()}
            disabled={saving || loading || !dirty}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-primary bg-primary/10 border border-primary/25 rounded-lg hover:bg-primary/15 transition-colors disabled:opacity-50"
          >
            {saving && <Loader2 size={12} className="animate-spin" />}
            {saving ? 'Saving…' : 'Save'}
          </button>
        </div>
      </div>
    </Modal>
  );
}
