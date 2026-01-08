import { memo } from 'react';
import { Handle, Position } from '@xyflow/react';
import { Terminal, Box, Wifi, Server, Hash } from 'lucide-react';
import { cn } from '../../lib/cn';
import { Badge } from '../ui/Badge';
import { StatusDot } from '../ui/StatusDot';
import type { MCPServerNodeData, ResourceNodeData } from '../../types';

export type CustomNodeData = MCPServerNodeData | ResourceNodeData;

interface CustomNodeProps {
  data: CustomNodeData;
  selected?: boolean;
}

const CustomNode = memo(({ data, selected }: CustomNodeProps) => {
  const isServer = data.type === 'mcp-server';

  // Choose icon
  const Icon = isServer ? Terminal : Box;

  // Get transport info for MCP servers
  const transport = isServer ? (data as MCPServerNodeData).transport : null;
  const TransportIcon = transport === 'stdio' ? Server : Wifi;
  const toolCount = isServer ? (data as MCPServerNodeData).toolCount : null;

  // Get endpoint/containerId for MCP servers
  const endpoint = isServer ? (data as MCPServerNodeData).endpoint : null;
  const containerId = isServer ? (data as MCPServerNodeData).containerId : null;
  const hasValidEndpoint = endpoint && endpoint !== 'unknown';
  const hasValidContainerId = containerId && containerId !== 'unknown';

  // Image for resources
  const image = !isServer ? (data as ResourceNodeData).image : null;
  const network = !isServer ? (data as ResourceNodeData).network : null;

  return (
    <div
      className={cn(
        'w-64 rounded-xl',
        'backdrop-blur-xl border transition-all duration-300 ease-out',
        isServer
          ? 'bg-gradient-to-br from-surface/95 to-primary/[0.02] border-border/50'
          : 'bg-gradient-to-br from-surface/95 to-secondary/[0.02] border-border/50',
        selected && isServer && 'border-primary shadow-glow-primary ring-1 ring-primary/30',
        selected && !isServer && 'border-secondary shadow-glow-secondary ring-1 ring-secondary/30',
        !selected && 'hover:shadow-node-hover hover:border-text-muted/30'
      )}
    >
      {/* Header with gradient accent */}
      <div className={cn(
        'px-3 py-2.5 flex items-center justify-between border-b relative',
        isServer ? 'border-primary/10 bg-primary/[0.03]' : 'border-secondary/10 bg-secondary/[0.03]'
      )}>
        {/* Accent line */}
        <div className={cn(
          'absolute top-0 left-0 right-0 h-px',
          isServer
            ? 'bg-gradient-to-r from-transparent via-primary/40 to-transparent'
            : 'bg-gradient-to-r from-transparent via-secondary/40 to-transparent'
        )} />

        <div className="flex items-center gap-2.5 min-w-0">
          <div className={cn(
            'p-1.5 rounded-lg border',
            isServer
              ? 'bg-primary/10 border-primary/20'
              : 'bg-secondary/10 border-secondary/20'
          )}>
            <Icon
              size={14}
              className={isServer ? 'text-primary' : 'text-secondary'}
            />
          </div>
          <span className="font-semibold text-sm text-text-primary truncate tracking-tight">
            {data.name}
          </span>
        </div>
        <StatusDot status={data.status} />
      </div>

      {/* Body */}
      <div className="p-3 space-y-2.5">
        {/* Endpoint Row (for HTTP MCP servers) */}
        {isServer && hasValidEndpoint && (
          <div className="space-y-1">
            <div className="flex items-center gap-1.5">
              <Wifi size={10} className="text-secondary" />
              <span className="text-[10px] uppercase tracking-widest font-medium text-text-muted">
                Endpoint
              </span>
            </div>
            <div className="text-xs text-text-secondary font-mono truncate bg-background/50 px-2 py-1 rounded-md" title={endpoint}>
              {endpoint}
            </div>
          </div>
        )}

        {/* Container Row (for stdio MCP servers) */}
        {isServer && !hasValidEndpoint && hasValidContainerId && (
          <div className="space-y-1">
            <div className="flex items-center gap-1.5">
              <Hash size={10} className="text-primary" />
              <span className="text-[10px] uppercase tracking-widest font-medium text-text-muted">
                Container
              </span>
            </div>
            <div className="text-xs text-text-secondary font-mono truncate bg-background/50 px-2 py-1 rounded-md" title={containerId}>
              {containerId.slice(0, 12)}
            </div>
          </div>
        )}

        {/* Image Row (for resources) */}
        {!isServer && image && (
          <div className="space-y-1">
            <span className="text-[10px] uppercase tracking-widest font-medium text-text-muted">
              Image
            </span>
            <div className="text-xs text-text-secondary font-mono truncate bg-background/50 px-2 py-1 rounded-md" title={image}>
              {image}
            </div>
          </div>
        )}

        {/* Transport + Tool count (for MCP servers) */}
        {isServer && transport && (
          <div className="flex items-center justify-between text-xs pt-1">
            <div className={cn(
              'flex items-center gap-1.5 px-2 py-1 rounded-md',
              transport === 'stdio' ? 'bg-primary/10 text-primary' : 'bg-secondary/10 text-secondary'
            )}>
              <TransportIcon size={11} />
              <span className="uppercase text-[10px] tracking-wider font-medium">
                {transport}
              </span>
            </div>
            {toolCount !== null && toolCount !== undefined && (
              <span className="text-text-secondary font-mono text-[11px]">
                {toolCount} tools
              </span>
            )}
          </div>
        )}

        {/* Network (for resources) */}
        {!isServer && network && (
          <div className="flex items-center gap-1.5 text-xs text-secondary bg-secondary/10 px-2 py-1 rounded-md w-fit">
            <Server size={11} />
            <span className="font-medium">{network}</span>
          </div>
        )}

        {/* Status Badge */}
        <div className="pt-1">
          <Badge status={data.status}>
            <span className="capitalize">{data.status}</span>
          </Badge>
        </div>
      </div>

      {/* Connection Handles */}
      <Handle
        type="target"
        position={Position.Left}
        className={cn(
          '!w-2.5 !h-2.5 !border-2 !border-background !rounded-full',
          isServer ? '!bg-primary' : '!bg-secondary',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="input"
      />
      <Handle
        type="source"
        position={Position.Right}
        className={cn(
          '!w-2.5 !h-2.5 !border-2 !border-background !rounded-full',
          isServer ? '!bg-primary' : '!bg-secondary',
          'transition-all duration-200 hover:!scale-125'
        )}
        id="output"
      />
    </div>
  );
});

CustomNode.displayName = 'CustomNode';

export default CustomNode;
