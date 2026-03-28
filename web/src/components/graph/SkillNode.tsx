import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { BookOpen, CheckCircle, XCircle, Minus } from 'lucide-react';
import { cn } from '../../lib/cn';
import { useUIStore } from '../../stores/useUIStore';
import { LAYOUT } from '../../lib/constants';
import type { SkillNodeData, SkillTestStatus } from '../../types';

interface SkillNodeProps {
  data: SkillNodeData;
  selected?: boolean;
}

function TestBadge({ status, count }: { status: SkillTestStatus; count: number }) {
  if (status === 'passed') {
    return (
      <span className="flex items-center gap-0.5 text-[9px] font-medium text-emerald-400">
        <CheckCircle size={9} />
        tested
      </span>
    );
  }
  if (status === 'failing') {
    return (
      <span className="flex items-center gap-0.5 text-[9px] font-medium text-rose-400">
        <XCircle size={9} />
        failing
      </span>
    );
  }
  // untested
  return (
    <span className="flex items-center gap-0.5 text-[9px] font-medium text-text-muted">
      <Minus size={9} />
      {count > 0 ? 'untested' : 'no tests'}
    </span>
  );
}

function StateBadge({ state }: { state: SkillNodeData['state'] }) {
  const colors: Record<string, string> = {
    active: 'text-emerald-400 bg-emerald-400/10 border-emerald-400/20',
    draft: 'text-amber-400 bg-amber-400/10 border-amber-400/20',
    disabled: 'text-text-muted bg-surface-highlight border-border/30',
  };
  return (
    <span className={cn('text-[9px] font-medium px-1.5 py-0.5 rounded border uppercase tracking-wider', colors[state])}>
      {state}
    </span>
  );
}

const SkillNode = memo(({ data, selected }: SkillNodeProps) => {
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
          !selected && 'border-tertiary/25 hover:shadow-node-hover hover:border-tertiary/40'
        )}
        style={{ height: LAYOUT.NODE_HEIGHT_COMPACT }}
      >
        <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-tertiary/40 to-transparent" />

        <div className="p-1.5 rounded-md border bg-tertiary/10 border-tertiary/25 flex-shrink-0">
          <BookOpen size={14} className="text-tertiary" />
        </div>
        <span className="font-semibold text-xs text-text-primary truncate min-w-0 flex-1">
          {data.name}
        </span>
        <TestBadge status={data.testStatus} count={data.criteriaCount} />

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
        <BookOpen size={20} className="text-tertiary" />
      </div>

      {/* Name */}
      <span className="font-semibold text-xs text-text-primary truncate max-w-[140px] px-1">
        {data.name}
      </span>

      {/* Description */}
      {data.description && (
        <span className="text-[9px] text-text-muted leading-tight max-w-[140px] line-clamp-2 px-1">
          {data.description}
        </span>
      )}

      {/* State + test status */}
      <div className="flex items-center gap-1.5 flex-wrap justify-center">
        <StateBadge state={data.state} />
        <TestBadge status={data.testStatus} count={data.criteriaCount} />
      </div>

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

SkillNode.displayName = 'SkillNode';

export default SkillNode;
