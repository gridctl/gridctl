import { cn } from '../../lib/cn';
import { SkillCard } from './SkillCard';
import type { AgentSkill } from '../../types';

function getGroupKey(dir?: string): string {
  if (!dir) return '';
  return dir.split('/')[0];
}

function groupSkills(skills: AgentSkill[]): Map<string, AgentSkill[]> {
  const groups = new Map<string, AgentSkill[]>();
  for (const skill of skills) {
    const key = getGroupKey(skill.dir);
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key)!.push(skill);
  }
  return groups;
}

function toTitleCase(key: string): string {
  return key.replace(/-/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase());
}

export interface LibraryGridProps {
  skills: AgentSkill[];
  hasSearch: boolean;
  onEnable: (skill: AgentSkill) => void;
  onDisable: (skill: AgentSkill) => void;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (skill: AgentSkill) => void;
  className?: string;
}

/**
 * Card grid used by both the in-app Library workspace and the detached
 * /library-window page. Groups skills by their top-level directory when the
 * structure is meaningful (2+ groups with at least one populated group);
 * otherwise renders a flat grid so single-skill "groups" don't waste a
 * full-width header.
 */
export function LibraryGrid({
  skills,
  hasSearch,
  onEnable,
  onDisable,
  onEdit,
  onDelete,
  className,
}: LibraryGridProps) {
  const groups = groupSkills(skills);
  const hasMeaningfulGrouping =
    groups.size > 1 && Array.from(groups.values()).some((g) => g.length > 1);

  const gridStyle: React.CSSProperties = {
    display: 'grid',
    gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
    gap: '12px',
  };

  if (!hasMeaningfulGrouping) {
    return (
      <div className={cn('p-4', className)} style={gridStyle}>
        {skills.map((skill, i) => (
          <SkillCard
            key={skill.name}
            skill={skill}
            className={cn('motion-safe:animate-fade-in-scale', skill.metadata?.colspan === '2' ? 'col-span-2' : undefined)}
            style={{ animationDelay: `${Math.min(i, 10) * 30}ms` }}
            onEnable={onEnable}
            onDisable={onDisable}
            onEdit={onEdit}
            onDelete={onDelete}
          />
        ))}
      </div>
    );
  }

  return (
    <div className={cn('p-4', className)} style={gridStyle}>
      {Array.from(groups.entries()).map(([key, groupSkillList]) => (
        <GroupSection
          key={key || '__ungrouped__'}
          groupKey={key}
          skills={groupSkillList}
          hasSearch={hasSearch}
          onEnable={onEnable}
          onDisable={onDisable}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      ))}
    </div>
  );
}

interface GroupSectionProps {
  groupKey: string;
  skills: AgentSkill[];
  hasSearch: boolean;
  onEnable: (skill: AgentSkill) => void;
  onDisable: (skill: AgentSkill) => void;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (skill: AgentSkill) => void;
}

function GroupSection({ groupKey, skills, hasSearch, onEnable, onDisable, onEdit, onDelete }: GroupSectionProps) {
  return (
    <>
      <div style={{ gridColumn: '1 / -1' }} className="flex flex-col gap-1 mt-2 first:mt-0 animate-fade-in-scale">
        <div className="flex items-center justify-between">
          <span className="text-[10px] uppercase tracking-widest text-text-muted font-medium">
            {groupKey ? toTitleCase(groupKey) : 'Other'}
          </span>
          <span className="text-[10px] px-1.5 rounded-full bg-surface-highlight text-text-muted">
            {skills.length} {hasSearch ? 'matched' : 'skills'}
          </span>
        </div>
        <div className="border-b border-border/30" />
      </div>
      {skills.map((skill, i) => (
        <SkillCard
          key={skill.name}
          skill={skill}
          className={cn('motion-safe:animate-fade-in-scale', skill.metadata?.colspan === '2' ? 'col-span-2' : undefined)}
          style={{ animationDelay: `${Math.min(i, 10) * 30}ms` }}
          onEnable={onEnable}
          onDisable={onDisable}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      ))}
    </>
  );
}
