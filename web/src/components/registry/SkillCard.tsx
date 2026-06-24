import { memo } from 'react';
import { BookOpen, Check, GitBranch } from 'lucide-react';
import { cn } from '../../lib/cn';
import { StateBadge } from './StateBadge';
import { SkillActions } from './SkillActions';
import { skillCategory, skillMetaSummary } from '../../lib/skillMeta';
import { toTitleCase } from '../../lib/text';
import type { AgentSkill, SkillSourceStatus } from '../../types';

export interface SkillCardProps {
  skill: AgentSkill;
  onEnable: (skill: AgentSkill) => void;
  onDisable: (skill: AgentSkill) => void;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (skill: AgentSkill) => void;
  /** Imported-from source, when this skill came from a git source. */
  source?: SkillSourceStatus;
  /** Select this card into the inspector. When omitted (e.g. the detached
   *  grid), the card body is not interactive. */
  onSelect?: (skill: AgentSkill) => void;
  /** Whether this card is the one shown in the inspector. */
  isActive?: boolean;
  /** Toggle this card's membership in the multi-select set. When omitted, no
   *  selection checkbox is rendered (e.g. the detached grid). */
  onToggleSelect?: (skill: AgentSkill) => void;
  /** Whether this card is currently in the multi-select set. */
  selected?: boolean;
  /** Whether any cards are selected, which keeps every checkbox visible while a
   *  selection is in progress, not just on hover. */
  selectionActive?: boolean;
  className?: string;
  style?: React.CSSProperties;
}

export const SkillCard = memo(({
  skill,
  onEnable,
  onDisable,
  onEdit,
  onDelete,
  source,
  onSelect,
  isActive = false,
  onToggleSelect,
  selected = false,
  selectionActive = false,
  className,
  style,
}: SkillCardProps) => {
  const handleToggle = (s: AgentSkill) => {
    if (s.state === 'active') onDisable(s);
    else onEnable(s);
  };

  // Scannability signals, both derived from fields the skill already carries.
  const category = skillCategory(skill.dir);
  const metaSummary = skillMetaSummary(skill).join(' · ');
  const hasLocalEdits = source?.driftedSkills?.includes(skill.name) ?? false;

  return (
    <div
      style={style}
      aria-current={isActive ? 'true' : undefined}
      className={cn(
        'group relative rounded-xl overflow-hidden flex flex-col',
        'backdrop-blur-xl border transition-all duration-200 ease-out',
        'bg-gradient-to-b from-surface/95 via-surface/90 to-primary/[0.02]',
        'border-white/[0.08] hover:border-primary/40 focus-within:border-primary/40 hover:shadow-node-hover',
        isActive && 'border-primary/50 bg-primary/[0.06] shadow-node-hover',
        className,
      )}
    >
      {/* Top accent line — muted at rest, warms on hover/focus (and when active) */}
      <div className={cn(
        'absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent to-transparent transition-colors duration-200',
        'via-white/10 group-hover:via-primary/40 group-focus-within:via-primary/40',
        isActive && 'via-primary/50',
      )} />

      {/* Left accent — neutral at rest for cross-card scanning rhythm, warming
          to amber only while this card is the active selection. The amber bar
          rhymes with the detail pane's leading edge (PaneAnchor), tracing a
          breadcrumb from the selected card to its expanded properties. It marks
          transient selection, not category identity (which reads as text). */}
      <div
        aria-hidden="true"
        className={cn(
          'absolute top-0 bottom-0 left-0 w-0.5 transition-colors duration-200',
          isActive ? 'bg-primary' : 'bg-white/10',
        )}
      />

      {/* Card body — clickable to open the inspector */}
      <div
        className={cn('p-3 flex flex-col gap-2 flex-1', onSelect && 'cursor-pointer')}
        role={onSelect ? 'button' : undefined}
        tabIndex={onSelect ? 0 : undefined}
        aria-label={onSelect ? `Inspect ${skill.name}` : undefined}
        onClick={onSelect ? () => onSelect(skill) : undefined}
        onKeyDown={
          onSelect
            ? (e) => {
                if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  onSelect(skill);
                }
              }
            : undefined
        }
      >
        {/* Header: optional select checkbox + icon + name + state badge */}
        <div className="flex items-start gap-2">
          {onToggleSelect && (
            <button
              type="button"
              role="checkbox"
              aria-checked={selected}
              aria-label={selected ? `Deselect ${skill.name}` : `Select ${skill.name}`}
              onClick={(e) => { e.stopPropagation(); onToggleSelect(skill); }}
              onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') e.stopPropagation(); }}
              className={cn(
                'flex-shrink-0 mt-0.5 w-4 h-4 rounded border flex items-center justify-center transition-all duration-150',
                selected
                  ? 'bg-primary/20 border-primary/50 text-primary opacity-100'
                  : 'border-border/50 text-transparent opacity-0 group-hover:opacity-100 group-focus-within:opacity-100',
                selectionActive && !selected && 'opacity-100',
              )}
            >
              <Check size={11} />
            </button>
          )}
          <div className="p-1.5 rounded-md border bg-surface-highlight/60 border-border/40 flex-shrink-0 mt-0.5 transition-colors duration-200 group-hover:bg-primary/10 group-hover:border-primary/20 group-focus-within:bg-primary/10 group-focus-within:border-primary/20">
            <BookOpen size={14} className="text-text-muted transition-colors duration-200 group-hover:text-primary/70 group-focus-within:text-primary/70" />
          </div>
          <span className="font-semibold log-text text-text-primary truncate flex-1 min-w-0 leading-tight mt-0.5">
            {skill.name}
          </span>
          {source && (
            <span
              title={source.repo}
              aria-label={`Imported from ${source.repo}`}
              className="flex-shrink-0 inline-flex items-center text-text-muted/50 transition-colors group-hover:text-text-muted/80 mt-0.5"
            >
              <GitBranch size={12} />
            </span>
          )}
          {hasLocalEdits && (
            <span
              title="Edited locally; a sync will skip this unless you overwrite"
              className="flex-shrink-0 text-[9px] font-medium uppercase tracking-wider px-1.5 py-0.5 rounded-full border border-amber-400/30 bg-amber-400/10 text-amber-300 mt-0.5"
            >
              Modified
            </span>
          )}
          <StateBadge state={skill.state} />
        </div>

        {/* Description */}
        <p className={cn(
          'log-text leading-relaxed line-clamp-2',
          skill.description ? 'text-text-secondary' : 'text-text-muted/40 italic',
        )}>
          {skill.description || 'No description'}
        </p>

        {/* Metadata line: category + weight summary, drawn only from existing
            fields. Fixed height (h-4) keeps every card the same height and
            reserves the trailing slot for a future validation chip — no
            reflow when one lands. Empty when a skill has no category or
            summary. */}
        <div
          data-testid="skill-meta"
          className="flex items-center gap-1.5 h-4 min-w-0 log-text-detail text-text-muted"
        >
          {category && (
            <span className="uppercase tracking-wider font-medium flex-shrink-0">
              {toTitleCase(category)}
            </span>
          )}
          {category && metaSummary && (
            <span aria-hidden="true" className="text-text-muted/40">·</span>
          )}
          {metaSummary && <span className="truncate min-w-0">{metaSummary}</span>}
        </div>
      </div>

      {/* Footer: actions */}
      <div className="px-3 pb-3 pt-2 border-t border-border-subtle/50 flex items-center justify-end gap-2">
        <SkillActions
          skill={skill}
          onToggle={handleToggle}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      </div>
    </div>
  );
});

SkillCard.displayName = 'SkillCard';
