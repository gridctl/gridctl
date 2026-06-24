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
  Filter,
  KeyRound,
  Lock,
  LockOpen,
  Plus,
  RefreshCw,
  Search,
  Trash2,
  Upload,
  X,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import { ConfirmDialog } from '../ui/ConfirmDialog';
import { showToast } from '../ui/Toast';
import { WorkspaceShell } from '../layout/WorkspaceShell';
import { NewSetForm } from '../vault/NewSetForm';
import { VariableInspector } from '../vault/VariableInspector';
import { VariableQuickAddForm } from '../vault/VariableQuickAddForm';
import { VariableRow, VARIABLE_ROW_GRID } from '../vault/VariableRow';
import { VaultEncryptForm } from '../vault/VaultEncryptForm';
import { VaultLockPrompt } from '../vault/VaultLockPrompt';
import { EnvImportModal } from '../vault/EnvImportModal';
import { useUIStore } from '../../stores/useUIStore';
import { useStackStore } from '../../stores/useStackStore';
import { useVaultManager } from '../../hooks/useVaultManager';
import { useListNav } from '../../hooks/useListNav';
import { useRevealedValues } from '../../hooks/useRevealedValues';
import { usePageFileDrop } from '../../hooks/usePageFileDrop';
import { isImportableFile } from '../../lib/parseFile';
import type { Consumer, Variable } from '../../lib/api';

const ALL_SETS_KEY = '__all__';

// VaultWorkspace is the top-level Variables surface, sibling to Topology
// and Library. It owns the set-navigator, the variable table, the
// lock/encrypt controls, and the bulk `.env` import flow that the sidebar
// deliberately doesn't host.
export function VaultWorkspace() {
  const [searchParams, setSearchParams] = useSearchParams();
  const compact = useUIStore((s) => s.compactMode.vault);

  const selectNode = useStackStore((s) => s.selectNode);
  const revealedState = useRevealedValues();
  const vault = useVaultManager({ onPlaintextLoaded: revealedState.bulkSet });
  const {
    variables: vaultVariables,
    sets: vaultSets,
    usage,
    recentlyEdited,
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
  // ?filter=server:<name> deep-links from Topology's server inspector. The
  // server name is matched against variable keys as a substring — see
  // filteredByServer below for the documented limitation.
  const filterParam = searchParams.get('filter') ?? '';
  const serverFilter = filterParam.startsWith('server:')
    ? filterParam.slice('server:'.length)
    : '';

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

  const clearServerFilter = useCallback(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        next.delete('filter');
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  // Inspector selection — the variable shown in the right-rail inspector.
  // URL-synced as ?selected=<key> (replace history) so reload and deep-links
  // restore it, mirroring the Library workspace.
  const selectedKey = searchParams.get('selected');
  const setSelectedKey = useCallback(
    (key: string | null) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (key) next.set('selected', key);
          else next.delete('selected');
          return next;
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  // ---- Local UI state -----------------------------------------------------
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const [confirmDeleteSet, setConfirmDeleteSet] = useState<string | null>(null);

  const [encryptOpen, setEncryptOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  // Content seeded into the import modal when it's opened by a file drop.
  const [droppedText, setDroppedText] = useState('');
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
  // Set names whose members include a variable edited this session — drives the
  // left-rail "recently edited" dot.
  const recentlyEditedSets = useMemo(() => {
    const names = new Set<string>();
    for (const v of allVariables) {
      if (v.set && v.key in recentlyEdited) names.add(v.set);
    }
    return names;
  }, [allVariables, recentlyEdited]);
  const isEmpty = allVariables.length === 0 && allSets.length === 0;

  // Exact consumption filter: keep variables actually referenced by the named
  // server/resource, using the backend usage index (GET /api/var/usage). This
  // replaces the former approximate key-substring heuristic.
  const filteredByServer = useMemo(() => {
    if (!serverFilter) return allVariables;
    return allVariables.filter((v) =>
      (usage[v.key] ?? []).some(
        (c) =>
          (c.kind === 'mcp-server' || c.kind === 'resource') &&
          c.name === serverFilter,
      ),
    );
  }, [allVariables, serverFilter, usage]);

  const filteredBySet = useMemo(() => {
    if (activeSet === ALL_SETS_KEY) return filteredByServer;
    return filteredByServer.filter((v) => v.set === activeSet);
  }, [filteredByServer, activeSet]);

  const filteredBySearch = useMemo(() => {
    if (!searchQuery) return filteredBySet;
    const lower = searchQuery.toLowerCase();
    return filteredBySet.filter(
      (v) =>
        v.key.toLowerCase().includes(lower) ||
        (v.set ?? '').toLowerCase().includes(lower),
    );
  }, [filteredBySet, searchQuery]);

  // The inspector shows the selection only while it survives the active
  // filters; a filtered-out key keeps its URL param (the pane falls back to
  // the overview) so clearing the filter restores the selection.
  const selectedVariable = useMemo(
    () => filteredBySearch.find((v) => v.key === selectedKey) ?? null,
    [filteredBySearch, selectedKey],
  );

  // A key that no longer exists at all (deleted, vault locked) clears the
  // param. Guarded on a loaded list so initial load never wipes a deep link.
  useEffect(() => {
    if (!selectedKey) return;
    if (locked) {
      setSelectedKey(null);
      return;
    }
    if (vaultVariables && !vaultVariables.some((v) => v.key === selectedKey)) {
      setSelectedKey(null);
    }
  }, [selectedKey, locked, vaultVariables, setSelectedKey]);

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

  const handleCreate = useCallback(
    async (input: Parameters<typeof createVar>[0]) => {
      await createVar(input);
      showToast('success', `Variable "${input.key}" created`);
      setAddOneOpen(false);
    },
    [createVar],
  );

  const handleDeleteConfirm = useCallback(async () => {
    if (!confirmDelete) return;
    try {
      await deleteVar(confirmDelete);
      setConfirmDelete(null);
      // The inspector falls back to its overview once its subject is gone.
      if (confirmDelete === selectedKey) setSelectedKey(null);
      showToast('success', `Variable "${confirmDelete}" deleted`);
    } catch {
      showToast('error', 'Failed to delete variable');
    }
  }, [confirmDelete, deleteVar, selectedKey, setSelectedKey]);

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

  // Page-level file drop: dragging a .env/.json file anywhere over the
  // workspace opens the import modal pre-seeded with the file's contents.
  // Validation happens here, before the modal opens, so failures surface as a
  // toast rather than an empty modal.
  const handleDroppedFiles = useCallback(async (files: FileList) => {
    if (files.length > 1) {
      showToast('warning', 'Dropped multiple files — importing the first only');
    }
    const file = files[0];
    if (!isImportableFile(file)) {
      showToast('error', 'Only .env and .json files can be imported');
      return;
    }
    let content: string;
    try {
      content = await file.text();
    } catch {
      showToast('error', 'Could not read that file');
      return;
    }
    if (!content.trim()) {
      showToast('warning', 'That file looks empty');
      return;
    }
    setDroppedText(content);
    setImportOpen(true);
  }, []);

  // Overlay is suppressed while the modal is open (its textarea has its own
  // dropzone for mid-edit drops) and while the vault is locked.
  const { isDragging } = usePageFileDrop({
    enabled: !importOpen && !locked,
    onFiles: handleDroppedFiles,
  });

  // ---- Keyboard navigation ------------------------------------------------
  // Bumping the counter asks the inspector to enter edit mode for the current
  // selection ('e' / Enter from the list). The inspector ignores it when
  // nothing is selected.
  const [editSignal, setEditSignal] = useState(0);
  const requestInspectorEdit = useCallback(() => {
    setEditSignal((s) => s + 1);
  }, []);

  const selectedIndex = useMemo(
    () => filteredBySearch.findIndex((v) => v.key === selectedKey),
    [filteredBySearch, selectedKey],
  );

  useListNav({
    itemCount: filteredBySearch.length,
    selectedIndex,
    setSelectedIndex: (i) => {
      const next = filteredBySearch[i];
      if (next) setSelectedKey(next.key);
    },
    onEnter: requestInspectorEdit,
    onEdit: requestInspectorEdit,
    enabled: !locked && !importOpen,
  });

  const closeImport = useCallback(() => {
    setImportOpen(false);
    setDroppedText('');
  }, []);

  // Selecting a consumer highlights its topology node (ids are mcp-<name> /
  // resource-<name>). We stay on the Variables route — a toast points the user
  // to Topology rather than yanking them out of their current view.
  const handleConsumerClick = useCallback(
    (consumer: Consumer) => {
      const nodeId =
        consumer.kind === 'mcp-server'
          ? `mcp-${consumer.name}`
          : consumer.kind === 'resource'
            ? `resource-${consumer.name}`
            : null;
      if (!nodeId) return;
      selectNode(nodeId);
      showToast('success', `Selected ${consumer.name} — open Topology to inspect`);
    },
    [selectNode],
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
      recentlyEditedSets={recentlyEditedSets}
      newSetOpen={newSetOpen}
      onNewSetOpen={setNewSetOpen}
      newSetName={newSetName}
      onNewSetNameChange={setNewSetName}
      onNewSetSave={handleCreateSet}
      onDeleteSet={(name) => setConfirmDeleteSet(name)}
      locked={locked}
    />
  );

  const inspector = (
    <VariableInspector
      variable={selectedVariable}
      consumers={selectedVariable ? (usage[selectedVariable.key] ?? []) : []}
      allVariables={vaultVariables}
      usage={usage}
      setNames={setNames}
      locked={locked}
      compact={compact}
      editSignal={editSignal}
      getValue={getVar}
      onUpdate={updateVar}
      onAssignSet={handleAssignSet}
      onDelete={(key) => setConfirmDelete(key)}
      onConsumerClick={handleConsumerClick}
      onClose={() => setSelectedKey(null)}
    />
  );

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell
        workspace="vault"
        defaultLeftPct={20}
        defaultRightPct={30}
        left={leftRail}
        right={inspector}
        minLeftPx={200}
        minRightPx={300}
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
              {serverFilter && (
                <ServerFilterBanner
                  serverName={serverFilter}
                  matchCount={filteredByServer.length}
                  onClear={clearServerFilter}
                />
              )}

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
                      onCancel={() => setAddOneOpen(false)}
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
                  <div className="p-6 space-y-3">
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
                    usage={usage}
                    selectedKey={selectedKey}
                    onSelect={setSelectedKey}
                    compact={compact}
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
            {confirmDelete && (usage[confirmDelete]?.length ?? 0) > 0 && (
              <p className="mt-2 px-2.5 py-2 rounded-md bg-status-error/10 border border-status-error/20 text-[11px] text-status-error">
                Used by {usage[confirmDelete].length}{' '}
                {usage[confirmDelete].length === 1 ? 'consumer' : 'consumers'} in
                the active stack. Deleting it may break{' '}
                {usage[confirmDelete].length === 1 ? 'it' : 'them'}.
              </p>
            )}
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
          onClose={closeImport}
          onImport={handleImport}
          existingVariables={allVariables}
          sets={allSets}
          defaultSet={activeSet === ALL_SETS_KEY ? '' : activeSet}
          initialText={droppedText}
        />
      )}

      {/* Drag-activated dropzone overlay. Decorative — the window-level
          listeners (usePageFileDrop) own the drop; the existing modal file
          picker remains the keyboard/screen-reader path. */}
      {isDragging && (
        <div
          aria-hidden="true"
          className={cn(
            'absolute inset-0 z-[55] pointer-events-none p-6',
            'flex items-center justify-center',
            'bg-background/80 backdrop-blur-sm animate-fade-in-scale',
          )}
        >
          <div className="flex flex-col items-center justify-center gap-3 w-full max-w-2xl px-10 py-16 rounded-2xl border-2 border-dashed border-primary/50 bg-primary/5 text-primary">
            <Upload size={28} />
            <p className="text-sm font-medium">
              Drop a .env or .json file to import
            </p>
            <p className="text-[11px] text-text-muted">
              You'll review the parsed variables before anything is saved
            </p>
          </div>
        </div>
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
  recentlyEditedSets: Set<string>;
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
  recentlyEditedSets,
  newSetOpen,
  onNewSetOpen,
  newSetName,
  onNewSetNameChange,
  onNewSetSave,
  onDeleteSet,
  locked,
}: VaultLeftRailProps) {
  return (
    <aside className="h-full flex flex-col bg-surface border-r border-border-subtle">
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
            recentlyEdited={recentlyEditedSets.has(set.name)}
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
  // When true, a dot marks the set as containing a variable edited this
  // session. Absence is the signal — nothing renders when false/omitted.
  recentlyEdited?: boolean;
}

function SetPill({
  label,
  count,
  active,
  onClick,
  mono,
  onDelete,
  recentlyEdited,
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
        <span className="flex-shrink-0 flex items-center gap-1.5">
          {recentlyEdited && (
            <span
              className="h-1.5 w-1.5 rounded-full bg-secondary/70 flex-shrink-0"
              title="Recently edited"
              aria-label="Recently edited"
            />
          )}
          <span
            className={cn(
              'text-[10px] font-mono px-1.5 py-0.5 rounded',
              active
                ? 'bg-primary/15 text-primary'
                : 'bg-surface-elevated text-text-muted',
            )}
          >
            {count}
          </span>
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

// Fixed-length mask for secret previews — independent of the real value's
// length so the row leaks nothing.
const SECRET_PREVIEW = '••••••••••';

interface VariableListProps {
  variables: Variable[];
  revealed: Record<string, string>;
  usage: Record<string, Consumer[]>;
  selectedKey: string | null;
  onSelect: (key: string) => void;
  compact: boolean;
}

// Table-like list: aligned columns under a sticky header row, with all depth
// (value, consumers, actions) living in the right-rail inspector. Rows select;
// they no longer expand or edit inline.
function VariableList({
  variables,
  revealed,
  usage,
  selectedKey,
  onSelect,
  compact,
}: VariableListProps) {
  const headerLabel =
    'text-[9px] uppercase tracking-[0.24em] text-text-muted/50';
  return (
    <div role="listbox" aria-label="Variables" className="pb-4">
      <div
        aria-hidden="true"
        className={cn(
          'sticky top-0 z-10 px-6 border-l-2 border-l-transparent border-b border-border-subtle bg-background/95 backdrop-blur-sm',
          VARIABLE_ROW_GRID,
          compact ? 'py-1' : 'py-1.5',
        )}
      >
        <span className={headerLabel}>key</span>
        <span className={headerLabel}>type</span>
        <span className={headerLabel}>set</span>
        <span className={headerLabel}>value</span>
        <span className={cn(headerLabel, 'text-right')}>used by</span>
      </div>
      {variables.map((variable) => (
        <VariableRow
          key={variable.key}
          variable={variable}
          selected={variable.key === selectedKey}
          onSelect={() => onSelect(variable.key)}
          preview={
            variable.is_secret
              ? SECRET_PREVIEW
              : (revealed[variable.key] ?? '•••')
          }
          consumerCount={(usage[variable.key] ?? []).length}
          compact={compact}
        />
      ))}
    </div>
  );
}

interface ServerFilterBannerProps {
  serverName: string;
  matchCount: number;
  onClear: () => void;
}

// Inline banner shown when the workspace is deep-linked from a Topology
// server node. Backed by the exact usage index (GET /api/var/usage): the filter
// shows the variables that server actually references.
function ServerFilterBanner({
  serverName,
  matchCount,
  onClear,
}: ServerFilterBannerProps) {
  return (
    <div className="flex-shrink-0 px-6 py-2 border-b border-border-subtle bg-primary/[0.04] flex items-center gap-2">
      <Filter size={12} className="text-primary/70 flex-shrink-0" />
      <div className="flex-1 min-w-0 text-[11px] text-text-secondary">
        Variables used by{' '}
        <span className="font-mono text-primary">{serverName}</span>
        <span className="text-text-muted/70 ml-2">
          · {matchCount} {matchCount === 1 ? 'variable' : 'variables'}
        </span>
      </div>
      <button
        onClick={onClear}
        aria-label="Clear server filter"
        className="flex items-center gap-1 px-2 py-0.5 text-[10px] text-text-muted hover:text-text-primary hover:bg-surface-highlight rounded transition-colors"
      >
        <X size={11} /> Clear
      </button>
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
          Tip: drop a{' '}
          <code className="font-mono text-text-secondary">.env</code> or{' '}
          <code className="font-mono text-text-secondary">.json</code> file
          anywhere on this page.
        </p>
        <p className="text-[10px] text-text-muted/70">
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
