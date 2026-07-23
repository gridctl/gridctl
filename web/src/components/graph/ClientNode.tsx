import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { cn } from '../../lib/cn';
import { StatusDot } from '../ui/StatusDot';
import { useUIStore } from '../../stores/useUIStore';
import { LAYOUT } from '../../lib/constants';
import { getClientIcon } from '../../lib/clientIcons';
import type { ClientNodeData } from '../../types';

interface ClientNodeProps {
  data: ClientNodeData;
  selected?: boolean;
}

const ClientNode = memo(({ data, selected }: ClientNodeProps) => {
  const isCompact = useUIStore((s) => s.compactCards);
  const Icon = getClientIcon(data.slug);

  if (isCompact) {
    return (
      <div
        className={cn(
          'w-40 rounded-xl relative',
          'backdrop-blur-xl border transition-all duration-200 ease-out',
          'bg-gradient-to-b from-surface/95 via-surface/90 to-surface/80',
          'shadow-bevel',
          'flex items-center px-2.5 gap-2',
          selected && 'border-text-secondary ring-2 ring-white/15',
          !selected && 'border-border hover:shadow-node-hover hover:border-text-secondary/40'
        )}
        style={{ height: LAYOUT.CLIENT_HEIGHT_COMPACT }}
      >
        {/* Top accent */}
        <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-white/20 to-transparent" />

        <div className="p-1.5 rounded-md border bg-white/[0.04] border-white/10 flex-shrink-0">
          <Icon size={14} className="text-text-primary" />
        </div>
        <span className="font-semibold text-xs text-text-primary truncate min-w-0">
          {data.name}
        </span>
        <StatusDot status={data.status} />

        <Handle
          type="source"
          position={Position.Right}
          className={cn(
            '!w-2.5 !h-2.5 !bg-text-secondary !border-2 !border-background !rounded-full',
            'transition-all duration-200 hover:!scale-125'
          )}
          id="output"
        />
      </div>
    );
  }

  return (
    <div
      className={cn(
        'w-52 rounded-xl relative',
        'backdrop-blur-xl border transition-all duration-200 ease-out',
        'bg-gradient-to-b from-surface/95 via-surface/90 to-surface/80',
        'shadow-bevel',
        'flex items-center gap-3 px-3',
        selected && 'border-text-secondary ring-2 ring-white/15',
        !selected && 'border-border hover:shadow-node-hover hover:border-text-secondary/40'
      )}
      style={{ height: LAYOUT.CLIENT_HEIGHT }}
    >
      {/* Top accent */}
      <div className="absolute top-0 left-0 right-0 h-px bg-gradient-to-r from-transparent via-white/20 to-transparent" />

      {/* Icon */}
      <div className="p-2 rounded-lg border bg-white/[0.04] border-white/10 flex-shrink-0">
        <Icon size={20} className="text-text-primary" />
      </div>

      {/* Name, transport, and status stack beside the icon */}
      <div className="flex flex-col items-start gap-0.5 min-w-0">
        <span className="font-semibold text-xs text-text-primary truncate max-w-full">
          {data.name}
        </span>
        <span className="text-[9px] text-text-muted font-mono uppercase tracking-wider">
          {data.transport}
        </span>
        <span className="mt-0.5 inline-flex items-center gap-1.5 text-[10px] px-1.5 py-0.5 rounded font-medium border bg-status-running/10 border-status-running/25 text-status-running">
          <StatusDot status={data.status} />
          Linked
        </span>
      </div>

      {/* Source handle (connects to gateway on the right) */}
      <Handle
        type="source"
        position={Position.Right}
        className={cn(
          '!w-2.5 !h-2.5 !bg-text-secondary !border-2 !border-background !rounded-full',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="output"
      />
    </div>
  );
});

ClientNode.displayName = 'ClientNode';

export default ClientNode;
