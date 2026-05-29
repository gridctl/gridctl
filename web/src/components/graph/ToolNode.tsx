import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Wrench } from 'lucide-react';
import { cn } from '../../lib/cn';
import { LAYOUT } from '../../lib/constants';
import type { ToolNodeData } from '../../types';

interface ToolNodeProps {
  data: ToolNodeData;
}

/**
 * A single tool fanned out from an expanded MCP server. Renders as a compact
 * violet pill (tools inherit the server's violet theme) and slides in from the
 * left when mounted, so expanding a server reads as the tools flowing out of
 * it. Not selectable - it is a read-only affordance for PR 2.
 */
const ToolNode = memo(({ data }: ToolNodeProps) => {
  return (
    <div
      className={cn(
        'animate-slide-in-right',
        'flex items-center gap-2 px-2.5 rounded-lg relative',
        'border border-violet-500/25 bg-gradient-to-r from-surface/95 to-violet-500/[0.05]',
        'backdrop-blur-xl shadow-bevel',
        'transition-colors duration-200 hover:border-violet-400/50'
      )}
      style={{ width: LAYOUT.TOOL_WIDTH, height: LAYOUT.TOOL_HEIGHT }}
      title={`${data.serverName} · ${data.name}`}
    >
      <Wrench size={11} className="text-violet-400 flex-shrink-0" />
      {/* min-w-0 lets the flex item shrink so truncate actually clips long
          tool names instead of overflowing the pill. */}
      <span className="min-w-0 flex-1 font-mono text-[11px] text-text-secondary truncate tracking-tight">
        {data.name}
      </span>

      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-2 !h-2 !bg-violet-500 !border-2 !border-background !rounded-full',
          'transition-all duration-200'
        )}
        id="input"
      />
    </div>
  );
});

ToolNode.displayName = 'ToolNode';

export default ToolNode;
