import { memo } from 'react';
import {
  BookOpen,
  Power,
  PowerOff,
  Pencil,
  Trash2,
  CheckCircle,
  XCircle,
  Minus,
} from 'lucide-react';
import { cn } from '../../lib/cn';
import { IconButton } from '../ui/IconButton';
import type { AgentSkill, ItemState, SkillTestResult } from '../../types';

export interface SkillCardProps {
  skill: AgentSkill;
  testResult?: SkillTestResult | null;
  onEnable: (skill: AgentSkill) => void;
  onDisable: (skill: AgentSkill) => void;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (skill: AgentSkill) => void;
  className?: string;
}

function StateBadge({ state }: { state: ItemState }) {
  const styles: Record<ItemState, string> = {
    active: 'text-emerald-400 bg-emerald-400/10 border border-emerald-400/25',
    draft: 'text-amber-400 bg-amber-400/10 border border-amber-400/25',
    disabled: 'text-text-muted bg-surface border border-border/40',
  };

  return (
    <span className={cn('text-[9px] px-1.5 py-0.5 rounded font-mono flex-shrink-0', styles[state] ?? styles.draft)}>
      {state}
    </span>
  );
}

function TestStatusBadge({ testResult }: { testResult?: SkillTestResult | null }) {
  if (!testResult || testResult.status === 'untested') {
    return (
      <span className="flex items-center gap-1 text-[10px] font-medium text-amber-400/80 bg-amber-400/8 border border-amber-400/20 rounded px-1.5 py-0.5">
        <Minus size={10} />
        untested
      </span>
    );
  }

  if (testResult.failed > 0) {
    return (
      <span className="flex items-center gap-1 text-[10px] font-medium text-rose-400">
        <XCircle size={11} />
        {testResult.failed} failing
      </span>
    );
  }

  return (
    <span className="flex items-center gap-1 text-[10px] font-medium text-emerald-400">
      <CheckCircle size={11} />
      {testResult.passed} passing
    </span>
  );
}

export const SkillCard = memo(({
  skill,
  testResult,
  onEnable,
  onDisable,
  onEdit,
  onDelete,
  className,
}: SkillCardProps) => {
  return (
    <div
      className={cn(
        'relative rounded-xl overflow-hidden flex flex-col',
        'backdrop-blur-xl border transition-all duration-200 ease-out',
        'bg-gradient-to-b from-surface/95 via-surface/90 to-primary/[0.02]',
        'border-border/60 hover:border-primary/40 hover:shadow-node-hover',
        className,
      )}
    >
      {/* Top accent line */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-primary/40 to-transparent" />

      {/* Card body */}
      <div className="p-3 flex flex-col gap-2 flex-1">
        {/* Header: icon + name + state badge */}
        <div className="flex items-start gap-2">
          <div className="p-1.5 rounded-md border bg-primary/10 border-primary/20 flex-shrink-0 mt-0.5">
            <BookOpen size={14} className="text-primary/70" />
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
        <TestStatusBadge testResult={testResult} />

        <div className="flex items-center gap-0.5">
          {skill.state === 'active' ? (
            <IconButton
              icon={PowerOff}
              size="sm"
              variant="ghost"
              onClick={() => onDisable(skill)}
              tooltip="Disable skill"
              className="hover:text-amber-400"
            />
          ) : (
            <IconButton
              icon={Power}
              size="sm"
              variant="ghost"
              onClick={() => onEnable(skill)}
              tooltip="Activate skill"
              className="hover:text-emerald-400"
            />
          )}
          <IconButton
            icon={Pencil}
            size="sm"
            variant="ghost"
            onClick={() => onEdit(skill)}
            tooltip="Edit skill"
            className="hover:text-primary"
          />
          <IconButton
            icon={Trash2}
            size="sm"
            variant="ghost"
            onClick={() => onDelete(skill)}
            tooltip="Delete skill"
            className="hover:text-status-error"
          />
        </div>
      </div>
    </div>
  );
});

SkillCard.displayName = 'SkillCard';
