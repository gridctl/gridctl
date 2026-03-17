import { useEffect, useState, useCallback, useRef, useMemo, Component, type ReactNode } from 'react';
import {
  KeyRound,
  Plus,
  Eye,
  EyeOff,
  Pencil,
  Trash2,
  Check,
  XCircle,
  FolderOpen,
  ChevronDown,
  ChevronRight,
  AlertCircle,
  Package,
  Lock,
  LockOpen,
  RefreshCw,
  Search,
  X,
} from 'lucide-react';
import { cn } from '../lib/cn';
import { IconButton } from '../components/ui/IconButton';
import { Button } from '../components/ui/Button';
import { ZoomControls } from '../components/log/ZoomControls';
import { VaultLockPrompt } from '../components/vault/VaultLockPrompt';
import { ToastContainer, showToast } from '../components/ui/Toast';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { useLogFontSize } from '../hooks/useLogFontSize';
import { useVaultStore } from '../stores/useVaultStore';
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
} from '../lib/api';
import type { VaultSecret } from '../lib/api';
import { POLLING } from '../lib/constants';

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

function DetachedVaultContent() {
  const secrets = useVaultStore((s) => s.secrets);
  const sets = useVaultStore((s) => s.sets);
  const loading = useVaultStore((s) => s.loading);
  const error = useVaultStore((s) => s.error);
  const locked = useVaultStore((s) => s.locked);
  const encrypted = useVaultStore((s) => s.encrypted);

  // Search state
  const [searchQuery, setSearchQuery] = useState('');

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

  // Reveal state
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

  // Text zoom
  const contentRef = useRef<HTMLElement>(null);
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } =
    useLogFontSize(contentRef);

  // Register with main window
  useDetachedWindowSync('vault');

  // Filtered secrets
  const allSecrets = secrets ?? [];
  const filteredSecrets = useMemo(() => {
    if (!searchQuery) return allSecrets;
    const lower = searchQuery.toLowerCase();
    return allSecrets.filter(
      (s) => s.key.toLowerCase().includes(lower) || (s.set ?? '').toLowerCase().includes(lower),
    );
  }, [allSecrets, searchQuery]);

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
    const interval = window.setInterval(refresh, POLLING.STATUS);
    return () => {
      clearInterval(interval);
      Object.values(revealTimers.current).forEach(clearTimeout);
    };
  }, [refresh]);

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

  const unassigned = filteredSecrets.filter((s) => !s.set);
  const setNames = (sets ?? []).map((s) => s.name);
  const isEmpty = allSecrets.length === 0 && (sets ?? []).length === 0;

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
            <KeyRound size={14} className="text-primary" />
          </div>
          <div className="flex items-center gap-2">
            <span className="text-sm font-semibold text-text-primary tracking-tight">Vault</span>
            <span className="text-[10px] text-text-muted uppercase tracking-wider">Secrets</span>
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
          {!locked && !encrypted && allSecrets.length > 0 && (
            <button
              onClick={() => setShowLockForm(!showLockForm)}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-primary hover:text-primary/80 bg-primary/10 hover:bg-primary/15 border border-primary/20 rounded-lg transition-colors"
            >
              <Lock size={12} /> Encrypt
            </button>
          )}
          <IconButton icon={RefreshCw} onClick={refresh} tooltip="Refresh" size="sm" variant="ghost" />
        </div>
      </header>

      {/* Lock prompt */}
      {locked && (
        <VaultLockPrompt onUnlock={handleUnlock} />
      )}

      {/* Unlocked content */}
      {!locked && (
        <>
          {/* Item count + New button */}
          <div className="border-b border-border/20 flex-shrink-0 z-10 relative px-4 py-2">
            <div className="max-w-2xl mx-auto flex items-center justify-between">
            <span className="text-[10px] text-text-muted">
              {searchQuery
                ? `${filteredSecrets.length} of ${allSecrets.length} secrets`
                : `${allSecrets.length} secrets`}
            </span>
            <button
              onClick={() => {/* scroll to add form */}}
              className="flex items-center gap-1 text-[10px] text-primary hover:text-primary/80 transition-colors"
            >
              <Plus size={10} /> New
            </button>
            </div>
          </div>

          {/* Search */}
          <div className="px-2 py-1.5 border-b border-border/20 flex-shrink-0 z-10 relative" role="search">
            <div className="max-w-2xl mx-auto relative">
              <Search size={12} className="absolute left-2 top-1/2 -translate-y-1/2 text-text-muted/50" />
              <input
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="Search secrets..."
                aria-label="Filter secrets"
                className="w-full bg-background/40 border border-border/30 rounded-lg pl-7 pr-7 py-1 text-xs text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/40"
              />
              {searchQuery && (
                <button
                  onClick={() => setSearchQuery('')}
                  className="absolute right-2 top-1/2 -translate-y-1/2 p-0.5 rounded hover:bg-surface-highlight transition-colors"
                >
                  <X size={12} className="text-text-muted" />
                </button>
              )}
            </div>
          </div>

          {/* Scrollable content */}
          <main
            ref={contentRef}
            className="flex-1 overflow-y-auto scrollbar-dark relative z-10"
            style={{ '--log-font-size': `${fontSize}px` } as React.CSSProperties}
          >
            {/* Lock form */}
            {showLockForm && (
              <div className="px-4 pt-3 pb-2 border-b border-border-subtle/50 max-w-2xl mx-auto">
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
              <div className="max-w-2xl mx-auto mt-3 px-4 flex items-center gap-2 py-2 rounded-lg bg-status-error/10 border border-status-error/20 text-xs text-status-error">
                <AlertCircle size={12} className="flex-shrink-0" />
                <span>{error}</span>
              </div>
            )}

            {/* Loading skeleton */}
            {loading && !secrets && (
              <div className="p-4 space-y-3 max-w-2xl mx-auto">
                {[1, 2, 3].map((i) => (
                  <div key={i} className="h-10 rounded-lg bg-surface-elevated animate-pulse" />
                ))}
              </div>
            )}

            {/* Quick-add form */}
            <form onSubmit={handleAdd} className="px-4 pt-3 pb-2 border-b border-border-subtle/50 max-w-2xl mx-auto">
              <div className="space-y-2">
                <input
                  type="text"
                  value={newKey}
                  onChange={(e) => { setNewKey(e.target.value.toUpperCase().replace(/[^A-Z0-9_]/g, '')); setAddError(null); }}
                  placeholder="KEY_NAME"
                  className={cn(
                    'w-full bg-surface border rounded-lg px-3 py-2 text-xs font-mono text-text-primary log-text',
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
                    className="w-full bg-surface border border-border rounded-lg px-3 py-2 pr-10 text-xs font-mono text-text-primary log-text placeholder:text-text-muted focus:border-primary/50 focus:ring-1 focus:ring-primary/30 outline-none transition-colors"
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
              <div className="px-4 py-8 text-center max-w-md mx-auto">
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

            {/* Search empty state */}
            {!loading && !isEmpty && filteredSecrets.length === 0 && searchQuery && (
              <div className="p-6 text-center max-w-md mx-auto">
                <KeyRound size={24} className="text-text-muted/30 mx-auto mb-2" />
                <p className="text-text-muted text-xs">No matching secrets</p>
              </div>
            )}

            {/* Unassigned secrets */}
            {!loading && unassigned.length > 0 && (
              <div className="p-3 space-y-2 max-w-2xl mx-auto">
                {unassigned.map((secret) => (
                  <SecretItem
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
            )}

            {/* Variable sets */}
            {!loading && (sets ?? []).length > 0 && (
              <div className="px-3 py-2 max-w-2xl mx-auto">
                <div className="flex items-center justify-between px-2 mb-2">
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
                  className="mb-2 px-2"
                />

                <div className="space-y-2">
                  {(sets ?? []).map((set) => {
                    const setSecrets = filteredSecrets.filter((s) => s.set === set.name);
                    const isExpanded = expandedSets[set.name] ?? false;
                    return (
                      <div key={set.name} className="group rounded-lg bg-surface-elevated/50 border border-border-subtle overflow-hidden">
                        <button
                          onClick={() => toggleSetExpand(set.name)}
                          className="w-full flex items-center justify-between px-3 py-2 text-left hover:bg-surface-highlight/50 transition-colors"
                        >
                          <div className="flex items-center gap-2">
                            {isExpanded ? <ChevronDown size={12} className="text-text-muted" /> : <ChevronRight size={12} className="text-text-muted" />}
                            <FolderOpen size={12} className="text-secondary" />
                            <span className="text-xs font-mono text-text-primary log-text">{set.name}</span>
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
                          <div className="px-2 pb-2 space-y-1">
                            {setSecrets.map((secret) => (
                              <SecretItem
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
              <div className="px-4 py-2 max-w-2xl mx-auto">
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
          </main>
        </>
      )}

      {/* Status footer */}
      <footer className="h-6 flex-shrink-0 bg-surface/90 backdrop-blur-xl border-t border-border/50 flex items-center px-4 z-10">
        <div className="max-w-2xl mx-auto w-full flex items-center justify-between text-[10px] text-text-muted">
          <span>
            {allSecrets.length > 0 ? `${allSecrets.length} secrets` : ''}
            {(sets ?? []).length > 0 ? ` \u00B7 ${(sets ?? []).length} sets` : ''}
            {locked ? 'Vault locked' : ''}
          </span>
          <span className="flex items-center gap-1">
            <span className="w-1.5 h-1.5 rounded-full bg-status-running animate-pulse" />
            Detached Window
          </span>
        </div>
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
              <Button variant="danger" size="sm" onClick={handleDeleteConfirm}>
                Delete
              </Button>
            </div>
          </div>
        </div>
      )}

      <ToastContainer />
    </div>
  );
}

// SecretItem — expandable row matching SkillItem pattern
interface SecretItemProps {
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

function SecretItem({
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
}: SecretItemProps) {
  const [expanded, setExpanded] = useState(false);

  if (isEditing) {
    return (
      <div className="rounded-lg bg-surface-elevated/50 border border-primary/20 p-2 space-y-2">
        <div className="text-xs font-mono text-primary log-text">{secret.key}</div>
        <div className="relative">
          <input
            type={showEditValue ? 'text' : 'password'}
            value={editValue}
            onChange={(e) => onEditValueChange(e.target.value)}
            placeholder="New value"
            autoFocus
            className="w-full bg-surface border border-border rounded-lg px-2 py-1.5 pr-8 text-xs font-mono text-text-primary log-text placeholder:text-text-muted focus:border-primary/50 outline-none transition-colors"
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
    <div className="rounded-lg bg-surface-elevated/50 border border-border-subtle overflow-hidden">
      {/* Header row */}
      <button
        onClick={() => setExpanded(!expanded)}
        className={cn(
          'w-full flex items-center gap-2 hover:bg-surface-highlight/50 transition-colors',
          compact ? 'px-2 py-1.5' : 'p-3',
        )}
      >
        <div className="p-0.5 text-text-muted">
          {expanded ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
        </div>
        <KeyRound size={12} className="text-primary/60 flex-shrink-0" />
        <span className="text-xs font-mono font-medium text-text-primary flex-1 text-left truncate log-text">
          {secret.key}
        </span>
        <span className="text-[10px] font-mono text-text-muted truncate max-w-[100px] log-text-detail">
          {revealed !== undefined ? revealed : '\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022'}
        </span>
      </button>

      {/* Expanded content */}
      {expanded && (
        <div className="px-3 pb-3 border-t border-border-subtle">
          {/* Value display */}
          <div className="mt-2 mb-2">
            <div className="text-[10px] text-text-muted mb-1">Value</div>
            <div className="text-xs font-mono text-text-secondary bg-background/60 px-2 py-1.5 rounded break-all log-text">
              {revealed !== undefined ? revealed : '\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022\u2022'}
            </div>
          </div>

          {/* Set assignment */}
          {!compact && sets.length > 0 && !secret.set && (
            <div className="mb-2">
              <select
                value=""
                onChange={(e) => { if (e.target.value) onAssignSet(e.target.value); }}
                className="w-full bg-surface border border-border rounded-lg px-2 py-1 text-[10px] text-text-muted focus:outline-none focus:border-primary/40 transition-colors"
                title="Assign to set"
              >
                <option value="">Assign to set...</option>
                {sets.map((name) => (
                  <option key={name} value={name}>{name}</option>
                ))}
              </select>
            </div>
          )}

          {/* Actions */}
          <div className="flex items-center gap-1.5 mt-2 pt-2 border-t border-border-subtle/50">
            <button
              onClick={(e) => { e.stopPropagation(); onReveal(); }}
              className={cn(
                'flex items-center gap-1 px-2.5 py-1 rounded-lg text-[10px] font-semibold transition-all duration-200',
                revealed !== undefined
                  ? 'bg-status-pending text-background shadow-[0_1px_8px_rgba(234,179,8,0.2)] hover:shadow-[0_2px_12px_rgba(234,179,8,0.3)] hover:-translate-y-0.5 active:translate-y-0'
                  : 'bg-status-running text-background shadow-[0_1px_8px_rgba(16,185,129,0.2)] hover:shadow-[0_2px_12px_rgba(16,185,129,0.3)] hover:-translate-y-0.5 active:translate-y-0'
              )}
            >
              {revealed !== undefined ? <EyeOff size={10} /> : <Eye size={10} />}
              {revealed !== undefined ? 'Hide' : 'Reveal'}
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); onEdit(); }}
              className="flex items-center gap-1 px-2.5 py-1 rounded-lg text-[10px] font-semibold bg-gradient-to-r from-primary to-primary-dark text-background shadow-[0_1px_8px_rgba(245,158,11,0.2)] hover:shadow-[0_2px_12px_rgba(245,158,11,0.3)] hover:-translate-y-0.5 active:translate-y-0 transition-all duration-200"
            >
              <Pencil size={10} /> Edit
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); onDelete(); }}
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

// NewSetForm
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
        className="flex-1 bg-surface border border-border rounded-lg px-2 py-1 text-xs font-mono text-text-primary log-text placeholder:text-text-muted focus:border-primary/50 outline-none transition-colors"
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

export function DetachedVaultPage() {
  return (
    <DetachedErrorBoundary>
      <DetachedVaultContent />
    </DetachedErrorBoundary>
  );
}
