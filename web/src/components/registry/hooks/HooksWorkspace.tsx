import { useCallback, useEffect, useId, useMemo, useState, createElement } from 'react';
import { useSearchParams } from 'react-router';
import { List, Package, RefreshCw, Bot, FileDiff } from 'lucide-react';
import { cn } from '../../../lib/cn';
import { WORKSPACE_LABELS } from '../../../types/workspace';
import { IconButton } from '../../ui/IconButton';
import { showToast } from '../../ui/Toast';
import { WorkspaceShell } from '../../layout/WorkspaceShell';
import { useCommandRegistry } from '../../../hooks/useCommandRegistry';
import { LibraryKindSwitch } from '../LibraryKindSwitch';
import type { RegistryKind } from '../../../lib/registryKind';
import { HookCard } from './HookCard';
import { HookDetailPanel } from './HookDetailPanel';
import { hookSourceKey, statusesByHook } from './hookModel';
import { fetchHookProjectionStatus, fetchRegistryHooks } from '../../../lib/api';
import type { PaletteCommand } from '../../../types/palette';
import type { HookProjectionStatus, HookUnsupportedClient, RegistryHook } from '../../../types';
import { describeError, isCancellation } from '../../../lib/describeError';

interface HooksWorkspaceProps {
  onKindChange: (kind: RegistryKind) => void;
}

export function HooksWorkspace({ onKindChange }: HooksWorkspaceProps) {
  const headingId = useId();
  const [searchParams, setSearchParams] = useSearchParams();
  const [hooks, setHooks] = useState<RegistryHook[] | null>(null);
  const [statuses, setStatuses] = useState<HookProjectionStatus[] | null>(null);
  const [unsupported, setUnsupported] = useState<HookUnsupportedClient[]>([]);
  const searchQuery = searchParams.get('q') ?? '';
  const selectedName = searchParams.get('selected');

  const setSearchQuery = useCallback((value: string) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (value.trim()) next.set('q', value);
      else next.delete('q');
      return next;
    }, { replace: true });
  }, [setSearchParams]);

  const setSelectedName = useCallback((name: string | null) => {
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      if (name) next.set('selected', name);
      else next.delete('selected');
      return next;
    }, { replace: true });
  }, [setSearchParams]);

  const refresh = useCallback(async () => {
    try {
      const [list, rows] = await Promise.all([
        fetchRegistryHooks(),
        fetchHookProjectionStatus(),
      ]);
      setHooks(list.hooks ?? []);
      setUnsupported(list.unsupported_clients ?? []);
      setStatuses(rows ?? []);
    } catch (err) {
      if (!isCancellation(err)) {
        const described = describeError(err, 'Load hooks');
        showToast('error', described.text, { detail: described.fullDetail });
      }
    }
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- async callback; state is set only after await, not synchronously
    void refresh();
  }, [refresh]);

  const { registerCommands, unregisterCommands } = useCommandRegistry();
  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'library:hooks-refresh',
        label: 'Library: Refresh Hooks',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(RefreshCw, { size: 14 }),
        keywords: ['reload', 'refresh', 'hooks', 'hook'],
        onSelect: () => void refresh(),
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
        id: 'library:view-agents',
        label: 'Library: View Agents',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Bot, { size: 14 }),
        keywords: ['agents', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('agent'),
      },
      {
        id: 'library:view-packs',
        label: 'Library: View Packs',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Package, { size: 14 }),
        keywords: ['packs', 'segment', 'switch', 'kind'],
        onSelect: () => onKindChange('pack'),
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
    registerCommands('library-hooks', commands);
    return () => unregisterCommands('library-hooks');
  }, [registerCommands, unregisterCommands, refresh, onKindChange]);

  const statusMap = useMemo(() => statusesByHook(statuses), [statuses]);
  const visible = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    const list = hooks ?? [];
    if (!q) return list;
    return list.filter((h) => h.name.toLowerCase().includes(q) || h.description.toLowerCase().includes(q));
  }, [hooks, searchQuery]);

  const groups = useMemo(() => {
    const bySource = new Map<string, RegistryHook[]>();
    for (const hook of visible) {
      const key = hookSourceKey(hook);
      const group = bySource.get(key);
      if (group) group.push(hook);
      else bySource.set(key, [hook]);
    }
    return [...bySource.entries()]
      .map(([source, members]) => ({
        source,
        members: [...members].sort((a, b) => a.name.localeCompare(b.name)),
      }))
      .sort((a, b) => a.source.localeCompare(b.source));
  }, [visible]);

  const selected = useMemo(
    () => (selectedName ? (hooks ?? []).find((h) => h.name === selectedName) ?? null : null),
    [selectedName, hooks],
  );

  return (
    <div className="absolute inset-0 flex flex-col bg-background text-text-primary overflow-hidden">
      <WorkspaceShell
        workspace="library"
        defaultRightPct={30}
        minRightPx={300}
        right={(
          <HookDetailPanel
            key={selected?.name ?? 'none'}
            hook={selected}
            statuses={selected ? statusMap.get(selected.name) ?? [] : []}
            unsupported={unsupported}
            onClose={() => setSelectedName(null)}
            onRefresh={refresh}
          />
        )}
      >
        <section aria-labelledby={headingId} className="flex flex-col h-full overflow-hidden">
          <h1 id={headingId} className="sr-only">{WORKSPACE_LABELS.library}</h1>
          <header className="flex-shrink-0 border-b border-border-subtle px-6 py-3 flex items-center justify-between gap-3">
            <LibraryKindSwitch kind="hook" onChange={onKindChange} />
            <IconButton icon={RefreshCw} onClick={() => void refresh()} tooltip="Refresh" size="sm" variant="ghost" />
          </header>
          <div className="px-6 py-2">
            <input
              aria-label="Search hooks"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search hooks"
              className="w-full text-xs rounded-lg border border-border bg-background px-3 py-1.5"
            />
          </div>
          <div className="flex-1 overflow-auto px-6 pb-6 flex flex-col gap-4">
            {hooks === null && <p className="text-xs text-text-muted">Loading hooks…</p>}
            {hooks !== null && groups.length === 0 && (
              <p className="text-xs text-text-muted">No hooks imported. Hooks enter through a pack.</p>
            )}
            {groups.map((group) => (
              <section key={group.source} aria-label={`Hooks from ${group.source}`}>
                <h2 className="text-3xs uppercase tracking-wider text-text-muted mb-2">{group.source}</h2>
                <div className={cn('grid gap-2', 'grid-cols-1 sm:grid-cols-2')}>
                  {group.members.map((hook) => (
                    <HookCard
                      key={hook.name}
                      hook={hook}
                      statuses={statusMap.get(hook.name) ?? []}
                      isActive={hook.name === selectedName}
                      onSelect={(h) => setSelectedName(h.name)}
                    />
                  ))}
                </div>
              </section>
            ))}
          </div>
        </section>
      </WorkspaceShell>
    </div>
  );
}
