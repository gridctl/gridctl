import { useEffect, useState, useCallback, useRef, useMemo } from 'react';
import {
  KeyRound,
  Plus,
  AlertCircle,
  Package,
  Lock,
  LockOpen,
  RefreshCw,
  Search,
  X,
} from 'lucide-react';
import { IconButton } from '../components/ui/IconButton';
import { ConfirmDialog } from '../components/ui/ConfirmDialog';
import { ZoomControls } from '../components/ui/ZoomControls';
import { VaultLockPrompt } from '../components/vault/VaultLockPrompt';
import { ToastContainer, showToast } from '../components/ui/Toast';
import { useDetachedWindowSync } from '../hooks/useBroadcastChannel';
import { useLogFontSize } from '../hooks/useLogFontSize';
import { useVaultManager } from '../hooks/useVaultManager';
import { useRevealedValues } from '../hooks/useRevealedValues';
import { POLLING } from '../lib/constants';
import { SecretItem } from '../components/vault/SecretItem';
import { NewSetForm } from '../components/vault/NewSetForm';
import { VariableQuickAddForm } from '../components/vault/VariableQuickAddForm';
import { VaultEncryptForm } from '../components/vault/VaultEncryptForm';
import { EmptyVaultState } from '../components/vault/EmptyVaultState';
import { SetGroup } from '../components/vault/SetGroup';
import { DetachedVaultErrorBoundary } from '../components/vault/DetachedVaultErrorBoundary';

function DetachedVaultContent() {
  const revealedState = useRevealedValues();
  const vault = useVaultManager({ onPlaintextLoaded: revealedState.bulkSet });

  const {
    variables: secrets,
    sets,
    loading,
    error,
    locked,
    encrypted,
    refresh,
    unlock,
    lock,
    createVar,
    updateVar,
    deleteVar,
    getVar,
    createSet,
    deleteSet,
    assignToSet,
  } = vault;

  const [searchQuery, setSearchQuery] = useState('');
  const [showLockForm, setShowLockForm] = useState(false);

  // Edit state — the detached page sends only `{ value }` on save (no type
  // validation or is_secret preservation). This differs from VaultPanel
  // and is intentionally preserved for behavioral parity.
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');
  const [showEditValue, setShowEditValue] = useState(false);

  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  const [expandedSets, setExpandedSets] = useState<Record<string, boolean>>({});
  const [showNewSet, setShowNewSet] = useState(false);
  const [newSetName, setNewSetName] = useState('');

  const contentRef = useRef<HTMLElement>(null);
  const { fontSize, zoomIn, zoomOut, resetZoom, isMin, isMax, isDefault } =
    useLogFontSize(contentRef);

  useDetachedWindowSync('var');

  const allSecrets = useMemo(() => secrets ?? [], [secrets]);
  const filteredSecrets = useMemo(() => {
    if (!searchQuery) return allSecrets;
    const lower = searchQuery.toLowerCase();
    return allSecrets.filter(
      (s) =>
        s.key.toLowerCase().includes(lower) ||
        (s.set ?? '').toLowerCase().includes(lower),
    );
  }, [allSecrets, searchQuery]);

  useEffect(() => {
    refresh();
    const interval = window.setInterval(refresh, POLLING.STATUS);
    return () => clearInterval(interval);
  }, [refresh]);

  const handleUnlock = useCallback(
    async (passphrase: string): Promise<boolean> => {
      const ok = await unlock(passphrase);
      if (ok) showToast('success', 'Vault unlocked');
      return ok;
    },
    [unlock],
  );

  const handleEncrypt = useCallback(
    async (passphrase: string) => {
      await lock(passphrase);
      setShowLockForm(false);
      showToast('success', 'Vault encrypted');
    },
    [lock],
  );

  const handleReveal = useCallback(
    async (key: string) => {
      const target = allSecrets.find((v) => v.key === key);
      const isPlaintext = target ? !target.is_secret : false;
      try {
        await revealedState.reveal(
          key,
          async () => (await getVar(key)).value,
          !isPlaintext,
        );
      } catch {
        showToast('error', `Failed to reveal ${key}`);
      }
    },
    [allSecrets, revealedState, getVar],
  );

  const handleCreate = useCallback(
    async (input: Parameters<typeof createVar>[0]) => {
      await createVar(input);
      showToast('success', `Variable "${input.key}" created`);
    },
    [createVar],
  );

  const handleEdit = useCallback((key: string) => {
    setEditingKey(key);
    setEditValue('');
    setShowEditValue(false);
  }, []);

  const handleEditSave = useCallback(async () => {
    if (!editingKey || !editValue) return;
    try {
      await updateVar(editingKey, { value: editValue });
      setEditingKey(null);
      setEditValue('');
      showToast('success', `Variable "${editingKey}" updated`);
    } catch {
      showToast('error', 'Failed to update secret');
    }
  }, [editingKey, editValue, updateVar]);

  const handleEditCancel = useCallback(() => {
    setEditingKey(null);
    setEditValue('');
    setShowEditValue(false);
  }, []);

  const handleDeleteConfirm = useCallback(async () => {
    if (!confirmDelete) return;
    try {
      await deleteVar(confirmDelete);
      setConfirmDelete(null);
      showToast('success', `Secret "${confirmDelete}" deleted`);
    } catch {
      showToast('error', 'Failed to delete secret');
    }
  }, [confirmDelete, deleteVar]);

  const handleCreateSet = useCallback(async () => {
    const name = newSetName.trim();
    if (!name) return;
    try {
      await createSet(name);
      setNewSetName('');
      setShowNewSet(false);
      showToast('success', `Set "${name}" created`);
    } catch (err) {
      showToast(
        'error',
        err instanceof Error ? err.message : 'Failed to create set',
      );
    }
  }, [newSetName, createSet]);

  const handleDeleteSet = useCallback(
    async (name: string) => {
      try {
        await deleteSet(name);
        showToast('success', `Set "${name}" deleted`);
      } catch {
        showToast('error', 'Failed to delete set');
      }
    },
    [deleteSet],
  );

  const handleAssignSet = useCallback(
    async (key: string, set: string) => {
      try {
        await assignToSet(key, set);
      } catch {
        showToast('error', 'Failed to assign set');
      }
    },
    [assignToSet],
  );

  const toggleSetExpand = useCallback((name: string) => {
    setExpandedSets((prev) => ({ ...prev, [name]: !prev[name] }));
  }, []);

  const unassigned = filteredSecrets.filter((s) => !s.set);
  const setNames = (sets ?? []).map((s) => s.name);
  const isEmpty = allSecrets.length === 0 && (sets ?? []).length === 0;

  const rowHandlers = {
    revealed: revealedState.revealed,
    editingKey,
    editValue,
    showEditValue,
    setNames,
    onReveal: handleReveal,
    onEdit: handleEdit,
    onDeleteSecret: (key: string) => setConfirmDelete(key),
    onEditValueChange: setEditValue,
    onEditToggleShow: () => setShowEditValue(!showEditValue),
    onEditSave: handleEditSave,
    onEditCancel: handleEditCancel,
    onAssignSet: handleAssignSet,
  };

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
            <span className="text-sm font-semibold text-text-primary tracking-tight">
              Variables
            </span>
            <span className="text-[10px] text-text-muted uppercase tracking-wider">
              Secrets
            </span>
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
          <IconButton
            icon={RefreshCw}
            onClick={refresh}
            tooltip="Refresh"
            size="sm"
            variant="ghost"
          />
        </div>
      </header>

      {locked && <VaultLockPrompt onUnlock={handleUnlock} />}

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
              <button className="flex items-center gap-1 text-[10px] text-primary hover:text-primary/80 transition-colors">
                <Plus size={10} /> New
              </button>
            </div>
          </div>

          {/* Search */}
          <div
            className="px-2 py-1.5 border-b border-border/20 flex-shrink-0 z-10 relative"
            role="search"
          >
            <div className="max-w-2xl mx-auto relative">
              <Search
                size={12}
                className="absolute left-2 top-1/2 -translate-y-1/2 text-text-muted/50"
              />
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
            {showLockForm && (
              <VaultEncryptForm
                onLock={handleEncrypt}
                onCancel={() => setShowLockForm(false)}
                className="px-4 pt-3 pb-2 border-b border-border-subtle/50 max-w-2xl mx-auto"
              />
            )}

            {error && (
              <div className="max-w-2xl mx-auto mt-3 px-4 flex items-center gap-2 py-2 rounded-lg bg-status-error/10 border border-status-error/20 text-xs text-status-error">
                <AlertCircle size={12} className="flex-shrink-0" />
                <span>{error}</span>
              </div>
            )}

            {loading && !secrets && (
              <div className="p-4 space-y-3 max-w-2xl mx-auto">
                {[1, 2, 3].map((i) => (
                  <div
                    key={i}
                    className="h-10 rounded-lg bg-surface-elevated animate-pulse"
                  />
                ))}
              </div>
            )}

            <VariableQuickAddForm
              setNames={setNames}
              onSubmit={handleCreate}
              enableZoom
              className="px-4 pt-3 pb-2 border-b border-border-subtle/50 max-w-2xl mx-auto"
            />

            {!loading && isEmpty && (
              <EmptyVaultState cliVerb="vault" className="max-w-md mx-auto" />
            )}

            {!loading &&
              !isEmpty &&
              filteredSecrets.length === 0 &&
              searchQuery && (
                <div className="p-6 text-center max-w-md mx-auto">
                  <KeyRound
                    size={24}
                    className="text-text-muted/30 mx-auto mb-2"
                  />
                  <p className="text-text-muted text-xs">No matching secrets</p>
                </div>
              )}

            {!loading && unassigned.length > 0 && (
              <div className="p-3 space-y-2 max-w-2xl mx-auto">
                {unassigned.map((secret) => (
                  <SecretItem
                    key={secret.key}
                    secret={secret}
                    revealed={revealedState.revealed[secret.key]}
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
                    enableZoom
                  />
                ))}
              </div>
            )}

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
                    <Plus
                      size={12}
                      className="text-text-muted hover:text-primary"
                    />
                  </button>
                </div>

                <NewSetForm
                  show={showNewSet}
                  value={newSetName}
                  onChange={setNewSetName}
                  onSave={handleCreateSet}
                  onCancel={() => {
                    setShowNewSet(false);
                    setNewSetName('');
                  }}
                  className="mb-2 px-2"
                  enableZoom
                />

                <div className="space-y-2">
                  {(sets ?? []).map((set) => (
                    <SetGroup
                      key={set.name}
                      set={set}
                      variables={filteredSecrets.filter(
                        (s) => s.set === set.name,
                      )}
                      expanded={expandedSets[set.name] ?? false}
                      onToggleExpand={() => toggleSetExpand(set.name)}
                      onDeleteSet={() => handleDeleteSet(set.name)}
                      handlers={rowHandlers}
                      enableZoom
                      nameClassName="log-text"
                    />
                  ))}
                </div>
              </div>
            )}

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
                  onCancel={() => {
                    setShowNewSet(false);
                    setNewSetName('');
                  }}
                  className="mt-2"
                  enableZoom
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
            {(sets ?? []).length > 0 ? ` · ${(sets ?? []).length} sets` : ''}
            {locked ? 'Vault locked' : ''}
          </span>
          <span className="flex items-center gap-1">
            <span className="w-1.5 h-1.5 rounded-full bg-text-muted animate-pulse" />
            Detached Window
          </span>
        </div>
      </footer>

      <ConfirmDialog
        isOpen={confirmDelete !== null}
        onClose={() => setConfirmDelete(null)}
        onConfirm={handleDeleteConfirm}
        title="Delete secret"
        message={
          <>
            <p>
              Delete{' '}
              <span className="font-mono text-primary">{confirmDelete}</span>?
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

      <ToastContainer />
    </div>
  );
}

export function DetachedVaultPage() {
  return (
    <DetachedVaultErrorBoundary>
      <DetachedVaultContent />
    </DetachedVaultErrorBoundary>
  );
}
