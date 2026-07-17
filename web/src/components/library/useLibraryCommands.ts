import { useEffect } from 'react';
import { createElement } from 'react';
import {
  CheckCircle2,
  ExternalLink,
  FileText,
  FolderGit2,
  Layers,
  List,
  Plus,
  PowerOff,
  RefreshCw,
} from 'lucide-react';
import { useCommandRegistry } from '../../hooks/useCommandRegistry';
import type { PaletteCommand } from '../../types/palette';
import type { GroupMode } from '../registry/LibraryGrid';

export type LibraryFilter = 'all' | 'active' | 'draft' | 'disabled';

interface UseLibraryCommandsOptions {
  onNewSkill: () => void;
  onRefresh: () => void;
  onShowAll: () => void;
  onFilter: (filter: LibraryFilter) => void;
  onOpenInNewWindow: () => void;
  /** Optional: switch the grouping axis (Source / Category / None). */
  onSetGroup?: (mode: GroupMode) => void;
}

/**
 * Workspace-scoped palette commands for /library. Registered on mount,
 * unregistered on unmount — so Stack, Stage, and Runs never see them.
 */
export function useLibraryCommands({
  onNewSkill,
  onRefresh,
  onShowAll,
  onFilter,
  onOpenInNewWindow,
  onSetGroup,
}: UseLibraryCommandsOptions): void {
  const { registerCommands, unregisterCommands } = useCommandRegistry();

  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'library:new-skill',
        label: 'Library: New Skill',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(Plus, { size: 14 }),
        keywords: ['create', 'add', 'skill', 'new'],
        onSelect: onNewSkill,
      },
      {
        id: 'library:refresh',
        label: 'Library: Refresh',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(RefreshCw, { size: 14 }),
        keywords: ['reload', 'rescan', 'refresh'],
        onSelect: onRefresh,
      },
      {
        id: 'library:show-all',
        label: 'Library: Show All',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(List, { size: 14 }),
        keywords: ['clear', 'filter', 'reset', 'all'],
        onSelect: onShowAll,
      },
      {
        id: 'library:filter-active',
        label: 'Library: Filter Active',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(CheckCircle2, { size: 14 }),
        keywords: ['filter', 'active', 'enabled'],
        onSelect: () => onFilter('active'),
      },
      {
        id: 'library:filter-draft',
        label: 'Library: Filter Draft',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(FileText, { size: 14 }),
        keywords: ['filter', 'draft', 'unfinished'],
        onSelect: () => onFilter('draft'),
      },
      {
        id: 'library:filter-disabled',
        label: 'Library: Filter Disabled',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(PowerOff, { size: 14 }),
        keywords: ['filter', 'disabled', 'inactive', 'off'],
        onSelect: () => onFilter('disabled'),
      },
      {
        id: 'library:open-new-window',
        label: 'Library: Open in New Window',
        section: 'registry',
        workspaces: ['library'],
        icon: createElement(ExternalLink, { size: 14 }),
        keywords: ['popout', 'window', 'detach', 'tab'],
        onSelect: onOpenInNewWindow,
      },
    ];

    if (onSetGroup) {
      commands.push(
        {
          id: 'library:group-source',
          label: 'Library: Group by Source',
          section: 'registry',
          workspaces: ['library'],
          icon: createElement(FolderGit2, { size: 14 }),
          keywords: ['group', 'source', 'provenance', 'origin', 'repo'],
          onSelect: () => onSetGroup('source'),
        },
        {
          id: 'library:group-category',
          label: 'Library: Group by Category',
          section: 'registry',
          workspaces: ['library'],
          icon: createElement(Layers, { size: 14 }),
          keywords: ['group', 'category', 'directory', 'folder'],
          onSelect: () => onSetGroup('category'),
        },
        {
          id: 'library:group-none',
          label: 'Library: No Grouping',
          section: 'registry',
          workspaces: ['library'],
          icon: createElement(List, { size: 14 }),
          keywords: ['group', 'none', 'flat', 'ungroup'],
          onSelect: () => onSetGroup('none'),
        },
      );
    }

    registerCommands('library', commands);
    return () => unregisterCommands('library');
  }, [
    registerCommands,
    unregisterCommands,
    onNewSkill,
    onRefresh,
    onShowAll,
    onFilter,
    onOpenInNewWindow,
    onSetGroup,
  ]);
}
