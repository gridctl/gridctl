import { useEffect } from 'react';
import { createElement } from 'react';
import { Layers, List, Network, PlayCircle, RefreshCw, Minimize2 } from 'lucide-react';
import { useCommandRegistry } from '../../hooks/useCommandRegistry';
import { useUIStore } from '../../stores/useUIStore';
import type { PaletteCommand } from '../../types/palette';
import type { SkillSummary } from '../../lib/agent-api';
import type { RefObject } from 'react';

type ViewMode = 'list' | 'canvas';

interface UseSkillsCommandsOptions {
  skills: SkillSummary[];
  activeSkill: string | null;
  viewMode: ViewMode;
  onSetView: (m: ViewMode) => void;
  onSelectSkill: (name: string) => void;
  // The IDE expects a ref so it can return focus to the originating button
  // when the launcher closes. Palette-driven launches have no origin, so we
  // hand in a one-off detached ref pointing at nothing.
  onLaunchSkill: (name: string, originRef: RefObject<HTMLButtonElement | null>) => void;
  onRefresh: () => void;
}

/**
 * Workspace-scoped palette commands for /skills. Registered on mount,
 * unregistered on unmount — so Topology and Runs never see them.
 */
export function useSkillsCommands({
  skills,
  activeSkill,
  viewMode,
  onSetView,
  onSelectSkill,
  onLaunchSkill,
  onRefresh,
}: UseSkillsCommandsOptions): void {
  const { registerCommands, unregisterCommands } = useCommandRegistry();
  const toggleCompactMode = useUIStore((s) => s.toggleCompactMode);

  useEffect(() => {
    const commands: PaletteCommand[] = [
      {
        id: 'skills:toggle-view',
        label: `Switch view: ${viewMode === 'list' ? 'canvas' : 'list'}`,
        section: 'canvas',
        workspaces: ['skills'],
        icon: createElement(viewMode === 'list' ? Network : List, { size: 14 }),
        keywords: ['view', 'list', 'canvas', 'toggle'],
        onSelect: () => onSetView(viewMode === 'list' ? 'canvas' : 'list'),
      },
      {
        id: 'skills:refresh',
        label: 'Refresh skills',
        section: 'canvas',
        workspaces: ['skills'],
        icon: createElement(RefreshCw, { size: 14 }),
        keywords: ['reload', 'rescan'],
        onSelect: onRefresh,
      },
      {
        id: 'skills:toggle-compact',
        label: 'Toggle Compact Mode',
        section: 'canvas',
        workspaces: ['skills'],
        icon: createElement(Minimize2, { size: 14 }),
        keywords: ['compact', 'density', 'shrink'],
        shortcut: ['⌘', '\\'],
        onSelect: () => toggleCompactMode('skills'),
      },
      ...skills.map<PaletteCommand>((skill) => ({
        id: `skills:select:${skill.name}`,
        label: `Open skill: ${skill.name}`,
        section: 'canvas',
        workspaces: ['skills'],
        icon: createElement(Layers, { size: 14 }),
        keywords: [skill.lang ?? '', skill.dir ?? '', 'graph', 'open'].filter(Boolean),
        unavailable: skill.name === activeSkill,
        onSelect: () => onSelectSkill(skill.name),
      })),
      ...skills
        .filter((s) => s.lang === 'ts')
        .map<PaletteCommand>((skill) => ({
          id: `skills:launch:${skill.name}`,
          label: `Run skill: ${skill.name}`,
          section: 'canvas',
          workspaces: ['skills'],
          icon: createElement(PlayCircle, { size: 14 }),
          keywords: ['launch', 'execute', skill.lang ?? ''],
          onSelect: () => onLaunchSkill(skill.name, { current: null }),
        })),
    ];
    registerCommands('skills', commands);
    return () => unregisterCommands('skills');
  }, [
    registerCommands,
    unregisterCommands,
    skills,
    activeSkill,
    viewMode,
    onSetView,
    onSelectSkill,
    onLaunchSkill,
    onRefresh,
    toggleCompactMode,
  ]);
}
