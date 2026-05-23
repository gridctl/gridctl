import { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import { createPortal } from 'react-dom';
import { Link } from 'react-router-dom';
import {
  X,
  Plus,
  KeyRound,
  AlertCircle,
  Lock,
  LockOpen,
  Search,
} from 'lucide-react';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { ResizeHandle } from '../ui/ResizeHandle';
import { PopoutButton } from '../ui/PopoutButton';
import { useUIStore } from '../../stores/useUIStore';
import { showToast } from '../ui/Toast';
import { useVaultManager } from '../../hooks/useVaultManager';
import { useRevealedValues } from '../../hooks/useRevealedValues';
import { validateVariableInput } from './variableTypeHelpers';
import { VaultLockPrompt } from './VaultLockPrompt';
import { SecretItem } from './SecretItem';
import { VariableQuickAddForm } from './VariableQuickAddForm';
import { EmptyVaultState } from './EmptyVaultState';
import { SetGroup } from './SetGroup';
import type { VariableType } from '../../lib/api';

interface VaultPanelProps {
  onClose: () => void;
}

export function VaultPanel({ onClose }: VaultPanelProps) {
  const vaultDetached = useUIStore((s) => s.vaultDetached);
  const revealedState = useRevealedValues();
  const vault = useVaultManager({ onPlaintextLoaded: revealedState.bulkSet });

  const {
    variables: secrets,
    sets,
    recentlyEdited,
    loading,
    error,
    locked,
    encrypted,
    refresh,
    unlock,
    createVar,
    updateVar,
    deleteVar,
    getVar,
    assignToSet,
  } = vault;

  // Panel width (resizable)
  const [panelWidth, setPanelWidth] = useState(380);
  const handleResize = useCallback((delta: number) => {
    setPanelWidth((prev) => Math.min(600, Math.max(280, prev + delta)));
  }, []);

  const [searchQuery, setSearchQuery] = useState('');

  // Edit state — VaultPanel preserves the variable's current type/is_secret
  // on save (and validates the value against the type), so we track them
  // here in addition to the value being edited.
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');
  const [editType, setEditType] = useState<VariableType>('string');
  const [editIsSecret, setEditIsSecret] = useState(true);
  const [showEditValue, setShowEditValue] = useState(false);

  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  const [expandedSets, setExpandedSets] = useState<Record<string, boolean>>({});

  const keyInputRef = useRef<HTMLInputElement>(null);

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
  }, [refresh]);

  // Escape closes the panel unless the user is in an input.
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

  const handlePopout = useCallback(() => {
    window.open('/var', 'gridctl-var');
    onClose();
  }, [onClose]);

  const handleUnlock = useCallback(
    async (passphrase: string): Promise<boolean> => {
      const ok = await unlock(passphrase);
      if (ok) showToast('success', 'Vault unlocked');
      return ok;
    },
    [unlock],
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

  const handleEdit = useCallback(
    (key: string) => {
      const current = allSecrets.find((v) => v.key === key);
      setEditingKey(key);
      setEditValue('');
      setEditType(current?.type ?? 'string');
      setEditIsSecret(current?.is_secret ?? true);
      setShowEditValue(false);
    },
    [allSecrets],
  );

  const handleEditSave = useCallback(async () => {
    if (!editingKey || !editValue) return;
    const validation = validateVariableInput(editType, editValue);
    if (!validation.ok) {
      showToast('error', validation.error);
      return;
    }
    try {
      await updateVar(editingKey, {
        value: validation.normalized,
        type: editType,
        isSecret: editIsSecret,
      });
      setEditingKey(null);
      setEditValue('');
      showToast('success', `Variable "${editingKey}" updated`);
    } catch {
      showToast('error', 'Failed to update variable');
    }
  }, [editingKey, editValue, editType, editIsSecret, updateVar]);

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

  // Set names with a member edited this session — drives the per-set
  // "recently edited" dot. Derived from the full (unfiltered) list so the
  // search query doesn't suppress the hint.
  const recentlyEditedSets = useMemo(() => {
    const names = new Set<string>();
    for (const v of allSecrets) {
      if (v.set && v.key in recentlyEdited) names.add(v.set);
    }
    return names;
  }, [allSecrets, recentlyEdited]);

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

  return createPortal(
    <div
      className="fixed inset-y-0 right-0 z-40 max-w-full flex flex-row bg-surface/95 backdrop-blur-xl border-l border-border/50 shadow-2xl animate-slide-in-right"
      style={{ width: panelWidth }}
    >
      <ResizeHandle
        direction="vertical"
        onResize={handleResize}
        className="flex-shrink-0"
      />

      <div className="flex-1 min-w-0 flex flex-col overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between p-4 border-b border-border/50 bg-surface-elevated/30 flex-shrink-0">
          <div className="flex items-center gap-3 min-w-0">
            <div className="p-2 rounded-xl flex-shrink-0 border bg-primary/10 border-primary/20">
              <KeyRound size={16} className="text-primary" />
            </div>
            <div className="min-w-0">
              <h2 className="font-semibold text-text-primary truncate tracking-tight">
                Variables
              </h2>
              <div className="flex items-center gap-1.5">
                <p className="text-[10px] text-text-muted uppercase tracking-wider">
                  Secrets
                </p>
                {encrypted && !locked && (
                  <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-status-running/10 text-status-running flex items-center gap-0.5">
                    <LockOpen size={8} />
                    Encrypted
                  </span>
                )}
                {locked && (
                  <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-primary/10 text-primary flex items-center gap-0.5">
                    <Lock size={8} />
                    Locked
                  </span>
                )}
              </div>
            </div>
          </div>
          <div className="flex items-center gap-1">
            <PopoutButton onClick={handlePopout} disabled={vaultDetached} />
            <button
              onClick={onClose}
              className="p-1.5 rounded-lg hover:bg-surface-highlight transition-colors group"
            >
              <X
                size={16}
                className="text-text-muted group-hover:text-text-primary transition-colors"
              />
            </button>
          </div>
        </div>

        {locked && <VaultLockPrompt onUnlock={handleUnlock} />}

        {!locked && (
          <>
            {/* Item count + actions bar */}
            <div className="flex items-center justify-between px-4 py-2 border-b border-border/20 flex-shrink-0">
              <span className="text-[10px] text-text-muted">
                {searchQuery
                  ? `${filteredSecrets.length} of ${allSecrets.length} secrets`
                  : `${allSecrets.length} secrets`}
              </span>
              <button
                onClick={() => keyInputRef.current?.focus()}
                className="flex items-center gap-1 text-[10px] text-primary hover:text-primary/80 transition-colors"
              >
                <Plus size={10} /> New
              </button>
            </div>

            {/* Search */}
            <div
              className="px-2 py-1.5 border-b border-border/20 flex-shrink-0"
              role="search"
            >
              <div className="relative">
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

            <div className="flex-1 overflow-y-auto scrollbar-dark min-h-0">
              {error && (
                <div className="mx-4 mt-3 flex items-center gap-2 px-3 py-2 rounded-lg bg-status-error/10 border border-status-error/20 text-xs text-status-error">
                  <AlertCircle size={12} className="flex-shrink-0" />
                  <span>{error}</span>
                </div>
              )}

              {loading && !secrets && (
                <div className="p-4 space-y-3">
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
                keyInputRef={keyInputRef}
                className="px-4 pt-3 pb-2 border-b border-border-subtle/50"
              />

              {!loading && isEmpty && <EmptyVaultState cliVerb="var" />}

              {!loading &&
                !isEmpty &&
                filteredSecrets.length === 0 &&
                searchQuery && (
                  <div className="p-6 text-center">
                    <KeyRound
                      size={24}
                      className="text-text-muted/30 mx-auto mb-2"
                    />
                    <p className="text-text-muted text-xs">
                      No matching secrets
                    </p>
                  </div>
                )}

              {!loading && unassigned.length > 0 && (
                <div className="p-2 space-y-1">
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
                    />
                  ))}
                </div>
              )}

              {!loading && (sets ?? []).length > 0 && (
                <div className="px-2 py-2">
                  <div className="text-[10px] font-medium text-text-muted uppercase tracking-wider px-2 mb-2">
                    Variable Sets
                  </div>
                  <div className="space-y-1">
                    {(sets ?? []).map((set) => (
                      <SetGroup
                        key={set.name}
                        set={set}
                        variables={filteredSecrets.filter(
                          (s) => s.set === set.name,
                        )}
                        expanded={expandedSets[set.name] ?? false}
                        onToggleExpand={() => toggleSetExpand(set.name)}
                        recentlyEdited={recentlyEditedSets.has(set.name)}
                        handlers={rowHandlers}
                      />
                    ))}
                  </div>
                </div>
              )}
            </div>

            {/* Status footer — also surfaces a hint pointing power-user tasks
                (set management, bulk import, lock/unlock) to the Variables
                workspace, since the sidebar intentionally doesn't host them. */}
            <div className="px-4 py-2 border-t border-border/30 bg-surface/30 flex-shrink-0 space-y-1.5">
              <Link
                to="/vault"
                className="block text-[10px] text-text-muted/70 hover:text-primary transition-colors"
              >
                Manage sets and bulk-import in the Variables workspace →
              </Link>
              <div className="flex items-center justify-between text-[10px] text-text-muted">
                <span>{allSecrets.length} total</span>
                <span>
                  {(sets ?? []).length > 0 && (
                    <span className="text-secondary">
                      {(sets ?? []).length} sets
                    </span>
                  )}
                </span>
              </div>
            </div>
          </>
        )}

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
      </div>
    </div>,
    document.body,
  );
}
