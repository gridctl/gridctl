import { Power, PowerOff, Pencil, Trash2 } from 'lucide-react';
import { IconButton } from '../ui/IconButton';
import { cn } from '../../lib/cn';
import type { AgentSkill } from '../../types';

interface SkillActionsProps {
  skill: AgentSkill;
  onToggle: (skill: AgentSkill) => void;
  onEdit: (skill: AgentSkill) => void;
  onDelete: (skill: AgentSkill) => void;
  /** Render the activate/disable toggle. Set false when the toggle is rendered
   *  elsewhere (e.g. on the collapsed sidebar row) to avoid duplication. */
  showToggle?: boolean;
  className?: string;
}

/**
 * Icon-only action cluster (toggle / edit / delete) for registry surfaces.
 * Same visual treatment in the sidebar and the detached grid.
 */
export function SkillActions({
  skill,
  onToggle,
  onEdit,
  onDelete,
  showToggle = true,
  className,
}: SkillActionsProps) {
  return (
    <div className={cn('flex items-center gap-0.5', className)}>
      {showToggle && (
        skill.state === 'active' ? (
          <IconButton
            icon={PowerOff}
            size="sm"
            variant="ghost"
            onClick={() => onToggle(skill)}
            tooltip="Disable skill"
            className="hover:text-amber-400"
          />
        ) : (
          <IconButton
            icon={Power}
            size="sm"
            variant="ghost"
            onClick={() => onToggle(skill)}
            tooltip="Activate skill"
            className="hover:text-emerald-400"
          />
        )
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
  );
}
