import { useState, useEffect, useCallback, useRef } from 'react';
import {
  X,
  Plus,
  Eye,
  EyeOff,
  Pencil,
  Trash2,
  Check,
  XCircle,
  KeyRound,
  FolderOpen,
  ChevronDown,
  ChevronRight,
  AlertCircle,
  Package,
  Lock,
  LockOpen,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { Button } from '../ui/Button';
import { useVaultStore } from '../../stores/useVaultStore';
import { showToast } from '../ui/Toast';
import {
  fetchVaultSecrets,
  fetchVaultSets,
  createVaultSecret,
  getVaultSecret,
  updateVaultSecret,
  deleteVaultSecret,
  createVaultSet,
  deleteVaultSet,
  assignSecretToSet,
  fetchVaultStatus,
  unlockVault,
  lockVault,
} from '../../lib/api';
import type { VaultSecret } from '../../lib/api';
import { VaultLockPrompt } from './VaultLockPrompt';

interface VaultPanelProps {
  onClose: () => void;
}

export function VaultPanel({ onClose }: VaultPanelProps) {
  const secrets = useVaultStore((s) => s.secrets);
  const sets = useVaultStore((s) => s.sets);
  const loading = useVaultStore((s) => s.loading);
  const error = useVaultStore((s) => s.error);
  const locked = useVaultStore((s) => s.locked);
  const encrypted = useVaultStore((s) => s.encrypted);

  // Lock UI state
  const [showLockForm, setShowLockForm] = useState(false);
  const [lockPassphrase, setLockPassphrase] = useState('');
  const [lockConfirm, setLockConfirm] = useState('');
  const [lockError, setLockError] = useState<string | null>(null);
  const [isLocking, setIsLocking] = useState(false);

  // Quick-add form state
  const [newKey, setNewKey] = useState('');
  const [newValue, setNewValue] = useState('');
  const [newSet, setNewSet] = useState('');
  const [showNewValue, setShowNewValue] = useState(false);
  const [addError, setAddError] = useState<string | null>(null);
  const [isAdding, setIsAdding] = useState(false);
  const keyInputRef = useRef<HTMLInputElement>(null);

  // Reveal state: map of key -> revealed value
  const [revealed, setRevealed] = useState<Record<string, string>>({});
  const revealTimers = useRef<Record<string, ReturnType<typeof setTimeout>>>({});

  // Edit state
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');
  const [showEditValue, setShowEditValue] = useState(false);

  // Delete confirmation
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  // Set management
  const [expandedSets, setExpandedSets] = useState<Record<string, boolean>>({});
  const [showNewSet, setShowNewSet] = useState(false);
  const [newSetName, setNewSetName] = useState('');

  // Fetch data on mount
  const refresh = useCallback(async () => {
    useVaultStore.getState().setLoading(true);
    useVaultStore.getState().setError(null);
    try {
      const status = await fetchVaultStatus();
      useVaultStore.getState().setLocked(status.locked);
      useVaultStore.getState().setEncrypted(status.encrypted);

      if (!status.locked) {
        const [secretsData, setsData] = await Promise.all([
          fetchVaultSecrets(),
          fetchVaultSets(),
        ]);
        useVaultStore.getState().setSecrets(secretsData);
        useVaultStore.getState().setSets(setsData);
      }
    } catch (err) {
      useVaultStore.getState().setError(err instanceof Error ? err.message : 'Failed to load vault');
    } finally {
      useVaultStore.getState().setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    return () => {
      // Clear reveal timers on unmount
      Object.values(revealTimers.current).forEach(clearTimeout);
    };
  }, [refresh]);

  // Handle Escape key
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        const tag = (e.target as HTMLElement)?.tagName;
        if (tag === 'INPUT' || tag === 'TEXTAREA') {
          (e.target as HTMLElement).blur();
          return;
        }
        onClose();
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const handleUnlock = useCallback(async (passphrase: string): Promise<boolean> => {
    try {
      await unlockVault(passphrase);
      useVaultStore.getState().setLocked(false);
      await refresh();
      showToast('success', 'Vault unlocked');
      return true;
    } catch {
      return false;
    }
  }, [refresh]);

  const handleLock = useCallback(async () => {
    if (!lockPassphrase.trim()) return;
    if (lockPassphrase !== lockConfirm) {
      setLockError('Passphrases do not match');
      return;
    }

    setIsLocking(true);
    setLockError(null);
    try {
      await lockVault(lockPassphrase);
      setShowLockForm(false);
      setLockPassphrase('');
      setLockConfirm('');
      showToast('success', 'Vault encrypted');
      await refresh();
    } catch (err) {
      setLockError(err instanceof Error ? err.message : 'Failed to lock vault');
    } finally {
      setIsLocking(false);
    }
  }, [lockPassphrase, lockConfirm, refresh]);

  const handleReveal = useCallback(async (key: string) => {
    if (revealed[key]) {
      // Hide
      setRevealed((prev) => {
        const next = { ...prev };
        delete next[key];
        return next;
      });
      if (revealTimers.current[key]) {
        clearTimeout(revealTimers.current[key]);
        delete revealTimers.current[key];
      }
      return;
    }

    try {
      const data = await getVaultSecret(key);
      setRevealed((prev) => ({ ...prev, [key]: data.value }));
      // Auto-hide after 10 seconds
      revealTimers.current[key] = setTimeout(() => {
        setRevealed((prev) => {
          const next = { ...prev };
          delete next[key];
          return next;
        });
        delete revealTimers.current[key];
      }, 10000);
    } catch {
      showToast('error', `Failed to reveal ${key}`);
    }
  }, [revealed]);

  const handleAdd = useCallback(async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newKey.trim() || !newValue) return;

    const key = newKey.trim();
    setIsAdding(true);
    setAddError(null);
    try {
      await createVaultSecret(key, newValue, newSet || undefined);
      setNewKey('');
      setNewValue('');
      setNewSet('');
      setShowNewValue(false);
      await refresh();
      showToast('success', `Secret "${key}" created`);
    } catch (err) {
      setAddError(err instanceof Error ? err.message : 'Failed to create secret');
    } finally {
      setIsAdding(false);
    }
  }, [newKey, newValue, newSet, refresh]);

  const handleEdit = useCallback((key: string) => {
    setEditingKey(key);
    setEditValue('');
    setShowEditValue(false);
  }, []);

  const handleEditSave = useCallback(async () => {
    if (!editingKey || !editValue) return;
    try {
      await updateVaultSecret(editingKey, editValue);
      setEditingKey(null);
      setEditValue('');
      await refresh();
      showToast('success', `Secret "${editingKey}" updated`);
    } catch {
      showToast('error', 'Failed to update secret');
    }
  }, [editingKey, editValue, refresh]);

  const handleEditCancel = useCallback(() => {
    setEditingKey(null);
    setEditValue('');
    setShowEditValue(false);
  }, []);

  const handleDeleteConfirm = useCallback(async () => {
    if (!confirmDelete) return;
    try {
      await deleteVaultSecret(confirmDelete);
      setConfirmDelete(null);
      await refresh();
      showToast('success', `Secret "${confirmDelete}" deleted`);
    } catch {
      showToast('error', 'Failed to delete secret');
    }
  }, [confirmDelete, refresh]);

  const handleCreateSet = useCallback(async () => {
    if (!newSetName.trim()) return;
    try {
      await createVaultSet(newSetName.trim());
      setNewSetName('');
      setShowNewSet(false);
      await refresh();
      showToast('success', `Set "${newSetName.trim()}" created`);
    } catch (err) {
      showToast('error', err instanceof Error ? err.message : 'Failed to create set');
    }
  }, [newSetName, refresh]);

  const handleDeleteSet = useCallback(async (name: string) => {
    try {
      await deleteVaultSet(name);
      await refresh();
      showToast('success', `Set "${name}" deleted`);
    } catch {
      showToast('error', 'Failed to delete set');
    }
  }, [refresh]);

  const handleAssignSet = useCallback(async (key: string, set: string) => {
    try {
      await assignSecretToSet(key, set);
      await refresh();
    } catch {
      showToast('error', 'Failed to assign set');
    }
  }, [refresh]);

  const toggleSetExpand = useCallback((name: string) => {
    setExpandedSets((prev) => ({ ...prev, [name]: !prev[name] }));
  }, []);

  // Group secrets by set
  const unassigned = (secrets ?? []).filter((s) => !s.set);
  const setNames = (sets ?? []).map((s) => s.name);

  const isEmpty = (secrets ?? []).length === 0 && (sets ?? []).length === 0;

  return (
    <div className="fixed inset-y-0 right-0 z-40 w-[380px] max-w-full flex flex-col bg-surface/95 backdrop-blur-xl border-l border-border/50 shadow-2xl animate-slide-in-right">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border/30 flex-shrink-0">
        <div className="flex items-center gap-2">
          <KeyRound size={16} className="text-primary" />
          <h2 className="text-sm font-medium text-text-primary">Vault</h2>
          {encrypted && !locked && (
            <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-status-running/10 text-status-running flex items-center gap-1">
              <LockOpen size={10} />
              Encrypted
            </span>
          )}
          {locked && (
            <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-primary/10 text-primary flex items-center gap-1">
              <Lock size={10} />
              Locked
            </span>
          )}
          {!locked && (secrets ?? []).length > 0 && (
            <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-primary/10 text-primary">
              {(secrets ?? []).length}
            </span>
          )}
        </div>
        <div className="flex items-center gap-1">
          {!locked && !encrypted && (secrets ?? []).length > 0 && (
            <button
              onClick={() => setShowLockForm(!showLockForm)}
              className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors"
              title="Encrypt vault"
            >
              <Lock size={14} className="text-text-muted hover:text-primary" />
            </button>
          )}
          <button
            onClick={onClose}
            className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors"
          >
            <X size={14} className="text-text-muted" />
          </button>
        </div>
      </div>

      {/* Lock prompt */}
      {locked && (
        <VaultLockPrompt onUnlock={handleUnlock} />
      )}

      {/* Content */}
      {!locked && <div className="flex-1 overflow-y-auto scrollbar-dark min-h-0">
        {/* Lock form */}
        {showLockForm && (
          <div className="px-4 pt-3 pb-2 border-b border-border-subtle/50">
            <div className="space-y-2">
              <div className="text-xs text-text-secondary mb-2">Encrypt vault with a passphrase:</div>
              <input
                type="password"
                value={lockPassphrase}
                onChange={(e) => { setLockPassphrase(e.target.value); setLockError(null); }}
                placeholder="New passphrase"
                autoFocus
                className="w-full bg-surface border border-border rounded-lg px-3 py-2 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:ring-1 focus:ring-primary/30 outline-none transition-colors"
              />
              <input
                type="password"
                value={lockConfirm}
                onChange={(e) => { setLockConfirm(e.target.value); setLockError(null); }}
                placeholder="Confirm passphrase"
                className="w-full bg-surface border border-border rounded-lg px-3 py-2 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:ring-1 focus:ring-primary/30 outline-none transition-colors"
                onKeyDown={(e) => { if (e.key === 'Enter') handleLock(); }}
              />
              {lockError && (
                <p className="text-[10px] text-status-error">{lockError}</p>
              )}
              <div className="flex justify-end gap-2">
                <button
                  onClick={() => { setShowLockForm(false); setLockPassphrase(''); setLockConfirm(''); setLockError(null); }}
                  className="px-2 py-1 text-[10px] text-text-secondary hover:text-text-primary rounded transition-colors"
                >
                  Cancel
                </button>
                <Button variant="primary" size="sm" onClick={handleLock} disabled={!lockPassphrase.trim() || !lockConfirm.trim() || isLocking}>
                  <Lock size={12} />
                  {isLocking ? 'Encrypting...' : 'Encrypt'}
                </Button>
              </div>
            </div>
          </div>
        )}

        {/* Error */}
        {error && (
          <div className="mx-4 mt-3 flex items-center gap-2 px-3 py-2 rounded-lg bg-status-error/10 border border-status-error/20 text-xs text-status-error">
            <AlertCircle size={12} className="flex-shrink-0" />
            <span>{error}</span>
          </div>
        )}

        {/* Loading skeleton */}
        {loading && !secrets && (
          <div className="p-4 space-y-3">
            {[1, 2, 3].map((i) => (
              <div key={i} className="h-10 rounded-lg bg-surface-elevated animate-pulse" />
            ))}
          </div>
        )}

        {/* Quick-add form */}
        <form onSubmit={handleAdd} className="px-4 pt-3 pb-2 border-b border-border-subtle/50">
          <div className="space-y-2">
            <input
              ref={keyInputRef}
              type="text"
              value={newKey}
              onChange={(e) => { setNewKey(e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, '')); setAddError(null); }}
              placeholder="KEY_NAME"
              className={cn(
                'w-full bg-surface border rounded-lg px-3 py-2 text-xs font-mono text-text-primary',
                'placeholder:text-text-muted focus:border-primary/50 focus:ring-1 focus:ring-primary/30 outline-none transition-colors',
                addError ? 'border-status-error/50' : 'border-border'
              )}
            />
            <div className="relative">
              <input
                type={showNewValue ? 'text' : 'password'}
                value={newValue}
                onChange={(e) => setNewValue(e.target.value)}
                placeholder="Secret value"
                className="w-full bg-surface border border-border rounded-lg px-3 py-2 pr-10 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 focus:ring-1 focus:ring-primary/30 outline-none transition-colors"
              />
              <button
                type="button"
                onClick={() => setShowNewValue(!showNewValue)}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 p-1 rounded text-text-muted hover:text-text-primary transition-colors"
              >
                {showNewValue ? <EyeOff size={12} /> : <Eye size={12} />}
              </button>
            </div>
            <div className="flex gap-2">
              <select
                value={newSet}
                onChange={(e) => setNewSet(e.target.value)}
                className="flex-1 bg-surface border border-border rounded-lg px-3 py-2 text-xs text-text-secondary focus:border-primary/50 outline-none transition-colors"
              >
                <option value="">No set</option>
                {setNames.map((name) => (
                  <option key={name} value={name}>{name}</option>
                ))}
              </select>
              <Button type="submit" variant="primary" size="sm" disabled={!newKey.trim() || !newValue || isAdding}>
                <Plus size={12} />
                Add
              </Button>
            </div>
          </div>
          {addError && (
            <p className="mt-1.5 text-[10px] text-status-error">{addError}</p>
          )}
        </form>

        {/* Empty state */}
        {!loading && isEmpty && (
          <div className="px-4 py-8 text-center">
            <div className="mx-auto w-12 h-12 mb-4 rounded-xl bg-primary/10 border border-primary/20 flex items-center justify-center">
              <KeyRound size={20} className="text-primary/60" />
            </div>
            <p className="text-sm text-text-secondary mb-2">No secrets stored</p>
            <p className="text-xs text-text-muted leading-relaxed">
              Add secrets using the form above, or via CLI:
            </p>
            <div className="mt-2 space-y-1">
              <code className="block text-[10px] font-mono text-primary/80 bg-surface-elevated rounded px-2 py-1">
                gridctl vault set API_KEY
              </code>
              <code className="block text-[10px] font-mono text-primary/80 bg-surface-elevated rounded px-2 py-1">
                gridctl vault import .env
              </code>
            </div>
          </div>
        )}

        {/* Unassigned secrets */}
        {!loading && unassigned.length > 0 && (
          <div className="px-4 py-2">
            <div className="text-[10px] font-medium text-text-muted uppercase tracking-wider mb-2">
              Secrets
            </div>
            <div className="space-y-1">
              {unassigned.map((secret) => (
                <SecretRow
                  key={secret.key}
                  secret={secret}
                  revealed={revealed[secret.key]}
                  isEditing={editingKey === secret.key}
                  editValue={editValue}
                  showEditValue={showEditValue}
                  onReveal={() => handleReveal(secret.key)}
                  onEdit={() => handleEdit(secret.key)}
                  onDelete={() => setConfirmDelete(secret.key)}
                  onEditValueChange={setEditValue}
                  onEditToggleShow={() => setShowEditValue(!showEditValue)}
                  onEditSave={handleEditSave}
                  onEditCancel={handleEditCancel}
                  sets={setNames}
                  onAssignSet={(set) => handleAssignSet(secret.key, set)}
                />
              ))}
            </div>
          </div>
        )}

        {/* Variable sets */}
        {!loading && (sets ?? []).length > 0 && (
          <div className="px-4 py-2">
            <div className="flex items-center justify-between mb-2">
              <div className="text-[10px] font-medium text-text-muted uppercase tracking-wider">
                Variable Sets
              </div>
              <button
                onClick={() => setShowNewSet(true)}
                className="p-1 rounded hover:bg-surface-highlight transition-colors"
                title="New set"
              >
                <Plus size={12} className="text-text-muted hover:text-primary" />
              </button>
            </div>

            <NewSetForm
              show={showNewSet}
              value={newSetName}
              onChange={setNewSetName}
              onSave={handleCreateSet}
              onCancel={() => { setShowNewSet(false); setNewSetName(''); }}
              className="mb-2"
            />

            <div className="space-y-1">
              {(sets ?? []).map((set) => {
                const setSecrets = (secrets ?? []).filter((s) => s.set === set.name);
                const isExpanded = expandedSets[set.name] ?? false;
                return (
                  <div key={set.name} className="group rounded-lg bg-surface-elevated/50 border border-border/30">
                    <button
                      onClick={() => toggleSetExpand(set.name)}
                      className="w-full flex items-center justify-between px-3 py-2 text-left hover:bg-surface-highlight/50 rounded-lg transition-colors"
                    >
                      <div className="flex items-center gap-2">
                        {isExpanded ? <ChevronDown size={12} className="text-text-muted" /> : <ChevronRight size={12} className="text-text-muted" />}
                        <FolderOpen size={12} className="text-secondary" />
                        <span className="text-xs font-mono text-text-primary">{set.name}</span>
                        <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-secondary/10 text-secondary">
                          {set.count}
                        </span>
                      </div>
                      <button
                        onClick={(e) => { e.stopPropagation(); handleDeleteSet(set.name); }}
                        className="p-1 rounded hover:bg-status-error/10 transition-colors opacity-0 group-hover:opacity-100"
                        title="Delete set"
                      >
                        <Trash2 size={10} className="text-text-muted hover:text-status-error" />
                      </button>
                    </button>
                    {isExpanded && setSecrets.length > 0 && (
                      <div className="px-3 pb-2 space-y-1">
                        {setSecrets.map((secret) => (
                          <SecretRow
                            key={secret.key}
                            secret={secret}
                            revealed={revealed[secret.key]}
                            isEditing={editingKey === secret.key}
                            editValue={editValue}
                            showEditValue={showEditValue}
                            onReveal={() => handleReveal(secret.key)}
                            onEdit={() => handleEdit(secret.key)}
                            onDelete={() => setConfirmDelete(secret.key)}
                            onEditValueChange={setEditValue}
                            onEditToggleShow={() => setShowEditValue(!showEditValue)}
                            onEditSave={handleEditSave}
                            onEditCancel={handleEditCancel}
                            sets={setNames}
                            onAssignSet={(set) => handleAssignSet(secret.key, set)}
                            compact
                          />
                        ))}
                      </div>
                    )}
                    {isExpanded && setSecrets.length === 0 && (
                      <div className="px-3 pb-2">
                        <p className="text-[10px] text-text-muted italic">No secrets in this set</p>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </div>
        )}

        {/* Create set button when no sets exist */}
        {!loading && !isEmpty && (sets ?? []).length === 0 && (
          <div className="px-4 py-2">
            <button
              onClick={() => setShowNewSet(true)}
              className="w-full flex items-center justify-center gap-2 px-3 py-2 rounded-lg border border-dashed border-border/50 text-xs text-text-muted hover:text-text-secondary hover:border-border transition-colors"
            >
              <Package size={12} />
              Create a variable set
            </button>
            <NewSetForm
              show={showNewSet}
              value={newSetName}
              onChange={setNewSetName}
              onSave={handleCreateSet}
              onCancel={() => { setShowNewSet(false); setNewSetName(''); }}
              className="mt-2"
            />
          </div>
        )}
      </div>}

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
              <Button variant="danger" size="sm" onClick={handleDeleteConfirm}>
                Delete
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// NewSetForm renders the inline set creation form
interface NewSetFormProps {
  show: boolean;
  value: string;
  onChange: (val: string) => void;
  onSave: () => void;
  onCancel: () => void;
  className?: string;
}

function NewSetForm({ show, value, onChange, onSave, onCancel, className }: NewSetFormProps) {
  if (!show) return null;
  return (
    <div className={cn('flex gap-2', className)}>
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, ''))}
        placeholder="set-name"
        autoFocus
        className="flex-1 bg-surface border border-border rounded-lg px-2 py-1 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 outline-none transition-colors"
        onKeyDown={(e) => {
          if (e.key === 'Enter') onSave();
          if (e.key === 'Escape') onCancel();
        }}
      />
      <button onClick={onSave} className="p-1 rounded hover:bg-surface-highlight transition-colors" disabled={!value.trim()}>
        <Check size={12} className="text-status-running" />
      </button>
      <button onClick={onCancel} className="p-1 rounded hover:bg-surface-highlight transition-colors">
        <XCircle size={12} className="text-text-muted" />
      </button>
    </div>
  );
}

// SecretRow renders a single secret entry
interface SecretRowProps {
  secret: VaultSecret;
  revealed?: string;
  isEditing: boolean;
  editValue: string;
  showEditValue: boolean;
  onReveal: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onEditValueChange: (val: string) => void;
  onEditToggleShow: () => void;
  onEditSave: () => void;
  onEditCancel: () => void;
  sets: string[];
  onAssignSet: (set: string) => void;
  compact?: boolean;
}

function SecretRow({
  secret,
  revealed,
  isEditing,
  editValue,
  showEditValue,
  onReveal,
  onEdit,
  onDelete,
  onEditValueChange,
  onEditToggleShow,
  onEditSave,
  onEditCancel,
  sets,
  onAssignSet,
  compact,
}: SecretRowProps) {
  if (isEditing) {
    return (
      <div className="rounded-lg bg-surface-elevated/50 border border-primary/20 p-2 space-y-2">
        <div className="text-xs font-mono text-primary">{secret.key}</div>
        <div className="relative">
          <input
            type={showEditValue ? 'text' : 'password'}
            value={editValue}
            onChange={(e) => onEditValueChange(e.target.value)}
            placeholder="New value"
            autoFocus
            className="w-full bg-surface border border-border rounded-lg px-2 py-1.5 pr-8 text-xs font-mono text-text-primary placeholder:text-text-muted focus:border-primary/50 outline-none transition-colors"
            onKeyDown={(e) => {
              if (e.key === 'Enter') onEditSave();
              if (e.key === 'Escape') onEditCancel();
            }}
          />
          <button
            type="button"
            onClick={onEditToggleShow}
            className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 rounded text-text-muted hover:text-text-primary transition-colors"
          >
            {showEditValue ? <EyeOff size={10} /> : <Eye size={10} />}
          </button>
        </div>
        <div className="flex justify-end gap-1.5">
          <button
            onClick={onEditCancel}
            className="px-2 py-1 text-[10px] text-text-secondary hover:text-text-primary rounded transition-colors"
          >
            Cancel
          </button>
          <Button variant="primary" size="sm" onClick={onEditSave} disabled={!editValue}>
            Save
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className={cn(
      'group flex items-center gap-2 rounded-lg hover:bg-surface-highlight/50 transition-colors',
      compact ? 'px-2 py-1.5' : 'px-3 py-2'
    )}>
      <div className="flex-1 min-w-0">
        <div className="text-xs font-mono text-text-primary truncate">{secret.key}</div>
        <div className="text-[10px] font-mono text-text-muted truncate">
          {revealed !== undefined ? revealed : '\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022'}
        </div>
      </div>
      <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100 transition-opacity">
        {!compact && sets.length > 0 && !secret.set && (
          <select
            value=""
            onChange={(e) => { if (e.target.value) onAssignSet(e.target.value); }}
            className="w-16 bg-transparent border-none text-[10px] text-text-muted focus:outline-none cursor-pointer"
            title="Assign to set"
          >
            <option value="">Set...</option>
            {sets.map((name) => (
              <option key={name} value={name}>{name}</option>
            ))}
          </select>
        )}
        <button
          onClick={onReveal}
          className="p-1 rounded hover:bg-surface-highlight transition-colors"
          title={revealed !== undefined ? 'Hide value' : 'Reveal value'}
        >
          {revealed !== undefined ? <EyeOff size={12} className="text-text-muted" /> : <Eye size={12} className="text-text-muted" />}
        </button>
        <button
          onClick={onEdit}
          className="p-1 rounded hover:bg-surface-highlight transition-colors"
          title="Edit value"
        >
          <Pencil size={12} className="text-text-muted" />
        </button>
        <button
          onClick={onDelete}
          className="p-1 rounded hover:bg-status-error/10 transition-colors"
          title="Delete"
        >
          <Trash2 size={12} className="text-text-muted hover:text-status-error" />
        </button>
      </div>
    </div>
  );
}
