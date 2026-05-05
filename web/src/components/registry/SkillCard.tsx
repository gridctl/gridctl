import { memo } from 'react';
import { BookOpen } from 'lucide-react';
import { cn } from '../../lib/cn';
import { StateBadge } from './StateBadge';
import { TestStatusBadge } from './TestStatusBadge';
import { SkillActions } from './SkillActions';
import type { AgentSkill, SkillTestResult } from '../../types';

export interface SkillCardProps {
  skill: AgentSkill;
  testResult?: SkillTestResult | null;
  onEnable: (skill: AgentSkill) => void;
  onDisable: (skill: AgentSkill) => void;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (skill: AgentSkill) => void;
  className?: string;
  style?: React.CSSProperties;
}

export const SkillCard = memo(({
  skill,
  testResult,
  onEnable,
  onDisable,
  onEdit,
  onDelete,
  className,
  style,
}: SkillCardProps) => {
  const handleToggle = (s: AgentSkill) => {
    if (s.state === 'active') onDisable(s);
    else onEnable(s);
  };

  return (
    <div
      style={style}
      className={cn(
        'group relative rounded-xl overflow-hidden flex flex-col',
        'backdrop-blur-xl border transition-all duration-200 ease-out',
        'bg-gradient-to-b from-surface/95 via-surface/90 to-primary/[0.02]',
        'border-white/[0.08] hover:border-primary/40 focus-within:border-primary/40 hover:shadow-node-hover',
        className,
      )}
    >
      {/* Top accent line — muted at rest, warms on hover/focus */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-white/10 group-hover:via-primary/40 group-focus-within:via-primary/40 to-transparent transition-colors duration-200" />

      {/* Card body */}
      <div className="p-3 flex flex-col gap-2 flex-1">
        {/* Header: icon + name + state badge */}
        <div className="flex items-start gap-2">
          <div className="p-1.5 rounded-md border bg-surface-highlight/60 border-border/40 flex-shrink-0 mt-0.5 transition-colors duration-200 group-hover:bg-primary/10 group-hover:border-primary/20 group-focus-within:bg-primary/10 group-focus-within:border-primary/20">
            <BookOpen size={14} className="text-text-muted transition-colors duration-200 group-hover:text-primary/70 group-focus-within:text-primary/70" />
          </div>
          <span className="font-semibold log-text text-text-primary truncate flex-1 min-w-0 leading-tight mt-0.5">
            {skill.name}
          </span>
          <StateBadge state={skill.state} />
        </div>

        {/* Description */}
        <p className={cn(
          'log-text leading-relaxed line-clamp-2',
          skill.description ? 'text-text-secondary' : 'text-text-muted/40 italic',
        )}>
          {skill.description || 'No description'}
        </p>
      </div>

      {/* Footer: test status + actions */}
      <div className="px-3 pb-3 pt-2 border-t border-border-subtle/50 flex items-center justify-between gap-2">
        <TestStatusBadge testResult={testResult} density="card" />
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
