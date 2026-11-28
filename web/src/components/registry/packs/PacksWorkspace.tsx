import { useCallback, useEffect, useId, useMemo, useRef, useState, createElement } from 'react';
import { useSearchParams } from 'react-router';
import { Copy, Download, List, Package, RefreshCw, Bot, Webhook, FileDiff, Globe } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { WORKSPACE_LABELS } from '../../../types/workspace';
import { IconButton } from '../../ui/IconButton';
import { WorkspaceHeader } from '../../ui/WorkspaceHeader';
import { useUIStore } from '../../../stores/useUIStore';
import { showToast } from '../../ui/Toast';
import { FailedState, FailureBanner } from '../../ui/EmptyState';
import { WorkspaceShell } from '../../layout/WorkspaceShell';
import { useCommandRegistry } from '../../../hooks/useCommandRegistry';
import { useRegistryStore } from '../../../stores/useRegistryStore';
import { useWizardStore } from '../../../stores/useWizardStore';
import { LibraryKindSwitch } from '../LibraryKindSwitch';
import type { RegistryKind } from '../../../lib/registryKind';
import { PackDetailPanel } from './PackDetailPanel';
import { packNeedsAttention, sortPacks } from './packModel';
import { fetchPacks, type PackListItem } from '../../../lib/api';
import type { PaletteCommand } from '../../../types/palette';
import { describeError, isCancellation } from '../../../lib/describeError';

interface PacksWorkspaceProps {
  /** Switch the Library segment (updates ?kind). */
  onKindChange: (kind: RegistryKind) => void;
}

/**
 * The Packs segment of the Library workspace: installed team packs (one
 * git repo plus a gridctl-pack.yaml manifest selecting skills, agents,
 * rules, and wiring) with per-resource lifecycle state. At the expected
 * scale (a handful of packs, not a hundred skills), the catalog is a
 * single-column list beside the detail pane; no search, no source
 * grouping (a pack IS a source).
 */
export function PacksWorkspace({ onKindChange }: PacksWorkspaceProps) {
  const headingId = useId();
  const compact = useUIStore((st) => st.compactMode.library);
  const [searchParams, setSearchParams] = useSearchParams();
  const packs = useRegistryStore((s) => s.packs);
  const openWizard = useWizardStore((s) => s.open);

  const isLoading = packs === null;
  const hasPacks = (packs ?? []).length > 0;
  const selectedName = searchParams.get('selected');
  // The store list can predate this mount (the skills segment caches it
  // for the reverse chips), so deep-link validation waits for this
  // segment's own fetch: a pack imported moments ago must never have its
  // own deep link toast-and-cleared against a stale list.
  const [segmentLoaded, setSegmentLoaded] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  const setSelectedName = useCallback((name: string | null) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        if (name) next.set('selected', name);
        else next.delete('selected');
        return next;
      },
      { replace: true },
    );
  }, [setSearchParams]);

  // Segment-entry fetch: packs are not on the global polling cycle, so a
  // pack added via the CLI appears when the segment mounts (or on
  // Refresh / after any mutation, which own their refetch).
  const refresh = useCallback(async () => {
    try {
      const list = await fetchPacks();
      useRegistryStore.getState().setPacks(list);
      setSegmentLoaded(true);
      setLoadError(null);
    } catch (err) {
      if (!isCancellation(err)) setLoadError(describeError(err, 'Load packs').text);
    }
  }, []);

  useEffect(() => {
    let cancelled = false;
    fetchPacks()
      .then((list) => {
        if (cancelled) return;
        useRegistryStore.getState().setPacks(list);
        setSegmentLoaded(true);
        setLoadError(null);
      })
      .catch((err) => {
        if (cancelled) return;
        if (!isCancellation(err)) setLoadError(describeError(err, 'Load packs').text);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Deep-link contract for ?selected=: wait for the load, then
  // toast-and-clear on a miss (the Library's shipped grammar).
  const lastResolvedRef = useRef<string | null>(null);
  useEffect(() => {
    if (!selectedName) {
      lastResolvedRef.current = null;
      return;
    }
    if (lastResolvedRef.current === selectedName || !segmentLoaded) return;
    lastResolvedRef.current = selectedName;
    if (!(packs ?? []).some((p) => p.name === selectedName)) {
      showToast('error', `Pack "${selectedName}" not found`);
      setSelectedName(null);
    }
  }, [selectedName, segmentLoaded, packs, setSelectedName]);

  const handleImport = useCallback(() => openWizard('pack'), [openWizard]);

  // Palette commands under the segment's own scope, with switches to the
  // other two segments (keywords use "pack", never "bundle").
  const { registerCommands, unregisterCommands } = useCommandRegistry();
  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'library:packs-refresh',
        label: 'Library: Refresh Packs',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(RefreshCw, { size: 14 }),
        keywords: ['reload', 'refresh', 'packs', 'pack'],
        onSelect: () => void refresh(),
      },
      {
        id: 'library:packs-import',
        label: 'Library: Import Pack from Git',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Download, { size: 14 }),
        keywords: ['import', 'git', 'pack', 'manifest'],
        onSelect: handleImport,
      },
      {
        id: 'library:view-skills',
        label: 'Library: View Skills',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(List, { size: 14 }),
        keywords: ['skills', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('skill'),
      },
      {
        id: 'library:view-hooks',
        label: 'Library: View Hooks',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Webhook, { size: 14 }),
        keywords: ['hooks', 'hook', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('hook'),
      },
      {
        id: 'library:view-agents',
        label: 'Library: View Agents',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Bot, { size: 14 }),
        keywords: ['agents', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('agent'),
      },
      {
        id: 'library:view-context',
        label: 'Library: View Context',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Globe, { size: 14 }),
        keywords: ['context', 'global', 'agents.md', 'rules', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('context'),
      },
      {
        id: 'library:view-proposals',
        label: 'Library: View Proposals',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(FileDiff, { size: 14 }),
        keywords: ['proposals', 'proposal', 'review', 'staged', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('proposal'),
      },
    ];
    registerCommands('library-packs', commands);
    return () => unregisterCommands('library-packs');
  }, [registerCommands, unregisterCommands, refresh, handleImport, onKindChange]);

  // Esc closes the detail selection, guarded against open dialogs and
  // text inputs (the segment owns Esc; LibraryWorkspace's shared handler
  // carves this kind out).
  useEffect(() => {
    if (!selectedName) return;
    const handler = (e: KeyboardEvent) => {
      if (e.key !== 'Escape') return;
      if (document.querySelector('[role="dialog"], [role="alertdialog"]')) return;
      const t = e.target as HTMLElement | null;
      if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
      setSelectedName(null);
    };
    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [selectedName, setSelectedName]);

  const sorted = useMemo(() => sortPacks(packs ?? []), [packs]);
  const attentionCount = useMemo(
    () => (packs ?? []).filter(packNeedsAttention).length,
    [packs],
  );

  const inspector = (
    <PackDetailPanel
      name={selectedName}
      onClose={() => setSelectedName(null)}
      onChanged={refresh}
    />
  );

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell workspace="library" defaultRightPct={42} minRightPx={380} right={inspector} rightKey={selectedName}>
        <section aria-labelledby={headingId} className="flex flex-col h-full overflow-hidden">
          <WorkspaceHeader
            title={WORKSPACE_LABELS.library}
            headingId={headingId}
            compact={compact}
            switcher={<LibraryKindSwitch kind="pack" onChange={onKindChange} />}
            summary={isLoading ? undefined : (
              <span className="font-mono">
                {sorted.length} {sorted.length === 1 ? 'pack' : 'packs'}
              </span>
            )}
            actions={(
              <>
                <button
                  onClick={handleImport}
                  className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium whitespace-nowrap text-secondary hover:text-secondary/80 border border-secondary/25 hover:bg-secondary/10 rounded-lg transition-colors"
                  title="Import a pack repository (gridctl-pack.yaml at its root)"
                >
                  <Download size={12} /> Import
                </button>
                <IconButton icon={RefreshCw} onClick={() => void refresh()} tooltip="Refresh" size="sm" variant="ghost" />
              </>
            )}
          >
            {/* KPI strip: Total and Attention only. A pack's completion is
                not one number; that story lives on the detail pane. */}
            <div className="flex items-center gap-1.5 flex-wrap" role="group" aria-label="Pack summary">
              <PackKpi label="Total" value={isLoading ? null : sorted.length} />
              <PackKpi label="Attention" value={isLoading ? null : attentionCount} />
            </div>
          </WorkspaceHeader>

          <div className="flex-1 overflow-y-auto scrollbar-dark flex flex-col">
            {isLoading && loadError && (
              <FailedState
                feature="Packs"
                message={loadError}
                onRetry={() => {
                  setLoadError(null);
                  void refresh();
                }}
                className="flex-1"
              />
            )}
            {!isLoading && loadError && (
              <FailureBanner message={loadError} onRetry={() => void refresh()} className="mx-4" />
            )}

            {isLoading && !loadError && (
              <div className="h-full flex items-center justify-center text-sm text-text-muted">
                Loading packs…
              </div>
            )}

            {!isLoading && !hasPacks && (
              <div className="h-full flex flex-col items-center justify-center text-text-muted gap-3 animate-fade-in-scale p-8 text-center">
                <div className="p-4 rounded-xl bg-surface-elevated/50 border border-border/30">
                  <Package size={32} className="text-text-muted" />
                </div>
                <span className="text-sm text-text-secondary">No packs imported</span>
                <span className="text-2xs text-text-muted max-w-md">
                  A pack is one git repository with a{' '}
                  <span className="font-mono">gridctl-pack.yaml</span> manifest selecting
                  skills, agents, rules, and wiring, so a single import configures a whole
                  setup. Packs are opt-in; see{' '}
                  <span className="font-mono">examples/portable-pack/</span> for a complete
                  repo layout.
                </span>
                <div className="flex items-center gap-2">
                  <button
                    onClick={handleImport}
                    className="flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-primary bg-primary/10 hover:bg-primary/15 border border-primary/20 rounded-lg transition-colors"
                  >
                    <Download size={12} /> Import from git
                  </button>
                  <button
                    onClick={() => {
                      void navigator.clipboard?.writeText('gridctl pack add <repo-url>');
                      showToast('success', 'CLI command copied');
                    }}
                    className="flex items-center gap-1.5 px-3 py-1.5 text-2xs font-mono text-text-muted border border-border/40 hover:bg-surface-highlight rounded-lg transition-colors"
                    title="Copy the CLI command"
                  >
                    <Copy size={11} /> gridctl pack add
                  </button>
                </div>
              </div>
            )}

            {!isLoading && hasPacks && (
              <ul className="p-4 flex flex-col gap-2 max-w-3xl" aria-label="Installed packs">
                {sorted.map((p) => (
                  <PackCard
                    key={`${p.origin.source}:${p.name}`}
                    pack={p}
                    isActive={p.name === selectedName}
                    onSelect={() => setSelectedName(p.name)}
                  />
                ))}
              </ul>
            )}
          </div>
        </section>
      </WorkspaceShell>
    </div>
  );
}

/** One installed pack: a full-width row card, not a grid tile. */
function PackCard({
  pack: p,
  isActive,
  onSelect,
}: {
  pack: PackListItem;
  isActive: boolean;
  onSelect: () => void;
}) {
  const attention = packNeedsAttention(p);
  const counts: string[] = [];
  if (p.counts.skills > 0) counts.push(`${p.counts.skills} skill${p.counts.skills === 1 ? '' : 's'}`);
  if (p.counts.agents > 0) counts.push(`${p.counts.agents} agent${p.counts.agents === 1 ? '' : 's'}`);
  if (p.counts.rules > 0) counts.push(`${p.counts.rules} rule${p.counts.rules === 1 ? '' : 's'}`);
  const hooks = p.counts.hooks ?? 0;
  if (hooks > 0) counts.push(`${hooks} hook${hooks === 1 ? '' : 's'}`);
  if (p.counts.wiring) counts.push('wiring');

  return (
    <li>
      <button
        onClick={onSelect}
        aria-current={isActive ? 'true' : undefined}
        className={cn(
          'w-full text-left rounded-xl border px-4 py-3 transition-colors',
          isActive
            ? 'border-primary/40 bg-primary/5'
            : 'border-border/40 bg-surface/40 hover:bg-surface-highlight/50',
        )}
      >
        <div className="flex items-center gap-2">
          <Package size={14} className={attention ? 'text-status-pending' : 'text-text-muted'} aria-hidden="true" />
          <span className="text-sm font-medium text-text-primary">{p.name}</span>
          {p.version && <span className="text-3xs font-mono text-text-muted">v{p.version}</span>}
          <span className="flex-1" />
          {p.collision && (
            <span className="text-3xs font-medium uppercase tracking-wider px-1.5 py-0.5 rounded-full border border-status-error/30 bg-status-error/10 text-status-error">
              Name collision
            </span>
          )}
          {!p.applied && !p.collision && (
            <span className="text-3xs font-medium uppercase tracking-wider px-1.5 py-0.5 rounded-full border border-status-pending/30 bg-status-pending/10 text-status-pending">
              Not applied
            </span>
          )}
          {p.applied && p.needs_attention && (
            <span className="text-3xs font-medium uppercase tracking-wider px-1.5 py-0.5 rounded-full border border-status-pending/30 bg-status-pending/10 text-status-pending">
              Needs attention
            </span>
          )}
        </div>
        {p.description && (
          <p className="text-2xs text-text-muted mt-1 line-clamp-2">{p.description}</p>
        )}
        <div className="flex items-center gap-2 mt-1.5 text-3xs text-text-muted font-mono">
          <span>{counts.length ? counts.join(' · ') : 'empty selection'}</span>
          {(p.unresolved ?? []).length > 0 && (
            <span className="text-status-pending">{(p.unresolved ?? []).length} unresolved</span>
          )}
        </div>
      </button>
    </li>
  );
}

function PackKpi({ label, value }: { label: string; value: number | null }) {
  return (
    <div className="flex flex-col items-start rounded-lg border bg-background/40 border-border/40 px-3 py-1.5 min-w-[60px]">
      <span className="text-3xs uppercase tracking-wider font-medium text-text-muted">{label}</span>
      <span className="font-mono leading-none mt-0.5 text-sm text-text-secondary">
        {value === null ? '–' : value}
      </span>
    </div>
  );
}
