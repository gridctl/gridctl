import { Fragment } from 'react';
import { cn } from '../../lib/cn';
import { toTitleCase } from '../../lib/text';
import { SkillCard } from './SkillCard';
import { SourceGroupHeader } from './SourceGroupHeader';
import { skillCategory } from '../../lib/skillMeta';
import type { AgentSkill, SkillSourceStatus } from '../../types';

export type GroupMode = 'source' | 'category' | 'none';

function groupSkills(skills: AgentSkill[]): Map<string, AgentSkill[]> {
  const groups = new Map<string, AgentSkill[]>();
  for (const skill of skills) {
    const key = skillCategory(skill.dir);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key)!.push(skill);
  }
  return groups;
}

const GRID_STYLE: React.CSSProperties = {
  display: 'grid',
  gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
  gap: '12px',
};

export interface LibraryGridProps {
  skills: AgentSkill[];
  hasSearch: boolean;
  onEnable: (skill: AgentSkill) => void;
  onDisable: (skill: AgentSkill) => void;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (skill: AgentSkill) => void;
  className?: string;
  /**
   * Grouping mode. When omitted, the grid uses its legacy category-or-flat
   * heuristic — keeping the detached registry window and existing callers
   * unchanged.
   */
  groupMode?: GroupMode;
  /** skill name → owning source, used for provenance grouping and card badges. */
  sourceMap?: Map<string, SkillSourceStatus>;
  /** Active provenance isolate: a source name, `local`, or null. */
  activeSource?: string | null;
  onIsolateSource?: (key: string | null) => void;
  /** Refresh callback invoked after an inline source update. */
  onRefresh?: () => void;
  /** Select a card into the inspector. Optional so the detached grid (which
   *  has no inspector rail) is unaffected. */
  onSelect?: (skill: AgentSkill) => void;
  /** Name of the skill currently shown in the inspector, for active styling. */
  activeSkillName?: string | null;
}

/**
 * Card grid used by both the in-app Library workspace and the detached
 * /library-window page. With `groupMode` omitted (or `'category'`), it groups
 * skills by their top-level directory when the structure is meaningful (2+
 * groups with at least one populated group) and otherwise renders a flat grid.
 * `groupMode='source'` groups by provenance ("My Skills" + one section per
 * imported source); `groupMode='none'` renders a flat grid.
 */
export function LibraryGrid({
  skills,
  hasSearch,
  onEnable,
  onDisable,
  onEdit,
  onDelete,
  className,
  groupMode,
  sourceMap,
  activeSource = null,
  onIsolateSource,
  onRefresh,
  onSelect,
  activeSkillName = null,
}: LibraryGridProps) {
  const cardHandlers = { onEnable, onDisable, onEdit, onDelete };

  const renderCard = (skill: AgentSkill, i: number) => (
    <SkillCard
      key={skill.name}
      skill={skill}
      source={sourceMap?.get(skill.name)}
      onSelect={onSelect}
      isActive={skill.name === activeSkillName}
      className={cn(
        'motion-safe:animate-fade-in-scale',
        skill.metadata?.colspan === '2' ? 'col-span-2' : undefined,
      )}
      style={{ animationDelay: `${Math.min(i, 10) * 30}ms` }}
      {...cardHandlers}
    />
  );

  // Provenance grouping: "My Skills" first, then imported sources by name.
  if (groupMode === 'source' && sourceMap) {
    const local: AgentSkill[] = [];
    const bySource = new Map<string, { source: SkillSourceStatus; skills: AgentSkill[] }>();
    for (const skill of skills) {
      const src = sourceMap.get(skill.name);
      if (!src) {
        local.push(skill);
        continue;
      }
      const group = bySource.get(src.name);
      if (group) group.skills.push(skill);
      else bySource.set(src.name, { source: src, skills: [skill] });
    }
    const sourceGroups = Array.from(bySource.values()).sort((a, b) =>
      a.source.name.localeCompare(b.source.name),
    );

    return (
      <div className={cn('p-4', className)} style={GRID_STYLE}>
        {local.length > 0 && (
          <>
            <SourceGroupHeader
              count={local.length}
              hasSearch={hasSearch}
              isActive={activeSource === 'local'}
              onToggle={() => onIsolateSource?.(activeSource === 'local' ? null : 'local')}
            />
            {local.map(renderCard)}
          </>
        )}
        {sourceGroups.map(({ source, skills: groupSkills }) => (
          <Fragment key={source.name}>
            <SourceGroupHeader
              source={source}
              count={groupSkills.length}
              hasSearch={hasSearch}
              isActive={activeSource === source.name}
              onToggle={() => onIsolateSource?.(activeSource === source.name ? null : source.name)}
              onUpdated={onRefresh}
            />
            {groupSkills.map(renderCard)}
          </Fragment>
        ))}
      </div>
    );
  }

  // Explicit flat mode.
  if (groupMode === 'none') {
    return (
      <div className={cn('p-4', className)} style={GRID_STYLE}>
        {skills.map(renderCard)}
      </div>
    );
  }

  // Legacy / category mode: group by top-level directory when meaningful.
  const groups = groupSkills(skills);
  const hasMeaningfulGrouping =
    groups.size > 1 && Array.from(groups.values()).some((g) => g.length > 1);

  if (!hasMeaningfulGrouping) {
    return (
      <div className={cn('p-4', className)} style={GRID_STYLE}>
        {skills.map(renderCard)}
      </div>
    );
  }

  return (
    <div className={cn('p-4', className)} style={GRID_STYLE}>
      {Array.from(groups.entries()).map(([key, groupSkillList]) => (
        <Fragment key={key || '__ungrouped__'}>
          <div
            style={{ gridColumn: '1 / -1' }}
            className="flex flex-col gap-1 mt-2 first:mt-0 animate-fade-in-scale"
          >
            <div className="flex items-center justify-between">
              <span className="text-[10px] uppercase tracking-widest text-text-muted font-medium">
                {key ? toTitleCase(key) : 'Other'}
              </span>
              <span className="text-[10px] px-1.5 rounded-full bg-surface-highlight text-text-muted">
                {groupSkillList.length} {hasSearch ? 'matched' : 'skills'}
              </span>
            </div>
            <div className="border-b border-border/30" />
          </div>
          {groupSkillList.map(renderCard)}
        </Fragment>
      ))}
    </div>
  );
}
