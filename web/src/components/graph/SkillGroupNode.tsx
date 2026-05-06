import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { FolderOpen, CheckCircle, XCircle, Minus } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useUIStore } from '../../stores/useUIStore';
import { LAYOUT } from '../../lib/constants';
import type { SkillGroupNodeData } from '../../types';

interface SkillGroupNodeProps {
  data: SkillGroupNodeData;
  selected?: boolean;
}

function AggregateStatus({ failingSkills, untestedSkills, activeSkills }: {
  failingSkills: number;
  untestedSkills: number;
  activeSkills: number;
}) {
  if (failingSkills > 0) {
    return (
      <span className="flex items-center gap-0.5 text-[9px] font-medium text-rose-400">
        <XCircle size={9} />
        {failingSkills} failing
      </span>
    );
  }
  if (untestedSkills > 0) {
    return (
      <span className="flex items-center gap-0.5 text-[9px] font-medium text-text-muted">
        <Minus size={9} />
        {activeSkills} active · untested
      </span>
    );
  }
  return (
    <span className="flex items-center gap-0.5 text-[9px] font-medium text-emerald-400">
      <CheckCircle size={9} />
      {activeSkills} active · passing
    </span>
  );
}

const SkillGroupNode = memo(({ data, selected }: SkillGroupNodeProps) => {
  const isCompact = useUIStore((s) => s.compactCards);

  if (isCompact) {
    return (
      <div
        className={cn(
          'w-44 rounded-xl relative',
          'backdrop-blur-xl border transition-all duration-200 ease-out',
          'bg-gradient-to-b from-surface/95 via-surface/90 to-tertiary/[0.03]',
          'flex items-center px-2.5 gap-2',
          selected && 'border-tertiary shadow-glow-tertiary ring-2 ring-tertiary/20',
          !selected && 'border-border hover:shadow-node-hover hover:border-tertiary-light/60'
        )}
        style={{ height: LAYOUT.NODE_HEIGHT_COMPACT }}
      >
        <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-tertiary/40 to-transparent" />

        <div className="p-1.5 rounded-md border bg-tertiary/10 border-tertiary/25 flex-shrink-0">
          <FolderOpen size={14} className="text-tertiary" />
        </div>
        <span className="font-semibold text-xs text-text-primary truncate min-w-0 flex-1">
          {data.groupName}
        </span>
        <span className="text-[9px] font-medium px-1.5 py-0.5 rounded border text-tertiary bg-tertiary/10 border-tertiary/25 flex-shrink-0">
          {data.totalSkills}
        </span>

        <Handle
          type="target"
          position={Position.Left}
          className={cn(
            '!w-2.5 !h-2.5 !bg-tertiary !border-2 !border-background !rounded-full',
            'transition-all duration-200 hover:!scale-125'
          )}
          id="input"
        />
      </div>
    );
  }

  return (
    <div
      className={cn(
        'w-44 rounded-xl relative',
        'backdrop-blur-xl border transition-all duration-200 ease-out',
        'bg-gradient-to-b from-surface/95 via-surface/90 to-tertiary/[0.03]',
        'flex flex-col items-center justify-center text-center p-3 gap-1.5',
        selected && 'border-tertiary shadow-glow-tertiary ring-2 ring-tertiary/20',
        !selected && 'border-tertiary/25 hover:shadow-node-hover hover:border-tertiary/40'
      )}
      style={{ height: LAYOUT.NODE_HEIGHT }}
    >
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-tertiary/40 to-transparent" />

      {/* Icon */}
      <div className="p-2 rounded-lg border bg-tertiary/10 border-tertiary/25">
        <FolderOpen size={20} className="text-tertiary" />
      </div>

      {/* Group name + count badge */}
      <div className="flex items-center gap-1.5 max-w-[140px]">
        <span className="font-semibold text-xs text-text-primary truncate">
          {data.groupName}
        </span>
        <span className="text-[9px] font-medium px-1 py-0.5 rounded border text-tertiary bg-tertiary/10 border-tertiary/25 flex-shrink-0">
          {data.totalSkills}
        </span>
      </div>

      {/* Aggregate status */}
      <AggregateStatus
        failingSkills={data.failingSkills}
        untestedSkills={data.untestedSkills}
        activeSkills={data.activeSkills}
      />

      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-2.5 !h-2.5 !bg-tertiary !border-2 !border-background !rounded-full',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="input"
      />
    </div>
  );
});

SkillGroupNode.displayName = 'SkillGroupNode';

export default SkillGroupNode;
