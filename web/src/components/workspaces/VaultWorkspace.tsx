import {
  useCallback,
  useEffect,
  useMemo,
  useState,
} from 'react';
import { useSearchParams } from 'react-router-dom';
import {
  AlertCircle,
  FileUp,
  KeyRound,
  Lock,
  LockOpen,
  Plus,
  RefreshCw,
  Search,
  Trash2,
  X,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { showToast } from '../ui/Toast';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { SecretItem } from '../vault/SecretItem';
import { NewSetForm } from '../vault/NewSetForm';
import { VariableQuickAddForm } from '../vault/VariableQuickAddForm';
import { VaultEncryptForm } from '../vault/VaultEncryptForm';
import { VaultLockPrompt } from '../vault/VaultLockPrompt';
import { EnvImportModal } from '../vault/EnvImportModal';
import { useUIStore } from '../../stores/useUIStore';
import { useVaultManager } from '../../hooks/useVaultManager';
import { useRevealedValues } from '../../hooks/useRevealedValues';
import { validateVariableInput } from '../vault/variableTypeHelpers';
import type { Variable, VariableType } from '../../lib/api';

const ALL_SETS_KEY = '__all__';

// VaultWorkspace is the top-level Variables surface, sibling to Topology
// and Library. It owns the set-navigator, the variable table, the
// lock/encrypt controls, and the bulk `.env` import flow that the sidebar
// deliberately doesn't host.
export function VaultWorkspace() {
  const [searchParams, setSearchParams] = useSearchParams();
  const compact = useUIStore((s) => s.compactMode.vault);

  const revealedState = useRevealedValues();
  const vault = useVaultManager({ onPlaintextLoaded: revealedState.bulkSet });
  const {
    variables: vaultVariables,
    sets: vaultSets,
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
    importVars,
  } = vault;

  useEffect(() => {
    refresh();
  }, [refresh]);

  // ---- URL state ----------------------------------------------------------
  const activeSet = searchParams.get('set') ?? ALL_SETS_KEY;
  const searchQuery = searchParams.get('q') ?? '';

  const setActiveSet = useCallback(
    (name: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (name === ALL_SETS_KEY) next.delete('set');
          else next.set('set', name);
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const setSearchQuery = useCallback(
    (q: string) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (q.trim()) next.set('q', q);
          else next.delete('q');
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // ---- Local UI state -----------------------------------------------------
  // Edit state — mirrors VaultPanel: validate type on save, preserve is_secret.
  const [editingKey, setEditingKey] = useState<string | null>(null);
  const [editValue, setEditValue] = useState('');
  const [editType, setEditType] = useState<VariableType>('string');
  const [editIsSecret, setEditIsSecret] = useState(true);
  const [showEditValue, setShowEditValue] = useState(false);

  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [confirmDeleteSet, setConfirmDeleteSet] = useState<string | null>(null);

  const [encryptOpen, setEncryptOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [addOneOpen, setAddOneOpen] = useState(false);
  const [newSetOpen, setNewSetOpen] = useState(false);
  const [newSetName, setNewSetName] = useState('');

  // ---- Derived state ------------------------------------------------------
  const allVariables = useMemo(
    () => vaultVariables ?? [],
    [vaultVariables],
  );
  const allSets = useMemo(() => vaultSets ?? [], [vaultSets]);
  const setNames = useMemo(() => allSets.map((s) => s.name), [allSets]);
  const isEmpty = allVariables.length === 0 && allSets.length === 0;

  const filteredBySet = useMemo(() => {
    if (activeSet === ALL_SETS_KEY) return allVariables;
    return allVariables.filter((v) => v.set === activeSet);
  }, [allVariables, activeSet]);

  const filteredBySearch = useMemo(() => {
    if (!searchQuery) return filteredBySet;
    const lower = searchQuery.toLowerCase();
    return filteredBySet.filter(
      (v) =>
        v.key.toLowerCase().includes(lower) ||
        (v.set ?? '').toLowerCase().includes(lower),
    );
  }, [filteredBySet, searchQuery]);

  // ---- Handlers -----------------------------------------------------------
  const handleUnlock = useCallback(
    async (passphrase: string) => {
      const ok = await unlock(passphrase);
      if (ok) showToast('success', 'Vault unlocked');
      return ok;
    },
    [unlock],
  );

  const handleEncrypt = useCallback(
    async (passphrase: string) => {
      await lock(passphrase);
      setEncryptOpen(false);
      showToast('success', encrypted ? 'Vault locked' : 'Vault encrypted');
    },
    [lock, encrypted],
  );

  const handleReveal = useCallback(
    async (key: string) => {
      const target = allVariables.find((v) => v.key === key);
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
    [allVariables, revealedState, getVar],
  );

  const handleCreate = useCallback(
    async (input: Parameters<typeof createVar>[0]) => {
      await createVar(input);
      showToast('success', `Variable "${input.key}" created`);
      setAddOneOpen(false);
    },
    [createVar],
  );

  const handleEdit = useCallback(
    (key: string) => {
      const current = allVariables.find((v) => v.key === key);
      setEditingKey(key);
      setEditValue('');
      setEditType(current?.type ?? 'string');
      setEditIsSecret(current?.is_secret ?? true);
      setShowEditValue(false);
    },
    [allVariables],
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
      showToast('success', `Variable "${confirmDelete}" deleted`);
    } catch {
      showToast('error', 'Failed to delete variable');
    }
  }, [confirmDelete, deleteVar]);

  const handleCreateSet = useCallback(async () => {
    const name = newSetName.trim();
    if (!name) return;
    try {
      await createSet(name);
      setNewSetName('');
      setNewSetOpen(false);
      setActiveSet(name);
      showToast('success', `Set "${name}" created`);
    } catch (err) {
      showToast(
        'error',
        err instanceof Error ? err.message : 'Failed to create set',
      );
    }
  }, [newSetName, createSet, setActiveSet]);

  const handleDeleteSet = useCallback(async () => {
    if (!confirmDeleteSet) return;
    try {
      await deleteSet(confirmDeleteSet);
      if (activeSet === confirmDeleteSet) setActiveSet(ALL_SETS_KEY);
      showToast('success', `Set "${confirmDeleteSet}" deleted`);
    } catch {
      showToast('error', 'Failed to delete set');
    } finally {
      setConfirmDeleteSet(null);
    }
  }, [confirmDeleteSet, deleteSet, activeSet, setActiveSet]);

  const handleAssignSet = useCallback(
    async (key: string, name: string) => {
      try {
        await assignToSet(key, name);
      } catch {
        showToast('error', 'Failed to assign set');
      }
    },
    [assignToSet],
  );

  const handleImport = useCallback(
    async (vars: Parameters<typeof importVars>[0]) => {
      const result = await importVars(vars);
      showToast(
        'success',
        `Imported ${result.imported} variable${result.imported === 1 ? '' : 's'}`,
      );
      return result;
    },
    [importVars],
  );

  // ---- Rendering ----------------------------------------------------------
  const leftRail = (
    <VaultLeftRail
      compact={compact}
      sets={allSets}
      activeSet={activeSet}
      onSelectSet={setActiveSet}
      totalCount={allVariables.length}
      unassignedCount={allVariables.filter((v) => !v.set).length}
      newSetOpen={newSetOpen}
      onNewSetOpen={setNewSetOpen}
      newSetName={newSetName}
      onNewSetNameChange={setNewSetName}
      onNewSetSave={handleCreateSet}
      onDeleteSet={(name) => setConfirmDeleteSet(name)}
      locked={locked}
    />
  );

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell
        workspace="vault"
        defaultLeftPct={20}
        defaultRightPct={0}
        left={leftRail}
        minLeftPx={200}
      >
        <main className="flex flex-col h-full overflow-hidden">
          <VaultHeader
            compact={compact}
            totalVariables={allVariables.length}
            totalSets={allSets.length}
            locked={locked}
            encrypted={encrypted}
            onRefresh={refresh}
            onOpenEncrypt={() => setEncryptOpen(true)}
            onOpenImport={() => setImportOpen(true)}
          />

          {/* Inline encrypt drawer slides in below the header when invoked. */}
          {encryptOpen && !locked && (
            <div className="flex-shrink-0 px-6 py-3 border-b border-border-subtle bg-surface-elevated/40">
              <div className="max-w-md">
                <p className="text-[10px] uppercase tracking-[0.18em] text-text-muted mb-2">
                  {encrypted ? 're-enter passphrase to lock' : 'set a passphrase to encrypt'}
                </p>
                <VaultEncryptForm
                  onLock={handleEncrypt}
                  onCancel={() => setEncryptOpen(false)}
                />
              </div>
            </div>
          )}

          {locked ? (
            <div className="flex-1 min-h-0 flex items-center justify-center">
              <VaultLockPrompt onUnlock={handleUnlock} />
            </div>
          ) : (
            <>
              {/* Search + add-one strip */}
              <div className="flex-shrink-0 px-6 py-3 border-b border-border-subtle bg-surface/30 flex flex-col gap-2">
                <div className="flex items-center gap-2">
                  <div className="relative flex-1">
                    <Search
                      size={13}
                      className="absolute left-3 top-1/2 -translate-y-1/2 text-text-muted/50 pointer-events-none"
                    />
                    <input
                      value={searchQuery}
                      onChange={(e) => setSearchQuery(e.target.value)}
                      placeholder={
                        activeSet === ALL_SETS_KEY
                          ? 'Search all variables…'
                          : `Search ${activeSet}…`
                      }
                      aria-label="Filter variables"
                      className="w-full bg-background/60 border border-border/40 rounded-lg pl-9 pr-8 py-2 text-sm text-text-primary placeholder:text-text-muted/40 focus:outline-none focus:border-primary/50 transition-colors"
                    />
                    {searchQuery && (
                      <button
                        onClick={() => setSearchQuery('')}
                        aria-label="Clear search"
                        className="absolute right-2.5 top-1/2 -translate-y-1/2 p-0.5 rounded hover:bg-surface-highlight transition-colors"
                      >
                        <X size={13} className="text-text-muted" />
                      </button>
                    )}
                  </div>
                  <button
                    onClick={() => setAddOneOpen((v) => !v)}
                    className={cn(
                      'flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-lg border transition-colors',
                      addOneOpen
                        ? 'text-primary bg-primary/15 border-primary/30'
                        : 'text-text-secondary hover:text-text-primary bg-surface-elevated border-border/40 hover:border-border',
                    )}
                  >
                    <Plus size={12} /> Add one
                  </button>
                </div>
                {addOneOpen && (
                  <div className="pt-2 border-t border-border-subtle/40">
                    <VariableQuickAddForm
                      setNames={setNames}
                      onSubmit={handleCreate}
                      className="max-w-2xl"
                    />
                  </div>
                )}
              </div>

              {/* Body */}
              <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark">
                {error && (
                  <div className="mx-6 mt-4 flex items-center gap-2 px-3 py-2 rounded-lg bg-status-error/10 border border-status-error/20 text-xs text-status-error">
                    <AlertCircle size={12} className="flex-shrink-0" />
                    <span>{error}</span>
                  </div>
                )}

                {loading && !vaultVariables && (
                  <div className="p-6 space-y-3 max-w-3xl">
                    {[1, 2, 3, 4].map((i) => (
                      <div
                        key={i}
                        className="h-12 rounded-lg bg-surface-elevated animate-pulse"
                      />
                    ))}
                  </div>
                )}

                {!loading && isEmpty && (
                  <VaultEmptyState
                    onImport={() => setImportOpen(true)}
                    onAddOne={() => setAddOneOpen(true)}
                  />
                )}

                {!loading && !isEmpty && filteredBySearch.length === 0 && (
                  <NoMatchesState
                    activeSet={activeSet}
                    searchQuery={searchQuery}
                    onClear={() => setSearchQuery('')}
                  />
                )}

                {!loading && filteredBySearch.length > 0 && (
                  <VariableList
                    variables={filteredBySearch}
                    revealed={revealedState.revealed}
                    editingKey={editingKey}
                    editValue={editValue}
                    showEditValue={showEditValue}
                    setNames={setNames}
                    onReveal={handleReveal}
                    onEdit={handleEdit}
                    onDelete={(key) => setConfirmDelete(key)}
                    onEditValueChange={setEditValue}
                    onEditToggleShow={() => setShowEditValue(!showEditValue)}
                    onEditSave={handleEditSave}
                    onEditCancel={handleEditCancel}
                    onAssignSet={handleAssignSet}
                  />
                )}
              </div>
            </>
          )}
        </main>
      </WorkspaceShell>

      <ConfirmDialog
        isOpen={confirmDelete !== null}
        onClose={() => setConfirmDelete(null)}
        onConfirm={handleDeleteConfirm}
        title="Delete variable"
        message={
          <>
            <p>
              Delete <span className="font-mono text-primary">{confirmDelete}</span>?
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

      <ConfirmDialog
        isOpen={confirmDeleteSet !== null}
        onClose={() => setConfirmDeleteSet(null)}
        onConfirm={handleDeleteSet}
        title="Delete variable set"
        message={
          <>
            <p>
              Delete the set{' '}
              <span className="font-mono text-primary">{confirmDeleteSet}</span>?
            </p>
            <p>
              Variables in this set keep their values but become unassigned.
            </p>
          </>
        }
        confirmLabel={
          <span>
            Delete <span className="font-mono">"{confirmDeleteSet}"</span>
          </span>
        }
        variant="danger"
      />

      {importOpen && (
        <EnvImportModal
          onClose={() => setImportOpen(false)}
          onImport={handleImport}
          existingVariables={allVariables}
          sets={allSets}
          defaultSet={activeSet === ALL_SETS_KEY ? '' : activeSet}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Header
// ---------------------------------------------------------------------------

interface VaultHeaderProps {
  compact: boolean;
  totalVariables: number;
  totalSets: number;
  locked: boolean;
  encrypted: boolean;
  onRefresh: () => void;
  onOpenEncrypt: () => void;
  onOpenImport: () => void;
}

function VaultHeader({
  compact,
  totalVariables,
  totalSets,
  locked,
  encrypted,
  onRefresh,
  onOpenEncrypt,
  onOpenImport,
}: VaultHeaderProps) {
  return (
    <header
      className={cn(
        'flex-shrink-0 bg-surface/30 backdrop-blur-sm border-b border-border-subtle flex items-center justify-between px-6',
        compact ? 'py-2' : 'py-3',
      )}
    >
      <div className="flex items-center gap-3">
        <div className="font-sans text-text-muted/60 text-[10px] uppercase tracking-[0.4em]">
          variables
        </div>
        <div className="font-mono text-[10px] text-text-muted">
          {totalVariables} {totalVariables === 1 ? 'variable' : 'variables'}
        </div>
        {totalSets > 0 && (
          <div className="font-mono text-[10px] text-secondary">
            · {totalSets} {totalSets === 1 ? 'set' : 'sets'}
          </div>
        )}
        {encrypted && !locked && (
          <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-status-running/10 text-status-running flex items-center gap-1">
            <LockOpen size={9} />
            encrypted
          </span>
        )}
        {locked && (
          <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-primary/10 text-primary flex items-center gap-1">
            <Lock size={9} />
            locked
          </span>
        )}
      </div>

      <div className="flex items-center gap-2">
        {!locked && (
          <>
            <button
              onClick={onOpenImport}
              className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-primary hover:text-primary/80 bg-primary/10 hover:bg-primary/15 border border-primary/20 rounded-lg transition-colors"
            >
              <FileUp size={12} /> Import .env
            </button>
            {encrypted ? (
              <IconButton
                icon={Lock}
                onClick={onOpenEncrypt}
                tooltip="Lock vault"
                size="sm"
                variant="ghost"
              />
            ) : (
              totalVariables > 0 && (
                <button
                  onClick={onOpenEncrypt}
                  className="flex items-center gap-1.5 px-2.5 py-1.5 text-[11px] font-medium text-text-secondary hover:text-text-primary border border-border/40 hover:border-border rounded-lg transition-colors"
                >
                  <Lock size={11} /> Encrypt
                </button>
              )
            )}
            <IconButton
              icon={RefreshCw}
              onClick={onRefresh}
              tooltip="Refresh"
              size="sm"
              variant="ghost"
            />
          </>
        )}
      </div>
    </header>
  );
}

// ---------------------------------------------------------------------------
// Left rail
// ---------------------------------------------------------------------------

interface VaultLeftRailProps {
  compact: boolean;
  sets: { name: string; count: number }[];
  activeSet: string;
  onSelectSet: (name: string) => void;
  totalCount: number;
  unassignedCount: number;
  newSetOpen: boolean;
  onNewSetOpen: (open: boolean) => void;
  newSetName: string;
  onNewSetNameChange: (value: string) => void;
  onNewSetSave: () => void;
  onDeleteSet: (name: string) => void;
  locked: boolean;
}

function VaultLeftRail({
  compact,
  sets,
  activeSet,
  onSelectSet,
  totalCount,
  unassignedCount,
  newSetOpen,
  onNewSetOpen,
  newSetName,
  onNewSetNameChange,
  onNewSetSave,
  onDeleteSet,
  locked,
}: VaultLeftRailProps) {
  return (
    <aside className="h-full flex flex-col bg-surface/40 backdrop-blur-sm border-r border-border-subtle">
      <div
        className={cn(
          'flex-shrink-0 px-3 border-b border-border-subtle/60',
          compact ? 'py-2' : 'py-3',
        )}
      >
        <div className="text-[10px] font-medium text-text-muted/60 uppercase tracking-[0.3em]">
          sets
        </div>
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto scrollbar-dark px-2 py-2 space-y-0.5">
        <SetPill
          label="All variables"
          count={totalCount}
          active={activeSet === ALL_SETS_KEY}
          onClick={() => onSelectSet(ALL_SETS_KEY)}
        />
        {unassignedCount > 0 && (
          <p className="px-3 pt-2 pb-1 text-[9px] uppercase tracking-[0.24em] text-text-muted/50">
            grouped
          </p>
        )}
        {sets.map((set) => (
          <SetPill
            key={set.name}
            label={set.name}
            count={set.count}
            mono
            active={activeSet === set.name}
            onClick={() => onSelectSet(set.name)}
            onDelete={locked ? undefined : () => onDeleteSet(set.name)}
          />
        ))}
        {sets.length === 0 && (
          <p className="px-3 py-2 text-[10px] text-text-muted/60 leading-relaxed">
            No sets yet. Groups appear here as you assign variables.
          </p>
        )}
      </div>

      <div className="flex-shrink-0 px-2 py-2 border-t border-border-subtle/60">
        {newSetOpen ? (
          <NewSetForm
            show
            value={newSetName}
            onChange={onNewSetNameChange}
            onSave={onNewSetSave}
            onCancel={() => {
              onNewSetOpen(false);
              onNewSetNameChange('');
            }}
            className="px-1"
          />
        ) : (
          <button
            onClick={() => onNewSetOpen(true)}
            disabled={locked}
            className="w-full flex items-center justify-center gap-1.5 px-2 py-1.5 text-[10px] uppercase tracking-[0.18em] text-text-muted hover:text-text-primary hover:bg-surface-highlight rounded transition-colors disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-text-muted"
          >
            <Plus size={11} /> new set
          </button>
        )}
      </div>
    </aside>
  );
}

interface SetPillProps {
  label: string;
  count: number;
  active: boolean;
  onClick: () => void;
  mono?: boolean;
  onDelete?: () => void;
}

function SetPill({
  label,
  count,
  active,
  onClick,
  mono,
  onDelete,
}: SetPillProps) {
  return (
    <div
      className={cn(
        'group flex items-center gap-1.5 rounded-md transition-colors',
        active
          ? 'bg-primary/10 text-primary'
          : 'text-text-secondary hover:bg-surface-highlight/50 hover:text-text-primary',
      )}
    >
      <button
        onClick={onClick}
        className="flex-1 min-w-0 flex items-center justify-between gap-2 px-3 py-1.5 text-left"
      >
        <span
          className={cn(
            'text-xs truncate',
            mono ? 'font-mono' : 'font-medium',
            active && 'text-primary',
          )}
        >
          {label}
        </span>
        <span
          className={cn(
            'flex-shrink-0 text-[10px] font-mono px-1.5 py-0.5 rounded',
            active
              ? 'bg-primary/15 text-primary'
              : 'bg-surface-elevated text-text-muted',
          )}
        >
          {count}
        </span>
      </button>
      {onDelete && (
        <button
          onClick={onDelete}
          className="flex-shrink-0 p-1 mr-1.5 rounded hover:bg-status-error/10 transition-colors opacity-0 group-hover:opacity-100 focus:opacity-100"
          title={`Delete ${label}`}
          aria-label={`Delete set ${label}`}
        >
          <Trash2 size={10} className="text-text-muted hover:text-status-error" />
        </button>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Body states
// ---------------------------------------------------------------------------

interface VariableListProps {
  variables: Variable[];
  revealed: Record<string, string>;
  editingKey: string | null;
  editValue: string;
  showEditValue: boolean;
  setNames: string[];
  onReveal: (key: string) => void;
  onEdit: (key: string) => void;
  onDelete: (key: string) => void;
  onEditValueChange: (val: string) => void;
  onEditToggleShow: () => void;
  onEditSave: () => void;
  onEditCancel: () => void;
  onAssignSet: (key: string, set: string) => void;
}

function VariableList({
  variables,
  revealed,
  editingKey,
  editValue,
  showEditValue,
  setNames,
  onReveal,
  onEdit,
  onDelete,
  onEditValueChange,
  onEditToggleShow,
  onEditSave,
  onEditCancel,
  onAssignSet,
}: VariableListProps) {
  return (
    <div className="px-6 py-4 space-y-2 max-w-3xl">
      {variables.map((variable) => (
        <SecretItem
          key={variable.key}
          secret={variable}
          revealed={revealed[variable.key]}
          isEditing={editingKey === variable.key}
          editValue={editValue}
          showEditValue={showEditValue}
          onReveal={() => onReveal(variable.key)}
          onEdit={() => onEdit(variable.key)}
          onDelete={() => onDelete(variable.key)}
          onEditValueChange={onEditValueChange}
          onEditToggleShow={onEditToggleShow}
          onEditSave={onEditSave}
          onEditCancel={onEditCancel}
          sets={setNames}
          onAssignSet={(set) => onAssignSet(variable.key, set)}
        />
      ))}
    </div>
  );
}

interface VaultEmptyStateProps {
  onImport: () => void;
  onAddOne: () => void;
}

function VaultEmptyState({ onImport, onAddOne }: VaultEmptyStateProps) {
  return (
    <div className="h-full flex items-center justify-center px-6 py-12">
      <div className="max-w-md w-full text-center space-y-5 animate-fade-in-scale">
        <div className="relative mx-auto w-16 h-16">
          <div className="absolute inset-0 rounded-2xl bg-primary/10 border border-primary/20 flex items-center justify-center">
            <KeyRound size={26} className="text-primary/70" />
          </div>
          <div className="absolute -inset-2 rounded-3xl bg-primary/5 blur-2xl -z-10" />
        </div>
        <div className="space-y-1.5">
          <h2 className="text-base font-semibold text-text-primary">
            Your variables home
          </h2>
          <p className="text-xs text-text-muted leading-relaxed">
            Bring a <code className="font-mono text-text-secondary">.env</code> file
            over, or add a key by hand. Secrets stay encrypted at rest when you
            set a passphrase.
          </p>
        </div>
        <div className="flex items-center justify-center gap-2 pt-1">
          <button
            onClick={onImport}
            className="flex items-center gap-1.5 px-4 py-2 text-xs font-semibold rounded-lg bg-gradient-to-r from-primary to-primary-dark text-background shadow-[0_1px_12px_rgba(245,158,11,0.3)] hover:shadow-[0_2px_18px_rgba(245,158,11,0.4)] hover:-translate-y-0.5 active:translate-y-0 transition-all duration-200"
          >
            <FileUp size={13} /> Import from .env
          </button>
          <button
            onClick={onAddOne}
            className="flex items-center gap-1.5 px-3 py-2 text-xs font-medium text-text-secondary hover:text-text-primary border border-border/40 hover:border-border rounded-lg transition-colors"
          >
            <Plus size={12} /> Add one manually
          </button>
        </div>
        <p className="text-[10px] text-text-muted/70 pt-2">
          Or use the CLI:
          <code className="ml-1 font-mono text-text-secondary">
            gridctl var import .env
          </code>
        </p>
      </div>
    </div>
  );
}

interface NoMatchesProps {
  activeSet: string;
  searchQuery: string;
  onClear: () => void;
}

function NoMatchesState({ activeSet, searchQuery, onClear }: NoMatchesProps) {
  const scopeLabel =
    activeSet === ALL_SETS_KEY ? 'this view' : `the ${activeSet} set`;
  return (
    <div className="h-full flex items-center justify-center px-6 py-10">
      <div className="text-center space-y-2 max-w-sm">
        <div className="mx-auto w-12 h-12 rounded-xl bg-surface-elevated/60 border border-border/30 flex items-center justify-center">
          <Search size={20} className="text-text-muted/50" />
        </div>
        <p className="text-xs text-text-secondary">
          {searchQuery
            ? `No variables match "${searchQuery}" in ${scopeLabel}.`
            : `No variables in ${scopeLabel} yet.`}
        </p>
        {searchQuery && (
          <button
            onClick={onClear}
            className="text-[11px] text-primary hover:text-primary/80 underline underline-offset-2 transition-colors"
          >
            Clear search
          </button>
        )}
      </div>
    </div>
  );
}

export default VaultWorkspace;
