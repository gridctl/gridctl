import { useState, useEffect, useCallback } from 'react';
import { Save, FolderOpen, Trash2, Clock, Loader2 } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useWizardStore, type WizardDraft } from '../../stores/useWizardStore';
import { fetchWizardDrafts, saveWizardDraft, deleteWizardDraft } from '../../lib/api';
import { showToast } from '../ui/Toast';
import { ConfirmDialog } from '../ui/ConfirmDialog';

interface DraftManagerProps {
  className?: string;
}

export function DraftManager({ className }: DraftManagerProps) {
  const { drafts, setDrafts, setDraftsLoading, draftsLoading, loadDraft, selectedType, formData } =
    useWizardStore();
  const [showList, setShowList] = useState(false);
  const [saveName, setSaveName] = useState('');
  const [showSave, setShowSave] = useState(false);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [pendingDelete, setPendingDelete] = useState<WizardDraft | null>(null);

  const refreshDrafts = useCallback(async () => {
    setDraftsLoading(true);
    try {
      const result = await fetchWizardDrafts();
      setDrafts(result);
    } catch {
      // Silently fail — drafts are optional
    } finally {
      setDraftsLoading(false);
    }
  }, [setDrafts, setDraftsLoading]);

  useEffect(() => {
    refreshDrafts();
  }, [refreshDrafts]);

  const handleSave = async () => {
    if (!saveName.trim() || !selectedType) return;
    setSaving(true);
    try {
      const currentData = formData[selectedType as keyof typeof formData] as unknown as Record<string, unknown>;
      await saveWizardDraft({
        name: saveName.trim(),
        resourceType: selectedType,
        formData: currentData,
      });
      showToast('success', `Draft "${saveName}" saved`);
      setSaveName('');
      setShowSave(false);
      await refreshDrafts();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Failed to save draft');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (id: string) => {
    setDeleting(id);
    try {
      await deleteWizardDraft(id);
      showToast('success', 'Draft deleted');
      await refreshDrafts();
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Failed to delete draft');
    } finally {
      setDeleting(null);
    }
  };

  const handleLoad = (draft: WizardDraft) => {
    loadDraft(draft);
    setShowList(false);
    showToast('success', `Loaded draft "${draft.name}"`);
  };

  const formatDate = (dateStr: string) => {
    const date = new Date(dateStr);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMin = Math.floor(diffMs / 60000);
    if (diffMin < 1) return 'just now';
    if (diffMin < 60) return `${diffMin}m ago`;
    const diffHr = Math.floor(diffMin / 60);
    if (diffHr < 24) return `${diffHr}h ago`;
    return date.toLocaleDateString();
  };

  return (
    <div className={cn('flex items-center gap-1.5', className)}>
      {/* Save Button */}
      <button
        onClick={() => {
          setShowSave(!showSave);
          setShowList(false);
        }}
        disabled={!selectedType}
        className={cn(
          'flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs transition-all duration-200',
          'text-text-muted hover:text-text-secondary hover:bg-surface-highlight/60',
          'disabled:opacity-40 disabled:cursor-not-allowed',
        )}
        title="Save as draft"
      >
        <Save size={12} />
        Save
      </button>

      {/* Load Button */}
      <button
        onClick={() => {
          setShowList(!showList);
          setShowSave(false);
        }}
        className={cn(
          'flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-xs transition-all duration-200',
          'text-text-muted hover:text-text-secondary hover:bg-surface-highlight/60',
          drafts.length > 0 && 'text-text-secondary',
        )}
        title="Load draft"
      >
        <FolderOpen size={12} />
        Drafts
        {drafts.length > 0 && (
          <span className="text-[10px] bg-surface-highlight px-1.5 py-0.5 rounded-full text-text-muted">
            {drafts.length}
          </span>
        )}
      </button>

      {/* Save Dialog */}
      {showSave && (
        <div className="absolute top-full right-0 mt-2 z-popover w-64 bg-surface-elevated border border-border/50 rounded-xl shadow-lg p-3 animate-fade-in-scale">
          <div className="text-xs font-medium text-text-secondary mb-2">Save Draft</div>
          <div className="flex gap-2">
            <input
              type="text"
              value={saveName}
              onChange={(e) => setSaveName(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleSave()}
              placeholder="Draft name..."
              aria-label="Draft name"
              className="flex-1 bg-background/60 border border-border/40 rounded-lg px-3 py-1.5 text-xs focus:outline-none focus:border-primary/50 text-text-primary placeholder:text-text-muted/50"
              autoFocus
            />
            <button
              onClick={handleSave}
              disabled={!saveName.trim() || saving}
              className={cn(
                'px-3 py-1.5 rounded-lg text-xs font-medium transition-all duration-200',
                'bg-primary/15 text-primary hover:bg-primary/25',
                'disabled:opacity-40 disabled:cursor-not-allowed',
              )}
            >
              {saving ? <Loader2 size={12} className="animate-spin" /> : 'Save'}
            </button>
          </div>
        </div>
      )}

      {/* Drafts List */}
      {showList && (
        <div className="absolute top-full right-0 mt-2 z-popover w-72 bg-surface-elevated border border-border/50 rounded-xl shadow-lg overflow-hidden animate-fade-in-scale">
          <div className="px-3 py-2.5 border-b border-border/30">
            <div className="text-xs font-medium text-text-secondary">Saved Drafts</div>
          </div>
          <div className="max-h-48 overflow-y-auto scrollbar-dark">
            {draftsLoading ? (
              <div className="flex items-center justify-center py-6">
                <Loader2 size={14} className="text-text-muted animate-spin" />
              </div>
            ) : drafts.length === 0 ? (
              <div className="text-center py-6 text-xs text-text-muted">No saved drafts</div>
            ) : (
              drafts.map((draft) => (
                <div
                  key={draft.id}
                  className="flex items-center justify-between px-3 py-2 hover:bg-surface-highlight/60 transition-colors group"
                >
                  <button
                    onClick={() => handleLoad(draft)}
                    className="flex-1 text-left min-w-0"
                  >
                    <div className="text-xs font-medium text-text-primary truncate">
                      {draft.name}
                    </div>
                    <div className="flex items-center gap-1.5 text-[10px] text-text-muted mt-0.5">
                      <span className="capitalize">{draft.resourceType}</span>
                      <span>·</span>
                      <Clock size={9} />
                      <span>{formatDate(draft.updatedAt)}</span>
                    </div>
                  </button>
                  <button
                    type="button"
                    onClick={() => setPendingDelete(draft)}
                    disabled={deleting === draft.id}
                    title="Delete draft"
                    aria-label={`Delete draft ${draft.name}`}
                    className="p-1 rounded text-text-muted/40 hover:text-status-error opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 hover-none:opacity-100 transition-all duration-200"
                  >
                    {deleting === draft.id ? (
                      <Loader2 size={12} className="animate-spin" />
                    ) : (
                      <Trash2 size={12} />
                    )}
                  </button>
                </div>
              ))
            )}
          </div>
        </div>
      )}

      <ConfirmDialog
        isOpen={pendingDelete !== null}
        onClose={() => setPendingDelete(null)}
        onConfirm={() => {
          const id = pendingDelete?.id;
          setPendingDelete(null);
          if (id) void handleDelete(id);
        }}
        title="Delete draft"
        message={
          <p>
            Delete draft <span className="font-mono text-primary">{pendingDelete?.name}</span> ({pendingDelete?.resourceType})?
          </p>
        }
        confirmLabel={`Delete "${pendingDelete?.name ?? ''}"`}
        variant="danger"
      />
    </div>
  );
}
